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

// ihEnv runs install-hook (and init) in Bash, through a bin/ctx whose lib dir
// is lib, and in Go with CTX_LIB=lib, against the same fixture HOME.
type ihEnv struct {
	f    *fixture
	p    *ptest
	bin  string // bin/ctx to run Bash with
	lib  string // its lib dir (CTX_LIB for Go)
	logs map[string]string
}

// ihInstall is a copy of bin/ctx and lib/ in a temp dir, without the named
// lib files: Bash finds its lib next to the script, so a missing autoswitch
// lib needs a copy. The path has no symlinks, so bin/ctx's `pwd` and CTX_LIB agree.
func ihInstall(t *testing.T, drop ...string) (bin, lib string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// cp is looked up under procMu: a parallel sibling's inProc may have
	// the process PATH set to its fakes right now.
	cp, err := lookPath("cp")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(cp, "-R", filepath.Join(repoRoot, "lib"), filepath.Join(root, "lib")).CombinedOutput()
	if err != nil {
		t.Fatalf("cp lib: %v %s", err, out)
	}
	if out, err := exec.Command(cp, filepath.Join(repoRoot, "bin", "ctx"), filepath.Join(root, "bin", "ctx")).CombinedOutput(); err != nil {
		t.Fatalf("cp bin/ctx: %v %s", err, out)
	}
	for _, d := range drop {
		if err := os.Remove(filepath.Join(root, "lib", d)); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "bin", "ctx"), filepath.Join(root, "lib")
}

func newIhEnv(t *testing.T, mise bool, drop ...string) *ihEnv {
	t.Helper()
	p := newPtest(t)
	e := &ihEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
	if len(drop) > 0 {
		e.bin, e.lib = ihInstall(t, drop...)
	} else {
		e.bin, e.lib = filepath.Join(repoRoot, "bin", "ctx"), filepath.Join(repoRoot, "lib")
	}
	path := ""
	if mise {
		dir, log := testutil.FakeBin(t, "mise", "exit 0")
		path = dir + string(os.PathListSeparator)
		e.logs["mise"] = log
	}
	p.Setenv("PATH", path+testutil.SystemPath(t))
	p.Setenv("CTX_NO_GUM", "1")
	return e
}

