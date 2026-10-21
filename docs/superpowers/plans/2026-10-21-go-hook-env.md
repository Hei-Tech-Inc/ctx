# Go port, phase E5: shell hook via `ctx activate` / `ctx hook-env`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the per-shell autoswitch logic out of `lib/ctx_autoswitch.bash` and `lib/ctx_autoswitch.fish` (~680 lines) into Go:
- `ctx activate bash|zsh|fish` prints small shell glue.
- `ctx hook-env bash|zsh|fish` prints only the exports and unsets that changed.

The shell keeps a fork-free guard, so prompts stay as fast as today.

**Architecture:** This follows the mise/direnv model.
- **The glue** (from `activate`):
  - defines the `ctx` wrapper function, with unchanged behavior;
  - registers one hook per shell: zsh `chpwd` and `precmd`; bash `PROMPT_COMMAND`, supporting both the string and the array form; fish `--on-variable PWD` and `fish_postexec`;
  - bakes in the absolute ctx-core path.
- **The hook** runs `eval "$(<ctx-core> hook-env <shell>)"` only when `$PWD` changed or `~/.ctx/config` is newer than the last stamp. Both checks use builtins only: string compare and `-nt`.
- **New package `internal/hookenv`** computes the target profile, the manual lock, the prompt exports, GH_TOKEN, the notify lines, and the diff against the previous state.
- **Legacy files:** `lib/ctx_autoswitch.*` become shims that `eval` the activate output when ctx-core exists and otherwise fall back to the current shell code. Old rc blocks therefore keep working with no edits.

**Tech Stack:** Go and existing modules. Shell-level tests use real bash and zsh, and fish where it is installed; CI installs fish.

**Spec:** None; the maintainer asked to always take the recommended approach. The behavior map is `/private/tmp/claude-501/-Users-isaackumi-Downloads-ctx-v3/3965e05e-b2a2-40ac-84a4-a8e2197b9f47/scratchpad/e5-map.md`, copied into the SDD workspace as `map.md`. The binding behavior is the current hook at 3374605, as corrected by the rulings.

## Global Constraints

- Branch `feat/go-hook-env`, worktree `.claude/worktrees/go-hook-env`, base 3374605. PR base: `feat/go-port-rest`.
- **Performance budget:** measured with 10 profiles on this Mac, using the map's bench approach committed as `scripts/bench-hook.sh`.
  - A prompt with no `$PWD` or config change must not fork, and must stay at or below 4 ms (bash and zsh).
  - A profile switch must take at most 30 ms (today about 107 ms).
  - Enter/leave must take at most 25 ms.
- **Kept byte-identical:**
  - every notify string and the stream it goes to;
  - the `CTX_PROMPT_SHOW`/`CTX_PROMPT_PROFILE`/`CTX_PROMPT_WORK_DIR` set and unset rules;
  - `CTX_ACTIVE_PROFILE`, and `CTX_ACTIVATION_TRIGGER=auto`;
  - the per-shell `GH_TOKEN` (a user-set `GH_TOKEN` is left alone), and `_CTX_GH_PROFILE`;
  - the manual-lock rules (`active_source=manual`, `manual_pwd` exact equality);
  - machine-wide `active=` shared across terminals;
  - the exports of `ctx use --eval` and `ctx deactivate --eval`.
- **Secrets** appear only in eval'd stdout. They are never logged, and never stored in the state variable, which holds key names only.
- **Both code paths keep passing:** `CTX_CORE=off` (legacy shell hook) and ctx-core. Bash 3.2 compatible and ShellCheck-clean. `go vet ./... && go test ./...`, `bash test/test.sh` and `CTX_CORE=off bash test/test.sh` all pass, and Linux CI is green.
- No `Co-authored-by` or agent trailers.

## Rulings

- **R1, cheap guard:** the guard is builtins only: `[[ $PWD != $__CTX_PWD ]] || [[ $CTX_CONFIG -nt $__CTX_STAMP ]]`, and fish `test`. `__CTX_STAMP` is a file ctx-core touches under `$CTX_DIR`. This picks up a `ctx use` in another terminal on the next prompt, as today.
- **R2, state:** `__CTX_STATE` is a shell-local variable, not exported, so child shells recompute. It is versioned: `v1|pwd|target|keys`, where `keys` is the space-separated names hook-env exported. Leaving or switching profiles then unsets exactly those keys, even if the profile was deleted. Today's deactivate dies with "Profile not found" in that case.
- **R3, compatibility:** `lib/ctx_autoswitch.bash` and `.fish` stay and become shims:
  - when ctx-core is present they `eval "$(ctx-core activate <shell>)"`;
  - otherwise (or with `CTX_CORE=off`) they run the legacy code;
  - re-sourcing removes legacy hook registrations, so nothing runs twice.
  
  install-hook's block is unchanged.
