# Go port, phase 2: read-only commands in `ctx-core`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move `list --json`, `status --json`, `workdir-prompt`, `env`, `run` and `guard check` from Bash into the Go binary `ctx-core`, with identical behavior, while Bash stays the fallback.

**Architecture:** `bin/ctx` forwards these commands to `ctx-core` when it is available and `CTX_CORE` is not `off`; otherwise it runs the existing Bash code. The existing `test/test.sh` becomes the conformance suite: CI runs it once with `ctx-core` and once with `CTX_CORE=off`. Go code reuses the packages from phase 1 (`internal/profile`, `internal/resolve`, `internal/secret`).

**Tech Stack:** Go (go.mod from phase 1: go 1.26.0, toolchain go1.27.2), stdlib only. Bash tests in `test/test.sh`.

**Spec:** No separate spec. The maintainer opted out of design questions and asked to implement directly. The binding behavior is the current Bash implementation in `lib/cmd_ops.sh` / `lib/core.sh` on branch `feat/ctx-mcp` (commit c5a4f58), and the maintainer's goals: "port to golang" and the staged approach agreed earlier (strangler, Bash keeps working, `test/test.sh` as conformance). Rulings made by the controller are listed in the ledger.

## Global Constraints

- Branch `feat/go-port-read`, based on `feat/ctx-mcp` (c5a4f58). Worktree `.claude/worktrees/go-port-read`.
- Behavior must match Bash exactly: the same stdout, stderr wording and exit codes. JSON may differ only in whitespace. `env` output must be **eval-equivalent** to Bash's: evaluating it yields the same variables and values. It doesn't need to be byte-identical, since Bash's `%q` quoting isn't reproducible.
- Text (non-JSON) `list` and `status` stay in Bash in this phase.
- Forwarding falls back to Bash silently when `ctx-core` is not found. `CTX_CORE=off` forces Bash. Unlike `mcp`/`knowledge`, which still error when ctx-core is missing.
- `bin/ctx` exports `CTX_VERSION` (from lib/core.sh) to ctx-core. Go output uses `$CTX_VERSION` when set, else its build version.
- ShellCheck-clean (`shellcheck -x -S warning bin/ctx lib/*.sh lib/*.bash install.sh test/*.sh`); bash 3.2 compatible; `go vet ./... && go test ./...` pass.
- No `Co-authored-by` or agent trailers in commits.

## Review Focus

1. **Two profiles with the same WORK_DIR, or no profiles dir at all:** Go and Bash pick the same profile, or both print nothing. Test: Task 2 differential cases.
2. **`~/.ctx/config` with duplicate `active=` lines, CRLF line endings, or a missing file:** Go reads exactly what Bash's `grep … | cut` / `tail -1` reads for each key. Test: Task 1 `TestConfigMatchesBashReaders`.
3. **Values with quotes, spaces, `$`, backslashes or newlines** in profile fields, secrets and EXTRA_ENVS: `env` (bash and fish) round-trips them exactly. Test: Task 4.
4. **`ctx guard check` outside a git repo, in a repo outside every WORK_DIR, and with `CTX_GUARD=off`:** exit 0, no output, same as Bash. Test: Task 5.
5. **ctx-core crashes or isn't executable:** the user still gets a working command, because forwarding falls back to Bash. Test: Task 1 `test_core_forward_fallback`.

---

### Task 1: Forwarding in `bin/ctx`, config reader, conformance CI

**Files:**
- Create: `internal/ctxconfig/config.go`, `internal/ctxconfig/config_test.go`
- Modify: `bin/ctx`, `cmd/ctx-core/main.go`, `.github/workflows/ci.yml`, `test/test.sh`

