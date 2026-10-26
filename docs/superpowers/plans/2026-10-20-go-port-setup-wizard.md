# Go port, phase E4b: the setup wizard (`setup`, `import`, `add`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move `ctx setup` and its aliases `import` and `add` into `ctx-core`. That covers all of the following:
- batch mode (`--config FILE`);
- the interactive clone-only and full wizards;
- `--extend NAME`;
- `--dry-run`;
- `--help`.

**Architecture:**
- **`internal/setup`:** a new package. It has a pure model (`Inputs`), validation, a config-file reader, external-account detectors, and a single writer, `Write`, that ports `_write_all`. The batch path and all wizard paths fill an `Inputs`, show the review, and call `Write`.
- **`internal/tui`:** gains `Ask`, `PickOne`, a step banner, a review table and a spinner. Plain output stays byte-identical to Bash `--no-gum`; rich output uses huh and lipgloss.
- **Testing:** the interactive wizards are tested differentially. Bash (`CTX_CORE=off bin/ctx --no-gum setup`) and Go each run under a pseudo-terminal with echo off, are fed the same answers, and are compared on the transcript, the exit code, the fake tools' argv, and a `HOME` snapshot.

**Tech Stack:**
- Go: existing modules plus the E4a lipgloss v2 and huh v2.
- Test-only: `github.com/creack/pty v1.1.23` (pty harness) and `github.com/BurntSushi/toml v1.4.0` (checks that generated `mise.toml` parses).

**Spec:** None. The maintainer asked to always take the recommended approach. The binding behavior is the Bash code on `feat/go-port-ui` at e9cca6c (lib/cmd_import.sh, plus the setup helpers in lib/core.sh), as corrected by Task 1 and the rulings below.

## Global Constraints

- Branch `feat/go-port-setup`, worktree `.claude/worktrees/go-port-setup`, base e9cca6c. PR base: `feat/go-port-ui`.
- **Plain mode** (non-TTY, or `--no-gum`/`CTX_NO_GUM=1`) must match Bash with `--no-gum`. The match covers:
  - stdout and stderr bytes;
  - exit codes;
  - files written, both content and mode;
  - external tool argv and order;
  - backups under `~/.ctx/backups` (timestamps normalized in tests).
- **Rich mode** keeps the same wording, order, defaults and exit codes; only the styling differs.
- **Cancellation:** Ctrl+C or EOF at any prompt prints `! Cancelled by user.` and exits 130. Nothing under `~/.ctx/profiles`, `~/.config/git`, `~/.gitconfig`, `~/.ssh/ctx_config` or `<WORK_DIR>/mise.toml` changes unless the review was confirmed (or auto-confirmed).
- **Forwarding:** `bin/ctx` forwards `setup|import|add` to ctx-core through `_ctx_core_forward`. `implements setup` is true only for argument shapes that are fully implemented. Task 4 turns it on for `--config`; Task 7 turns it on for everything.
- **Profile files** are written atomically at mode 0600 from the start. Go writes `KEY=value` with `bashQuote` (internal/cli/env.go), which must produce bytes identical to Bash `printf '%q'` for every value in the tests.
- **Checks:** ShellCheck-clean, bash 3.2 compatible. `go vet ./... && go test ./...`, `bash test/test.sh` and `CTX_CORE=off bash test/test.sh` all pass.
- No `Co-authored-by` or agent trailers.

## Rulings (Bash bugs fixed in Bash and Go, not mirrored)

