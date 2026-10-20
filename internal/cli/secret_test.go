package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

func TestSecretListColors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CTX_DIR", dir)
	t.Setenv("CTX_SECRET_PROVIDER", "file")
	for p, body := range map[string]string{"profiles/p.conf": "", "secrets/p/B": "1", "secrets/p/A": "2"} {
		path := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	term := tui.Term{Out: &out, Err: &errb, Colors: true}
	if code := SecretIn(term, []string{"list", "p"}); code != 0 {
		t.Fatalf("list code %d", code)
	}
	// What lib/cmd_ops.sh prints on a terminal: bold header, green bullets.
	want := "\033[1m\n  Secrets for: p\n\033[0m\n" +
		"  \033[0;32m\u2022\033[0m A\n  \033[0;32m\u2022\033[0m B\n\n"
	if out.String() != want {
		t.Errorf("colored list = %q, want %q", out.String(), want)
	}
	code := SecretIn(term, []string{"get", "p", "NOPE"})
	if code != 1 || errb.String() != "\033[0;31m\u2717\033[0m No secret 'NOPE' for profile 'p'\n" {
		t.Errorf("colored die = %d %q", code, errb.String())
	}
	out.Reset()
	if code := SecretIn(term, []string{"delete", "p", "A"}); code != 0 || out.String() != "\033[0;32m\u2713\033[0m Deleted A from file store\n" {
		t.Errorf("colored delete = %q, %d", out.String(), code)
	}
}

