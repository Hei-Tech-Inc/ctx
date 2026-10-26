# Go port, phase E4a: terminal UI, text list/status, use, secret set/migrate, remove

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Go terminal UI layer and use it to move text `list`, text `status`, non-quiet `use`, `secret set`, `secret migrate` and `remove` into `ctx-core`.

**Architecture:** This uses the same forwarding as E2 and E3: `bin/ctx` asks `ctx-core implements <cmd> [sub]`. A new package, `internal/tui`, holds two parts:
- **Prompter:** the interactive prompts. It has a *plain* implementation that is byte-identical to the Bash plain fallbacks (`ask_secret`, `ask_yn`) and a *rich* implementation built on huh v2.
- **Styles:** lipgloss v2 renderers that match the gum boxes Bash draws (`ctx_header`, the status box, the "Active context" line).

Plain mode is tested differentially against `CTX_CORE=off bin/ctx --no-gum`, comparing stdout, stderr, exit code, tool argv and resulting files. Rich mode is tested with golden strings and by driving huh with scripted input.

**Tech Stack:** Go. New dependencies: `charm.land/lipgloss/v2 v2.0.5` and `charm.land/huh/v2 v2.0.2`. Any `github.com/charmbracelet/x/*` or `colorprofile` modules come in only as their transitive dependencies.

**Spec:** None, as in E2 and E3. The maintainer asked to always take the recommended approach. Scope comes from SPRINT.md row E4, which this plan splits:
- **E4a:** this plan.
- **E4b:** the `setup` wizard with its `import`/`add` aliases, plus `Ask`/`PickOne`/`PickMany`/`Spin`.
- **E4c:** the remaining Bash-only commands: `agent-rules`, `doctor`, `verify`, `clone`, `init`, `install-hook`, `upgrade`, `uninstall`, `undo`, `edit`, `completion` and `help`.

The binding behavior is the Bash code on `feat/go-port-write` at ad46e50, as corrected by Task 1.

## Global Constraints

- Branch `feat/go-port-ui`, worktree `.claude/worktrees/go-port-ui`, base ad46e50. PR base: `feat/go-port-write`.
- **Rich vs plain:** Go uses the rich UI exactly when stdin and stdout are both terminals and `CTX_NO_GUM` is not `1`. Otherwise it uses plain mode. gum does not need to be installed. This intentionally changes behavior for users without gum, who now get the rich UI from Go (ruling R1).
- **Plain mode** must match Bash run with `--no-gum`: stdout, stderr text, exit codes, files written (content and mode), and external tool argv and order. Rich mode keeps the same wording, order and exit codes, and only the styling differs from gum.
- **Colors:** ANSI colors appear only when stdout is a terminal, as in E3's `ttyColors`. Rich output goes through a lipgloss/colorprofile writer so that `NO_COLOR` and dumb terminals get downgraded.
- **Cancellation:** Ctrl+C or EOF at any prompt prints `! Cancelled by user.` (yellow `!` only on a TTY) to stderr, exits 130, and leaves nothing changed by the cancelled step.
- **Checks:** ShellCheck-clean, bash 3.2 compatible. `go vet ./... && go test ./...`, `bash test/test.sh` and `CTX_CORE=off bash test/test.sh` all pass.
- No `Co-authored-by` or agent trailers in commits or the PR.

## Rulings

- **R1:** rich UI no longer needs gum. Why: the reason for using Charm in Go is to drop the gum runtime dependency. Cost if wrong: one env check to restore.
- **R2:** Bash plain prompts write everything except the answer to stderr (Task 1). Why: `val=$(ask_secret …)` and `x=$(pick_one …)` capture stdout. Without gum, `ctx secret set` currently stores `? Value for KEY (hidden): \n<value>` as the secret (reproduced), and the setup pickers show no menu. Cost if wrong: none. Prompts on stderr is how `ask` already behaves.
- **R3:** `remove` deletes only the exact `  path = <identity file>` line and the `[includeIf …]` header directly above it. Bash's `grep -v "$identity_file"` also matches `ctx-acme-old` when removing `acme`, and it leaves the header behind. Both are fixed in Bash and Go (Task 6). Cost if wrong: a stale include line, the same as today.
- **R4:** `ssh-add -l` in `status` is matched as a substring (Go `strings.Contains`) where Bash uses a `grep` regex. They differ only for key paths containing regex metacharacters.

## Review Focus

