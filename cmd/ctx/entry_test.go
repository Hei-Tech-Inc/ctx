package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// globalEnvKeys are the variables runCtx exports; ctxTestEnv restores them.
var globalEnvKeys = []string{"CTX_DRY_RUN", "CTX_QUIET", "CTX_JSON", "CTX_NO_GUM"}

// ctxTestEnv points CTX_DIR and HOME at temp dirs holding one profile, "demo",
// and clears the global flag variables (restored after the test). It returns
// CTX_DIR.
func ctxTestEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	ctxDir := filepath.Join(home, ".ctx")
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", ctxDir)
	for _, k := range globalEnvKeys {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	writeProfile(t, ctxDir, "demo", filepath.Join(home, "work", "demo"))
	t.Chdir(home)
	return ctxDir
}

type result struct {
	code        int
	stdout, err string
}

func callCtx(args ...string) result {
	var out, errb bytes.Buffer
	code := runCtx(args, &out, &errb)
	return result{code, out.String(), errb.String()}
}

func callCore(args ...string) result {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return result{code, out.String(), errb.String()}
}

func TestParseGlobal(t *testing.T) {
	cases := []struct {
		name     string
		inQuiet  string
		args     []string
		wantRest []string
		wantEnv  map[string]string
	}{
		{"json before", "", []string{"--json", "list"}, []string{"list"},
			map[string]string{"CTX_JSON": "1", "CTX_DRY_RUN": "0", "CTX_QUIET": "0"}},
		{"quiet after subcommand", "", []string{"use", "demo", "-q"}, []string{"use", "demo"},
			map[string]string{"CTX_JSON": "0", "CTX_DRY_RUN": "0", "CTX_QUIET": "1"}},
		{"after -- passes through", "", []string{"run", "--", "tool", "--json"}, []string{"run", "--", "tool", "--json"},
			map[string]string{"CTX_JSON": "0", "CTX_DRY_RUN": "0", "CTX_QUIET": "0"}},
		{"no-gum and dry-run", "", []string{"--no-gum", "--dry-run", "setup"}, []string{"setup"},
			map[string]string{"CTX_JSON": "0", "CTX_DRY_RUN": "1", "CTX_QUIET": "0", "CTX_NO_GUM": "1"}},
		{"long quiet", "", []string{"--quiet", "list"}, []string{"list"},
			map[string]string{"CTX_JSON": "0", "CTX_DRY_RUN": "0", "CTX_QUIET": "1"}},
		{"quiet inherited", "1", []string{"list"}, []string{"list"},
			map[string]string{"CTX_JSON": "0", "CTX_DRY_RUN": "0", "CTX_QUIET": "1"}},
		{"no args", "", nil, nil,
			map[string]string{"CTX_JSON": "0", "CTX_DRY_RUN": "0", "CTX_QUIET": "0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CTX_QUIET", c.inQuiet)
			rest, env := parseGlobal(c.args)
			if !reflect.DeepEqual(rest, c.wantRest) {
				t.Errorf("rest = %q, want %q", rest, c.wantRest)
			}
			if !reflect.DeepEqual(env, c.wantEnv) {
				t.Errorf("env = %v, want %v", env, c.wantEnv)
			}
		})
	}
}

// TestParseGlobalExports checks runCtx exports what parseGlobal returns, as
// bin/ctx does, so the commands (which read the environment) see the flags.
func TestParseGlobalExports(t *testing.T) {
	ctxTestEnv(t)
	callCtx("--no-gum", "--dry-run", "-q", "--json", "list")
	for _, k := range globalEnvKeys {
		if got := os.Getenv(k); got != "1" {
			t.Errorf("%s = %q, want 1", k, got)
		}
	}
}

