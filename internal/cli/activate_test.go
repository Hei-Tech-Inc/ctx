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
  # As bash runs PROMPT_COMMAND: $? is the last command's status.
  _st() { return "$1"; }
  prompt() {
    local _s=$?
    if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
      for _c in "${PROMPT_COMMAND[@]}"; do _st "$_s"; eval "$_c"; _s=$?; done
    else
      _st "$_s"; eval "${PROMPT_COMMAND-}"
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
	check                func(t *testing.T, td string)
}

var activateCases = []activateCase{
	{name: "source twice registers one hook", script: `
if [ -n "${ZSH_VERSION-}" ]; then precmd_functions=(user1); else PROMPT_COMMAND='user1;user2'; fi
src; src
if [ -n "${ZSH_VERSION-}" ]; then
  nhooks _ctx_hook; [ $N = 2 ] || fail "zsh: $N registrations (want chpwd+precmd)"
  [ "${precmd_functions[1]}" = user1 ] || fail "user precmd lost: $precmd_functions"
else
  [ "$PROMPT_COMMAND" = '_ctx_hook keep && :;user1;user2' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
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
  [ "$PROMPT_COMMAND" = '_ctx_hook keep && :;user1' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
  declare -F _ctx_profile_autoswitch >/dev/null && fail "legacy function left"
fi
settle; cd w/a; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "not entered"
delta 1 "enter"
`},
	{name: "legacy tokens in a PROMPT_COMMAND string", shells: "bash", script: `
PROMPT_COMMAND='_ctx_profile_autoswitch;user1;_ctx_auto_switch;user2;_ctx_hook;_ctx_hook keep && :'
src; src
[ "$PROMPT_COMMAND" = '_ctx_hook keep && :;user1;user2' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
`},
	{name: "PROMPT_COMMAND array keeps the user's entries", shells: "bash", script: `
if (( BASH_VERSINFO[0] < 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] < 1) )); then echo SKIP; exit 0; fi
PROMPT_COMMAND=(user1 user2)
CTX_CORE=off source "$CTX_ROOT/lib/ctx_autoswitch.bash"
PROMPT_COMMAND+=('_ctx_profile_autoswitch;user3')
src; src
want=('_ctx_hook keep && :' user1 user2 user3)
[ "${#PROMPT_COMMAND[@]}" = 4 ] && [ "${PROMPT_COMMAND[*]}" = "${want[*]}" ] || fail "PROMPT_COMMAND=(${PROMPT_COMMAND[*]})"
[[ "$(declare -p PROMPT_COMMAND)" == "declare -a"* ]] || fail "no longer an array"
settle; cd w/a; prompt
[ "${CTX_ACTIVE_PROFILE-}" = a ] && [ "$ran" = 123 ] || fail "prompt: active=${CTX_ACTIVE_PROFILE-} ran=$ran"
delta 1 "enter"
`},
	{name: "bash-preexec", shells: "bash", script: `
PROMPT_COMMAND='user1'
src
[ "$PROMPT_COMMAND" = '_ctx_hook keep && :;user1' ] || fail "before bash-preexec: $PROMPT_COMMAND"
# A minimal bash-preexec: precmd_functions, and an install that wraps
# PROMPT_COMMAND between its own commands (as __bp_install does).
bash_preexec_imported=defined
precmd_functions=(user2)
__bp_precmd_invoke_cmd() { local _s=$? _f; for _f in "${precmd_functions[@]}"; do (exit $_s); "$_f"; done; }
__bp_interactive_mode() { :; }
PROMPT_COMMAND=$'__bp_precmd_invoke_cmd\n'"$PROMPT_COMMAND"$'\n__bp_interactive_mode'
src; src
[ "$PROMPT_COMMAND" = $'__bp_precmd_invoke_cmd\nuser1\n__bp_interactive_mode' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
[ "${precmd_functions[*]}" = '_ctx_hook user2' ] || fail "precmd_functions=(${precmd_functions[*]})"
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
[ $_rc = 0 ] || fail "status $_rc, want 0"
if [ -n "${BASH_VERSION-}" ]; then (exit 7); _ctx_hook keep; _rc=$?; [ $_rc = 7 ] || fail "keep: status $_rc, want 7"; fi
ctx -q use b || fail "wrapper without ctx-core"
[ "${CTX_ACTIVE_PROFILE-}" = b ] || fail "wrapper fallback did not apply"
mv "$FAKE.gone" "$FAKE"
(exit 5); _ctx_hook; _rc=$?
[ $_rc = 0 ] || fail "status $_rc, want 0"
if [ -n "${BASH_VERSION-}" ]; then (exit 5); _ctx_hook keep; _rc=$?; [ $_rc = 5 ] || fail "keep: status $_rc, want 5"; fi
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
	{name: "an alias named ctx does not stop the glue", script: `
if [ -n "${BASH_VERSION-}" ]; then shopt -s expand_aliases; fi
alias ctx='echo ALIAS'
src || fail "source failed"
unalias ctx
ctx -q use a || fail "use after unalias"
[ "${CTX_ACTIVE_PROFILE-}" = a ] || fail "wrapper not defined"
`},
	{name: "shell variables reach hook-env presence-encoded", script: `
src; GH_TOKEN='x
y'
rm -f "$__CTX_STAMP"; prompt
unset GH_TOKEN; rm -f "$__CTX_STAMP"; prompt
GH_TOKEN=; rm -f "$__CTX_STAMP"; prompt
`, check: func(t *testing.T, td string) {
		got := hookEnvTokens(t, td)
		want := []string{"", "=x\ny", "", "="}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("__CTX_SH_GH_TOKEN per run = %q, want %q", got, want)
		}
	}},
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
	// The fake logs each call and, for hook-env, the GH_TOKEN it was passed
	// (<none> when the variable is absent), NUL-terminated.
	testutil.Script(t, fake, "#!/bin/sh\nprintf '%s\\n' \"$*\" >>"+shellQ(log)+
		"\n[ \"$1\" = hook-env ] && printf '%s\\0' \"${__CTX_SH_GH_TOKEN-<none>}\" >>"+shellQ(log+".env")+
		"\n[ -n \"${CTX_FAKE_FAIL-}\" ] && exit 3"+
		// CTX_FAKE_SIGINT=pid: interrupt that shell, once (or every prompt would).
		"\nif [ -n \"${CTX_FAKE_SIGINT-}\" ] && mkdir "+shellQ(log+".sigint")+" 2>/dev/null; then kill -INT \"$CTX_FAKE_SIGINT\"; sleep 1; exit 1; fi"+
		"\nexec "+shellQ(core)+" \"$@\"\n")
	glue := filepath.Join(td, "glue."+shell)
	hookWrite(t, glue, activateScript(shell, fake))
	env = append(env,
		"TD="+td, "GLUE="+glue, "FAKE="+fake, "LOG="+log, "CTX_ROOT="+repoRoot,
		"CTX_CORE_BIN="+core, "CTX_AUTOSWITCH_NOTIFY=0", "EXPECT_SECRET="+hookSecret,
	)
	return env, fake, log
}

// hookEnvTokens are the __CTX_SH_GH_TOKEN values the fake ctx-core got per
// hook-env run.
func hookEnvTokens(t *testing.T, td string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(td, "core.log.env"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
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
				if c.check != nil {
					c.check(t, td)
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

// TestActivateFish sources the fish glue in real fish (when installed): the
// builtin test supports -nt; one handler after sourcing twice; the legacy
// functions gone; the guard: no call on unchanged prompts, one call when the
// stamp is missing, the config or the profiles dir is newer; silence without
// ctx-core; the shell variables passed presence-encoded; the wrapper.
func TestActivateFish(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	t.Parallel()
	td := t.TempDir()
	env, _, _ := activateFixture(t, td, "fish")
	script := `
function fail; printf 'FAIL: %s\n' "$argv"; exit 1; end
function calls; if test -f $LOG; wc -l < $LOG | string trim; else; echo 0; end; end
function delta -a want what
    set -l n (calls)
    test (math $n - $base) = $want; or fail "$what: "(math $n - $base)" ctx-core calls, want $want"
    set -g base $n
end
function prompt; emit fish_postexec; end
cd $TD
touch -t 200001010000 old; touch new
test new -nt old; or fail "fish test: no -nt"
test old -nt new; and fail "fish test -nt: old newer"
fish --version
CTX_CORE=off source $CTX_ROOT/lib/ctx_autoswitch.fish
source $GLUE
set -l h1 (functions --handlers | string match '*_ctx_hook*' | count)
source $GLUE
set -l h2 (functions --handlers | string match '*_ctx_hook*' | count)
test $h1 -ge 1; and test $h1 = $h2; or fail "handlers: $h1 then $h2"
functions -q _ctx_profile_autoswitch; and fail "legacy hook left"
functions -q _ctx_profile_autoswitch_postexec; and fail "legacy postexec left"
set -g base (calls)
cd w/a; prompt
test "$CTX_ACTIVE_PROFILE" = a; or fail "not entered: $CTX_ACTIVE_PROFILE"
delta 1 "enter"
for i in (seq 50); prompt; end
delta 0 "50 unchanged prompts"
rm -f $__CTX_STAMP; prompt; delta 1 "stamp missing"
test -e $__CTX_STAMP; or fail "stamp not recreated"
prompt; delta 0 "after the stamp is back"
touch -t 200001010000 $__CTX_STAMP; prompt; delta 1 "config newer"
prompt; delta 0 "after config"
touch -t 200001010000 $CTX_DIR/config; touch -t 200101010000 $__CTX_STAMP
prompt; delta 1 "profiles dir newer"
prompt; delta 0 "after profiles"
set -g GH_TOKEN 'x
y'
rm -f $__CTX_STAMP; prompt
set -e GH_TOKEN; rm -f $__CTX_STAMP; prompt
set -g GH_TOKEN ''; rm -f $__CTX_STAMP; prompt
set -e GH_TOKEN
mv $FAKE $FAKE.gone; cd ../b; prompt; rm -f $__CTX_STAMP; prompt
mv $FAKE.gone $FAKE
set -g base (calls)
ctx -q use b; or fail "use b"
test "$CTX_ACTIVE_PROFILE" = b; or fail "wrapper"
delta 1 "use runs the baked ctx-core"
grep -qx 'use --eval fish b' $LOG; or fail "log: "(cat $LOG)
`
	cmd := exec.Command(fish, "--no-config", "-c", script)
	cmd.Dir = td
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || stderr.Len() > 0 {
		t.Fatalf("fish: %v\nstdout: %s\nstderr: %s", err, out, stderr.String())
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
	got := hookEnvTokens(t, td)
	// first source, cd, stamp, config, profiles: GH_TOKEN unset; then the
	// newline value, unset, empty.
	if len(got) < 3 || strings.Join(got[len(got)-3:], "|") != "=x\ny||=" {
		t.Errorf("__CTX_SH_GH_TOKEN per run = %q", got)
	}
	for _, v := range got {
		if v == "<none>" {
			t.Errorf("a run without __CTX_SH_GH_TOKEN: %q", got)
		}
	}
}

// TestActivateGuardForkFree keeps the guard builtin-only: the hook's text up
// to the ctx-core check has no command substitution.
func TestActivateGuardForkFree(t *testing.T) {
	for _, c := range []struct{ shell, start, end string }{
		{"bash", "_ctx_hook() {", "[[ -x "},
		{"zsh", "_ctx_hook() {", "[[ -x "},
		{"fish", "function _ctx_hook ", "test -x "},
	} {
		g := activateScript(c.shell, "/c/ctx-core")
		i := strings.Index(g, c.start)
		j := strings.Index(g[i:], c.end)
		if i < 0 || j < 0 {
			t.Fatalf("%s: guard section not found", c.shell)
		}
		guard := g[i : i+j]
		body := strings.TrimPrefix(guard, c.start)
		// Command substitution ($(, backticks, fish's parentheses) or a pipe.
		bad := regexp.MustCompile("\\$\\(|`|[^|]\\|[^|]")
		if c.shell == "fish" {
			bad = regexp.MustCompile("[()`]|[^|]\\|[^|]")
		}
		if m := bad.FindString(body); m != "" {
			t.Errorf("%s guard contains %q:\n%s", c.shell, m, guard)
		}
	}
}

