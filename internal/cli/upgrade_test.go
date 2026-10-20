package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// upgrade downloads a script and runs it, so these tests never reach the
// network: install.sh and lib/core.sh come from an httptest server on
// 127.0.0.1 (mustLocal checks CTX_UPGRADE_REPO and PATH before every run);
// curl is a fake that logs its argv, refuses any URL but 127.0.0.1 and then
// runs the real curl without .curlrc or proxies; wget is a fake that fetches
// through the real curl the same way; the installer is a stub that records
// its env and stdin and exits CTX_TEST_INSTALL_EXIT. Both implementations run
// a fake install's bin/ctx (prefix/bin/ctx with install.sh's CTX_LIB line):
// Bash with CTX_CORE=off, Go through the real forwarder.

const upCurlFake = `for a in "$@"; do
  case "$a" in
    http://127.0.0.1:*) ;;
    *://*) echo "test curl: refusing $a" >&2; exit 99 ;;
  esac
done
exec "$CTX_TEST_REAL_CURL" -q --noproxy '*' "$@"`

const upWgetFake = `[ "$1" = -qO ] || { echo "test wget: unexpected $*" >&2; exit 98; }
case "$3" in http://127.0.0.1:*) ;; *) echo "test wget: refusing $3" >&2; exit 99 ;; esac
exec "$CTX_TEST_REAL_CURL" -q --noproxy '*' -fs -o "$2" "$3"`

// upInstaller is the stub install.sh (served with CRLF line endings and a
// lone \r, so it runs only once normalized the way perl does).
const upInstaller = `printf 'CTX_REPO=%s\nCTX_UPGRADE_ONLY=%s\nCTX_INSTALL_BIN=%s\nCTX_INSTALL_LIB=%s\nargs=%s\n' "$CTX_REPO" "${CTX_UPGRADE_ONLY-unset}" "${CTX_INSTALL_BIN-unset}" "${CTX_INSTALL_LIB-unset}" "$#" >"$CTX_TEST_INSTALL_LOG"
if read -r line; then echo "installer read: $line"; fi
echo "installer: on stdout"
echo "installer: on stderr" >&2
true` + "\r" + `echo "lone CR split"
exit "${CTX_TEST_INSTALL_EXIT:-0}"
`

// upInstallerNoLoneCR is the stub for runs without perl (tr -d '\r' would
// glue the lone-CR line together, and bash's error names the temp file).
const upInstallerNoLoneCR = `printf 'CTX_REPO=%s\nCTX_UPGRADE_ONLY=%s\n' "$CTX_REPO" "${CTX_UPGRADE_ONLY-unset}" >"$CTX_TEST_INSTALL_LOG"
echo "installer: no perl"
exit "${CTX_TEST_INSTALL_EXIT:-0}"
`

// curlMS is the timing in curl's connect errors ("after 3 ms").
var curlMS = regexp.MustCompile(`after \d+ ms`)

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

type upRun struct {
	stdout, stderr string
	code           int
	tree           map[string]string
	curl, wget     [][]string
	reqs           []string
}

type upEnv struct {
	p                              *ptest
	root, home, prefix, tmp, serve string
	srv                            *httptest.Server
	mu                             sync.Mutex
	reqs                           []string
	curlDir, curlLog               string
	wgetDir, wgetLog               string
	coreDir, coreLog               string
	sys, noFetch, noPerl, realCurl string
	repoURL                        string // CTX_UPGRADE_REPO (the server's, unless a case overrides it)
}

// upPaths: SystemPath has no curl or wget (testutil.ExternalTools), so it is
// the "no downloader" PATH; noPerl is SystemPath without perl too. Built once
// per top-level test.
type upPaths struct{ noFetch, noPerl string }

func newUpPaths(t *testing.T) upPaths {
	t.Helper()
	return upPaths{noFetch: testutil.SystemPath(t), noPerl: pathWithout(t, "perl", "curl", "wget")}
}

