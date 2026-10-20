package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

type edRun struct {
	stdout, stderr string
	code           int
	argv           [][]string
	tree           map[string]string
}

// editScript echoes the environment it was given, reads stdin, and exits with $ED_EXIT.
const editScript = `echo "out FOO=${FOO:-}"; echo "err" >&2; echo "stdin=$(cat)"; exit ${ED_EXIT:-0}`

func TestEditMatchesBash(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		env     func(ed string) []string // VISUAL/EDITOR/... given the fake editor's path
		nano    bool                     // put the fake as "nano" on PATH
		stdin   string
		noProf  bool
		vacuous string
	}{
		{name: "visual wins", args: []string{"acme"}, env: func(ed string) []string { return []string{"VISUAL=" + ed, "EDITOR=/nonexistent/ed"} }, vacuous: "out FOO="},
		{name: "editor with args", args: []string{"acme"}, env: func(ed string) []string { return []string{"EDITOR=" + ed + " -w --flag 'two words'"} }, vacuous: "out FOO="},
		{name: "visual empty falls to editor", args: []string{"acme"}, env: func(ed string) []string { return []string{"VISUAL=", "EDITOR=" + ed} }, vacuous: "out FOO="},
		{name: "unset uses nano", args: []string{"acme"}, nano: true, env: func(string) []string { return []string{"VISUAL=", "EDITOR="} }, vacuous: "out FOO="},
		{name: "env and stdin reach the editor", args: []string{"acme"}, stdin: "typed\n", env: func(ed string) []string { return []string{"EDITOR=" + ed, "FOO=bar"} }, vacuous: "stdin=typed"},
		{name: "exit code propagated", args: []string{"acme"}, env: func(ed string) []string { return []string{"EDITOR=" + ed, "ED_EXIT=7"} }, vacuous: "out FOO="},
		{name: "extra args ignored", args: []string{"acme", "other"}, env: func(ed string) []string { return []string{"EDITOR=" + ed} }, vacuous: "out FOO="},
		{name: "editor not found", args: []string{"acme"}, env: func(string) []string { return []string{"EDITOR=/nonexistent/ed"} }, vacuous: "No such file"},
		{name: "shell syntax in editor", args: []string{"acme"}, env: func(ed string) []string { return []string{"EDITOR=" + ed + " ;"} }, vacuous: "out FOO="},
		{name: "missing profile", args: []string{"ghost"}, env: func(ed string) []string { return []string{"EDITOR=" + ed} }, vacuous: "Profile 'ghost' not found."},
		{name: "no args", env: func(ed string) []string { return []string{"EDITOR=" + ed} }, vacuous: "Usage: ctx edit <profile>"},
		{name: "empty arg", args: []string{""}, env: func(ed string) []string { return []string{"EDITOR=" + ed} }, vacuous: "Usage: ctx edit <profile>"},
		{name: "dry run does not stop it", args: []string{"acme"}, env: func(ed string) []string { return []string{"EDITOR=" + ed, "CTX_DRY_RUN=1"} }, vacuous: "out FOO="},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newPtest(t)
			f := newPFixture(p)
			dir, log := testutil.FakeBin(t, "myed", editScript)
			ed := filepath.Join(dir, "myed")
			path := testutil.SystemPath(t)
			if c.nano {
				ndir, nlog := testutil.FakeBin(t, "nano", editScript)
				path, log = ndir+string(os.PathListSeparator)+path, nlog
			}
			p.Setenv("PATH", path)
			p.Setenv("CTX_NO_GUM", "1")
			f.write("profiles/acme.conf", "PROFILE_NAME=acme\n")
			var extra []string
			if c.env != nil {
				extra = c.env(ed)
			}
			collect := func(o, eb string, code int) edRun {
				return edRun{o, eb, code, testutil.ReadArgv(t, log), testutil.Snapshot(t, f.home)}
			}
			args := c.args
			// Bash.
			bargs := append([]string{"--no-gum", "edit"}, args...)
			cmd := exec.Command(bashBin(t), bargs...)
			cmd.Env = p.Environ(append([]string{"CTX_CORE=off"}, extra...)...)
			cmd.Stdin = strings.NewReader(c.stdin)
			var o, eb bytes.Buffer
			cmd.Stdout, cmd.Stderr = &o, &eb
			code := 0
			if err := cmd.Run(); err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				code = ee.ExitCode()
			}
			want := collect(o.String(), eb.String(), code)
			_ = os.Remove(log)
			// Go.
			o.Reset()
			eb.Reset()
			p.inProc("", func() {
				for _, kv := range extra {
					k, v, _ := strings.Cut(kv, "=")
					_ = os.Setenv(k, v) // inProc restores the environment
				}
				code = Edit(tui.Term{In: strings.NewReader(c.stdin), Out: &o, Err: &eb}, args)
			})
			got := collect(o.String(), eb.String(), code)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n argv go %q bash %q\n tree: %s",
					got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, got.argv, want.argv, testutil.DiffSnapshots(want.tree, got.tree))
			}
			if !strings.Contains(want.stdout+want.stderr, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
			if c.name == "exit code propagated" && want.code != 7 {
				t.Errorf("vacuous: bash exit %d, want 7", want.code)
			}
			if strings.HasPrefix(c.vacuous, "out") || strings.HasPrefix(c.vacuous, "stdin") {
				if c.name == "shell syntax in editor" {
					return // "<ed> ; "$@"": the editor runs without the file
				}
				if len(want.argv) != 1 || len(want.argv[0]) == 0 || !strings.HasSuffix(want.argv[0][len(want.argv[0])-1], "/profiles/acme.conf") {
					t.Errorf("vacuous: editor argv %q", want.argv)
				}
			}
		})
	}
}

// TestEditCtrlC: Ctrl+C goes to the whole foreground group. ctx-core must not
// die before the editor: the editor's own handling decides the exit code.
func TestEditCtrlC(t *testing.T) {
	core := buildCore(t)
	for _, c := range []struct {
		name, script string
		want         int
	}{
		{"editor handles it", `trap 'exit 3' INT; echo ready > "$READY"; while :; do sleep 0.1; done`, 3},
		{"editor dies by it", `echo ready > "$READY"; exec sleep 30`, 130},
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
			ed := filepath.Join(home, "ed.sh")
			testutil.Script(t, ed, "#!/bin/sh\n"+c.script+"\n")
			ready := filepath.Join(home, "ready")
			cmd := exec.Command(core, "edit", "acme")
			cmd.Env = append(os.Environ(), "HOME="+home, "CTX_DIR="+ctxDir, "EDITOR="+ed, "READY="+ready, "VISUAL=", "CTX_NO_GUM=1")
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
					t.Fatalf("editor never started: %s", eb.String())
				}
			}
			time.Sleep(100 * time.Millisecond) // let the trap be installed
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
