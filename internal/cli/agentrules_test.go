package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
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

var mktempRandRE = regexp.MustCompile(`AGENTS\.md\.[A-Za-z0-9]{6}:`)

// stripMktempMsg drops mktemp(1)'s own stderr lines, which differ by platform and locale.
func stripMktempMsg(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "✗ ") || l == "" {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
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
		{name: "write dash file", args: []string{"--write", "-x"}, vacuous: "Cannot write next to -x"},
		{name: "extra args", args: []string{"--write", "R.md", "more"}, vacuous: "written to"},
		{name: "upsert existing", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "# Mine\n\nkeep\n\n"+blockOld+"\ntail\n"), vacuous: "written to"},
		{name: "upsert appends", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "# Mine\nkeep"), vacuous: "written to"},
		{name: "upsert ends blank", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "# Mine\n\n"), vacuous: "written to"},
		{name: "upsert only newline", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "\n"), vacuous: "written to"},
		{name: "upsert whitespace last line", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "a\n  \n"), vacuous: "written to"},
		{name: "upsert only block", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", blockOld), vacuous: "written to"},
		{name: "upsert empty file", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", ""), vacuous: "written to"},
		{name: "unterminated begin drops rest", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "head\n<!-- BEGIN ctx agent rules -->\nlost\n"), vacuous: "written to"},
		{name: "stray end dropped", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "a\n<!-- END ctx agent rules -->\nb\n"), vacuous: "written to"},
		{name: "two blocks", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", blockOld+"mid\n"+blockOld), vacuous: "written to"},
		{name: "crlf lines kept", args: []string{"--write", "AGENTS.md"}, tweak: write("AGENTS.md", "a\r\n"+blockOld), vacuous: "written to"},
		{name: "symlink replaced", args: []string{"--write", "link.md"}, tweak: func(home string) {
			write("real.md", "real\n")(home)
			_ = os.Symlink("real.md", filepath.Join(home, "proj", "link.md"))
		}, vacuous: "written to"},
		{name: "target is a directory", args: []string{"--write", "d"}, tweak: func(home string) { _ = os.Mkdir(filepath.Join(home, "proj", "d"), 0o755) }, vacuous: "written to"},
		{name: "dry run default", dry: true, args: []string{"--write"}, vacuous: "[dry-run] Would write ctx agent rules to"},
		{name: "dry run file", dry: true, args: []string{"--write", "R.md"}, tweak: write("R.md", "x\n"), vacuous: "[dry-run]"},
		{name: "dry run print is not dry", dry: true, vacuous: "BEGIN ctx agent rules"},
		{name: "missing parent dir", args: []string{"--write", "nope/AGENTS.md"}, vacuous: "Cannot write next to nope/AGENTS.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newArEnv(t)
			e.seed(t, c.repo, c.tweak)
			want := e.bash(t, c.dry, c.args...)
			e.seed(t, c.repo, c.tweak)
			got := e.goRun(t, c.dry, c.args...)
			if runtime.GOOS != "darwin" {
				got.stderr, want.stderr = stripMktempMsg(got.stderr), stripMktempMsg(want.stderr)
			}
			// BSD mktemp reports the template with its random part filled in.
			want.stderr = mktempRandRE.ReplaceAllString(want.stderr, "AGENTS.md.XXXXXX:")
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
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600 (mktemp quirk mirrored)", st.Mode().Perm())
	}
}
