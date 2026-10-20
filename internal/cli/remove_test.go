package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

const removeGitconfig = `[user]
	name = Me
[core]
	editor = vi

[includeIf "gitdir:~/w/acme/"]
  path = %HOME%/.config/git/ctx-acme

[includeIf "gitdir:~/w/acme-old/"]
  path = %HOME%/.config/git/ctx-acme-old
[alias]
	co = checkout
`

const removeSSH = `
Host github-acme
  HostName github.com
  User git
  IdentityFile ~/.ssh/id_acme
  IdentitiesOnly yes

Host github-acme-old
  HostName github.com
  User git
  IdentityFile ~/.ssh/id_acme_old
  IdentitiesOnly yes

Host other
  HostName example.com
`

type removeRun struct {
	stdout, stderr string
	code           int
	tree           map[string]string
	argv           [][]string
}

// removeEnv holds one fixture HOME plus fakes that are built once per test.
type removeEnv struct {
	f       *fixture
	p       *ptest
	passLog string
	cwd     string
}

// newRemoveEnv marks t parallel (see ptest).
func newRemoveEnv(t *testing.T) *removeEnv {
	t.Helper()
	p := newPtest(t)
	e := &removeEnv{f: newPFixture(p), p: p}
	dir, log := testutil.FakeBin(t, "pass", `case "$1" in rm) : ;; esac`)
	e.passLog = log
	p.Setenv("PATH", dir+string(os.PathListSeparator)+p.Getenv("PATH"))
	p.Setenv("CTX_NO_GUM", "1")
	p.Setenv("CTX_SECRET_PROVIDER", "file")
	p.Setenv("CTX_ACTIVE_PROFILE", "")
	return e
}