func TestAliases(t *testing.T) {
	cases := []struct {
		alias  []string
		target []string
	}{
		{[]string{"ls"}, []string{"list"}},
		{[]string{"st"}, []string{"status"}},
		{[]string{"rm", "nosuch"}, []string{"remove", "nosuch"}},
		{[]string{"del", "nosuch"}, []string{"remove", "nosuch"}},
		{[]string{"import", "--help"}, []string{"setup", "--help"}},
		{[]string{"add", "--help"}, []string{"setup", "--help"}},
	}
	for _, c := range cases {
		t.Run(c.alias[0], func(t *testing.T) {
			ctxTestEnv(t)
			want := callCore(c.target...)
			got := callCtx(c.alias...)
			if got != want {
				t.Errorf("ctx %q = %+v\nwant (ctx-core %q) %+v", c.alias, got, c.target, want)
			}
			if want.stdout == "" && want.err == "" {
				t.Errorf("ctx-core %q printed nothing; the comparison proves nothing", c.target)
			}
		})
	}
}

func TestVersionForms(t *testing.T) {
	for _, a := range []string{"version", "-v", "--version"} {
		t.Run(a, func(t *testing.T) {
			ctxTestEnv(t)
			want := result{0, "ctx version " + version + "\n", ""}
			if got := callCtx(a); got != want {
				t.Errorf("ctx %s = %+v, want %+v", a, got, want)
			}
		})
	}
}

func TestHelpForms(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			ctxTestEnv(t)
			want := callCore("help")
			if got := callCtx(args...); got != want {
				t.Errorf("ctx %q = %+v\nwant %+v", args, got, want)
			}
			if want.code != 0 || !strings.Contains(want.stdout, "ctx") {
				t.Errorf("help itself looks wrong: %+v", want)
			}
		})
	}
}

// unknownCommandWant is bin/ctx's unknown-command output (stdout not a
// terminal, so no color): the error line on stderr, the hint on stdout.
var unknownCommandWant = result{
	code:   1,
	stdout: "\n  Run 'ctx help' for usage.\n\n",
	err:    "✗ Unknown command: frobnicate\n",
}

// repoRoot is the checkout root, resolved before any test changes directory.
var repoRoot = func() string {
	p, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	return p
}()

// bashUnknownCommand runs `CTX_CORE=off bin/ctx --no-gum frobnicate` (the Bash
// entry point) and returns its output. It goes away with the Bash code: the
// pinned unknownCommandWant stays.
func bashUnknownCommand(t *testing.T) result {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command("bash", filepath.Join(repoRoot, "bin", "ctx"), "--no-gum", "frobnicate")
	cmd.Env = []string{"HOME=" + home, "CTX_DIR=" + filepath.Join(home, ".ctx"), "CTX_CORE=off",
		"PATH=" + os.Getenv("PATH"), "LC_ALL=en_US.UTF-8"}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	code := 0
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("bin/ctx: %v", err)
	}
	return result{code, out.String(), errb.String()}
}

func TestUnknownCommand(t *testing.T) {
	ctxTestEnv(t)
	got := callCtx("frobnicate")
	if got != unknownCommandWant {
		t.Errorf("ctx frobnicate = %+v, want %+v", got, unknownCommandWant)
	}
	if bash := bashUnknownCommand(t); got != bash {
		t.Errorf("ctx frobnicate = %+v, bin/ctx (Bash) = %+v", got, bash)
	}
	// Names only ctx-core answers to are unknown to ctx, as in bin/ctx.
	for _, c := range []string{"ctx-version", "implements"} {
		if r := callCtx(c); r.code != 1 || !strings.Contains(r.err, "Unknown command: "+c) {
			t.Errorf("ctx %s = %+v, want unknown command", c, r)
		}
	}
}

func TestListJSONGlobal(t *testing.T) {
	cases := []struct{ ctx, core []string }{
		{[]string{"--json", "list"}, []string{"list", "--json"}},
		{[]string{"list", "--json"}, []string{"list", "--json"}},
		{[]string{"--json", "st"}, []string{"status", "--json"}},
		{[]string{"list", "extra"}, []string{"list"}}, // bin/ctx forwards list without its arguments
	}
	for _, c := range cases {
		t.Run(strings.Join(c.ctx, " "), func(t *testing.T) {
			ctxTestEnv(t)
			want := callCore(c.core...)
			if got := callCtx(c.ctx...); got != want {
				t.Errorf("ctx %q = %+v\nwant %+v", c.ctx, got, want)
			}
			if want.code != 0 {
				t.Errorf("ctx-core %q failed: %+v", c.core, want)
			}
		})
	}
}

