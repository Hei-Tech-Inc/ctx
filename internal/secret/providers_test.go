package secret

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func TestEffectiveProvider(t *testing.T) {
	cases := []struct {
		name, env, config, goos, want string
	}{
		{"default darwin", "", "", "darwin", "keychain"},
		{"default linux", "", "", "linux", "file"},
		{"env wins over config", "pass", "secret_provider=file\n", "linux", "pass"},
		{"config used", "", "secret_provider=pass\n", "linux", "pass"},
		{"config last wins", "", "secret_provider=pass\nsecret_provider=file\n", "darwin", "file"},
		{"env case folded", "FILE", "", "darwin", "file"},
		{"config explicit keychain on linux", "", "secret_provider=keychain\n", "linux", "keychain"},
		{"invalid env is auto", "vault", "secret_provider=pass\n", "linux", "file"},
		{"invalid config is auto", "", "secret_provider=vault\n", "darwin", "keychain"},
		{"CRLF config is invalid", "", "secret_provider=file\r\n", "darwin", "keychain"},
		{"auto stays auto", "auto", "", "linux", "file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CTX_DIR", dir)
			t.Setenv("CTX_SECRET_PROVIDER", c.env)
			if c.config != "" {
				if err := os.WriteFile(filepath.Join(dir, "config"), []byte(c.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			old := goos
			goos = c.goos
			t.Cleanup(func() { goos = old })
			if got := EffectiveProvider(); got != c.want {
				t.Errorf("EffectiveProvider() = %q, want %q", got, c.want)
			}
			// Bash agrees.
			root, _ := filepath.Abs("../..")
			script := `source "$1/lib/core.sh"; uname() { echo ` + map[string]string{"darwin": "Darwin", "linux": "Linux"}[c.goos] + `; }; ctx_effective_secret_provider`
			out, err := exec.Command("bash", "-c", script, "_", root).Output()
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(out)) != c.want {
				t.Errorf("bash says %q, want %q", out, c.want)
			}
		})
	}
}

func TestOpenAvailability(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no security, no pass
	for _, p := range []string{ProviderKeychain, ProviderPass} {
		st, err := Open(p)
		if err != nil {
			t.Fatal(err)
		}
		if v, err := st.Get("p", "K"); v != "" || err != nil {
			t.Errorf("%s Get = %q, %v", p, v, err)
		}
		if l, _ := st.List("p"); len(l) != 0 {
			t.Errorf("%s List = %v", p, l)
		}
		if err := st.Delete("p", "K"); err == nil {
			t.Errorf("%s Delete succeeded without its tool", p)
		}
	}
	if _, err := Open("vault"); err == nil {
		t.Error("Open(vault) succeeded")
	}
	old := goos
	goos = "linux"
	defer func() { goos = old }()
	dir, _ := testutil.FakeBin(t, "security", "exit 0")
	t.Setenv("PATH", dir)
	st, _ := Open(ProviderKeychain)
	if _, ok := st.(unavailable); !ok {
		t.Errorf("keychain on linux = %T, want unavailable", st)
	}
	goos = "darwin"
	st, _ = Open(ProviderKeychain)
	if _, ok := st.(Keychain); !ok {
		t.Errorf("keychain on darwin = %T, want Keychain", st)
	}
}

// fakeEnv sets up fake security and pass on PATH, with state under a temp dir.
type fakeEnv struct {
	t                 *testing.T
	state, ctxDir     string
	securityLog, pLog string
	passStore         string
}

