package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// Fake tools for the verify and clone differential tests. Each logs its argv
// (testutil.FakeBin); their behavior comes from the environment.
const (
	// fakeSSH hangs (sleeps) unless told never to prompt, like a real ssh
	// waiting for a host-key answer or a passphrase.
	fakeSSH = `case " $* " in *" BatchMode=yes "*) ;; *) sleep 30 ;; esac
printf '%b' "${SSH_OUT-Hi acme! You have successfully authenticated, but GitHub does not provide shell access.\n}" >&2
exit ${SSH_EXIT:-1}`
	fakeGH  = `printf '%b' "${GH_OUT-github.com\n  ✓ Logged in to github.com account alice (keyring)\n  - Active account: true\n}"; exit ${GH_EXIT:-0}`
	fakeGit = `echo "git out"; echo "git err" >&2; echo "stdin=$(cat)"; [ -n "$GIT_KILL" ] && kill -TERM $$; exit ${GIT_EXIT:-0}`
)

// cvEnv is a fixture HOME with fake ssh, gh and git first on PATH.
type cvEnv struct {
	f    *fixture
	p    *ptest
	logs map[string]string // tool -> argv log
}

func newCVEnv(t *testing.T, tools ...string) *cvEnv {
	t.Helper()
	p := newPtest(t)
	e := &cvEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
	path := ""
	scripts := map[string]string{"ssh": fakeSSH, "gh": fakeGH, "git": fakeGit}
	for _, tool := range tools {
		dir, log := testutil.FakeBin(t, tool, scripts[tool])
		path += dir + string(os.PathListSeparator)
		e.logs[tool] = log
	}
	p.Setenv("PATH", path+testutil.SystemPath(t))
	p.Setenv("CTX_NO_GUM", "1")
	return e
}

// seed resets HOME to the given files (paths relative to HOME; "$HOME" in a
// content is replaced) and the fake tools' logs.
func (e *cvEnv) seed(t *testing.T, files map[string]string) {
	t.Helper()
	if err := os.RemoveAll(e.f.home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.f.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, c := range files {
		p := filepath.Join(e.f.home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, "/") {
			continue
		}
		if err := os.WriteFile(p, []byte(strings.ReplaceAll(c, "$HOME", e.f.home)), mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range e.logs {
		_ = os.Remove(l)
	}
}

type cvRun struct {
	stdout, stderr string
	code           int
	argv           map[string][][]string
	tree           map[string]string
}

func (e *cvEnv) collect(t *testing.T, o, eb string, code int) cvRun {
	argv := map[string][][]string{}
	for tool, l := range e.logs {
		argv[tool] = testutil.ReadArgv(t, l)
	}
	return cvRun{o, eb, code, argv, testutil.Snapshot(t, e.f.home)}
}

// bash runs `bin/ctx --no-gum <flags...> <args...>` with CTX_CORE=off.
func (e *cvEnv) bash(t *testing.T, stdin string, args ...string) cvRun {
	t.Helper()
	cmd := exec.Command(bashBin(t), append([]string{"--no-gum"}, args...)...)
	cmd.Env = e.p.Environ("CTX_CORE=off")
	cmd.Dir = e.f.home
	cmd.Stdin = strings.NewReader(stdin)
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
	return e.collect(t, o.String(), eb.String(), code)
}

// goRun runs fn in-process with extra env (the global flags as bin/ctx exports them).
func (e *cvEnv) goRun(t *testing.T, stdin string, env []string, fn func(tui.Term) int) cvRun {
	t.Helper()
	var o, eb bytes.Buffer
	code := 0
	e.p.inProc(e.f.home, func() {
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			_ = os.Setenv(k, v) // inProc restores the environment
		}
		code = fn(tui.Term{In: strings.NewReader(stdin), Out: &o, Err: &eb})
	})
	return e.collect(t, o.String(), eb.String(), code)
}

func (e *cvEnv) compare(t *testing.T, want, got cvRun) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n argv go %q\n argv bash %q\n tree diff: %s",
			got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, got.argv, want.argv, testutil.DiffSnapshots(want.tree, got.tree))
	}
}

const vConf = "PROFILE_NAME=acme\nGIT_EMAIL=a@b.co\nGITHUB_USER=alice\nWORK_DIR=$HOME/work\nSSH_KEY_PATH=$HOME/.ssh/id_acme\n"

