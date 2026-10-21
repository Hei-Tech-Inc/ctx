package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// TestActivateUsage rejects a missing or unknown shell without output.
func TestActivateUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"tcsh"}, {"bash", "extra"}} {
		var out, errb bytes.Buffer
		code := Activate(tui.Term{Out: &out, Err: &errb}, args)
		if code != 2 || out.Len() != 0 || !strings.Contains(errb.String(), activateUsage) {
			t.Errorf("%q: code %d out %q err %q", args, code, out.String(), errb.String())
		}
	}
}

// TestActivateCLI runs the built ctx-core: the glue bakes the path it was
// started by (a symlink stays a symlink, a PATH name is resolved through PATH).
func TestActivateCLI(t *testing.T) {
	core := buildCore(t)
	td := t.TempDir()
	link := filepath.Join(td, "l dir", "ctx-core")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(core, link); err != nil {
		t.Fatal(err)
	}
	for _, sh := range []string{"bash", "zsh", "fish"} {
		for _, c := range []struct {
			name, path, arg0, want string
		}{
			{"absolute", core, core, core},
			{"symlink", link, link, link},
			{"PATH name", link, "ctx-core", link},
		} {
			cmd := exec.Command(c.path, "activate", sh)
			cmd.Args[0] = c.arg0
			cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(link)+":"+testutil.SystemPath(t))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s %s: %v %s", sh, c.name, err, stderr.String())
			}
			if string(out) != activateScript(sh, c.want) {
				t.Errorf("%s %s: glue does not bake %s", sh, c.name, c.want)
			}
		}
	}
}

// activateShells are the shells the glue is tested in: the system bash (3.2
// on macOS), a newer bash on PATH when there is one, zsh, fish when installed.
func activateShells(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	if b := testutil.SystemTool("bash"); b != "" {
		out["bash"] = []string{b, "--norc", "--noprofile"}
	}
	if b, err := exec.LookPath("bash"); err == nil && b != out["bash"][0] {
		out["bash-path"] = []string{b, "--norc", "--noprofile"}
	}
	if z := testutil.SystemTool("zsh"); z != "" {
		out["zsh"] = []string{z, "-f"}
	}
	return out
}

// shellOf is the glue dialect of an activateShells key.
func shellOf(key string) string { return strings.TrimSuffix(key, "-path") }

// TestActivateSyntax parses the glue with bash -n, zsh -n and fish -n (when
// installed), for a core path with a space, a quote and a dollar.
func TestActivateSyntax(t *testing.T) {
	core := "/opt/c dir'q/$x/ctx-core"
	shells := activateShells(t)
	if f, err := exec.LookPath("fish"); err == nil {
		shells["fish"] = []string{f, "--no-config"}
	}
	for key, argv := range shells {
		td := t.TempDir()
		glue := filepath.Join(td, "glue")
		hookWrite(t, glue, activateScript(shellOf(key), core))
		out, err := exec.Command(argv[0], "-n", glue).CombinedOutput()
		if err != nil || len(out) != 0 {
			t.Errorf("%s -n: %v %s", key, err, out)
		}
	}
	for _, sh := range []string{"bash", "zsh", "fish"} {
		g := activateScript(sh, core)
		if strings.Contains(g, "@CTX_CORE@") {
			t.Errorf("%s: placeholder left", sh)
		}
		if m := unsafeGlueRE.FindString(g); m != "" {
			t.Errorf("%s glue contains %q", sh, m)
		}
	}
}

// unsafeGlueRE finds what must not run in the user's shell: shell options
// that end it on an error, and exit.
var unsafeGlueRE = regexp.MustCompile(`(?m)^\s*(set -[a-z]*[eu][a-z]*\s*$|set -o (errexit|nounset)|exit\b)`)