// seed resets HOME to a ctx setup with profiles acme (secrets API_KEY DB_PASS) and acme-old.
func (e *removeEnv) seed(t *testing.T, config string, tweak func(f *fixture)) {
	t.Helper()
	f := e.f
	if err := os.RemoveAll(f.home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(e.passLog)
	for _, n := range []string{"acme", "acme-old"} {
		f.write("profiles/"+n+".conf", "PROFILE_NAME="+n+"\nGIT_NAME=\"Dev "+n+"\"\nGIT_EMAIL=\""+n+"@x.com\"\nWORK_DIR=\""+f.home+"/w/"+n+"\"\nGITHUB_USER=\"gh-"+n+"\"\nSECRET_KEYS=\"API_KEY DB_PASS\"\n")
		f.write("secrets/"+n+"/API_KEY", "k-"+n)
		f.write("secrets/"+n+"/DB_PASS", "p-"+n+"\n")
		f.writeHome(".config/git/ctx-"+n, "[user]\n\tname = "+n+"\n")
	}
	f.write("config", "secret_provider=file\n"+config)
	f.writeHome(".ssh/ctx_config", removeSSH)
	f.writeHome(".gitconfig", strings.ReplaceAll(removeGitconfig, "%HOME%", f.home))
	if tweak != nil {
		tweak(f)
	}
}

func (e *removeEnv) collect(t *testing.T, out, errb string, code int) removeRun {
	return removeRun{out, errb, code, testutil.Snapshot(t, e.f.home), testutil.ReadArgv(t, e.passLog)}
}

func (e *removeEnv) bash(t *testing.T, stdin string, args ...string) removeRun {
	cmd := exec.Command(bashBin(t), append([]string{"--no-gum", "remove"}, args...)...)
	cmd.Env = e.p.Environ("CTX_CORE=off")
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

func (e *removeEnv) goRun(t *testing.T, stdin string, args ...string) removeRun {
	var o, eb bytes.Buffer
	var code int
	e.p.inProc("", func() { code = Remove(tui.Term{In: strings.NewReader(stdin), Out: &o, Err: &eb}, args) })
	return e.collect(t, o.String(), eb.String(), code)
}

func TestRemoveMatchesBash(t *testing.T) {
	cases := []struct {
		name     string
		stdin    string
		args     []string
		config   string
		provider string
		tweak    func(f *fixture)
		vacuous  string // substring Bash must print
	}{
		{name: "yes", stdin: "y\n", args: []string{"acme"}, vacuous: "Profile 'acme' removed."},
		{name: "yes upper", stdin: "Y\n", args: []string{"acme"}, vacuous: "Profile 'acme' removed."},
		{name: "extra args ignored", stdin: "y\n", args: []string{"acme", "acme-old"}, vacuous: "Profile 'acme' removed."},
		{name: "prefix profile", stdin: "y\n", args: []string{"acme-old"}, vacuous: "Profile 'acme-old' removed."},
		{name: "active profile", stdin: "y\n", args: []string{"acme"}, config: "active=acme\nactive_source=manual\nmanual_pwd=L3g=\nkeep=1\n", vacuous: "Active profile was removed"},
		{name: "other profile active", stdin: "y\n", args: []string{"acme"}, config: "active=acme-old\n", vacuous: "Profile 'acme' removed."},
		{name: "missing profile", args: []string{"ghost"}, vacuous: "not found"},
		{name: "no args", vacuous: "Usage: ctx remove <profile>"},
		{name: "empty arg", args: []string{""}, vacuous: "Usage: ctx remove <profile>"},
		{name: "pass provider", stdin: "y\n", args: []string{"acme"}, provider: "pass", vacuous: "Profile secrets removed"},
		{name: "no secret keys", stdin: "y\n", args: []string{"acme"}, tweak: func(f *fixture) {
			f.write("profiles/acme.conf", "PROFILE_NAME=acme\nGIT_NAME=\"A\"\nGIT_EMAIL=\"a@x\"\n")
		}, vacuous: "SSH host removed"},
		{name: "no ssh file or gitconfig or identity", stdin: "y\n", args: []string{"acme"}, tweak: func(f *fixture) {
			for _, p := range []string{".ssh/ctx_config", ".gitconfig", ".config/git/ctx-acme"} {
				_ = os.Remove(filepath.Join(f.home, p))
			}
		}, vacuous: "Profile 'acme' removed."},
		{name: "ssh block last without trailing newline", stdin: "y\n", args: []string{"acme"}, tweak: func(f *fixture) {
			f.writeHome(".ssh/ctx_config", "Host github-acme\n  HostName github.com\n  User git\n  IdentityFile ~/k\n  IdentitiesOnly yes")
		}, vacuous: "Profile 'acme' removed."},
		{name: "profile without work dir", stdin: "y\n", args: []string{"acme"}, tweak: func(f *fixture) {
			f.write("profiles/acme.conf", "PROFILE_NAME=acme\nGIT_NAME=\"A \\\\n B\"\nGIT_EMAIL=\"a@x\"\nSECRET_KEYS=\"K\"\n")
		}, vacuous: "Dir    : —"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRemoveEnv(t)
			if c.provider != "" {
				e.p.Setenv("CTX_SECRET_PROVIDER", c.provider)
			}
			e.seed(t, c.config, c.tweak)
			want := e.bash(t, c.stdin, c.args...)
			e.seed(t, c.config, c.tweak)
			got := e.goRun(t, c.stdin, c.args...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n argv go %q bash %q\n tree diff: %s",
					got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, got.argv, want.argv, testutil.DiffSnapshots(want.tree, got.tree))
			}
			if c.provider == "pass" && len(want.argv) != 2 {
				t.Errorf("vacuous: bash made %d pass calls, want 2: %q", len(want.argv), want.argv)
			}
			if !strings.Contains(want.stdout+want.stderr, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
		})
	}
}

// TestRemoveExactInclude checks what is removed from ~/.gitconfig and ctx_config.
func TestRemoveExactInclude(t *testing.T) {
	for _, impl := range []string{"go", "bash"} {
		t.Run(impl, func(t *testing.T) {
			e := newRemoveEnv(t)
			e.seed(t, "", nil)
			var r removeRun
			if impl == "go" {
				r = e.goRun(t, "y\n", "acme")
			} else {
				r = e.bash(t, "y\n", "acme")
			}
			if r.code != 0 {
				t.Fatalf("code %d: %s", r.code, r.stderr)
			}
			gc, _ := os.ReadFile(filepath.Join(e.f.home, ".gitconfig"))
			wantGC := strings.ReplaceAll(`[user]
	name = Me
[core]
	editor = vi

[includeIf "gitdir:~/w/acme-old/"]
  path = %HOME%/.config/git/ctx-acme-old
[alias]
	co = checkout
`, "%HOME%", e.f.home)
			if string(gc) != wantGC {
				t.Errorf("gitconfig:\n%s\nwant:\n%s", gc, wantGC)
			}
			ssh, _ := os.ReadFile(filepath.Join(e.f.home, ".ssh/ctx_config"))
			// The Host line and five lines after it go; the next block keeps its content.
			wantSSH := "\nHost github-acme-old\n  HostName github.com\n  User git\n  IdentityFile ~/.ssh/id_acme_old\n  IdentitiesOnly yes\n\nHost other\n  HostName example.com\n"
			if string(ssh) != wantSSH {
				t.Errorf("ssh:\n%q\nwant:\n%q", ssh, wantSSH)
			}
			for _, p := range []string{"profiles/acme.conf", "secrets/acme", ".config/git/ctx-acme"} {
				if _, err := os.Lstat(filepath.Join(e.f.home, ".ctx", p)); err == nil && !strings.HasPrefix(p, ".config") {
					t.Errorf("%s still exists", p)
				}
			}
			if _, err := os.Stat(filepath.Join(e.f.home, ".config/git/ctx-acme")); err == nil {
				t.Error("identity file still exists")
			}
			for _, p := range []string{".ctx/profiles/acme-old.conf", ".ctx/secrets/acme-old/API_KEY", ".config/git/ctx-acme-old"} {
				if _, err := os.Stat(filepath.Join(e.f.home, p)); err != nil {
					t.Errorf("acme-old lost %s", p)
				}
			}
		})
	}
}

func TestRemoveDeclined(t *testing.T) {
	for _, c := range []struct {
		name, stdin string
		code        int
		stderr      string
	}{
		{"n", "n\n", 1, "Aborted."},
		{"default is no", "\n", 1, "Aborted."},
		{"other text", "yes\n", 1, "Aborted."},
		{"eof", "", 130, "Cancelled by user."},
		{"eof mid line", "y", 130, "Cancelled by user."},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newRemoveEnv(t)
			e.seed(t, "active=acme\n", nil)
			before := testutil.Snapshot(t, e.f.home)
			want := e.bash(t, c.stdin, "acme")
			e.seed(t, "active=acme\n", nil)
			got := e.goRun(t, c.stdin, "acme")
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n go:   %+v\n bash: %+v", got, want)
			}
			if got.code != c.code || !strings.Contains(got.stderr, c.stderr) {
				t.Errorf("code %d stderr %q", got.code, got.stderr)
			}
			if d := testutil.DiffSnapshots(before, got.tree); d != "" {
				t.Errorf("declined run changed HOME: %s", d)
			}
		})
	}
}

