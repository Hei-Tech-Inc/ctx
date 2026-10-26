# Go port phase 6 — drop the Bash implementation

Date: 2026-10-26. Status: approved by the maintainer's standing preference
("pick the recommended option and keep going"). Calls marked *(Claude's call)*
were not discussed; check them while reading.

## Intent

Since E4c every `ctx` command runs in `ctx-core`; `bin/ctx` (637 lines) and
`lib/core.sh`, `lib/cmd_ops.sh`, `lib/cmd_import.sh` (≈5300 lines) survive only
as a forwarder and as the `CTX_CORE=off` fallback. Two implementations means
every fix lands twice (E4c–E5 fixed most bugs "in Bash and Go"). E6 makes Go
the only implementation: the user runs one binary named `ctx`.

## Goals

1. The installed `ctx` is the Go binary. No Bash on the command path.
2. Existing installs keep working across `ctx upgrade` with no manual step:
   rc blocks written by `install-hook` (they source `lib/ctx_autoswitch.bash`
   / `.fish`) stay valid, shells already running keep their hook until restart,
   and git hooks written by `guard install` (they call `ctx guard check`) work.
3. The conformance suite (`test/test.sh`) runs against the Go binary and
   stays green; Go tests stay green; CI stays green on macOS-free Linux runners
   with bash, zsh and fish.
4. Delivered as small stacked PRs, each green, pushed as it is done.

## Non-goals

- No behavior or output changes to any command beyond what removing Bash forces
  (byte-for-byte output is the contract the Go port kept; keep it).
- No rewording of the many Go comments that cite `lib/core.sh` — they record
  provenance. Only fix comments that become false (e.g. "bin/ctx passes …").
- No new features (dashboard, website, logo stay in the backlog).
- Not removing the `gum` dependency from install.sh beyond what is now unused.

## Anti-goals (failure even if it "works")

- An upgrade that leaves a user without a working `ctx`, without the prompt
  hook, or with an rc file they must edit by hand.
- Losing a tested behavior: a `test.sh` case may be deleted only if it tests
  Bash internals (sources `lib/*.sh`, or is a `CTX_CORE=off` duplicate) — every
  user-visible behavior it covered must stay covered by a Go or `test.sh` test.
- Agent attribution trailers in commits/PRs (repo hooks reject them).

## Decisions

### D1. One binary, named `ctx` *(Claude's call)*
- `cmd/ctx-core` moves to `cmd/ctx` (package main). The installed file is
  `<bin>/ctx`.
- The binary also answers to argv[0] `ctx-core` with today's ctx-core
  semantics (`version` → `ctx-core X`, `implements`, direct subcommands), so
  hooks already loaded in open shells (activate bakes the ctx-core path in)
  keep working. install.sh leaves `<lib>/ctx-core` as a symlink to `<bin>/ctx`.
- As `ctx`, the binary owns what `bin/ctx` did before dispatch:
  - global flags anywhere before `--`: `--dry-run`, `--quiet|-q`, `--no-gum`,
    `--json`; exported as `CTX_DRY_RUN`, `CTX_QUIET`, `CTX_NO_GUM=1`,
    `CTX_JSON` exactly as bin/ctx does today (internal code reads them; keep).
    `CTX_QUIET` is inherited from the environment unless `-q` sets it.
  - aliases: `ls`→list, `st`→status, `rm|del`→remove, `import|add`→setup,
    `version|-v|--version`→ctx-version, `help|-h|--help`, no args → help.
  - `list`/`status` with `--json` global → `list --json`.
  - unknown command: same text and exit 1 as bin/ctx today.
  - `mcp`, `knowledge`: run directly.
  - INT: exit 130, as bin/ctx's trap does.
- `implements` and the `implemented` table stay only as the ctx-core
  compatibility surface (an old bin/ctx could still call it); nothing new uses it.

### D2. Version source: a `VERSION` file *(Claude's call)*
- Root `VERSION` holds the version (`3.2.0` today; the maintainer bumps it at
  release — E6 is a good point for 4.0.0 but that is their call).
- Builds pass it via `-ldflags "-X main.version=$(cat VERSION)"` (install.sh,
  release.yml, CI). An unstamped `go build` prints `dev`.
- `ctx version` prints `ctx version <version>` from the build, not
  `CTX_VERSION` env. `CTX_VERSION` env is no longer read.
- `upgrade --check` fetches `<repo>/VERSION` instead of `lib/core.sh`.
  Known gap: clients older than E6 fetch `lib/core.sh` for `--check` and will
  report they cannot check; the real `ctx upgrade` still works for them because
  it runs the remote install.sh. Note it in CHANGELOG.
- release.yml fails if the tag (minus `v`) differs from `VERSION`.

### D3. Release assets and install.sh
- release.yml publishes `ctx_<os>_<arch>` + `checksums.txt` (darwin/linux ×
  amd64/arm64). *(Claude's call: also keep publishing `ctx-core_<os>_<arch>`
  copies for one release so a pre-E6 install.sh pinned to that tag still works.)*
- install.sh:
  - from a checkout with `go`: build `cmd/ctx`; otherwise download
    `ctx_<os>_<arch>` and verify the checksum (same rules as today).
  - installs `<bin>/ctx` (binary, atomic rename), `<lib>/ctx_autoswitch.bash`,
    `<lib>/ctx_autoswitch.fish`, `<lib>/ctx-core` → symlink to `<bin>/ctx`, and
    an install marker (D5).
  - removes the old `<lib>/core.sh`, `cmd_ops.sh`, `cmd_import.sh` if present.
  - validates with `<bin>/ctx version` instead of `bash -n`.
  - keeps `CTX_REPO`, `CTX_RELEASE_BASE`, `CTX_INSTALL_LIB`, the fallback to
    `~/.local`, and running `ctx install-hook`.
  - no install path may leave no `ctx`: stage everything, then swap.
  - drop gum installation if nothing in Go uses gum (verify; Go uses huh).

### D4. Shell hook files become thin shims
- `lib/ctx_autoswitch.bash` and `.fish` keep their names (rc blocks source
  them) and shrink to: find the `ctx` binary (`<this dir>/../../bin/ctx`,
  `<this dir>/ctx-core`, then PATH), `eval "$(ctx activate <shell>)"`.
- The `ctx()` wrapper function that evals `use`/`deactivate`: if `ctx activate`
  already emits it, the shim doesn't; otherwise move it into the activate
  output so `eval "$(ctx activate zsh)"` users get it too. One definition.
- The legacy prompt hook (≈800 lines) is deleted, with its quirks (first
  element of array `PROMPT_COMMAND`, fish glob matching).
- If no binary is found: print one line to stderr naming the fix
  (`reinstall ctx`) and define nothing. Never break the shell startup.
- Re-sourcing must still leave exactly one hook (activate already handles it).
- `install-hook` keeps writing the same rc block; `doctor`/`init` keep
  accepting both rc forms.

### D5. uninstall without the CTX_LIB line *(Claude's call)*
- Today uninstall finds the lib dir from the `CTX_LIB="…"` line install.sh
  writes into bin/ctx. Replace it with an install marker `<lib>/.ctx-install`
  that install.sh writes, containing the absolute path of `<bin>/ctx`.
- uninstall: resolve the running executable (`os.Executable`, physical path);
  the lib dir is `<exe>/../../lib/ctx` or `CTX_INSTALL_LIB`-equivalent
  candidates; it must hold a marker naming this exe. Otherwise refuse and print
  what to remove by hand — the same refuse-on-doubt rules as today (physical
  identity, symlinks, `..`, case-insensitive disks). Removes bin, lib dir
  (shims, symlink, marker), and runs `guard uninstall` when installed;
  `--purge` unchanged.
- An install upgraded from pre-E6 gets the marker from the new install.sh, so
  no legacy path is needed. `CTX_BIN_PATH`/`CTX_LIB` env are no longer read.

### D6. Tests
- `test/test.sh` builds `go build -o "$TMP/bin/ctx" ./cmd/ctx` once at start
  and every `"$ROOT_DIR/bin/ctx"` becomes `"$CTX"`. Tests needing an installed
  layout use install.sh into a temp prefix (as today).
- Delete: tests that source `lib/*.sh`, `CTX_CORE=off` duplicates, Bash
  forwarder tests (`test_ctx_core_dispatch`, `test_core_forward_fallback`).
  Before deleting any, list it with the behavior it covered and the test that
  still covers it (put the list in the PR description).
- Go tests that exec `bin/ctx` or set `CTX_CORE` move to the built binary.
- New Go tests for D1 (flags, aliases, unknown command, argv[0] ctx-core),
  D2 (version, upgrade --check against a local VERSION), D5 (marker rules).
- `scripts/bench-hook.sh`: drop the legacy-hook column or keep it only if it
  still runs; it must not break.
- CI: shellcheck glob becomes `lib/*.bash install.sh test/*.sh scripts/*.sh`
  (whatever Bash remains); Go tests and test.sh as today.

### D7. Docs
README (install, how it works), AGENTS.md ("This repository is a Bash CLI" →
Go CLI + shell shims; commands table; "Where things live"), CONTRIBUTING (test
commands), CHANGELOG [Unreleased] (E6 + the upgrade --check gap +
`CTX_CORE` no longer read), SPRINT.md (E6 done; move the E5 hook notes that
still apply to a "Known limits" list).

## Delivery (stacked PRs, push each when green)

1. **E6a — `ctx` entry point in Go.** D1 + D2 (minus install/release), `cmd/ctx`,
   `bin/ctx` reduced to a ~10-line `exec` of the built binary next to it or on
   PATH for source checkouts *(temporary; deleted in E6c)*. test.sh switched to
   the built binary. All Bash fallbacks unreachable but files still present.
2. **E6b — install, upgrade, uninstall, release, shims.** D3, D4, D5.
3. **E6c — delete Bash.** Remove `bin/ctx`, `lib/core.sh`, `lib/cmd_ops.sh`,
   `lib/cmd_import.sh`, dead tests, `CTX_CORE` handling; CI glob; D7 docs.

## Left to the builder

Exact file splits in `cmd/ctx`, how test.sh caches the build, whether the
argv[0] check uses `filepath.Base`, shim lookup order details, and anything a
sensible builder could settle without changing what users see.
