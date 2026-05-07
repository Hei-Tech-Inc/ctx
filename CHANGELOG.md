# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Fixed
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
