package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// doctorTools are the tools doctor probes, in its order.
var doctorTools = []string{"git", "mise", "gum", "gh", "aws", "az", "gcloud", "kubectl"}

// fakeDoctorTool prints two lines (stdout and stderr) naming itself and its
// arguments, so a changed probe command shows in the output. GUARD_HOOKS is
// what `git config --global --get core.hooksPath` answers.
const fakeDoctorTool = `case "$*" in *core.hooksPath*) [ -n "$GUARD_HOOKS" ] && echo "$GUARD_HOOKS"; exit 0;; esac
[ -n "$QUIET_TOOL" ] && [ "$QUIET_TOOL" = "$(basename "$0")" ] && exit 0
echo "$(basename "$0") fake 1.2.3 args: $*"; echo "second line" >&2`

// fakeLoginBash records `bash -lc` calls and drops the -l: a login shell puts
// the machine's own tool dirs (path_helper) ahead of the fakes. Other calls
// (bin/ctx's own shebang) are passed through unrecorded.
const fakeLoginBash = `#!/bin/sh
if [ "$1" = -lc ]; then
  for a in "$@"; do printf '%s\000' "$a" >> "$0.argv"; done
  printf '\n' >> "$0.argv"
  exec /bin/bash -c "$2"
fi
exec /bin/bash "$@"
`

type doctorCase struct {
	name    string
	present []string // fake tools on PATH
	omitSys []string // system commands hidden from PATH
	files   map[string]string
	modes   map[string]os.FileMode // HOME-relative path -> mode, applied after seeding
	env     []string
	vacuous string
}

func doctorHealthy() map[string]string {
	return map[string]string{
		".zshrc":                  "eval \"$(mise activate zsh)\"\n# BEGIN ctx profile autoswitch\n",
		".ssh/config":             "Include $HOME/.ssh/ctx_config\n",
		".ctx/profiles/good.conf": "WORK_DIR=$HOME/work\nSSH_KEY_PATH=$HOME/.ssh/id_good\n",
		"work/mise.toml":          "",
		".ssh/id_good":            "KEY",
		".ctx/config":             "",
		".ctx/git-hooks/.keep":    "",
		".ssh/ctx_config":         "",
	}
}

