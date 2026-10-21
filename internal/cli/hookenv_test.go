package cli

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
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/hookenv"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// hookSecret is profile a's API_KEY: quotes, a dollar, a newline, UTF-8.
const hookSecret = "s3'cret $v\nline2 é"

// hookCase is a scripted shell session: cd through steps (relative to the
// fixture root) running the hook at each. config holds extra config lines
// (@TD is the fixture root, @B64(path) a base64 manual_pwd); env extra
// exported variables; locals shell variables set but not exported.
type hookCase struct {
	name   string
	steps  []string
	config []string
	env    []string
	locals []string
}

var hookCases = []hookCase{
	{name: "enter, switch, leave", steps: []string{"w/a/x", "w/b", "."}},
	{name: "nested WORK_DIR and back", steps: []string{"w/a", "w/a/n/deep", "w/a/x", "."}},
	{name: "cd within a profile, prompt depth", steps: []string{"w/a", "w/a/x/y/z", "w/a/x"}},
	{name: "new terminal with a machine-wide active profile", steps: []string{"w/a/x", "."}, config: []string{"active=a"}},
	{
		name: "manual lock held, then cleared", steps: []string{"w/b/q", "w/b/q/r", "w/b/q"},
		config: []string{"active=a", "active_source=manual", "manual_pwd=@B64(@TD/w/b/q)"},
	},
	{name: "failed use is not retried", steps: []string{"w/f", "w/f/sub", "w/a"}},
	{name: "exported user GH_TOKEN left alone", steps: []string{"w/a", "w/b", "."}, env: []string{"GH_TOKEN=mine"}},
	{name: "unexported user GH_TOKEN left alone", steps: []string{"w/a", "."}, locals: []string{"GH_TOKEN=mine"}},
	{
		name: "unexported CTX_AUTOSWITCH_NOTIFY=1 beats config off", steps: []string{"w/a", "w/b", "."},
		config: []string{"autoswitch_notify=off"}, locals: []string{"CTX_AUTOSWITCH_NOTIFY=1"},
	},
	{name: "exported CTX_AUTOSWITCH_NOTIFY=0 beats config on", steps: []string{"w/a", "."}, env: []string{"CTX_AUTOSWITCH_NOTIFY=0"}},
}