- **R4, bash `PROMPT_COMMAND`:** when it is an array (bash 5.1+), the hook is added as an element. When it is a string, it is prepended once, deduplicated as today.
- **R5, prompt_extra_paths:** the last entry is checked in the hook and in `ctx workdir-prompt` alike, fixing the known E2 follow-up in both Bash and Go.
- **R6, quirks kept:**
  - a failed `ctx use` is not retried until the pwd or target changes;
  - `.ctx` counts only under a profile `WORK_DIR` with a `.git` ancestor;
  - `CTX_ACTIVATION_TRIGGER` is always `auto` from the hook.
- **R7, fish:** profile parsing happens in Go, so there are no per-conf `bash -c` forks.

## Review Focus

1. **Many prompts with no directory change** never fork. Test: Task 6 bench asserts no exec, through a PATH canary.
2. **Another terminal runs `ctx use`/`deactivate`:** the next prompt here reflects it, matching the legacy behavior. Test: Task 6 integration.
3. **Leaving a profile whose `.conf` was deleted or edited meanwhile:** every key exported earlier is unset. Test: Task 2 table plus Task 6.
4. **Re-sourcing the rc, or an old rc block plus a new install, in an already-hooked shell:** exactly one hook is registered and runs once per prompt. Test: Task 4 and Task 6 for bash, zsh and fish.
5. **bash with `PROMPT_COMMAND` as an array, or bash-preexec / starship setups:** the user's own hooks are kept and run. Test: Task 4.

---

### Task 1: Refactors to reuse

**Files:**
- `internal/cli/workdirprompt.go`: extract `PromptScope(pwd string, cfg *ctxconfig.Config, profiles []profile.Profile) (show bool, profile, root string)`, shared by `workdir-prompt` and hookenv. Apply R5 in Go and in Bash `cmd_workdir_prompt`, plus the hook's shell code for the legacy path.
- `internal/ctxconfig`: typed `AutoswitchNotify` plus a parse matching the hook.
- `internal/cli/use.go`: `type UseOpts struct{ Quiet, AutoSwitch bool; Shell string }`, so callers stop setting `CTX_QUIET`/`CTX_AUTO_SWITCH` env. `UseIn` keeps its behavior.
- `internal/cli/deactivate.go`: `DeactivateUnsets(p, shell, wd) []string`, exported.

- [ ] Tests:
  - the existing workdir-prompt, use and deactivate parity tests pass unchanged;
  - a new R5 case (last `prompt_extra_paths` entry) in Go and test.sh, both modes;
  - a `ParseNotify` table test.
- [ ] Commits: `refactor(core): shared prompt scope, typed notify, use options` and `fix: workdir-prompt checks the last prompt_extra_paths entry`.

### Task 2: `internal/hookenv` (pure core)

**Interfaces — produces:**
- `type State struct{ PWD, Target string; Keys []string }`, with `Encode() string` and `Decode(s string) (State, bool)`. The format is `v1|…`, escaped.
- `type Input struct{ PWD string; Prev State; Env map[string]string; Cfg *ctxconfig.Config; Profiles []profile.Profile; GH func(user string) (string, error) }`
- `type Output struct{ Exports [][2]string; Unsets []string; Notify []string; State State; Use *UseCall; Deactivate bool }`
- `func Compute(in Input) Output`. It ports the decision logic of `_ctx_profile_autoswitch` step by step, using the map's §1.3–1.5 with exact strings:
  - target resolution;
  - the manual lock;
  - enter, leave and switch;
  - prompt exports;
  - GH_TOKEN, including the user-owned GH_TOKEN rule;
  - notify lines per `autoswitch_notify`;
  - the failure-not-retried rule.

