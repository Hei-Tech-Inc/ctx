# Go port phase 6 (drop Bash) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Go binary, installed as `ctx`, the only implementation of ctx; delete `bin/ctx` (Bash) and `lib/core.sh`, `lib/cmd_ops.sh`, `lib/cmd_import.sh`.

**Architecture:** `cmd/ctx-core` becomes `cmd/ctx` and takes over what the Bash entry point did (global flags, aliases, unknown-command error), while still answering as `ctx-core` for hooks already loaded in open shells. A root `VERSION` file stamps builds. install.sh installs the binary plus two thin hook shims and an install marker that uninstall trusts. Tests that compared Go against Bash are frozen into golden files before Bash is removed.

**Tech Stack:** Go (module `github.com/Hei-Tech-Inc/ctx`), Bash for install.sh/test.sh/shims, fish shim, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-10-26-go-drop-bash-design.md`

## Global Constraints

- No user-visible output change for any command (byte-for-byte as today), except `ctx version` coming from the build stamp.
- Global flags: `--dry-run`, `--quiet|-q`, `--no-gum`, `--json`, honored anywhere before `--`; exported as `CTX_DRY_RUN=1`, `CTX_QUIET=1`, `CTX_NO_GUM=1`, `CTX_JSON=1`. `CTX_QUIET` inherited from env unless `-q` sets it.
- Aliases: `ls`→list, `st`→status, `rm|del`→remove, `import|add`→setup, `version|-v|--version`→`ctx version X`, `help|-h|--help`, no args → help.
- Unknown command: stderr `✗ Unknown command: <cmd>` styled as today, blank line, `  Run 'ctx help' for usage.`, blank line; exit 1 (copy from `bin/ctx` main `*)` branch and its `error` helper).
- argv[0] base `ctx-core` keeps today's ctx-core behavior exactly (`version` → `ctx-core X`, `implements`).
- Version: root `VERSION` = `3.2.0`; builds use `-ldflags "-X main.version=$(cat VERSION)"`; unstamped = `dev`.
- Release assets: `ctx_<os>_<arch>` and, for one release, `ctx-core_<os>_<arch>` copies; `checksums.txt`; os ∈ {darwin, linux}, arch ∈ {amd64, arm64}.
- Install marker: `<lib>/.ctx-install`, one line: absolute physical path of `<bin>/ctx`.
- rc block written by `install-hook` unchanged; `lib/ctx_autoswitch.bash` and `lib/ctx_autoswitch.fish` keep their names.
- ShellCheck clean (`-x -S warning`); no `Co-authored-by` or any agent trailer in commits/PRs.
- Go tests run locally with `go test -p 2 -parallel 4 ./...` (unbounded runs saturate the dev Mac).

## Review Focus

1. Upgrade from a pre-E6 install (Bash `bin/ctx` + `lib/*.sh` + `lib/ctx-core`) with an open shell: after `install.sh`, the open shell's hook (bakes in `<lib>/ctx-core`) and a fresh shell both work → Task 4 test `upgrade_from_bash_layout`.
2. Global flag after the subcommand (`ctx use demo -q`, `ctx list --json`) and after `--` (`ctx run -- tool --json` must pass `--json` through) → Task 2 tests.
3. Shim sourced when no binary is found: shell startup must not fail, one stderr line → Task 5 test.
4. uninstall run via a symlink to the binary, or with a marker naming another path → refuse → Task 4 tests.
5. `upgrade --check` when remote `VERSION` has trailing newline/CRLF or is unreachable → Task 1 tests.

---

## PR E6a — `ctx` entry point in Go (branch `feat/go-drop-bash`, base `main`)

### Task 1: VERSION file and build stamping

**Files:**
- Create: `VERSION`
- Modify: `cmd/ctx-core/main.go` (ctxVersion), `internal/cli/upgrade.go` (check fetches `VERSION`), `.github/workflows/release.yml`, `.github/workflows/ci.yml`, `install.sh` (`_build_ctx_core`, `_staged_ctx_version`), `test/test.sh` (wherever it builds ctx-core)
- Test: `cmd/ctx-core/version_test.go`, `internal/cli/upgrade_test.go`

**Interfaces:**
- Produces: `main.version` is the only version source in Go; `ctxVersion()` returns it (no `CTX_VERSION` env read). `parseVersionFile(b []byte) string` in `internal/cli/upgrade.go`: trims `\r`, whitespace; returns "" if not `^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`.

- [ ] **Step 1: Failing tests**
  - `TestCtxVersionIgnoresEnv`: build with `-ldflags -X main.version=9.8.7`, run `ctx-version` with `CTX_VERSION=1.0.0` set → stdout `ctx version 9.8.7\n`.
  - `TestParseVersionFile`: `"3.2.0\n"`→`3.2.0`, `"3.2.0\r\n"`→`3.2.0`, `"<html>"`→`""`, `""`→`""`.
  - `TestUpgradeCheckReadsVersionFile`: point `CTX_UPGRADE_REPO` at a `file://` dir holding `VERSION` = `9.9.9\n`; `upgrade --check` reports 9.9.9 available with the same wording as today; with no `VERSION` file, prints today's cannot-check message and exits as today.
- [ ] **Step 2:** `go test ./cmd/ctx-core ./internal/cli -run 'CtxVersion|ParseVersionFile|UpgradeCheck'` → FAIL.
- [ ] **Step 3:** Implement. `VERSION` content `3.2.0` (must equal `CTX_VERSION` in `lib/core.sh` until E6c deletes it; add a test.sh check that they match). Every `go build` of ctx-core in CI, release.yml, install.sh, test.sh passes `-ldflags "-X main.version=$(cat VERSION)"` (install.sh: read the staged/checkout `VERSION`, falling back to `lib/core.sh` parse only for a remote repo that has no `VERSION`). release.yml: fail when `${GITHUB_REF_NAME#v}` != `$(cat VERSION)`.
- [ ] **Step 4:** Same command → PASS; `bash test/test.sh` passes.
- [ ] **Step 5:** Commit `feat(core): VERSION file is the single version source`.

### Task 2: `cmd/ctx` owns the entry point

**Files:**
- Move: `cmd/ctx-core/` → `cmd/ctx/` (`git mv`; update every `./cmd/ctx-core` build path in CI, release.yml, install.sh, test.sh, scripts, docs commands). Built artifact name stays `bin/ctx-core` in E6a.
- Create: `cmd/ctx/entry.go`
- Modify: `cmd/ctx/main.go` (`main` picks mode by argv[0])
- Test: `cmd/ctx/entry_test.go`

**Interfaces:**
- Produces: `func runCtx(args []string, stdout, stderr io.Writer) int` — user-facing `ctx`; `func run(args []string, stdout, stderr io.Writer) int` — unchanged ctx-core surface. `main()` calls `run` when `filepath.Base(os.Args[0]) == "ctx-core"`, else `runCtx`.
- `parseGlobal(args []string) (rest []string, env map[string]string)`: strips global flags before the first `--` (the `--` itself is kept in rest, as bin/ctx does); sets env keys listed in Global Constraints; caller `os.Setenv`s them before dispatch.

- [ ] **Step 1: Failing tests** (table-driven, call `runCtx` with a temp `CTX_DIR`/`HOME`, capture output; run the binary via `exec` only for argv[0] and signal cases):
  - `TestParseGlobal`: `[--json list]`→rest `[list]`, `CTX_JSON=1`; `[use demo -q]`→`[use demo]`, `CTX_QUIET=1`; `[run -- tool --json]`→`[run -- tool --json]`, no `CTX_JSON`; `[--no-gum --dry-run setup]`→`[setup]`, `CTX_NO_GUM=1 CTX_DRY_RUN=1`.
  - `TestAliases`: `ls`,`st`,`rm`,`del`,`import`,`add` produce the same output/exit as their targets on a fixture profile dir.
  - `TestVersionForms`: `version`,`-v`,`--version` → `ctx version <v>`.
  - `TestHelpForms`: no args, `help`, `-h`, `--help` → same as `run([help])`.
  - `TestUnknownCommand`: `frobnicate` → exit 1, stderr equals today's `CTX_CORE=off bin/ctx --no-gum frobnicate` output (capture it in the test while Bash exists and also pin it as a literal).
  - `TestListJSONGlobal`: `--json list` == `run([list --json])`; `--json st` == `run([status --json])`.
  - `TestArgv0CtxCore`: binary copied/symlinked as `ctx-core`: `version` → `ctx-core <v>`; `implements use` exit 0. Copied as `ctx`: `version` → `ctx version <v>`.
  - `TestInterruptExit130`: binary as `ctx` running `setup` under a pty, send SIGINT → exit 130 (reuse `testutil.RunPTY`).
- [ ] **Step 2:** `go test ./cmd/ctx -run 'ParseGlobal|Aliases|VersionForms|HelpForms|UnknownCommand|ListJSONGlobal|Argv0|Interrupt'` → FAIL.
- [ ] **Step 3:** Implement `runCtx`: parseGlobal → map aliases → `mcp|knowledge` direct → `version` forms → `ctx-version` → else `run(rest)` when `implements(cmd, rest)`, else unknown-command output. `--json` with list/status appends `--json`. Signal handling: INT → exit 130 unless the command installs its own handler (existing code that already handles INT keeps doing so).
- [ ] **Step 4:** PASS; full `go test -p 2 -parallel 4 ./...` and `bash test/test.sh` pass.
- [ ] **Step 5:** Commit `feat(core): ctx binary parses global flags and aliases itself`.

- [ ] **PR:** push `feat/go-drop-bash`, open PR "Go port phase 6a — ctx entry point in Go" against `main`; wait for green CI.

---

## PR E6b — install, upgrade, uninstall, release, shims (branch `feat/go-drop-bash-install`, base E6a)

### Task 3: Release assets and install.sh installs the Go binary

**Files:**
- Modify: `.github/workflows/release.yml`, `install.sh`, `test/test.sh` (install tests)
- Test: `test/test.sh` new cases

**Interfaces:**
- Produces: installed layout `<bin>/ctx` (Go binary), `<lib>/ctx_autoswitch.bash`, `<lib>/ctx_autoswitch.fish`, `<lib>/ctx-core` → symlink to `<bin>/ctx`, `<lib>/.ctx-install` (marker, see Global Constraints). No `<lib>/*.sh`.

- [ ] **Step 1: Failing test.sh cases** (install into a temp prefix via `CTX_INSTALL_LIB` + bin dir, with a local `CTX_RELEASE_BASE` file dir serving fake assets + checksums, as existing install tests do):
  - `test_install_layout_go`: after install, `<bin>/ctx version` prints `ctx version 3.2.0`; `<bin>/ctx` is not a shell script (`head -c2` != `#!`); `<lib>/ctx-core` is a symlink resolving to `<bin>/ctx`; marker content == physical path of `<bin>/ctx`; no `<lib>/core.sh`.
  - `test_install_downloads_ctx_asset`: without go on PATH, install fetches `ctx_<os>_<arch>` and verifies checksum; bad checksum → install fails and leaves the previous `<bin>/ctx` untouched.
  - `test_upgrade_from_bash_layout`: pre-seed prefix with an old layout (Bash `bin/ctx` with `CTX_LIB=` line, `lib/core.sh`, `cmd_ops.sh`, `cmd_import.sh`, `ctx-core` binary); run install.sh; assert new layout and old `.sh` files gone; `bash -ic` sourcing `<lib>/ctx_autoswitch.bash` then `ctx use demo` works.
- [ ] **Step 2:** `bash test/test.sh` → those FAIL.
- [ ] **Step 3:** Implement per spec D3. release.yml builds both `ctx_*` and `ctx-core_*` names from `./cmd/ctx`. install.sh: build `./cmd/ctx` from a checkout with go, else download; stage in a temp dir; validate with `<staged>/ctx version`; atomic `mv` into place; symlink + marker; remove old `.sh`; keep `CTX_REPO`, `CTX_RELEASE_BASE`, `CTX_INSTALL_LIB`, `~/.local` fallback, `ctx install-hook` run. Drop gum install only if `grep -rn gum internal cmd` shows no runtime use beyond `init`'s optional check (keep `init`'s check as is).
- [ ] **Step 4:** PASS; shellcheck clean.
- [ ] **Step 5:** Commit `feat(install): install the Go ctx binary`.

### Task 4: uninstall trusts the install marker

**Files:**
- Modify: `internal/cli/uninstall.go`
- Test: `internal/cli/uninstall_test.go`

**Interfaces:**
- Consumes: marker from Task 3.
- Produces: `func installedLayout(exe string) (bin, lib string, err error)`: `exe` physical path; candidates `<exe dir>/../lib/ctx`; returns lib only if `<lib>/.ctx-install` exists, is a regular file, and its trimmed content equals `exe` by physical identity (`os.SameFile`). Otherwise error explaining what to remove by hand.

- [ ] **Step 1: Failing tests**: valid marker → removes `<bin>/ctx`, the lib dir (shims, symlink, marker) and runs guard uninstall when installed; marker naming another file → refuse, nothing removed; no marker → refuse; exe reached through a symlink (`~/bin/ctx` → real) → resolves and succeeds; `--purge` path unchanged (existing tests keep passing). Remove tests for the `CTX_LIB=` line and `CTX_BIN_PATH`.
- [ ] **Step 2:** `go test ./internal/cli -run Uninstall` → FAIL.
- [ ] **Step 3:** Implement; drop reads of `CTX_LIB`, `CTX_BIN_PATH`, `installerLibLine`. Keep the existing refuse-on-doubt checks for every path deleted.
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** Commit `feat(core): uninstall finds the install by its marker`.

### Task 5: Hook files become thin shims

**Files:**
- Modify: `lib/ctx_autoswitch.bash`, `lib/ctx_autoswitch.fish`, `internal/cli/text/activate.{bash,zsh,fish}` only if a comment/path says ctx-core where it must say ctx, `scripts/bench-hook.sh`
- Test: `test/test.sh`, `internal/cli/activate_test.go`

**Interfaces:**
- Shim lookup order: `<shim dir>/ctx-core`, `<shim dir>/../../bin/ctx`, `<shim dir>/../bin/ctx-core` (source checkout), then `command -v ctx` / fish `command -s ctx`. Then `eval "$(<found> activate bash|zsh)"` / `<found> activate fish | source`. zsh detected via `$ZSH_VERSION` as today.

- [ ] **Step 1: Failing tests**:
  - `test_shim_no_binary`: `HOME` temp, PATH without ctx, source the shim in `bash -ic` and `zsh -ic` (skip if absent) → shell exits 0, stderr has exactly one line containing `reinstall ctx`, `type ctx` fails.
  - `test_shim_resource_one_hook`: source twice → one `_ctx_hook` registration (reuse the existing E5 "keeps one hook" assertions).
  - Existing autoswitch/integration tests (`test_autoswitch_*`, `test_hook_integration`, fish tests in CI) pass unchanged against the shim.
- [ ] **Step 2:** FAIL on the new tests.
- [ ] **Step 3:** Replace each file with the shim (bash file serves bash and zsh). Delete the legacy hook and the `ctx()` wrapper from the files (activate's glue defines the wrapper). bench-hook.sh: drop the legacy column.
- [ ] **Step 4:** PASS; shellcheck clean; CI fish job green (fish is CI-only).
- [ ] **Step 5:** Commit `feat(hook): autoswitch files are shims for ctx activate`.

- [ ] **PR:** push, open "Go port phase 6b — install the Go binary; hook shims" against E6a's branch; wait for green CI.

---

## PR E6c — delete Bash (branch `feat/go-drop-bash-delete`, base E6b)

### Task 6: Freeze the Bash oracle in Go tests

**Files:**
- Modify: the 39 `*_test.go` files that run `bin/ctx`, `CTX_CORE=off`, `bashOld()` or source `lib/*.sh` (list: `grep -rlE '"bin", "ctx"|lib/core.sh|CTX_CORE=off|"CTX_CORE"|bashOld\(' internal cmd`)
- Create: `testdata/golden/*` under each affected package; `internal/testutil/golden.go`

**Interfaces:**
- Produces: `func Golden(t *testing.T, name string, got []byte)` — compares to `testdata/golden/<name>`; with `-update` (package flag `testutil.Update`) writes it. Names are stable per case (`<Test>/<case>.out`, `.err`, `.files`).

Rule per test: if it compares Go against Bash output, record the expected side as a golden **generated from the Bash side at this commit** (run with `-update` while Bash still exists; the update path writes Bash's output, not Go's), then compare Go to the golden. If it only uses Bash as a fixture builder, replace with a Go/fixture equivalent. If it tests a Bash internal with no user-visible behavior, delete it and list it in the PR body.

- [ ] **Step 1:** Add `Golden` + `TestGoldenRoundTrip` in testutil; run → FAIL, implement, PASS.
- [ ] **Step 2:** Convert package by package (`internal/cli` last); after each package: `go test -p 2 -parallel 4 ./<pkg>` PASS with `CTX_CORE` unset **and** with `bin/ctx`/`lib/*.sh` temporarily moved aside (`git stash`-free: rename to `*.off` in a scratch copy of the tree) → still PASS. Commit per package: `test(<pkg>): freeze Bash output as golden files`.
- [ ] **Step 3:** Grep in the scratch copy shows no test referencing `bin/ctx`, `lib/*.sh`, `CTX_CORE`, `bashOld`.

### Task 7: Delete Bash and move test.sh to the binary

**Files:**
- Delete: `bin/ctx`, `lib/core.sh`, `lib/cmd_ops.sh`, `lib/cmd_import.sh`
- Modify: `test/test.sh`, `.github/workflows/ci.yml`, `.gitignore` (`bin/ctx`), `cmd/ctx/main.go` (drop `CTX_CORE` mention in usage/comments that became false), `internal/cli/doctor*.go`/`initcmd.go` if they check `lib/*.sh`, `internal/cli/upgrade.go` (no `lib/core.sh` fallback)

- [ ] **Step 1:** test.sh: build once at start `go build -ldflags "-X main.version=$(cat VERSION)" -o "$TEST_BIN/ctx" ./cmd/ctx`, export `CTX="$TEST_BIN/ctx"`, replace all `"$ROOT_DIR/bin/ctx"` with `"$CTX"`. Delete: tests that `source "$ROOT_DIR/lib/…"`, `CTX_CORE=off` duplicates, `test_ctx_core_dispatch`, `test_core_forward_fallback`, `test_ctx_core_is_exercised`, the VERSION/core.sh match check. For each deleted test, add a row (test, behavior, still covered by) to `docs/superpowers/plans/2026-10-26-go-drop-bash-deleted-tests.md`; any behavior without a remaining cover gets a new test.sh case first.
- [ ] **Step 2:** Delete the Bash files. CI: shellcheck glob `lib/*.bash install.sh test/*.sh scripts/*.sh`; drop the "Bash only, ctx-core disabled" job; build `bin/ctx` instead of `bin/ctx-core`.
- [ ] **Step 3:** `bash test/test.sh`, `go test -p 2 -parallel 4 ./...`, shellcheck → all PASS. `grep -rn 'CTX_CORE\b\|lib/core.sh\|cmd_ops.sh\|cmd_import.sh' --exclude-dir=.git . | grep -v 'CHANGELOG\|docs/superpowers\|_test.go\|// '` → only provenance comments remain.
- [ ] **Step 4:** Commit `feat!: drop the Bash implementation; ctx is the Go binary`.

### Task 8: Docs

**Files:** `README.md`, `AGENTS.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, `docs/sprint/SPRINT.md`, `docs/runbooks/common-issues.md` (if it mentions `CTX_CORE` or lib files)

- [ ] **Step 1:** Per spec D7. AGENTS.md: "Go CLI (`cmd/ctx`, `internal/…`) with shell hook shims in `lib/`", commands (`go test -p 2 -parallel 4 ./...`, `bash test/test.sh`, `shellcheck -x -S warning lib/*.bash install.sh test/*.sh scripts/*.sh`), Where-things-live table. CHANGELOG [Unreleased]: E6 entry, `CTX_CORE` no longer read, pre-E6 `upgrade --check` gap, release asset rename. SPRINT: E6 `[x]`, E5 hook notes that still apply → "Known limits".
- [ ] **Step 2:** `markdown` links resolve (`grep -o '](\([^)]*\))'` paths exist). Commit `docs: Go port phase 6`.

- [ ] **PR:** push, open "Go port phase 6c — drop Bash" against E6b's branch, PR body includes the deleted-tests table; wait for green CI.
