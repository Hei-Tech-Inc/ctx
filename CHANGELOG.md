# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- **`ctx --json` / `ctx list --json` / `ctx status --json`:** machine-readable output with a **`version`** field and stable keys for scripting and CI; completions advertise `--json` where relevant.
- **`ctx_resolve_path_profile`** in `lib/core.sh` (longest `WORK_DIR` prefix + repo **`.ctx`** `profile=` override), covered by unit tests.
- **Golden fixture** for minimal **`generate_mise_toml`** output under `test/fixtures/mise_generated_minimal.toml`.
- **Bash auto-switch hook:** idempotent **`PROMPT_COMMAND`** wiring — strips duplicate **`_ctx_profile_autoswitch`** and legacy **`_ctx_auto_switch`** tokens before prepending once.
- **Setup reference in the CLI:** `ctx setup --help`, `ctx import --help`, and `ctx help setup` show the same reference (flags, `--config` keys, dry-run, example file URL). Completions advertise `help setup` and setup `-h` / `--help`.
- **Example config:** header comment in `examples/setup.noninteractive.conf.example` points to `ctx help setup`.
- **ctx doctor:** warns if `~/.ssh` is group- or world-writable and suggests `chmod 700` (OpenSSH may otherwise ignore keys).
- **`ctx deactivate`:** clears `active=` / manual-lock fields in `~/.ctx/config` and prints eval-able unsets for secrets and profile env (`--eval bash|fish` for the hook).
- **Directory-scoped auto-switch:** shell hook (`lib/ctx_autoswitch.bash` / `lib/ctx_autoswitch.fish`) picks the profile with the **longest `WORK_DIR` prefix** of `$PWD`, applies **`.ctx` / `profile=`** repo overrides, prints **`[ctx] ←/→`** transitions, runs **`eval "$(ctx deactivate --eval …)"`** before switching, and sets **`CTX_AUTO_SWITCH=1`** on hook-driven `ctx use`. **Manual `ctx use`** sets a **manual lock** until you `cd` away from that directory.
- **`ctx clone`:** prints **`[ctx] cloning as <profile> (email)`**; with **`CTX_QUIET=1`** (auto-switch), HTTPS GitHub URLs default to SSH rewrite without prompting.
- **`ctx status`:** shows **activation** mode (manual lock vs directory auto) and **`CTX_ACTIVE_PROFILE`** when set by the hook.

### Changed
- **Installer** “next steps” after `install.sh` lists `ctx doctor`, `ctx help setup`, and `ctx verify` so the flow matches the docs and post-setup help text.
- **README:** added **Enterprise secrets (Vault / 1Password)** — built-in backends vs external vaults, how `mise` hooks inject values, and practical patterns (`op run`, Vault Agent, `pass`).

### Fixed
- **Auto-switch (sibling clients):** when one profile’s **`WORK_DIR`** is a **parent** of several client dirs (e.g. `…/clients`), ctx now prefers **`…/clients/<segment>.conf`** named after the first path segment under that prefix when that profile exists and **`…/clients/<segment>`** is a directory (even if that `.conf` omits **`WORK_DIR`**).
- **Installer safety check:** `install.sh` now validates the installed `ctx` script with `bash -n` and aborts fast if syntax is invalid (prevents silent broken upgrades).
- **Atomic installer writes:** `install.sh` now stages `ctx` files in a temp directory, validates syntax, then swaps into place to avoid partially-written binaries during upgrade.
- **Doctor checks:** fixed SSH include detection, and updated command version probes so `kubectl` reports correctly instead of showing a false unknown-flag error.
- **Prompt UX + cancel behavior:** interactive gum prompts now explicitly indicate when input is expected, and `Ctrl+C` cleanly aborts setup instead of continuing to the next question.
- **Setup loading feedback:** `ctx setup` now announces credential/profile discovery up front and bounds `gh auth status` detection with a timeout to avoid appearing hung.
- **Ctrl+C reliability:** non-gum prompt paths now treat interrupted reads as cancellation (`exit 130`) so setup cannot continue after user aborts.
- **Ctrl+C propagation fix:** cancellation inside command-substitution prompts now signals the parent `ctx` process, preventing `"Cancelled by user"` text from being captured as field input.