// pathWithout is a dir of symlinks to every command of SystemPath but drop.
func pathWithout(t *testing.T, drop ...string) string {
	t.Helper()
	sys := testutil.SystemPath(t)
	dir := t.TempDir()
	ents, err := os.ReadDir(sys)
	if err != nil {
		t.Fatal(err)
	}
	skip := map[string]bool{}
	for _, d := range drop {
		skip[d] = true
	}
	for _, e := range ents {
		if skip[e.Name()] {
			continue
		}
		if err := os.Symlink(filepath.Join(sys, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newUpEnv(t *testing.T, paths upPaths) *upEnv {
	t.Helper()
	_ = buildCore(t)
	p := newPtest(t)
	sys := testutil.SystemPath(t)
	realCurl := testutil.SystemTool("curl")
	if realCurl == "" {
		t.Skip("curl is not installed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &upEnv{p: p, root: root, home: filepath.Join(root, "home"), prefix: filepath.Join(root, "prefix"),
		tmp: filepath.Join(root, "tmp"), serve: t.TempDir(), sys: sys, noFetch: paths.noFetch, noPerl: paths.noPerl, realCurl: realCurl}
	e.curlDir, e.curlLog = testutil.FakeBin(t, "curl", upCurlFake)
	e.wgetDir, e.wgetLog = testutil.FakeBin(t, "wget", upWgetFake)
	e.coreDir = t.TempDir()
	e.coreLog = filepath.Join(e.coreDir, "core.log")
	testutil.Script(t, filepath.Join(e.coreDir, "ctx-core"), coreWrapper)

	e.srv = httptest.NewServer(http.HandlerFunc(e.handle))
	t.Cleanup(e.srv.Close)
	e.repoURL = e.srv.URL + "/main"

	for _, d := range []string{e.home, e.tmp, filepath.Join(e.prefix, "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeInstalledCtx(t, filepath.Join(e.prefix, "bin", "ctx"), filepath.Join(e.prefix, "lib", "ctx"))
	copyLib(t, filepath.Join(e.prefix, "lib", "ctx"))
	// A dev checkout and a symlink to the install, for the cases that put
	// them first on PATH.
	writeCheckout(t, filepath.Join(root, "checkout"))
	if err := os.MkdirAll(filepath.Join(root, "links"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.prefix, "bin", "ctx"), filepath.Join(root, "links", "ctx")); err != nil {
		t.Fatal(err)
	}
	return e
}

// writeCheckout copies the repo's bin/ctx (its own CTX_LIB probe line) and lib.
func writeCheckout(t *testing.T, dir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "bin", "ctx"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "ctx"), b, 0o755); err != nil {
		t.Fatal(err)
	}
	copyLib(t, filepath.Join(dir, "lib"))
}

// handle serves e.serve under /main. A file NAME.partial makes NAME's
// response promise 1000 more bytes than it sends, then close the connection
// (a download cut off part-way).
func (e *upEnv) handle(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.reqs = append(e.reqs, r.URL.Path)
	e.mu.Unlock()
	rel, ok := strings.CutPrefix(r.URL.Path, "/main/")
	if !ok || strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	f := filepath.Join(e.serve, filepath.FromSlash(rel))
	if b, err := os.ReadFile(f + ".partial"); err == nil {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: " +
			strconv.Itoa(len(b)+1000) + "\r\n\r\n")
		_, _ = buf.Write(b)
		_ = buf.Flush()
		_ = conn.Close()
		return
	}
	b, err := os.ReadFile(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(b)
}

// setServed replaces the served files (paths under /main).
func (e *upEnv) setServed(t *testing.T, files map[string]string) {
	t.Helper()
	_ = os.RemoveAll(e.serve)
	for rel, c := range files {
		f := filepath.Join(e.serve, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// environ is the run's environment: the process environment without any
// CTX_* or proxy variable, then the sandbox's.
func (e *upEnv) environ(path string, extra ...string) []string {
	var env []string
	for _, kv := range e.p.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "CTX_") || strings.Contains(strings.ToLower(k), "proxy") || k == "CURL_HOME" || k == "WGETRC" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+e.home, "CTX_DIR="+filepath.Join(e.home, ".ctx"), "TMPDIR="+e.tmp,
		"GIT_CONFIG_GLOBAL="+filepath.Join(e.home, ".gitconfig"), "GIT_CONFIG_NOSYSTEM=1",
		"CTX_NO_GUM=1", "USER=tester", "NO_PROXY=*", "no_proxy=*",
		"CTX_UPGRADE_REPO="+e.repoURL,
		"CTX_TEST_REAL_CURL="+e.realCurl,
		"CTX_TEST_INSTALL_LOG="+filepath.Join(e.root, "installer.log"),
		"CTX_TEST_CORELOG="+e.coreLog,
		"PATH="+path,
	)
	return append(env, extra...)
}

// mustLocal fails the test unless the run cannot reach the network or leave
// the sandbox: the repo is on 127.0.0.1, HOME and TMPDIR are in the sandbox,
// and the first curl, wget and ctx on PATH are the fakes or the sandbox's.
func (e *upEnv) mustLocal(t *testing.T, env []string, bin string) {
	t.Helper()
	vals := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		vals[k] = v
	}
	if !strings.HasPrefix(vals["CTX_UPGRADE_REPO"], "http://127.0.0.1:") {
		t.Fatalf("sandbox: CTX_UPGRADE_REPO=%q is not local", vals["CTX_UPGRADE_REPO"])
	}
	for _, k := range []string{"HOME", "TMPDIR", "CTX_DIR"} {
		if !under(e.root, vals[k]) {
			t.Fatalf("sandbox: %s=%q is outside %s", k, vals[k], e.root)
		}
	}
	if !under(e.root, bin) {
		t.Fatalf("sandbox: running %s", bin)
	}
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(vals["PATH"]) {
		for _, name := range []string{"curl", "wget", "ctx", "ctx-core"} {
			p := filepath.Join(d, name)
			if seen[name] || !exists(p) {
				continue
			}
			seen[name] = true
			ok := map[string]bool{"curl": p == filepath.Join(e.curlDir, "curl"), "wget": p == filepath.Join(e.wgetDir, "wget"),
				"ctx": under(e.root, p), "ctx-core": under(e.root, p)}[name]
			if !ok {
				t.Fatalf("sandbox: the first %s on PATH is %s", name, p)
			}
		}
	}
}

func (e *upEnv) pathFor(kind, first string) string {
	var parts []string
	switch kind {
	case "", "curl":
		parts = []string{e.curlDir, e.wgetDir, e.noFetch}
	case "wget":
		parts = []string{e.wgetDir, e.noFetch}
	case "none":
		parts = []string{e.noFetch}
	case "noperl":
		parts = []string{e.curlDir, e.wgetDir, e.noPerl}
	}
	if first != "" {
		parts = append([]string{first}, parts...)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// tmpArgs replaces the temp file in argv logs (its name is random).
func (e *upEnv) tmpArgs(argv [][]string) [][]string {
	for _, a := range argv {
		for i, s := range a {
			if strings.HasPrefix(s, e.tmp+"/") {
				a[i] = "<tmp>"
			}
		}
	}
	return argv
}

type upCase struct {
	name        string
	args        []string
	stdin       string
	dry         bool
	path        string            // "", "wget", "none", "noperl"
	first       string            // "", "checkout", "links", "none": what comes first on PATH (default prefix/bin)
	files       map[string]string // served under /main; nil: the default stub and a core.sh
	repo        string            // CTX_UPGRADE_REPO override
	installExit string
	wantCode    int
	vacuous     string
	ran         bool // the installer must have run
	check       func(t *testing.T, e *upEnv, r upRun)
}

func (e *upEnv) run(t *testing.T, goImpl bool, c upCase) upRun {
	t.Helper()
	files := c.files
	if files == nil {
		files = map[string]string{"install.sh": crlf(upInstaller), "lib/core.sh": crlf(`#!/usr/bin/env bash` + "\n" + `CTX_VERSION="9.9.9"` + "\n")}
	}
	e.setServed(t, files)
	_ = os.Remove(filepath.Join(e.root, "installer.log"))
	_ = os.Remove(e.curlLog)
	_ = os.Remove(e.wgetLog)
	_ = os.Remove(e.coreLog)
	e.mu.Lock()
	e.reqs = nil
	e.mu.Unlock()
	if c.repo != "" {
		e.repoURL = c.repo
	} else {
		e.repoURL = e.srv.URL + "/main"
	}

	bin := filepath.Join(e.prefix, "bin", "ctx")
	first := filepath.Join(e.prefix, "bin")
	switch c.first {
	case "checkout":
		bin = filepath.Join(e.root, "checkout", "bin", "ctx")
		first = filepath.Join(e.root, "checkout", "bin")
	case "links":
		first = filepath.Join(e.root, "links")
	case "none":
		first = ""
	}
	impl := []string{"CTX_CORE=off", "CTX_CORE_BIN="}
	if goImpl {
		impl = []string{"CTX_CORE=", "CTX_CORE_BIN=" + filepath.Join(e.coreDir, "ctx-core"), "CTX_TEST_CORE=" + buildCore(t)}
	}
	if c.installExit != "" {
		impl = append(impl, "CTX_TEST_INSTALL_EXIT="+c.installExit)
	}
	env := e.environ(e.pathFor(c.path, first), impl...)
	e.mustLocal(t, env, bin)

	pre := []string{"--no-gum"}
	if c.dry {
		pre = append(pre, "--dry-run")
	}
	cmd := exec.Command(bin, append(append(pre, "upgrade"), c.args...)...)
	cmd.Env = env
	cmd.Dir = e.root
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
	if goImpl {
		log, _ := os.ReadFile(e.coreLog)
		if !strings.Contains("\n"+string(log), "\nupgrade") {
			t.Fatalf("ctx-core did not run upgrade (log %q); stderr %s", log, eb.String())
		}
	}
	if ents, _ := os.ReadDir(e.tmp); len(ents) != 0 {
		t.Errorf("temp files left (go=%v): %v", goImpl, ents)
	}
	e.mu.Lock()
	reqs := append([]string(nil), e.reqs...)
	e.mu.Unlock()
	srvHost := strings.TrimPrefix(e.srv.URL, "http://")
	norm := func(s string) string {
		return curlMS.ReplaceAllString(strings.ReplaceAll(s, srvHost, "<srv>"), "after N ms")
	}
	return upRun{norm(o.String()), norm(eb.String()), code, testutil.Snapshot(t, e.root),
		e.tmpArgs(testutil.ReadArgv(t, e.curlLog)), e.tmpArgs(testutil.ReadArgv(t, e.wgetLog)), reqs}
}

func TestUpgradeMatchesBash(t *testing.T) {
	paths := newUpPaths(t)
	cur := parseCtxVersion(mustRead(t, filepath.Join(repoRoot, "lib", "core.sh")))
	if cur == "" {
		t.Fatal("no CTX_VERSION in lib/core.sh")
	}
	core := func(v string) string { return crlf("#!/usr/bin/env bash\nCTX_VERSION=\"" + v + "\"\n") }
	installed := func(e *upEnv) string {
		return "CTX_INSTALL_BIN=" + filepath.Join(e.prefix, "bin") + "\nCTX_INSTALL_LIB=" + filepath.Join(e.prefix, "lib", "ctx") + "\n"
	}
	ranWith := func(want func(e *upEnv) string) func(t *testing.T, e *upEnv, r upRun) {
		return func(t *testing.T, e *upEnv, r upRun) {
			log := r.tree["installer.log"]
			if !strings.Contains(log, strings.ReplaceAll(want(e), e.root, "<ROOT>")) {
				t.Errorf("installer env:\n%s\nwant it to contain:\n%s", log, want(e))
			}
		}
	}
	cases := []upCase{
		{name: "check up to date", args: []string{"--check"}, files: map[string]string{"lib/core.sh": core(cur)},
			vacuous: "Already on latest advertised version (" + cur + ")."},
		{name: "check newer remote", args: []string{"--check"}, files: map[string]string{"lib/core.sh": core("9.9.9")},
			vacuous: "Update available: " + cur + " → 9.9.9 (run: ctx upgrade)"},
		{name: "check quoted value, CRLF, second CTX_VERSION ignored", args: []string{"--check"},
			files:   map[string]string{"lib/core.sh": crlf("# x\nCTX_VERSION=\"1.\"2\"\nCTX_VERSION=\"3\"\n")},
			vacuous: "Remote main: ctx version 1.2"},
		{name: "check 404 (offline)", args: []string{"--check"}, files: map[string]string{},
			vacuous: "Could not fetch remote version (offline?)."},
		{name: "check offline (connection refused)", args: []string{"--check"}, repo: "http://127.0.0.1:1/main",
			vacuous: "Could not fetch remote version (offline?)."},
		{name: "check no CTX_VERSION line", args: []string{"--check"}, files: map[string]string{"lib/core.sh": "echo hi\n"},
			vacuous: "Could not fetch remote version (offline?)."},
		{name: "check with wget", args: []string{"--check"}, path: "wget", files: map[string]string{"lib/core.sh": core("9.9.9")},
			vacuous: "Remote main: ctx version 9.9.9"},
		{name: "check without curl or wget", args: []string{"--check"}, path: "none",
			vacuous: "Could not fetch remote version (offline?)."},
		{name: "yes: downloads, normalizes and runs the installer", stdin: "y\nnext line\n", ran: true,
			vacuous: "Upgrade finished.", check: ranWith(func(e *upEnv) string {
				return "CTX_REPO=" + e.repoURL + "\nCTX_UPGRADE_ONLY=1\n" + installed(e) + "args=0\n"
			})},
		{name: "default answer is yes; other args ignored", args: []string{"--force"}, stdin: "\n", ran: true, vacuous: "lone CR split"},
		{name: "no", stdin: "n\n", wantCode: 1, vacuous: "Aborted."},
		{name: "yes spelled out is no", stdin: "yes\n", wantCode: 1, vacuous: "Aborted."},
		{name: "EOF at the prompt", stdin: "", wantCode: 130, vacuous: "Cancelled by user."},
		{name: "download fails (404)", stdin: "y\n", files: map[string]string{}, wantCode: 1, vacuous: "Download failed: http://"},
		{name: "download cut off part-way", stdin: "y\n", files: map[string]string{"install.sh.partial": "echo half of a scr"},
			wantCode: 1, vacuous: "Download failed: http://"},
		{name: "download refused (offline)", stdin: "y\n", repo: "http://127.0.0.1:1/main", wantCode: 1, vacuous: "Download failed: http://127.0.0.1:1/main/install.sh"},
		{name: "installer exits 1", stdin: "y\n", installExit: "1", wantCode: 1, ran: true, vacuous: "Upgrade installer failed"},
		{name: "installer exits 7", stdin: "y\n", installExit: "7", wantCode: 1, ran: true, vacuous: "Upgrade installer failed"},
		{name: "wget only", stdin: "y\n", path: "wget", ran: true, vacuous: "Upgrade finished."},
		{name: "wget download fails", stdin: "y\n", path: "wget", files: map[string]string{}, wantCode: 1, vacuous: "Download failed"},
		{name: "no curl or wget", stdin: "y\n", path: "none", wantCode: 1, vacuous: "Need curl or wget to download install.sh"},
		{name: "no perl: tr drops CR", stdin: "y\n", path: "noperl", ran: true,
			files: map[string]string{"install.sh": crlf(upInstallerNoLoneCR)}, vacuous: "installer: no perl"},
		{name: "dry-run", dry: true, vacuous: "[dry-run] would run it with bash: CTX_REPO=http://", check: func(t *testing.T, e *upEnv, r upRun) {
			want := "  [dry-run] would download " + strings.Replace(e.repoURL, strings.TrimPrefix(e.srv.URL, "http://"), "<srv>", 1) + "/install.sh with curl\n" +
				"  [dry-run] would run it with bash: CTX_REPO=" + strings.Replace(e.repoURL, strings.TrimPrefix(e.srv.URL, "http://"), "<srv>", 1) +
				" CTX_UPGRADE_ONLY=1 CTX_INSTALL_BIN=" + filepath.Join(e.prefix, "bin") + " CTX_INSTALL_LIB=" + filepath.Join(e.prefix, "lib", "ctx") + "\n"
			if !strings.Contains(r.stdout, want) || strings.Contains(r.stderr, "Continue?") {
				t.Errorf("dry-run output:\n%s%s\nwant:\n%s", r.stdout, r.stderr, want)
			}
		}},
		{name: "dry-run with wget", dry: true, path: "wget", vacuous: "install.sh with wget"},
		{name: "dry-run without curl or wget", dry: true, path: "none", wantCode: 1, vacuous: "Need curl or wget"},
		{name: "dry-run check still checks", dry: true, args: []string{"--check"}, vacuous: "Remote main: ctx version 9.9.9"},
		{name: "dev checkout on PATH: default paths", first: "checkout", stdin: "y\n", ran: true,
			vacuous: "Target: default install paths (see installer output)", check: ranWith(func(*upEnv) string {
				return "CTX_INSTALL_BIN=unset\nCTX_INSTALL_LIB=unset\n"
			})},
		{name: "dev checkout dry-run", first: "checkout", dry: true, vacuous: "CTX_UPGRADE_ONLY=1\n"},
		{name: "symlinked ctx on PATH", first: "links", dry: true, vacuous: "/links/ctx + "},
		{name: "ctx not on PATH", first: "none", stdin: "y\n", wantCode: 1,
			vacuous: "ctx not found in PATH — install first: curl -fsSL http://<srv>/main/install.sh | bash"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newUpEnv(t, paths)
			before := testutil.Snapshot(t, e.root)
			want := e.run(t, false, c)
			bashTree := want.tree
			// Reset what the run may have written (the installer log only).
			_ = os.Remove(filepath.Join(e.root, "installer.log"))
			got := e.run(t, true, c)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n curl go %q bash %q\n wget go %q bash %q\n reqs go %q bash %q\n tree diff: %s",
					got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, got.curl, want.curl, got.wget, want.wget, got.reqs, want.reqs, testutil.DiffSnapshots(bashTree, got.tree))
			}
			if want.code != c.wantCode {
				t.Errorf("bash exit %d, want %d\n%s%s", want.code, c.wantCode, want.stdout, want.stderr)
			}
			if !strings.Contains(want.stdout+want.stderr, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
			_, ran := got.tree["installer.log"]
			if ran != c.ran {
				t.Errorf("installer ran: %v, want %v", ran, c.ran)
			}
			// Nothing but the installer's log may change: no install file is
			// replaced, and no temp file is left.
			after := map[string]string{}
			for k, v := range got.tree {
				if k != "installer.log" {
					after[k] = v
				}
			}
			if d := testutil.DiffSnapshots(before, after); d != "" {
				t.Errorf("files changed:\n%s", d)
			}
			if c.dry && !slices.Contains(c.args, "--check") && (len(got.reqs) != 0 || len(got.curl) != 0 || len(got.wget) != 0) {
				t.Errorf("dry-run downloaded: reqs %q curl %q wget %q", got.reqs, got.curl, got.wget)
			}
			if c.check != nil {
				c.check(t, e, got)
			}
		})
	}
}

func mustRead(t *testing.T, f string) []byte {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestUpgradeCtrlC: Ctrl+C at the prompt prints the cancel line and exits
// 130 with nothing downloaded; Ctrl+C while the installer runs exits 130
// after it, without a message, and leaves no temp file (Go and Bash alike).
func TestUpgradeCtrlC(t *testing.T) {
	paths := newUpPaths(t)
	waiting := crlf("printf 'installer waiting: '\nread -r x\necho \"installer got $x\"\n")
	for _, tc := range []struct {
		name    string
		goImpl  bool
		answers []string
		files   map[string]string
		cancel  int // cancel lines expected
	}{
		// Bash's INT trap exits 130 without the cancel line at a plain read;
		// Go prints it (R10).
		{"prompt bash", false, []string{testutil.CtrlC}, nil, 0},
		{"prompt go", true, []string{testutil.CtrlC}, nil, 1},
		{"installer bash", false, []string{"y", testutil.CtrlC}, map[string]string{"install.sh": waiting}, 0},
		{"installer go", true, []string{"y", testutil.CtrlC}, map[string]string{"install.sh": waiting}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newUpEnv(t, paths)
			if tc.files != nil {
				e.setServed(t, tc.files)
			} else {
				e.setServed(t, map[string]string{"install.sh": crlf(upInstaller)})
			}
			bin := filepath.Join(e.prefix, "bin", "ctx")
			impl := []string{"CTX_CORE=off", "CTX_CORE_BIN="}
			if tc.goImpl {
				impl = []string{"CTX_CORE=", "CTX_CORE_BIN=" + filepath.Join(e.coreDir, "ctx-core"), "CTX_TEST_CORE=" + buildCore(t)}
			}
			e.repoURL = e.srv.URL + "/main"
			env := e.environ(e.pathFor("", filepath.Join(e.prefix, "bin")), impl...)
			e.mustLocal(t, env, bin)
			// RunPTY answers after a second of silence; warm up the first
			// exec of bin/ctx and ctx-core.
			warm := exec.Command(bin, "version")
			warm.Env = env
			_ = warm.Run()
			_ = os.Remove(e.coreLog)
			before := testutil.Snapshot(t, e.root)
			cmd := exec.Command(bin, "--no-gum", "upgrade")
			cmd.Env = env
			cmd.Dir = e.root
			out, code := testutil.RunPTY(t, cmd, tc.answers, 60*time.Second)
			if code != 130 || strings.Count(out, "Cancelled by user.") != tc.cancel {
				t.Errorf("exit %d, want 130 with %d cancel line(s):\n%s", code, tc.cancel, out)
			}
			for _, bad := range []string{"Upgrade finished", "Upgrade installer failed", "installer got"} {
				if strings.Contains(out, bad) {
					t.Errorf("output has %q:\n%s", bad, out)
				}
			}
			if ents, _ := os.ReadDir(e.tmp); len(ents) != 0 {
				t.Errorf("temp files left: %v", ents)
			}
			if d := testutil.DiffSnapshots(before, testutil.Snapshot(t, e.root)); d != "" && len(tc.answers) == 1 {
				t.Errorf("Ctrl+C at the prompt changed files:\n%s", d)
			}
			e.mu.Lock()
			n := len(e.reqs)
			e.mu.Unlock()
			if len(tc.answers) == 1 && n != 0 {
				t.Errorf("Ctrl+C at the prompt still downloaded: %v", e.reqs)
			}
			if tc.goImpl {
				if log, _ := os.ReadFile(e.coreLog); !strings.Contains("\n"+string(log), "\nupgrade") {
					t.Errorf("ctx-core did not run upgrade: %q", log)
				}
			}
		})
	}
}
