package guard

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

type guardStep struct {
	name string
	cmd  string   // status, install, uninstall
	cwd  string   // path relative to the temp root ("" = root)
	dry  bool     // --dry-run
	pre  []string // git config --global args to run before the step
}

type guardRes struct {
	out, err string
	code     int
	tree     map[string]string
}

// guardTree builds a temp root with a profile "acme" and repos, returning the root.
func guardTree(t *testing.T) (root string, env []string) {
	t.Helper()
	td, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(td, "home")
	ctxDir := filepath.Join(home, ".ctx")
	for _, d := range []string{filepath.Join(ctxDir, "profiles"), filepath.Join(td, "w", "acme"), filepath.Join(td, "plain")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(ctxDir, "profiles", "acme.conf"), "GIT_EMAIL=dev@acme.com\nWORK_DIR="+filepath.Join(td, "w", "acme")+"\n")
	w(filepath.Join(ctxDir, "profiles", "bare.conf"), "WORK_DIR="+filepath.Join(td, "w", "bare")+"\n")
	w(filepath.Join(ctxDir, "config"), "")
	env = []string{"HOME=" + home, "CTX_DIR=" + ctxDir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"), "CTX_DRY_RUN=0"}
	gitIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	gitIn(td, "config", "--global", "user.name", "Tester")
	gitIn(td, "config", "--global", "user.email", "me@personal.com")
	for _, r := range []string{"w/acme/match", "w/acme/mismatch", "w/acme/localhp", "w/bare/r", "elsewhere/r"} {
		p := filepath.Join(td, r)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(p, "init", "-q")
	}
	gitIn(filepath.Join(td, "w/acme/match"), "config", "user.email", "DEV@acme.com")
	gitIn(filepath.Join(td, "w/acme/localhp"), "config", "user.email", "dev@acme.com")
	gitIn(filepath.Join(td, "w/acme/localhp"), "config", "--local", "core.hooksPath", "/somewhere")
	return td, env
}

func TestGuardInstallMatchesBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctxBin, err := filepath.Abs("../../bin/ctx")
	if err != nil {
		t.Fatal(err)
	}
	steps := []guardStep{
		{name: "status-off", cmd: "status", cwd: "plain"},
		{name: "install-dry", cmd: "install", dry: true},
		{name: "install", cmd: "install"},
		{name: "install-again", cmd: "install"},
		{name: "status-on-nonrepo", cmd: "status", cwd: "plain"},
		{name: "status-on-match", cmd: "status", cwd: "w/acme/match"},
		{name: "status-on-mismatch", cmd: "status", cwd: "w/acme/mismatch"},
		{name: "status-on-local-hookspath", cmd: "status", cwd: "w/acme/localhp"},
		{name: "status-on-no-email", cmd: "status", cwd: "w/bare/r"},
		{name: "status-on-outside", cmd: "status", cwd: "elsewhere/r"},
		{name: "uninstall-dry", cmd: "uninstall", dry: true},
		{name: "uninstall", cmd: "uninstall"},
		{name: "uninstall-again", cmd: "uninstall"},
		{name: "status-off-in-repo", cmd: "status", cwd: "w/acme/match"},
		{name: "foreign-hookspath-install", cmd: "install", pre: []string{"core.hooksPath", "@ROOT@/mine"}},
		{name: "foreign-hookspath-status", cmd: "status", cwd: "plain"},
		{name: "foreign-hookspath-uninstall", cmd: "uninstall"},
		{name: "tilde-hookspath-install", cmd: "install", pre: []string{"core.hooksPath", "~/.ctx/git-hooks"}},
		{name: "tilde-hookspath-status", cmd: "status", cwd: "plain"},
		{name: "tilde-hookspath-uninstall", cmd: "uninstall"},
	}

	runAll := func(useGo bool) map[string]guardRes {
		td, env := guardTree(t)
		res := map[string]guardRes{}
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			t.Setenv(k, v)
		}
		t.Setenv("CTX_CORE", "off")
		for _, s := range steps {
			cwd := filepath.Join(td, s.cwd)
			if len(s.pre) > 0 {
				args := append([]string{"config", "--global"}, strings.ReplaceAll(s.pre[0], "@ROOT@", td), strings.ReplaceAll(s.pre[1], "@ROOT@", td))
				cmd := exec.Command("git", args...)
				cmd.Dir = td
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("pre: %v: %s", err, out)
				}
			}
			var out, errb bytes.Buffer
			code := 0
			if useGo {
				t.Chdir(cwd)
				if s.dry {
					t.Setenv("CTX_DRY_RUN", "1")
				} else {
					t.Setenv("CTX_DRY_RUN", "0")
				}
				switch s.cmd {
				case "install":
					code = Install(&out, &errb)
				case "uninstall":
					code = Uninstall(&out, &errb)
				case "status":
					code = Status(&out, &errb)
				}
			} else {
				args := []string{ctxBin, "guard", s.cmd}
				if s.dry {
					args = append(args, "--dry-run")
				}
				cmd := exec.Command(bash, args...)
				cmd.Dir = cwd
				cmd.Stdout, cmd.Stderr = &out, &errb
				if err := cmd.Run(); err != nil {
					var ee *exec.ExitError
					if !errors.As(err, &ee) {
						t.Fatal(err)
					}
					code = ee.ExitCode()
				}
			}
			norm := func(b *bytes.Buffer) string { return strings.ReplaceAll(b.String(), td, "<ROOT>") }
			res[s.name] = guardRes{out: norm(&out), err: norm(&errb), code: code, tree: testutil.Snapshot(t, td)}
		}
		return res
	}
	want := runAll(false)
	got := runAll(true)
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			w, g := want[s.name], got[s.name]
			if g.code != w.code {
				t.Errorf("exit go=%d bash=%d", g.code, w.code)
			}
			if g.out != w.out {
				t.Errorf("stdout\n go:   %q\n bash: %q", g.out, w.out)
			}
			if g.err != w.err {
				t.Errorf("stderr\n go:   %q\n bash: %q", g.err, w.err)
			}
			if d := testutil.DiffSnapshots(w.tree, g.tree); d != "" {
				t.Errorf("tree differs:\n%s", d)
			}
		})
	}
	// Sanity: the install step really wrote hooks (guards against a vacuous comparison).
	if !strings.Contains(want["install"].out, "ctx guard on") || want["install"].tree["home/.ctx/git-hooks/pre-commit"] == "" {
		t.Fatalf("bash install did not install: %+v", want["install"].out)
	}
}
