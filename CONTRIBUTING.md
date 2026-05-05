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

## Git and automation tooling

- **Do not commit** local IDE metadata folders listed in `.gitignore`. Rely on repo scripts and CI, not editor-specific metadata.
- **Contributors:** GitHub counts `Co-authored-by:` lines. Disable automated “co-author” / agent attribution in your editor so vendors do not appear as contributors.
- **Hooks (required if you use AI-assisted commits):** run `./scripts/install-git-hooks.sh` — installs `prepare-commit-msg`, `commit-msg`, and **`post-commit`** (amends off any trailer that still slips through). See `scripts/git-hooks/README.md`.
- If your editor still injects automation trailers after `git commit`, use plain **`git`** from a terminal, turn off that editor’s Git integration for this repo, or ask a maintainer to record commits with `git commit-tree` so hooks cannot append trailers.

### GitHub still lists an IDE vendor under “Contributors”?

The sidebar can lag **hours or days** after a history rewrite or force-push. This repo’s **`git-history-hygiene`** CI job proves current `main` has **no** vendor emails and **no** `Co-authored-by:` lines mentioning that IDE. To double-check locally:

```bash
git fetch origin
git shortlog -sne origin/main
git log origin/main --format='%B' | grep -iE '^co-authored-by:.*cursor' || echo 'no vendor co-author trailers'
```

If CI is green but the UI still shows the bot, wait for GitHub to refresh the graph or contact [GitHub Support](https://support.github.com/) — there is no per-user “remove contributor” button.

## Tests

`test/test.sh` covers pure helpers (env keys, timeouts, clone URL rewrite, version parsing, secret paths), plus a smoke check that `bin/ctx version` prints the version from `lib/core.sh`. Interactive flows (`ctx setup`, real `git clone`) are not automated yet — exercise those manually when changing onboarding or clone behavior.

Pushing a tag matching `v*` (for example `v3.1.1`) triggers the release workflow, which creates a GitHub Release with auto-generated notes.