// seed resets HOME to files (HOME-relative; "$HOME" in content replaced; a
// name ending in "/" is a directory), then applies tweak.
func (e *ihEnv) seed(t *testing.T, files map[string]string, tweak func(home string)) {
	t.Helper()
	_ = filepath.WalkDir(e.f.home, func(p string, d os.DirEntry, err error) error {
		if err == nil {
			_ = os.Chmod(p, 0o755) // read-only fixtures of a previous seed
		}
		return nil
	})
	if err := os.RemoveAll(e.f.home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, c := range files {
		p := filepath.Join(e.f.home, rel)
		if strings.HasSuffix(rel, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.ReplaceAll(c, "$HOME", e.f.home)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if tweak != nil {
		tweak(e.f.home)
	}
	for _, l := range e.logs {
		_ = os.Remove(l)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(e.f.home, func(p string, d os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
}

func (e *ihEnv) collect(t *testing.T, o, eb string, code int) cvRun {
	argv := map[string][][]string{}
	for tool, l := range e.logs {
		argv[tool] = testutil.ReadArgv(t, l)
	}
	return cvRun{o, eb, code, argv, ihSnapshot(t, e.f.home)}
}

// ihSnapshot is testutil.Snapshot of a HOME that may hold an unreadable
// file: it is made readable for the snapshot (which then shows it with the
// owner read bit) and put back as it was.
func ihSnapshot(t *testing.T, home string) map[string]string {
	t.Helper()
	modes := map[string]os.FileMode{}
	_ = filepath.WalkDir(home, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if st, err := os.Stat(p); err == nil && st.Mode().Perm()&0o400 == 0 {
				modes[p] = st.Mode().Perm()
				_ = os.Chmod(p, st.Mode().Perm()|0o400)
			}
		}
		return nil
	})
	snap := testutil.Snapshot(t, home)
	for p, m := range modes {
		_ = os.Chmod(p, m)
	}
	return snap
}

func (e *ihEnv) bash(t *testing.T, env []string, args ...string) cvRun {
	t.Helper()
	cmd := exec.Command(e.bin, append([]string{"--no-gum"}, args...)...)
	cmd.Env = e.p.Environ(append([]string{"CTX_CORE=off"}, env...)...)
	cmd.Dir = e.f.home
	cmd.Stdin = strings.NewReader("")
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

func (e *ihEnv) goRun(t *testing.T, env []string, fn func(tui.Term) int) cvRun {
	t.Helper()
	var o, eb bytes.Buffer
	code := 0
	e.p.inProc(e.f.home, func() {
		_ = os.Setenv("CTX_LIB", e.lib)
		_ = os.Setenv("CTX_VERSION", "3.2.0")
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			_ = os.Setenv(k, v) // inProc restores the environment
		}
		code = fn(tui.Term{In: strings.NewReader(""), Out: &o, Err: &eb})
	})
	return e.collect(t, o.String(), eb.String(), code)
}

func ihCompare(t *testing.T, want, got cvRun) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Go and Bash differ\n stdout go:   %q\n stdout bash: %q\n stderr go:   %q\n stderr bash: %q\n code go %d bash %d\n argv go %q\n argv bash %q\n tree diff: %s",
			got.stdout, want.stdout, got.stderr, want.stderr, got.code, want.code, got.argv, want.argv, testutil.DiffSnapshots(want.tree, got.tree))
	}
}

const (
	ihRule  = "# ─────────────────────────────────────────────────────────────────────────────\n"
	ihBlock = "# BEGIN ctx profile autoswitch\nold block\n# END ctx profile autoswitch\n"
	// The exact headers of the pastes before 970aa1a (install-hook's and
	// install.sh's); R17 removes a block only under one of these lines.
	ihLegacyHook      = "# ── ctx auto-switch ───────────────────────────────────────────────────────────"
	ihLegacyInstaller = "# ── ctx auto-switch — added by ctx installer ─────────────────────────────────"
	ihPasteBash       = "# ctx profile autoswitch — appended to shell rc by install.sh / ctx install-hook"
)

type ihCase struct {
	name    string
	args    []string
	shell   string
	mise    bool
	drop    []string // lib files missing
	dry     bool
	files   map[string]string
	tweak   func(home string)
	code    int
	vacuous string
	check   func(t *testing.T, home string, r cvRun)
}

// ihStamp is the backup stamp both sides use: install-hook backs up the rc
// and then ~/.ssh/config, and under load the two can land in different
// seconds on one side only, so each side's clock is frozen (Bash through a
// `date` on PATH, Go through ctxconfig.Now).
const ihStamp = "20260102-030405"

// runIhCases must be called from a sequential top-level test: it pins
// ctxconfig.Now until its parallel subtests are done.
func runIhCases(t *testing.T, cases []ihCase) {
	stamp, err := time.ParseInLocation("20060102-150405", ihStamp, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	oldNow := ctxconfig.Now
	ctxconfig.Now = func() time.Time { return stamp }
	t.Cleanup(func() { ctxconfig.Now = oldNow })
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newIhEnv(t, c.mise, c.drop...)
			var dateDir string
			locked(func() { dateDir = testutil.FixedStampDate(t, ihStamp) })
			env := []string{"SHELL=" + c.shell}
			if c.dry {
				env = append(env, "CTX_DRY_RUN=1")
			}
			args := append([]string{}, c.args...)
			for i := range args {
				args[i] = strings.ReplaceAll(args[i], "$HOME", e.f.home)
			}
			e.seed(t, c.files, c.tweak)
			before := ihSnapshot(t, e.f.home)
			bargs := append([]string{"install-hook"}, args...)
			if c.dry {
				bargs = append([]string{"--dry-run"}, bargs...)
			}
			want := e.bash(t, append(env, "PATH="+dateDir+string(os.PathListSeparator)+e.p.Getenv("PATH")), bargs...)
			e.seed(t, c.files, c.tweak)
			got := e.goRun(t, env, func(tm tui.Term) int { return InstallHook(tm, args) })
			ihCompare(t, want, got)
			if want.code != c.code {
				t.Errorf("bash exit %d, want %d\n%s%s", want.code, c.code, want.stdout, want.stderr)
			}
			if !strings.Contains(want.stdout+want.stderr, strings.ReplaceAll(c.vacuous, "$HOME", e.f.home)) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
			if c.code != 0 || c.dry {
				if d := testutil.DiffSnapshots(before, got.tree); d != "" {
					t.Errorf("refusal or dry-run changed HOME:\n%s", d)
				}
			}
			if c.check != nil {
				c.check(t, e.f.home, got)
			}
		})
	}
}