- **R1:** `--extend NAME` works. The guard at cmd_import.sh:309 compares the strings "true"/"false" with `!`, so every `--extend` call dies today. That makes the clone summary's "Later: ctx setup --extend P" a dead end. *Cost if wrong:* a feature nobody could use stays usable.
- **R2:** `--dry-run` is honored in interactive mode too. None of these run under dry-run: `ssh-keygen`, `gh auth login`/`switch`/`ssh-key add`, secret storage, `mkdir` of paths, or `ctx_init_dirs`. Each skipped action prints `dim "  [dry-run] would <action>"`. The "Would create" list names only files that would actually be written. *Cost if wrong:* a dry run changes nothing, which is the point of it.
- **R3:** `detect_gh_accounts` parses both gh formats, `… account USER (keyring)` and `… as USER (keyring)`. The account is the word after `account` or `as`. *Cost if wrong:* the picker shows real accounts instead of "keyring" or nothing.
- **R4:** secrets entered in step 6 (and in extend) are stored only after the review is confirmed. A key is added to `SECRET_KEYS` only if storing it succeeded. Store failures warn as before. *Cost if wrong:* a secret entered and then aborted is not stored, which matches "Nothing is written until you confirm".
- **R5:** every generated key's comment uses `_ctx_setup_ssh_key_comment`, which falls back to `ctx-<P>@local`. An empty `-C ""` is never used.
- **R6:** the SSH-key choice index is parsed portably. Bash uses `sed 's/^\[\([0-9][0-9]*\)\].*/\1/'`; Go parses directly. Today, BSD sed can silently generate a new key after the user picked an existing one.
- **R7:** `mise.toml` values are TOML basic strings, with `\` and `"` escaped and control characters as `\uXXXX`. If `mise.toml` already exists, the message is `dim "  mise.toml kept (already exists): <path>"`, not "generated".
- **R8:** `ask` in plain mode reads stdin like every other prompt. The `/dev/tty` fallback is removed, so scripted plain runs read their answers from stdin. Under gum, the `< /dev/tty` on gum calls stays.
- **R9:** an empty `SECRET_KEYS` array no longer trips `set -u` on bash 3.2 (`${SECRET_KEYS[*]+"${SECRET_KEYS[*]}"}`).
- **R10:** with ctx-core installed, the rich UI does not need gum (as in E4a, R1). The Bash fallback still uses gum when present.

## Review Focus

1. **Ctrl+C or EOF at any step, including the review "No":** nothing listed under Cancellation changes, and no secret is stored (R4). An SSH key generated earlier in the run stays, as it does in Bash. Test: Task 7 `TestWizardCancelEachStep` (EOF injected after step k, for every k).
2. **Re-running setup for an existing profile with Overwrite=yes:**
   - The includeIf is not duplicated.
   - The SSH host block is not duplicated.
   - An existing `mise.toml` is kept, with the R7 message.
   - The identity file is rewritten and the insteadOf rewrite is restored.
   Test: Task 4 `TestBatchRerunIdempotent`.
3. **Values with spaces, quotes, `$`, backslashes, unicode or newlines** in the name, path, env values or GPG key: `.conf` round-trips through `profile.Load` to the same values, `mise.toml` parses with BurntSushi/toml, and the bytes match Bash. Test: Task 4 `TestWriteRoundTrip`.
4. **Account tools that are missing, failing or hanging** (`gh`, `aws`, `kubectl`, `az`, `gcloud`): detection degrades to empty lists, and each hanging tool is cut off at 8 s. Test: Task 3 `TestDetectTimeout` with a fake that sleeps 30 s (the timeout is injectable; the test uses 200 ms).
5. **Setup on a non-terminal stdin without `--config`:** plain prompts read stdin, and EOF cancels with exit 130 and nothing written. `--config` on non-TTY stdin without `--yes`/`AUTO_CONFIRM` dies with the Bash message. Tests: Task 7 `TestWizardPipedStdin`, Task 4 `TestBatchNeedsYes`.

---

### Task 1: Bash fixes R1–R9

**Files:** Modify `lib/cmd_import.sh`, `lib/core.sh` (`ask`, `detect_gh_accounts`, `generate_mise_toml`) and `test/test.sh`.

**Interfaces:**
- Produces: the behavior in rulings R1–R9, in Bash. Later Go tasks are compared against it.

