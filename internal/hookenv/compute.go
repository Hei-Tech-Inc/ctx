// Package hookenv decides what the autoswitch hook does on one prompt. It
// ports the decision logic of _ctx_profile_autoswitch (lib/ctx_autoswitch.bash)
// as a pure function: Compute reads only its Input and returns the shell
// changes, the notify lines and the side effects (ctx deactivate / ctx use)
// for the caller (ctx hook-env) to perform.
package hookenv

import (
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// Input is everything one hook run looks at.
type Input struct {
	// PWD is the shell's $PWD (logical, symlinks kept).
	PWD string
	// Prev is the decoded __CTX_STATE; the zero State for a fresh shell.
	Prev State
	// Env is the shell's exported environment (HOME, CTX_ACTIVE_PROFILE,
	// CTX_AUTOSWITCH_NOTIFY, GH_TOKEN, _CTX_GH_PROFILE, CTX_PROMPT_*, …).
	Env map[string]string
	// Cfg is $CTX_DIR/config.
	Cfg *ctxconfig.Config
	// PathProfile and PathWD are resolve.Path(PWD, profiles dir): the
	// hook's path_profile and path_wd (or best_work_dir). Empty: no profile.
	PathProfile, PathWD string
	// Profiles holds the profiles that could be loaded; at least the active
	// one (Cfg) and PathProfile when they exist. A missing one is deleted.
	Profiles []profile.Profile
	// GH is `gh auth token -u user`; an error (gh missing, not logged in)
	// means no token. nil: never look one up.
	GH func(user string) (string, error)
}

// UseCall is the `ctx use <Target>` the hook runs in-process, with the hook's
// options (CTX_QUIET=1 CTX_AUTO_SWITCH=1: no gh switch, the manual lock kept
// when re-applying a locked profile).
type UseCall struct {
	Target            string
	Quiet, AutoSwitch bool
}

// UseResult is what that `ctx use` did: the variables it exported (applied to
// the shell even when it fails, like the hook's eval) and its exit code.
type UseResult struct {
	Exports [][2]string
	Code    int
}

// Output is one hook run's result. Exports and Unsets are the net change to
// the shell (disjoint; apply in any order) after every step, including the
// pending Use when Complete has folded it in.
//
// The caller runs the side effects in the hook's order: (if Deactivate) ctx
// deactivate's side effects, then (if Use) the quiet use, whose messages are
// dropped, then Complete with its result exactly once. Complete's Notify holds
// every line of the run (it may add the failure line), so the caller prints
// Notify after Complete.
type Output struct {
	Exports [][2]string
	Unsets  []string
	// Notify lines go to stderr, one per line, exactly as the hook's
	// `echo -e "\033[2m…\033[0m" >&2` prints them (without the newline).
	Notify []string
	// State is the next __CTX_STATE.
	State State
	// Use is the `ctx use` to run (nil: none). Until Complete is called,
	// Exports, Unsets and State do not include it.
	Use *UseCall
	// Deactivate asks for ctx deactivate's side effects (clear the
	// machine-wide activation, drop the ssh key it added, unset the git
	// identity under WORK_DIR). Its variables are already in Unsets.
	Deactivate bool

	pend *pending
}

// pending is a run waiting for its Use result.
type pending struct {
	in      Input
	v       *view
	target  string
	notify  bool
	always  bool // apply CTX_ACTIVE_PROFILE etc. even when use fails
	keys    []string
	show    bool
	pprof   string
	proot   string
	notifyL []string
}

const (
	dim = "\x1b[2m"
	rst = "\x1b[0m"
)

// Variables ctx deactivate always unsets (_ctx_deactivate_emit_unsets).
var deactivateFixed = []string{
	"AWS_PROFILE", "AZURE_SUBSCRIPTION", "AZURE_TENANT", "GCLOUD_PROJECT", "GCP_PROJECT", "GCP_ACCOUNT",
	"CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER",
	"CTX_PROMPT_SHOW", "CTX_PROMPT_PROFILE", "CTX_PROMPT_WORK_DIR",
}

// Compute is one run of _ctx_profile_autoswitch (map §1.3–1.5).
func Compute(in Input) Output {
	cfg := in.Cfg
	if cfg == nil {
		cfg = ctxconfig.Parse(nil)
	}
	// The hook's read loop: last active= line, whole value, CR stripped.
	current := stripCR(cfg.ActiveLast)
	manual := strings.EqualFold(stripCR(cfg.Last["active_source"]), "manual")
	anchor := ""
	if raw := stripCR(cfg.Last["manual_pwd"]); manual && raw != "" {
		anchor = ctxconfig.DecodeManualPWD(raw)
	}
	target := in.PathProfile
	if manual && anchor != "" && in.PWD == anchor {
		target = current // the manual lock: keep the hand-picked profile here
	}

	// Prompt exports run on every call, before the cache guard.
	pcfg := *cfg
	pcfg.Active = current
	show, pprof, proot := PromptScope(in.PWD, in.Env["HOME"], &pcfg, in.PathProfile, in.PathWD)

	v := newView(in.Env)
	if in.Prev.PWD == in.PWD && in.Prev.Target == target {
		promptOps(v, show, pprof, proot)
		return Output{Exports: v.exports(), Unsets: v.unsets(), State: in.Prev}
	}

	p := &pending{in: in, v: v, target: target, show: show, pprof: pprof, proot: proot}
	p.notify = cfg.NotifyEnabled(in.Env["CTX_AUTOSWITCH_NOTIFY"])
	out := Output{}
	say := func(s string) {
		if p.notify {
			p.notifyL = append(p.notifyL, dim+s+rst)
		}
	}
	// Leaving or re-applying drops what this shell got for the previous target (R2).
	dropPrev := func() {
		if in.Prev.Target != "" {
			for _, k := range in.Prev.Keys {
				v.unset(k)
			}
		}
	}
	deactivate := func() {
		out.Deactivate = true
		for _, k := range deactivateNames(findProfile(in.Profiles, current)) {
			v.unset(k)
		}
	}

	if target == "" {
		if current != "" {
			say("[ctx] ← " + current)
			deactivate()
		}
		dropPrev()
		v.unset("CTX_ACTIVE_PROFILE")
		v.unset("CTX_ACTIVATION_TRIGGER")
		p.ghToken("")
		promptOps(v, show, pprof, proot)
		out.Exports, out.Unsets, out.Notify = v.exports(), v.unsets(), p.notifyL
		out.State = State{PWD: in.PWD}
		return out
	}

	switch {
	case current != "" && current != target:
		say("[ctx] ← " + current + " → " + target)
		deactivate()
	case current == "":
		say("[ctx] → " + target)
	case in.Env["CTX_ACTIVE_PROFILE"] != target:
		// Active machine-wide (e.g. a new terminal): this shell still needs the exports.
		p.always = true
	default:
		// Applied already: no subprocess.
		// Nothing is unset here, so whatever the previous target exported
		// may still be set: keep tracking it.
		if in.Prev.Target != "" {
			p.keys = append(p.keys, in.Prev.Keys...)
		}
		p.finish(true)
		out.Exports, out.Unsets, out.Notify, out.State = v.exports(), v.unsets(), p.notifyL, p.state()
		return out
	}
	dropPrev()
	out.Use = &UseCall{Target: target, Quiet: true, AutoSwitch: true}
	out.Notify = append([]string(nil), p.notifyL...)
	out.pend = p
	return out
}

// Complete folds the pending Use's result into o: its exports, then (when it
// succeeded, or always for a new terminal) CTX_ACTIVE_PROFILE,
// CTX_ACTIVATION_TRIGGER=auto and GH_TOKEN; on failure the notify line. A
// failed use is not retried: State records the target either way. Without a
// pending Use it returns o unchanged.
func (o Output) Complete(r UseResult) Output {
	p := o.pend
	if p == nil {
		return o
	}
	for _, kv := range r.Exports {
		p.v.set(kv[0], kv[1])
		p.keys = append(p.keys, kv[0])
	}
	ok := r.Code == 0
	if !ok && !p.always && p.notify {
		p.notifyL = append(p.notifyL, dim+"[ctx] ctx use "+p.target+" failed — run: ctx use "+p.target+rst)
	}
	p.finish(ok || p.always)
	o.Exports, o.Unsets, o.Notify, o.State = p.v.exports(), p.v.unsets(), p.notifyL, p.state()
	o.pend = nil
	return o
}

// finish is the tail of the target branch: the applied exports and the prompt.
func (p *pending) finish(applied bool) {
	if applied {
		p.v.set("CTX_ACTIVE_PROFILE", p.target)
		p.v.set("CTX_ACTIVATION_TRIGGER", "auto")
		p.keys = append(p.keys, "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER")
		p.ghToken(p.target)
	}
	promptOps(p.v, p.show, p.pprof, p.proot)
}

func (p *pending) state() State {
	return State{PWD: p.in.PWD, Target: p.target, Keys: dedupe(p.keys)}
}

// ghToken is _ctx_as_gh_token: a per-shell GH_TOKEN for the profile's
// GITHUB_USER. _CTX_GH_PROFILE marks a token ctx set; a GH_TOKEN without it
// is the user's and is left alone.
func (p *pending) ghToken(name string) {
	v := p.v
	if owner, ok := v.get("_CTX_GH_PROFILE"); ok {
		if owner == name {
			return
		}
		v.unset("GH_TOKEN")
		v.unset("_CTX_GH_PROFILE")
	}
	if tok, _ := v.get("GH_TOKEN"); name == "" || tok != "" {
		return
	}
	pr := findProfile(p.in.Profiles, name)
	if pr == nil || p.in.GH == nil {
		return
	}
	user := strings.NewReplacer(`"`, "", `'`, "").Replace(pr.GithubUser)
	if user == "" {
		return
	}
	tok, err := p.in.GH(user)
	tok = strings.TrimRight(tok, "\n") // $(…) drops trailing newlines
	if err != nil || tok == "" {
		return
	}
	v.set("GH_TOKEN", tok)
	v.set("_CTX_GH_PROFILE", name)
}

// promptOps is the export half of _ctx_prompt_update_exports.
func promptOps(v *view, show bool, prof, root string) {
	if show {
		v.set("CTX_PROMPT_SHOW", "1")
		v.set("CTX_PROMPT_PROFILE", prof)
		v.set("CTX_PROMPT_WORK_DIR", root)
		return
	}
	v.set("CTX_PROMPT_SHOW", "0")
	v.unset("CTX_PROMPT_PROFILE")
	v.unset("CTX_PROMPT_WORK_DIR")
}

// deactivateNames are the variables ctx deactivate unsets for p (nil: the
// profile is gone, so only the fixed ones).
func deactivateNames(p *profile.Profile) []string {
	var out []string
	isSep := func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }
	if p != nil {
		for _, k := range strings.FieldsFunc(p.SecretKeysRaw, isSep) {
			if validKey(k) {
				out = append(out, k)
			}
		}
	}
	out = append(out, deactivateFixed...)
	if p != nil {
		for _, pair := range strings.Split(p.ExtraEnvsRaw, "\n") {
			if k, _, _ := strings.Cut(pair, "="); validKey(k) {
				out = append(out, k)
			}
		}
	}
	return out
}