// interactiveShell runs script on the stdin of an interactive shell, which
// runs the prompt hooks before each line as a terminal session does, and
// returns stdout (prompts and job-control notes go to stderr).
func interactiveShell(t *testing.T, env []string, td string, argv []string, script string) string {
	t.Helper()
	cmd := exec.Command(argv[0], append(append([]string{}, argv[1:]...), "-i")...)
	cmd.Dir = td
	cmd.Env = append(append([]string{}, env...), "PS1=", "PROMPT=", "RPROMPT=")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Logf("%s -i: %v (stderr %q)", argv[0], err, stderr.String())
	}
	return string(out)
}

// TestActivateErrexit runs the glue in interactive shells with errexit and
// ERR traps: sourcing succeeds, `false && true` at the prompt does not end
// the shell, the ERR trap / TRAPZERR fires once per failing command, and a
// later PROMPT_COMMAND entry still sees the command's status (bash).
func TestActivateErrexit(t *testing.T) {
	if testing.Short() {
		t.Skip("runs interactive shells")
	}
	for key, argv := range activateShells(t) {
		sh := shellOf(key)
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			td := t.TempDir()
			env, _, _ := activateFixture(t, td, sh)
			var exitScript, trapScript string
			if sh == "zsh" {
				exitScript = `setopt err_exit
source "$GLUE"
echo M-sourced
cd w/a
false && true
echo M-alive=$? active=$CTX_ACTIVE_PROFILE
`
				trapScript = `source "$GLUE"
TRAPZERR() { echo M-ZERR; }
cd w/a
false
echo M-after
`
			} else {
				exitScript = `cd "$TD"
seen() { echo "M-seen=$?"; }
PROMPT_COMMAND=seen
set -e
source "$GLUE"
echo M-sourced
cd w/a
false && true
echo M-alive active=$CTX_ACTIVE_PROFILE
`
				trapScript = `cd "$TD"
source "$GLUE"
trap 'echo M-ERR' ERR
cd w/a
false
echo M-after
`
			}
			out := interactiveShell(t, env, td, argv, `cd "$TD"`+"\n"+exitScript)
			for _, want := range []string{"M-sourced", "M-alive", "active=a"} {
				if !strings.Contains(out, want) {
					t.Errorf("errexit session: no %q in %q", want, out)
				}
			}
			if sh == "zsh" && !strings.Contains(out, "M-alive=1") {
				t.Errorf("zsh $? after precmd: %q", out)
			}
			if sh == "bash" && !strings.Contains(out, "M-seen=1") {
				t.Errorf("a later PROMPT_COMMAND entry did not see status 1: %q", out)
			}
			out = interactiveShell(t, env, td, argv, `cd "$TD"`+"\n"+trapScript)
			if n := strings.Count(out, "M-ERR") + strings.Count(out, "M-ZERR"); n != 1 || !strings.Contains(out, "M-after") {
				t.Errorf("ERR trap fired %d times (want 1): %q", n, out)
			}
			// A failing ctx-core under errtrace (bash set -E) fires no ERR trap
			// (bash 3.2 ran it inside the substitution); zsh TRAPZERR likewise.
			failScript := `cd "$TD"
set -E
trap 'echo M-ERR' ERR
source "$GLUE"
export CTX_FAKE_FAIL=1
cd w/a
echo M-after
`
			if sh == "zsh" {
				failScript = `cd "$TD"
TRAPZERR() { echo M-ZERR; }
source "$GLUE"
export CTX_FAKE_FAIL=1
cd w/a
echo M-after
`
			}
			out = interactiveShell(t, env, td, argv, failScript)
			if n := strings.Count(out, "M-ERR") + strings.Count(out, "M-ZERR"); n != 0 || !strings.Contains(out, "M-after") {
				t.Errorf("failing ctx-core fired the ERR trap %d times: %q", n, out)
			}
		})
	}
}