// hookFixture writes profiles, dirs, config, secrets and fakes under td and
// returns the session's base environment.
func hookFixture(t *testing.T, td string, c hookCase) []string {
	t.Helper()
	ctxDir := filepath.Join(td, "ctx")
	for _, d := range []string{"home", "ctx/profiles", "ctx/secrets/a", "bin", "w/a/x/y/z", "w/a/n/deep", "w/b/q/r", "w/f/sub"} {
		if err := os.MkdirAll(filepath.Join(td, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	confs := map[string]string{
		"a": "WORK_DIR=@TD/w/a\nAWS_PROFILE_NAME=pa\nGITHUB_USER=alice\nSECRET_KEYS=API_KEY\n",
		"b": "WORK_DIR=\"@TD/w/b\"\nGCP_PROJECT=gb\nSECRET_KEYS=''\n",
		"n": "WORK_DIR=@TD/w/a/n\nAWS_PROFILE_NAME=pn\nGITHUB_USER='nina'\nSECRET_KEYS=''\n",
		"f": "WORK_DIR=@TD/w/f\nAZURE_SUBSCRIPTION=sub\nSECRET_KEYS=''\n",
	}
	for n, body := range confs {
		// Profiles ctx writes always set these (Bash use needs PROFILE_NAME
		// for secrets, Bash deactivate SECRET_KEYS, under set -u).
		body = "PROFILE_NAME=" + n + "\n" + body + "EXTRA_ENVS=''\n"
		hookWrite(t, filepath.Join(ctxDir, "profiles", n+".conf"), strings.ReplaceAll(body, "@TD", td))
	}
	hookWrite(t, filepath.Join(ctxDir, "secrets/a/API_KEY"), hookSecret)
	b64 := regexp.MustCompile(`@B64\(([^)]*)\)`)
	cfg := "autoswitch_notify=on\nsecret_provider=file\n"
	for _, l := range c.config {
		l = strings.ReplaceAll(l, "@TD", td)
		l = b64.ReplaceAllStringFunc(l, func(m string) string {
			return base64.StdEncoding.EncodeToString([]byte(b64.FindStringSubmatch(m)[1]))
		})
		cfg += l + "\n"
	}
	hookWrite(t, filepath.Join(ctxDir, "config"), cfg)

	bin := filepath.Join(td, "bin")
	testutil.Script(t, filepath.Join(bin, "ctx"), "#!/bin/sh\nexec "+shellQ(filepath.Join(repoRoot, "bin/ctx"))+" \"$@\"\n")
	testutil.Script(t, filepath.Join(bin, "gh"), "#!/bin/sh\n[ \"$1 $2 $3\" = 'auth token -u' ] && printf 'tok-%s\\n' \"$4\"\n")
	testutil.Script(t, filepath.Join(bin, "az"), "#!/bin/sh\nexit 1\n")
	testutil.Script(t, filepath.Join(bin, "gcloud"), "#!/bin/sh\nexit 0\n")
	env := []string{
		"PATH=" + bin + ":" + testutil.SystemPath(t),
		"HOME=" + filepath.Join(td, "home"),
		"CTX_DIR=" + ctxDir,
		"TMPDIR=" + td,
		"LC_ALL=C",
		"CORE=" + buildCore(t),
	}
	return append(env, c.env...)
}

func hookWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shellQ(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// hookSnap is the shell after one step: the exported variables that differ
// from the session's start (removed ones map to "<unset>"), the values of
// the shell-local variables, and the notify output.
type hookSnap struct {
	env    map[string]string
	locals map[string]string
	notify string
}

// shellLocals are read as shell variables (exported or not) at every step.
var shellLocals = []string{"GH_TOKEN", "CTX_AUTOSWITCH_NOTIFY", "__CTX_STATE"}

// dumpFn is the session scripts' dump function (bash 3.2 and zsh): the
// exported environment from env -0, then the shell variables.
const dumpFn = `
dump() {
  printf '@@STEP\0'
  command env -0
  for k in GH_TOKEN CTX_AUTOSWITCH_NOTIFY __CTX_STATE; do
    eval "printf '@sh:%s=%s\0' $k \"\${$k-<unset>}\""
  done
}
`

// hookSession is the hook-env glue under test: what activate's glue passes
// (see HookEnv), in a function that runs hook-env and evals its output. Each
// step runs it three times: the second (zsh precmd after chpwd) must print
// nothing; the third, after the config's mtime moved, only the same state.
const hookSession = dumpFn + `
h() {
  local out
  out="$(__CTX_STATE="${__CTX_STATE-}" __CTX_STAMP="${__CTX_STAMP-}" \
    __CTX_SH_NOTIFY="${CTX_AUTOSWITCH_NOTIFY+=$CTX_AUTOSWITCH_NOTIFY}" \
    __CTX_SH_GH_TOKEN="${GH_TOKEN+=$GH_TOKEN}" \
    __CTX_SH_ACTIVE="${CTX_ACTIVE_PROFILE+=$CTX_ACTIVE_PROFILE}" \
    __CTX_SH_GH_PROFILE="${_CTX_GH_PROFILE+=$_CTX_GH_PROFILE}" \
    "$CORE" hook-env "$SH" 2>>"$TMPDIR/err.$i")"
  printf '%s' "$out" >"$TMPDIR/out.$i.$r"
  eval "$out"
}
eval "$LOCALS"
dump
i=0
for d in "$@"; do
  cd "$d" || exit 1
  r=1; h
  r=2; h
  touch "$CTX_DIR/config"
  r=3; h
  dump
  i=$((i+1))
done
`

// legacyHookSession runs lib/ctx_autoswitch.bash (CTX_CORE=off) twice per
// step, as the hookenv differential test does.
const legacyHookSession = dumpFn + `
eval "$LOCALS"
dump
i=0
for d in "$@"; do
  cd "$d" || exit 1
  if [ $i = 0 ]; then
    { source "$CTX_ROOT/lib/ctx_autoswitch.bash"; _ctx_profile_autoswitch; } 2>"$TMPDIR/err.$i"
  else
    { _ctx_profile_autoswitch; _ctx_profile_autoswitch; } 2>"$TMPDIR/err.$i"
  fi
  dump
  i=$((i+1))
done
`

// runSession runs script in shell (argv[0] plus flags) through the steps and
// returns one hookSnap per step and the config's activation lines.
func runSession(t *testing.T, td string, env []string, c hookCase, script string, shell ...string) []hookSnap {
	t.Helper()
	args := append(append([]string{}, shell[1:]...), "-c", script, "session")
	for _, s := range c.steps {
		args = append(args, filepath.Join(td, s))
	}
	cmd := exec.Command(shell[0], args...)
	cmd.Dir = td
	cmd.Env = append(append([]string{}, env...), "CTX_ROOT="+repoRoot, "LOCALS="+strings.Join(c.locals, "\n"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s session: %v\n%s", shell[0], err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("%s session wrote to stderr: %q", shell[0], stderr.String())
	}
	return parseSnaps(t, td, string(out))
}

func parseSnaps(t *testing.T, td, out string) []hookSnap {
	t.Helper()
	var raw []map[string]string
	for _, f := range strings.Split(out, "\x00") {
		if f == "@@STEP" {
			raw = append(raw, map[string]string{})
			continue
		}
		if k, v, ok := strings.Cut(f, "="); ok && len(raw) > 0 {
			raw[len(raw)-1][k] = strings.ReplaceAll(v, td, "@TD")
		}
	}
	if len(raw) < 1 {
		t.Fatalf("no dump in %q", out)
	}
	base := raw[0]
	var snaps []hookSnap
	for i, m := range raw[1:] {
		s := hookSnap{env: map[string]string{}, locals: map[string]string{}}
		for k, v := range m {
			if name, ok := strings.CutPrefix(k, "@sh:"); ok {
				s.locals[name] = v
			} else if base[k] != v || !hasKey(base, k) {
				s.env[k] = v
			}
		}
		for k := range base {
			if !strings.HasPrefix(k, "@sh:") && !hasKey(m, k) {
				s.env[k] = "<unset>"
			}
		}
		for _, k := range []string{"PWD", "OLDPWD", "_"} {
			delete(s.env, k)
		}
		b, _ := os.ReadFile(filepath.Join(td, fmt.Sprintf("err.%d", i)))
		s.notify = string(b)
		snaps = append(snaps, s)
	}
	return snaps
}

func hasKey(m map[string]string, k string) bool { _, ok := m[k]; return ok }

// activationLines are the config's active/active_source lines.
func activationLines(t *testing.T, td string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(td, "ctx", "config"))
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "active=") || strings.HasPrefix(l, "active_source=") {
			keep = append(keep, strings.ReplaceAll(l, td, "@TD"))
		}
	}
	return strings.Join(keep, "\n")
}

// sessionShells are the shells hook-env is eval'd in: the system bash (3.2
// on macOS) and zsh, and fish when installed.
func sessionShells(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	if b := testutil.SystemTool("bash"); b != "" {
		out["bash"] = []string{b, "--norc", "--noprofile"}
	}
	if z := testutil.SystemTool("zsh"); z != "" {
		out["zsh"] = []string{z, "-f"}
	}
	return out
}

// TestHookEnvMatchesLegacyHook evals hook-env in real bash and zsh through
// every hookCase and compares, per step, the exported environment, the
// shell variables the hook reads and the notify output with the legacy
// hook's; and the machine-wide activation at the end. It also checks the
// run-twice idempotency of every step and that secrets reach stdout only.
func TestHookEnvMatchesLegacyHook(t *testing.T) {
	if testing.Short() {
		t.Skip("runs bash, zsh and ctx-core")
	}
	shells := sessionShells(t)
	if shells["bash"] == nil {
		t.Skip("no bash in the system dirs")
	}
	for _, c := range hookCases {
		for _, sh := range []string{"bash", "zsh"} {
			argv := shells[sh]
			t.Run(c.name+"/"+sh, func(t *testing.T) {
				t.Parallel()
				if argv == nil {
					t.Skip(sh + " not installed")
				}
				tdL, tdG := t.TempDir(), t.TempDir()
				leg := runSession(t, tdL, append(hookFixture(t, tdL, c), "CTX_CORE=off"), c, legacyHookSession, shells["bash"]...)
				legAct := activationLines(t, tdL)
				got := runSession(t, tdG, append(hookFixture(t, tdG, c), "SH="+sh), c, hookSession, argv...)
				if len(leg) != len(got) {
					t.Fatalf("steps: legacy %d, hook-env %d", len(leg), len(got))
				}
				for i := range leg {
					delete(got[i].locals, "__CTX_STATE")
					delete(leg[i].locals, "__CTX_STATE")
					delete(got[i].env, "SHLVL")
					delete(leg[i].env, "SHLVL")
					if d := mapDiff(leg[i].env, got[i].env); d != "" {
						t.Errorf("step %d (%s): exported env differs (legacy vs hook-env):\n%s", i, c.steps[i], d)
					}
					if d := mapDiff(leg[i].locals, got[i].locals); d != "" {
						t.Errorf("step %d (%s): shell variables differ (legacy vs hook-env):\n%s", i, c.steps[i], d)
					}
					if leg[i].notify != got[i].notify {
						t.Errorf("step %d (%s): notify legacy %q, hook-env %q", i, c.steps[i], leg[i].notify, got[i].notify)
					}
				}
				if a := activationLines(t, tdG); a != legAct {
					t.Errorf("activation: legacy %q, hook-env %q", legAct, a)
				}
				checkRuns(t, tdG, len(c.steps))
				checkSecretOnlyOnStdout(t, tdG, len(c.steps))
			})
		}
	}
}

// checkRuns asserts the run-twice idempotency of every step: the second run
// prints nothing (fast path), the third (config touched) only the state the
// first one left, as a shell-local assignment.
func checkRuns(t *testing.T, td string, steps int) {
	t.Helper()
	for i := 0; i < steps; i++ {
		first := readOut(t, td, i, 1)
		if o := readOut(t, td, i, 2); o != "" {
			t.Errorf("step %d: second run printed %q, want nothing", i, o)
		}
		third := readOut(t, td, i, 3)
		m := regexp.MustCompile(`(?m)^__CTX_STATE=.*$`).FindAllString(first, -1)
		if len(m) != 1 || third != m[0] {
			t.Errorf("step %d: third run printed %q, want only the state line %q", i, third, m)
		}
		if strings.Contains(first, "export __CTX_") {
			t.Errorf("step %d: state exported: %q", i, first)
		}
	}
}

func readOut(t *testing.T, td string, step, run int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(td, fmt.Sprintf("out.%d.%d", step, run)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// checkSecretOnlyOnStdout asserts the secret value appears in no notify
// output, no state and no file but its own (the stdout captures excepted).
func checkSecretOnlyOnStdout(t *testing.T, td string, steps int) {
	t.Helper()
	parts := []string{hookSecret, "s3'cret", "line2"}
	_ = filepath.Walk(td, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(td, path)
		if rel == "ctx/secrets/a/API_KEY" || strings.HasPrefix(rel, "out.") {
			return nil
		}
		b, _ := os.ReadFile(path)
		for _, p := range parts {
			if bytes.Contains(b, []byte(p)) {
				t.Errorf("secret part %q found in %s", p, rel)
			}
		}
		return nil
	})
	for i := 0; i < steps; i++ {
		for r := 1; r <= 3; r++ {
			for _, l := range strings.Split(readOut(t, td, i, r), "\n") {
				if strings.HasPrefix(l, "__CTX_") && strings.Contains(l, "s3") {
					t.Errorf("secret in state line %q", l)
				}
			}
		}
	}
}

func mapDiff(a, b map[string]string) string {
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

var stampRE = regexp.MustCompile(`hook-stamps/[0-9a-f]{16}`)

// The golden session: enter a (fresh shell), switch to b, leave.
var goldenSteps = []string{"w/a/x", "w/b", "."}

const goldenNotify = "\x1b[2m[ctx] → a\x1b[0m\n|\x1b[2m[ctx] ← a → b\x1b[0m\n|\x1b[2m[ctx] ← b\x1b[0m\n"

// goldenBash is hook-env's stdout for bash and zsh at each golden step.
var goldenBash = []string{
	`export AWS_PROFILE=pa
export API_KEY=$'s3\'cret $v\nline2 \303\251'
export CTX_ACTIVE_PROFILE=a
export CTX_ACTIVATION_TRIGGER=auto
export GH_TOKEN=tok-alice
export _CTX_GH_PROFILE=a
export CTX_PROMPT_SHOW=1
export CTX_PROMPT_PROFILE=a
export CTX_PROMPT_WORK_DIR=@TD/w/a
__CTX_STATE=v1\|@TD/w/a/x\|a\|AWS_PROFILE\ API_KEY\ CTX_ACTIVE_PROFILE\ CTX_ACTIVATION_TRIGGER
__CTX_STAMP=@TD/ctx/@STAMP
`,
	`unset API_KEY
unset AWS_PROFILE
unset GH_TOKEN
unset _CTX_GH_PROFILE
export CTX_ACTIVE_PROFILE=b
export CTX_PROMPT_PROFILE=b
export CTX_PROMPT_WORK_DIR=@TD/w/b
__CTX_STATE=v1\|@TD/w/b\|b\|CTX_ACTIVE_PROFILE\ CTX_ACTIVATION_TRIGGER
`,
	`unset CTX_ACTIVE_PROFILE
unset CTX_ACTIVATION_TRIGGER
unset CTX_PROMPT_PROFILE
unset CTX_PROMPT_WORK_DIR
export CTX_PROMPT_SHOW=0
__CTX_STATE=v1\|@TD\|\|
`,
}

// goldenFish is the same session's stdout for fish.
var goldenFish = []string{
	`set -gx AWS_PROFILE 'pa'
set -gx API_KEY 's3\'cret $v
line2 é'
set -gx CTX_ACTIVE_PROFILE 'a'
set -gx CTX_ACTIVATION_TRIGGER 'auto'
set -gx GH_TOKEN 'tok-alice'
set -gx _CTX_GH_PROFILE 'a'
set -gx CTX_PROMPT_SHOW '1'
set -gx CTX_PROMPT_PROFILE 'a'
set -gx CTX_PROMPT_WORK_DIR '@TD/w/a'
set -gu __CTX_STATE 'v1|@TD/w/a/x|a|AWS_PROFILE API_KEY CTX_ACTIVE_PROFILE CTX_ACTIVATION_TRIGGER'
set -gu __CTX_STAMP '@TD/ctx/@STAMP'
`,
	`set -e API_KEY
set -e AWS_PROFILE
set -e GH_TOKEN
set -e _CTX_GH_PROFILE
set -gx CTX_ACTIVE_PROFILE 'b'
set -gx CTX_PROMPT_PROFILE 'b'
set -gx CTX_PROMPT_WORK_DIR '@TD/w/b'
set -gu __CTX_STATE 'v1|@TD/w/b|b|CTX_ACTIVE_PROFILE CTX_ACTIVATION_TRIGGER'
`,
	`set -e CTX_ACTIVE_PROFILE
set -e CTX_ACTIVATION_TRIGGER
set -e CTX_PROMPT_PROFILE
set -e CTX_PROMPT_WORK_DIR
set -gx CTX_PROMPT_SHOW '0'
set -gu __CTX_STATE 'v1|@TD||'
`,
}

func goldenNorm(s, td string) string {
	return stampRE.ReplaceAllString(strings.ReplaceAll(s, td, "@TD"), "@STAMP")
}

// TestHookEnvGoldenBashZsh checks hook-env's exact output, eval'd in real
// bash and zsh, for enter, switch and leave.
func TestHookEnvGoldenBashZsh(t *testing.T) {
	shells := sessionShells(t)
	c := hookCase{steps: goldenSteps}
	for _, sh := range []string{"bash", "zsh"} {
		argv := shells[sh]
		t.Run(sh, func(t *testing.T) {
			t.Parallel()
			if argv == nil {
				t.Skip(sh + " not installed")
			}
			td := t.TempDir()
			snaps := runSession(t, td, append(hookFixture(t, td, c), "SH="+sh), c, hookSession, argv...)
			var notify []string
			for i := range goldenSteps {
				if got := goldenNorm(readOut(t, td, i, 1), td); got != strings.TrimSuffix(goldenBash[i], "\n") {
					t.Errorf("step %d (%s) stdout:\n%s\nwant:\n%s", i, goldenSteps[i], got, goldenBash[i])
				}
				notify = append(notify, snaps[i].notify)
			}
			if got := strings.Join(notify, "|"); got != goldenNotify {
				t.Errorf("notify %q, want %q", got, goldenNotify)
			}
			if v := snaps[0].env["API_KEY"]; v != hookSecret {
				t.Errorf("API_KEY after eval = %q, want %q", v, hookSecret)
			}
		})
	}
}

// TestHookEnvGoldenFish checks the fish output of the same session, applied
// by a parser of the fish lines hook-env prints (fish itself is exercised by
// TestHookEnvFishSession when installed).
func TestHookEnvGoldenFish(t *testing.T) {
	t.Parallel()
	td := t.TempDir()
	base := hookFixture(t, td, hookCase{})
	env := map[string]string{}
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	locals := map[string]string{}
	var notify []string
	for i, s := range goldenSteps {
		pwd := filepath.Join(td, s)
		cmd := exec.Command(env["CORE"], "hook-env", "fish")
		cmd.Dir = pwd
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Env = append(cmd.Env, "PWD="+pwd, "__CTX_STATE="+locals["__CTX_STATE"], "__CTX_STAMP="+locals["__CTX_STAMP"])
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("hook-env fish: %v\n%s", err, stderr.String())
		}
		if got := goldenNorm(string(out), td); got != goldenFish[i] {
			t.Errorf("step %d (%s) stdout:\n%s\nwant:\n%s", i, s, got, goldenFish[i])
		}
		notify = append(notify, stderr.String())
		applyFish(t, string(out), env, locals)
		if i == 0 && env["API_KEY"] != hookSecret {
			t.Errorf("API_KEY = %q, want %q", env["API_KEY"], hookSecret)
		}
	}
	if got := strings.Join(notify, "|"); got != goldenNotify {
		t.Errorf("notify %q, want %q", got, goldenNotify)
	}
	if _, ok := hookenv.Decode(locals["__CTX_STATE"]); !ok {
		t.Errorf("state %q does not decode", locals["__CTX_STATE"])
	}
}

var fishLineRE = regexp.MustCompile(`(?s)^set (-gx|-gu|-e) ([A-Za-z_][A-Za-z0-9_]*)(?: '((?:[^'\\]|\\.)*)')?$`)

// applyFish applies hook-env's fish output: set -gx exports, set -gu shell
// locals, set -e erases. Quoted values may span lines.
func applyFish(t *testing.T, out string, env, locals map[string]string) {
	t.Helper()
	var stmts []string
	cur, open := "", false
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if open {
			cur += "\n" + l
		} else {
			cur = l
		}
		// A statement is complete when its quotes are balanced.
		q := 0
		for i := 0; i < len(cur); i++ {
			if cur[i] == '\\' && q%2 == 1 {
				i++
				continue
			}
			if cur[i] == '\'' {
				q++
			}
		}
		open = q%2 == 1
		if !open {
			stmts = append(stmts, cur)
		}
	}
	for _, s := range stmts {
		m := fishLineRE.FindStringSubmatch(s)
		if m == nil {
			t.Fatalf("unexpected fish line %q", s)
		}
		v := strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(m[3])
		switch m[1] {
		case "-gx":
			env[m[2]] = v
		case "-gu":
			locals[m[2]] = v
		case "-e":
			delete(env, m[2])
		}
	}
}

// TestHookEnvFishSession sources hook-env in real fish through the golden
// session and compares the exported env with the parser's result.
func TestHookEnvFishSession(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	t.Parallel()
	td := t.TempDir()
	c := hookCase{steps: goldenSteps}
	env := hookFixture(t, td, c)
	script := `
function dump
  printf '@@STEP\0'
  command env -0
  for k in GH_TOKEN CTX_AUTOSWITCH_NOTIFY __CTX_STATE
    if set -q $k
      printf '@sh:%s=%s\0' $k "$$k"
    else
      printf '@sh:%s=<unset>\0' $k
    end
  end
end
function shvar
  set -q $argv[1]; and printf '=%s' "$$argv[1]"
end
function h
  env __CTX_STATE="$__CTX_STATE" __CTX_STAMP="$__CTX_STAMP" \
    __CTX_SH_NOTIFY=(shvar CTX_AUTOSWITCH_NOTIFY) __CTX_SH_GH_TOKEN=(shvar GH_TOKEN) \
    __CTX_SH_ACTIVE=(shvar CTX_ACTIVE_PROFILE) __CTX_SH_GH_PROFILE=(shvar _CTX_GH_PROFILE) \
    $CORE hook-env fish 2>>$TMPDIR/err.$i | source
end
dump
set -g i 0
for d in $argv
  cd $d; or exit 1
  h
  dump
  set i (math $i + 1)
end
`
	cmd := exec.Command(fish, "--no-config", "-c", script, "--")
	for _, s := range c.steps {
		cmd.Args = append(cmd.Args, filepath.Join(td, s))
	}
	cmd.Dir = td
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("fish session: %v\n%s", err, stderr.String())
	}
	snaps := parseSnaps(t, td, string(out))
	if len(snaps) != len(c.steps) {
		t.Fatalf("steps: %d", len(snaps))
	}
	if v := snaps[0].env["API_KEY"]; v != hookSecret {
		t.Errorf("API_KEY = %q, want %q", v, hookSecret)
	}
	if v := snaps[1].env["CTX_ACTIVE_PROFILE"]; v != "b" {
		t.Errorf("after switch CTX_ACTIVE_PROFILE = %q", v)
	}
	if v := snaps[2].env["CTX_PROMPT_SHOW"]; v != "0" {
		t.Errorf("after leave CTX_PROMPT_SHOW = %q", v)
	}
	if _, ok := snaps[2].env["__CTX_STATE"]; ok {
		t.Errorf("__CTX_STATE exported in fish")
	}
}

// TestHookEnvUsage rejects a missing or unknown shell without output.
func TestHookEnvUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"tcsh"}, {"bash", "extra"}} {
		var out, errb bytes.Buffer
		code := HookEnv(tui.Term{Out: &out, Err: &errb}, args)
		if code != 2 || out.Len() != 0 || !strings.Contains(errb.String(), hookEnvUsage) {
			t.Errorf("%q: code %d out %q err %q", args, code, out.String(), errb.String())
		}
	}
}

