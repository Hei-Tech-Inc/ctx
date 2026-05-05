# Contributing

Thanks for contributing to `ctx`.

## Development setup

- Run `bash install.sh` for local dependencies.
- Run `bin/ctx help` to validate CLI wiring.
- Run `bash test/test.sh` before opening a pull request (CI runs the same script on Ubuntu).

## Pull requests

- Keep pull requests focused and small.
- Add or update tests when behavior changes.
- Ensure `shellcheck` passes on all shell scripts.

## Commit messages

Use clear, imperative messages describing user-facing value.

## Tests

`test/test.sh` covers pure helpers (env keys, timeouts, clone URL rewrite, version parsing, secret paths), plus a smoke check that `bin/ctx version` prints the version from `lib/core.sh`. Interactive flows (`ctx setup`, real `git clone`) are not automated yet — exercise those manually when changing onboarding or clone behavior.

Pushing a tag matching `v*` (for example `v3.1.1`) triggers the release workflow, which creates a GitHub Release with auto-generated notes.
