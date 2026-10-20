package cli

import (
	"encoding/base64"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
	"github.com/charmbracelet/colorprofile"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*.golden")

var fakeTools = []string{"gh", "ssh-add", "mise", "aws", "git"}

var (
	sharedOnce sync.Once
	sharedRoot string // holds fake.sh and the real-tool symlink farm
)

func TestMain(m *testing.M) {
	// The differential subtests mostly wait, on child processes and, in the
	// pty suites, on RunPTY's quiet gaps: the package keeps well under one
	// CPU busy at the default -parallel (GOMAXPROCS). Run more at once unless
	// -parallel was given.
	flag.Parse()
	parallelSet := false
	flag.Visit(func(f *flag.Flag) { parallelSet = parallelSet || f.Name == "test.parallel" })
	if !parallelSet {
		_ = flag.Set("test.parallel", strconv.Itoa(4*runtime.GOMAXPROCS(0)))
	}
	startCoreBuild()
	code := m.Run()
	coreOnce.Do(func() {}) // wait for a build still running before removing it
	for _, d := range []string{sharedRoot, coreDir} {
		if d != "" {
			_ = os.RemoveAll(d)
		}
	}
	os.Exit(code)
}

// shared builds, once per test binary, the fake script every fake tool
// symlinks to (a fresh script is slow on its first exec, so it is written a
// single time) and a farm of the few real tools Bash needs.
func shared(t *testing.T) (script, farm string) {
	t.Helper()
	sharedOnce.Do(func() {
		root, err := os.MkdirTemp("", "ctx-fakes")
		if err != nil {
			t.Fatal(err)
		}
		sharedRoot = root
		body := "#!/bin/sh\n" +
			"n=${0##*/}\n" +
			"for a in \"$@\"; do printf '%s\\000' \"$a\" >> \"$FAKE_LOGDIR/$n.argv\"; done\n" +
			"printf '\\n' >> \"$FAKE_LOGDIR/$n.argv\"\n" +
			"case $n in\n" +
			"gh) eval \"$FAKE_BODY_gh\" ;;\n" +
			"ssh-add) eval \"$FAKE_BODY_ssh_add\" ;;\n" +
			"mise) eval \"$FAKE_BODY_mise\" ;;\n" +
			"aws) eval \"$FAKE_BODY_aws\" ;;\n" +
			"git) eval \"$FAKE_BODY_git\" ;;\n" +
			"esac\n"
		if err := os.WriteFile(filepath.Join(root, "fake.sh"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "farm"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, tool := range []string{"bash", "env", "grep", "cut", "tr", "awk", "head", "wc", "basename", "date", "mkdir", "chmod", "cat", "sed", "dirname", "touch", "rm", "uname", "sort", "id", "base64", "tail", "mktemp", "mv", "cp", "ln", "tee", "sleep", "stat", "ls", "pwd", "readlink", "tput", "xargs", "find", "diff", "ps", "hostname", "whoami"} {
			if p, err := lookReal(tool); err == nil {
				_ = os.Symlink(p, filepath.Join(root, "farm", tool))
			}
		}
	})
	return filepath.Join(sharedRoot, "fake.sh"), filepath.Join(sharedRoot, "farm")
}

// statusEnv puts the given fake tools (name -> shell body) ahead of a PATH of
// just the real tools Bash needs, so absent tools are really absent. It
// returns setLog, which points the fakes at a fresh argv log dir and returns it.
func statusEnv(t *testing.T, fakes map[string]string) (setLog func() string) {
	t.Helper()
	return statusEnvOn(t, t.Setenv, fakes)
}

// statusEnvOn is statusEnv with setenv in place of t.Setenv (a ptest's).
func statusEnvOn(t *testing.T, setenv func(k, v string), fakes map[string]string) (setLog func() string) {
	t.Helper()
	script, farm := shared(t)
	dir := t.TempDir()
	for name := range fakes {
		if err := os.Symlink(script, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range fakeTools {
		setenv("FAKE_BODY_"+strings.ReplaceAll(name, "-", "_"), fakes[name])
	}
	setenv("PATH", dir+string(os.PathListSeparator)+farm)
	return func() string {
		d := t.TempDir()
		setenv("FAKE_LOGDIR", d)
		return d
	}
}

// argvs returns every fake's recorded invocations.
func argvs(t *testing.T, dir string) map[string][][]string {
	t.Helper()
	m := map[string][][]string{}
	for _, name := range fakeTools {
		if a := testutil.ReadArgv(t, filepath.Join(dir, name+".argv")); a != nil {
			m[name] = a
		}
	}
	return m
}

func lookReal(name string) (string, error) {
	for _, d := range []string{"/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin"} {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func TestStatusTextMatchesBash(t *testing.T) {
	good := map[string]string{
		"gh":      `echo "  ✓ Logged in to github.com account gooduser (keyring)"; echo "  - Active account: true"`,
		"ssh-add": `echo "256 SHA256:abc $HOME/.ssh/id_good (ED25519)"`,
		"mise":    `exit 0`,
		"aws":     `exit 0`,
		"git":     `echo "a@x.io"`,
	}
	type tc struct {
		name  string
		fakes map[string]string
		setup func(f *fixture) (cwd string)
	}
	prof := func(f *fixture, kv ...string) {
		f.profile("acme", append([]string{"GIT_NAME", "A", "GIT_EMAIL", "a@x.io"}, kv...)...)
	}
	full := func(f *fixture, extra ...string) string {
		wd := filepath.Join(f.home, "work")
		_ = os.MkdirAll(wd, 0o755)
		key := filepath.Join(f.home, ".ssh", "id_good")
		_ = os.MkdirAll(filepath.Dir(key), 0o755)
		_ = os.WriteFile(key, []byte("k"), 0o600)
		_ = os.WriteFile(filepath.Join(wd, "mise.toml"), nil, 0o644)
		prof(f, append([]string{"WORK_DIR", wd, "GITHUB_USER", "gooduser", "SSH_KEY_PATH", key,
			"AWS_PROFILE_NAME", "awsp", "AZURE_SUBSCRIPTION", "sub", "GCP_PROJECT", "gp",
			"KUBE_CONTEXT", "kc", "SECRET_KEYS", "K1 K2"}, extra...)...)
		f.write("config", "active=acme\n")
		return wd
	}
	with := func(over map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range good {
			m[k] = v
		}
		for k, v := range over {
			if v == "" {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		return m
	}
	cases := []tc{
		{"no active", good, func(f *fixture) string { return f.home }},
		{"auto in workdir", good, func(f *fixture) string { return full(f) }},
		{"manual locked", good, func(f *fixture) string {
			wd := full(f)
			f.write("config", "active=acme\nactive_source=manual\nmanual_pwd="+base64.StdEncoding.EncodeToString([]byte(wd))+"\n")
			return wd
		}},
		{"manual elsewhere", good, func(f *fixture) string {
			full(f)
			f.write("config", "active=acme\nactive_source=manual\nmanual_pwd="+base64.StdEncoding.EncodeToString([]byte("/nowhere"))+"\n")
			return f.home
		}},
		{"gh real format", with(map[string]string{"gh": `echo "  ✓ Logged in to github.com account gooduser (keyring)"`}), func(f *fixture) string { return full(f) }},
		{"gh old paren format", with(map[string]string{"gh": `echo "Logged in to github.com account (gooduser)"`}), func(f *fixture) string { return full(f) }},
		{"gh mismatch", with(map[string]string{"gh": `echo "Logged in to github.com account other (keyring)"`}), func(f *fixture) string { return full(f) }},
		{"key not in agent", with(map[string]string{"ssh-add": `echo "256 SHA256:abc /other (ED25519)"`}), func(f *fixture) string { return full(f) }},
		{"key file missing", good, func(f *fixture) string {
			wd := full(f)
			_ = os.Remove(filepath.Join(f.home, ".ssh", "id_good"))
			return wd
		}},
		{"mise.toml absent", good, func(f *fixture) string {
			wd := full(f)
			_ = os.Remove(filepath.Join(wd, "mise.toml"))
			return wd
		}},
		{"aws invalid", with(map[string]string{"aws": `exit 255`}), func(f *fixture) string { return full(f) }},
		{"git email mismatch", with(map[string]string{"git": `echo "other@x.io"`}), func(f *fixture) string { return full(f) }},
		{"git no email", with(map[string]string{"git": `exit 1`}), func(f *fixture) string { return full(f) }},
		{"outside workdir", good, func(f *fixture) string { full(f); return f.home }},
		{"sibling prefix dir", good, func(f *fixture) string {
			wd := full(f)
			sib := wd + "-two"
			_ = os.MkdirAll(sib, 0o755)
			return sib
		}},
		{"no tools", map[string]string{}, func(f *fixture) string { return full(f) }},
		{"active env var", good, func(f *fixture) string {
			f.p.Setenv("CTX_ACTIVE_PROFILE", "acme")
			return full(f)
		}},
		{"minimal profile", good, func(f *fixture) string {
			prof(f)
			f.write("config", "active=acme\n")
			return f.home
		}},
		{"missing profile file", good, func(f *fixture) string {
			f.write("config", "active=ghost\n")
			return f.home
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newPtest(t)
			f := newPFixture(p)
			p.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
			cwd := c.setup(f)
			setLog := statusEnvOn(t, p.Setenv, c.fakes)
			bashLog := setLog()
			want, wantCode := f.bashText(cwd, "status")
			wantArgv := argvs(t, bashLog)
			p.Setenv("PWD", cwd)
			goLog := setLog()
			var got, stderr string
			var code int
			p.inProc(cwd, func() { got, stderr, code = goText(StatusText) })
			if gotArgv := argvs(t, goLog); !reflect.DeepEqual(gotArgv, wantArgv) {
				t.Errorf("external argv differ\n go:   %q\nbash: %q", gotArgv, wantArgv)
			}
			if c.name == "auto in workdir" {
				for tool, args := range map[string][]string{
					"gh": {"auth", "status"}, "ssh-add": {"-l"},
					"aws": {"configure", "list", "--profile", "awsp"}, "git": {"config", "user.email"},
				} {
					if g := wantArgv[tool]; !reflect.DeepEqual(g, [][]string{args}) {
						t.Errorf("%s argv = %q, want %q", tool, g, args)
					}
				}
				if _, ran := wantArgv["mise"]; ran {
					t.Errorf("mise must only be looked up, not run: %q", wantArgv["mise"])
				}
			}
			if got != want || code != wantCode {
				t.Errorf("code %d want %d\n got: %q\nwant: %q", code, wantCode, got, want)
			}
			if c.name == "missing profile file" && stderr != "\u2717 Profile 'ghost' not found. Run: ctx list\n" {
				t.Errorf("stderr = %q", stderr)
			}
		})
	}
}

func TestStatusTextRichGolden(t *testing.T) {
	path := filepath.Join(repoRoot, "internal", "cli", "testdata", "status_rich.golden")
	f := newFixture(t)
	f.profile("acme", "GIT_NAME", "A", "GIT_EMAIL", "a@x.io", "WORK_DIR", "/w/acme", "GITHUB_USER", "gooduser",
		"SSH_KEY_PATH", "/k/id_good", "AWS_PROFILE_NAME", "awsp", "SECRET_KEYS", "K1 K2")
	f.write("config", "active=acme\n")
	statusEnv(t, map[string]string{})()
	t.Chdir(f.home)
	t.Setenv("PWD", f.home)
	var out, errb strings.Builder
	var raw strings.Builder
	w := &colorprofile.Writer{Forward: &raw, Profile: colorprofile.ANSI256}
	_ = w
	code := StatusText(tui.Term{Out: &out, Err: &errb, Rich: true, Colors: true})
	if code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if _, err := w.WriteString(out.String()); err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(raw.String(), f.home, "$HOME")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestStatusTextRichNoColor(t *testing.T) {
	f := newFixture(t)
	f.profile("acme", "GIT_NAME", "A", "GIT_EMAIL", "a@x.io", "SSH_KEY_PATH", "/k/id_good")
	f.write("config", "active=acme\n")
	statusEnv(t, map[string]string{})()
	t.Chdir(f.home)
	t.Setenv("PWD", f.home)
	t.Setenv("NO_COLOR", "1")
	for _, fn := range []func(tui.Term) int{StatusText, ListText} {
		out, err := os.Create(filepath.Join(t.TempDir(), "out"))
		if err != nil {
			t.Fatal(err)
		}
		if code := fn(tui.Term{Out: out, Err: out, Rich: true, Colors: true}); code != 0 {
			t.Fatalf("code %d", code)
		}
		out.Close()
		got, _ := os.ReadFile(out.Name())
		if strings.Contains(string(got), "\033") || !strings.Contains(string(got), "acme") {
			t.Errorf("rich output with NO_COLOR: %q", got)
		}
	}
}
