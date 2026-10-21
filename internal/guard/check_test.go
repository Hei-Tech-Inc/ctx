package guard

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}

// TestGuardMatchesBash compares Check with `CTX_CORE=off bin/ctx guard check`.
func TestGuardMatchesBash(t *testing.T) {
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
	td, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(td, "home")
	ctxDir := filepath.Join(home, ".ctx")
	acme := filepath.Join(td, "w", "acme")
	other := filepath.Join(td, "w", "other")
	for _, d := range []string{filepath.Join(ctxDir, "profiles"), filepath.Join(home, ".ssh"), acme, other, filepath.Join(td, "elsewhere", "r"), filepath.Join(td, "plain")} {
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
	w(filepath.Join(ctxDir, "profiles", "acme.conf"), "GIT_EMAIL=dev@acme.com\nWORK_DIR="+acme+"\n")
	w(filepath.Join(ctxDir, "profiles", "other.conf"), "WORK_DIR="+other+"\n")
	w(filepath.Join(ctxDir, "config"), "")
	w(filepath.Join(home, ".ssh", "ctx_config"), "Host github-acme\n  HostName github.com\n")

	baseEnv := []string{"HOME=" + home, "CTX_DIR=" + ctxDir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"), "CTX_CORE=off"}
	run(t, td, baseEnv, "git", "config", "--global", "user.name", "Tester")
	run(t, td, baseEnv, "git", "config", "--global", "user.email", "me@personal.com")

	mk := func(rel string) string {
		p := filepath.Join(td, rel)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		run(t, p, baseEnv, "git", "init", "-q")
		return p
	}
	repoMismatch := mk("w/acme/mismatch")
	repoLocal := mk("w/acme/local")
	run(t, repoLocal, baseEnv, "git", "config", "user.email", "x@corp.com")
	repoMatch := mk("w/acme/match")
	run(t, repoMatch, baseEnv, "git", "config", "user.email", "DEV@acme.com")
	repoEnv := mk("w/acme/envrepo")
	run(t, repoEnv, baseEnv, "git", "config", "user.email", "dev@acme.com")
	repoElse := mk("elsewhere/r")

	cases := []struct {
		name string
		dir  string
		env  []string
		args []string
		code int
	}{
		{"match", repoMatch, nil, []string{"pre-commit"}, 0},
		{"mismatch-config", repoMismatch, nil, []string{"pre-commit"}, 3},
		{"local-override", repoLocal, nil, []string{"pre-commit"}, 3},
		{"env-override", repoEnv, []string{"GIT_AUTHOR_EMAIL=zed@corp.com"}, []string{"pre-commit"}, 3},
		{"push-other-alias", repoMatch, nil, []string{"pre-push", "origin", "git@github-other:org/r.git"}, 3},
		{"push-other-alias-ssh", repoMatch, nil, []string{"pre-push", "origin", "ssh://git@github-other:22/org/r.git"}, 3},
		{"push-own-alias", repoMatch, nil, []string{"pre-push", "origin", "git@github-acme:org/r.git"}, 0},
		{"push-unknown-alias", repoMatch, nil, []string{"pre-push", "origin", "git@github-nope:org/r.git"}, 0},
		{"push-github-scp", repoMatch, nil, []string{"pre-push", "origin", "git@github.com:org/r.git"}, 0},
		{"push-github-ssh", repoMatch, nil, []string{"pre-push", "origin", "ssh://git@github.com/org/r"}, 0},
		{"push-github-https", repoMatch, nil, []string{"pre-push", "origin", "https://github.com/org/r"}, 0},
		{"push-other-host", repoMatch, nil, []string{"pre-push", "origin", "https://gitlab.com/org/r"}, 0},
		{"push-no-url", repoMatch, nil, []string{"pre-push"}, 0},
		{"outside-workdir", repoElse, nil, []string{"pre-commit"}, 0},
		{"not-a-repo", filepath.Join(td, "plain"), nil, []string{"pre-commit"}, 0},
		{"ctx-guard-off", repoMismatch, []string{"CTX_GUARD=off"}, []string{"pre-commit"}, 3},
		{"unknown-hook", repoMismatch, nil, []string{"post-commit"}, 0},
		{"no-hook", repoMismatch, nil, nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Bash reference.
			cmd := exec.Command(bash, append([]string{ctxBin, "guard", "check"}, c.args...)...)
			cmd.Dir = c.dir
			cmd.Env = append(append(os.Environ(), baseEnv...), c.env...)
			var bout, berr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &bout, &berr
			bcode := 0
			if err := cmd.Run(); err != nil {
				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					t.Fatal(err)
				}
				bcode = ee.ExitCode()
			}
			if bcode != c.code {
				t.Fatalf("bash exit = %d, want %d (stderr %q)", bcode, c.code, berr.String())
			}

			if wantMsg := c.code == 3 || strings.HasPrefix(c.name, "push-github-"); wantMsg != (berr.Len() > 0) {
				t.Fatalf("bash stderr = %q, message expected: %v", berr.String(), wantMsg)
			}

			// Go.
			for _, kv := range append(append([]string{}, baseEnv...), c.env...) {
				k, v, _ := cutEnv(kv)
				t.Setenv(k, v)
			}
			t.Setenv("CTX_CORE", "")
			t.Chdir(c.dir)
			var gerr bytes.Buffer
			hook := ""
			var rest []string
			if len(c.args) > 0 {
				hook, rest = c.args[0], c.args[1:]
			}
			gcode := Check(hook, rest, &gerr)
			if gcode != bcode {
				t.Errorf("go exit = %d, bash %d", gcode, bcode)
			}
			if gerr.String() != berr.String() {
				t.Errorf("stderr differs\n go:   %q\n bash: %q", gerr.String(), berr.String())
			}
			if bout.Len() != 0 {
				t.Errorf("bash wrote stdout %q", bout.String())
			}
		})
	}
}

func cutEnv(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return kv, "", false
}
