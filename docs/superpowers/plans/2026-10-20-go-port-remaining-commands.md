# Go port, phase E4c: the remaining Bash-only commands

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move `version`, `help`, `completion`, `agent-rules`, `edit`, `undo`, `uninstall`, `verify`, `clone`, `doctor`, `install-hook`, `init` and `upgrade` into `ctx-core`, so that every `ctx` command runs in Go when ctx-core is installed.

**Architecture:** Forwarding works as before: `bin/ctx` asks `ctx-core implements <cmd> …` and execs on a yes, and `CTX_CORE=off` keeps the Bash code. Each task adds differential tests in which Bash (`CTX_CORE=off bin/ctx --no-gum …`) and Go run against the same fixture HOME with fake tools, and their outputs are compared. Every test that runs external tools uses a hermetic PATH: fakes first, then `testutil.SystemPath` (no cloud CLIs). Interactive cases use `testutil.RunPTY`.

**Tech Stack:** Go, with existing modules only. `go:embed` is used for the help and completion text.

**Spec:** None; the maintainer asked to always take the recommended approach. The binding behavior is the Bash code on `feat/go-port-setup` at 91de3c1, as corrected by the rulings below. The command-by-command map is in `/private/tmp/claude-501/-Users-isaackumi-Downloads-ctx-v3/3965e05e-b2a2-40ac-84a4-a8e2197b9f47/scratchpad/e4c-map.md`. Implementers read it for exact strings, argv and line numbers.

## Global Constraints

- Branch `feat/go-port-rest`, worktree `.claude/worktrees/go-port-rest`, base 91de3c1. PR base: `feat/go-port-setup`.
- **Plain mode** (non-TTY, or `--no-gum`/`CTX_NO_GUM=1`) must match Bash `--no-gum` on all of these:
  - stdout and stderr bytes;
  - exit codes;
  - files written, content and mode;
  - backups (stamps normalized);
  - external tool argv and order.
- **Rich mode** keeps the same wording, order, defaults and exit codes.
- **Global flags:** ctx-core reads `CTX_DRY_RUN`, `CTX_QUIET`, `CTX_JSON` and `CTX_NO_GUM` from the environment. `bin/ctx` strips the flags from argv.
- **Cancellation:** Ctrl+C or EOF at any prompt prints `! Cancelled by user.`, exits 130 and changes nothing.
- **Network:** no test touches the network. `upgrade` is tested against a local HTTP server or a `file://` repo, and `verify` and `clone` against fake `ssh`, `git` and `gh`.
- **Checks:** ShellCheck-clean, bash 3.2 compatible. `go vet ./... && go test ./...`, `bash test/test.sh` and `CTX_CORE=off bash test/test.sh` all pass, and Linux CI is green.
- No `Co-authored-by` or agent trailers.

## Rulings (Bash bugs fixed in both Bash and Go, not mirrored)

- **R1:** `--dry-run` is honored by every command that changes something:
  - `install-hook`, `init`, `clone`, `undo`, `uninstall` and `upgrade` print `dim "  [dry-run] would <action>"` for each action and change nothing;
  - `agent-rules --write` keeps its current dry-run;
  - `edit` ignores dry-run, since opening an editor is the user's action.
- **R2:** `install-hook`:
  - The legacy-block strip no longer deletes everything to EOF. A legacy block ends at its closing `# ─{25,}` line, or at the next blank line if there is none.
  - The rc file is rewritten through symlinks and keeps its mode, as E4a's gitconfig fix does.
  - The mise activation line follows the rc file's shell (`config.fish` → fish, `*zsh*` → zsh, otherwise bash) rather than `$SHELL`.
  - The missing-lib check runs before any edit.
- **R3:** `verify`:
  - It reads the active gh account with the E4b detector rule ("account USER" / "as USER").
  - It exits 1 when any check fails; today it always exits 0.
- **R4:** `doctor` never prints "Everything looks good." after any warning, including per-profile warnings.
- **R5:** `edit` opens `$VISUAL`, else `$EDITOR`, else `nano`. The value is run like git runs it (`sh -c '<editor> "$@"'`), so `EDITOR="code -w"` works.
- **R6:** `clone` with no extra git args no longer trips `set -u` on bash 3.2.
- **R7:** `undo`:
  - It lists the files it will restore before asking.
  - It backs up the current versions first, so an undo can itself be undone.
  - A restored file gets the backup's mode.
  - It exits 1 if any restore failed.
  
  The basename-collision limit of the backup layout stays and is documented.