func newFakeEnv(t *testing.T) *fakeEnv {
	t.Helper()
	f := &fakeEnv{t: t, state: t.TempDir(), ctxDir: t.TempDir(), passStore: t.TempDir()}
	secDir, secLog := testutil.FakeBin(t, "security", `
case "$1" in
find-generic-password) f="$FAKE_STATE/kc/$5"; [ -f "$f" ] || exit 44; cat "$f" ;;
delete-generic-password) rm -f "$FAKE_STATE/kc/$5"; [ "$5" = ctx-p-A_KEY ] || exit 44 ;;
dump-keychain)
  echo 'keychain: "/x/login.keychain-db"'
  for f in "$FAKE_STATE"/kc/*; do [ -e "$f" ] || continue; echo "    \"svce\"<blob>=\"$(basename "$f")\""; done
  echo '    "acct"<blob>="someone"' ;;
esac`)
	passDir, passLog := testutil.FakeBin(t, "pass", `
case "$1" in
show) f="$FAKE_STATE/pass/$2"; [ -f "$f" ] || exit 1; cat "$f" ;;
rm) rm -f "$FAKE_STATE/pass/$3" ;;
insert) cat > "$FAKE_STATE/pass_stdin" ;;
esac`)
	f.securityLog, f.pLog = secLog, passLog
	t.Setenv("PATH", secDir+":"+passDir+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_STATE", f.state)
	t.Setenv("CTX_DIR", f.ctxDir)
	t.Setenv("PASSWORD_STORE_DIR", f.passStore)
	t.Setenv("USER", "tester")
	t.Setenv("HOME", f.ctxDir)
	return f
}

func (f *fakeEnv) write(path, body string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeEnv) seed() {
	for svc, body := range map[string]string{
		"ctx-p-A_KEY": "alpha\n", "ctx-p-b_key": "beta", "ctx-p-Z": "zed\n\n", "ctx-p-multi": "l1\nl2\n", "ctx-other-X": "x",
	} {
		f.write(filepath.Join(f.state, "kc", svc), body)
		f.write(filepath.Join(f.state, "pass", "ctx", strings.Replace(strings.TrimPrefix(svc, "ctx-"), "-", "/", 1)), body)
		prof, key, _ := strings.Cut(strings.TrimPrefix(svc, "ctx-"), "-")
		f.write(filepath.Join(f.ctxDir, "secrets", prof, key), body)
		f.write(filepath.Join(f.passStore, "ctx", prof, key+".gpg"), "x")
	}
	f.write(filepath.Join(f.passStore, "ctx", "p", "not-a-secret.txt"), "x")
	f.write(filepath.Join(f.ctxDir, "secrets", "p", ".hidden"), "x")
}

func (f *fakeEnv) bash(script string, args ...string) string {
	f.t.Helper()
	root, _ := filepath.Abs("../..")
	out, err := exec.Command("bash", append([]string{"-c", `source "$1/lib/core.sh"; shift; ` + script, "_", root}, args...)...).Output()
	if err != nil {
		f.t.Fatalf("bash %q: %v", script, err)
	}
	return string(out)
}

func (f *fakeEnv) resetLogs() {
	_ = os.Remove(f.securityLog)
	_ = os.Remove(f.pLog)
}

func (f *fakeEnv) argv() [][]string {
	return append(testutil.ReadArgv(f.t, f.securityLog), testutil.ReadArgv(f.t, f.pLog)...)
}

func TestProvidersMatchBash(t *testing.T) {
	for _, c := range []struct {
		name   string
		store  Store
		prefix string // Bash function prefix
	}{
		{"keychain", Keychain{}, "_secret_keychain"},
		{"file", File{}, "_secret_file"},
		{"pass", Pass{}, "_secret_pass"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEnv(t)
			f.seed()
			bashList := func(profile string) []string {
				out := strings.TrimRight(f.bash(c.prefix+`_list_keys "$1"`, profile), "\n")
				if out == "" {
					return nil
				}
				return strings.Split(out, "\n")
			}
			for _, k := range [][2]string{{"p", "A_KEY"}, {"p", "b_key"}, {"p", "Z"}, {"p", "multi"}, {"p", "missing"}, {"nope", "X"}} {
				f.resetLogs()
				want := f.bash(`v="$(`+c.prefix+`_get "$1" "$2")"; printf '%s' "$v"`, k[0], k[1])
				wantArgv := f.argv()
				f.resetLogs()
				got, err := c.store.Get(k[0], k[1])
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("Get(%v) = %q, bash %q", k, got, want)
				}
				if !reflect.DeepEqual(f.argv(), wantArgv) {
					t.Errorf("Get(%v) argv %v, bash %v", k, f.argv(), wantArgv)
				}
			}
			for _, prof := range []string{"p", "other", "nope"} {
				f.resetLogs()
				want := bashList(prof)
				wantArgv := f.argv()
				f.resetLogs()
				got, _ := c.store.List(prof)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("List(%s) = %q, bash %q", prof, got, want)
				}
				if !reflect.DeepEqual(f.argv(), wantArgv) {
					t.Errorf("List(%s) argv %v, bash %v", prof, f.argv(), wantArgv)
				}
			}
			if got, _ := c.store.List("p"); len(got) == 0 {
				t.Error("List(p) found nothing; fixture is broken")
			}
			// Delete: Go first, then compare to what Bash did against a fresh copy.
			for _, k := range [][2]string{{"p", "A_KEY"}, {"p", "missing"}} {
				f.resetLogs()
				f.bash(c.prefix+`_delete "$1" "$2"`, k[0], k[1])
				wantArgv, wantAfter := f.argv(), bashList("p")
				f.seed()
				f.resetLogs()
				if err := c.store.Delete(k[0], k[1]); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(f.argv(), wantArgv) {
					t.Errorf("Delete(%v) argv %v, bash %v", k, f.argv(), wantArgv)
				}
				if after, _ := c.store.List("p"); c.name != "pass" && !reflect.DeepEqual(after, wantAfter) {
					t.Errorf("after Delete(%v): %q, bash %q", k, after, wantAfter)
				}
				f.seed()
			}
		})
	}
}