func readHomeFile(t *testing.T, home, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallHookMatchesBash(t *testing.T) {
	sshOK := map[string]string{".ssh/config": "Include $HOME/.ssh/ctx_config\n", ".ssh/ctx_config": ""}
	with := func(m map[string]string, kv ...string) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			out[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}
	runIhCases(t, []ihCase{
		{name: "zsh default, mise", shell: "/bin/zsh", mise: true, files: with(nil, ".zshrc", "export A=1\n"),
			vacuous: "mise activation added to $HOME/.zshrc", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".zshrc"); !strings.Contains(s, "eval \"$(mise activate zsh)\"\n") {
					t.Errorf(".zshrc:\n%s", s)
				}
			}},
		{name: "bash default, no mise, new rc", shell: "/usr/local/bin/bash", vacuous: "ctx profile autoswitch hook refreshed in $HOME/.bashrc (sources lib)"},
		{name: "fish default, mise", shell: "/opt/homebrew/bin/fish", mise: true, files: with(nil, ".config/fish/config.fish", "set -x A 1\n"),
			vacuous: "mise activation added", check: func(t *testing.T, home string, r cvRun) {
				s := readHomeFile(t, home, ".config/fish/config.fish")
				if !strings.Contains(s, "mise activate fish | source\n") || !strings.Contains(s, "ctx_autoswitch.fish") {
					t.Errorf("config.fish:\n%s", s)
				}
			}},
		{name: "fish default, no fish dir", shell: "/bin/fish", code: 1, vacuous: "Cannot write $HOME/.config/fish/config.fish"},
		{name: "explicit zsh rc, bash shell", args: []string{"$HOME/my.zshrc"}, shell: "/bin/bash", mise: true,
			vacuous: "refreshed in $HOME/my.zshrc", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, "my.zshrc"); !strings.Contains(s, "mise activate zsh") {
					t.Errorf("my.zshrc:\n%s", s)
				}
			}},
		{name: "explicit other rc gets bash", args: []string{"$HOME/.profile"}, shell: "/bin/zsh", mise: true, files: with(sshOK, ".profile", "x\n"),
			vacuous: "refreshed in $HOME/.profile", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".profile"); !strings.Contains(s, "mise activate bash") {
					t.Errorf(".profile:\n%s", s)
				}
			}},
		{name: "relative config.fish is fish", args: []string{"config.fish"}, shell: "/bin/zsh", mise: true, vacuous: "refreshed in config.fish"},
		{name: "rerun is idempotent", shell: "/bin/zsh", mise: true,
			files:   with(sshOK, ".zshrc", "x\n\n# mise-en-place — added by ctx\neval \"$(mise activate zsh)\"\n\n"+ihBlock),
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				s := readHomeFile(t, home, ".zshrc")
				if strings.Count(s, "mise activate") != 1 || strings.Count(s, "# BEGIN ctx profile autoswitch") != 1 || strings.Contains(s, "old block") {
					t.Errorf(".zshrc:\n%s", s)
				}
			}},
		{name: "block in the middle", shell: "/bin/zsh", files: with(sshOK, ".zshrc", "a\n"+ihBlock+"b\n"), vacuous: "refreshed"},
		{name: "two blocks and a lone END", shell: "/bin/zsh", files: with(sshOK, ".zshrc", ihBlock+"m\n# END ctx profile autoswitch\n"+ihBlock), vacuous: "refreshed"},
		{name: "legacy paste with closing rule", shell: "/bin/zsh",
			files:   with(sshOK, ".zshrc", "top\n"+ihLegacyHook+"\n_ctx_auto_switch() { :; }\nmore() { :; }\n"+ihRule+"after=1\n"),
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				s := readHomeFile(t, home, ".zshrc")
				if !strings.HasPrefix(s, "top\nafter=1\n\n# BEGIN") {
					t.Errorf(".zshrc:\n%s", s)
				}
			}},
		{name: "legacy block ends at blank line", shell: "/bin/bash",
			files: with(sshOK, ".bashrc", "top\n"+ihPasteBash+"\n# more\n\n_f() { :; }\n"), vacuous: "refreshed",
			check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".bashrc"); !strings.HasPrefix(s, "top\n\n_f() { :; }\n\n# BEGIN") {
					t.Errorf(".bashrc:\n%s", s)
				}
			}},
		{name: "blank line before the rule ends the block", shell: "/bin/zsh", files: with(sshOK, ".zshrc", ihLegacyInstaller+"\nx\n\ny\n"+ihRule+"z\n"), vacuous: "refreshed",
			check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, "\ny\n"+ihRule+"z\n\n# BEGIN") {
					t.Errorf(".zshrc:\n%s", s)
				}
			}},
		{name: "legacy block to end of file", shell: "/bin/bash", files: with(sshOK, ".bashrc", "top\n"+ihPasteBash+"\nx\n"), vacuous: "refreshed"},
		{name: "short rule does not end a legacy block", shell: "/bin/bash", files: with(sshOK, ".bashrc", ihPasteBash+"\n# ────\nx\n\nkeep\n"), vacuous: "refreshed"},
		{name: "BEGIN inside a legacy block", shell: "/bin/bash", files: with(sshOK, ".bashrc", ihPasteBash+"\n"+ihBlock+"gone\n\nkeep\n"), vacuous: "refreshed"},
		{name: "no trailing newline", shell: "/bin/zsh", files: with(sshOK, ".zshrc", "a\nlast"), vacuous: "refreshed"},
		{name: "empty rc", shell: "/bin/zsh", files: with(sshOK, ".zshrc", ""), vacuous: "refreshed"},
		{name: "unterminated BEGIN", shell: "/bin/zsh", files: with(sshOK, ".zshrc", "a\n# BEGIN ctx profile autoswitch\nb\n"), code: 1,
			vacuous: "Unterminated ctx block in $HOME/.zshrc; add the line '# END ctx profile autoswitch' or remove the '# BEGIN ctx profile autoswitch' line"},
		{name: "unknown shell", shell: "/bin/tcsh", code: 1, vacuous: "Unknown shell. Pass rc file: ctx install-hook ~/.zshrc"},
		{name: "empty SHELL", shell: "", code: 1, vacuous: "Unknown shell."},
		{name: "rc is a directory", args: []string{"$HOME/d"}, shell: "/bin/zsh", files: map[string]string{"d/": ""}, code: 1, vacuous: "$HOME/d is a directory"},
		{name: "unreadable rc", shell: "/bin/zsh", files: with(nil, ".zshrc", "secret\n"), code: 1, vacuous: "Cannot read $HOME/.zshrc",
			tweak: func(home string) { _ = os.Chmod(filepath.Join(home, ".zshrc"), 0o200) }},
		{name: "missing parent dir", args: []string{"$HOME/no/rc"}, shell: "/bin/zsh", code: 1, vacuous: "Cannot write $HOME/no/rc"},
		{name: "missing bash lib", shell: "/bin/zsh", mise: true, drop: []string{"ctx_autoswitch.bash"}, files: with(nil, ".zshrc", "keep\n"), code: 1,
			vacuous: "/lib/ctx_autoswitch.bash — reinstall ctx."},
		{name: "missing fish lib", shell: "/bin/fish", drop: []string{"ctx_autoswitch.fish"}, files: map[string]string{".config/fish/": ""}, code: 1,
			vacuous: "/lib/ctx_autoswitch.fish — reinstall ctx."},
		{name: "fish lib missing does not matter for zsh", shell: "/bin/zsh", drop: []string{"ctx_autoswitch.fish"}, vacuous: "refreshed"},
		{name: "ssh config gets the Include", shell: "/bin/zsh", files: map[string]string{".ssh/config": "Host x\n"}, vacuous: "SSH config isolated to $HOME/.ssh/ctx_config"},
		{name: "dry run, existing rc", shell: "/bin/zsh", mise: true, dry: true, files: with(nil, ".zshrc", "x\n"),
			vacuous: "[dry-run] would add Include $HOME/.ssh/ctx_config to $HOME/.ssh/config"},
		{name: "dry run, new rc, include present", shell: "/bin/bash", dry: true, files: sshOK,
			vacuous: "[dry-run] would refresh the ctx profile autoswitch hook in $HOME/.bashrc"},
		{name: "dry run still refuses", shell: "/bin/zsh", dry: true, files: with(nil, ".zshrc", "# BEGIN ctx profile autoswitch\n"), code: 1, vacuous: "Unterminated ctx block"},
	})
}

