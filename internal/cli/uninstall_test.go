package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// Uninstall deletes files, so these tests run it only inside a sandbox: one
// t.TempDir() root holding HOME, CTX_DIR, the global git config and a fake
// install (prefix/bin/ctx + prefix/lib/ctx, as install.sh lays them out).
// PATH is the fake install, fakes and SystemPath (never the real ctx).
// mustSandbox checks all of that before every run and fails the test
// otherwise. Both implementations run the fake install's bin/ctx: Bash with
// CTX_CORE=off, Go through the real forwarder (CTX_CORE_BIN is a wrapper that
// logs and execs the test build of ctx-core), so CTX_BIN_PATH and CTX_LIB come
// from bin/ctx as in a real install.

const keychainFake = `case "$1" in
dump-keychain) cat "$KEYCHAIN" 2>/dev/null ;;
delete-generic-password)
  svc=""; while [ $# -gt 0 ]; do [ "$1" = -s ] && svc="$2"; shift; done
  grep -v "\"$svc\"" "$KEYCHAIN" > "$KEYCHAIN.tmp"; mv "$KEYCHAIN.tmp" "$KEYCHAIN" ;;
esac`

const coreWrapper = "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CTX_TEST_CORELOG\"\nexec \"$CTX_TEST_CORE\" \"$@\"\n"

type unRun struct {
	stdout, stderr string
	code           int
	tree           map[string]string
	argv           [][]string
}

type unEnv struct {
	p                          *ptest
	root, home, ctxDir, prefix string
	binTmpl                    string // bin/ctx for prefix, hard-linked into place by seed
	secDir, secLog             string
	coreDir, coreLog           string
}

func newUnEnv(t *testing.T) *unEnv {
	t.Helper()
	core := buildCore(t)
	p := newPtest(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &unEnv{p: p, root: root, home: filepath.Join(root, "home"), prefix: filepath.Join(root, "prefix")}
	e.ctxDir = filepath.Join(e.home, ".ctx")
	e.secDir, e.secLog = testutil.FakeBin(t, "security", keychainFake)
	e.coreDir = t.TempDir()
	e.coreLog = filepath.Join(e.coreDir, "core.log")
	testutil.Script(t, filepath.Join(e.coreDir, "ctx-core"), coreWrapper)
	e.binTmpl = filepath.Join(t.TempDir(), "ctx")
	writeInstalledCtx(t, e.binTmpl, filepath.Join(e.prefix, "lib", "ctx"))
	p.Setenv("HOME", e.home)
	p.Setenv("CTX_DIR", e.ctxDir)
	p.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(e.home, ".gitconfig"))
	p.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	p.Setenv("CTX_SECRET_PROVIDER", "file")
	p.Setenv("CTX_ACTIVE_PROFILE", "")
	p.Setenv("CTX_NO_GUM", "1")
	p.Setenv("USER", "tester")
	p.Setenv("KEYCHAIN", filepath.Join(root, "keychain.txt"))
	p.Setenv("CTX_TEST_CORE", core)
	p.Setenv("CTX_TEST_CORELOG", e.coreLog)
	p.Setenv("PASSWORD_STORE_DIR", filepath.Join(root, "pass"))
	p.Setenv("PATH", e.pathWith(filepath.Join(e.prefix, "bin")))
	return e
}