func vFiles(conf string, more map[string]string) map[string]string {
	m := map[string]string{
		".ctx/profiles/acme.conf": conf,
		"work/.keep":              "",
		".ssh/id_acme":            "KEY",
		".ssh/ctx_config":         "Host github-acme\n  HostName github.com\n",
		".ctx/config":             "active=acme\n",
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

func TestVerifyMatchesBash(t *testing.T) {
	two := "github.com\n  ✓ Logged in to github.com account bob (keyring)\n  - Active account: false\n  ✓ Logged in to github.com account alice (keyring)\n  - Active account: true\n"
	cases := []struct {
		name    string
		args    []string
		files   map[string]string
		env     []string
		code    int
		vacuous string
	}{
		{name: "all ok, active profile", files: vFiles(vConf, nil), vacuous: "Profile verification passed."},
		{name: "all ok, named", args: []string{"acme"}, files: vFiles(vConf, map[string]string{".ctx/config": "-"}), vacuous: "ssh -T git@github-acme: OK"},
		{name: "named wins over active", args: []string{"acme", "extra"}, files: vFiles(vConf, map[string]string{".ctx/config": "active=ghost\n"}), vacuous: "Profile verification passed."},
		{name: "gh mismatch", files: vFiles(vConf, nil), env: []string{"GH_OUT=github.com\\n  ✓ Logged in to github.com account bob (keyring)\\n  - Active account: true\\n"}, code: 1, vacuous: "gh active: 'bob', profile expects: 'alice'"},
		{name: "gh multi-account, active is second", files: vFiles(vConf, nil), env: []string{"GH_OUT=" + strings.ReplaceAll(two, "\n", "\\n")}, vacuous: "gh active user matches: alice"},
		{name: "gh older format, first is active", files: vFiles(vConf, nil), env: []string{"GH_OUT=github.com\\n  ✓ Logged in to github.com as alice (/x/hosts.yml)\\n  ✓ Logged in to github.com as bob (/x/hosts.yml)\\n"}, vacuous: "gh active user matches: alice"},
		{name: "gh logged out", files: vFiles(vConf, nil), env: []string{"GH_OUT=You are not logged into any GitHub hosts.\\n", "GH_EXIT=1"}, code: 1, vacuous: "gh active: '', profile expects"},
		{name: "ssh denied", files: vFiles(vConf, nil), env: []string{"SSH_OUT=git@github.com: Permission denied (publickey).\\nfatal: no\\n"}, code: 1, vacuous: "failed or unexpected: git@github.com: Permission denied (publickey). fatal: no"},
		{name: "ssh silent", files: vFiles(vConf, nil), env: []string{"SSH_OUT="}, code: 1, vacuous: "failed or unexpected: "},
		{name: "ssh answer in capitals", files: vFiles(vConf, nil), env: []string{"SSH_OUT=HI! YOU'VE SUCCESSFULLY AUTHENTICATED\\n"}, vacuous: "ssh -T git@github-acme: OK"},
		{name: "key missing", files: vFiles(vConf, map[string]string{".ssh/id_acme": "-"}), code: 1, vacuous: "SSH private key missing:"},
		{name: "host missing from ssh config", files: vFiles(vConf, map[string]string{".ssh/ctx_config": "Host github-acme2\n"}), code: 1, vacuous: "SSH config missing Host github-acme in"},
		{name: "no ssh config file", files: vFiles(vConf, map[string]string{".ssh/ctx_config": "-"}), code: 1, vacuous: "SSH config missing Host"},
		{name: "work dir missing", files: vFiles(vConf, map[string]string{"work/.keep": "-"}), code: 1, vacuous: "Work dir missing:"},
		{name: "no SSH_KEY_PATH", files: vFiles("GIT_EMAIL=a@b.co\nGITHUB_USER=alice\nWORK_DIR=$HOME/work\n", nil), code: 1, vacuous: "No SSH_KEY_PATH in profile"},
		{name: "bad email", files: vFiles(strings.Replace(vConf, "a@b.co", "alice", 1), nil), code: 1, vacuous: "GIT_EMAIL looks invalid: alice"},
		{name: "no github user skips gh", files: vFiles(strings.Replace(vConf, "GITHUB_USER=alice\n", "", 1), nil), env: []string{"GH_OUT=boom\\n"}, vacuous: "Profile verification passed."},
		{name: "empty profile", files: vFiles("PROFILE_NAME=acme\n", nil), code: 1, vacuous: "Work dir missing: "},
		{name: "profile missing", args: []string{"ghost"}, files: vFiles(vConf, nil), code: 1, vacuous: "Profile 'ghost' not found."},
		{name: "no profile, none active", files: vFiles(vConf, map[string]string{".ctx/config": "-"}), code: 1, vacuous: "Usage: ctx verify <profile>"},
		{name: "active profile gone", files: vFiles(vConf, map[string]string{".ctx/config": "active=ghost\n"}), code: 1, vacuous: "Profile 'ghost' not found."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newCVEnv(t, "ssh", "gh")
			for _, kv := range c.env {
				k, v, _ := strings.Cut(kv, "=")
				e.p.Setenv(k, v)
			}
			e.seed(t, c.files)
			start := time.Now()
			want := e.bash(t, "", append([]string{"verify"}, c.args...)...)
			e.seed(t, c.files)
			got := e.goRun(t, "", nil, func(tm tui.Term) int { return Verify(tm, c.args) })
			e.compare(t, want, got)
			if time.Since(start) > 25*time.Second {
				t.Errorf("verify waited for ssh: %v", time.Since(start))
			}
			if want.code != c.code {
				t.Errorf("bash exit %d, want %d\n%s%s", want.code, c.code, want.stdout, want.stderr)
			}
			if !strings.Contains(want.stdout+want.stderr, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
		})
	}
}

// TestVerifySSHArgv pins the ssh command line: it never prompts and gives up.
func TestVerifySSHArgv(t *testing.T) {
	e := newCVEnv(t, "ssh", "gh")
	e.seed(t, vFiles(vConf, nil))
	got := e.goRun(t, "", nil, func(tm tui.Term) int { return Verify(tm, []string{"acme"}) })
	want := [][]string{{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "-T", "git@github-acme"}}
	if !reflect.DeepEqual(got.argv["ssh"], want) {
		t.Errorf("ssh argv %q, want %q", got.argv["ssh"], want)
	}
	if want := [][]string{{"auth", "status"}}; !reflect.DeepEqual(got.argv["gh"], want) {
		t.Errorf("gh argv %q, want %q", got.argv["gh"], want)
	}
}