// TestActivateBashPreexec runs the glue with the real bash-preexec 0.5.0
// (testdata, vendored) in interactive bash, sourced before and after the
// glue: one registration, first in precmd_functions (prompt tools that come
// later see this prompt's CTX_PROMPT_*). Not under errexit: bash-preexec 0.5
// itself ends a set -e shell after a failed command (__bp_set_ret_value).
func TestActivateBashPreexec(t *testing.T) {
	if testing.Short() {
		t.Skip("runs interactive bash")
	}
	bp, err := filepath.Abs("testdata/bash-preexec/bash-preexec.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Each setup line is one rc file: the hook runs only at the prompts after it.
	scripts := map[string]string{
		"bash-preexec first": `source "$BP"; tool() { echo "M-tool show=${CTX_PROMPT_SHOW-unset}"; }; precmd_functions+=(tool); source "$GLUE"
`,
		// bash-preexec's documented setup: sourced last, after the glue.
		"glue first": `source "$GLUE"; source "$BP"; tool() { echo "M-tool show=${CTX_PROMPT_SHOW-unset}"; }; precmd_functions+=(tool)
`,
		// The rc sourced again in the running shell (both orders).
		"re-sourced": `source "$GLUE"; source "$BP"; tool() { echo "M-tool show=${CTX_PROMPT_SHOW-unset}"; }; precmd_functions+=(tool); source "$GLUE"; source "$BP"; source "$GLUE"
`,
	}
	withArrayPromptCommand(scripts)
	for key, argv := range activateShells(t) {
		if shellOf(key) != "bash" {
			continue
		}
		for name, setup := range scripts {
			t.Run(name+"/"+key, func(t *testing.T) {
				t.Parallel()
				if strings.HasSuffix(name, " array") && !bashArrayPromptCommand(t, argv) {
					t.Skip("needs bash 5.1+")
				}
				td := t.TempDir()
				env, _, log := activateFixture(t, td, "bash")
				env = append(env, "BP="+bp)
				out := interactiveShell(t, env, td, argv, `cd "$TD"
`+setup+`cd w/a; echo M-mark
_n=0; for _f in "${precmd_functions[@]}"; do [ "$_f" = _ctx_hook ] && _n=$((_n+1)); done; echo "M-pf0=${precmd_functions[0]} n=$_n"
echo "M-pc=$(declare -p PROMPT_COMMAND | grep -c _ctx_hook)"
false
echo M-alive active=$CTX_ACTIVE_PROFILE
`)
				for _, want := range []string{"M-pf0=_ctx_hook n=1", "M-pc=0", "M-alive active=a"} {
					if !strings.Contains(out, want) {
						t.Errorf("no %q in %q", want, out)
					}
				}
				// The first prompt after `cd w/a` already shows the profile to the tool.
				i := strings.Index(out, "M-mark\n")
				if i < 0 || !strings.HasPrefix(out[i+len("M-mark\n"):], "M-tool show=1\n") {
					t.Errorf("tool did not see this prompt's CTX_PROMPT_SHOW: %q", out)
				}
				if strings.HasSuffix(name, " array") && !strings.Contains(out[max(i, 0):], "M-user") {
					t.Errorf("the user's PROMPT_COMMAND element no longer runs: %q", out)
				}
				b, _ := os.ReadFile(log)
				if n := strings.Count(string(b), "hook-env"); n > 3 {
					t.Errorf("%d hook-env runs for 2 cds:\n%s", n, b)
				}
			})
		}
	}
}

// TestActivatePreservedPromptCommand re-sources the glue after starship's
// preserve mode (it moves PROMPT_COMMAND to _PRESERVED_PROMPT_COMMAND and
// runs it from starship_precmd): the hook stays registered once.
func TestActivatePreservedPromptCommand(t *testing.T) {
	for key, argv := range activateShells(t) {
		if shellOf(key) != "bash" {
			continue
		}
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			td := t.TempDir()
			env, _, _ := activateFixture(t, td, "bash")
			cmd := exec.Command(argv[0], append(append([]string{}, argv[1:]...), "-c", activatePrelude+`
PROMPT_COMMAND=user1
src
_PRESERVED_PROMPT_COMMAND=$PROMPT_COMMAND
starship_precmd() { eval "$_PRESERVED_PROMPT_COMMAND"; }
PROMPT_COMMAND=starship_precmd
src
[ "$_PRESERVED_PROMPT_COMMAND" = user1 ] || fail "preserved: $_PRESERVED_PROMPT_COMMAND"
[ "$PROMPT_COMMAND" = '_ctx_hook keep && :;starship_precmd' ] || fail "PROMPT_COMMAND=$PROMPT_COMMAND"
`)...)
			cmd.Dir = td
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
				t.Errorf("%v: %s", err, out)
			}
		})
	}
}