// TestInstallHookRcEdge (Review Focus 2): a symlinked rc, a read-only rc, CRLF
// lines and a legacy block without an END marker. The link and mode stay and
// what follows the block is kept, in Bash and Go alike.
func TestInstallHookRcEdge(t *testing.T) {
	link := func(home string) {
		_ = os.Remove(filepath.Join(home, ".zshrc"))
		if err := os.Symlink("dots/zshrc", filepath.Join(home, ".zshrc")); err != nil {
			panic(err)
		}
		_ = os.Chmod(filepath.Join(home, "dots/zshrc"), 0o640)
	}
	keepsLink := func(t *testing.T, home string, r cvRun) {
		if dest, err := os.Readlink(filepath.Join(home, ".zshrc")); err != nil || dest != "dots/zshrc" {
			t.Errorf("link: %q %v", dest, err)
		}
		if st, err := os.Stat(filepath.Join(home, "dots/zshrc")); err != nil || st.Mode().Perm() != 0o640 {
			t.Errorf("mode: %v %v", st, err)
		}
	}
	runIhCases(t, []ihCase{
		{name: "symlinked rc", shell: "/bin/zsh", mise: true, files: map[string]string{"dots/zshrc": "a\n" + ihBlock + "b\n"}, tweak: link,
			vacuous: "refreshed in $HOME/.zshrc", check: func(t *testing.T, home string, r cvRun) {
				keepsLink(t, home, r)
				if s := readHomeFile(t, home, "dots/zshrc"); !strings.HasPrefix(s, "a\nb\n\n# mise-en-place") || strings.Contains(s, "old block") {
					t.Errorf("target:\n%s", s)
				}
			}},
		{name: "dangling symlink creates the target", shell: "/bin/zsh", files: map[string]string{"dots/": ""},
			tweak: func(home string) { _ = os.Symlink("dots/zshrc", filepath.Join(home, ".zshrc")) }, vacuous: "refreshed",
			check: func(t *testing.T, home string, r cvRun) {
				if st, err := os.Lstat(filepath.Join(home, ".zshrc")); err != nil || st.Mode()&os.ModeSymlink == 0 {
					t.Errorf("link replaced: %v", err)
				}
			}},
		{name: "read-only rc", shell: "/bin/zsh", files: map[string]string{".zshrc": "ro\n"}, code: 1, vacuous: "Cannot write $HOME/.zshrc",
			tweak: func(home string) { _ = os.Chmod(filepath.Join(home, ".zshrc"), 0o444) }},
		{name: "read-only rc through a link", shell: "/bin/zsh", files: map[string]string{"dots/zshrc": "ro\n"}, code: 1, vacuous: "Cannot write $HOME/.zshrc",
			tweak: func(home string) { link(home); _ = os.Chmod(filepath.Join(home, "dots/zshrc"), 0o444) }},
		{name: "symlink loop rc is refused, links kept", shell: "/bin/zsh", code: 1, vacuous: "Cannot write $HOME/.zshrc",
			tweak: func(home string) {
				_ = os.Symlink("loop", filepath.Join(home, ".zshrc"))
				_ = os.Symlink(".zshrc", filepath.Join(home, "loop"))
			}},
		{name: "mode kept", shell: "/bin/zsh", files: map[string]string{".zshrc": "x\n"}, vacuous: "refreshed",
			tweak: func(home string) { _ = os.Chmod(filepath.Join(home, ".zshrc"), 0o600) },
			check: func(t *testing.T, home string, r cvRun) {
				if st, _ := os.Stat(filepath.Join(home, ".zshrc")); st.Mode().Perm() != 0o600 {
					t.Errorf("mode %v", st.Mode())
				}
			}},
		{name: "CRLF lines kept", shell: "/bin/zsh", files: map[string]string{".zshrc": "one\r\n# BEGIN ctx profile autoswitch\r\nold\r\n# END ctx profile autoswitch\r\ntwo\r\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				s := readHomeFile(t, home, ".zshrc")
				if !strings.HasPrefix(s, "one\r\ntwo\r\n\n# BEGIN ctx profile autoswitch\n") || strings.Contains(s, "old") {
					t.Errorf(".zshrc %q", s)
				}
			}},
		{name: "CRLF legacy block ends at CRLF rule", shell: "/bin/zsh",
			files:   map[string]string{".zshrc": ihLegacyHook + "\r\nx\r\n" + strings.Replace(ihRule, "\n", "\r\n", 1) + "after\r\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, "after\r\n\n# BEGIN") {
					t.Errorf(".zshrc %q", s)
				}
			}},
		{name: "legacy block without END keeps what follows, link and mode", shell: "/bin/zsh",
			files: map[string]string{"dots/zshrc": "head\n" + ihPasteBash + "\n_old() { :; }\n\nexport KEEP=1\nalias k=ls\n"}, tweak: link,
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				keepsLink(t, home, r)
				s := readHomeFile(t, home, "dots/zshrc")
				if !strings.HasPrefix(s, "head\n\nexport KEEP=1\nalias k=ls\n\n# BEGIN") {
					t.Errorf("target:\n%s", s)
				}
			}},
	})
}