func TestEchoE(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:      "plain\n",
		`a\tb\\c`:    "a\tb\\c\n",
		`x\0101y`:    "xAy\n",
		`\x41\u00e9`: "A\u00e9\n",
		`stop\cnot`:  "stop",
		`\q\x`:       "\\q\\x\n",
		`end\`:       "end\\\n",
	} {
		if got := echoE(in); got != want {
			t.Errorf("echoE(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"-n": "", "-e": "\n", "-neE": "", "-x": "-x\n", "-": "-\n", "ok": "ok\n"} {
		if got := echoPlain(in); got != want {
			t.Errorf("echoPlain(%q) = %q, want %q", in, got, want)
		}
	}
}

// secretHarness runs `ctx secret ...` through Bash (--no-gum) and through Go
// against identical fixtures: file store plus a fake `pass`.
type secretHarness struct {
	t               *testing.T
	p               *ptest
	f               *fixture
	state, store    string
	passLog         string
	passFail        bool
	failKey         string
	noProfile       bool
	seed            map[string]string // "profile/KEY" -> file store content
	passSeed        map[string]string // "KEY" -> pass content (profile p)
	provider        string
	withoutFakePass bool
}

type secretResult struct {
	stdout, stderr string
	code           int
	state          string
}

func (h *secretHarness) setup() {
	t := h.t
	_ = os.RemoveAll(h.f.dir)
	_ = os.RemoveAll(h.state)
	_ = os.RemoveAll(h.store)
	_ = os.Remove(h.passLog)
	for _, d := range []string{h.state, h.store, h.f.dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if !h.noProfile {
		h.f.write("profiles/p.conf", "")
	}
	for k, v := range h.seed {
		h.f.write("secrets/"+k, v)
	}
	for k, v := range h.passSeed {
		for _, p := range []string{filepath.Join(h.state, "pass/ctx/p", k), filepath.Join(h.store, "ctx/p", k+".gpg")} {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (h *secretHarness) snapshot() string {
	var b strings.Builder
	for _, root := range []string{h.f.dir, h.state, h.store} {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || p == root {
				return nil
			}
			rel := strings.TrimPrefix(p, filepath.Dir(root))
			fmt.Fprintf(&b, "%s %v", rel, info.Mode())
			if info.Mode().IsRegular() {
				data, _ := os.ReadFile(p)
				fmt.Fprintf(&b, " %q", data)
			}
			b.WriteString("\n")
			return nil
		})
	}
	fmt.Fprintf(&b, "pass argv: %q\n", testutil.ReadArgv(h.t, h.passLog))
	return b.String()
}

// newSecretHarness marks t parallel (see ptest).
func newSecretHarness(t *testing.T) *secretHarness {
	t.Helper()
	p := newPtest(t)
	h := &secretHarness{t: t, p: p, f: newPFixture(p), provider: "file"}
	h.state, h.store = t.TempDir(), t.TempDir()
	return h
}

// install puts the fake pass on PATH (unless the case wants it absent).
func (h *secretHarness) install() {
	t := h.t
	if h.withoutFakePass {
		if _, err := lookPath("pass"); err == nil {
			t.Skip("a real pass is installed")
		}
	} else {
		dir, log := testutil.FakeBin(t, "pass", `
[ -z "$FAKE_PASS_FAIL" ] || exit 1
[ -z "$FAKE_PASS_FAIL_KEY" ] || [ "$1" != insert ] || [ "$4" != "ctx/p/$FAKE_PASS_FAIL_KEY" ] || exit 1
case "$1" in
insert) mkdir -p "$FAKE_STATE/pass/$(dirname "$4")" "$PASSWORD_STORE_DIR/$(dirname "$4")"; cat > "$FAKE_STATE/pass/$4"; : > "$PASSWORD_STORE_DIR/$4.gpg" ;;
show) f="$FAKE_STATE/pass/$2"; [ -f "$f" ] || exit 1; cat "$f" ;;
rm) rm -f "$FAKE_STATE/pass/$3" "$PASSWORD_STORE_DIR/$3.gpg" ;;
esac`)
		h.passLog = log
		h.p.Setenv("PATH", dir+string(os.PathListSeparator)+h.p.Getenv("PATH"))
	}
	h.p.Setenv("FAKE_STATE", h.state)
	h.p.Setenv("PASSWORD_STORE_DIR", h.store)
	h.p.Setenv("USER", "tester")
	h.p.Setenv("CTX_SECRET_PROVIDER", h.provider)
	h.p.Setenv("CTX_NO_GUM", "1")
	h.p.Setenv("FAKE_PASS_FAIL_KEY", h.failKey)
	if h.passFail {
		h.p.Setenv("FAKE_PASS_FAIL", "1")
	} else {
		h.p.Setenv("FAKE_PASS_FAIL", "")
	}
}

func (h *secretHarness) bash(stdin string, args []string) secretResult {
	h.setup()
	cmd := exec.Command(bashBin(h.t), append([]string{"--no-gum"}, args...)...)
	cmd.Env = h.p.Environ("CTX_CORE=off")
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			h.t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return secretResult{o.String(), e.String(), code, h.snapshot()}
}

func (h *secretHarness) goRun(stdin string, args []string) secretResult {
	h.setup()
	var o, e bytes.Buffer
	term := tui.Term{In: strings.NewReader(stdin), Out: &o, Err: &e}
	var code int
	h.p.inProc("", func() { code = SecretIn(term, args[1:]) }) // args[0] is "secret"
	return secretResult{o.String(), e.String(), code, h.snapshot()}
}

func TestSecretSetMatchesBash(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		stdin    string
		provider string
		passFail bool
		noProf   bool
		noPass   bool
	}{
		{name: "valid", args: []string{"p", "K"}, stdin: "hunter2\n"},
		{name: "spaces trimmed", args: []string{"p", "K"}, stdin: "  a b \t\n"},
		{name: "backslash and dollar", args: []string{"p", "K"}, stdin: "a\\nb$HOME\\\n"},
		{name: "quotes", args: []string{"p", "K"}, stdin: "q\"'`x\n"},
		{name: "leading dash", args: []string{"p", "K"}, stdin: "-x\n"},
		{name: "echo option -n is swallowed", args: []string{"p", "K"}, stdin: "-n\n"},
		{name: "echo option -e is swallowed", args: []string{"p", "K"}, stdin: "-e\n"},
		{name: "empty value", args: []string{"p", "K"}, stdin: "\n"},
		{name: "missing profile", args: []string{"nope", "K"}, stdin: "v\n", noProf: true},
		{name: "missing key arg", args: []string{"p"}, stdin: "v\n"},
		{name: "missing args", args: nil, stdin: "v\n"},
		{name: "pass provider", args: []string{"p", "K"}, stdin: "v 1\n", provider: "pass"},
		{name: "pass failing", args: []string{"p", "K"}, stdin: "v\n", provider: "pass", passFail: true},
		{name: "pass absent", args: []string{"p", "K"}, stdin: "v\n", provider: "pass", noPass: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newSecretHarness(t)
			h.noProfile, h.passFail, h.withoutFakePass = c.noProf, c.passFail, c.noPass
			if c.provider != "" {
				h.provider = c.provider
			}
			h.install()
			args := append([]string{"secret", "set"}, c.args...)
			want := h.bash(c.stdin, args)
			got := h.goRun(c.stdin, args)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n go:   %+v\n bash: %+v", got, want)
			}
			if c.name == "valid" && !strings.Contains(want.state, `"hunter2"`) {
				t.Errorf("vacuous: bash stored nothing: %s", want.state)
			}
		})
	}
}