- **R8:** `uninstall`:
  - It refuses to `rm -rf` a path that is `/`, `$HOME`, `$CTX_DIR`'s parent, or one whose basename isn't `ctx` (lib dir) or `.ctx` (purge target), unless `CTX_DIR` was set explicitly and is not `/` or `$HOME`.
  - It also removes `ctx-core` next to the binary.
  - It runs `guard uninstall` if the guard is installed.
  - `--purge` also deletes every profile's secrets from the configured store before removing `~/.ctx`.
  - The closing hint names the real rc markers (`# BEGIN ctx profile autoswitch`).
- **R9:** `help` and `completion`:
  - The first commit ports the text byte-identical.
  - A second commit fixes stale text in Bash and Go together: aliases, `mcp install`, `knowledge init`, `env`/`run` flags, the `agent-rules --write` and `guard check|remove` completions, and `-q` in the zsh flags.
- **R10:** a single SIGINT handler in ctx-core (generalized from E4b's setup handler) covers every interactive Go command except exec paths (`run`, `env` exec). It restores the terminal, prints the cancel line, kills registered child process groups and exits 130.
- **R11:** `upgrade` keeps its design: download `install.sh` and run it with bash. `--check` keeps parsing `CTX_VERSION` from the remote `lib/core.sh`. Changing that is phase E6. `CTX_UPGRADE_REPO` stays the test seam.

## Review Focus

1. **Destructive commands on unusual installs:**
   - a symlinked `ctx` on PATH;
   - a dev checkout on PATH;
   - `CTX_LIB` pointing somewhere odd;
   - `CTX_DIR=/`.
   
   Expected: a refusal with a clear message, and nothing deleted. Test: Task 4 `TestUninstallSafety`.
2. **An rc file that is a symlink, read-only, CRLF, or has a legacy block without an END marker:** the content after the block is preserved, and the link and mode are kept. Test: Task 7 `TestInstallHookRcEdge`.
3. **`undo` when the latest backup holds two files with the same basename, or a file whose original directory no longer exists:** a clear warning, no wrong-file overwrite, exit 1. Test: Task 4 `TestUndoEdge`.
4. **`upgrade` when the download fails part-way, the installer exits non-zero, or neither curl nor wget is on PATH:** the same messages as Bash, nothing replaced, temp files removed. Test: Task 8.
5. **`clone` and `verify` when git or ssh prompts or hangs** (unknown host key, passphrase): git's own prompt is passed through on the terminal and its exit code is returned. ssh in `verify` uses `-o BatchMode=yes` and a timeout so it can't hang. Test: Task 5.

---

### Task 1: Shared plumbing, global SIGINT handler, Bash fixes R5–R6

**Files:**
- `cmd/ctx-core/main.go`: the env flag reader, and the R10 handler moved from internal/cli/setup.go.
- `internal/ghacct` (new): the gh active-account and accounts parser, moved out of internal/setup/detect.go.
- `internal/guard`: export `Installed()`.
- A shared `CloneURLForProfile`, moved from internal/guard/check.go to internal/gitssh.
- `bin/ctx`: `_ctx_core_forward` also exports `CTX_LIB`.
- `lib/cmd_ops.sh` and `bin/ctx` (R5 `edit`, R6 `clone`), plus `test/test.sh`.

**Interfaces:**
- Produces:
  - `type Flags struct{ DryRun, Quiet, JSON, NoGum bool }` and `func FlagsFromEnv() Flags` in `internal/cli`.
  - `func ghacct.Parse(statusOutput string) []string` and `func ghacct.Active(statusOutput string) string`.
  - `func guard.Installed() bool`.
  - `func gitssh.CloneURLForProfile(profile, url string) string`.
  - `func cli.OnInterrupt(t tui.Term, kill func()) (stop func())`.
  - The `CTX_LIB` env in ctx-core.
- [ ] **Step 1: Write failing tests.**
  - Unit tests for each moved helper, with no behavior change (existing tests keep passing).
  - test.sh cases for R5 (`EDITOR="fake -w"` gets `-w <file>`; `VISUAL` wins) and R6 (`/bin/bash` 3.2 `clone` with no extra args, using fake git).
  - `TestOnInterruptAnyCommand`: a pty run of a plain `AskYN` in a small ctx-core path, with `\x03` giving 130 and one cancel line.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run all checks.** Expected: PASS.
- [ ] **Step 5: Commit** `refactor(core): shared helpers and Ctrl+C handling for the remaining commands`, plus `fix: edit runs EDITOR with arguments; clone works on bash 3.2`.

### Task 2: `version`, `help`, `completion`

**Files:** Create `internal/cli/help.go`, `internal/cli/completion.go`, and `internal/cli/text/{help_plain.txt,completion.zsh,completion.bash,completion.fish}` (embedded), with tests. Modify `cmd/ctx-core/main.go` (`implements version|-v|--version|help|-h|--help|completion` → true; `ctx-core version` stays `ctx-core <ver>`) and `bin/ctx` (forward these before the Bash fallbacks; `ctx` with no args counts as `help`).

**Interfaces:**
- Produces:
  - `func Version(w io.Writer) int` prints `ctx version <CTX_VERSION>`.
  - `func Help(t tui.Term, args []string) int`. `help setup|import|add|wizard` goes to `setup.Help`; rich mode renders a lipgloss card with the gum-help content.
  - `func Completion(w, errw io.Writer, args []string) int`.
- [ ] **Step 1: Write failing tests.**
  - `TestVersionHelpCompletionMatchBash`: byte-exact for `version`, `-v`, `--version`, `help` (plain), `help setup` and `completion zsh|bash|fish|bogus`, with exit codes.
  - `bash -n` / `zsh -n` (if present) on the completion output.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement the byte-identical port and commit** `feat(core): version, help and completion in Go`.
- [ ] **Step 4: R9 stale-text fixes** in `bin/ctx` and the embedded files, identically, with the test still differential. Commit `fix: help and completion list current commands and flags`.
- [ ] **Step 5: Run all checks.** Expected: PASS.

### Task 3: `agent-rules` and `edit`

**Files:** Create `internal/cli/agentrules.go` and `internal/cli/edit.go`, with tests. Modify main.go and bin/ctx.

**Interfaces:**
- Produces:
  - `func AgentRules(t tui.Term, args []string) int` ports `cmd_agent_rules`, including the `--write` upsert and dry-run.
  - `func Edit(t tui.Term, args []string) int` implements R5. The editor gets the Term's stdio, and Edit returns the editor's exit code.
- [ ] **Step 1: Write failing tests.**
  - `TestAgentRulesMatchesBash`: print, `--write` new, `--write` existing (upsert), `--write` under dry-run, a missing profile, and no active profile.
  - `TestEditMatchesBash`: a fake editor records argv and env; covers `VISUAL`, `EDITOR` with args, unset (nano fake), a missing profile, and no args.
- [ ] **Steps 2–5:** run (FAIL), implement, run all checks (PASS), then commit `feat(core): agent-rules and edit in Go`.

### Task 4: `undo` and `uninstall` (destructive)

**Files:** Create `internal/cli/undo.go` and `internal/cli/uninstall.go`, with tests. Modify `lib/cmd_ops.sh` (R7, R8 in Bash), main.go, bin/ctx and test.sh.

**Interfaces:**
- Consumes: `ctxconfig.LatestBackup`, `BackupFile`, `CopyFile`; `guard.Installed` / `guard.Uninstall`; `secret.Open(EffectiveProvider())` with List and Delete.
- Produces:
  - `func Undo(t tui.Term, args []string) int`, implementing R7 and R1.
  - `func Uninstall(t tui.Term, args []string) int`, implementing R8 and R1. The install location comes from `CTX_BIN_PATH` (the bin/ctx path) and `CTX_LIB`, not from `os.Executable`.
- [ ] **Step 1: Write failing tests**, all hermetic: a temp install prefix with a fake `bin/ctx` and `lib`, `PATH` pointing at it, and a temp `CTX_DIR`.
  - `TestUndoMatchesBash`: normal restore, restore under dry-run, no backups, no manifest, answering No, and EOF.
  - `TestUndoEdge` (Review Focus 3).
  - `TestUninstallMatchesBash`: plain, `--purge` with secrets in the file store, the guard installed, dry-run, No, a bad argument, and ctx not on PATH.
  - `TestUninstallSafety` (Review Focus 1).
  - Matching test.sh cases for the Bash side.
- [ ] **Steps 2–5:** run (FAIL), implement (Bash R7/R8 first, then Go), run all checks (PASS), then commit `fix: safer undo and uninstall` and `feat(core): undo and uninstall in Go`.

### Task 5: `verify` and `clone`

**Files:** Create `internal/cli/verify.go` and `internal/cli/clone.go`, with tests. Modify `lib/cmd_ops.sh` (R3, and the R1 clone dry-run), main.go, bin/ctx and test.sh.

**Interfaces:**
- Consumes: `ghacct.Active`, `gitssh.CloneURLForProfile`, `profile.Load`, `resolve` (the profile for the cwd).
- Produces:
  - `func Verify(t tui.Term, args []string) int`. The ssh check uses `ssh -o BatchMode=yes -o ConnectTimeout=10 -T git@github-<P>`; match Bash's argv first, adding the options in both. It implements R3.
  - `func Clone(t tui.Term, args []string) int` ports the arg parsing matrix and the URL rewrite. git runs attached to the terminal, and Clone returns git's exit code.
- [ ] **Step 1: Write failing tests.**
  - `TestVerifyMatchesBash`, with fake ssh and gh: all ok, gh mismatch, ssh denied, key missing, profile missing, and the active profile by default.
  - `TestCloneMatchesBash`, with fake git: `-p`/`--profile`, inferred profile, https and ssh and `github-` URLs, extra git args after `--`, `-q`, dry-run, git failing, and a missing URL.
- [ ] **Steps 2–5:** run (FAIL), implement, run all checks (PASS), then commit `fix: verify reads current gh output and exits 1 on failure` and `feat(core): verify and clone in Go`.

### Task 6: `doctor`

**Files:** Create `internal/cli/doctor.go` and its test. Modify `lib/cmd_ops.sh` (R4), main.go and bin/ctx. Update test.sh:867-910: the coverage guard must locate ctx-core some other way, for example `ctx-core version` or `CTX_CORE_BIN`.

**Interfaces:**
- Produces: `func Doctor(t tui.Term, args []string) int`. It ports every check in `cmd_doctor` with the same order, text and tool probes (the probes keep the `bash -lc` semantics if Bash uses them; see the map), plus the per-profile checks and R4.
- [ ] **Step 1: Write failing tests.** `TestDoctorMatchesBash` covers:
  - all tools present vs absent (hermetic PATH farm);
  - ctx-core present vs missing;
  - each secret provider;
  - the guard installed;
  - a profile with a missing key or a bad email;
  - the R4 case.
- [ ] **Steps 2–5:** run (FAIL), implement, run all checks (PASS), then commit `fix: doctor does not report all good after warnings` and `feat(core): doctor in Go`.

### Task 7: `install-hook` and `init` (rc edits)

**Files:** Create `internal/cli/installhook.go` and `internal/cli/initcmd.go`, with tests. Modify `lib/cmd_ops.sh` (R2, and R1 for init and install-hook), main.go, bin/ctx and test.sh.

**Interfaces:**
- Consumes: `ctxconfig.BackupFile`, `gitssh.EnsureInclude`, the `writeThrough`-style rc rewrite (shared with E4a), and `CTX_LIB` for the autoswitch lib path.
- Produces:
  - `func InstallHook(t tui.Term, args []string) int`, implementing R2 and R1. The appended block is byte-identical to Bash.
  - `func Init(t tui.Term, args []string) int` ports `cmd_init`'s prompts and steps, including its tool-install offers. Each tool install is run, or reported under dry-run, exactly as Bash does.
- [ ] **Step 1: Write failing tests.**
  - `TestInstallHookMatchesBash`: zsh, bash and fish rc, an explicit rc argument, a rerun (idempotent), mise present vs absent, a missing lib, and dry-run.
  - `TestInstallHookRcEdge` (Review Focus 2).
  - `TestInitMatchesBash` (pty): each prompt path.
  - Matching test.sh cases for the R2 Bash fixes.
- [ ] **Steps 2–5:** run (FAIL), implement (Bash fixes first), run all checks (PASS), then commit `fix: install-hook keeps the rest of your rc file` and `feat(core): install-hook and init in Go`.

### Task 8: `upgrade`

**Files:** Create `internal/cli/upgrade.go` and its test. Modify `lib/cmd_ops.sh` (R1 dry-run), main.go and bin/ctx.

**Interfaces:**
- Produces: `func Upgrade(t tui.Term, args []string) int`, implementing R11 and R1. The installer and the `CTX_LIB` line are found from `CTX_BIN_PATH`. The download goes through curl or wget as in Bash, so proxies and certificates behave the same; the CRLF normalization is done in Go. It runs `bash <tmp>` with the same exported env and removes the temp file on every path.
- [ ] **Step 1: Write failing tests.**
  - `TestUpgradeMatchesBash`: a local HTTP server, or `CTX_UPGRADE_REPO=file://…`, serving a stub `install.sh` that records its env and a stub `lib/core.sh`. Cases:
    - `--check` up to date, newer remote, offline;
    - a real run answering yes or no;
    - the download failing;
    - the installer exiting 1;
    - no curl or wget;
    - dry-run.
  - Review Focus 4.
- [ ] **Steps 2–5:** run (FAIL), implement, run all checks (PASS), then commit `feat(core): upgrade in Go`.

### Task 9: Docs

**Files:**
- `CHANGELOG.md`: a phase E4c line, and Fixed lines for R1–R9 as users see them.
- `README.md`: every command runs in ctx-core; Bash is the fallback until E6.
- `docs/sprint/SPRINT.md`: E4c done. Note E5 (`ctx hook-env`) and E6 (drop Bash: version source, install.sh file list, `upgrade --check`).
- `AGENTS.md`.

- [ ] **Step 1: Write the docs.**
- [ ] **Step 2: Run all checks.** Expected: PASS.
- [ ] **Step 3: Commit** `docs: Go port phase E4c`.