// TestInstallHookNeedsCTXLib: without CTX_LIB (ctx-core not run by bin/ctx)
// Go refuses instead of guessing the lib dir, and changes nothing.
func TestInstallHookNeedsCTXLib(t *testing.T) {
	e := newIhEnv(t, false)
	e.seed(t, map[string]string{".zshrc": "x\n"}, nil)
	before := testutil.Snapshot(t, e.f.home)
	got := e.goRun(t, []string{"SHELL=/bin/zsh"}, func(tm tui.Term) int {
		_ = os.Unsetenv("CTX_LIB")
		return InstallHook(tm, nil)
	})
	if got.code != 1 || !strings.Contains(got.stderr, "CTX_LIB is not set") {
		t.Errorf("exit %d stderr %q", got.code, got.stderr)
	}
	if d := testutil.DiffSnapshots(before, got.tree); d != "" {
		t.Errorf("changed HOME:\n%s", d)
	}
}

// TestInstallHookWriteFailure: a failed write leaves the rc as it was and exits 1.
func TestInstallHookWriteFailure(t *testing.T) {
	e := newIhEnv(t, false)
	e.seed(t, map[string]string{".zshrc": "mine\n"}, nil)
	got := e.goRun(t, []string{"SHELL=/bin/zsh"}, func(tm tui.Term) int {
		old := writeTemp
		defer func() { writeTemp = old }()
		writeTemp = func(f *os.File, data []byte) error { _, _ = f.Write(data[:3]); return os.ErrInvalid }
		return InstallHook(tm, nil)
	})
	if got.code != 1 || !strings.Contains(got.stderr, "Cannot write "+e.f.home+"/.zshrc") {
		t.Errorf("exit %d stderr %q", got.code, got.stderr)
	}
	if s := readHomeFile(t, e.f.home, ".zshrc"); s != "mine\n" {
		t.Errorf(".zshrc = %q", s)
	}
	ents, _ := os.ReadDir(e.f.home)
	for _, en := range ents {
		if strings.HasPrefix(en.Name(), ".zshrc.") {
			t.Errorf("temp file left: %s", en.Name())
		}
	}
}