**Interfaces:**
- Produces (bin/ctx): `_ctx_core_forward <cmd> [args…]`. If `CTX_CORE` is not `off` and `_ctx_core_bin` finds an executable, it runs `CTX_VERSION=… CTX_DIR=… "$bin" <cmd> [args…]` and returns its exit code. If ctx-core exits with code 126/127, or can't be executed, it falls back to the Bash implementation. Otherwise it returns 1 so the caller runs Bash. Wire `list|ls` and `status|st` when `CTX_JSON=1` (forward as `ctx-core list --json` / `status --json`), `workdir-prompt`, `env`, `run`, and `guard check …` (only the `check` subcommand) through it. All other paths are unchanged.
- Produces (Go): package `ctxconfig` reading `$CTX_DIR/config` (default `~/.ctx/config`):
  - `func Path() string`.
  - `func Load() (*Config, error)`. A missing file is not an error.
  - `type Config struct{ Active, ActiveSource, ManualPWD /* decoded */, PromptExtraPaths string; PromptWorkdirMaxDepth int; SSHAgentAdded string }`.
  - Last-wins per key, matching Bash `grep "^key=" | tail -1 | cut -d= -f2-` for every key. `active` uses the same rule as `active_profile`; if the readers disagree on duplicates, match `active_profile` exactly and document it.
  - `ActiveSource` lowercased, defaulting to `auto`.
  - `manual_pwd` base64-decoded the way `ctx_activation_get_manual_pwd` does.
  - Max depth defaults to 2 when the value isn't a non-negative integer.
  - CR stripped.
- Produces (Go): `func ctxVersion() string` in cmd/ctx-core (env `CTX_VERSION` else build version).

- [ ] **Step 1: Write failing tests.** `TestConfigMatchesBashReaders` is table-driven over config fixtures: duplicates, CRLF, missing file, base64 manual_pwd, a bad depth. For each key it compares `Load()` with the Bash reader output (source `lib/core.sh` with `CTX_DIR` pointed at the fixture and call `active_profile`, `ctx_activation_get_source`, `ctx_activation_get_manual_pwd`, `ctx_config_prompt_workdir_max_depth`; skip without bash). In test.sh, `test_core_forward_fallback` checks three cases:
  - `CTX_CORE_BIN` set to a script that exits 127 → `ctx --json list` still prints the Bash JSON.
  - `CTX_CORE=off` → the Bash JSON, and the fake is never called.
  - A fake that prints `GO` → `ctx --json list` prints `GO`.
- [ ] **Step 2: Run** `go test ./internal/ctxconfig/` and `bash test/test.sh`. Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces. In ctx-core, add `list`, `status`, `workdir-prompt`, `env`, `run` and `guard` cases. Until a later task implements one, it prints `not implemented` and exits 127, so forwarding falls back. That keeps every intermediate commit working.
- [ ] **Step 4: CI.** In the `unit-tests` job, after the existing `bash test/test.sh` (which now runs with the built ctx-core), add a step `CTX_CORE=off bash test/test.sh`.
- [ ] **Step 5: Run** both suites, `go test ./...` and shellcheck. Expected: PASS.
- [ ] **Step 6: Commit** `feat(core): forward read-only commands to ctx-core with Bash fallback`

### Task 2: `list --json` and `status --json`

**Files:**
- Create: `internal/cli/listjson.go`, `internal/cli/statusjson.go`, `internal/cli/json_test.go`
- Modify: `cmd/ctx-core/main.go`, `test/test.sh`

**Interfaces:**
- Consumes: `profile.List/Load/Dir`, `ctxconfig.Load`, `ctxVersion`.
- Produces: `func ListJSON(w io.Writer, version string) error` and `func StatusJSON(w io.Writer, version, pwd string) error`. Their keys, key order and values are exactly those of `cmd_list` / `cmd_status` in their `CTX_JSON=1` branches (lib/cmd_ops.sh).
  - **list:** `version`, `command`, `profiles[]` with `name, active, git_name, git_email, work_dir, github_user, aws_profile, azure_subscription, gcp_project, kube_context, secret_keys`.
  - **status:** `version`, `command`, `active` (`null` when none), `activation`, `activation_note` (`manual_locked_at_pwd` | `manual_cleared_auto_applies` | `auto`), `git_name`, `git_email`, `work_dir`, `github_user`, `ssh_key_path`, `aws_profile`, `azure_subscription`, `gcp_project`, `kube_context`, `secret_keys`, `ctx_active_profile` (from env).
  - `secret_keys` is the space-joined original string. Both must also create the ctx dirs like `ctx_init_dirs` (mkdir `$CTX_DIR`, `profiles`, `backups`; touch config; chmod 700).