// activatePrelude is the test scripts' helpers (bash 3.2, bash 5, zsh):
// prompt runs one prompt's hooks the way the shell does; settle/delta count
// the fake ctx-core's calls.
const activatePrelude = `
cd "$TD" || exit 1
fail() { printf 'FAIL: %s\n' "$*"; exit 1; }
count() { N=0; if [ -f "$LOG" ]; then while IFS= read -r _l; do N=$((N+1)); done <"$LOG"; fi; }
settle() { count; _base=$N; }
delta() { count; [ $((N-_base)) = "$1" ] || fail "$2: $((N-_base)) ctx-core calls, want $1"; _base=$N; }
if [ -n "${ZSH_VERSION-}" ]; then
  prompt() { local _f; for _f in $precmd_functions; do "$_f"; done; }
  nhooks() { local _f; N=0; for _f in $precmd_functions $chpwd_functions; do [ "$_f" = "$1" ] && N=$((N+1)); done; }
else
  prompt() {
    if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
      for _c in "${PROMPT_COMMAND[@]}"; do eval "$_c"; done
    else
      eval "${PROMPT_COMMAND-}"
    fi
  }
fi
ran=""
user1() { ran="${ran}1"; }
user2() { ran="${ran}2"; }
user3() { ran="${ran}3"; }
src() { source "$GLUE" >>"$TMPDIR/src.out"; }
`

// activateCase is one scripted scenario; shells limits it ("" = bash and zsh).
type activateCase struct {
	name, shells, script string
}

