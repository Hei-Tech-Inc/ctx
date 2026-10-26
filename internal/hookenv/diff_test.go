package hookenv_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/hookenv"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if coreDir != "" {
		_ = os.RemoveAll(coreDir)
	}
	testutil.CleanupSystemPath()
	os.Exit(code)
}

var (
	coreOnce sync.Once
	coreDir  string
	coreBin  string
	coreErr  error
)

func buildCore(t *testing.T) string {
	t.Helper()
	coreOnce.Do(func() {
		coreDir, coreErr = os.MkdirTemp("", "hookenv-core")
		if coreErr != nil {
			return
		}
		coreBin = filepath.Join(coreDir, "ctx-core")
		cmd := exec.Command("go", "build", "-o", coreBin, "./cmd/ctx")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			coreErr = fmt.Errorf("%v: %s", err, out)
		}
	})
	if coreErr != nil {
		t.Fatalf("build ctx-core: %v", coreErr)
	}
	return coreBin
}

func repoRoot() string {
	r, _ := filepath.Abs("../..")
	return r
}

// diffCase is a scripted shell session: cd through steps (relative to the
// fixture root), running the hook twice per step (zsh chpwd + precmd; the
// second run is bash's next prompt). config holds extra config lines, with
// @TD for the fixture root and @B64(path) for a base64 manual_pwd.
type diffCase struct {
	name   string
	steps  []string
	config []string
	env    []string
}

var diffCases = []diffCase{
	{name: "enter and leave", steps: []string{".", "w/a/x", "."}},
	{name: "switch a to b and back", steps: []string{"w/a", "w/b", "w/a/x"}},
	{name: "nested WORK_DIRs: longest prefix wins", steps: []string{"w/a", "w/a/n/deep", "w/a/x", "w/a/n"}},
	{name: "cd within a profile, prompt depth", steps: []string{"w/a", "w/a/x", "w/a/x/y/z", "w/a/x"}},
	{name: ".ctx marker under a .git ancestor", steps: []string{"w/mono", "w/mono/svc", "w/mono/svc/deep", "w/mono"}},
	{name: "sibling rule under a broad WORK_DIR", steps: []string{"w/clients", "w/clients/acme", "w/clients/other"}},
	{name: "symlinked pwd", steps: []string{"w/b/lnk", "link", "w/a/x"}},
	{
		name: "manual lock held, then cleared", steps: []string{"w/b/q", "w/b/q/r", "w/b/q"},
		config: []string{"active=a", "active_source=manual", "manual_pwd=@B64(@TD/w/b/q)"},
	},
	{name: "multiple active= lines", steps: []string{"w/a", "w/b"}, config: []string{"active=b", "active=a"}},
	{name: "new terminal with a machine-wide active profile", steps: []string{"w/a/x", "w/a"}, config: []string{"active=a"}},
	{name: "user-set GH_TOKEN left alone", steps: []string{"w/a", "w/b", "."}, env: []string{"GH_TOKEN=mine"}},
	{name: "failed use is not retried", steps: []string{"w/f", "w/f/sub", "w/a", "w/f"}},
	{name: "notify off from the environment", steps: []string{"w/a", "w/b", "."}, env: []string{"CTX_AUTOSWITCH_NOTIFY=0"}},
	{
		name: "prompt extra paths and depth from config", steps: []string{"e/x", "w/a/x/y/z", "."},
		config: []string{"prompt_extra_paths=/nowhere:@TD/e/", "prompt_workdir_max_depth=3"},
	},
}