// pasteRevs are the commits whose lib/ctx_autoswitch.bash and .fish install-hook
// appended whole to the rc (970aa1a..a65d9a9; b15588c switched to sourcing).
// testdata/autoswitch-paste holds them as `git show <rev>:lib/ctx_autoswitch.*`
// printed them (CI checkouts are shallow).
var pasteRevs = []string{"970aa1a", "295f87f", "3b6c98b", "a4de152", "2ae9b68", "a65d9a9"}

// TestInstallHookRemovesHistoricPastes (R19): each historic full paste, with
// user lines before and after it, goes completely; the user lines stay; Go
// matches Bash.
func TestInstallHookRemovesHistoricPastes(t *testing.T) {
	var cases []ihCase
	for _, rev := range pasteRevs {
		for _, kind := range []string{"bash", "fish"} {
			paste, err := os.ReadFile(filepath.Join("testdata", "autoswitch-paste", rev+"."+kind))
			if err != nil {
				t.Fatal(err)
			}
			rc, shell := ".zshrc", "/bin/zsh"
			if kind == "fish" {
				rc, shell = ".config/fish/config.fish", "/bin/fish"
			}
			kept := "user before\n\n\nuser after\nalias x=y\n"
			body := "user before\n\n" + string(paste) + "\nuser after\nalias x=y\n"
			rcRel := rc
			cases = append(cases, ihCase{name: rev + " " + kind, shell: shell, files: map[string]string{rc: body}, vacuous: "refreshed",
				check: func(t *testing.T, home string, r cvRun) {
					s := readHomeFile(t, home, rcRel)
					if !strings.HasPrefix(s, kept+"\n# BEGIN ctx profile autoswitch\n") {
						t.Errorf("paste not removed cleanly:\n%s", s)
					}
				}})
		}
	}
	paste970, err := os.ReadFile(filepath.Join("testdata", "autoswitch-paste", "970aa1a.bash"))
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases,
		ihCase{name: "paste header without its end line keeps R17's rule", shell: "/bin/zsh",
			files:   map[string]string{".zshrc": pasteHeaderBash + "\n# Longest\n\nkeep() { :; }\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, "\nkeep() { :; }\n\n# BEGIN") {
					t.Errorf(".zshrc:\n%s", s)
				}
			}},
		ihCase{name: "CRLF paste", shell: "/bin/zsh",
			files:   map[string]string{".zshrc": "u\r\n" + pasteHeaderBash + "\r\nbody\r\n\r\nif x; then\r\n  PROMPT_COMMAND=\"_ctx_profile_autoswitch${PROMPT_COMMAND:+;$PROMPT_COMMAND}\"\r\n  _ctx_profile_autoswitch\r\nfi\r\nv\r\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, "u\r\nv\r\n\n# BEGIN") {
					t.Errorf(".zshrc %q", s)
				}
			}},
		// A user's own `fi` (or fish `_ctx_profile_autoswitch`) below a
		// paste that is not a full one must not be taken for the paste's end.
		ihCase{name: "a user fi below a partial paste keeps the user lines", shell: "/bin/zsh",
			files: map[string]string{".zshrc": "export A=1\n" + ihPasteBash + "\n# Longest WORK_DIR prefix wins\n_ctx_profile_autoswitch() { :; }\nPROMPT_COMMAND=x\n\n" +
				"export MYVAR=keep\nalias ll='ls -l'\nif [ -f ~/.aliases ]; then\n  . ~/.aliases\nfi\nexport AFTER=1\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				want := "export A=1\n\nexport MYVAR=keep\nalias ll='ls -l'\nif [ -f ~/.aliases ]; then\n  . ~/.aliases\nfi\nexport AFTER=1\n\n# BEGIN"
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, want) {
					t.Errorf("user lines lost:\n%s", s)
				}
			}},
		// Reviewer's repro: a header left behind with its body deleted, and a
		// user block that ends like a paste ("  _ctx_profile_autoswitch", "fi")
		// but without the historic PROMPT_COMMAND line before it.
		ihCase{name: "a leftover header above a user block ending like a paste keeps the user lines", shell: "/bin/zsh",
			files:   map[string]string{".zshrc": "export A=1\n" + ihPasteBash + "\n\nexport MYVAR=keep\nif [ -n \"$X\" ]; then\n  _ctx_profile_autoswitch\nfi\nexport AFTER=1\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				want := "export A=1\n\nexport MYVAR=keep\nif [ -n \"$X\" ]; then\n  _ctx_profile_autoswitch\nfi\nexport AFTER=1\n\n# BEGIN"
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, want) {
					t.Errorf("user lines lost:\n%s", s)
				}
			}},
		ihCase{name: "a user _ctx_profile_autoswitch below a partial fish paste keeps the user lines", shell: "/bin/fish",
			files: map[string]string{".config/fish/config.fish": "set -x A 1\n" + "# ctx profile autoswitch — fish (see lib/ctx_autoswitch.bash for behavior)" +
				"\nfunction _ctx_profile_autoswitch\nend\n\nset -x MYVAR keep\n_ctx_profile_autoswitch\nset -x AFTER 1\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				want := "set -x A 1\n\nset -x MYVAR keep\n_ctx_profile_autoswitch\nset -x AFTER 1\n\n# BEGIN"
				if s := readHomeFile(t, home, ".config/fish/config.fish"); !strings.HasPrefix(s, want) {
					t.Errorf("user lines lost:\n%s", s)
				}
			}},
		ihCase{name: "a full paste followed by a user if block", shell: "/bin/zsh",
			files:   map[string]string{".zshrc": "u\n" + string(paste970) + "if true; then\n  :\nfi\nz\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, "u\nif true; then\n  :\nfi\nz\n\n# BEGIN") {
					t.Errorf(".zshrc:\n%s", s)
				}
			}},
		ihCase{name: "a comment that only starts like a legacy header is the user's", shell: "/bin/zsh",
			files:   map[string]string{".zshrc": "# ctx profile autoswitch notes\nmine=1\n# ── ctx auto-switch ────\nmine=2\n"},
			vacuous: "refreshed", check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".zshrc"); !strings.HasPrefix(s, "# ctx profile autoswitch notes\nmine=1\n# ── ctx auto-switch ────\nmine=2\n\n# BEGIN") {
					t.Errorf(".zshrc:\n%s", s)
				}
			}},
		ihCase{name: "BEGIN inside an open BEGIN block is refused", shell: "/bin/zsh",
			files: map[string]string{".zshrc": "a\n" + autoswitchBegin + "\nx\n" + autoswitchBegin + "\ny\n" + autoswitchEnd + "\nz\n"}, code: 1,
			vacuous: "Unterminated ctx block in $HOME/.zshrc"},
	)
	runIhCases(t, cases)
}