// arrayPromptCommand makes PROMPT_COMMAND an array with a user element.
const arrayPromptCommand = `PROMPT_COMMAND=('echo M-user'); `

// withArrayPromptCommand adds an " array" variant of each setup, run with
// an array PROMPT_COMMAND (bash 5.1+) set before it.
func withArrayPromptCommand(scripts map[string]string) {
	names := make([]string, 0, len(scripts))
	for name := range scripts {
		names = append(names, name)
	}
	for _, name := range names {
		scripts[name+" array"] = arrayPromptCommand + scripts[name]
	}
}

// bashArrayPromptCommand reports whether the bash at argv runs an array
// PROMPT_COMMAND (5.1+).
func bashArrayPromptCommand(t *testing.T, argv []string) bool {
	t.Helper()
	out, err := exec.Command(argv[0], "-c", `(( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1) )) && echo yes`).Output()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.TrimSpace(string(out)) == "yes"
}

// TestActivateBashPreexecDelayed sources bash-preexec with __bp_delay_install,
// before or after the glue: until the user installs it nothing runs
// precmd_functions, so the hook stays in PROMPT_COMMAND and keeps working;
// after __bp_install_after_session_init it moves first into precmd_functions.
func TestActivateBashPreexecDelayed(t *testing.T) {
	if testing.Short() {
		t.Skip("runs interactive bash")
	}
	bp, err := filepath.Abs("testdata/bash-preexec/bash-preexec.sh")
	if err != nil {
		t.Fatal(err)
	}
	const tool = `tool() { echo "M-tool prof=${CTX_PROMPT_PROFILE-}"; }; precmd_functions+=(tool)`
	scripts := map[string]string{
		"glue first":         `source "$GLUE"; __bp_delay_install=1; source "$BP"; ` + tool + "\n",
		"bash-preexec first": `__bp_delay_install=1; source "$BP"; ` + tool + `; source "$GLUE"` + "\n",
	}
	withArrayPromptCommand(scripts)
	// Only bash-preexec's exact install string means an install is pending.
	scripts["user command naming __bp_install"] = `PROMPT_COMMAND='echo M-__bp_install'; source "$GLUE"; __bp_delay_install=1; source "$BP"; ` + tool + "\n"
	for key, argv := range activateShells(t) {
		if shellOf(key) != "bash" {
			continue
		}
		for name, setup := range scripts {
			t.Run(name+"/"+key, func(t *testing.T) {
				t.Parallel()
				array := strings.HasSuffix(name, " array")
				if array && !bashArrayPromptCommand(t, argv) {
					t.Skip("needs bash 5.1+")
				}
				td := t.TempDir()
				env, _, _ := activateFixture(t, td, "bash")
				env = append(env, "BP="+bp)
				out := interactiveShell(t, env, td, argv, `cd "$TD"
`+setup+`cd w/a
echo "M-before show=$CTX_PROMPT_SHOW prof=${CTX_PROMPT_PROFILE-} active=$CTX_ACTIVE_PROFILE"
__bp_install_after_session_init
cd ../b; echo M-mark
_n=0; for _f in "${precmd_functions[@]}"; do [ "$_f" = _ctx_hook ] && _n=$((_n+1)); done; echo "M-pf0=${precmd_functions[0]} n=$_n"
echo "M-pc=$(declare -p PROMPT_COMMAND | grep -c _ctx_hook)"
echo "M-after active=$CTX_ACTIVE_PROFILE"
`)
				for _, want := range []string{"M-before show=1 prof=a active=a", "M-pf0=_ctx_hook n=1", "M-pc=0", "M-after active=b"} {
					if !strings.Contains(out, want) {
						t.Errorf("no %q in %q", want, out)
					}
				}
				i := strings.Index(out, "M-mark\n")
				if i < 0 || !strings.HasPrefix(out[i+len("M-mark\n"):], "M-tool prof=b\n") {
					t.Errorf("tool did not see this prompt's CTX_PROMPT_PROFILE after the install: %q", out)
				}
				if array && !strings.Contains(out[max(i, 0):], "M-user") {
					t.Errorf("the user's PROMPT_COMMAND element no longer runs: %q", out)
				}
			})
		}
	}
}

