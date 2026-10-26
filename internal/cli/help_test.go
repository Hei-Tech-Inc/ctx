package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// runCtxBin runs bin/ctx with a hermetic environment. core "" is Bash
// (CTX_CORE=off); otherwise it is the ctx-core binary to forward to. With a
// pty the output is the terminal transcript.
func runCtxBin(t *testing.T, core string, pty bool, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(filepath.Join(repoRoot, "bin", "ctx"), args...)
	cmd.Dir = home
	cmd.Env = []string{
		"HOME=" + home, "CTX_DIR=" + filepath.Join(home, ".ctx"), "PATH=" + testutil.SystemPath(t),
		"TERM=xterm", "LC_ALL=en_US.UTF-8", "CTX_SECRET_PROVIDER=file",
	}
	if core == "" {
		cmd.Env = append(cmd.Env, "CTX_CORE=off")
	} else {
		cmd.Env = append(cmd.Env, "CTX_CORE_BIN="+core)
	}
	if pty {
		out, c := testutil.RunPTY(t, cmd, nil, 60*time.Second)
		return out, "", c
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return so.String(), se.String(), code
}

func TestVersionHelpCompletionMatchBash(t *testing.T) {
	core := buildCore(t)
	cases := [][]string{
		{"version"}, {"-v"}, {"--version"},
		{"help"}, {"--no-gum", "help"}, {"-h"}, {"--help"}, {"help", "bogus"},
		{"help", "setup"}, {"help", "import"}, {"help", "add"}, {"help", "wizard"}, {"help", "setup", "extra"},
		{"completion"}, {"completion", "zsh"}, {"completion", "bash"}, {"completion", "fish"},
		{"completion", "zsh", "extra"}, {"completion", ""}, {"completion", "bogus"}, {"completion", "Zsh"},
	}
	for _, args := range cases {
		args := append([]string{"--no-gum"}, args...)
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			wo, we, wc := runCtxBin(t, "", false, args...)
			go1, ge, gc := runCtxBin(t, core, false, args...)
			if wo != go1 || we != ge || wc != gc {
				t.Errorf("bash: %q %q %d\ngo:   %q %q %d", wo, we, wc, go1, ge, gc)
			}
			if wo == "" && we == "" {
				t.Error("no output at all")
			}
		})
	}
	// A bare `ctx` is help.
	wo, _, _ := runCtxBin(t, "", false, "--no-gum")
	go1, _, _ := runCtxBin(t, core, false, "--no-gum")
	if wo != go1 || !strings.Contains(go1, "client context switcher") {
		t.Errorf("bare ctx differs:\n%q\n%q", wo, go1)
	}
}

// With a terminal (and --no-gum) Bash prints bold escapes; so must Go.
func TestHelpPlainOnTTYMatchesBash(t *testing.T) {
	core := buildCore(t)
	for _, args := range [][]string{{"--no-gum", "help"}, {"--no-gum", "help", "setup"}} {
		wo, _, wc := runCtxBin(t, "", true, args...)
		go1, _, gc := runCtxBin(t, core, true, args...)
		if wo != go1 || wc != gc {
			t.Errorf("%v:\nbash %q\ngo   %q", args, wo, go1)
		}
	}
}

// ctx-core keeps its own `version`; bin/ctx forwards ctx-version.
func TestVersionForwardNames(t *testing.T) {
	core := buildCore(t)
	out, _, _ := runCore(t, t.TempDir(), "version")
	if !strings.HasPrefix(out, "ctx-core ") {
		t.Errorf("ctx-core version = %q", out)
	}
	for _, c := range []string{"ctx-version", "help", "completion"} {
		if _, _, code := runCore(t, t.TempDir(), "implements", c); code != 0 {
			t.Errorf("implements %s = %d", c, code)
		}
	}
	// bin/ctx hands each command to the core under the right name.
	fake := filepath.Join(t.TempDir(), "ctx-core")
	script := "#!/bin/sh\n[ \"$1\" = implements ] && exit 0\necho \"FWD $*\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = core
	for args, want := range map[string]string{
		"version": "FWD ctx-version", "-v": "FWD ctx-version", "--version": "FWD ctx-version",
		"help setup": "FWD help setup", "-h": "FWD help", "completion bash": "FWD completion bash",
	} {
		out, _, _ := runCtxBin(t, fake, false, strings.Fields(args)...)
		if strings.TrimSpace(out) != want {
			t.Errorf("ctx %s forwarded as %q, want %q", args, out, want)
		}
	}
}

