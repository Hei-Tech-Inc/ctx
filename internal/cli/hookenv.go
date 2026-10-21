package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/hookenv"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
	"github.com/Hei-Tech-Inc/ctx/internal/shquote"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

const hookEnvUsage = "Usage: ctx hook-env bash|zsh|fish"

// HookShellVars maps the variables the activate glue passes to hook-env to
// the shell variables they carry. The legacy hook read these as shell
// variables, which need not be exported (CTX_AUTOSWITCH_NOTIFY=1 in a
// .zshrc), so the glue passes each one explicitly in the command's
// environment: "" when the shell variable is unset, "=" followed by the value
// when it is set (bash/zsh: VAR_SH="${VAR+=$VAR}"). When the glue does not
// pass one, hook-env falls back to the exported value.
var HookShellVars = [][2]string{
	{"__CTX_SH_NOTIFY", "CTX_AUTOSWITCH_NOTIFY"},
	{"__CTX_SH_GH_TOKEN", "GH_TOKEN"},
	{"__CTX_SH_ACTIVE", "CTX_ACTIVE_PROFILE"},
	{"__CTX_SH_GH_PROFILE", "_CTX_GH_PROFILE"},
}

// stampsDir holds one stamp file per shell (see HookEnv).
const stampsDir = "hook-stamps"

// stampMaxAge is how long a stamp nobody touched survives. A shell whose stamp
// was removed gets a new one on its next prompt (the glue's guard runs
// hook-env when the stamp file is missing).
const stampMaxAge = 30 * 24 * time.Hour

// HookEnv runs `ctx hook-env bash|zsh|fish`: one run of the autoswitch hook
// for the calling shell. stdout is shell code to eval; notify lines go to
// stderr. The calling contract (what activate's glue passes):
//
//   - PWD: the shell's $PWD (exported by every shell); else the working dir.
//   - __CTX_STATE: the shell-local state from the previous run ("" in a new
//     shell); hook-env prints the next one as a shell-local assignment.
//   - __CTX_STAMP: the shell-local path of this shell's stamp file ("" in a
//     new shell); hook-env creates one under $CTX_DIR/hook-stamps, touches
//     it after every run that read the config, and prints the path when it
//     changes. The glue's guard calls hook-env only when $PWD changed, the
//     stamp is missing, or $CTX_DIR/config is newer than it.
//   - HookShellVars: the shell variables the hook reads.
//
// Every variable of the contract is removed from the environment before the
// use runs, so tools never see them.
func HookEnv(t tui.Term, args []string) int {
	u := ui{out: t.Err, err: t.Err}
	if len(args) != 1 || (args[0] != "bash" && args[0] != "zsh" && args[0] != "fish") {
		u.error(hookEnvUsage)
		return 2
	}
	shell := args[0]

	shellVars := map[string]*string{}
	for _, pv := range HookShellVars {
		raw, passed := os.LookupEnv(pv[0])
		_ = os.Unsetenv(pv[0])
		if !passed {
			continue
		}
		if v, set := strings.CutPrefix(raw, "="); set {
			shellVars[pv[1]] = &v
		} else {
			shellVars[pv[1]] = nil
		}
	}
	stateRaw, stampIn := os.Getenv("__CTX_STATE"), os.Getenv("__CTX_STAMP")
	_ = os.Unsetenv("__CTX_STATE")
	_ = os.Unsetenv("__CTX_STAMP")

	pwd := os.Getenv("PWD")
	if !filepath.IsAbs(pwd) {
		pwd, _ = os.Getwd()
	}
	prev, prevOK := hookenv.Decode(stateRaw)
	profilesDir := profile.Dir()
	stamp, stampNew := stampPath(stampIn)

	// Fast path: nothing the run reads changed since this shell's last run.
	if prevOK && prev.PWD == pwd && !stampNew {
		if st, err := os.Stat(stamp); err == nil && !newerThan(st.ModTime(), profilesDir) {
			return 0
		}
	}

	var b strings.Builder
	// The legacy hook returns early (no state, no prompt exports) without a
	// profiles dir; the stamp keeps the guard from calling again.
	if st, err := os.Stat(profilesDir); err == nil && st.IsDir() {
		out := hookRun(pwd, prev, shellVars, shell, profilesDir)
		writeHookScript(&b, shell, out)
		for _, l := range out.Notify {
			_, _ = io.WriteString(t.Err, l+"\n")
		}
	}
	touchStamp(stamp, stampNew)
	if stamp != stampIn {
		writeShellLocal(&b, shell, "__CTX_STAMP", stamp)
	}
	_, _ = io.WriteString(t.Out, b.String())
	return 0
}