// TestInstallHookSSHConfig (R20): the Include goes into ~/.ssh/config through
// a symlink, a private mode is kept, a group/world-writable one becomes 600
// with a warning, a new file is 600, an unwritable one is left alone. Go == Bash.
func TestInstallHookSSHConfig(t *testing.T) {
	rc := map[string]string{".zshrc": "x\n"}
	with := func(kv ...string) map[string]string {
		m := map[string]string{".zshrc": "x\n"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	mode := func(rel string, want os.FileMode) func(t *testing.T, home string, r cvRun) {
		return func(t *testing.T, home string, r cvRun) {
			st, err := os.Stat(filepath.Join(home, rel))
			if err != nil || st.Mode().Perm() != want {
				t.Errorf("%s mode %v (%v), want %v", rel, st.Mode().Perm(), err, want)
			}
			if s := readHomeFile(t, home, rel); !strings.HasPrefix(s, "# ctx managed hosts — added by ctx installer\nInclude "+home+"/.ssh/ctx_config\n\n") {
				t.Errorf("%s:\n%s", rel, s)
			}
		}
	}
	runIhCases(t, []ihCase{
		{name: "symlinked ssh config", shell: "/bin/zsh", files: with("dots/ssh_config", "Host mine\n"),
			tweak: func(home string) {
				_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
				_ = os.Symlink("../dots/ssh_config", filepath.Join(home, ".ssh", "config"))
			}, vacuous: "SSH config isolated",
			check: func(t *testing.T, home string, r cvRun) {
				if dest, err := os.Readlink(filepath.Join(home, ".ssh", "config")); err != nil || dest != "../dots/ssh_config" {
					t.Errorf("link replaced: %q %v", dest, err)
				}
				mode("dots/ssh_config", 0o644)(t, home, r)
				if s := readHomeFile(t, home, "dots/ssh_config"); !strings.HasSuffix(s, "\n\nHost mine\n") {
					t.Errorf("target:\n%s", s)
				}
			}},
		{name: "ssh config 644 kept", shell: "/bin/zsh", files: with(".ssh/config", "Host a\n"), vacuous: "SSH config isolated", check: mode(".ssh/config", 0o644)},
		{name: "ssh config 600 kept", shell: "/bin/zsh", files: with(".ssh/config", "Host a\n"),
			tweak: func(home string) { _ = os.Chmod(filepath.Join(home, ".ssh/config"), 0o600) }, vacuous: "SSH config isolated", check: mode(".ssh/config", 0o600)},
		{name: "ssh config 666 tightened", shell: "/bin/zsh", files: with(".ssh/config", "Host a\n"),
			tweak:   func(home string) { _ = os.Chmod(filepath.Join(home, ".ssh/config"), 0o666) },
			vacuous: "/.ssh/config was writable by group or others; it is now 600 (ssh refuses such a file)", check: mode(".ssh/config", 0o600)},
		{name: "ssh config 664 tightened", shell: "/bin/zsh", files: with(".ssh/config", "Host a\n"),
			tweak:   func(home string) { _ = os.Chmod(filepath.Join(home, ".ssh/config"), 0o664) },
			vacuous: "it is now 600", check: mode(".ssh/config", 0o600)},
		{name: "new ssh config is 600", shell: "/bin/zsh", files: rc, vacuous: "SSH config isolated", check: mode(".ssh/config", 0o600)},
		{name: "read-only ssh config left alone", shell: "/bin/zsh", files: with(".ssh/config", "Host a\n"),
			tweak:   func(home string) { _ = os.Chmod(filepath.Join(home, ".ssh/config"), 0o400) },
			vacuous: "Could not add Include $HOME/.ssh/ctx_config to $HOME/.ssh/config",
			check: func(t *testing.T, home string, r cvRun) {
				if s := readHomeFile(t, home, ".ssh/config"); s != "Host a\n" {
					t.Errorf(".ssh/config = %q", s)
				}
				if strings.Contains(r.stdout+r.stderr, "SSH config isolated") {
					t.Errorf("success claimed after a failed Include:\n%s%s", r.stdout, r.stderr)
				}
				if !strings.Contains(r.stdout, "Reload your shell") {
					t.Errorf("rc step output lost:\n%s", r.stdout)
				}
			}},
	})
}