- [ ] Tests: a table-driven `TestCompute` covering:
  - enter, leave, switch, same profile with a cd, and nested `WORK_DIR`s (the longest prefix wins);
  - the manual lock held, and the lock cleared when leaving `manual_pwd`;
  - a deleted profile while active (Review Focus 3);
  - `GH_TOKEN` set by the user vs by ctx;
  - each notify mode;
  - prompt depth and extra paths;
  - a failed use not being retried.
  
  Expected outputs are derived from the legacy hook. Add one differential helper that runs the legacy bash hook in a subshell on the same fixture and compares the resulting env for at least 8 of the cases.
- [ ] Commit: `feat(hookenv): compute autoswitch transitions in Go`.

### Task 3: `ctx hook-env <shell>`

**Files:** `internal/cli/hookenv.go` and its test. In main.go, add the `implemented` entry and the case. In `bin/ctx`, add the forward.

**Interfaces — produces:** `func HookEnv(t tui.Term, args []string) int`. It:
- reads `$PWD`, `__CTX_STATE` and the config;
- runs `Compute`, then performs the `Use` or `Deactivate` side effects in-process (use and deactivate stay the single source of the config writes and tool switching);
- emits shell code for bash/zsh (`export K=<shquote>` / `unset K`) or fish (`set -gx` / `set -e`), plus `__CTX_STATE` and the stamp update;
- writes notify lines to stderr;
- has an unchanged-state fast path that emits only the state.

- [ ] Tests:
  - golden outputs per shell for enter, leave and switch;
  - the eval'd result in real bash and zsh (fish when present) equals the legacy hook's resulting env for the same transitions;
  - secrets appear only in stdout;
  - a run-twice idempotency check (chpwd plus precmd).
- [ ] Commit: `feat(core): ctx hook-env`.

### Task 4: `ctx activate <shell>`

**Files:** `internal/cli/activate.go` and its test (with `text/activate.{bash,zsh,fish}` embedded). main.go and bin/ctx forwards.

**Interfaces — produces:** `func Activate(t tui.Term, args []string) int`. It prints the glue:
- the `ctx` wrapper function, the same as today;
- the guard from R1;
- registration per R4 and per shell;
- a call once at source time;
- cleanup of legacy registrations (`add-zsh-hook -d …`, stripping `PROMPT_COMMAND`, `functions -e`).

The ctx-core path is baked in, quoted with shquote.

- [ ] Tests:
  - re-sourcing twice registers one hook (Review Focus 4);
  - legacy-then-new sourcing in one shell leaves one hook;
  - a `PROMPT_COMMAND` array with the user's entries kept (Review Focus 5);
  - bash-preexec present;
  - `bash -n`, `zsh -n` and `fish -n` on the output.
- [ ] Commit: `feat(core): ctx activate`.

### Task 5: Legacy shims, install-hook, doctor, docs hooks

**Files:**
- `lib/ctx_autoswitch.bash` and `.fish`: a shim header that execs the activate path when ctx-core is usable and `CTX_CORE` is not `off`, otherwise the legacy body unchanged.
- `internal/cli/doctor.go` and Bash doctor: accept both hook forms and report which is active.
- `uninstall` hint text, if affected.
- README "shell hook" section.

- [ ] Tests:
  - test.sh's sourced-hook tests run under both `CTX_CORE=off` and ctx-core and pass;
  - doctor parity.
- [ ] Commit: `feat: autoswitch libs delegate to ctx activate when ctx-core is present`.

### Task 6: Integration tests and benchmark

**Files:**
- `scripts/bench-hook.sh`, the committed benchmark.
- New test.sh cases for Review Focus 1–5 with real bash and zsh.
- `.github/workflows/ci.yml`: install fish and zsh in the unit-tests job.

- [ ] Tests:
  - a PATH canary: a fake `ctx-core` that logs every call shows zero calls across 50 no-change prompts;
  - another-terminal `ctx use`;
  - deleted-profile leave;
  - re-source and legacy-plus-new;
  - array `PROMPT_COMMAND`.
  
  The bench asserts the performance budget loosely in CI (×5 headroom) and is reported exactly locally.
- [ ] Commit: `test: shell hook integration and latency budget`.

### Task 7: Docs

- [ ] Write the CHANGELOG phase E5 line, with measured numbers before and after, and Fixed lines for R4 and R5 and the deleted-profile leave.
- [ ] Mark E5 done in SPRINT and add the E6 notes.
- [ ] Add `internal/hookenv` to AGENTS.md.
- [ ] Commit: `docs: Go port phase E5`.
