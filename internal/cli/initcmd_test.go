package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// fakeInstaller stands in for install.sh next to the lib dir: it records its
// argv (as FakeBin does) and says it ran. Never the real installer.
const fakeInstaller = `#!/bin/sh
for a in "$@"; do printf '%s\000' "$a" >> "$0.argv"; done
printf '\n' >> "$0.argv"
echo "fake installer ran"
`

type initCase struct {
	name      string
	tools     []string // fakes on PATH among mise, gum, gh
	brew      string   // "" none, else the fake brew's exit code
	installer bool     // install.sh next to the lib dir
	shell     string
	files     map[string]string
	answers   []string
	dry       bool
	code      int
	vacuous   string
}

func TestInitMatchesBash(t *testing.T) {
	core := buildCore(t)
	_ = exec.Command(core, "version").Run() // warm up: RunPTY answers after a quiet gap
	hooked := map[string]string{".zshrc": "eval \"$(mise activate zsh)\"\n# BEGIN ctx profile autoswitch\nx\n# END ctx profile autoswitch\n"}
	all := []string{"mise", "gum", "gh"}
	cases := []initCase{
		{name: "all there, hooks installed", tools: all, shell: "/bin/zsh", files: hooked, vacuous: "Shell hooks already installed"},
		{name: "tools missing, installer yes, hooks yes", tools: []string{"mise"}, installer: true, shell: "/bin/zsh",
			answers: []string{"y", "y"}, vacuous: "fake installer ran"},
		{name: "tools missing, no to both", installer: true, shell: "/bin/bash", answers: []string{"n", "n"}, vacuous: "Skipped. Run: ctx install-hook"},
		{name: "default answers", tools: []string{"gum", "gh"}, installer: true, shell: "/bin/zsh", answers: []string{"", ""}, vacuous: "fake installer ran"},
		{name: "yes is not y", tools: []string{"gum"}, installer: true, shell: "/bin/zsh", answers: []string{"yes", "yes"}, vacuous: "Skipped."},
		{name: "no installer, brew installs", tools: []string{"mise"}, brew: "0", shell: "/bin/zsh", files: hooked, answers: []string{"y"}, vacuous: "gum installed"},
		{name: "no installer, brew fails", brew: "1", shell: "/bin/zsh", files: hooked, answers: []string{"y"}, vacuous: "gh: install manually"},
		{name: "no installer, no brew", tools: []string{"mise", "gum"}, shell: "/bin/zsh", files: hooked, answers: []string{"y"}, vacuous: "gh: install manually"},
		{name: "mise there, autoswitch missing", tools: all, shell: "/bin/zsh", files: map[string]string{".zshrc": "eval \"$(mise activate zsh)\"\n"},
			answers: []string{"y"}, vacuous: "ctx profile autoswitch hook refreshed"},
		{name: "legacy autoswitch counts", tools: all, shell: "/bin/zsh", files: map[string]string{".zshrc": "mise activate\n_ctx_profile_autoswitch\n"}, vacuous: "Shell hooks already installed"},
		{name: "ctx activate counts", tools: all, shell: "/bin/zsh", files: map[string]string{".zshrc": "mise activate\neval \"$(ctx activate zsh)\"\n"}, vacuous: "Shell hooks already installed"},
		{name: "lib source line counts", tools: all, shell: "/bin/bash", files: map[string]string{".bashrc": "mise activate\nsource /opt/ctx/lib/ctx_autoswitch.bash\n"}, vacuous: "Shell hooks already installed"},
		{name: "fish without its dir: install-hook dies", tools: all, shell: "/usr/bin/fish", answers: []string{"y"}, code: 1, vacuous: "Cannot write"},
		{name: "unknown shell skips hooks", tools: all, shell: "/bin/tcsh", vacuous: "Next steps:"},
		{name: "unterminated block: install-hook refuses", tools: all, shell: "/bin/zsh", files: map[string]string{".zshrc": "# BEGIN ctx profile autoswitch\n"},
			answers: []string{"y"}, code: 1, vacuous: "Unterminated ctx block"},
		{name: "EOF at the tools prompt", installer: true, shell: "/bin/zsh", answers: []string{testutil.EOF}, code: 130, vacuous: "Cancelled by user."},
		{name: "EOF at the hooks prompt", tools: all, shell: "/bin/zsh", answers: []string{testutil.EOF}, code: 130, vacuous: "Cancelled by user."},
		{name: "dry run, installer", installer: true, shell: "/bin/zsh", dry: true, vacuous: "[dry-run] would run bash "},
		{name: "dry run, brew", tools: []string{"mise"}, brew: "0", shell: "/bin/bash", dry: true, vacuous: "[dry-run] would run brew install gum"},
		{name: "dry run, no brew", tools: []string{"gh"}, shell: "/bin/zsh", files: hooked, dry: true, vacuous: "mise: install manually"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newPtest(t)
			e := &ihEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
			e.bin, e.lib = ihInstall(t)
			if e.lib == filepath.Join(repoRoot, "lib") {
				t.Fatal("init must not run with the repo's lib: its install.sh is real")
			}
			root := filepath.Dir(e.lib)
			if c.installer {
				testutil.Script(t, filepath.Join(root, "install.sh"), fakeInstaller)
				e.logs["install.sh"] = filepath.Join(root, "install.sh.argv")
			}
			path := ""
			for _, tool := range c.tools {
				dir, log := testutil.FakeBin(t, tool, "exit 0")
				path += dir + string(os.PathListSeparator)
				e.logs[tool] = log
			}
			if c.brew != "" {
				dir, log := testutil.FakeBin(t, "brew", "echo brew-noise; exit "+c.brew)
				path += dir + string(os.PathListSeparator)
				e.logs["brew"] = log
			}
			p.Setenv("PATH", path+testutil.SystemPath(t))
			p.Setenv("SHELL", c.shell)
			p.Setenv("CTX_CORE", "")
			pre := []string{"--no-gum"}
			if c.dry {
				pre = append(pre, "--dry-run")
			}
			run := func(env ...string) (string, int, map[string][][]string, map[string]string) {
				e.seed(t, c.files, nil)
				for _, l := range e.logs {
					_ = os.Remove(l)
				}
				cmd := exec.Command(e.bin, append(pre, "init")...)
				cmd.Env = e.p.Environ(env...)
				cmd.Dir = e.f.home
				out, code := testutil.RunPTY(t, cmd, c.answers, 60*time.Second)
				argv := map[string][][]string{}
				for tool, l := range e.logs {
					argv[tool] = testutil.ReadArgv(t, l)
				}
				return strings.ReplaceAll(out, e.lib, "<LIB>"), code, argv, ihSnapshot(t, e.f.home)
			}
			// The Go run goes through a wrapper that logs ctx-core's calls, to
			// prove bin/ctx handed init over.
			calls := filepath.Join(t.TempDir(), "calls")
			wrapper := filepath.Join(t.TempDir(), "ctx-core")
			if err := os.WriteFile(wrapper, []byte("#!/bin/sh\necho \"$*\" >>'"+calls+"'\nexec '"+core+"' \"$@\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			wantOut, wantCode, wantArgv, wantTree := run("CTX_CORE=off")
			gotOut, gotCode, gotArgv, gotTree := run("CTX_CORE_BIN=" + wrapper)
			if b, _ := os.ReadFile(calls); string(b) != "implements init\ninit\n" {
				t.Errorf("ctx-core calls %q, want implements init + init", b)
			}
			if gotOut != wantOut || gotCode != wantCode {
				t.Errorf("Go and Bash differ\n go   (%d): %q\n bash (%d): %q", gotCode, gotOut, wantCode, wantOut)
			}
			if !reflect.DeepEqual(gotArgv, wantArgv) {
				t.Errorf("argv go %q bash %q", gotArgv, wantArgv)
			}
			if d := testutil.DiffSnapshots(wantTree, gotTree); d != "" {
				t.Errorf("trees differ:\n%s", d)
			}
			if wantCode != c.code {
				t.Errorf("bash exit %d, want %d:\n%s", wantCode, c.code, wantOut)
			}
			if !strings.Contains(wantOut, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q:\n%s", c.vacuous, wantOut)
			}
			if c.dry {
				if strings.Contains(wantOut, "[Y/n]") {
					t.Errorf("dry run asked:\n%s", wantOut)
				}
				for tool, a := range wantArgv {
					if len(a) > 0 {
						t.Errorf("dry run ran %s: %q", tool, a)
					}
				}
				if _, err := os.Stat(filepath.Join(e.f.home, ".ctx")); err == nil {
					t.Error("dry run created ~/.ctx")
				}
			}
		})
	}
}

// TestInitCtrlC: Ctrl+C at a prompt cancels with one cancel line and 130.
func TestInitCtrlC(t *testing.T) {
	core := buildCore(t)
	_ = exec.Command(core, "version").Run()
	p := newPtest(t)
	e := &ihEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
	e.bin, e.lib = ihInstall(t)
	p.Setenv("PATH", testutil.SystemPath(t))
	p.Setenv("SHELL", "/bin/zsh")
	e.seed(t, nil, nil)
	cmd := exec.Command(core, "init")
	cmd.Env = e.p.Environ("CTX_LIB="+e.lib, "CTX_NO_GUM=1")
	out, code := testutil.RunPTY(t, cmd, []string{testutil.CtrlC}, 30*time.Second)
	if code != 130 || strings.Count(out, "Cancelled by user.") != 1 {
		t.Errorf("exit %d, want 130 with one cancel line:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.f.home, ".zshrc")); err == nil {
		t.Error("Ctrl+C still wrote .zshrc")
	}
}

// TestInitNeedsCTXLib: Go refuses without CTX_LIB instead of guessing where
// install.sh and the autoswitch lib are.
func TestInitNeedsCTXLib(t *testing.T) {
	core := buildCore(t)
	p := newPtest(t)
	f := newPFixture(p)
	cmd := exec.Command(core, "init")
	cmd.Env = p.Environ("CTX_LIB=", "PATH="+testutil.SystemPath(t), "SHELL=/bin/zsh")
	cmd.Dir = f.home
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 || !strings.Contains(string(out), "CTX_LIB is not set") {
		t.Errorf("err %v out %q", err, out)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".ctx")); err == nil {
		t.Error("init created ~/.ctx without CTX_LIB")
	}
}