// pathWith is first (when not empty), the fakes and SystemPath.
func (e *unEnv) pathWith(first string) string {
	parts := []string{e.secDir, testutil.SystemPath(e.p.t)}
	if first != "" {
		parts = append([]string{first}, parts...)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// writeInstalledCtx writes the repo's bin/ctx with line 9 rewritten the way
// install.sh does (CTX_LIB="<lib>").
func writeInstalledCtx(t *testing.T, dst, lib string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, "bin", "ctx"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")
	if !strings.HasPrefix(lines[8], "CTX_LIB=") {
		t.Fatalf("bin/ctx line 9 is not the CTX_LIB line: %q", lines[8])
	}
	lines[8] = `CTX_LIB="` + lib + `"`
	if err := os.WriteFile(dst, []byte(strings.Join(lines, "\n")), 0o755); err != nil {
		t.Fatal(err)
	}
}

// copyLib copies the repo's lib scripts into dir.
func copyLib(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, pat := range []string{"*.sh", "*.bash", "*.fish"} {
		ms, _ := filepath.Glob(filepath.Join(repoRoot, "lib", pat))
		for _, m := range ms {
			b, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(m)), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// seed resets the sandbox: the fake install (bin/ctx, a ctx-core stub next to
// it, lib/ctx), HOME with CTX_DIR, profiles acme and beta with file-store
// secrets, then tweak.
func (e *unEnv) seed(t *testing.T, tweak func(e *unEnv)) {
	t.Helper()
	_ = filepath.WalkDir(e.root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	ents, _ := os.ReadDir(e.root)
	for _, ent := range ents {
		if err := os.RemoveAll(filepath.Join(e.root, ent.Name())); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(e.prefix, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(e.binTmpl, filepath.Join(bin, "ctx")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ctx-core"), []byte("stub\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyLib(t, filepath.Join(e.prefix, "lib", "ctx"))
	for _, d := range []string{"profiles", "backups", "secrets/acme", "secrets/beta"} {
		if err := os.MkdirAll(filepath.Join(e.ctxDir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"config":               "secret_provider=file\n",
		"profiles/acme.conf":   "PROFILE_NAME=acme\nSECRET_KEYS=\"API_KEY DB_PASS\"\n",
		"profiles/beta.conf":   "PROFILE_NAME=beta\n",
		"secrets/acme/API_KEY": "k1",
		"secrets/acme/DB_PASS": "k2",
		"secrets/beta/TOKEN":   "k3",
	}
	for rel, c := range files {
		if err := os.WriteFile(filepath.Join(e.ctxDir, rel), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.home, ".gitconfig"), []byte("[user]\n\tname = Me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if tweak != nil {
		tweak(e)
	}
	_ = os.Remove(e.secLog)
	_ = os.Remove(e.coreLog)
	t.Cleanup(func() {
		_ = filepath.WalkDir(e.root, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
}

func under(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && filepath.IsAbs(p)
}

// mustSandbox fails the test unless every path the run could delete is
// inside the sandbox (CTX_DIR may name / or HOME only when the answer is No,
// so a missing guard stops at the prompt).
func (e *unEnv) mustSandbox(t *testing.T, env []string, bin, stdin string) {
	t.Helper()
	vals := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		vals[k] = v
	}
	for _, k := range []string{"HOME", "GIT_CONFIG_GLOBAL", "KEYCHAIN", "PASSWORD_STORE_DIR"} {
		if !under(e.root, vals[k]) {
			t.Fatalf("sandbox: %s=%q is outside %s", k, vals[k], e.root)
		}
	}
	if !under(e.root, vals["CTX_DIR"]) && stdin != "n\n" {
		t.Fatalf("sandbox: CTX_DIR=%q is outside the sandbox and the answer is not No", vals["CTX_DIR"])
	}
	if !under(e.root, bin) {
		t.Fatalf("sandbox: running %s", bin)
	}
	sys := testutil.SystemPath(t)
	security := ""
	for _, d := range filepath.SplitList(vals["PATH"]) {
		if d != sys && d != e.secDir && !under(e.root, d) {
			t.Fatalf("sandbox: PATH entry %q", d)
		}
		for _, name := range []string{"ctx", "ctx-core"} {
			if p := filepath.Join(d, name); exists(p) && !under(e.root, p) {
				t.Fatalf("sandbox: %s on PATH is outside the sandbox", p)
			}
		}
		if p := filepath.Join(d, "security"); security == "" && exists(p) {
			security = p
		}
	}
	// The Keychain is reached only through the first security on PATH.
	if security != filepath.Join(e.secDir, "security") {
		t.Fatalf("sandbox: security on PATH is %q, not the fake", security)
	}
}

// run runs bin (default the fake install's bin/ctx) with the global flags and
// `uninstall args`; goImpl selects the forwarded Go command.
func (e *unEnv) run(t *testing.T, goImpl bool, bin, stdin string, dry bool, extraEnv []string, args ...string) unRun {
	t.Helper()
	if bin == "" {
		bin = filepath.Join(e.prefix, "bin", "ctx")
	}
	pre := []string{"--no-gum"}
	if dry {
		pre = append(pre, "--dry-run")
	}
	impl := []string{"CTX_CORE=off", "CTX_CORE_BIN="}
	if goImpl {
		impl = []string{"CTX_CORE=", "CTX_CORE_BIN=" + filepath.Join(e.coreDir, "ctx-core")}
	}
	env := e.p.Environ(append(impl, extraEnv...)...)
	e.mustSandbox(t, env, bin, stdin)
	cmd := exec.Command(bin, append(append(pre, "uninstall"), args...)...)
	cmd.Env = env
	cmd.Dir = e.root
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
	if goImpl {
		log, _ := os.ReadFile(e.coreLog)
		if !strings.Contains("\n"+string(log), "\nuninstall") {
			t.Fatalf("ctx-core did not run uninstall (log %q); stderr %s", log, eb.String())
		}
	}
	return unRun{o.String(), eb.String(), code, testutil.Snapshot(t, e.root), testutil.ReadArgv(t, e.secLog)}
}

type unCase struct {
	name     string
	tweak    func(e *unEnv)
	bin      func(e *unEnv) string // the ctx to run; default the fake install's
	path     func(e *unEnv) string // PATH; default the fake install first
	env      func(e *unEnv) []string
	stdin    string
	dry      bool
	args     []string
	wantCode int
	vacuous  string
	changes  bool // a failing or dry run that is expected to change files
	check    func(t *testing.T, e *unEnv, r unRun)
}

func runUnCases(t *testing.T, cases []unCase) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newUnEnv(t)
			var bin string
			var extra []string
			prep := func() {
				e.seed(t, c.tweak)
				bin = ""
				if c.bin != nil {
					bin = c.bin(e)
				}
				extra = nil
				if c.path != nil {
					extra = append(extra, "PATH="+c.path(e))
				}
				if c.env != nil {
					extra = append(extra, c.env(e)...)
				}
			}
			prep()
			before := testutil.Snapshot(t, e.root)
			want := e.run(t, false, bin, c.stdin, c.dry, extra, c.args...)
			prep()
			got := e.run(t, true, bin, c.stdin, c.dry, extra, c.args...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n argv go %q bash %q\n tree diff: %s",
					got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, got.argv, want.argv, testutil.DiffSnapshots(want.tree, got.tree))
			}
			if want.code != c.wantCode {
				t.Errorf("bash exit %d, want %d\n%s%s", want.code, c.wantCode, want.stdout, want.stderr)
			}
			if !strings.Contains(want.stdout+want.stderr, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
			if c.check != nil {
				c.check(t, e, got)
			}
			if (c.wantCode != 0 || c.dry) && !c.changes {
				if d := testutil.DiffSnapshots(before, got.tree); d != "" {
					t.Errorf("files changed:\n%s", d)
				}
			}
		})
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestUninstallMatchesBash(t *testing.T) {
	removed := func(purge bool) func(t *testing.T, e *unEnv, r unRun) {
		return func(t *testing.T, e *unEnv, r unRun) {
			for _, p := range []string{"bin/ctx", "bin/ctx-core", "lib/ctx"} {
				if exists(filepath.Join(e.prefix, p)) {
					t.Errorf("%s left behind", p)
				}
			}
			if !exists(filepath.Join(e.prefix, "lib")) || !exists(e.home) {
				t.Error("removed more than the install")
			}
			if exists(e.ctxDir) == purge {
				t.Errorf("CTX_DIR exists=%v with purge=%v", exists(e.ctxDir), purge)
			}
		}
	}
	guardOn := func(e *unEnv) {
		hooks := filepath.Join(e.ctxDir, "git-hooks")
		_ = os.MkdirAll(hooks, 0o755)
		_ = os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\n"), 0o755)
		cmd := exec.Command("git", "config", "--global", "core.hooksPath", hooks)
		cmd.Env = e.p.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			panic(string(out))
		}
	}
	keychain := func(e *unEnv) {
		_ = os.WriteFile(filepath.Join(e.root, "keychain.txt"), []byte(
			"    \"svce\"<blob>=\"ctx-acme-API_KEY\"\n    \"svce\"<blob>=\"ctx-beta-TOKEN\"\n    \"svce\"<blob>=\"other\"\n"), 0o600)
	}
	runUnCases(t, []unCase{
		{name: "plain", stdin: "y\n", vacuous: "# BEGIN ctx profile autoswitch", check: removed(false)},
		{name: "purge file store", stdin: "y\n", args: []string{"--purge"}, vacuous: "Deleted 2 secret(s) of acme from the file store", check: removed(true)},
		{name: "purge keychain", stdin: "y\n", args: []string{"--purge"}, tweak: keychain, env: func(*unEnv) []string { return []string{"CTX_SECRET_PROVIDER=keychain"} },
			vacuous: "Removed ", check: func(t *testing.T, e *unEnv, r unRun) {
				removed(true)(t, e, r)
				if runtime.GOOS != "darwin" {
					return
				}
				kc, _ := os.ReadFile(filepath.Join(e.root, "keychain.txt"))
				if string(kc) != "    \"svce\"<blob>=\"other\"\n" {
					t.Errorf("keychain left %q", kc)
				}
				if !strings.Contains(r.stdout, "Deleted 1 secret(s) of beta from the Keychain") {
					t.Errorf("stdout %q", r.stdout)
				}
			}},
		{name: "guard installed", stdin: "y\n", tweak: guardOn, vacuous: "ctx guard off", check: func(t *testing.T, e *unEnv, r unRun) {
			removed(false)(t, e, r)
			if exists(filepath.Join(e.ctxDir, "git-hooks")) {
				t.Error("guard hooks left")
			}
			gc, _ := os.ReadFile(filepath.Join(e.home, ".gitconfig"))
			if strings.Contains(string(gc), "hooksPath") {
				t.Errorf("hooksPath left: %s", gc)
			}
		}},
		{name: "dry run", dry: true, vacuous: "[dry-run] would remove "},
		{name: "dry run purge guard", dry: true, args: []string{"--purge"}, tweak: guardOn, vacuous: "[dry-run] would run ctx guard uninstall"},
		{name: "answer no", stdin: "n\n", wantCode: 1, vacuous: "Aborted."},
		{name: "default is no", stdin: "\n", wantCode: 1, vacuous: "Aborted."},
		{name: "eof", wantCode: 130, vacuous: "Cancelled by user."},
		{name: "bad argument", args: []string{"--force"}, wantCode: 1, vacuous: "Usage: ctx uninstall [--purge]"},
		{name: "purge then bad argument", args: []string{"--purge", "x"}, wantCode: 1, vacuous: "Usage: ctx uninstall [--purge]"},
		{name: "ctx not on PATH", stdin: "y\n", path: func(e *unEnv) string { return e.pathWith("") }, wantCode: 1, vacuous: "ctx not found in PATH"},
		{name: "no ctx-core next to ctx", stdin: "y\n", tweak: func(e *unEnv) { _ = os.Remove(filepath.Join(e.prefix, "bin", "ctx-core")) }, vacuous: "Removed ", check: removed(false)},
		{name: "purge without CTX_DIR", stdin: "y\n", args: []string{"--purge"}, tweak: func(e *unEnv) { _ = os.RemoveAll(e.ctxDir) }, vacuous: "Removed ", check: removed(true)},
		{name: "explicit data dir", stdin: "y\n", args: []string{"--purge"}, env: func(e *unEnv) []string { return []string{"CTX_DIR=" + filepath.Join(e.root, "data")} },
			tweak: func(e *unEnv) { _ = os.MkdirAll(filepath.Join(e.root, "data", "profiles"), 0o700) }, vacuous: "Removed ",
			check: func(t *testing.T, e *unEnv, r unRun) {
				if exists(filepath.Join(e.root, "data")) || !exists(e.ctxDir) {
					t.Error("purged the wrong data dir")
				}
			}},
		{name: "cannot remove ctx", stdin: "y\n", changes: true, tweak: func(e *unEnv) { _ = os.Chmod(filepath.Join(e.prefix, "bin"), 0o555) }, wantCode: 1, vacuous: "Could not remove ",
			check: func(t *testing.T, e *unEnv, r unRun) {
				if !exists(filepath.Join(e.prefix, "bin", "ctx")) || exists(filepath.Join(e.prefix, "lib", "ctx")) {
					t.Error("want ctx kept and the lib dir removed")
				}
			}},
	})
}

// TestUninstallSafety is Review Focus 1: unusual installs are refused before
// the prompt with nothing deleted. Every case answers No, so a guard that
// failed to refuse stops at the prompt rather than deleting.
func TestUninstallSafety(t *testing.T) {
	lineTo := func(lib string, withScripts bool) func(e *unEnv) {
		return func(e *unEnv) {
			if withScripts {
				copyLib(e.p.t, lib)
			}
			bin := filepath.Join(e.prefix, "bin", "ctx")
			_ = os.Remove(bin) // a hard link to the template: replace, never edit
			writeInstalledCtx(e.p.t, bin, lib)
		}
	}
	cases := []unCase{
		{name: "symlinked ctx on PATH", tweak: func(e *unEnv) {
			_ = os.MkdirAll(filepath.Join(e.root, "links"), 0o755)
			_ = os.Symlink(filepath.Join(e.prefix, "bin", "ctx"), filepath.Join(e.root, "links", "ctx"))
		}, bin: func(e *unEnv) string { return filepath.Join(e.root, "links", "ctx") },
			path: func(e *unEnv) string { return e.pathWith(filepath.Join(e.root, "links")) }, vacuous: "is a symlink (to "},
		{name: "dev checkout on PATH", tweak: func(e *unEnv) {
			co := filepath.Join(e.root, "checkout")
			_ = os.MkdirAll(filepath.Join(co, "bin"), 0o755)
			_ = os.MkdirAll(filepath.Join(co, ".git"), 0o755)
			b, _ := os.ReadFile(filepath.Join(repoRoot, "bin", "ctx"))
			_ = os.WriteFile(filepath.Join(co, "bin", "ctx"), b, 0o755)
			copyLib(e.p.t, filepath.Join(co, "lib"))
		}, bin: func(e *unEnv) string { return filepath.Join(e.root, "checkout", "bin", "ctx") },
			path: func(e *unEnv) string { return e.pathWith(filepath.Join(e.root, "checkout", "bin")) }, vacuous: "has no installer CTX_LIB line"},
		{name: "running ctx is not the one on PATH", tweak: func(e *unEnv) {
			other := filepath.Join(e.root, "other")
			_ = os.MkdirAll(filepath.Join(other, "bin"), 0o755)
			writeInstalledCtx(e.p.t, filepath.Join(other, "bin", "ctx"), filepath.Join(other, "lib", "ctx"))
			copyLib(e.p.t, filepath.Join(other, "lib", "ctx"))
		}, bin: func(e *unEnv) string { return filepath.Join(e.root, "other", "bin", "ctx") }, vacuous: "is not the one running"},
		{name: "CTX_LIB is HOME", tweak: func(e *unEnv) { lineTo(e.home, true)(e) }, vacuous: "it is your home directory or holds it"},
		{name: "CTX_LIB not named ctx", tweak: func(e *unEnv) { lineTo(filepath.Join(e.prefix, "lib", "notctx"), true)(e) }, vacuous: "its name is not 'ctx'"},
		{name: "CTX_LIB is CTX_DIR's parent", tweak: func(e *unEnv) { lineTo(filepath.Join(e.root, "ctx"), true)(e) },
			env: func(e *unEnv) []string { return []string{"CTX_DIR=" + filepath.Join(e.root, "ctx", ".ctx")} }, vacuous: "it is the parent of CTX_DIR"},
		{name: "CTX_LIB holds CTX_DIR", env: func(e *unEnv) []string {
			return []string{"CTX_DIR=" + filepath.Join(e.prefix, "lib", "ctx", "a", ".ctx")}
		}, vacuous: "it holds CTX_DIR"},
		{name: "CTX_LIB is a git checkout", tweak: func(e *unEnv) { _ = os.MkdirAll(filepath.Join(e.prefix, "lib", "ctx", ".git"), 0o755) }, vacuous: "it is a git checkout"},
		{name: "CTX_LIB is a symlink", tweak: func(e *unEnv) {
			_ = os.MkdirAll(filepath.Join(e.root, "ln"), 0o755)
			_ = os.Symlink(filepath.Join(e.prefix, "lib", "ctx"), filepath.Join(e.root, "ln", "ctx"))
			lineTo(filepath.Join(e.root, "ln", "ctx"), false)(e)
		}, vacuous: "it is a symlink"},
		{name: "CTX_LIB missing", tweak: func(e *unEnv) {
			lineTo(filepath.Join(e.root, "gone", "ctx"), false)(e)
			copyLib(e.p.t, filepath.Join(e.prefix, "bin")) // bin/ctx falls back to its own dir
		}, vacuous: "but this ctx runs from"},
		{name: "purge CTX_DIR is /", args: []string{"--purge"}, env: func(*unEnv) []string { return []string{"CTX_DIR=/"} }, vacuous: "Refusing to remove /: it is /."},
		{name: "purge CTX_DIR is HOME", args: []string{"--purge"}, env: func(e *unEnv) []string { return []string{"CTX_DIR=" + e.home} }, vacuous: "it is your home directory or holds it"},
		{name: "purge CTX_DIR holds HOME", args: []string{"--purge"}, env: func(e *unEnv) []string { return []string{"CTX_DIR=" + e.root} }, vacuous: "it is your home directory or holds it"},
		{name: "purge CTX_DIR odd name", args: []string{"--purge"}, tweak: func(e *unEnv) { _ = os.MkdirAll(filepath.Join(e.root, "docs", "x"), 0o755) },
			env: func(e *unEnv) []string { return []string{"CTX_DIR=" + filepath.Join(e.root, "docs")} }, vacuous: "does not look like a ctx data directory"},
		{name: "purge CTX_DIR symlink", args: []string{"--purge"}, tweak: func(e *unEnv) { _ = os.Symlink(e.ctxDir, filepath.Join(e.home, "link.ctx")) },
			env: func(e *unEnv) []string { return []string{"CTX_DIR=" + filepath.Join(e.home, "link.ctx")} }, vacuous: "it is a symlink"},
		{name: "purge CTX_DIR relative", args: []string{"--purge"}, tweak: func(e *unEnv) { _ = os.MkdirAll(filepath.Join(e.root, "rel", ".ctx"), 0o755) },
			env: func(*unEnv) []string { return []string{"CTX_DIR=rel/.ctx"} }, vacuous: "it is not an absolute path"},
	}
	for i := range cases {
		cases[i].stdin = "n\n"
		cases[i].wantCode = 1
		v := cases[i].vacuous
		cases[i].check = func(t *testing.T, e *unEnv, r unRun) {
			if !strings.Contains(r.stderr, "Refusing") || !strings.Contains(r.stderr, v) {
				t.Errorf("want a refusal with %q, stderr %q", v, r.stderr)
			}
			if strings.Contains(r.stderr, "Continue?") {
				t.Errorf("prompted before refusing: %q", r.stderr)
			}
		}
	}
	runUnCases(t, cases)
}

// TestUninstallRefusalChangesNothing: a refusal comes before the prompt, so
// even a Yes typed ahead changes nothing.
func TestUninstallRefusalChangesNothing(t *testing.T) {
	t.Run("checkout", func(t *testing.T) {
		e := newUnEnv(t)
		e.seed(t, func(e *unEnv) {
			co := filepath.Join(e.root, "checkout", "bin")
			_ = os.MkdirAll(co, 0o755)
			b, _ := os.ReadFile(filepath.Join(repoRoot, "bin", "ctx"))
			_ = os.WriteFile(filepath.Join(co, "ctx"), b, 0o755)
			copyLib(e.p.t, filepath.Join(e.root, "checkout", "lib"))
		})
		before := testutil.Snapshot(t, e.root)
		r := e.run(t, true, filepath.Join(e.root, "checkout", "bin", "ctx"), "y\n", false,
			[]string{"PATH=" + e.pathWith(filepath.Join(e.root, "checkout", "bin"))}, "--purge")
		if r.code != 1 {
			t.Errorf("exit %d", r.code)
		}
		if d := testutil.DiffSnapshots(before, r.tree); d != "" {
			t.Errorf("a refusal changed files even when answering yes:\n%s", d)
		}
	})
}

// TestUninstallCtrlC: Ctrl+C at the prompt cancels with 130 and changes nothing.
func TestUninstallCtrlC(t *testing.T) {
	e := newUnEnv(t)
	e.seed(t, nil)
	before := testutil.Snapshot(t, e.root)
	bin := filepath.Join(e.prefix, "bin", "ctx")
	env := e.p.Environ("CTX_CORE=", "CTX_CORE_BIN="+filepath.Join(e.coreDir, "ctx-core"))
	e.mustSandbox(t, env, bin, "n\n")
	cmd := exec.Command(bin, "--no-gum", "uninstall", "--purge")
	cmd.Env = env
	out, code := testutil.RunPTY(t, cmd, []string{testutil.CtrlC}, 30*time.Second)
	if code != 130 || strings.Count(out, "Cancelled by user.") != 1 {
		t.Errorf("exit %d, want 130 with one cancel line:\n%s", code, out)
	}
	if d := testutil.DiffSnapshots(before, testutil.Snapshot(t, e.root)); d != "" {
		t.Errorf("Ctrl+C changed files:\n%s", d)
	}
	if log, _ := os.ReadFile(e.coreLog); !strings.Contains(string(log), "\nuninstall") && !strings.HasPrefix(string(log), "uninstall") {
		t.Errorf("ctx-core did not run uninstall: %q", log)
	}
}
