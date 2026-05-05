# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
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
