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

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// undoStamp is the frozen clock of both implementations unless a case freezes
// it elsewhere: the pre-restore backup lands in backups/<undoStamp>.
const undoStamp = "20301231-235959"

type undoRun struct {
	stdout, stderr string
	code           int
	tree           map[string]string
}

type undoEnv struct {
	f *fixture
	p *ptest
}

func newUndoEnv(t *testing.T) *undoEnv {
	t.Helper()
	p := newPtest(t)
	e := &undoEnv{f: newPFixture(p), p: p}
	p.Setenv("PATH", testutil.SystemPath(t))
	p.Setenv("CTX_NO_GUM", "1")
	return e
}

// backup describes one backup dir: its stamp, files (name -> content, mode)
// and manifest text.
type backup struct {
	stamp    string
	files    map[string]string
	modes    map[string]os.FileMode
	manifest string
	age      time.Duration // mtime offset before now (larger is older)
}

// seed resets HOME, writes the current files (path relative to HOME -> content)
// and the backups, then runs tweak.
func (e *undoEnv) seed(t *testing.T, current map[string]string, backups []backup, tweak func(home string)) {
	t.Helper()
	home := e.f.home
	_ = filepath.WalkDir(home, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.f.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, c := range current {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	for _, b := range backups {
		d := filepath.Join(e.f.dir, "backups", b.stamp)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, c := range b.files {
			mode := os.FileMode(0o644)
			if m, ok := b.modes[name]; ok {
				mode = m
			}
			if err := os.WriteFile(filepath.Join(d, name), []byte(c), mode); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(filepath.Join(d, name), mode)
		}
		if b.manifest != "-" {
			m := strings.ReplaceAll(b.manifest, "$HOME", home)
			if err := os.WriteFile(filepath.Join(d, "manifest.txt"), []byte(m), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		mt := now.Add(-time.Hour - b.age)
		if err := os.Chtimes(d, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	if tweak != nil {
		tweak(home)
	}
	t.Cleanup(func() { // read-only fixtures must not block TempDir's cleanup
		_ = filepath.WalkDir(home, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
}

func (e *undoEnv) collect(t *testing.T, o, eb string, code int) undoRun {
	return undoRun{o, eb, code, testutil.Snapshot(t, e.f.home)}
}

func (e *undoEnv) bash(t *testing.T, stamp, stdin string, dry bool) undoRun {
	pre := []string{"--no-gum"}
	if dry {
		pre = append(pre, "--dry-run")
	}
	var dateDir string
	locked(func() { dateDir = testutil.FixedStampDate(t, stamp) })
	cmd := exec.Command(bashBin(t), append(pre, "undo")...)
	cmd.Env = e.p.Environ("CTX_CORE=off", "PATH="+dateDir+string(os.PathListSeparator)+e.p.Getenv("PATH"))
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

func (e *undoEnv) goRun(t *testing.T, stamp, stdin string, dry bool) undoRun {
	fixed, err := time.ParseInLocation("20060102-150405", stamp, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	var o, eb bytes.Buffer
	var code int
	e.p.inProc(e.f.home, func() {
		if dry {
			_ = os.Setenv("CTX_DRY_RUN", "1") // inProc restores the environment
		}
		old := ctxconfig.Now
		ctxconfig.Now = func() time.Time { return fixed }
		defer func() { ctxconfig.Now = old }()
		code = Undo(tui.Term{In: strings.NewReader(stdin), Out: &o, Err: &eb}, nil)
	})
	return e.collect(t, o.String(), eb.String(), code)
}

type undoCase struct {
	name     string
	current  map[string]string
	backups  []backup
	tweak    func(home string)
	stdin    string
	dry      bool
	stamp    string // frozen clock; default undoStamp
	wantCode int
	vacuous  string // substring Bash must print
	check    func(t *testing.T, home string, r undoRun)
}

func runUndoCases(t *testing.T, cases []undoCase) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newUndoEnv(t)
			stamp := c.stamp
			if stamp == "" {
				stamp = undoStamp
			}
			e.seed(t, c.current, c.backups, c.tweak)
			want := e.bash(t, stamp, c.stdin, c.dry)
			e.seed(t, c.current, c.backups, c.tweak)
			got := e.goRun(t, stamp, c.stdin, c.dry)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n tree diff: %s",
					got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, testutil.DiffSnapshots(want.tree, got.tree))
			}
			if want.code != c.wantCode {
				t.Errorf("bash exit %d, want %d\n%s%s", want.code, c.wantCode, want.stdout, want.stderr)
			}
			if !strings.Contains(want.stdout+want.stderr, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
			if c.check != nil {
				c.check(t, e.f.home, got)
			}
		})
	}
}

func readHome(t *testing.T, home, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestUndoMatchesBash(t *testing.T) {
	cur := map[string]string{".gitconfig": "git now\n", ".ssh/config": "ssh now\n"}
	b1 := backup{stamp: "20200101-000000", files: map[string]string{".gitconfig": "git old\n", "config": "ssh old\n"},
		modes: map[string]os.FileMode{"config": 0o600}, manifest: "$HOME/.gitconfig\n$HOME/.ssh/config\n"}
	older := backup{stamp: "20190101-000000", files: map[string]string{".gitconfig": "git oldest\n"}, manifest: "$HOME/.gitconfig\n", age: time.Hour}
	restored := func(t *testing.T, home string, r undoRun) {
		if got := readHome(t, home, ".gitconfig"); got != "git old\n" {
			t.Errorf(".gitconfig = %q", got)
		}
		if got := readHome(t, home, ".ssh/config"); got != "ssh old\n" {
			t.Errorf(".ssh/config = %q", got)
		}
		if m := modeOf(t, filepath.Join(home, ".ssh/config")); m != 0o600 {
			t.Errorf("mode %v, want the backup's 0600", m)
		}
		nb := filepath.Join(home, ".ctx/backups", undoStamp)
		if got := readHome(t, nb, ".gitconfig"); got != "git now\n" {
			t.Errorf("pre-restore backup .gitconfig = %q", got)
		}
		if got := readHome(t, nb, "manifest.txt"); got != filepath.Join(home, ".gitconfig")+"\n"+filepath.Join(home, ".ssh/config")+"\n" {
			t.Errorf("pre-restore manifest = %q", got)
		}
	}
	unchanged := func(t *testing.T, home string, r undoRun) {
		if got := readHome(t, home, ".gitconfig"); got != "git now\n" {
			t.Errorf(".gitconfig changed: %q", got)
		}
		if _, err := os.Stat(filepath.Join(home, ".ctx/backups", undoStamp)); err == nil {
			t.Error("a backup was made")
		}
	}
	runUndoCases(t, []undoCase{
		{name: "restore", current: cur, backups: []backup{older, b1}, stdin: "y\n", vacuous: "Undo complete.", check: restored},
		{name: "default answer", current: cur, backups: []backup{b1}, stdin: "\n", vacuous: "Restored: ", check: restored},
		{name: "dry run", current: cur, backups: []backup{b1}, dry: true, vacuous: "[dry-run] would restore", check: unchanged},
		{name: "no backups", current: cur, wantCode: 1, vacuous: "No backups found in"},
		{name: "empty backups dir", current: cur, tweak: func(home string) { _ = os.MkdirAll(filepath.Join(home, ".ctx/backups"), 0o755) }, wantCode: 1, vacuous: "No backups found in"},
		{name: "no manifest", current: cur, backups: []backup{{stamp: "20200101-000000", files: map[string]string{"x": "1"}, manifest: "-"}}, wantCode: 1, vacuous: "Manifest not found at"},
		{name: "answer no", current: cur, backups: []backup{b1}, stdin: "n\n", wantCode: 1, vacuous: "Aborted.", check: unchanged},
		{name: "yes is not y", current: cur, backups: []backup{b1}, stdin: "yes\n", wantCode: 1, vacuous: "Aborted.", check: unchanged},
		{name: "eof", current: cur, backups: []backup{b1}, wantCode: 130, vacuous: "Cancelled by user.", check: unchanged},
		{name: "original missing", current: map[string]string{".ssh/x": ""}, backups: []backup{b1}, stdin: "y\n", vacuous: "Undo complete.", check: func(t *testing.T, home string, r undoRun) {
			if m := modeOf(t, filepath.Join(home, ".ssh/config")); m != 0o600 {
				t.Errorf("new file mode %v, want 0600", m)
			}
			if _, err := os.Stat(filepath.Join(home, ".ctx/backups", undoStamp)); err == nil {
				t.Error("nothing to back up, yet a backup dir was made")
			}
		}},
		{name: "symlink written through", current: map[string]string{"dot/gitconfig": "git now\n", ".ssh/config": "ssh now\n"}, backups: []backup{b1},
			tweak: func(home string) { _ = os.Symlink("dot/gitconfig", filepath.Join(home, ".gitconfig")) }, stdin: "y\n", vacuous: "Undo complete.",
			check: func(t *testing.T, home string, r undoRun) {
				if l, err := os.Readlink(filepath.Join(home, ".gitconfig")); err != nil || l != "dot/gitconfig" {
					t.Errorf("link replaced: %q %v", l, err)
				}
				if got := readHome(t, home, "dot/gitconfig"); got != "git old\n" {
					t.Errorf("target = %q", got)
				}
			}},
		{name: "manifest without final newline", current: cur, backups: []backup{{stamp: "20200101-000000", files: b1.files, modes: b1.modes, manifest: "$HOME/.gitconfig\n\n$HOME/.ssh/config"}}, stdin: "y\n", vacuous: "Undo complete.", check: restored},
		{name: "not in backup", current: cur, backups: []backup{{stamp: "20200101-000000", files: map[string]string{".gitconfig": "git old\n"}, manifest: "$HOME/.gitconfig\n$HOME/.ssh/config\n"}}, stdin: "y\n", wantCode: 1, vacuous: "Not in backup: "},
		{name: "relative path", current: cur, backups: []backup{{stamp: "20200101-000000", files: map[string]string{".gitconfig": "git old\n", "rel": "r\n"}, manifest: "$HOME/.gitconfig\nsub/rel\n"}}, stdin: "y\n", wantCode: 1, vacuous: "not an absolute path"},
	})
}

// TestUndoEdge is Review Focus 3: same-name files, a vanished directory, an
// undo in the same second as its backup, and a failing restore.
func TestUndoEdge(t *testing.T) {
	cur := map[string]string{".gitconfig": "git now\n", ".ssh/config": "ssh now\n", ".ctx/config": "ctx now\n"}
	same := backup{stamp: "20261020-101010", files: map[string]string{"config": "which one?\n", ".gitconfig": "git old\n"},
		manifest: "$HOME/.ssh/config\n$HOME/.ctx/config\n$HOME/.gitconfig\n$HOME/.gitconfig\n"}
	runUndoCases(t, []undoCase{
		{name: "same basename skipped", current: cur, backups: []backup{same}, stdin: "y\n", wantCode: 1, vacuous: "Ambiguous backup for ",
			check: func(t *testing.T, home string, r undoRun) {
				if got := readHome(t, home, ".ssh/config"); got != "ssh now\n" {
					t.Errorf(".ssh/config overwritten: %q", got)
				}
				if got := readHome(t, home, ".ctx/config"); got != "ctx now\n" {
					t.Errorf(".ctx/config overwritten: %q", got)
				}
				if got := readHome(t, home, ".gitconfig"); got != "git old\n" {
					t.Errorf(".gitconfig = %q", got)
				}
				if n := strings.Count(r.stdout, "Restored: "); n != 1 {
					t.Errorf("restored %d times, want once (duplicate manifest line)", n)
				}
			}},
		{name: "same second as the backup", current: cur, backups: []backup{same}, stamp: "20261020-101010", stdin: "y\n", wantCode: 1, vacuous: "backups/20261020-101010-2",
			check: func(t *testing.T, home string, r undoRun) {
				old := filepath.Join(home, ".ctx/backups/20261020-101010")
				if got := readHome(t, old, ".gitconfig"); got != "git old\n" {
					t.Errorf("restored backup overwritten: %q", got)
				}
				if got := readHome(t, old+"-2", ".gitconfig"); got != "git now\n" {
					t.Errorf("new backup = %q", got)
				}
			}},
		{name: "only ambiguous", current: cur, backups: []backup{{stamp: "20200101-000000", files: map[string]string{"config": "x\n"}, manifest: "$HOME/.ssh/config\n$HOME/.ctx/config\n"}}, stdin: "y\n", wantCode: 1, vacuous: "Nothing to restore from 20200101-000000."},
		{name: "a file named manifest.txt", current: map[string]string{"manifest.txt": "mine\n"}, backups: []backup{{stamp: "20200101-000000", manifest: "$HOME/manifest.txt\n"}}, wantCode: 1, vacuous: "Ambiguous backup for "},
		{name: "directory gone", current: cur, backups: []backup{{stamp: "20200101-000000", files: map[string]string{".gitconfig": "git old\n", "f.txt": "f\n"}, manifest: "$HOME/gone/f.txt\n$HOME/.gitconfig\n"}}, stdin: "y\n", wantCode: 1, vacuous: "/gone no longer exists; skipped",
			check: func(t *testing.T, home string, r undoRun) {
				if _, err := os.Stat(filepath.Join(home, "gone")); err == nil {
					t.Error("gone/ was created")
				}
			}},
		{name: "dot component skipped", current: map[string]string{"sub/x": "", "f.txt": "now\n", ".gitconfig": "git now\n"}, backups: []backup{{stamp: "20200101-000000", files: map[string]string{".gitconfig": "git old\n", "f.txt": "old\n"}, manifest: "$HOME/sub/../f.txt\n$HOME/.gitconfig\n"}},
			stdin: "y\n", wantCode: 1, vacuous: "it has a . or .. component; skipped",
			check: func(t *testing.T, home string, r undoRun) {
				if got := readHome(t, home, "f.txt"); got != "now\n" {
					t.Errorf("f.txt = %q", got)
				}
			}},
		{name: "backup dir cannot be made", current: map[string]string{".gitconfig": "git now\n"}, backups: []backup{{stamp: "20200101-000000", files: map[string]string{".gitconfig": "git old\n", "new.txt": "new\n"}, manifest: "$HOME/new.txt\n$HOME/.gitconfig\n"}},
			tweak: func(home string) { _ = os.Chmod(filepath.Join(home, ".ctx", "backups"), 0o555) }, stdin: "y\n", wantCode: 1, vacuous: "Could not create a backup directory; nothing restored.",
			check: func(t *testing.T, home string, r undoRun) {
				if _, err := os.Stat(filepath.Join(home, "new.txt")); err == nil {
					t.Error("new.txt was restored before the backup dir failed")
				}
			}},
		{name: "restore fails", current: map[string]string{"ro/f.txt": "now\n", ".gitconfig": "git now\n"}, backups: []backup{{stamp: "20200101-000000", files: map[string]string{".gitconfig": "git old\n", "f.txt": "old\n"}, manifest: "$HOME/ro/f.txt\n$HOME/.gitconfig\n"}},
			tweak: func(home string) { _ = os.Chmod(filepath.Join(home, "ro"), 0o555) }, stdin: "y\n", wantCode: 1, vacuous: "Could not restore ",
			check: func(t *testing.T, home string, r undoRun) {
				if got := readHome(t, home, "ro/f.txt"); got != "now\n" {
					t.Errorf("ro/f.txt = %q", got)
				}
				if got := readHome(t, home, ".gitconfig"); got != "git old\n" {
					t.Errorf(".gitconfig = %q", got)
				}
			}},
	})
}

// TestUndoCtrlC: Ctrl+C at the prompt cancels with 130 and restores nothing.
func TestUndoCtrlC(t *testing.T) {
	core := buildCore(t)
	e := newUndoEnv(t)
	e.seed(t, map[string]string{".gitconfig": "git now\n"}, []backup{{stamp: "20200101-000000",
		files: map[string]string{".gitconfig": "git old\n"}, manifest: "$HOME/.gitconfig\n"}}, nil)
	before := testutil.Snapshot(t, e.f.home)
	// RunPTY answers after a second of silence; a first exec of a new binary
	// can take that long on macOS, so warm it up first.
	_ = exec.Command(core, "version").Run()
	cmd := exec.Command(core, "undo")
	cmd.Env = e.p.Environ()
	out, code := testutil.RunPTY(t, cmd, []string{testutil.CtrlC}, 30*time.Second)
	if code != 130 || strings.Count(out, "Cancelled by user.") != 1 {
		t.Errorf("exit %d, want 130 with one cancel line:\n%s", code, out)
	}
	if d := testutil.DiffSnapshots(before, testutil.Snapshot(t, e.f.home)); d != "" {
		t.Errorf("Ctrl+C changed files:\n%s", d)
	}
}