// hookRun computes the run and performs its side effects in-process, in the
// hook's order: deactivate, the quiet use, then Finish.
func hookRun(pwd string, prev hookenv.State, shellVars map[string]*string, shell, profilesDir string) hookenv.Output {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	for k, v := range shellVars {
		if v == nil {
			delete(env, k)
		} else {
			env[k] = *v
		}
	}
	data, _ := os.ReadFile(ctxconfig.Path())
	cfg := ctxconfig.Parse(data)
	name, wd, _ := resolve.Path(pwd, profilesDir)

	var profiles []profile.Profile
	for _, n := range []string{strings.ReplaceAll(cfg.ActiveLast, "\r", ""), name} {
		if n == "" || (len(profiles) > 0 && profiles[0].Name == n) || !isRegular(filepath.Join(profilesDir, n+".conf")) {
			continue
		}
		if p, err := profile.Load(profilesDir, n); err == nil {
			profiles = append(profiles, *p)
		}
	}

	plan := hookenv.Compute(hookenv.Input{
		PWD: pwd, Prev: prev, Env: env, Cfg: cfg,
		PathProfile: name, PathWD: wd, Profiles: profiles,
		GH: func(user string) (string, error) {
			if tok := GHToken(user); tok != "" {
				return tok, nil
			}
			return "", errors.New("no token")
		},
	})
	// The hook drops the messages of both commands (2>/dev/null).
	quietUI := ui{out: io.Discard, err: io.Discard}
	if plan.Deactivate {
		if _, gitWD, _, ok := deactivateApply(quietUI, true); ok && gitWD != "" {
			unsetGitIdentity(pwd, gitWD, shell == "fish")
		}
	}
	var res hookenv.UseResult
	if plan.Use != nil {
		tm := tui.Term{In: os.Stdin, Out: io.Discard, Err: io.Discard}
		a := &useApply{u: termUI(tm), out: io.Discard, tm: tm,
			opts: UseOpts{Quiet: plan.Use.Quiet, AutoSwitch: plan.Use.AutoSwitch, Shell: shell}}
		code, ok := a.run(plan.Use.Target)
		res.Code = code
		if ok {
			res.Exports = a.exports
		}
	}
	return plan.Finish(res)
}

// unsetGitIdentity is the git half of deactivate's eval'd output: under the
// old profile's WORK_DIR, drop the repo's user.name and user.email. The test
// is the shell's: bash/zsh `[[ $PWD == wd* ]]`, fish `wd/*` or wd itself.
func unsetGitIdentity(pwd, wd string, fish bool) {
	under := strings.HasPrefix(pwd, wd)
	if fish {
		under = pwd == wd || strings.HasPrefix(pwd, wd+"/")
	}
	if !under {
		return
	}
	for _, k := range []string{"user.name", "user.email"} {
		cmd := exec.Command("git", "config", "--unset", k)
		cmd.Dir = pwd
		_ = cmd.Run()
	}
}

// writeHookScript renders out for the shell: unsets, exports, then the state.
func writeHookScript(b *strings.Builder, shell string, out hookenv.Output) {
	for _, k := range out.Unsets {
		if shell == "fish" {
			b.WriteString("set -e " + k + "\n")
		} else {
			b.WriteString("unset " + k + "\n")
		}
	}
	for _, kv := range out.Exports {
		if shell == "fish" {
			// Like ctx use --eval fish (FormatEnv): trailing newlines dropped.
			b.WriteString("set -gx " + kv[0] + " " + fishQuote(strings.TrimRight(kv[1], "\n")) + "\n")
		} else {
			b.WriteString("export " + kv[0] + "=" + hookQuote(kv[1]) + "\n")
		}
	}
	writeShellLocal(b, shell, "__CTX_STATE", out.State.Encode())
}

// writeShellLocal assigns a shell variable that is not exported.
func writeShellLocal(b *strings.Builder, shell, k, v string) {
	if shell == "fish" {
		b.WriteString("set -gu " + k + " " + fishQuote(v) + "\n")
	} else {
		b.WriteString(k + "=" + hookQuote(v) + "\n")
	}
}

// hookQuote is printf %q (shquote), safe in bash and zsh: a leading "=" is
// escaped too, which zsh would otherwise expand to a command path.
func hookQuote(v string) string {
	q := shquote.Quote(v)
	if strings.HasPrefix(q, "=") {
		q = `\` + q
	}
	return q
}

// fishQuote single-quotes v for fish, where only \\ and \' are escapes.
func fishQuote(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// stampPath returns this shell's stamp file: the one the glue passed when it
// is a stamp under $CTX_DIR (CTX_DIR may have changed since), else a new one.
func stampPath(in string) (path string, isNew bool) {
	dir := filepath.Join(ctxconfig.Dir(), stampsDir)
	if in != "" && filepath.Dir(in) == dir && validStampID(filepath.Base(in)) {
		return in, false
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	return filepath.Join(dir, hex.EncodeToString(id)), true
}

func validStampID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

// newerThan reports whether the config, the profiles dir or a profile was
// modified after t.
func newerThan(t time.Time, profilesDir string) bool {
	paths := []string{ctxconfig.Path(), profilesDir}
	if ents, err := os.ReadDir(profilesDir); err == nil {
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".conf") {
				paths = append(paths, filepath.Join(profilesDir, e.Name()))
			}
		}
	}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(t) {
			return true
		}
	}
	return false
}

// touchStamp sets the stamp's mtime to now (creating it); a new stamp also
// sweeps the stamps of shells gone for stampMaxAge. Failures are ignored: a
// missing stamp only makes the guard call hook-env again.
func touchStamp(path string, isNew bool) {
	dir := filepath.Dir(path)
	if isNew {
		_ = os.MkdirAll(dir, 0o700)
		if ents, err := os.ReadDir(dir); err == nil {
			cutoff := time.Now().Add(-stampMaxAge)
			for _, e := range ents {
				if info, err := e.Info(); err == nil && validStampID(e.Name()) && info.ModTime().Before(cutoff) {
					_ = os.Remove(filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_ = f.Close()
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}
