package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// bashOld is the bash the differential tests run: macOS's /bin/bash 3.2 when
// present (the shell ctx must support), else the first bash on PATH.
func bashOld() string {
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "bash"
}

// bashText runs `CTX_CORE=off bin/ctx --no-gum args...` with stdout piped and
// returns stdout and the exit code.
func (f *fixture) bashText(dir string, args ...string) (string, int) {
	f.t.Helper()
	cmd := exec.Command(bashOld(), append([]string{filepath.Join(repoRoot, "bin", "ctx"), "--no-gum"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CTX_CORE=off", "HOME="+f.home, "CTX_DIR="+f.dir)
	var out bytes.Buffer
	cmd.Stdout = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			f.t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

// goText runs fn and returns stdout, stderr and the exit code.
func goText(fn func(tui.Term) int) (string, string, int) {
	var out, errb bytes.Buffer
	code := fn(tui.Term{Out: &out, Err: &errb})
	return out.String(), errb.String(), code
}

func TestListTextMatchesBash(t *testing.T) {
	cases := map[string]func(f *fixture){
		"no profiles": func(f *fixture) {},
		"active and inactive": func(f *fixture) {
			f.profile("acme", "GIT_NAME", "A", "GIT_EMAIL", "a@x.io")
			f.profile("zed", "GIT_NAME", "Z", "GIT_EMAIL", "z@x.io")
			f.write("config", "active=acme\n")
		},
		"all tags": func(f *fixture) {
			f.profile("full", "GIT_NAME", "F", "GIT_EMAIL", "f@x.io", "WORK_DIR", f.home+"/work/full",
				"GITHUB_USER", "fu", "AWS_PROFILE_NAME", "fa", "AZURE_SUBSCRIPTION", "sub",
				"GCP_PROJECT", "gp", "KUBE_CONTEXT", "kc", "SECRET_KEYS", "A B  C")
		},
		"no git identity": func(f *fixture) { f.profile("bare", "GITHUB_USER", "u") },
		"workdir under home": func(f *fixture) {
			f.profile("h", "WORK_DIR", f.home+"/code")
		},
		"home mid-path": func(f *fixture) {
			f.profile("m", "WORK_DIR", "/mnt"+f.home+"/code")
		},
		"workdir outside home": func(f *fixture) { f.profile("o", "WORK_DIR", "/srv/x") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			setup(f)
			want, _ := f.bashText(f.home, "list")
			got, _, code := goText(ListText)
			if code != 0 || got != want {
				t.Errorf("code=%d\n got: %q\nwant: %q", code, got, want)
			}
			if name != "no profiles" && !strings.Contains(got, "ctx profiles") {
				t.Errorf("missing heading: %q", got)
			}
		})
	}
}

func TestListTextNoTTY(t *testing.T) {
	f := newFixture(t)
	f.profile("acme", "GIT_NAME", "A", "WORK_DIR", f.home+"/w")
	f.write("config", "active=acme\n")
	got, _, _ := goText(ListText)
	if strings.Contains(got, "\033") {
		t.Errorf("escape codes in non-tty output: %q", got)
	}
}