## [3.2.0] - 2026-05-07

### Fixed
- **Secrets backend adapters:** added provider-aware storage with `ctx config secret-provider <auto|keychain|file|pass>` and a first community-backed adapter for `pass` (`ctx/<profile>/<KEY>`), while preserving existing defaults.
- **Provider operations:** added `ctx secret migrate` to copy profile secrets between backends and extended `ctx doctor` to validate provider availability (`keychain`/`file`/`pass`).
- **Laptop migration:** added `ctx config export/import` for safe portable config transfer (profiles/git identities/SSH config), explicitly excluding secrets.
- **Chezmoi docs:** added `docs/chezmoi.md` for reuse-first cross-machine config management without syncing secrets.
- **`ctx setup`:** onboarding now provides three location modes (current directory default, existing path, or new client folder), supports manual-vs-imported reference selection, and defaults secrets to explicit opt-in with clearer local-compromise warnings.
- **Docs:** added a Windows section clarifying WSL2 install/usage (`ctx` runs inside Linux shell, not native PowerShell/CMD).
- **`ctx upgrade`:** runs an **in-place** install (`CTX_UPGRADE_ONLY`) — updates only the `ctx` binary and `lib/*.sh`; skips Homebrew/mise/gum/gh steps, shell-rc snippets, and the auto-switch hook installer so existing profiles and client directories are not disturbed by noise or side effects.
- **`ctx clone`:** stopped passing the repository URL twice to `git clone` (second argument was treated as the target directory, producing folders named like `git@github.com:…`).
- **`ctx list`:** dim lines use `echo -e` so ANSI styling renders instead of literal `\033[…`.
- **Installer:** patch `CTX_LIB` only on the bootstrap line of `bin/ctx` (not a broad `CTX_LIB=.*` replace) so the installed script cannot be corrupted during upgrade.

### Changed
- Git hooks: stronger vendor co-author stripping; **`post-commit`** amends `HEAD` if a trailer still appears; `install-git-hooks.sh` installs all hook scripts.

### Added
- **`packaging/homebrew-tap/`** — Homebrew formula (`Formula/ctx.rb`) + README; tap lives at **`isaackumi/homebrew-tap`** (`brew tap isaackumi/tap`).
- CI job **git-history-hygiene** rejects vendor-bot emails or `Co-authored-by:` lines mentioning the IDE on `main`.
- Smoke test: `bin/ctx version` matches `CTX_VERSION` from `lib/core.sh`.
- GitHub Actions publishes a GitHub Release when a `v*` tag is pushed.
- `commit-msg` hook rejects IDE vendor substrings in messages (with gum/path allowlists); `prepare-commit-msg` strips problematic `Co-authored-by:` lines; `./scripts/install-git-hooks.sh`; README badges for CI, release, license.

### Changed
- Rewrote git messages that referenced IDE tooling or gum pointer flags so `git log` no longer surfaces banned substrings; CONTRIBUTING updated for hooks + hygiene.

## [3.1.1] - 2026-05-05

### Added
- CI runs `test/test.sh` on Ubuntu alongside ShellCheck.
- `SECURITY.md`; fish completions (`ctx completion fish`); `packaging/README.md`.

### Changed
- README: security/threat-model summary, pinned release install URL, doctor vs verify guidance; CONTRIBUTING/PR template aligned with CI.

## [3.1.0] - 2026-05-05

### Added
- `ctx clone`, `ctx verify`, `ctx uninstall`, and `ctx upgrade --check`.
- Documented multi-account clone flows and secret storage behavior across macOS vs Linux.