func TestSecretSetCancelled(t *testing.T) {
	h := newSecretHarness(t)
	h.install()
	h.setup()
	var o, e bytes.Buffer
	var code int
	h.p.inProc("", func() {
		code = SecretIn(tui.Term{In: strings.NewReader(""), Out: &o, Err: &e}, []string{"set", "p", "K"})
	})
	if code != 130 {
		t.Errorf("code = %d, want 130", code)
	}
	if !strings.HasSuffix(e.String(), "! Cancelled by user.\n") {
		t.Errorf("stderr = %q", e.String())
	}
	if o.Len() != 0 {
		t.Errorf("stdout = %q", o.String())
	}
	if _, err := os.Stat(filepath.Join(h.f.dir, "secrets/p/K")); !os.IsNotExist(err) {
		t.Errorf("secret stored on cancel: %v", err)
	}
	// Bash is not compared: its ask_secret runs in $(...) and signals $PPID (the
	// test process) with INT on cancel.
}

func TestSecretSetRich(t *testing.T) {
	h := newSecretHarness(t)
	h.install()
	h.setup()
	var o, e bytes.Buffer
	// Keys through an open pipe: a reader at EOF right after them can let
	// huh's EOF grace cancel the form first on a loaded machine.
	in, keys := io.Pipe()
	go func() { _, _ = io.WriteString(keys, "s3cret\r") }()
	t.Cleanup(func() { _ = keys.Close() })
	term := tui.Term{In: in, Out: &o, Err: &e, Rich: true}
	var code int
	h.p.inProc("", func() { code = SecretIn(term, []string{"set", "p", "K"}) })
	if code != 0 {
		t.Fatalf("code = %d, stderr %q", code, e.String())
	}
	got, err := os.ReadFile(filepath.Join(h.f.dir, "secrets/p/K"))
	if err != nil || string(got) != "s3cret" {
		t.Errorf("stored %q, %v", got, err)
	}
	if !strings.Contains(o.String(), "K stored (file store) for p") {
		t.Errorf("stdout = %q", o.String())
	}
}