var (
	stampedOnce sync.Once
	stampedPath string
	stampedErr  error
)

const stampedVersion = "9.8.7"

// stampedBinary builds the binary once, stamped with stampedVersion, under a
// name that is neither ctx nor ctx-core.
func stampedBinary(t *testing.T) string {
	t.Helper()
	stampedOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ctx-entry")
		if err != nil {
			stampedErr = err
			return
		}
		stampedPath = filepath.Join(dir, "ctx-bin")
		out, err := exec.Command("go", "build", "-ldflags", "-X main.version="+stampedVersion, "-o", stampedPath, ".").CombinedOutput()
		if err != nil {
			stampedErr = errors.New(err.Error() + ": " + string(out))
		}
	})
	if stampedErr != nil {
		t.Fatalf("build: %v", stampedErr)
	}
	return stampedPath
}

// binaryAs links the stamped binary into a temp dir under name.
func binaryAs(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.Symlink(stampedBinary(t), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestArgv0CtxCore(t *testing.T) {
	core := binaryAs(t, "ctx-core")
	out, err := exec.Command(core, "version").Output()
	if err != nil || string(out) != "ctx-core "+stampedVersion+"\n" {
		t.Errorf("ctx-core version = %q, %v", out, err)
	}
	if err := exec.Command(core, "implements", "use").Run(); err != nil {
		t.Errorf("ctx-core implements use: %v", err)
	}

	ctx := binaryAs(t, "ctx")
	out, err = exec.Command(ctx, "version").Output()
	if err != nil || string(out) != "ctx version "+stampedVersion+"\n" {
		t.Errorf("ctx version = %q, %v", out, err)
	}
}

func TestInterruptExit130(t *testing.T) {
	ctx := binaryAs(t, "ctx")
	home := t.TempDir()
	env := []string{"HOME=" + home, "CTX_DIR=" + filepath.Join(home, ".ctx"), "TERM=xterm",
		"PATH=" + testutil.SystemPath(t), "LC_ALL=en_US.UTF-8", "GIT_CONFIG_NOSYSTEM=1", "CTX_SECRET_PROVIDER=file"}

	// setup installs its own handler: one cancel line, exit 130.
	t.Run("setup", func(t *testing.T) {
		cmd := exec.Command(ctx, "--no-gum", "setup")
		cmd.Dir, cmd.Env = home, env
		transcript, code := testutil.RunPTY(t, cmd, []string{testutil.CtrlC}, 30*time.Second)
		if code != 130 || strings.Count(transcript, "Cancelled by user.") != 1 {
			t.Errorf("exit %d, want 130 and one cancel line; transcript:\n%s", code, transcript)
		}
	})

	// remove has no handler of its own: ctx exits 130 (not killed by the
	// signal), as bin/ctx's `trap 'exit 130' INT` does.
	t.Run("no handler", func(t *testing.T) {
		writeProfile(t, filepath.Join(home, ".ctx"), "demo", filepath.Join(home, "work"))
		cmd := exec.Command(ctx, "--no-gum", "remove", "demo")
		cmd.Dir, cmd.Env = home, env
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = stdin.Close() }()
		var errb syncBuffer
		cmd.Stderr = &errb
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for !strings.Contains(errb.String(), "Remove profile") {
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				t.Fatalf("no prompt; stderr %q", errb.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Wait()
		ps := cmd.ProcessState
		if !ps.Exited() || ps.ExitCode() != 130 {
			t.Errorf("state %v, want exit status 130", ps)
		}
		if _, err := os.Stat(filepath.Join(home, ".ctx", "profiles", "demo.conf")); err != nil {
			t.Errorf("profile removed: %v", err)
		}
	})
}

// syncBuffer is a bytes.Buffer safe for a writer goroutine and a reader.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
