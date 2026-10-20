package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

func cloneFiles(more map[string]string) map[string]string {
	m := map[string]string{
		".ctx/profiles/acme.conf":  "PROFILE_NAME=acme\nGIT_EMAIL=a@b.co\nGIT_NAME=Acme\n",
		".ctx/profiles/named.conf": "PROFILE_NAME=named\nGIT_NAME=\"Only Name\"\n",
		".ctx/profiles/bare.conf":  "PROFILE_NAME=bare\n",
		".ctx/config":              "active=acme\n",
	}
	for k, v := range more {
		if v == "-" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	return m
}

func TestCloneMatchesBash(t *testing.T) {
	gh := "https://github.com/o/r"
	cases := []struct {
		name    string
		flags   []string // global flags, as bin/ctx parses them
		args    []string
		stdin   string
		env     []string
		files   map[string]string
		code    int
		vacuous string
	}{
		{name: "active profile, ssh url", args: []string{"git@github.com:o/r.git"}, vacuous: "Rewrote GitHub SSH URL → git@github-acme:o/r.git"},
		{name: "-p", args: []string{"-p", "named", "git@github.com:o/r"}, vacuous: "cloning as named (Only Name)"},
		{name: "--profile", args: []string{"--profile", "bare", "git@github.com:o/r"}, vacuous: "cloning as bare (unknown)"},
		{name: "-p after the url", args: []string{"git@github.com:o/r", "-p", "named"}, vacuous: "cloning as named"},
		{name: "--profile=x is a git arg", args: []string{"--profile=named", "git@github.com:o/r"}, vacuous: "--profile=named"},
		{name: "-p without value", args: []string{"-p"}, code: 1, vacuous: "Usage: ctx clone [-p|--profile <name>] [--]"},
		{name: "-p empty value", args: []string{"-p", "", "x"}, code: 1, vacuous: "Usage: ctx clone [-p|--profile <name>] [--]"},
		{name: "missing url", code: 1, vacuous: "Usage: ctx clone [-p|--profile <name>] <repo-url>"},
		{name: "only -p", args: []string{"-p", "acme"}, code: 1, vacuous: "Usage: ctx clone [-p|--profile <name>] <repo-url>"},
		{name: "https yes", args: []string{gh}, stdin: "y\n", vacuous: "Using: git@github-acme:o/r.git"},
		{name: "https default yes", args: []string{gh}, stdin: "\n", vacuous: "Using: git@github-acme:o/r.git"},
		{name: "https no", args: []string{gh}, stdin: "n\n", vacuous: "Keeping HTTPS URL"},
		{name: "https word yes is no", args: []string{gh}, stdin: "yes\n", vacuous: "Keeping HTTPS URL"},
		{name: "https eof cancels", args: []string{gh}, stdin: "", code: 130, vacuous: "Cancelled"},
		{name: "https quiet needs no answer", flags: []string{"--quiet"}, env: []string{"CTX_QUIET=1"}, args: []string{gh + ".git"}, vacuous: "git@github-acme:o/r.git"},
		{name: "ssh quiet", flags: []string{"-q"}, env: []string{"CTX_QUIET=1"}, args: []string{"git@github.com:o/r.git"}, vacuous: "Rewrote"},
		{name: "other https untouched, no prompt", args: []string{"https://gitlab.com/o/r.git"}, vacuous: "stdin="},
		{name: "ssh:// url", args: []string{"ssh://git@github.com/o/r"}, vacuous: "git@github-acme:o/r.git"},
		{name: "github- host unchanged", args: []string{"git@github-other:o/r.git"}, vacuous: "git@github-other:o/r.git"},
		{name: "extra git args", args: []string{"git@github.com:o/r.git", "my dir", "--depth", "1"}, vacuous: "my dir"},
		{name: "dashes: args around the url", args: []string{"-p", "acme", "--", "--depth", "1", "--branch", "x y", "git@github.com:o/r.git", "dir"}, vacuous: "x y"},
		{name: "dashes: url is not first token", args: []string{"--", "-q", "https://github.com/o/r", "d"}, stdin: "y\n", vacuous: "Using: "},
		{name: "dashes: scp style non-github", args: []string{"--", "host:dir/x"}, vacuous: "host:dir/x"},
		{name: "dashes: local path is no url", args: []string{"--", "./repo", "dir"}, code: 1, vacuous: "Could not find a repo URL in: ctx clone ... -- ./repo dir"},
		{name: "dashes: nothing after", args: []string{"--"}, code: 1, vacuous: "Could not find a repo URL in: ctx clone ... -- "},
		{name: "dashes: -p after dashes is a git arg", args: []string{"--", "git@github.com:o/r.git", "-p", "named"}, vacuous: "cloning as acme"},
		{name: "first arg is the url even as a flag", args: []string{"--depth", "1", "git@github.com:o/r.git"}, vacuous: "stdin="},
		{name: "git fails", args: []string{"git@github.com:o/r.git"}, env: []string{"GIT_EXIT=3"}, code: 3, vacuous: "git err"},
		{name: "stdin reaches git", args: []string{"git@github.com:o/r.git"}, stdin: "typed\n", vacuous: "stdin=typed"},
		{name: "no active profile", args: []string{"git@github.com:o/r.git"}, files: cloneFiles(map[string]string{".ctx/config": "-"}), code: 1, vacuous: "No active profile. Run: ctx use <profile> or ctx clone -p <profile> <url>"},
		{name: "profile missing", args: []string{"-p", "ghost", "git@github.com:o/r.git"}, code: 1, vacuous: "Profile 'ghost' not found. Run: ctx list"},
		{name: "dry run ssh", flags: []string{"--dry-run"}, env: []string{"CTX_DRY_RUN=1"}, args: []string{"git@github.com:o/r.git"}, vacuous: "[dry-run] would run: git clone git@github-acme:o/r.git"},
		{name: "dry run quoting", flags: []string{"--dry-run"}, env: []string{"CTX_DRY_RUN=1"}, args: []string{"--", "--branch", "x y", "git@github.com:o/r.git", "it's $HOME;", "é"}, vacuous: "[dry-run] would run: git clone --branch x\\ y git@github-acme:o/r.git"},
		{name: "dry run asks https and changes nothing", flags: []string{"--dry-run"}, env: []string{"CTX_DRY_RUN=1"}, args: []string{gh}, stdin: "n\n", vacuous: "[dry-run] would run: git clone https://github.com/o/r"},
		{name: "dry run without ctx dir", flags: []string{"--dry-run"}, env: []string{"CTX_DRY_RUN=1"}, args: []string{"-p", "acme", "git@github.com:o/r.git"}, files: map[string]string{".ctx/profiles/acme.conf": "GIT_EMAIL=a@b.co\n"}, vacuous: "[dry-run] would run"},
		{name: "dry run missing profile", flags: []string{"--dry-run"}, env: []string{"CTX_DRY_RUN=1"}, args: []string{"-p", "ghost", "git@github.com:o/r.git"}, code: 1, vacuous: "not found"},
		{name: "dry run quiet", flags: []string{"--dry-run", "-q"}, env: []string{"CTX_DRY_RUN=1", "CTX_QUIET=1"}, args: []string{gh}, vacuous: "[dry-run] would run: git clone git@github-acme:o/r.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newCVEnv(t, "git")
			files := c.files
			if files == nil {
				files = cloneFiles(nil)
			}
			wantEnv := []string{}
			for _, kv := range c.env {
				if strings.HasPrefix(kv, "GIT_") {
					k, v, _ := strings.Cut(kv, "=")
					e.p.Setenv(k, v)
				} else {
					wantEnv = append(wantEnv, kv)
				}
			}
			e.seed(t, files)
			want := e.bash(t, c.stdin, append(append([]string{}, c.flags...), append([]string{"clone"}, c.args...)...)...)
			e.seed(t, files)
			got := e.goRun(t, c.stdin, wantEnv, func(tm tui.Term) int { return Clone(tm, c.args) })
			e.compare(t, want, got)
			if want.code != c.code {
				t.Errorf("bash exit %d, want %d\n%s%s", want.code, c.code, want.stdout, want.stderr)
			}
			seen := want.stdout + want.stderr
			for _, a := range want.argv["git"] {
				seen += "\n" + strings.Join(a, " ")
			}
			if !strings.Contains(seen, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
			dry := false
			for _, kv := range c.env {
				dry = dry || kv == "CTX_DRY_RUN=1"
			}
			if dry && len(want.argv["git"]) != 0 {
				t.Errorf("dry run ran git: %q", want.argv["git"])
			}
			if !dry && c.code != 1 && c.code != 130 && len(want.argv["git"]) != 1 {
				t.Errorf("vacuous: bash ran git %d times", len(want.argv["git"]))
			}
		})
	}
}

// TestCloneGitKilled: a git killed by a signal gives 128+signal, as in a shell.
func TestCloneGitKilled(t *testing.T) {
	e := newCVEnv(t, "git")
	e.p.Setenv("GIT_KILL", "1")
	e.seed(t, cloneFiles(nil))
	got := e.goRun(t, "", nil, func(tm tui.Term) int { return Clone(tm, []string{"git@github.com:o/r.git"}) })
	if got.code != 143 {
		t.Errorf("exit %d, want 143", got.code)
	}
}

// TestCloneArgvExact pins the git command line for the main shapes.
func TestCloneArgvExact(t *testing.T) {
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"git@github.com:o/r.git"}, []string{"clone", "git@github-acme:o/r.git"}},
		{[]string{"git@github.com:o/r.git", "d", "--depth", "1"}, []string{"clone", "git@github-acme:o/r.git", "d", "--depth", "1"}},
		{[]string{"--", "--depth", "1", "git@github.com:o/r.git", "d"}, []string{"clone", "--depth", "1", "git@github-acme:o/r.git", "d"}},
	} {
		e := newCVEnv(t, "git")
		e.seed(t, cloneFiles(nil))
		got := e.goRun(t, "", nil, func(tm tui.Term) int { return Clone(tm, c.args) })
		if len(got.argv["git"]) != 1 || strings.Join(got.argv["git"][0], "|") != strings.Join(c.want, "|") {
			t.Errorf("%q: git argv %q, want %q", c.args, got.argv["git"], c.want)
		}
	}
}