func TestSecretMigrateMatchesBash(t *testing.T) {
	two := map[string]string{"p/A": "a1", "p/B": "b2\n"}
	cases := []struct {
		name     string
		args     []string
		stdin    string
		seed     map[string]string
		passSeed map[string]string
		provider string
		passFail bool
		failKey  string
		noProf   bool
		noPass   bool
		vacuous  string // substring the Bash run must print
	}{
		{name: "file to pass, keep source", args: []string{"p", "pass"}, stdin: "n\n", seed: two, vacuous: "Migrated 2"},
		{name: "file to pass, delete source", args: []string{"p", "pass"}, stdin: "y\n", seed: two, vacuous: "Removed source"},
		{name: "default answer keeps source", args: []string{"p", "pass"}, stdin: "\n", seed: two},
		{name: "uppercase providers", args: []string{"p", "PASS", "FILE"}, stdin: "n\n", seed: two, vacuous: "Migrated 2"},
		{name: "pass to file", args: []string{"p", "file", "pass"}, stdin: "y\n", passSeed: map[string]string{"A": "pa\nsecond", "B": "pb"}, vacuous: "Removed source"},
		{name: "pass to file, pass absent", args: []string{"p", "file", "pass"}, stdin: "n\n", noPass: true, vacuous: ""},
		{name: "file to pass, pass absent", args: []string{"p", "pass"}, stdin: "y\n", seed: two, noPass: true, vacuous: "Migrated 0"},
		{name: "one key fails to copy", args: []string{"p", "pass"}, stdin: "y\n", seed: two, failKey: "B", vacuous: "Migrated 1"},
		{name: "pass failing", args: []string{"p", "pass"}, stdin: "n\n", seed: two, passFail: true, vacuous: "Migrated 0"},
		{name: "same provider", args: []string{"p", "file", "file"}, stdin: "n\n", seed: two},
		{name: "same provider by default", args: []string{"p", "file"}, stdin: "n\n", seed: two},
		{name: "invalid to-provider", args: []string{"p", "vault"}, stdin: "n\n", seed: two},
		{name: "invalid from-provider", args: []string{"p", "file", "vault"}, stdin: "n\n", seed: two},
		{name: "no keys", args: []string{"p", "pass"}, stdin: "n\n"},
		{name: "empty key skipped", args: []string{"p", "pass"}, stdin: "n\n", seed: map[string]string{"p/A": "", "p/B": "b"}, vacuous: "1 key(s) were empty"},
		{name: "cancel at delete question", args: []string{"p", "pass"}, stdin: "", seed: two, vacuous: "Migrated 2"},
		{name: "answer yes with capital", args: []string{"p", "pass"}, stdin: "Y\n", seed: two, vacuous: "Removed source"},
		{name: "answer yes word is no", args: []string{"p", "pass"}, stdin: "yes\n", seed: two},
		{name: "missing profile", args: []string{"nope", "pass"}, stdin: "n\n", seed: two, noProf: true},
		{name: "missing args", args: nil, stdin: "n\n"},
		{name: "missing to-provider", args: []string{"p"}, stdin: "n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newSecretHarness(t)
			h.seed, h.passSeed = c.seed, c.passSeed
			h.noProfile, h.passFail, h.withoutFakePass = c.noProf, c.passFail, c.noPass
			h.failKey = c.failKey
			if c.provider != "" {
				h.provider = c.provider
			}
			h.install()
			args := append([]string{"secret", "migrate"}, c.args...)
			want := h.bash(c.stdin, args)
			got := h.goRun(c.stdin, args)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n go:   %+v\n bash: %+v", got, want)
			}
			switch c.name {
			case "file to pass, pass absent", "pass failing":
				if !strings.Contains(want.state, "secrets/p/A") || strings.Contains(want.stdout, "Delete migrated") || strings.Contains(want.stderr, "Delete migrated") {
					t.Errorf("source keys must survive without a prompt: %+v", want)
				}
			case "one key fails to copy":
				if strings.Contains(want.state, "secrets/p/A") || !strings.Contains(want.state, "secrets/p/B") {
					t.Errorf("only the copied key may be deleted: %s", want.state)
				}
			}
			if c.vacuous != "" && !strings.Contains(want.stdout, c.vacuous) {
				t.Errorf("vacuous: bash stdout %q lacks %q", want.stdout, c.vacuous)
			}
		})
	}
}

func TestSecretMigrateCancelledKeepsCopies(t *testing.T) {
	h := newSecretHarness(t)
	h.seed = map[string]string{"p/A": "a1"}
	h.install()
	h.setup()
	var o, e bytes.Buffer
	var code int
	h.p.inProc("", func() {
		code = SecretIn(tui.Term{In: strings.NewReader(""), Out: &o, Err: &e}, []string{"migrate", "p", "pass"})
	})
	if code != 130 || !strings.HasSuffix(e.String(), "! Cancelled by user.\n") {
		t.Errorf("code %d stderr %q", code, e.String())
	}
	if _, err := os.Stat(filepath.Join(h.state, "pass/ctx/p/A")); err != nil {
		t.Errorf("copy missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.f.dir, "secrets/p/A")); err != nil {
		t.Errorf("source deleted on cancel: %v", err)
	}
}