func findProfile(ps []profile.Profile, name string) *profile.Profile {
	if name == "" {
		return nil
	}
	for i := range ps {
		if ps[i].Name == name {
			return &ps[i]
		}
	}
	return nil
}

func stripCR(s string) string { return strings.ReplaceAll(s, "\r", "") }

func dedupe(keys []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range keys {
		if !seen[k] && validKey(k) {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// view is the shell's environment as the hook's steps change it.
type view struct {
	base  map[string]string
	cur   map[string]string
	order []string
	seen  map[string]bool
}

func newView(env map[string]string) *view {
	cur := make(map[string]string, len(env))
	for k, val := range env {
		cur[k] = val
	}
	return &view{base: env, cur: cur, seen: map[string]bool{}}
}

func (v *view) touch(k string) {
	if !v.seen[k] {
		v.seen[k] = true
		v.order = append(v.order, k)
	}
}

func (v *view) set(k, val string) { v.touch(k); v.cur[k] = val }
func (v *view) unset(k string)    { v.touch(k); delete(v.cur, k) }
func (v *view) get(k string) (string, bool) {
	val, ok := v.cur[k]
	return val, ok
}

// exports are the touched variables whose final value is new.
func (v *view) exports() [][2]string {
	var out [][2]string
	for _, k := range v.order {
		val, ok := v.cur[k]
		if !ok {
			continue
		}
		if old, had := v.base[k]; had && old == val {
			continue
		}
		out = append(out, [2]string{k, val})
	}
	return out
}

// unsets are the touched variables that were set and are now gone.
func (v *view) unsets() []string {
	var out []string
	for _, k := range v.order {
		if _, ok := v.cur[k]; ok {
			continue
		}
		if _, had := v.base[k]; had {
			out = append(out, k)
		}
	}
	return out
}