- [ ] **Step 1: Write failing tests.**
  - `TestListJSONMatchesBash` and `TestStatusJSONMatchesBash` build fixture `CTX_DIR`s: no profiles; two profiles, one active; values with `"`, `\` and a tab; manual activation at pwd and elsewhere. They run the Bash function (`CTX_CORE=off bin/ctx --json list`) and the Go function, `json.Unmarshal` both, and compare the maps. The raw key order must also match.
  - Also `TestStatusNoActive`.
- [ ] **Step 2: Run** `go test ./internal/cli/`. Expected: FAIL.
- [ ] **Step 3: Implement** and wire into main.go (`list --json`, `status --json`; other args → exit 127 for fallback).
- [ ] **Step 4: Run** `go test ./...`, `bash test/test.sh`, and `CTX_CORE=off bash test/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): list and status --json in Go`

### Task 3: `workdir-prompt`

**Files:**
- Create: `internal/cli/workdirprompt.go`, test in `internal/cli/workdirprompt_test.go`
- Modify: `cmd/ctx-core/main.go`

**Interfaces:**
- Consumes: `resolve.Path`, `ctxconfig.Load`.
- Produces: `func WorkdirPrompt(w io.Writer, pwd string) error`, a port of `cmd_workdir_prompt` + `ctx_rel_path_depth_under`. It prints `work_dir='WORK_DIR=<root>'\n` when pwd is within max depth under the resolved WORK_DIR, or under a `prompt_extra_paths` entry (colon-separated; `~` expanded; trailing `/` trimmed). Otherwise it prints nothing. Exit 0 always.
- [ ] **Step 1: Write failing test** `TestWorkdirPromptMatchesBash`. Cases: exact WORK_DIR; depth 1, 2 and 3 with the default depth 2; depth 0 configured; an extra path; outside everything; no profiles dir. It compares with `CTX_CORE=off bin/ctx workdir-prompt` run with `cd` into each pwd.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement** and wire.
- [ ] **Step 4: Run** `go test ./...` and both test.sh modes. Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): workdir-prompt in Go`

### Task 4: `env` and `run`

**Files:**
- Create: `internal/cli/env.go`, `internal/cli/env_test.go`
- Modify: `cmd/ctx-core/main.go`

**Interfaces:**
- Consumes: `profile.Load`, `resolve.Path`, `ctxconfig.Load` (active fallback), `secret.CLIGetter` (CtxBin = `$CTX_BIN_PATH` exported by bin/ctx, i.e. the path of bin/ctx itself, so secrets use the Bash providers).
- Produces:
  - `func CollectEnv(p *profile.Profile, withSecrets bool, g secret.Getter, ghToken func(user string) string) ([][2]string, error)`. It returns ordered pairs exactly as `_ctx_profile_env_collect` (lib/cmd_ops.sh) builds them: same order, same conditions. That includes `GIT_SSH_COMMAND` only when the key file exists, `GH_TOKEN` only when `gh` is on PATH and returns a token, and invalid EXTRA_ENVS keys skipped (`is_valid_env_key`).
  - `func FormatEnv(pairs [][2]string, shell string /* bash|fish */) string`. Bash uses POSIX single-quote quoting, `export K='v'`, with `'` written as `'\''`. Fish uses the same escaping as Bash `_ctx_emit_export` / `cmd_env`: `set -gx K '…'`, with `\` and `'` backslash-escaped.
  - `func Run(args []string) error`. It parses `[-p NAME] [--] cmd…` like `_ctx_cmd_load_profile`: profile from `-p`, else resolved from cwd, else active, else the error `No profile for <pwd> and none active. Use -p <profile>.` with exit 1. It then `syscall.Exec`s the command with os env plus the pairs (secrets included). A missing command gives a Bash-identical usage error.
  - `env` parses `[-p NAME] [--fish] [--secrets]`, with the same usage error text as Bash for unknown args.
  - `bin/ctx` exports `CTX_BIN_PATH` when forwarding.