func merge(base, over map[string]string) map[string]string {
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	for k, v := range over {
		if v == "-" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	return m
}

// doctorSysPath is testutil.SystemPath minus the omitted commands.
func doctorSysPath(t *testing.T, omit []string) string {
	t.Helper()
	src := testutil.SystemPath(t)
	if len(omit) == 0 {
		return src
	}
	dir := t.TempDir()
	ents, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	skip := map[string]bool{}
	for _, o := range omit {
		skip[o] = true
	}
	for _, e := range ents {
		if skip[e.Name()] {
			continue
		}
		if err := os.Symlink(filepath.Join(src, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDoctorMatchesBash(t *testing.T) {
	healthyTools := append(append([]string{}, doctorTools...), "security", "pass")
	prof := func(conf string) map[string]string { return map[string]string{".ctx/profiles/acme.conf": conf} }
	cases := []doctorCase{
		{name: "everything present and healthy", present: healthyTools, files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "Everything looks good."},
		{name: "every tool absent, nothing configured", omitSys: []string{"git", "security"},
			files: map[string]string{".ctx/config": ""}, env: []string{"SHELL=/bin/bash", "CTX_SECRET_PROVIDER=file"},
			vacuous: "git: not found  →  "},
		{name: "empty version output", present: healthyTools, files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file", "QUIET_TOOL=gum"}, vacuous: "gum: installed"},
		{name: "keychain available", present: []string{"security"}, files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=keychain"}, vacuous: "secret provider"},
		{name: "keychain unavailable", omitSys: []string{"security"}, files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=keychain"}, vacuous: "Install/enable macOS security Keychain tooling"},
		{name: "pass available", present: []string{"pass"}, files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=pass"}, vacuous: "secret provider: pass (effective: pass)"},
		{name: "pass unavailable", files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=pass"}, vacuous: "Install pass (and initialize it)"},
		{name: "auto provider", files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=auto"}, vacuous: "secret provider"},
		{name: "guard installed", present: []string{"git"}, files: merge(doctorHealthy(), map[string]string{".ctx/git-hooks/pre-commit": "#!/bin/sh\n"}),
			modes: map[string]os.FileMode{".ctx/git-hooks/pre-commit": 0o755},
			env:   []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file", "GUARD_HOOKS=$HOME/.ctx/git-hooks"}, vacuous: "Git guard: on"},
		{name: "guard hooks path elsewhere", present: []string{"git"}, files: merge(doctorHealthy(), map[string]string{".ctx/git-hooks/pre-commit": "#!/bin/sh\n"}),
			modes: map[string]os.FileMode{".ctx/git-hooks/pre-commit": 0o755},
			env:   []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file", "GUARD_HOOKS=/elsewhere"}, vacuous: "Git guard: off"},
		{name: "profile: work dir, mise.toml and key missing", files: merge(doctorHealthy(), prof("WORK_DIR=$HOME/nowhere\nSSH_KEY_PATH=$HOME/.ssh/nokey\n")),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "Profile 'acme': mise.toml missing in"},
		{name: "profile: only mise.toml missing", files: merge(doctorHealthy(), merge(prof("WORK_DIR=$HOME/w2\n"), map[string]string{"w2/.keep": ""})),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "Profile 'acme': mise.toml missing in"},
		{name: "profile: bad email is not a doctor check", files: merge(doctorHealthy(), prof("GIT_EMAIL=alice\nWORK_DIR=$HOME/work\n")),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "Profile 'acme': ok"},
		{name: "profile: empty conf", files: merge(doctorHealthy(), prof("")),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "Profile 'acme': ok"},
		{name: "no profiles", files: merge(doctorHealthy(), map[string]string{".ctx/profiles/good.conf": "-"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "No profiles yet. Run: ctx setup"},
		{name: "dot conf is not a profile", files: merge(doctorHealthy(), map[string]string{".ctx/profiles/.conf": "WORK_DIR=/x\n", ".ctx/profiles/good.conf": "-"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "No profiles yet."},
		{name: "two profiles sorted", files: merge(doctorHealthy(), prof("WORK_DIR=$HOME/work\n")),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "Profile 'acme': ok"},
		{name: "bash rc without hooks", files: merge(doctorHealthy(), map[string]string{".bashrc": "# nothing\n"}),
			env: []string{"SHELL=/bin/bash", "CTX_SECRET_PROVIDER=file"}, vacuous: "mise activate: not in"},
		{name: "bash rc missing", files: doctorHealthy(),
			env: []string{"SHELL=/bin/bash", "CTX_SECRET_PROVIDER=file"}, vacuous: "ctx profile autoswitch: not in"},
		{name: "fish rc", files: merge(doctorHealthy(), map[string]string{".config/fish/config.fish": "mise activate fish | source\nctx auto-switch\n"}),
			env: []string{"SHELL=/usr/bin/fish", "CTX_SECRET_PROVIDER=file"}, vacuous: "mise shell hook: installed"},
		{name: "legacy autoswitch marker", files: merge(doctorHealthy(), map[string]string{".zshrc": "mise activate\n_ctx_profile_autoswitch() { :; }\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "ctx profile autoswitch: installed in"},
		{name: "activate form", files: merge(doctorHealthy(), map[string]string{".zshrc": "eval \"$(mise activate zsh)\"\neval \"$(ctx activate zsh)\"\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "autoswitch: installed in "},
		{name: "activate form names the form", files: merge(doctorHealthy(), map[string]string{".zshrc": "eval \"$(mise activate zsh)\"\neval \"$(ctx activate zsh)\"\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "/.zshrc (ctx activate)\n"},
		{name: "activate form, ctx-core path, bash", files: merge(doctorHealthy(), map[string]string{".bashrc": "eval \"$(mise activate bash)\"\neval \"$(/opt/ctx/ctx-core activate bash)\"\n"}),
			env: []string{"SHELL=/bin/bash", "CTX_SECRET_PROVIDER=file"}, vacuous: "(ctx activate)"},
		{name: "activate form, fish", files: merge(doctorHealthy(), map[string]string{".config/fish/config.fish": "mise activate fish | source\nctx activate fish | source\n"}),
			env: []string{"SHELL=/usr/bin/fish", "CTX_SECRET_PROVIDER=file"}, vacuous: "(ctx activate)"},
		{name: "lib source line", files: merge(doctorHealthy(), map[string]string{".zshrc": "mise activate\nsource ~/.local/lib/ctx/ctx_autoswitch.bash\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "(ctx_autoswitch lib"},
		{name: "lib block", files: doctorHealthy(),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "(ctx_autoswitch lib → ctx activate when ctx-core is installed)"},
		{name: "both forms", files: merge(doctorHealthy(), map[string]string{".zshrc": "mise activate\n# BEGIN ctx profile autoswitch\n# END ctx profile autoswitch\neval \"$(ctx activate zsh)\"\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "(ctx activate, and the ctx_autoswitch lib)"},
		{name: "mise activate is not ctx activate", files: merge(doctorHealthy(), map[string]string{".zshrc": "eval \"$(mise activate zsh)\"\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "ctx profile autoswitch: not in"},
		{name: "SHELL names zsh and bash, bash wins", files: merge(doctorHealthy(), map[string]string{".bashrc": "mise activate\n# BEGIN ctx profile autoswitch\n", ".zshrc": "-"}),
			env: []string{"SHELL=/opt/zsh/bash", "CTX_SECRET_PROVIDER=file"}, vacuous: "installed in "},
		{name: "SHELL empty", files: doctorHealthy(),
			env: []string{"SHELL=", "CTX_SECRET_PROVIDER=file"}, vacuous: "SSH isolation"},
		{name: "ssh config without Include", files: merge(doctorHealthy(), map[string]string{".ssh/config": "Host x\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "not included in ~/.ssh/config"},
		{name: "ssh Include commented out", files: merge(doctorHealthy(), map[string]string{".ssh/config": "# Include ctx_config\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "not included in ~/.ssh/config"},
		{name: "ssh Include indented, later line", files: merge(doctorHealthy(), map[string]string{".ssh/config": "Host x\n\t Include /a/b/ctx_config\n"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "ctx_config is included"},
		{name: "ssh dir group-writable", files: doctorHealthy(), modes: map[string]os.FileMode{".ssh": 0o770},
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "group- or world-writable"},
		{name: "ssh dir world-writable", files: doctorHealthy(), modes: map[string]os.FileMode{".ssh": 0o757},
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "Run: chmod 700"},
		{name: "ssh dir strict", files: doctorHealthy(), modes: map[string]os.FileMode{".ssh": 0o700},
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "strict enough"},
		{name: "no ssh dir", files: merge(doctorHealthy(), map[string]string{".ssh/config": "-", ".ssh/ctx_config": "-", ".ssh/id_good": "-"}),
			env: []string{"SHELL=/bin/zsh", "CTX_SECRET_PROVIDER=file"}, vacuous: "~/.ssh not present yet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newPtest(t)
			e := &cvEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
			var path string
			for _, tool := range c.present {
				script := fakeDoctorTool
				dir, log := testutil.FakeBin(t, tool, script)
				path += dir + string(os.PathListSeparator)
				e.logs[tool] = log
			}
			bdir := t.TempDir()
			testutil.Script(t, filepath.Join(bdir, "bash"), fakeLoginBash)
			e.logs["bash"] = filepath.Join(bdir, "bash.argv")
			coreDir := t.TempDir()
			corePath := filepath.Join(coreDir, "ctx-core")
			testutil.Script(t, corePath, "#!/bin/sh\necho 'ctx-core 9.9.9'\n")
			p.Setenv("PATH", path+bdir+string(os.PathListSeparator)+doctorSysPath(t, c.omitSys))
			p.Setenv("CTX_NO_GUM", "1")
			p.Setenv("CTX_CORE_BIN", corePath)
			for _, kv := range c.env {
				k, v, _ := strings.Cut(kv, "=")
				p.Setenv(k, strings.ReplaceAll(v, "$HOME", e.f.home))
			}
			files := map[string]string{}
			for k, v := range c.files {
				files[k] = v
			}
			if _, ok := files[".ctx/profiles/acme.conf"]; ok && c.name == "two profiles sorted" {
				files[".ctx/profiles/zed.conf"] = "WORK_DIR=$HOME/work\n"
			}
			seed := func() map[string]string {
				e.seed(t, files)
				for rel, m := range c.modes {
					if err := os.Chmod(filepath.Join(e.f.home, rel), m); err != nil {
						t.Fatal(err)
					}
				}
				return testutil.Snapshot(t, e.f.home)
			}

			before := seed()
			want := e.bash(t, "", "doctor")
			if d := testutil.DiffSnapshots(before, want.tree); d != "" {
				t.Errorf("bash doctor changed HOME: %s", d)
			}
			seed()
			got := e.goRun(t, "", nil, func(tm tui.Term) int {
				oldExe, oldVer := coreExecutable, CoreVersion
				defer func() { coreExecutable, CoreVersion = oldExe, oldVer }()
				coreExecutable = func() (string, error) { return corePath, nil }
				CoreVersion = "9.9.9"
				return Doctor(tm, nil)
			})
			if d := testutil.DiffSnapshots(before, got.tree); d != "" {
				t.Errorf("go doctor changed HOME: %s", d)
			}
			e.compare(t, want, got)
			if want.code != 0 {
				t.Errorf("bash exit %d", want.code)
			}
			if !strings.Contains(want.stdout, c.vacuous) {
				t.Errorf("vacuous: bash output lacks %q\n%s\n%s", c.vacuous, want.stdout, want.stderr)
			}
			if !strings.Contains(want.stdout, "ctx-core: 9.9.9 ("+corePath+")") {
				t.Errorf("no ctx-core line:\n%s", want.stdout)
			}
		})
	}
}

// TestDoctorR4 pins R4 in Go: a profile warning means the closing warning.
func TestDoctorR4(t *testing.T) {
	p := newPtest(t)
	e := &cvEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
	var path string
	for _, tool := range doctorTools {
		dir, _ := testutil.FakeBin(t, tool, fakeDoctorTool)
		path += dir + string(os.PathListSeparator)
	}
	bdir := t.TempDir()
	testutil.Script(t, filepath.Join(bdir, "bash"), fakeLoginBash)
	p.Setenv("PATH", path+bdir+string(os.PathListSeparator)+testutil.SystemPath(t))
	p.Setenv("SHELL", "/bin/zsh")
	p.Setenv("CTX_SECRET_PROVIDER", "file")
	e.seed(t, merge(doctorHealthy(), map[string]string{".ctx/profiles/acme.conf": "WORK_DIR=$HOME/nowhere\n"}))
	got := e.goRun(t, "", nil, func(tm tui.Term) int { return Doctor(tm, nil) })
	for _, l := range strings.Split(got.stdout, "\n") {
		if strings.HasPrefix(l, "! ") && !strings.HasPrefix(l, "! Profile 'acme'") && l != "! Fix the issues above." {
			t.Errorf("fixture should warn only about the profile: %q", l)
		}
	}
	if strings.Contains(got.stdout, "Everything looks good.") || !strings.Contains(got.stdout, "Fix the issues above.") {
		t.Errorf("R4:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "Profile 'acme': work dir missing:") {
		t.Errorf("no profile warning:\n%s", got.stdout)
	}
}

// TestDoctorProbeArgv pins the version probes: bash -lc with the tool's own
// command, in Bash's order, and none for an absent tool.
func TestDoctorProbeArgv(t *testing.T) {
	p := newPtest(t)
	e := &cvEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
	var path string
	for _, tool := range []string{"git", "az", "kubectl", "mise"} {
		dir, log := testutil.FakeBin(t, tool, fakeDoctorTool)
		path += dir + string(os.PathListSeparator)
		e.logs[tool] = log
	}
	bdir := t.TempDir()
	testutil.Script(t, filepath.Join(bdir, "bash"), fakeLoginBash)
	e.logs["bash"] = filepath.Join(bdir, "bash.argv")
	p.Setenv("PATH", path+bdir+string(os.PathListSeparator)+doctorSysPath(t, []string{"git"}))
	p.Setenv("SHELL", "/bin/zsh")
	e.seed(t, map[string]string{".ctx/config": ""})
	got := e.goRun(t, "", nil, func(tm tui.Term) int { return Doctor(tm, nil) })
	var cmds []string
	for _, a := range got.argv["bash"] {
		cmds = append(cmds, strings.Join(a, " "))
	}
	want := "-lc git --version|-lc mise --version|-lc az --version|-lc kubectl version --client=true"
	if strings.Join(cmds, "|") != want {
		t.Errorf("probes %q, want %q", strings.Join(cmds, "|"), want)
	}
	// git is also asked for core.hooksPath by the guard check.
	if want := [][]string{{"--version"}, {"config", "--global", "--get", "core.hooksPath"}}; !reflect.DeepEqual(got.argv["git"], want) {
		t.Errorf("git argv %q, want %q", got.argv["git"], want)
	}
	if want := [][]string{{"--version"}}; !reflect.DeepEqual(got.argv["mise"], want) {
		t.Errorf("mise argv %q, want %q", got.argv["mise"], want)
	}
	if !strings.Contains(got.stdout, "✓ git: git fake 1.2.3 args: --version\n") {
		t.Errorf("first line only:\n%s", got.stdout)
	}
}

// TestDoctorRich keeps the wording in rich mode (the header is a box).
func TestDoctorRich(t *testing.T) {
	p := newPtest(t)
	e := &cvEnv{f: newPFixture(p), p: p, logs: map[string]string{}}
	p.Setenv("PATH", doctorSysPath(t, []string{"git"}))
	p.Setenv("SHELL", "/bin/zsh")
	p.Setenv("CTX_SECRET_PROVIDER", "file")
	e.seed(t, map[string]string{".ctx/config": ""})
	got := e.goRun(t, "", nil, func(tm tui.Term) int { tm.Rich = true; return Doctor(tm, nil) })
	for _, s := range []string{"╭", "ctx doctor", "Machine-wide — tools on PATH", "git: not found  →  ", "Fix the issues above."} {
		if !strings.Contains(got.stdout, s) {
			t.Errorf("rich output lacks %q:\n%s", s, got.stdout)
		}
	}
}