func TestCompletionSyntax(t *testing.T) {
	for shell, tool := range map[string]string{"bash": "bash", "zsh": "zsh", "fish": "fish"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			testutil.ShellMissing(t, tool)
			t.Logf("%s not installed; skipping its syntax check", tool)
			continue
		}
		var buf bytes.Buffer
		if c := Completion(&buf, &buf, []string{shell}); c != 0 {
			t.Fatalf("%s: exit %d", shell, c)
		}
		f := filepath.Join(t.TempDir(), "c."+shell)
		if err := os.WriteFile(f, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(path, "-n", f).CombinedOutput(); err != nil {
			t.Errorf("%s -n: %v\n%s", tool, err, out)
		}
	}
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestHelpRichCard(t *testing.T) {
	t.Setenv("CTX_VERSION", "9.9.9")
	var out bytes.Buffer
	if c := Help(tui.Term{Out: &out, Err: &out, Rich: true, Colors: true}, nil); c != 0 {
		t.Fatalf("exit %d", c)
	}
	plain := ansiRE.ReplaceAllString(out.String(), "")
	for _, want := range []string{
		"╭", "╰", "ctx v9.9.9 — client context switcher", "SECRETS  (Keychain or ~/.ctx/secrets)",
		"  edit   <name>    Open profile in $EDITOR", "  setup --help     Same as: ctx help setup", "SETUP FLAGS",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("rich help lacks %q", want)
		}
	}
	// The gum rendering has no config-file or install-requirements section.
	if strings.Contains(plain, "SETUP CONFIG FILE") || strings.Contains(plain, "INSTALL REQUIREMENTS") {
		t.Error("rich help carries plain-only content")
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Error("rich help has no styling")
	}
}

// bin/ctx's gum help lines (one quoted argument each) are the rich card's
// content: R9 edits must land in both.
func TestHelpCardMatchesBashGumLines(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "bin", "ctx"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "if $HAS_GUM; then\n    gum style")
	end := strings.Index(text, "\n  else\n    # BOLD/RESET")
	if start < 0 || end < start {
		t.Fatal("cannot find the gum help branch")
	}
	t.Setenv("CTX_VERSION", "3.2.0")
	var out bytes.Buffer
	Help(tui.Term{Out: &out, Err: &out, Rich: true}, nil)
	card := ansiRE.ReplaceAllString(out.String(), "")
	lineRE := regexp.MustCompile(`(?m)^      "(  [^"]*(?:\\.[^"]*)*)"`)
	n := 0
	for _, m := range lineRE.FindAllStringSubmatch(text[start:end], -1) {
		want := strings.NewReplacer(`\$`, "$").Replace(m[1])
		n++
		if !strings.Contains(card, want) {
			t.Errorf("rich card lacks Bash gum line %q", want)
		}
	}
	if n < 40 {
		t.Errorf("only %d gum lines found", n)
	}
}

// Every command the completions offer is a case label in bin/ctx's dispatch.
func TestCompletionCommandsAreDispatched(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "bin", "ctx"))
	if err != nil {
		t.Fatal(err)
	}
	main := string(src)[strings.Index(string(src), "\nmain() {"):]
	m := regexp.MustCompile(`local cmds="([^"]*)"`).FindStringSubmatch(completionBash)
	if m == nil {
		t.Fatal("no cmds list in the bash completion")
	}
	for _, c := range strings.Fields(m[1]) {
		if !regexp.MustCompile(`(?m)^    (?:[a-z|-]*\|)?` + regexp.QuoteMeta(c) + `(?:\||\))`).MatchString(main) {
			t.Errorf("%q is completed but not dispatched", c)
		}
	}
	for _, c := range []string{"-v", "--version", "-h", "--help"} {
		if !strings.Contains(main, c) {
			t.Errorf("%q missing from dispatch", c)
		}
	}
}