// TestActivateInterruptKeepsErrtrace interrupts hook-env (the fake ctx-core
// sends SIGINT to the shell): the shell keeps errtrace (set -E) and its ERR
// trap, since the hook never changes either.
func TestActivateInterruptKeepsErrtrace(t *testing.T) {
	if testing.Short() {
		t.Skip("runs interactive bash")
	}
	for key, argv := range activateShells(t) {
		if shellOf(key) != "bash" {
			continue
		}
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			td := t.TempDir()
			env, _, log := activateFixture(t, td, "bash")
			out := interactiveShell(t, env, td, argv, `cd "$TD"
set -E
trap 'echo M-ERR' ERR
source "$GLUE"
export CTX_FAKE_SIGINT=$$
cd w/a
unset CTX_FAKE_SIGINT
echo "M-flags=$-"
trap -p ERR | sed 's/^/M-trap /'
`)
			if _, err := os.Stat(log + ".sigint"); err != nil {
				t.Fatalf("hook-env was not interrupted: %q", out)
			}
			i := strings.Index(out, "M-flags=")
			if i < 0 || !strings.Contains(strings.SplitN(out[i:], "\n", 2)[0], "E") {
				t.Errorf("errtrace lost after an interrupted hook-env: %q", out)
			}
			if !strings.Contains(out, "M-trap trap -- 'echo M-ERR' ERR") {
				t.Errorf("ERR trap lost: %q", out)
			}
		})
	}
}