func TestRemoveHelpers(t *testing.T) {
	f := newFixture(t)
	// Missing files are not errors and are not created.
	if err := sshRemoveHost("github-x"); err != nil {
		t.Fatal(err)
	}
	if err := gitconfigRemoveInclude(f.home+"/.config/git/ctx-x", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sshConfigPath()); err == nil {
		t.Error("ssh config created")
	}
	// An exact path line not under an includeIf header is left alone.
	f.writeHome(".gitconfig", "[include]\n  path = "+f.home+"/.config/git/ctx-x\n")
	if err := gitconfigRemoveInclude(f.home+"/.config/git/ctx-x", nil); err != nil {
		t.Fatal(err)
	}
	gc, _ := os.ReadFile(f.home + "/.gitconfig")
	if !strings.Contains(string(gc), "ctx-x") {
		t.Errorf("unrelated section touched: %q", gc)
	}
	// ssh file keeps mode 0600.
	f.writeHome(".ssh/ctx_config", "Host a\n1\n2\n3\n4\n")
	_ = os.Chmod(sshConfigPath(), 0o644)
	if err := sshRemoveHost("a"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(sshConfigPath()); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
}

// TestRemoveGitconfigLinkAndNoMatch covers ~/.gitconfig edge cases, Go vs Bash:
// a symlinked file is written through (link kept, target mode kept), and a
// file with nothing to drop is left untouched (no rewrite, no backup).
func TestRemoveGitconfigLinkAndNoMatch(t *testing.T) {
	linked := func(f *fixture) {
		body, err := os.ReadFile(filepath.Join(f.home, ".gitconfig"))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(f.home, "dotfiles", "gitconfig")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(target, 0o644)
		_ = os.Remove(filepath.Join(f.home, ".gitconfig"))
		if err := os.Symlink(target, filepath.Join(f.home, ".gitconfig")); err != nil {
			t.Fatal(err)
		}
	}
	noMatch := func(f *fixture) {
		f.writeHome(".gitconfig", "[user]\n\tname = Me\n")
		_ = os.Chmod(filepath.Join(f.home, ".gitconfig"), 0o644)
	}
	for _, c := range []struct {
		name  string
		tweak func(f *fixture)
		check func(t *testing.T, home string, r removeRun)
	}{
		{"symlink with match", linked, func(t *testing.T, home string, r removeRun) {
			if fi, err := os.Lstat(filepath.Join(home, ".gitconfig")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf(".gitconfig is no longer a symlink: %v %v", fi, err)
			}
			st, err := os.Stat(filepath.Join(home, "dotfiles", "gitconfig"))
			if err != nil || st.Mode().Perm() != 0o644 {
				t.Errorf("target mode: %v %v", st, err)
			}
			b, _ := os.ReadFile(filepath.Join(home, "dotfiles", "gitconfig"))
			if strings.Contains(string(b), "ctx-acme\n") || !strings.Contains(string(b), "ctx-acme-old") {
				t.Errorf("target content wrong:\n%s", b)
			}
		}},
		{"no match", noMatch, func(t *testing.T, home string, r removeRun) {
			p := filepath.Join(home, ".gitconfig")
			b, _ := os.ReadFile(p)
			st, _ := os.Stat(p)
			if string(b) != "[user]\n\tname = Me\n" || st.Mode().Perm() != 0o644 {
				t.Errorf("untouched file changed: %q %v", b, st.Mode())
			}
			for k := range r.tree {
				if strings.HasSuffix(k, "/.gitconfig") && strings.HasPrefix(k, ".ctx/backups") {
					t.Errorf("unexpected backup %s", k)
				}
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newRemoveEnv(t)
			e.seed(t, "", c.tweak)
			want := e.bash(t, "y\n", "acme")
			c.check(t, e.f.home, want)
			e.seed(t, "", c.tweak)
			got := e.goRun(t, "y\n", "acme")
			c.check(t, e.f.home, got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n stdout go: %q bash: %q\n stderr go: %q bash: %q\n tree diff: %s",
					got.stdout, want.stdout, got.stderr, want.stderr, testutil.DiffSnapshots(want.tree, got.tree))
			}
		})
	}
}