// fixture writes profiles, dirs, config and fakes under td and returns the
// base environment of the session.
func fixture(t *testing.T, td string, c diffCase) []string {
	t.Helper()
	ctxDir := filepath.Join(td, "ctx")
	for _, d := range []string{"home", "ctx/profiles", "bin", "w/a/x/y/z", "w/a/n/deep", "w/b/q/r", "w/mono/.git",
		"w/mono/svc/deep", "w/clients/acme", "w/clients/other", "w/f/sub", "e/x"} {
		if err := os.MkdirAll(filepath.Join(td, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	confs := map[string]string{
		"a":       "WORK_DIR=@TD/w/a\nAWS_PROFILE_NAME=pa\nGITHUB_USER=alice\n",
		"b":       "WORK_DIR=\"@TD/w/b\"\nAWS_PROFILE_NAME=pb\nGCP_PROJECT=gb\n",
		"n":       "WORK_DIR=@TD/w/a/n\nGITHUB_USER='nina'\n",
		"mono":    "WORK_DIR=@TD/w/mono\nAWS_PROFILE_NAME=pm\n",
		"clients": "WORK_DIR=@TD/w/clients\n",
		"acme":    "PROFILE_NAME=acme\nAZURE_TENANT=t\n",
		"f":       "WORK_DIR=@TD/w/f\nAZURE_SUBSCRIPTION=sub\n",
	}
	for n, body := range confs {
		// Profiles ctx writes always set these; Bash deactivate needs SECRET_KEYS (set -u).
		body += "SECRET_KEYS=''\nEXTRA_ENVS=''\n"
		write(t, filepath.Join(ctxDir, "profiles", n+".conf"), strings.ReplaceAll(body, "@TD", td))
	}
	write(t, filepath.Join(td, "w/mono/svc/.ctx"), "profile= b \n")
	if err := os.Symlink(filepath.Join(td, "w/a"), filepath.Join(td, "w/b/lnk")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(td, "w/a/x"), filepath.Join(td, "link")); err != nil {
		t.Fatal(err)
	}
	b64 := regexp.MustCompile(`@B64\(([^)]*)\)`)
	cfg := "autoswitch_notify=on\n"
	for _, l := range c.config {
		l = strings.ReplaceAll(l, "@TD", td)
		l = b64.ReplaceAllStringFunc(l, func(m string) string {
			return base64.StdEncoding.EncodeToString([]byte(b64.FindStringSubmatch(m)[1]))
		})
		cfg += l + "\n"
	}
	write(t, filepath.Join(ctxDir, "config"), cfg)

	bin := filepath.Join(td, "bin")
	testutil.Script(t, filepath.Join(bin, "ctx"), "#!/bin/sh\nexec "+shq(filepath.Join(repoRoot(), "bin/ctx"))+" \"$@\"\n")
	testutil.Script(t, filepath.Join(bin, "gh"), "#!/bin/sh\n[ \"$1 $2 $3\" = 'auth token -u' ] && printf 'tok-%s\\n' \"$4\"\n")
	testutil.Script(t, filepath.Join(bin, "az"), "#!/bin/sh\nexit 1\n")
	testutil.Script(t, filepath.Join(bin, "gcloud"), "#!/bin/sh\nexit 0\n")
	env := []string{
		"PATH=" + bin + ":" + testutil.SystemPath(t),
		"HOME=" + filepath.Join(td, "home"),
		"CTX_DIR=" + ctxDir,
		"CTX_CORE=off",
		"TMPDIR=" + td,
		"LC_ALL=C",
	}
	return append(env, c.env...)
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// snapshot is the session after one step: exported variables and notify output.
type snapshot struct {
	env    map[string]string
	notify string
}

// legacy runs the bash hook (lib/ctx_autoswitch.bash, CTX_CORE=off) through
// the steps in one bash --norc session.
func legacy(t *testing.T, td string, env []string, steps []string) ([]snapshot, string) {
	t.Helper()
	script := `
dump() { local k; printf '%s\0' @@STEP; for k in $(compgen -e); do printf '%s=%s\0' "$k" "${!k}"; done; }
i=0
for d in "$@"; do
  cd "$d" || exit 1
  if [[ $i == 0 ]]; then
    { source "$CTX_ROOT/lib/ctx_autoswitch.bash"; _ctx_profile_autoswitch; } 2>"$TMPDIR/err.$i"
  else
    { _ctx_profile_autoswitch; _ctx_profile_autoswitch; } 2>"$TMPDIR/err.$i"
  fi
  dump
  i=$((i+1))
done
`
	args := []string{"--norc", "--noprofile", "-c", script, "bash"}
	for _, s := range steps {
		args = append(args, filepath.Join(td, s))
	}
	cmd := exec.Command(systemBash(t), args...)
	cmd.Dir = td
	cmd.Env = append(append([]string{}, env...), "CTX_ROOT="+repoRoot())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("legacy hook: %v\n%s", err, stderr.String())
	}
	var snaps []snapshot
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "@@STEP" {
			snaps = append(snaps, snapshot{env: map[string]string{}})
			continue
		}
		if k, v, ok := strings.Cut(f, "="); ok && len(snaps) > 0 {
			snaps[len(snaps)-1].env[k] = v
		}
	}
	for i := range snaps {
		b, _ := os.ReadFile(filepath.Join(td, fmt.Sprintf("err.%d", i)))
		snaps[i].notify = string(b)
	}
	return snaps, activation(t, td)
}

// goSession applies Compute's outputs through the same steps: the shell env
// is a map, deactivate and use run as ctx-core (their side effects on the
// config, and use's exports, eval'd from its stdout).
func goSession(t *testing.T, td string, baseEnv []string, steps []string) ([]snapshot, string) {
	t.Helper()
	core := buildCore(t)
	env := map[string]string{}
	for _, kv := range baseEnv {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	profilesDir := filepath.Join(td, "ctx", "profiles")
	state := ""
	var snaps []snapshot
	for _, s := range steps {
		pwd := filepath.Join(td, s)
		var notify strings.Builder
		for r := 0; r < 2; r++ {
			data, _ := os.ReadFile(filepath.Join(td, "ctx", "config"))
			name, wd, err := resolve.Path(pwd, profilesDir)
			if err != nil {
				t.Fatal(err)
			}
			prev, _ := hookenv.Decode(state)
			plan := hookenv.Compute(hookenv.Input{
				PWD: pwd, Prev: prev, Env: env, Cfg: ctxconfig.Parse(data),
				PathProfile: name, PathWD: wd, Profiles: loadProfiles(t, profilesDir),
				GH: func(user string) (string, error) { return "tok-" + user + "\n", nil },
			})
			if plan.Deactivate {
				runCore(t, core, pwd, env, "CTX_QUIET=1")("deactivate", "--eval", "bash")
			}
			var res hookenv.UseResult
			if plan.Use != nil {
				stdout, code := runCore(t, core, pwd, env, "CTX_QUIET=1", "CTX_AUTO_SWITCH=1")("use", "--eval", "bash", plan.Use.Target)
				res = hookenv.UseResult{Exports: evalExports(t, stdout), Code: code}
			}
			out := plan.Finish(res)
			for _, l := range out.Notify {
				notify.WriteString(l + "\n")
			}
			next := map[string]string{}
			for k, v := range env {
				next[k] = v
			}
			for _, k := range out.Unsets {
				delete(next, k)
			}
			for _, kv := range out.Exports {
				next[kv[0]] = kv[1]
			}
			env = next
			state = out.State.Encode()
		}
		snaps = append(snaps, snapshot{env: env, notify: notify.String()})
	}
	return snaps, activation(t, td)
}

func loadProfiles(t *testing.T, dir string) []profile.Profile {
	t.Helper()
	names, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []profile.Profile
	for _, n := range names {
		if p, err := profile.Load(dir, n); err == nil {
			out = append(out, *p)
		}
	}
	return out
}

func runCore(t *testing.T, core, pwd string, env map[string]string, extra ...string) func(args ...string) (string, int) {
	return func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(core, args...)
		cmd.Dir = pwd
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Env = append(append(cmd.Env, "PWD="+pwd), extra...)
		out, err := cmd.Output()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
}

var exportRE = regexp.MustCompile(`(?m)^export ([A-Za-z_][A-Za-z0-9_]*)=`)

// evalExports evals ctx use's --eval bash output and reads back what it exported.
func evalExports(t *testing.T, out string) [][2]string {
	t.Helper()
	var names []string
	for _, m := range exportRE.FindAllStringSubmatch(out, -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		return nil
	}
	cmd := exec.Command(systemBash(t), append([]string{"--norc", "-c", `eval "$1"; shift; for k; do printf '%s\0' "${!k}"; done`, "bash", out}, names...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	b, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	vals := strings.Split(string(b), "\x00")
	res := make([][2]string, len(names))
	for i, n := range names {
		res[i] = [2]string{n, vals[i]}
	}
	return res
}

// activation is the config's active/active_source lines, the machine-wide state.
func activation(t *testing.T, td string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(td, "ctx", "config"))
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "active=") || strings.HasPrefix(l, "active_source=") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// ignored are variables bash itself sets.
var ignored = map[string]bool{"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true, "CTX_ROOT": true}

func normalize(env map[string]string, td string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		if !ignored[k] {
			out[k] = strings.ReplaceAll(v, td, "@TD")
		}
	}
	return out
}

// TestComputeMatchesLegacyHook runs every diffCase through the legacy bash
// hook and through Compute (with ctx-core doing use/deactivate), each on its
// own copy of the fixture, and compares the shell env and the notify output
// after every step, and the machine-wide activation at the end.
func TestComputeMatchesLegacyHook(t *testing.T) {
	if testing.Short() {
		t.Skip("differential test runs bash and ctx-core")
	}
	systemBash(t)
	buildCore(t)
	for _, c := range diffCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			tdL, tdG := t.TempDir(), t.TempDir()
			legSnaps, legAct := legacy(t, tdL, fixture(t, tdL, c), c.steps)
			goSnaps, goAct := goSession(t, tdG, fixture(t, tdG, c), c.steps)
			if len(legSnaps) != len(goSnaps) {
				t.Fatalf("steps: legacy %d, go %d", len(legSnaps), len(goSnaps))
			}
			for i := range legSnaps {
				le, ge := normalize(legSnaps[i].env, tdL), normalize(goSnaps[i].env, tdG)
				if d := envDiff(le, ge); d != "" {
					t.Errorf("step %d (%s): env differs (legacy vs go):\n%s", i, c.steps[i], d)
				}
				if legSnaps[i].notify != goSnaps[i].notify {
					t.Errorf("step %d (%s): notify legacy %q, go %q", i, c.steps[i], legSnaps[i].notify, goSnaps[i].notify)
				}
			}
			if strings.ReplaceAll(legAct, tdL, "@TD") != strings.ReplaceAll(goAct, tdG, "@TD") {
				t.Errorf("activation: legacy %q, go %q", legAct, goAct)
			}
		})
	}
}

// systemBash is the system's bash (/bin/bash, 3.2 on macOS), not whichever
// bash comes first on PATH: the legacy hook must keep working there.
func systemBash(t *testing.T) string {
	t.Helper()
	b := testutil.SystemTool("bash")
	if b == "" {
		t.Skip("no bash in the system dirs")
	}
	return b
}

func envDiff(a, b map[string]string) string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, k := range names {
		av, aok := a[k]
		bv, bok := b[k]
		if av != bv || aok != bok {
			fmt.Fprintf(&out, "  %s: %q (set=%v) vs %q (set=%v)\n", k, av, aok, bv, bok)
		}
	}
	return out.String()
}