var activateCases = []activateCase{
	{name: "source twice registers one hook", script: `
if [ -n "${ZSH_VERSION-}" ]; then precmd_functions=(user1); else PROMPT_COMMAND='user1;user2'; fi
src; src
if [ -n "${ZSH_VERSION-}" ]; then
  nhooks _ctx_hook; [ $N = 2 ] || fail "zsh: $N registrations (want chpwd+precmd)"
  [ "${precmd_functions[1]}" = user1 ] || fail "user precmd lost: $precmd_functions"
else
  [ "$PROMPT_COMMAND" = '_ctx_hook;user1;user2' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
fi
[ ! -s "$TMPDIR/src.out" ] || fail "source printed: $(cat "$TMPDIR/src.out")"
settle; cd w/a; prompt; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "not entered: ${CTX_ACTIVE_PROFILE-}"
delta 1 "enter"
case "$ran" in 11|1212) ;; *) fail "user hooks ran $ran" ;; esac
`},
	{name: "legacy hook then activate", script: `
if [ -n "${ZSH_VERSION-}" ]; then precmd_functions=(user1); else PROMPT_COMMAND='user1'; fi
CTX_CORE=off source "$CTX_ROOT/lib/ctx_autoswitch.bash"
src
if [ -n "${ZSH_VERSION-}" ]; then
  nhooks _ctx_profile_autoswitch; [ $N = 0 ] || fail "legacy zsh hook left"
  nhooks _ctx_hook; [ $N = 2 ] || fail "zsh: $N registrations"
  (( ${+functions[_ctx_profile_autoswitch]} )) && fail "legacy function left"
else
  [ "$PROMPT_COMMAND" = '_ctx_hook;user1' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
  declare -F _ctx_profile_autoswitch >/dev/null && fail "legacy function left"
fi
settle; cd w/a; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "not entered"
delta 1 "enter"
`},
	{name: "legacy tokens in a PROMPT_COMMAND string", shells: "bash", script: `
PROMPT_COMMAND='_ctx_profile_autoswitch;user1;_ctx_auto_switch;user2;_ctx_hook'
src; src
[ "$PROMPT_COMMAND" = '_ctx_hook;user1;user2' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
`},
	{name: "PROMPT_COMMAND array keeps the user's entries", shells: "bash", script: `
if (( BASH_VERSINFO[0] < 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] < 1) )); then echo SKIP; exit 0; fi
PROMPT_COMMAND=(user1 user2)
CTX_CORE=off source "$CTX_ROOT/lib/ctx_autoswitch.bash"
PROMPT_COMMAND+=('_ctx_profile_autoswitch;user3')
src; src
want=(_ctx_hook user1 user2 user3)
[ "${PROMPT_COMMAND[*]}" = "${want[*]}" ] || fail "PROMPT_COMMAND=(${PROMPT_COMMAND[*]})"
[[ "$(declare -p PROMPT_COMMAND)" == "declare -a"* ]] || fail "no longer an array"
settle; cd w/a; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] && [ "$ran" = 123 ] || fail "prompt: active=${CTX_ACTIVE_PROFILE-} ran=$ran"
delta 1 "enter"
`},
	{name: "bash-preexec", shells: "bash", script: `
PROMPT_COMMAND='user1'
src
[ "$PROMPT_COMMAND" = '_ctx_hook;user1' ] || fail "before bash-preexec: $PROMPT_COMMAND"
# A minimal bash-preexec: precmd_functions, and an install that wraps
# PROMPT_COMMAND between its own commands (as __bp_install does).
bash_preexec_imported=defined
precmd_functions=(user2)
__bp_precmd_invoke_cmd() { local _s=$? _f; for _f in "${precmd_functions[@]}"; do (exit $_s); "$_f"; done; }
__bp_interactive_mode() { :; }
PROMPT_COMMAND=$'__bp_precmd_invoke_cmd\n'"$PROMPT_COMMAND"$'\n__bp_interactive_mode'
src; src
[ "$PROMPT_COMMAND" = $'__bp_precmd_invoke_cmd\nuser1\n__bp_interactive_mode' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
[ "${precmd_functions[*]}" = 'user2 _ctx_hook' ] || fail "precmd_functions=(${precmd_functions[*]})"
settle; cd w/a; prompt; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "not entered"
delta 1 "enter"
[ "$ran" = 2121 ] || fail "ran=$ran"
`},
	{name: "unchanged prompts do not fork", script: `
src; cd w/a; prompt; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "not entered"
settle
_path=$PATH; PATH=/nonexistent/canary
if [ -n "${EPOCHREALTIME-}" ]; then _t0=$EPOCHREALTIME; fi
_i=0; while [ $_i -lt 50 ]; do prompt; _i=$((_i+1)); done
if [ -n "${EPOCHREALTIME-}" ]; then _t1=$EPOCHREALTIME; fi
PATH=$_path
delta 0 "50 unchanged prompts"
if [ -n "${_t0-}" ]; then echo "TIME ${_t0} ${_t1}"; fi
`},
	{name: "stamp missing, config and profiles newer", script: `
src; cd w/a; prompt; settle
[ -e "$__CTX_STAMP" ] || fail "no stamp"
case "$__CTX_STAMP" in "$CTX_DIR/hook-stamps/"*) ;; *) fail "stamp $__CTX_STAMP" ;; esac
rm -f "$__CTX_STAMP"; prompt; delta 1 "stamp missing"
[ -e "$__CTX_STAMP" ] || fail "stamp not recreated"
prompt; delta 0 "after the stamp is back"
touch -t 200001010000 "$__CTX_STAMP"; prompt; delta 1 "config newer"
prompt; delta 0 "after config"
touch -t 200001010000 "$CTX_DIR/config"; touch -t 200101010000 "$__CTX_STAMP"
prompt; delta 1 "profiles dir newer"
prompt; delta 0 "after profiles"
`},
	{name: "ctx-core removed at runtime", script: `
src; cd w/a; prompt
mv "$FAKE" "$FAKE.gone"
cd ../b; prompt; prompt
rm -f "$__CTX_STAMP"; prompt
(exit 7); _ctx_hook; _rc=$?
[ $_rc = 7 ] || fail "status $_rc, want 7"
ctx -q use b || fail "wrapper without ctx-core"
[ "${CTX_ACTIVE_PROFILE-}" = b ] || fail "wrapper fallback did not apply"
mv "$FAKE.gone" "$FAKE"
(exit 5); _ctx_hook; _rc=$?
[ $_rc = 5 ] || fail "status $_rc, want 5"
`},
	{name: "ctx wrapper", script: `
src; settle
ctx --no-gum use a >/dev/null 2>&1 || fail "use failed"
[ "$AWS_PROFILE" = pa ] && [ "$API_KEY" = "$EXPECT_SECRET" ] && [ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "use: AWS=${AWS_PROFILE-}"
delta 1 "use runs the baked ctx-core"
grep -qx 'use --eval bash a' "$LOG" || fail "log: $(cat "$LOG")"
ctx deactivate >/dev/null 2>&1
[ -z "${AWS_PROFILE+x}${API_KEY+x}${CTX_ACTIVE_PROFILE+x}" ] || fail "deactivate left vars"
delta 1 "deactivate"
ctx -q use a 2>"$TMPDIR/q.err" >/dev/null || fail "-q use"
[ ! -s "$TMPDIR/q.err" ] || fail "-q use printed"
[ "$AWS_PROFILE" = pa ] || fail "-q use: AWS unset"
ctx deactivate -q >/dev/null || fail "deactivate -q"
ctx use a --quiet 2>"$TMPDIR/q.err" >/dev/null || fail "use --quiet"
[ ! -s "$TMPDIR/q.err" ] || fail "use --quiet (after the name) printed"
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "use --quiet"
ctx -q deactivate
settle
ctx --dry-run use a >/dev/null 2>&1 || true
[ -z "${AWS_PROFILE+x}" ] || fail "--dry-run use changed the shell"
[ -z "$(ctx use a --eval bash 2>/dev/null | grep -v '^export ')" ] || fail "--eval output"
[ -z "${AWS_PROFILE+x}" ] || fail "explicit --eval was applied"
delta 0 "dry-run and --eval go through bin/ctx"
[ "$(ctx list 2>&1)" = "$(command ctx list 2>&1)" ] || fail "list passthrough"
ctx version | grep -q "^ctx version" || fail "version passthrough"
ctx use nosuch >/dev/null 2>&1 && fail "bad profile exit 0"
ctx use a -- x >/dev/null 2>&1 && fail "use a -- x exit 0"
ctx nosuchcmd >/dev/null 2>&1 && fail "bad command exit 0"
settle
CTX_CORE=off ctx -q use b || fail "CTX_CORE=off use"
[ "${CTX_ACTIVE_PROFILE-}" = b ] || fail "CTX_CORE=off use not applied"
delta 0 "CTX_CORE=off goes through bin/ctx"
`},
	{name: "set -u", script: `
set -u
src; cd w/a; prompt; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "not entered"
ctx -q use b; ctx -q deactivate; cd ..; prompt
[ "$CTX_PROMPT_SHOW" = 0 ] || fail "prompt show"
`},
}

