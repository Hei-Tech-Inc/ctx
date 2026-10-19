# Go port, phase E3: commands that change state

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move `secret get|list|delete`, `config` (show, setters, export, import), `deactivate`, quiet `use`, and `guard install|uninstall|status` into `ctx-core`. Behavior must be identical to Bash, and Bash stays the fallback.

**Architecture:** Same forwarding as phase E2: `bin/ctx` asks `ctx-core implements <cmd> [sub]` and execs ctx-core on a yes. `implements` may also look at the environment, e.g. `use` counts only when `CTX_QUIET=1`. Every task adds a differential test that runs the Bash path (`CTX_CORE=off`) and the Go path against the same fixture `HOME`/`CTX_DIR`, with fake external tools (`gh`, `ssh-add`, `ssh-keygen`, `security`, `pass`, `az`, `gcloud`, `kubectl`, `mise`) that record their argv. It then compares stdout, stderr, exit code, the recorded tool calls and the resulting files.

**Tech Stack:** Go (go.mod as before), stdlib only.

**Spec:** None. The maintainer asked to implement directly and to always take the recommended approach. The binding behavior is the Bash implementation on branch `feat/go-port-write` at commit 7458596: phase E2 plus the `ctx use --eval` fix merged in.

## Global Constraints

- Branch `feat/go-port-write`, worktree `.claude/worktrees/go-port-write`, base 7458596.
- Identical behavior to Bash: stdout, stderr text, exit codes, files written (content and mode), and external tools invoked (argv and order). Eval output must be eval-equivalent.
- Files under `~/.ctx` are written atomically (temp file + rename), with the same modes Bash produces: dirs 0700, secret files 0600.
- Stays in Bash this phase:
  - interactive paths: `secret set`, `secret migrate`, and anything calling `ask*`/`pick*`/gum;
  - non-quiet `use`, whose gum-styled output belongs to phase E4;
  - all other commands.
- ShellCheck-clean, bash 3.2 compatible, `go vet ./... && go test ./...`, `bash test/test.sh` and `CTX_CORE=off bash test/test.sh` all pass.
- No `Co-authored-by` or agent trailers.

## Review Focus

1. **Concurrent writers to `~/.ctx/config`:** two terminals autoswitching at once. Writes are atomic and never lose other keys. Test: Task 2 `TestConfigUpsertConcurrent` (20 goroutines × different keys → all keys present, file parseable).
2. **Profile name or secret key with odd characters** (`-`, `.`, uppercase, a key failing `is_valid_env_key`): Go accepts and rejects exactly what Bash does, with the same error text. Test: Tasks 1 and 4.
3. **A tool missing from PATH** (no `gh`, `az`, `gcloud`, `kubectl`, `mise` or `ssh-add`): Go takes the same fallback branch as Bash, e.g. exporting `AZURE_SUBSCRIPTION` when `az` is absent. Test: Task 4 with a minimal PATH.
4. **macOS Keychain vs Linux file provider** under `auto`: Go picks the provider exactly as `ctx_effective_secret_provider` does. Test: Task 1, table-driven on `GOOS` and config value, with a fake `security`.
5. **`use` after a manual lock, and `deactivate` when nothing is active:** same config result and same output as Bash. Test: Tasks 3 and 4.

---

### Task 1: Secret providers and `secret get|list|delete`

**Files:** Create `internal/secret/providers.go` and `providers_test.go`. Modify `cmd/ctx-core/main.go`, plus `internal/secret/secret.go` (in-process getter).

**Interfaces:**
- Produces:
  - `func EffectiveProvider() string` (`keychain|file|pass`), porting `ctx_secret_provider` / `ctx_effective_secret_provider`. Env `CTX_SECRET_PROVIDER` first, then the `secret_provider=` config key, then `auto` → keychain on darwin, otherwise file.
  - `type Store interface{ Get(profile, key string) (string, error); List(profile string) ([]string, error); Delete(profile, key string) error }`, with a `Keychain`, a `File` and a `Pass` implementation.
  - These port `_secret_keychain_*`, `_secret_file_*` and `_secret_pass_*` (lib/core.sh) exactly: the same `security`/`pass` argv, the file path `$CTX_DIR/secrets/<profile>/<KEY>`, and the same list ordering.
  - `func Open(provider string) (Store, error)`.
  - `type ProviderGetter struct{}` implements `Getter` via `Open(EffectiveProvider())`. Phase 1's `mcp`, and `env`/`run`, switch from `CLIGetter` to `ProviderGetter`. `CLIGetter` stays for tests.