- [ ] **Step 1: Write failing tests in test.sh**, each driving `CTX_CORE=off bin/ctx --no-gum …` with plain stdin answers (possible once R8 is in):
  - `--extend` on an existing profile reaches the extend flow (R1).
  - `--dry-run setup` through a generate-key path leaves `HOME` byte-identical (snapshot via `find … -exec shasum`) and prints `[dry-run] would` lines (R2).
  - A fake `gh` printing both line formats gives both users in the step-4 picker (R3).
  - Aborting at the review leaves no secret file (`secret_provider=file`) (R4).
  - A full run with an empty email never calls `ssh-keygen -C ""` (fake ssh-keygen records argv) (R5).
  - Choosing existing key `[2]` uses key 2 (R6).
  - An env value `a"b\c` gives `mise.toml` that `python3 -c 'import tomllib…'` parses back to `a"b\c`. Skip if python lacks tomllib (R7).
  - With bash 3.2 at `/bin/bash`, a run with no secrets doesn't die on an unbound variable (R9).
- [ ] **Step 2: Run `bash test/test.sh`.** Expected: the new tests FAIL.
- [ ] **Step 3: Implement R1–R9.**
- [ ] **Step 4: Run** `bash test/test.sh`, `CTX_CORE=off bash test/test.sh` and ShellCheck. Expected: all pass.
- [ ] **Step 5: Commit** `fix(setup): extend works, dry-run is honored, secrets wait for confirm, and other wizard fixes`.

### Task 2: `internal/setup` model, validators and config reader

**Files:** Create `internal/setup/model.go`, `validate.go`, `config.go` and their tests. Export `WorkRoot()` from internal/cli/config.go, or move it to internal/ctxconfig and re-point its caller.

**Interfaces:**
- Produces:
  - `type Inputs struct` with:
    - `Scope string` (`clone`|`full`), `Profile, GitName, GitEmail, WorkDir string`;
    - `ImportExisting bool`, `SSHKeyPath string`, `SSHKeyGenerated bool`;
    - `GitHubUser, AWSProfile, AzureSub, AzureTenant, GCPProject, GCPAccount, KubeContext string`;
    - `SecretKeys []string`, `PendingSecrets []Secret` (stored after confirm, R4), `ExtraEnvs []EnvPair`;
    - `SigningFormat, SigningKey string`;
    - `Overwrite, AutoYes, Extend bool`.
  - `type Secret struct{ Key, Value string }` and `type EnvPair struct{ Key, Value string }`.
  - Pure functions ported from Bash:
    - `func Slug(name string) string` (profile slug);
    - `func FolderSlug(s string) string`;
    - `func SensibleEmail(s string) bool`;
    - `func ValidEnvKey(s string) bool`;
    - `func NormalizeSigning(s string) (string, error)`;
    - `func KeyComment(profile, email string) string`.
  - `type ConfigFile map[string]string`, from `func ReadConfig(path string) (ConfigFile, error)`, with the semantics of `ctx_setup_cfg_get`: last match wins, value trimmed, CR stripped, one pair of surrounding quotes removed.
  - `func (c ConfigFile) Get(key, def string) string` and `func Truthy(s string) bool`.
  - `type Out interface{ Info(string); Success(string); Warn(string); Dim(string); Bold(string) }`. internal/cli's `ui` gets an adapter that implements it with Bash-identical bytes.
  - `func FromConfig(c ConfigFile, opts BatchOpts) (Inputs, error)`, where `type BatchOpts struct{ ScopeFlag string; AutoYes, DryRun bool; PWD, Home string }`. It ports the batch resolution and validation in cmd_import.sh:318-470. Each Bash `die` text is returned as the error message verbatim, and the caller prints it.
- [ ] **Step 1: Write failing tests.**
  - `TestPureMatchesBash` sources lib/core.sh and lib/cmd_import.sh in `bash -c` and compares each pure function on a table: unicode, `..`, `a@b`, `x@y.z`, mixed case, slashes, spaces.
  - `TestReadConfigMatchesBash` covers quotes, CRLF, repeated keys, `#` lines and empty values.
  - `TestFromConfigErrors` has one case per batch `die` message (about 15), with exact text.
