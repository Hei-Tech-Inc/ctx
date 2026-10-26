package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

var tmpNameRE = regexp.MustCompile(`^proj/d/d\.[A-Za-z0-9]+$`)

type arRun struct {
	stdout, stderr string
	code           int
	tree           map[string]string
}

// arEnv is a fixture HOME with the cwd proj/ inside it; git is the system one.
type arEnv struct {
	f   *fixture
	p   *ptest
	cwd string
}

func newArEnv(t *testing.T) *arEnv {
	t.Helper()
	p := newPtest(t)
	e := &arEnv{f: newPFixture(p), p: p}
	p.Setenv("PATH", testutil.SystemPath(t))
	p.Setenv("CTX_NO_GUM", "1")
	return e
}

// seed resets HOME to proj/ (and, with repo, a git repository at proj with a sub/ cwd).
func (e *arEnv) seed(t *testing.T, repo bool, tweak func(home string)) {
	t.Helper()
	t.Cleanup(func() { // read-only fixtures must not block TempDir's cleanup
		_ = filepath.WalkDir(e.f.home, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	_ = filepath.WalkDir(e.f.home, func(p string, d os.DirEntry, err error) error { // undo read-only fixtures
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	if err := os.RemoveAll(e.f.home); err != nil {
		t.Fatal(err)
	}
	e.cwd = filepath.Join(e.f.home, "proj")
	if err := os.MkdirAll(filepath.Join(e.cwd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if repo {
		cmd := exec.Command("git", "init", "-q")
		cmd.Dir = e.cwd
		cmd.Env = e.p.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		e.cwd = filepath.Join(e.cwd, "sub")
	}
	if tweak != nil {
		tweak(e.f.home)
	}
}

func (e *arEnv) collect(t *testing.T, o, eb string, code int) arRun {
	tree := map[string]string{}
	for k, v := range testutil.Snapshot(t, e.f.home) {
		tree[tmpNameRE.ReplaceAllString(k, "proj/d/d.TMP")] = v // mv into a directory keeps the random name
	}
	return arRun{o, eb, code, tree}
}

func (e *arEnv) bash(t *testing.T, dry bool, args ...string) arRun {
	pre := []string{"--no-gum"}
	if dry {
		pre = append(pre, "--dry-run")
	}
	cmd := exec.Command(bashBin(t), append(append(pre, "agent-rules"), args...)...)
	cmd.Env = e.p.Environ("CTX_CORE=off")
	cmd.Dir = e.cwd
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

func (e *arEnv) goRun(t *testing.T, dry bool, args ...string) arRun {
	var o, eb bytes.Buffer
	var code int
	e.p.inProc(e.cwd, func() {
		if dry {
			_ = os.Setenv("CTX_DRY_RUN", "1") // inProc restores the environment
		}
		code = AgentRules(tui.Term{Out: &o, Err: &eb}, args)
	})
	return e.collect(t, o.String(), eb.String(), code)
}

func TestAgentRulesMatchesBash(t *testing.T) {
	blockOld := "<!-- BEGIN ctx agent rules -->\nold text\n<!-- END ctx agent rules -->\n"
	write := func(rel, content string) func(string) {
		return func(home string) {
			p := filepath.Join(home, "proj", rel)
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(content), 0o640); err != nil {
				panic(err)
			}
		}
	}
	cases := []struct {
		name    string
		repo    bool
		dry     bool
		args    []string
		tweak   func(home string)
		vacuous string
	}{
		{name: "print", vacuous: "BEGIN ctx agent rules"},
		{name: "print ignores other args", args: []string{"x", "--write"}, vacuous: "END ctx agent rules"},
		{name: "write new default cwd", args: []string{"--write"}, vacuous: "written to"},
		{name: "write new in git top", repo: true, args: []string{"--write"}, vacuous: "written to"},
		{name: "write explicit file", args: []string{"--write", "R.md"}, vacuous: "written to"},
		{name: "write empty file arg is default", args: []string{"--write", ""}, vacuous: "written to"},
		{name: "write dash file", args: []string{"--write", "-x"}, vacuous: "written to ./-x"},
		{name: "extra args", args: []string{"--write", "R.md", "more"}, vacuous: "written to"},
		{name: "upsert existing", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "# Mine\n\nkeep\n\n"+blockOld+"\ntail\n"), vacuous: "written to"},
		{name: "upsert appends", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "# Mine\nkeep"), vacuous: "written to"},
		{name: "upsert ends blank", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "# Mine\n\n"), vacuous: "written to"},
		{name: "upsert only newline", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "\n"), vacuous: "written to"},
		{name: "upsert whitespace last line", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "a\n  \n"), vacuous: "written to"},
		{name: "upsert only block", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", blockOld), vacuous: "written to"},
		{name: "upsert empty file", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", ""), vacuous: "written to"},
		{name: "unterminated begin", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "head\n<!-- BEGIN ctx agent rules -->\nkept\n"), vacuous: "Unterminated ctx block in AGENTS.md; add the END marker or remove the BEGIN line"},
		{name: "stray end kept", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "a\n<!-- END ctx agent rules -->\nb\n"), vacuous: "written to"},
		{name: "two blocks", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", blockOld+"mid\n"+blockOld), vacuous: "written to"},
		{name: "crlf lines kept", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "a\r\n"+blockOld), vacuous: "written to"},
		{name: "symlink kept", args: []string{"--write", "link.md"}, tweak: func(home string) {
			write("real.md", "real\n")(home)
			_ = os.Symlink("real.md", filepath.Join(home, "proj", "link.md"))
		}, vacuous: "written to"},
		{name: "target is a directory", args: []string{"--write", "d"}, tweak: func(home string) { _ = os.Mkdir(filepath.Join(home, "proj", "d"), 0o755) }, vacuous: "d is a directory"},
		{name: "read-only target", args: []string{"--write", "AGENTS.md"}, tweak: func(home string) {
			write("AGENTS.md", "mine\n")(home)
			_ = os.Chmod(filepath.Join(home, "proj", "AGENTS.md"), 0o444)
		}, vacuous: "Cannot write AGENTS.md"},
		{name: "read-only directory new file", args: []string{"--write", "ro/AGENTS.md"}, tweak: func(home string) {
			_ = os.MkdirAll(filepath.Join(home, "proj", "ro"), 0o755)
			_ = os.Chmod(filepath.Join(home, "proj", "ro"), 0o555)
		}, vacuous: "Cannot write ro/AGENTS.md"},
		{name: "read-only directory existing file", args: []string{"--write", "ro/AGENTS.md"}, tweak: func(home string) {
			_ = os.MkdirAll(filepath.Join(home, "proj", "ro"), 0o755)
			write("ro/AGENTS.md", "mine\n")(home)
			_ = os.Chmod(filepath.Join(home, "proj", "ro"), 0o555)
		}, vacuous: "Cannot write ro/AGENTS.md"},
		{name: "dangling symlink creates target", args: []string{"--write", "dl.md"}, tweak: func(home string) {
			_ = os.Symlink("made.md", filepath.Join(home, "proj", "dl.md"))
		}, vacuous: "written to dl.md"},
		{name: "relative link chain", args: []string{"--write", "l1.md"}, tweak: func(home string) {
			_ = os.MkdirAll(filepath.Join(home, "proj", "sub2"), 0o755)
			write("sub2/real.md", "r\n")(home)
			_ = os.Symlink("sub2/l2.md", filepath.Join(home, "proj", "l1.md"))
			_ = os.Symlink("real.md", filepath.Join(home, "proj", "sub2", "l2.md"))
		}, vacuous: "written to l1.md"},
		{name: "mode kept", args: []string{"--write", "AGENTS.md"}, tweak: func(home string) {
			write("AGENTS.md", "mine\n")(home)
			_ = os.Chmod(filepath.Join(home, "proj", "AGENTS.md"), 0o644)
		}, vacuous: "written to"},
		{name: "dry run default", dry: true, args: []string{"--write"}, vacuous: "[dry-run] Would write ctx agent rules to"},
		{name: "dry run file", dry: true, args: []string{"--write", "R.md"}, tweak: write("R.md", "x\n"), vacuous: "[dry-run]"},
		{name: "dry run print is not dry", dry: true, vacuous: "BEGIN ctx agent rules"},
		{name: "missing parent dir", args: []string{"--write", "nope/AGENTS.md"}, vacuous: "Cannot write nope/AGENTS.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newArEnv(t)
			e.seed(t, c.repo, c.tweak)
			want := e.bash(t, c.dry, c.args...)
			e.seed(t, c.repo, c.tweak)
			got := e.goRun(t, c.dry, c.args...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n tree diff: %s",
					got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, testutil.DiffSnapshots(want.tree, got.tree))
			}
			if !strings.Contains(want.stdout+want.stderr, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
		})
	}
}

// TestAgentRulesWriteResult pins what the upsert leaves behind (Bash does the same).
func TestAgentRulesWriteResult(t *testing.T) {
	e := newArEnv(t)
	e.seed(t, false, nil)
	file := filepath.Join(e.cwd, "AGENTS.md")
	if err := os.WriteFile(file, []byte("# Mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // the second run changes nothing
		if r := e.goRun(t, false, "--write"); r.code != 0 {
			t.Fatalf("code %d %s", r.code, r.stderr)
		}
		got, _ := os.ReadFile(file)
		if want := "# Mine\n\n" + agentRulesText; string(got) != want {
			t.Fatalf("run %d:\n%q\nwant\n%q", i, got, want)
		}
	}
}

// TestAgentRulesWriteThrough: the link and the mode stay; a new file gets the umask default.
func TestAgentRulesWriteThrough(t *testing.T) {
	e := newArEnv(t)
	e.seed(t, false, nil)
	real := filepath.Join(e.cwd, "real.md")
	link := filepath.Join(e.cwd, "link.md")
	if err := os.WriteFile(real, []byte("x\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", link); err != nil {
		t.Fatal(err)
	}
	if r := e.goRun(t, false, "--write", "link.md"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("link replaced")
	}
	got, _ := os.ReadFile(real)
	if want := "x\n\n" + agentRulesText; string(got) != want {
		t.Errorf("target %q", got)
	}
	if st, _ := os.Stat(real); st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want 0640", st.Mode().Perm())
	}
	if r := e.goRun(t, false, "--write", "new.md"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if st, _ := os.Stat(filepath.Join(e.cwd, "new.md")); st.Mode().Perm()&0o044 == 0 {
		t.Errorf("new file mode %v, want the umask default", st.Mode().Perm())
	}
}

// TestAgentRulesUnreadable: an unreadable file is an error, not an empty file.
// (Apart from the differential suite because the tree snapshot reads files.)
func TestAgentRulesUnreadable(t *testing.T) {
	var res [2]arRun
	for i, impl := range []string{"bash", "go"} {
		e := newArEnv(t)
		e.seed(t, false, nil)
		file := filepath.Join(e.cwd, "AGENTS.md")
		if err := os.WriteFile(file, []byte("mine\n"), 0o200); err != nil {
			t.Fatal(err)
		}
		var o, eb bytes.Buffer
		code := 0
		if impl == "bash" {
			cmd := exec.Command(bashBin(t), "--no-gum", "agent-rules", "--write", "AGENTS.md")
			cmd.Env, cmd.Dir, cmd.Stdout, cmd.Stderr = e.p.Environ("CTX_CORE=off"), e.cwd, &o, &eb
			if err := cmd.Run(); err != nil {
				code = err.(*exec.ExitError).ExitCode()
			}
		} else {
			e.p.inProc(e.cwd, func() { code = AgentRules(tui.Term{Out: &o, Err: &eb}, []string{"--write", "AGENTS.md"}) })
		}
		_ = os.Chmod(file, 0o644)
		if b, _ := os.ReadFile(file); string(b) != "mine\n" {
			t.Errorf("%s changed the file: %q", impl, b)
		}
		res[i] = arRun{stdout: o.String(), stderr: eb.String(), code: code}
	}
	if res[0].code != 1 || !reflect.DeepEqual(res[0], res[1]) {
		t.Errorf("bash %+v go %+v", res[0], res[1])
	}
	if !strings.Contains(res[0].stderr, "Cannot read AGENTS.md") {
		t.Errorf("vacuous: %q", res[0].stderr)
	}
}

// TestAgentRulesWriteFailureKeepsOriginal: a failure while writing the new
// content leaves the file as it was and removes the temp file.
func TestAgentRulesWriteFailureKeepsOriginal(t *testing.T) {
	e := newArEnv(t)
	e.seed(t, false, nil)
	file := filepath.Join(e.cwd, "AGENTS.md")
	if err := os.WriteFile(file, []byte("mine\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	procMu.Lock()
	defer procMu.Unlock()
	saved := writeTemp
	defer func() { writeTemp = saved }()
	writeTemp = func(f *os.File, data []byte) error {
		_, _ = f.Write(data[:len(data)/2]) // a partial write, then ENOSPC
		return syscall.ENOSPC
	}
	var o, eb bytes.Buffer
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(e.cwd); err != nil {
		t.Fatal(err)
	}
	if code := AgentRules(tui.Term{Out: &o, Err: &eb}, []string{"--write", "AGENTS.md"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(eb.String(), "Cannot write AGENTS.md") {
		t.Errorf("stderr %q", eb.String())
	}
	if b, _ := os.ReadFile(file); string(b) != "mine\n" {
		t.Errorf("original changed: %q", b)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(e.cwd)
	for _, en := range entries {
		if en.Name() != "AGENTS.md" && en.Name() != "sub" {
			t.Errorf("leftover file: %s", en.Name())
		}
	}
}