- CLI: `secret get|list|delete` with Bash-identical output and errors, including the colored `list` output (colors only when stdout is a TTY) and the `die` messages. `implements secret get|list|delete` → 0; `set`/`migrate` → 1.
- [ ] Tests:
  - `TestProvidersMatchBash` runs each provider against fakes: fake `security` and `pass` record argv and keep state in a temp dir, and the file provider uses a temp CTX_DIR. It compares Go `Get`/`List`/`Delete` results and argv with the Bash functions sourced from lib/core.sh.
  - `TestEffectiveProvider` is table-driven.
  - `TestSecretCLIMatchesBash` compares CLI output and exit codes for get (present/missing), list (empty/two keys) and delete (present/missing).
  - The existing secret tests in test.sh pass in both modes.
- [ ] Implement, wire `implements`, run every check, commit `feat(core): secret providers and secret get/list/delete in Go`.

### Task 2: Config writes and `config` subcommands

**Files:** Modify `internal/ctxconfig/config.go`. Create `internal/ctxconfig/write.go`, `internal/cli/config.go`, and their tests. Modify `cmd/ctx-core/main.go`.

**Interfaces:**
- Produces:
  - `func Upsert(key, value string) error` and `func Remove(keys ...string) error`. These port `_ctx_config_upsert_line` / `_ctx_config_rm_key`: the same line format, last-wins handling of an existing key, multi-line values rejected with the same error, and an atomic write. They take an exclusive `flock` on `$CTX_DIR/config.lock` around read-modify-write. The lock is Go-only; Bash doesn't lock, and that's accepted.
  - `func InitDirs() error` ports `ctx_init_dirs`.