- [ ] **Step 2: Run the tests.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run `go vet ./... && go test ./internal/setup/ ./internal/cli/`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(setup): model, validators and config reader in Go`.

### Task 3: Detectors

**Files:** Create `internal/setup/detect.go` and its test.

**Interfaces:**
- Consumes: `Out` from Task 2.
- Produces:
  - `type Detected struct{ SSHKeys, GHAccounts, AWSProfiles, KubeContexts, AzureSubs, GCPProjects []string }`
  - `func Detect(home string, timeout time.Duration) Detected`, porting `detect_*` with R3. The gh, az and gcloud calls are bounded by `timeout` (8 s in production); kubectl and the files are not, as in Bash.
  - `func (d Detected) Report(u Out)`: the "Found N …" / "No …" summary lines, in Bash order.
  - [ ] **Step 1: Write failing tests.**
  - `TestDetectMatchesBash` uses fakes for each tool and fixture `~/.ssh` and `~/.aws/{credentials,config}` files. It compares the lists and the `Report` output with Bash's `detect_*` and scan block.
  - `TestDetectTimeout` (Review Focus 4).
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run all checks.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(setup): account and key detection in Go`.

### Task 4: Writer and batch mode (`setup --config`)

**Files:**
- Create `internal/setup/write.go` (with `mise.go`, `identity.go` and `sshhost.go` as needed) and `internal/cli/setup.go`, with tests.
- Modify `cmd/ctx-core/main.go`: `implements setup|import|add` is true when the args contain `--config`.
- Modify `bin/ctx`: `setup|import|add) _ctx_core_forward setup "$@"; cmd_import "$@"`. Forward the canonical name with the original args.

**Interfaces:**
- Consumes:
  - Task 2's `Inputs`, `FromConfig` and `KeyComment`;
  - `ctxconfig.BackupFile`, `ctxconfig.InitDirs`;
  - `ensureSSHInclude` and `gitconfigAddInclude` (internal/cli/ssh.go — move them to an internal package both cli and setup can import, e.g. `internal/gitssh`, keeping behavior);
  - `gitident.SetRewrite`, `secret.Open`, `bashQuote`.
- Produces:
  - `func Write(in Inputs, u Out) error`. It ports `_write_all`, including the signing normalization, the `.conf` (exact header and key order), the identity file via `git config --file`, `ctx_git_identity_set_signing`, the includeIf, the SSH host add (new `AddHost(host, key string) (added bool, err error)` porting `ctx_ssh_add_host`), the insteadOf rewrite, `mkdir` of `WORK_DIR`, `mise.toml` (R7) and `mise trust`. It also stores `PendingSecrets` first (R4), each followed by the success or warn line.
  - `func Backups(in Inputs) error`: the three `backup_file` calls that come before `Write`.
  - `func DryRunReport(in Inputs, u Out)`: the R2 "Would create" lines.
  - `func Summary(in Inputs, u Out, rich bool)`: the clone, full and extend summaries.
  - `func SetupIn(t tui.Term, args []string) int`: parses the flags exactly as cmd_import.sh:274-313 does. For `--config` it runs the scan (for parity; output is the Task 3 `Detected.Report`), then `FromConfig`, then the review (auto-confirm only; the interactive review comes in Tasks 5–7, and until then `implements` is false when a TTY review would be needed — see Step 3), then the backups, `Write` and `Summary`.
- [ ] **Step 1: Write failing tests.**
  - `TestBatchMatchesBash` runs `CTX_CORE=off bin/ctx --no-gum setup --config F --yes` against Go. It compares stdout, stderr, exit code, the `HOME` snapshot and the argv of fake `git`/`ssh-keygen`/`mise`. It covers:
    - full and clone scopes;
    - `SSH_MODE` generate, existing and skip;
    - every `WORK_MODE`;
    - signing ssh, gpg and none;
    - `--dry-run`;
    - OVERWRITE missing vs true.
    
    The examples in `examples/setup.*.conf.example` must be among the fixtures.
  - `TestWriteRoundTrip` (Review Focus 3): `profile.Load` round-trips the `.conf` and BurntSushi/toml parses `mise.toml`.
  - `TestBatchRerunIdempotent` (Review Focus 2).
  - `TestBatchNeedsYes`.
