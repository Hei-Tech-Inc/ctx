# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- Smoke test: `bin/ctx version` matches `CTX_VERSION` from `lib/core.sh`.
- GitHub Actions publishes a GitHub Release when a `v*` tag is pushed.
- Optional `scripts/git-hooks/prepare-commit-msg` to drop Cursor co-author trailers; `.cursor/` gitignored; CONTRIBUTING policy for contributor hygiene.

### Changed
- Rewrote git history on `main` to remove `Co-authored-by: Cursor` trailers (no Cursor as contributor from past commits).

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