func TestPassSecretsGetFirstLine(t *testing.T) {
	f := newFakeEnv(t)
	f.seed()
	if v, _ := (Pass{}).Get("p", "multi"); v != "l1" {
		t.Errorf("pass Get = %q, want first line", v)
	}
}

func TestFileDeleteRemovesFile(t *testing.T) {
	f := newFakeEnv(t)
	f.seed()
	p := filepath.Join(f.ctxDir, "secrets", "p", "Z")
	if err := (File{}).Delete("p", "Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still there: %v", err)
	}
}

func TestProviderGetter(t *testing.T) {
	f := newFakeEnv(t)
	f.seed()
	t.Setenv("CTX_SECRET_PROVIDER", "file")
	if v, err := (ProviderGetter{}).Get("p", "Z"); err != nil || v != "zed" {
		t.Errorf("Get = %q, %v", v, err)
	}
	_, err := ProviderGetter{}.Get("p", "missing")
	if err == nil || !strings.Contains(err.Error(), "No secret 'missing' for profile 'p'") {
		t.Errorf("missing err = %v", err)
	}
}

func TestStoreSetMatchesBash(t *testing.T) {
	for _, c := range []struct {
		name   string
		store  Store
		prefix string
	}{
		{"keychain", Keychain{}, "_secret_keychain"},
		{"file", File{}, "_secret_file"},
		{"pass", Pass{}, "_secret_pass"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEnv(t)
			snapshot := func() string {
				var b strings.Builder
				_ = filepath.Walk(f.ctxDir, func(p string, info os.FileInfo, err error) error {
					if err != nil || p == f.ctxDir {
						return nil
					}
					fmt.Fprintf(&b, "%s %v", strings.TrimPrefix(p, f.ctxDir), info.Mode())
					if info.Mode().IsRegular() {
						data, _ := os.ReadFile(p)
						fmt.Fprintf(&b, " %q", data)
					}
					b.WriteString("\n")
					return nil
				})
				stdin, _ := os.ReadFile(filepath.Join(f.state, "pass_stdin"))
				return b.String() + "stdin=" + string(stdin)
			}
			reset := func() {
				f.resetLogs()
				_ = os.RemoveAll(filepath.Join(f.ctxDir, "secrets"))
				_ = os.Remove(filepath.Join(f.state, "pass_stdin"))
			}
			for _, v := range []string{"hunter2", "a b", "-x", "$HOME", `q"'`, ""} {
				reset()
				f.bash(c.prefix+`_set "$1" "$2" "$3"`, "p", "K", v)
				wantArgv, wantState := f.argv(), snapshot()
				reset()
				if err := c.store.Set("p", "K", v); err != nil {
					t.Fatalf("Set(%q): %v", v, err)
				}
				if got := f.argv(); !reflect.DeepEqual(got, wantArgv) {
					t.Errorf("Set(%q) argv %q, bash %q", v, got, wantArgv)
				}
				if got := snapshot(); got != wantState {
					t.Errorf("Set(%q) state\n%s\nbash\n%s", v, got, wantState)
				}
			}
		})
	}
}

func TestOpenWith(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, p := range []string{ProviderKeychain, ProviderPass, "vault"} {
		if _, err := OpenWith(p); !errors.Is(err, ErrUnavailable) {
			t.Errorf("OpenWith(%s) err = %v, want ErrUnavailable", p, err)
		}
	}
	if st, err := OpenWith(ProviderFile); err != nil || st == nil {
		t.Errorf("OpenWith(file) = %v, %v", st, err)
	}
	dir, _ := testutil.FakeBin(t, "pass", "exit 0")
	t.Setenv("PATH", dir)
	if _, err := OpenWith(ProviderPass); err != nil {
		t.Errorf("OpenWith(pass) with pass: %v", err)
	}
	old := goos
	defer func() { goos = old }()
	sdir, _ := testutil.FakeBin(t, "security", "exit 0")
	t.Setenv("PATH", sdir)
	goos = "linux"
	if _, err := OpenWith(ProviderKeychain); !errors.Is(err, ErrUnavailable) {
		t.Errorf("keychain on linux err = %v", err)
	}
	goos = "darwin"
	if _, err := OpenWith(ProviderKeychain); err != nil {
		t.Errorf("keychain on darwin: %v", err)
	}
}