// TestCloneCtrlC: the terminal sends Ctrl+C to ctx-core and git together.
// ctx-core must outlive git and return git's code, and Ctrl+C at the https
// question cancels with 130.
func TestCloneCtrlC(t *testing.T) {
	core := buildCore(t)
	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"git handles it", `trap 'exit 3' INT; echo ready > "$READY"; while :; do sleep 0.1; done`, 3},
		{"git dies by it", `echo ready > "$READY"; exec sleep 30`, 130},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctxDir := filepath.Join(home, ".ctx")
			if err := os.MkdirAll(filepath.Join(ctxDir, "profiles"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ctxDir, "profiles", "acme.conf"), []byte("PROFILE_NAME=acme\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(home, "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			testutil.Script(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+c.script+"\n")
			ready := filepath.Join(home, "ready")
			cmd := exec.Command(core, "clone", "-p", "acme", "git@gitlab.com:o/r.git")
			cmd.Env = append(os.Environ(), "HOME="+home, "CTX_DIR="+ctxDir, "READY="+ready, "CTX_NO_GUM=1", "PATH="+bin+string(os.PathListSeparator)+testutil.SystemPath(t))
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var eb bytes.Buffer
			cmd.Stderr = &eb
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("git never started: %s", eb.String())
				}
			}
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
			select {
			case err := <-done:
				code := 0
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				}
				if code != c.want {
					t.Errorf("exit %d, want %d (stderr %q)", code, c.want, eb.String())
				}
			case <-time.After(20 * time.Second):
				t.Fatal("ctx-core did not exit")
			}
		})
	}
}