- [ ] **Step 2: Run the tests.** Expected: FAIL.
- [ ] **Step 3: Implement.** `implements setup` returns true only when `--config` is present and either `--yes`/`-y` is present or the file sets `AUTO_CONFIRM` truthy. Read the file in `implements`; on any read error return false so Bash reports it.
- [ ] **Step 4: Run all checks from Global Constraints.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): ctx setup --config in Go`.

### Task 5: TUI additions and the pty test harness

**Files:**
- Modify `internal/tui` (`tui.go`, `plain.go`, `rich.go`, `style.go`).
- Create `internal/testutil/pty.go` (build tag `!windows`).
- Tests go in `internal/tui` and `internal/testutil`.

**Interfaces:**
- Produces:
  - `Prompter` gains:
    - `Ask(prompt, def, hint string) (string, error)`, the plain bytes of the Bash `ask` plain branch after R8;
    - `PickOne(prompt string, items []string) (string, error)`, the plain bytes of `pick_one`, where invalid or 0 returns "" and an empty list returns "" without prompting.
  - `func Step(t Term, n, total int, title, why string) string`: the `ctx_setup_step` bytes in plain mode.
  - `func ReviewTable(t Term, rows [][2]string) string`: plain uses `printf '  %-16s %s\n'`; rich is a rounded lipgloss table with headers `Setting`/`Value` and widths 20/58.
  - `func Spin(t Term, msg string, fn func()) `: rich shows a dot spinner on Err while `fn` runs; plain shows `info msg`, then runs `fn`.
  - testutil:
    - `func RunPTY(t *testing.T, cmd *exec.Cmd, answers []string, timeout time.Duration) (transcript string, exit int)`. It starts `cmd` on a pty with echo off and a 120×40 window, writes each answer followed by `\r`, waits for a 300 ms quiet gap in output between answers, and returns the transcript with `\r\n` → `\n`.
    - `EOF` is a sentinel answer that sends Ctrl+D.
- [ ] **Step 1: Write failing tests.**
  - `TestPlainAskMatchesBash` and `TestPlainPickOneMatchesBash` source core.sh with `HAS_GUM=false` over piped stdin and compare exact stderr bytes, the answer and the cancel paths.
  - `TestRichAskPickOne` drives huh via `WithInput`.
  - `TestStepAndReviewGolden` uses goldens with the ANSI256 profile forced.
  - `TestRunPTY` runs `bash -c 'read -r a; echo "got:$a"'` and asserts `got:x`.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.** Add `github.com/creack/pty v1.1.23` as a test-only import.
- [ ] **Step 4: Run all checks.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(tui): ask, pick, step banner, review table, spinner; pty test harness`.

### Task 6: Clone-only wizard

**Files:** Create `internal/setup/wizard.go` (shared steps) and `internal/setup/clone.go` with tests. Modify `internal/cli/setup.go`.