- [ ] **Step 1: Write failing tests.**
  - `TestEnvEvalEquivalent`: for a profile with tricky values (quote, space, `$x`, backslash, newline) and secrets via a fake getter, run Bash `CTX_CORE=off ctx env --secrets` and Go `FormatEnv`. Eval each in `bash -c` and dump the resulting values with `printf '%s\0'`. The results must be identical. Do the same for `--fish` when `fish` is installed (skip otherwise), plus a text check of fish escaping.
  - `TestEnvOrderMatchesBash` compares the key order.
  - `TestRunExecsWithIdentity`: `ctx-core run -p demo -- sh -c 'printf %s "$GIT_AUTHOR_EMAIL|$AWS_PROFILE|$API_KEY"'` prints the expected values.
  - `TestRunNoProfile` checks the error text and exit 1.
  - The existing `test_ctx_run_env_agent_rules` in test.sh must pass in both modes.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement** and wire. For the `gh` token, exec `gh auth token -u <user>` with a 5 s timeout, and treat any error as "no token".
- [ ] **Step 4: Run** `go test ./...` and both test.sh modes. Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): env and run in Go`

### Task 5: `guard check`

**Files:**
- Create: `internal/guard/check.go`, `internal/guard/check_test.go`
- Modify: `cmd/ctx-core/main.go`

**Interfaces:**
- Consumes: `resolve.Path`, `profile.Load`.
- Produces: `func Check(hook string, args []string, stderr io.Writer) int`. It ports `_ctx_guard_check`, `_ctx_guard_check_commit`, `_ctx_guard_check_push`, `_ctx_guard_url_host`, `_ctx_guard_ident_email` and `github_clone_url_for_profile` (lib/core.sh) exactly, including:
  - message text;
  - the three `Fix:` variants for commits;
  - the `github.com` warning that checks `^Host[[:space:]]+github-<prof>` in `~/.ssh/ctx_config`;
  - exit 3 (`CTX_GUARD_BLOCK`) to block, 0 otherwise;
  - `CTX_GUARD=off` → 0 with no output. Check where Bash honors this (the hook stub or `_ctx_guard_check`) and match it.
  - Unknown hook names → 0.

  It uses `git rev-parse --show-toplevel`, `git var GIT_AUTHOR_IDENT` / `GIT_COMMITTER_IDENT` and `git config` via exec, like Bash. Colors: use Bash's `error`/`warn` prefixes (`✗`/`!` with ANSI only when stderr is a TTY), matching `lib/core.sh`.
- [ ] **Step 1: Write failing tests.** `TestGuardMatchesBash` uses real temp git repos under a fixture WORK_DIR, comparing Go `Check` with `CTX_CORE=off bin/ctx guard check …` (stderr text and exit code). Cases:
  - commit with matching email → 0;
  - mismatched config email → 3 with the `git config user.email` fix;
  - repo-local override → the `--local --unset` fix;
  - `GIT_AUTHOR_EMAIL` env override → the `unset` fix;
  - push to another profile's `github-<other>` alias → 3;
  - push via `github.com` with a ctx_config Host → warning, 0;
  - outside any WORK_DIR → 0, silent;
  - not a git repo → 0;
  - `CTX_GUARD=off` → 0;
  - an unknown hook → 0.
  
  The existing `test_guard_git_hooks` must pass in both modes.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement** and wire (`guard check …` only; other `guard` subcommands → exit 127 fallback).
- [ ] **Step 4: Run** `go test ./...`, both test.sh modes, and shellcheck. Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): guard check in Go`

### Task 6: Docs and measurements

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `docs/sprint/SPRINT.md`, `AGENTS.md`

- [ ] **Step 1:** Measure the per-call time of `ctx guard check pre-commit` in a repo under a WORK_DIR with 10 profiles: Bash (`CTX_CORE=off`) versus Go, median of 20 runs (`hyperfine` if installed, else a loop with `date +%s%N` or `/usr/bin/time`). Record the numbers in the CHANGELOG entry.
- [ ] **Step 2:** README gets a short "ctx-core" note: which commands run in Go, the `CTX_CORE=off` fallback, and that behavior is unchanged. CHANGELOG [Unreleased] → Changed. SPRINT.md gets a new Phase E "Go port" table: E1 (ctx mcp, done), E2 (this phase, done), E3 (use/deactivate/secret/config/guard install), E4 (setup wizard + text list/status with Charm libs), E5 (shell hook via `ctx hook-env`), E6 (drop Bash). AGENTS.md gets the `CTX_CORE=off bash test/test.sh` command.
- [ ] **Step 3: Run** both test.sh modes, `go test ./...` and shellcheck. Expected: PASS.
- [ ] **Step 4: Commit** `docs: Go port phase 2`