// activateFixture is hookFixture plus a logging fake ctx-core (at a path with
// a space and a quote) that runs the real one, and the glue baking the fake.
func activateFixture(t *testing.T, td, shell string) (env []string, fake, log string) {
	t.Helper()
	env = hookFixture(t, td, hookCase{})
	// ctx use (not the hook's) runs gh auth switch.
	gh := filepath.Join(td, "bin", "gh")
	if err := os.Remove(gh); err != nil {
		t.Fatal(err)
	}
	testutil.Script(t, gh, "#!/bin/sh\ncase \"$1 $2\" in 'auth token') printf 'tok-%s\\n' \"$4\" ;; 'auth switch') ;; *) exit 1 ;; esac\n")
	core := buildCore(t)
	fake = filepath.Join(td, "core dir'q", "ctx-core")
	log = filepath.Join(td, "core.log")
	if err := os.MkdirAll(filepath.Dir(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Script(t, fake, "#!/bin/sh\nprintf '%s\\n' \"$*\" >>"+shellQ(log)+"\nexec "+shellQ(core)+" \"$@\"\n")
	glue := filepath.Join(td, "glue."+shell)
	hookWrite(t, glue, activateScript(shell, fake))
	env = append(env,
		"TD="+td, "GLUE="+glue, "FAKE="+fake, "LOG="+log, "CTX_ROOT="+repoRoot,
		"CTX_CORE_BIN="+core, "CTX_AUTOSWITCH_NOTIFY=0", "EXPECT_SECRET="+hookSecret,
	)
	return env, fake, log
}

// TestActivateShells sources the glue in real bash and zsh through every
// activateCase: registration, legacy cleanup, the guard, the wrapper.
func TestActivateShells(t *testing.T) {
	if testing.Short() {
		t.Skip("runs bash, zsh and ctx-core")
	}
	shells := activateShells(t)
	for _, c := range activateCases {
		for key, argv := range shells {
			sh := shellOf(key)
			if c.shells != "" && c.shells != sh {
				continue
			}
			t.Run(c.name+"/"+key, func(t *testing.T) {
				t.Parallel()
				td := t.TempDir()
				env, _, _ := activateFixture(t, td, sh)
				args := append(append([]string{}, argv[1:]...), "-c", activatePrelude+c.script)
				cmd := exec.Command(argv[0], args...)
				cmd.Dir = td
				cmd.Env = env
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("%v\nstdout: %s\nstderr: %s", err, out, stderr.String())
				}
				if stderr.Len() > 0 {
					t.Errorf("stderr: %q", stderr.String())
				}
				for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					switch {
					case l == "SKIP":
						t.Skip("needs bash 5.1+")
					case strings.HasPrefix(l, "TIME "):
						logPromptTime(t, l)
					case l != "":
						t.Errorf("stdout: %q", l)
					}
				}
			})
		}
	}
}