// TestHookQuoteEvals evals hookQuote's output in bash and zsh: a leading "="
// (zsh would expand =cmd), "~" after ":" or "=", globs and control bytes.
func TestHookQuoteEvals(t *testing.T) {
	vals := []string{"=ls", "==", "a:~/b", "~x", "x=~", "*", "$HOME", "it's", "a\nb\tc\x7f", "é", ""}
	for _, sh := range []string{"bash", "zsh"} {
		bin := testutil.SystemTool(sh)
		if bin == "" {
			continue
		}
		var script strings.Builder
		for i, v := range vals {
			fmt.Fprintf(&script, "export V%d=%s\nprintf '%%s\\0' \"$V%d\"\n", i, hookQuote(v), i)
		}
		cmd := exec.Command(bin, "-c", script.String())
		cmd.Env = []string{"HOME=/home/u", "LC_ALL=C"}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		for i, v := range vals {
			if i >= len(got) || got[i] != v {
				t.Errorf("%s: %q evals to %q", sh, v, got)
				break
			}
		}
	}
}

// TestUnsetGitIdentity is the git half of deactivate's eval'd output that
// hook-env runs in-process: bash/zsh match `[[ $PWD == wd* ]]` (a plain
// prefix), fish wd itself or below it.
func TestUnsetGitIdentity(t *testing.T) {
	git := testutil.SystemTool("git")
	if git == "" {
		t.Skip("git not installed")
	}
	td := t.TempDir()
	cases := []struct {
		dir, wd string
		fish    bool
		unset   bool
	}{
		{"w/a/r", "@/w/a", false, true},
		{"w/abc", "@/w/a", false, true}, // prefix, as [[ == wd* ]]
		{"w/abc", "@/w/a", true, false}, // fish needs wd/ or wd
		{"w/a/r", "@/w/a", true, true},
		{"w/b", "@/w/a", false, false},
	}
	for i, c := range cases {
		dir := filepath.Join(td, fmt.Sprint(i), c.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "N"}, {"config", "user.email", "e@x"}} {
			cmd := exec.Command(git, args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
		unsetGitIdentity(dir, strings.Replace(c.wd, "@", filepath.Join(td, fmt.Sprint(i)), 1), c.fish)
		cmd := exec.Command(git, "config", "--local", "--get-regexp", "^user\\.")
		cmd.Dir = dir
		out, _ := cmd.Output()
		if gone := len(out) == 0; gone != c.unset {
			t.Errorf("case %d (%s under %s, fish=%v): unset=%v, config %q", i, c.dir, c.wd, c.fish, gone, out)
		}
	}
}

// TestHookEnvStamp: without a profiles dir hook-env prints only the stamp
// (the legacy hook does nothing there); a stamp outside $CTX_DIR/hook-stamps
// is never touched but replaced; a valid one is kept and not printed again.
func TestHookEnvStamp(t *testing.T) {
	t.Parallel()
	core := buildCore(t)
	td := t.TempDir()
	ctxDir := filepath.Join(td, "ctx")
	outside := filepath.Join(td, "outside")
	hookWrite(t, outside, "")
	old := time.Unix(1_000_000, 0)
	if err := os.Chtimes(outside, old, old); err != nil {
		t.Fatal(err)
	}
	run := func(stamp string) string {
		t.Helper()
		cmd := exec.Command(core, "hook-env", "bash")
		cmd.Dir = td
		cmd.Env = []string{"HOME=" + td, "CTX_DIR=" + ctxDir, "PWD=" + td, "PATH=/usr/bin:/bin", "__CTX_STAMP=" + stamp}
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	out := run(outside)
	m := regexp.MustCompile(`^__CTX_STAMP=(\S+)\n$`).FindStringSubmatch(out)
	if m == nil || filepath.Dir(m[1]) != filepath.Join(ctxDir, "hook-stamps") {
		t.Fatalf("output %q, want only a new stamp under %s", out, ctxDir)
	}
	if _, err := os.Stat(m[1]); err != nil {
		t.Errorf("stamp not created: %v", err)
	}
	if st, _ := os.Stat(outside); !st.ModTime().Equal(old) {
		t.Errorf("a stamp outside $CTX_DIR was touched")
	}
	if out := run(m[1]); out != "" {
		t.Errorf("with its stamp: %q, want nothing", out)
	}
}
