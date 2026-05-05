# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- Smoke test: `bin/ctx version` matches `CTX_VERSION` from `lib/core.sh`.
- GitHub Actions publishes a GitHub Release when a `v*` tag is pushed.

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