1. **Ctrl+C or EOF at a prompt** in `secret set`, `secret migrate`'s delete question, or `remove`'s confirmation: exits 130 with no partial write. Tests: Task 3 `TestSecretSetCancelled`, Task 6 `TestRemoveDeclined`.
2. **Secret values with surrounding spaces, `$`, quotes, backslashes or a leading `-`:** plain mode stores what Bash's `read -rs` would keep. Leading and trailing whitespace is trimmed and backslashes are kept. Rich mode stores exactly what was typed. The keychain argv must be identical. Test: Task 3 table.
3. **Not a terminal** (`ctx list | cat`, `ctx status > f`, piped stdin): plain mode, no escape codes, no TUI drawn. Tests: Task 2 `TestRichDetection`, Task 4 `TestListTextNoTTY`.
4. **`migrate` to a provider that isn't available** (no `pass` on PATH): same copied/skipped counts and messages as Bash, and the source is not touched unless the user confirms deletion. Test: Task 3.
5. **Removing a profile whose name is a prefix of another** (`acme` / `acme-old`), and removing the active profile: only `acme`'s SSH block, include and secrets go, and activation is cleared. Test: Task 6.

---

### Task 1: Bash fixes: prompts to stderr, `--no-gum` export, secret error label

**Files:** Modify `lib/core.sh` (`ask_secret`, `ask_yn`, `pick_one`, `pick_many` plain branches), `lib/cmd_ops.sh` (`cmd_secret`: `$(_lbl)` → `$_lbl` in the two `die` messages), `bin/ctx` (`--no-gum` also sets and exports `CTX_NO_GUM=1`) and `test/test.sh`.

**Interfaces:**
- Produces:
  - The plain fallbacks write the prompt, the menu and the trailing newline after hidden input to stderr. Stdout carries only the answer. The `gum` branches are unchanged.
  - `CTX_NO_GUM=1` is exported to ctx-core when `--no-gum` is given.

- [ ] **Step 1: Write failing tests in test.sh**
  - `printf 'hunter2\n' | ctx --no-gum secret set p API_KEY` with `secret_provider=file` gives a file containing exactly `hunter2`, and stdout contains no `?`.
  - `pick_one`, sourced from lib/core.sh with `HAS_GUM=false` and stdin `2`, given `x a b c`, makes `$(...)` capture exactly `b`.
  - `pick_many` with stdin `1,3` captures `a\nc`.
  - Under a failing provider (`secret_provider=pass` with no `pass` on PATH), `secret set` stderr contains `provider 'pass store'`, or whatever `ctx_secret_store_label` prints, and does not contain `command not found`.
- [ ] **Step 2: Run `bash test/test.sh`.** Expected: those tests FAIL.
- [ ] **Step 3: Implement.** Redirect each plain-branch `echo` of a prompt or menu line with `>&2`. Leave the `echo "$var"` / `echo "${items[...]}"` answer lines on stdout.
- [ ] **Step 4: Run `bash test/test.sh`, `CTX_CORE=off bash test/test.sh` and ShellCheck.** Expected: all pass.
- [ ] **Step 5: Commit** `fix(ui): plain prompts write to stderr so captured answers stay clean`.

### Task 2: `internal/tui`: prompts and styles

**Files:**
- Create `internal/tui/tui.go` (detection, `Term`), `internal/tui/plain.go`, `internal/tui/rich.go`, `internal/tui/style.go` and their `_test.go` files.
- Modify `go.mod` and `go.sum`.

