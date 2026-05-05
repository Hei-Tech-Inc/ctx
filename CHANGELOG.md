# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- Open-source project hygiene files, templates, and CI linting.
- `ctx clone` helper for GitHub SSH host rewriting; `ctx verify`, `ctx uninstall`, and `ctx upgrade --check`.

### Changed
- Hardened profile generation and git identity writes against injection.
- Added Linux-aware keychain behavior and cloud CLI timeouts.
- Improved shell hook support and doctor install hints.

## [3.1.0] - 2026-05-05

### Added
- `ctx clone`, `ctx verify`, `ctx uninstall`, and `ctx upgrade --check`.
- Documented multi-account clone flows and secret storage behavior across macOS vs Linux.
