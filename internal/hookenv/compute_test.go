package hookenv

import (
	"encoding/base64"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// Notify lines exactly as the legacy hook's echo -e prints them (without the newline).
func nEnter(p string) string     { return "\x1b[2m[ctx] → " + p + "\x1b[0m" }
func nLeave(p string) string     { return "\x1b[2m[ctx] ← " + p + "\x1b[0m" }
func nSwitch(a, b string) string { return "\x1b[2m[ctx] ← " + a + " → " + b + "\x1b[0m" }
func nFail(p string) string {
	return "\x1b[2m[ctx] ctx use " + p + " failed — run: ctx use " + p + "\x1b[0m"
}

var testProfiles = []profile.Profile{
	{Name: "a", WorkDir: "/w/a", GithubUser: "alice", SecretKeysRaw: "SA", ExtraEnvsRaw: "XA=1"},
	{Name: "b", WorkDir: "/w/b"},
	{Name: "n", WorkDir: "/w/a/n", GithubUser: `"nina"`},
}

func cfgOf(lines ...string) *ctxconfig.Config {
	return ctxconfig.Parse([]byte(strings.Join(lines, "\n") + "\n"))
}

func gh(user string) (string, error) {
	if user == "" {
		return "", errors.New("no user")
	}
	return "tok-" + user + "\n", nil
}

// promptA is the prompt env of a shell inside a's WORK_DIR within depth.
func promptIn(p, root string) map[string]string {
	return map[string]string{"CTX_PROMPT_SHOW": "1", "CTX_PROMPT_PROFILE": p, "CTX_PROMPT_WORK_DIR": root}
}

func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{"HOME": "/home/u"}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// activeA is a shell where the hook applied a (with GH token).
var activeA = map[string]string{
	"AWS_PROFILE": "pa", "SA": "secret", "XA": "1", "CTX_ACTIVE_PROFILE": "a",
	"CTX_ACTIVATION_TRIGGER": "auto", "GH_TOKEN": "tok-alice", "_CTX_GH_PROFILE": "a",
}

var keysA = []string{"AWS_PROFILE", "SA", "XA", "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}

type step struct {
	name string
	in   Input
	use  *UseResult // result of the pending use (nil: none expected)

	wantUse        string // target of Output.Use, "" for none
	wantDeactivate bool
	wantNotify     []string
	wantExports    map[string]string
	wantUnsets     []string
	wantState      State
	noProfiles     bool // Input.Profiles is empty: every profile is deleted
}

func TestCompute(t *testing.T) {
	useA := &UseResult{Exports: [][2]string{{"AWS_PROFILE", "pa"}, {"SA", "secret"}, {"XA", "1"}, {"CTX_ACTIVE_PROFILE", "a"}}}
	useB := &UseResult{Exports: [][2]string{{"CTX_ACTIVE_PROFILE", "b"}}}
	useN := &UseResult{Exports: [][2]string{{"AWS_PROFILE", "pn"}, {"CTX_ACTIVE_PROFILE", "n"}}}
	manual := base64.StdEncoding.EncodeToString([]byte("/w/b/q"))
	inB := State{PWD: "/w/b/x", Target: "b", Keys: []string{"CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}}
	envB := map[string]string{"CTX_ACTIVE_PROFILE": "b", "CTX_ACTIVATION_TRIGGER": "auto"}

	cases := []step{
		{
			name: "enter",
			in:   Input{PWD: "/w/a/x", Env: merge(), Cfg: cfgOf("autoswitch_notify=on"), PathProfile: "a", PathWD: "/w/a"},
			use:  useA, wantUse: "a",
			wantNotify:  []string{nEnter("a")},
			wantExports: merge(activeA, promptIn("a", "/w/a"), map[string]string{"HOME": "/home/u"}),
			wantState:   State{PWD: "/w/a/x", Target: "a", Keys: keysA},
		},
		{
			name: "enter notify off by default",
			in:   Input{PWD: "/w/a/x", Env: merge(), Cfg: cfgOf(), PathProfile: "a", PathWD: "/w/a"},
			use:  useA, wantUse: "a",
			wantExports: merge(activeA, promptIn("a", "/w/a")),
			wantState:   State{PWD: "/w/a/x", Target: "a", Keys: keysA},
		},
		{
			name: "leave",
			in: Input{PWD: "/tmp", Prev: State{PWD: "/w/a/x", Target: "a", Keys: keysA},
				Env: merge(activeA, promptIn("a", "/w/a")), Cfg: cfgOf("active=a", "autoswitch_notify=1")},
			wantDeactivate: true,
			wantNotify:     []string{nLeave("a")},
			wantExports:    map[string]string{"CTX_PROMPT_SHOW": "0"},
			wantUnsets: []string{"AWS_PROFILE", "SA", "XA", "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER",
				"GH_TOKEN", "_CTX_GH_PROFILE", "CTX_PROMPT_PROFILE", "CTX_PROMPT_WORK_DIR"},
			wantState: State{PWD: "/tmp"},
		},
		{
			name: "switch a to b",
			in: Input{PWD: "/w/b/x", Prev: State{PWD: "/w/a/x", Target: "a", Keys: keysA},
				Env: merge(activeA, promptIn("a", "/w/a")), Cfg: cfgOf("active=a", "autoswitch_notify=yes"), PathProfile: "b", PathWD: "/w/b"},
			use: useB, wantUse: "b", wantDeactivate: true,
			wantNotify:  []string{nSwitch("a", "b")},
			wantExports: merge(envB, promptIn("b", "/w/b")),
			wantUnsets:  []string{"AWS_PROFILE", "SA", "XA", "GH_TOKEN", "_CTX_GH_PROFILE"},
			wantState:   inB,
		},
		{
			name: "same profile, cd within it",
			in: Input{PWD: "/w/a/y", Prev: State{PWD: "/w/a/x", Target: "a", Keys: keysA},
				Env: merge(activeA, promptIn("a", "/w/a")), Cfg: cfgOf("active=a", "autoswitch_notify=on"), PathProfile: "a", PathWD: "/w/a"},
			wantExports: map[string]string{},
			wantState:   State{PWD: "/w/a/y", Target: "a", Keys: keysA},
		},
		{
			name: "nested WORK_DIR: the longer one wins",
			in: Input{PWD: "/w/a/n/x", Prev: State{PWD: "/w/a/x", Target: "a", Keys: keysA},
				Env: merge(activeA, promptIn("a", "/w/a")), Cfg: cfgOf("active=a"), PathProfile: "n", PathWD: "/w/a/n"},
			use: useN, wantUse: "n", wantDeactivate: true,
			wantExports: map[string]string{"AWS_PROFILE": "pn", "CTX_ACTIVE_PROFILE": "n", "GH_TOKEN": "tok-nina",
				"_CTX_GH_PROFILE": "n", "CTX_PROMPT_PROFILE": "n", "CTX_PROMPT_WORK_DIR": "/w/a/n"},
			wantUnsets: []string{"SA", "XA"},
			wantState:  State{PWD: "/w/a/n/x", Target: "n", Keys: []string{"AWS_PROFILE", "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}},
		},
		{
			name: "manual lock held at manual_pwd",
			in: Input{PWD: "/w/b/q", Prev: inB, Env: merge(envB, map[string]string{"CTX_ACTIVE_PROFILE": "a"}, promptIn("b", "/w/b")),
				Cfg: cfgOf("active=a", "active_source=MANUAL", "manual_pwd="+manual, "autoswitch_notify=on"), PathProfile: "b", PathWD: "/w/b"},
			wantExports: map[string]string{"GH_TOKEN": "tok-alice", "_CTX_GH_PROFILE": "a"},
			wantState:   State{PWD: "/w/b/q", Target: "a", Keys: []string{"CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}},
		},
		{
			name: "manual lock cleared when leaving manual_pwd",
			in: Input{PWD: "/w/b/q/sub", Prev: State{PWD: "/w/b/q", Target: "a", Keys: keysA},
				Env: merge(activeA, promptIn("b", "/w/b")),
				Cfg: cfgOf("active=a", "active_source=manual\r", "manual_pwd="+manual+"\r", "autoswitch_notify=on"), PathProfile: "b", PathWD: "/w/b"},
			use: useB, wantUse: "b", wantDeactivate: true,
			wantNotify:  []string{nSwitch("a", "b")},
			wantExports: map[string]string{"CTX_ACTIVE_PROFILE": "b"},
			wantUnsets:  []string{"AWS_PROFILE", "SA", "XA", "GH_TOKEN", "_CTX_GH_PROFILE"},
			wantState:   State{PWD: "/w/b/q/sub", Target: "b", Keys: []string{"CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}},
		},
		{
			name: "manual_pwd only counts with active_source=manual",
			in: Input{PWD: "/w/b/q", Env: merge(), Cfg: cfgOf("active=a", "active_source=auto", "manual_pwd="+manual),
				PathProfile: "b", PathWD: "/w/b"},
			use: useB, wantUse: "b", wantDeactivate: true,
			wantExports: merge(envB, promptIn("b", "/w/b")),
			wantState:   inB.with("/w/b/q"),
		},
		{
			name: "deleted profile while active (Review Focus 3)", noProfiles: true,
			in: Input{PWD: "/tmp", Prev: State{PWD: "/w/gone", Target: "gone", Keys: []string{"G1", "G2", "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}},
				Env: merge(map[string]string{"G1": "x", "G2": "y", "AWS_PROFILE": "pg", "CTX_ACTIVE_PROFILE": "gone", "CTX_ACTIVATION_TRIGGER": "auto"}),
				Cfg: cfgOf("active=gone")},
			wantDeactivate: true,
			wantExports:    map[string]string{"CTX_PROMPT_SHOW": "0"},
			wantUnsets:     []string{"G1", "G2", "AWS_PROFILE", "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"},
			wantState:      State{PWD: "/tmp"},
		},
		{
			name: "deleted current profile on switch: Prev.Keys and the fixed names unset",
			in: Input{PWD: "/w/b/x", Prev: State{PWD: "/w/gone", Target: "gone", Keys: []string{"G1", "G2", "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}},
				Env: merge(map[string]string{"G1": "x", "G2": "y", "AWS_PROFILE": "pg", "CTX_ACTIVE_PROFILE": "gone", "CTX_ACTIVATION_TRIGGER": "auto"}),
				Cfg: cfgOf("active=gone", "autoswitch_notify=on"), PathProfile: "b", PathWD: "/w/b"},
			use: useB, wantUse: "b", wantDeactivate: true,
			wantNotify:  []string{nSwitch("gone", "b")},
			wantExports: merge(map[string]string{"CTX_ACTIVE_PROFILE": "b"}, promptIn("b", "/w/b")),
			wantUnsets:  []string{"G1", "G2", "AWS_PROFILE"},
			wantState:   inB,
		},
		{
			name: "user-set GH_TOKEN is left alone on enter",
			in:   Input{PWD: "/w/a", Env: merge(map[string]string{"GH_TOKEN": "mine"}), Cfg: cfgOf(), PathProfile: "a", PathWD: "/w/a"},
			use:  useA, wantUse: "a",
			wantExports: merge(map[string]string{"AWS_PROFILE": "pa", "SA": "secret", "XA": "1", "CTX_ACTIVE_PROFILE": "a",
				"CTX_ACTIVATION_TRIGGER": "auto"}, promptIn("a", "/w/a")),
			wantState: State{PWD: "/w/a", Target: "a", Keys: keysA},
		},
		{
			name: "user-set GH_TOKEN is left alone on leave",
			in: Input{PWD: "/tmp", Prev: State{PWD: "/w/a", Target: "a", Keys: keysA},
				Env: merge(map[string]string{"GH_TOKEN": "mine", "CTX_ACTIVE_PROFILE": "a"}), Cfg: cfgOf("active=a")},
			wantDeactivate: true,
			wantExports:    map[string]string{"CTX_PROMPT_SHOW": "0"},
			wantUnsets:     []string{"CTX_ACTIVE_PROFILE"},
			wantState:      State{PWD: "/tmp"},
		},
		{
			name: "env CTX_AUTOSWITCH_NOTIFY=0 beats config on",
			in: Input{PWD: "/w/a", Env: merge(map[string]string{"CTX_AUTOSWITCH_NOTIFY": "0"}), Cfg: cfgOf("autoswitch_notify=on"),
				PathProfile: "a", PathWD: "/w/a"},
			use: useA, wantUse: "a",
			wantExports: merge(activeA, promptIn("a", "/w/a")),
			wantState:   State{PWD: "/w/a", Target: "a", Keys: keysA},
		},
		{
			name: "env CTX_AUTOSWITCH_NOTIFY=' TRUE ' with config off",
			in: Input{PWD: "/w/a", Env: merge(map[string]string{"CTX_AUTOSWITCH_NOTIFY": " TRUE "}), Cfg: cfgOf("autoswitch_notify=off"),
				PathProfile: "a", PathWD: "/w/a"},
			use: useA, wantUse: "a",
			wantNotify:  []string{nEnter("a")},
			wantExports: merge(activeA, promptIn("a", "/w/a")),
			wantState:   State{PWD: "/w/a", Target: "a", Keys: keysA},
		},
		{
			name: "prompt beyond max depth",
			in: Input{PWD: "/w/a/1/2/3", Prev: State{PWD: "/w/a/1/2", Target: "a", Keys: keysA},
				Env: merge(activeA, promptIn("a", "/w/a")), Cfg: cfgOf("active=a"), PathProfile: "a", PathWD: "/w/a"},
			wantExports: map[string]string{"CTX_PROMPT_SHOW": "0"},
			wantUnsets:  []string{"CTX_PROMPT_PROFILE", "CTX_PROMPT_WORK_DIR"},
			wantState:   State{PWD: "/w/a/1/2/3", Target: "a", Keys: keysA},
		},
		{
			name: "prompt depth from config",
			in: Input{PWD: "/w/a/1/2/3", Prev: State{PWD: "/w/a/1/2/3", Target: "a", Keys: keysA},
				Env: merge(activeA), Cfg: cfgOf("active=a", "prompt_workdir_max_depth= 3"), PathProfile: "a", PathWD: "/w/a"},
			wantExports: promptIn("a", "/w/a"),
			wantState:   State{PWD: "/w/a/1/2/3", Target: "a", Keys: keysA},
		},
		{
			name: "prompt extra paths, last entry, ~ expanded, active profile shown",
			in: Input{PWD: "/home/u/e/x", Prev: State{PWD: "/home/u/e/x"}, Env: merge(map[string]string{"CTX_PROMPT_SHOW": "0"}),
				Cfg: cfgOf("active=b", "active=a", "prompt_extra_paths=/nope:~/e/")},
			wantExports: map[string]string{"CTX_PROMPT_SHOW": "1", "CTX_PROMPT_PROFILE": "a", "CTX_PROMPT_WORK_DIR": "/home/u/e"},
			wantState:   State{PWD: "/home/u/e/x"},
		},
		{
			name: "failed use: notify, nothing applied by the hook",
			in:   Input{PWD: "/w/a", Env: merge(), Cfg: cfgOf("autoswitch_notify=on"), PathProfile: "a", PathWD: "/w/a"},
			use:  &UseResult{Exports: [][2]string{{"AWS_PROFILE", "pa"}, {"CTX_ACTIVE_PROFILE", "a"}}, Code: 1}, wantUse: "a",
			wantNotify:  []string{nEnter("a"), nFail("a")},
			wantExports: merge(map[string]string{"AWS_PROFILE": "pa", "CTX_ACTIVE_PROFILE": "a"}, promptIn("a", "/w/a")),
			wantState:   State{PWD: "/w/a", Target: "a", Keys: []string{"AWS_PROFILE", "CTX_ACTIVE_PROFILE"}},
		},
		{
			name: "failed use is not retried",
			in: Input{PWD: "/w/a", Prev: State{PWD: "/w/a", Target: "a", Keys: []string{"CTX_ACTIVE_PROFILE"}},
				Env: merge(promptIn("a", "/w/a")), Cfg: cfgOf("active=a", "autoswitch_notify=on"), PathProfile: "a", PathWD: "/w/a"},
			wantExports: map[string]string{},
			wantState:   State{PWD: "/w/a", Target: "a", Keys: []string{"CTX_ACTIVE_PROFILE"}},
		},
		{
			name: "new terminal: active machine-wide, not in this shell",
			in:   Input{PWD: "/w/a", Env: merge(), Cfg: cfgOf("active=a", "autoswitch_notify=on"), PathProfile: "a", PathWD: "/w/a"},
			use:  &UseResult{Exports: useA.Exports, Code: 2}, wantUse: "a",
			wantExports: merge(activeA, promptIn("a", "/w/a")),
			wantState:   State{PWD: "/w/a", Target: "a", Keys: keysA},
		},
		{
			name: "multiple active= lines: the last one is current",
			in: Input{PWD: "/w/b", Env: merge(map[string]string{"CTX_ACTIVE_PROFILE": "b"}),
				Cfg: cfgOf("active=a", "active=b\r", "autoswitch_notify=on"), PathProfile: "b", PathWD: "/w/b"},
			wantExports: merge(map[string]string{"CTX_ACTIVATION_TRIGGER": "auto"}, promptIn("b", "/w/b")),
			wantState:   State{PWD: "/w/b", Target: "b", Keys: []string{"CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}},
		},
		{
			name:           "fresh shell outside every profile with one active: leaves it",
			in:             Input{PWD: "/tmp", Env: merge(), Cfg: cfgOf("active=a", "autoswitch_notify=on")},
			wantDeactivate: true, wantNotify: []string{nLeave("a")},
			wantExports: map[string]string{"CTX_PROMPT_SHOW": "0"},
			wantState:   State{PWD: "/tmp"},
		},
		{
			name: "switch with GH: token of the old profile replaced",
			in: Input{PWD: "/w/a/n", Prev: State{PWD: "/w/a", Target: "a", Keys: keysA},
				Env: merge(activeA), Cfg: cfgOf("active=a"), PathProfile: "n", PathWD: "/w/a/n"},
			use: useN, wantUse: "n", wantDeactivate: true,
			wantExports: map[string]string{"AWS_PROFILE": "pn", "CTX_ACTIVE_PROFILE": "n", "GH_TOKEN": "tok-nina",
				"_CTX_GH_PROFILE": "n", "CTX_PROMPT_SHOW": "1", "CTX_PROMPT_PROFILE": "n", "CTX_PROMPT_WORK_DIR": "/w/a/n"},
			wantUnsets: []string{"SA", "XA"},
			wantState:  State{PWD: "/w/a/n", Target: "n", Keys: []string{"AWS_PROFILE", "CTX_ACTIVE_PROFILE", "CTX_ACTIVATION_TRIGGER"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := c.in
			in.Profiles = testProfiles
			if c.noProfiles {
				in.Profiles = nil
			}
			in.GH = gh
			plan := Compute(in)
			if plan.State.PWD != in.PWD {
				t.Errorf("Plan.State = %#v, want the run's pwd before Finish", plan.State)
			}
			gotUse := ""
			var res UseResult
			if plan.Use != nil {
				gotUse = plan.Use.Target
				if !plan.Use.Quiet || !plan.Use.AutoSwitch {
					t.Errorf("Use opts = %+v, want quiet auto-switch", *plan.Use)
				}
				if c.use == nil {
					t.Fatalf("unexpected Use %q", gotUse)
				}
				res = *c.use
			}
			if gotUse != c.wantUse {
				t.Errorf("Use = %q, want %q", gotUse, c.wantUse)
			}
			if plan.Deactivate != c.wantDeactivate {
				t.Errorf("Deactivate = %v, want %v", plan.Deactivate, c.wantDeactivate)
			}
			out := plan.Finish(res)
			if !reflect.DeepEqual(out.Notify, c.wantNotify) {
				t.Errorf("Notify = %q, want %q", out.Notify, c.wantNotify)
			}
			want := map[string]string{}
			for k, v := range c.wantExports {
				if c.in.Env[k] != v || !has(c.in.Env, k) {
					want[k] = v
				}
			}
			got := map[string]string{}
			for _, kv := range out.Exports {
				if _, dup := got[kv[0]]; dup {
					t.Errorf("export %s twice", kv[0])
				}
				got[kv[0]] = kv[1]
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Exports = %v\nwant      %v", got, want)
			}
			if g, w := sorted(out.Unsets), sorted(c.wantUnsets); !reflect.DeepEqual(g, w) {
				t.Errorf("Unsets = %v, want %v", g, w)
			}
			for _, k := range out.Unsets {
				if _, ok := got[k]; ok {
					t.Errorf("%s both exported and unset", k)
				}
			}
			if !stateEqual(out.State, c.wantState) {
				t.Errorf("State = %#v, want %#v", out.State, c.wantState)
			}
			// Run twice (zsh chpwd + precmd): the second run is a no-op.
			if out.State.PWD != "" {
				in2 := in
				in2.Prev = out.State
				in2.Env = apply(in.Env, out)
				again := Compute(in2)
				if again.Use != nil || again.Deactivate {
					t.Errorf("second run plans side effects: %+v", again)
				}
				if o := again.Finish(UseResult{}); len(o.Exports)+len(o.Unsets)+len(o.Notify) != 0 || !stateEqual(o.State, out.State) {
					t.Errorf("second run not a no-op: %+v", o)
				}
			}
		})
	}
}

func (s State) with(pwd string) State { s.PWD = pwd; return s }

func has(m map[string]string, k string) bool { _, ok := m[k]; return ok }

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func stateEqual(a, b State) bool {
	return a.PWD == b.PWD && a.Target == b.Target && reflect.DeepEqual(sorted(a.Keys), sorted(b.Keys))
}

// apply is the shell evaluating out on top of env.
func apply(env map[string]string, out Output) map[string]string {
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
	return next
}

func TestPlanFinishIsSingleUse(t *testing.T) {
	plan := Compute(Input{PWD: "/w/b", Env: map[string]string{}, Cfg: cfgOf(), PathProfile: "b", PathWD: "/w/b", Profiles: testProfiles})
	if plan.Use == nil {
		t.Fatal("want a pending use")
	}
	if plan.State.PWD != "/w/b" || plan.State.Target != "b" {
		t.Errorf("Plan.State before Finish = %#v, want pwd and target", plan.State)
	}
	cp := *plan // a copy shares the single use
	_ = plan.Finish(UseResult{})
	mustPanic(t, "second Finish", func() { plan.Finish(UseResult{}) })
	mustPanic(t, "Finish on a copy", func() { cp.Finish(UseResult{}) })
}

func TestPlanFinishRejectsUnplannedResult(t *testing.T) {
	plan := Compute(Input{PWD: "/tmp", Env: map[string]string{}, Cfg: cfgOf()})
	if plan.Use != nil {
		t.Fatal("want no use")
	}
	mustPanic(t, "a use result without a planned use", func() { plan.Finish(UseResult{Code: 1}) })
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", what)
		}
	}()
	f()
}