**Interfaces:**
- Produces:
  - `type Term struct { In io.Reader; Out, Err io.Writer; Rich bool; Colors bool }`
  - `func FromOS() Term`: `Rich` follows Global Constraints and `Colors = isatty(stdout)`. `In`, `Out` and `Err` are the os files.
  - `var ErrCancelled = errors.New("cancelled")`
  - `type Prompter interface { AskSecret(prompt string) (string, error); AskYN(prompt string, def bool) (bool, error) }`
  - `func (t Term) Prompter() Prompter`: plain or rich.
  - `func (t Term) Cancelled() int`: prints the cancel line to `t.Err` and returns 130.
  - **Plain `AskSecret`:** writes `\033[0;36m?\033[0m <prompt> \033[2m(hidden)\033[0m: ` to Err (colors only if `Colors`). It reads one line from `In`, without echo when `In` is a terminal (golang.org/x/term), then writes `\n` to Err and returns the line trimmed of leading and trailing spaces and tabs, the same as `read -rs`.
  - **Plain `AskYN`:** writes `? <prompt> [Y/n]: ` or `[y/N]: ` to Err. Empty means the default; it returns true only for `y` or `Y`. EOF on either prompt returns `ErrCancelled`.
  - **Plain input reader:** both prompts share one `bufio.Reader` on `In`, so successive prompts read successive lines.
  - **Rich:** `huh.NewInput().EchoMode(huh.EchoModePassword)` and `huh.NewConfirm()`, with the accent color `99`, the same title text as the gum flags in lib/core.sh, and the affirmative/negative labels swapped when the default is "n", as `ask_yn` does. `huh.ErrUserAborted` maps to `ErrCancelled`. Both read and write through the `Term` streams (`WithInput` / `WithOutput`).
  - `func Header(t Term, title string) string`: rich is a rounded border in color 99, padding 0 2, margin 1 0, bold. Plain is the `bold "\n  <title>\n"` bytes that Bash `ctx_header`/`cmd_status` emit.
  - `func Box(lines []string) string`: a normal border with padding 0 1, matching `gum style --border normal --padding "0 1"`.
  - `func Accent(fg int, s string) string`: foreground-colored text, used for `Active context` with color 78.
- [ ] **Step 1: Write failing tests**
  - `TestRichDetection` is a table over (stdin tty, stdout tty, CTX_NO_GUM) and checks that only (yes, yes, unset) is rich. Use an injectable `isTerminal func(fd uintptr) bool`.
  - `TestPlainAskYN` covers inputs `""`, `"y"`, `"Y"`, `"yes"` and `"n"` with defaults y and n, giving true/false according to the rules above. It asserts the exact stderr bytes with Colors false and true, and that EOF gives `ErrCancelled`.
  - `TestPlainAskSecret`: input `"  a b\\c$x  \n"` gives `a b\c$x`. The stderr bytes are exact and EOF gives `ErrCancelled`.
  - `TestPlainMatchesBash` runs `bash -c 'source lib/core.sh; HAS_GUM=false; ask_yn "Q?" n; echo rc=$?'` and the same for `ask_secret` with identical stdin. It compares stderr and the answer with the Go plain prompter.
  - `TestRichPrompts` drives huh with `WithInput(strings.NewReader("s3cret\r"))` and `("\r")` for confirm-with-default, and checks the returned values. Ctrl+C (`"\x03"`) gives `ErrCancelled`.
  - `TestHeaderGolden` and `TestBoxGolden` compare against `testdata/*.golden`, with the ANSI256 profile forced. Generate the goldens once, inspect them in a terminal, and commit them.
- [ ] **Step 2: Run `go test ./internal/tui/`.** Expected: FAIL (package missing).
- [ ] **Step 3: Implement.** Add the dependencies at exactly the versions in Tech Stack.
- [ ] **Step 4: Run `go vet ./... && go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(tui): plain and Charm prompts and styles for ctx-core`.

### Task 3: `secret set` and `secret migrate`

**Files:** Modify `internal/secret/providers.go` (add `Set`), `internal/cli/secret.go`, `cmd/ctx-core/main.go` (`implements secret set|migrate` → true) and `bin/ctx` (forward `set` and `migrate`). Test in `internal/secret/providers_test.go` and `internal/cli/secret_test.go`.

**Interfaces:**
- Consumes: `tui.Term`, `Prompter.AskSecret`, `Prompter.AskYN` and `Term.Cancelled` from Task 2, plus `secret.Open` and `secret.EffectiveProvider` from E3.
- Produces:
  - `Store` gains `Set(profile, key, value string) error`, ported from `_secret_{keychain,file,pass}_set` with the same argv, stdin and modes:
    - keychain: delete, then `add-generic-password -a $USER -s ctx-<p>-<k> -w <v> -T "" -U`;
    - file: dirs 0700, file 0600, no trailing newline;
    - pass: `pass insert -m -f ctx/<p>/<k>` with stdin `<v>\n`.
  - `func OpenWith(provider string) (Store, error)` mirrors `secret_*_with_provider`: keychain requires darwin and `security`, pass requires `pass`, and otherwise returns `ErrUnavailable`.
  - `func SecretIn(t tui.Term, args []string) int` replaces `Secret` as the entry point. get, list and delete keep their E3 behavior.