- CLI: `config [show]`, `work-root`, `secret-provider`, `prompt-workdir-depth`, `prompt-extra-paths`, `autoswitch-notify`, `export <dir>` and `import <dir>`. Each has output, validation errors, backups (`backup_file`) and files written identical to `cmd_config` and its helpers. `implements config <sub>` → 0 for all of these.
- [ ] Tests:
  - `TestUpsertMatchesBash` covers new key, existing key, duplicate keys, values with `&|\` and a multi-line value rejected.
  - `TestConfigUpsertConcurrent`.
  - `TestConfigCLIMatchesBash` runs each subcommand with valid and invalid input and compares stdout, stderr, exit code and resulting files.
  - `TestExportImportMatchesBash` compares the exported tree (files, contents and modes) and the import result, both against Bash.
- [ ] Implement, wire, run checks, commit `feat(core): config subcommands in Go`.

### Task 3: `deactivate`

**Files:** Create `internal/cli/deactivate.go` and its test. Modify `cmd/ctx-core/main.go`.

**Interfaces:**
- Consumes: `ctxconfig` (Task 2), `profile.Load`, `secret` store (Task 1; not needed unless Bash reads values here).
- Produces: `ctx-core deactivate [--eval bash|fish]`, porting `cmd_deactivate` and `_ctx_deactivate_emit_unsets`. Stdout is the same shell code; stderr messages match; config changes match (`clear_active_profile_activation`). The SSH agent key ctx added is dropped (`_ctx_agent_drop_added_key` → `ssh-add -d <key>` + remove `ssh_agent_added`). `implements deactivate` → 0.
- [ ] Tests: `TestDeactivateMatchesBash` covers: nothing active; active with secrets and EXTRA_ENVS; with `ssh_agent_added` set (fake `ssh-add` records argv); `--eval fish`. It compares stdout (exact for fish; eval-equivalent for bash), stderr, the config file and the ssh-add calls. The existing deactivate and autoswitch tests pass in both modes.
- [ ] Implement, wire, run checks, commit `feat(core): deactivate in Go`.

### Task 4: Quiet `use`

**Files:** Create `internal/cli/use.go`, `internal/cli/use_test.go` and `internal/gitident/rewrite.go` (+ test). Modify `cmd/ctx-core/main.go`.

**Interfaces:**
- Consumes: Tasks 1–3, `profile.Load`.
- Produces: `ctx-core use <name> [--eval bash|fish]` for `CTX_QUIET=1` only. `implements use` exits 0 only when `CTX_QUIET=1` is in the environment. It ports `cmd_use` + `_ctx_use_apply` exactly, in the same order:
  1. `gh auth switch -u` unless `CTX_AUTO_SWITCH=1`, with the same `has gh` check;
  2. the SSH agent logic (`_ctx_agent_added_key`, `_ctx_agent_has_key` via `ssh-keygen -lf` + `ssh-add -l`, `ssh-add`, the `ssh_agent_added` upsert, dropping the previous key);
  3. `ctx_git_identity_set_rewrite` (port to `internal/gitident`: the same git config file edits via `git config --file`);
  4. AWS, then Azure (`az account set` or the env fallback), GCP (`gcloud config set` or the fallback), kubectl;
  5. secrets via the Task 1 store, `is_valid_env_key`-filtered;
  6. `mise hook-env` / `direnv reload` when PWD is in WORK_DIR;
  7. `set_active_profile` + `log` line to `$CTX_DIR/ctx.log`, the activation manual/auto update including the manual-lock keep rule, and `CTX_ACTIVE_PROFILE`.

  Warnings go to stderr even when quiet, exactly as in Bash. The exit code is the error count, the eval output is the recorded exports, and without `--eval` the not-applied warning is suppressed by quiet, as in Bash.
- [ ] Tests: `TestUseMatchesBash` uses fake `gh`/`ssh-add`/`ssh-keygen`/`az`/`gcloud`/`kubectl`/`mise`, each recording argv. Scenarios:
  - full profile, auto switch;
  - manual (`CTX_AUTO_SWITCH` unset);
  - tools absent (minimal PATH);
  - previous agent key different / same / already loaded;
  - manual lock kept on re-apply;
  - missing secret (a warning);
  - invalid secret key name.

  For each it compares, Bash vs Go: stdout eval-equivalence, stderr, exit code, recorded tool argv in order, `~/.ctx/config`, the git identity file, and the ctx.log line (ignoring the timestamp). The existing use / autoswitch / wrapper tests in test.sh pass in both modes.
- [ ] Implement, wire, run checks, commit `feat(core): quiet ctx use in Go`.

### Task 5: `guard install|uninstall|status`

**Files:** Create `internal/guard/install.go` and its test. Modify `cmd/ctx-core/main.go`.

**Interfaces:**
- Produces: these port `_ctx_guard_install`, `_ctx_guard_uninstall`, `_ctx_guard_status` and `_ctx_guard_write_hook` exactly: the same hook stub bytes and modes for every name in `CTX_GUARD_HOOK_NAMES`, the same `git config --global` calls, the same refusal when a foreign `core.hooksPath` exists, the same dry-run output, and the same status lines. `implements guard install|uninstall|status` → 0.
- [ ] Tests: `TestGuardInstallMatchesBash` runs with a temp HOME and `GIT_CONFIG_GLOBAL`. It covers install, install again, install with a foreign hooksPath, dry-run, status (installed, not installed, repo-local hooksPath), and uninstall, comparing the hook dir tree (bytes + modes), the global git config and the output. The existing `test_guard_git_hooks` passes in both modes.
- [ ] Implement, wire, run checks, commit `feat(core): guard install/uninstall/status in Go`.

### Task 6: Docs and measurements

- [ ] Measure the autoswitch path, `CTX_QUIET=1 CTX_AUTO_SWITCH=1 ctx use --eval bash <p>` + `ctx deactivate --eval bash`, Bash vs Go: median of 20 runs, 10 profiles, fake tools. Record it in CHANGELOG [Unreleased] → Changed.
- [ ] Update the README "ctx-core in everyday commands" note, mark SPRINT.md E3 done, and list in E4 what is still Bash: interactive secret set/migrate, non-quiet use, text list/status, setup wizard. Update AGENTS.md if commands changed.
- [ ] Run every check, commit `docs: Go port phase E3`.