**Interfaces:**
- Consumes: `Prompter` (`Ask`, `AskYN`, `AskSecret`, `PickOne`), `Step` and `Spin` from Task 5; `Detect` from Task 3; `Write`, `Backups`, `Summary` and `DryRunReport` from Task 4.
- Produces:
  - `type Wizard struct{ T tui.Term; P tui.Prompter; U Out; Det Detected; DryRun bool; PWD, Home string; Run Runner }`, where `type Runner func(name string, args ...string) error` runs external tools.
  - `func (w *Wizard) GenKey(path, comment string) error` ports `_gen_ssh_key`, with R2 and R5.
  - `func (w *Wizard) UploadKey(user, pub, profile string) error`: `gh auth switch -u` then `gh ssh-key add … --title ctx-<P>-<hostname>-<YYYYMMDD>`. Hostname and date are injectable for tests.
  - `func (w *Wizard) Clone(in *Inputs) error`: the three clone steps.
  - `SetupIn` routes `--scope clone` (and the mode picker's clone answer, from Task 7) here. The review then comes from `tui.ReviewTable` plus `AskYN("Write this profile to disk now?", true)`.
- [ ] **Step 1: Write failing tests.** `TestCloneWizardMatchesBash` uses `RunPTY` on Bash and on Go with `--no-gum --scope clone` and the same answers. It covers:
  - existing key, generate key, and generate with a passphrase;
  - each folder choice;
  - GitHub user plus upload yes/no;
  - review no (exit 1, `Aborted before write.`);
  - `--dry-run`.
  
  It compares the transcript, exit code, argv logs and `HOME` snapshot.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.** `implements setup` also becomes true for `--scope clone` without `--config`.
- [ ] **Step 4: Run all checks.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(setup): clone-only wizard in Go`.

### Task 7: Full wizard, help, and forwarding everything

**Files:** Create `internal/setup/full.go`, `internal/setup/help.go` and tests. Modify `internal/cli/setup.go`, `cmd/ctx-core/main.go` (`implements setup|import|add` is always true; `usage` lists setup) and `test/test.sh` (forwarding test).

**Interfaces:**
- Consumes: Tasks 3–6.
- Produces:
  - `func (w *Wizard) Full(in *Inputs) error`: the welcome banner, scan, the mode picker, steps 1–7 with their exact texts, and the signing picker.
  - The step-8 review runs through `ctx_setup_review_confirm` semantics, including the auto-yes line and the different plain/rich row labels.
  - Dry-run report, backups, `Write` and summary.
  - `func Help(t tui.Term) string` ports `cmd_setup_help` (plain and rich), for `ctx setup|import|add --help`. `ctx help setup` stays in Bash until E4c, so both must print the same plain bytes; test that.
- [ ] **Step 1: Write failing tests.**
  - `TestFullWizardMatchesBash` (pty) covers:
    - import vs manual mode;
    - each SSH branch;
    - gh login/switch declined;
    - each cloud picker, including "None / skip" then typed;
    - secrets skip vs add, including an invalid key and a store failure;
    - extra envs;
    - signing ssh, gpg (and empty → none) and keep-current;
    - review yes/no;
    - an existing profile, overwrite no → `Aborted.`
  - `TestWizardCancelEachStep` (Review Focus 1).
  - `TestWizardPipedStdin` (Review Focus 5).
  - `TestHelpMatchesBash`.
  - `TestRichWizardSmoke`: a pty without `--no-gum`, driving huh with arrow and Enter keys through a clone run. Assert the files, not the transcript.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run all checks.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): full ctx setup wizard in Go`.

### Task 8: `--extend`

**Files:** Create `internal/setup/extend.go` and its test. Modify `internal/cli/setup.go`.

**Interfaces:**
- Consumes: Tasks 3–7, plus `profile.Load` to prefill `Inputs` from the existing profile (every field, including `SecretKeys`, `ExtraEnvs` and signing).
- Produces: `func (w *Wizard) Extend(in *Inputs) error`, porting `_ctx_setup_interactive_extend` and the "Profile 'X' updated." summary.
- [ ] **Step 1: Write failing tests.** `TestExtendMatchesBash` (pty), Bash vs Go, covers:
  - each optional section yes/no;
  - extend of a clone profile into a full one;
  - extend of a profile with existing secrets, which must be kept in `SECRET_KEYS`;
  - a missing profile, with the die text.
- [ ] **Step 2: Run.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run all checks.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(setup): ctx setup --extend in Go`.

### Task 9: Docs

**Files:** `CHANGELOG.md` (phase E4b line, plus a Fixed line for each of R1–R9 that a user would notice), `README.md` (setup runs in ctx-core), `docs/sprint/SPRINT.md` (E4b done), `AGENTS.md` (`internal/setup` row), and `bin/ctx` completions (add `--scope` and `--extend`).

- [ ] **Step 1: Write the docs and completions.** Run `bash -n bin/ctx`.
- [ ] **Step 2: Run all checks.** Expected: PASS.
- [ ] **Step 3: Commit** `docs: Go port phase E4b`.