// logPromptTime logs the mean of the 50 unchanged prompts ("TIME t0 t1").
func logPromptTime(t *testing.T, l string) {
	var t0, t1 float64
	f := strings.Fields(strings.ReplaceAll(l, ",", "."))
	if len(f) == 3 {
		if _, err := fmt.Sscan(f[1], &t0); err == nil {
			if _, err := fmt.Sscan(f[2], &t1); err == nil {
				t.Logf("unchanged prompt: %.3f ms (mean of 50)", (t1-t0)*1000/50)
			}
		}
	}
}

// TestActivateFish sources the fish glue twice in real fish (when installed):
// one handler, the legacy functions gone, the guard skipping unchanged
// prompts, the wrapper applying use.
func TestActivateFish(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	t.Parallel()
	td := t.TempDir()
	env, fake, _ := activateFixture(t, td, "fish")
	script := `
function fail; printf 'FAIL: %s\n' "$argv"; exit 1; end
CTX_CORE=off source $CTX_ROOT/lib/ctx_autoswitch.fish
source $GLUE; source $GLUE
functions -q _ctx_profile_autoswitch; and fail "legacy hook left"
functions -q _ctx_profile_autoswitch_postexec; and fail "legacy postexec left"
test (functions --handlers | string match -c '*_ctx_hook*') -ge 1; or fail "no handler"
cd w/a
test "${CTX_ACTIVE_PROFILE-}" = a; or fail "not entered: $CTX_ACTIVE_PROFILE"
set -l n0 (wc -l < $LOG)
for i in (seq 50); emit fish_postexec; end
test (wc -l < $LOG) = $n0; or fail "unchanged prompts called ctx-core"
rm -f $__CTX_STAMP; emit fish_postexec
test -e $__CTX_STAMP; or fail "stamp not recreated"
mv $FAKE $FAKE.gone; cd ../b; emit fish_postexec
mv $FAKE.gone $FAKE
ctx -q use b; or fail "use b"
test "${CTX_ACTIVE_PROFILE-}" = b; or fail "wrapper"
set -q AWS_PROFILE; and fail "AWS_PROFILE left from a"
`
	cmd := exec.Command(fish, "--no-config", "-c", script)
	cmd.Dir = td
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || stderr.Len() > 0 {
		t.Fatalf("fish: %v\n%s\n%s (fake %s)", err, out, stderr.String(), fake)
	}
}