- [ ] **Step 1: Write failing tests**
  - `TestStoreSetMatchesBash`: for each provider with fakes (pattern from E3 `TestProvidersMatchBash`), compares argv, stdin and file contents with `_secret_*_set`. Values to use: `hunter2`, `a b`, `-x`, `$HOME`, `q"'`.
  - `TestSecretSetMatchesBash` (plain): `CTX_CORE=off bin/ctx --no-gum secret set p K` vs Go with the same stdin. Compares stdout, stderr, exit code and the stored value, for a valid profile, a missing profile, missing args and a failing provider.
  - `TestSecretSetCancelled`: empty stdin exits 130, the cancel line goes to stderr, and nothing is stored.
  - `TestSecretMigrateMatchesBash` (plain), with these cases:
    - file→pass with fake `pass`, answering `n` and then `y` to the delete question;
    - pass→file with `pass` absent;
    - same provider;
    - invalid provider;
    - no keys;
    - one empty key, which is counted as skipped.
  - `TestSecretSetRich`: Term with Rich true and scripted huh input stores exactly `s3cret`.
- [ ] **Step 2: Run `go test ./internal/secret/ ./internal/cli/ -run 'Set|Migrate'`.** Expected: FAIL.
- [ ] **Step 3: Implement**, then wire `implements` and the `bin/ctx` forwarding: `case "${1:-}" in get|list|delete|set|migrate)`.
- [ ] **Step 4: Run** `go vet ./... && go test ./...`, `bash test/test.sh` and `CTX_CORE=off bash test/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): secret set and migrate in Go`.

### Task 4: Text `list` and `status`

**Files:** Create `internal/cli/listtext.go` and `internal/cli/statustext.go` with tests. Modify `cmd/ctx-core/main.go` (`implements list|status` for sub `""` and `--json`) and `bin/ctx`: forward `list` and `status` always, appending `--json` when `CTX_JSON=1`.

**Interfaces:**
- Consumes: `tui.Header`, `tui.Box` and `tui.Term` from Task 2, `profile.Load`, `ctxconfig.Load`, and the E2 JSON handlers, which stay as they are.
- Produces:
  - `func ListText(t tui.Term) int` ports `cmd_list` text mode:
    - the tags in Bash order;
    - `WORK_DIR` with the first occurrence of `$HOME` replaced by `~` (Bash `${WORK_DIR/$HOME/~}`);
    - the secret count as `wc -w` of `SECRET_KEYS`;
    - the `No profiles yet.` / `Run: ctx setup` pair.
  - `func StatusText(t tui.Term) int` ports `cmd_status` text mode, with these parts:
    - the header;
    - the profile box (rich) or the `hr` block (plain);
    - the activation note;
    - the five live checks, in order: gh, SSH, mise, AWS, git identity. Each check calls the same external commands as Bash (`gh auth status`, `ssh-add -l`, `aws configure list --profile`, `git config user.email`). `has` uses E3's `has()`.
- [ ] **Step 1: Write failing tests**
  - `TestListTextMatchesBash` covers no profiles, one active plus one inactive, all tags, a `WORK_DIR` under `$HOME`, and a `WORK_DIR` containing `$HOME` mid-path. It compares with `CTX_CORE=off bin/ctx --no-gum list`.
  - `TestListTextNoTTY`: output written to a pipe contains no `\033`.
  - `TestStatusTextMatchesBash` (plain) runs with fake `gh`, `ssh-add`, `mise`, `aws` and `git`, covering:
    - no active profile;
    - an active manual profile in its locked dir and elsewhere;
    - a gh mismatch;
    - a key not in the agent;
    - a missing key file;
    - `mise.toml` absent;
    - an invalid AWS profile;
    - a git email mismatch inside `WORK_DIR`;
    - a minimal PATH with none of the tools.
  - `TestStatusTextRichGolden`: Rich true with a fixed profile gives `testdata/status_rich.golden`.
- [ ] **Step 2: Run the tests.** Expected: FAIL.
- [ ] **Step 3: Implement and wire.**
- [ ] **Step 4: Run all checks from Global Constraints.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): text list and status in Go`.

### Task 5: Non-quiet `use`

**Files:** Modify `internal/cli/use.go`, `internal/cli/use_test.go` and `cmd/ctx-core/main.go` (`implements use` is always true). The `bin/ctx` comment on `use` forwarding is updated.

**Interfaces:**
- Consumes: `tui.Term` and `tui.Accent` from Task 2, and the E3 `useApply`.
- Produces: `func UseIn(t tui.Term, args []string) int`. With `CTX_QUIET` not set to 1, it emits Bash's non-quiet lines:
  - `→ Switching to: <bold name>`;
  - one success line per step, with exact text from `_ctx_use_apply`, in step order;
  - the closing blank line, `Active context: <name>` (rich: `Accent(78, …)`; plain: a success line with the name in bold), and another blank line;
  - the `These variables were not applied to this shell: …` warning on stderr, emitted only without `--eval` and when anything besides `CTX_ACTIVE_PROFILE` was exported.
  
  With `--eval`, all of these go to stderr, as in Bash's `_ctx_use_apply "$name" >&2`. The quiet path is unchanged.
- [ ] **Step 1: Write failing tests.** `TestUseNonQuietMatchesBash` (plain) runs with fakes and covers:
  - a gh switch that succeeds and one that fails;
  - an SSH key already in the agent, newly added, and failing to add;
  - AWS;
  - Azure with and without `az`;
  - GCP with and without `gcloud`;
  - kubectl that succeeds and one that fails;
  - secrets present and missing, plus an invalid key name;
  - with and without `--eval bash`, and `--eval fish`.
  
  It compares stdout, stderr, exit code, argv, the config file, and the eval output.
- [ ] **Step 2: Run the tests.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run all checks from Global Constraints.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): non-quiet ctx use in Go`.

### Task 6: `remove`, with exact include and host removal

**Files:** Create `internal/cli/remove.go` and its test. Modify `internal/cli/ssh.go` (add removal helpers), `lib/core.sh` (`gitconfig_remove_include` per R3), `cmd/ctx-core/main.go`, `bin/ctx` (forward `remove|rm|del`) and `test/test.sh`.

**Interfaces:**
- Consumes: `Prompter.AskYN` and `tui.Term` from Task 2, `secret.Open(EffectiveProvider())` from E3, and `ctxconfig.ClearActivation`.
- Produces:
  - `func sshRemoveHost(host string) error`: drops the line `Host <host>` and the four lines after it. It keeps the file at mode 0600 and does nothing if the file is missing.
  - `func gitconfigRemoveInclude(identityFile string) error`, per R3.
  - `func Remove(t tui.Term, args []string) int` ports `cmd_remove` with the same text, order and `die` messages. `remove`, `rm` and `del` all route to it.
- [ ] **Step 1: Write failing tests**
  - In test.sh, a gitconfig with includes for both `acme` and `acme-old`, plus an unrelated user section. After `printf 'y\n' | ctx --no-gum remove acme`, only `acme`'s header and path lines are gone, and `acme-old` and the user section are intact.
  - `TestRemoveMatchesBash` (plain) compares output, exit code and a snapshot of `HOME` (profile, secrets dir, `~/.ssh/ctx_config`, `~/.gitconfig`, `~/.config/git/ctx-<p>`, `~/.ctx/config`) with Bash. It covers answering `y`, removing the active profile, a missing profile and no args.
  - `TestRemoveDeclined`: answering `n` dies with `Aborted.` and exit 1, and the snapshot is unchanged. EOF exits 130 with the snapshot unchanged.
- [ ] **Step 2: Run the tests.** Expected: FAIL.
- [ ] **Step 3: Implement the Bash fix and the Go port, then wire.**
- [ ] **Step 4: Run all checks from Global Constraints.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): ctx remove in Go; remove only the profile's own include`.

### Task 7: Docs and dependency footprint

**Files:** `CHANGELOG.md`, `README.md` (which commands run in ctx-core; gum is now optional), `docs/sprint/SPRINT.md` (split E4 into E4a, E4b and E4c with the command lists from **Spec** above, and mark E4a done), and `AGENTS.md` (add `internal/tui` to Where things live).

- [ ] **Step 1: Record the binary size before and after**, using `go build -o /tmp/x ./cmd/ctx-core && ls -l`, on base ad46e50 and on HEAD. Put both numbers in the CHANGELOG line.
- [ ] **Step 2: Write the docs.**
  - The CHANGELOG Fixed section names the `secret set` bug and tells affected users to re-run `ctx secret set` for any secret set without gum.
  - The R3 fix gets its own CHANGELOG line.
- [ ] **Step 3: Run all checks from Global Constraints.** Expected: PASS.
- [ ] **Step 4: Commit** `docs: Go port phase E4a`.
