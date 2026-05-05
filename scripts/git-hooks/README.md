# Git hooks (optional)

Enable once per clone:

```bash
./scripts/install-git-hooks.sh
```

This sets `core.hooksPath` to `scripts/git-hooks` and marks hooks executable.

## Hooks

| Hook | Role |
|------|------|
| `prepare-commit-msg` | Drops `Co-authored-by:` lines that reference automation/agent vendors (pattern matches vendor emails). |
| `commit-msg` | **Blocks** the substring linked to a commercial IDE **unless** allowlisted (gum `--…` flags and the `.…/` metadata folder path). |

Together they prevent editor automation from appearing as a Git **contributor**.

Disable:

```bash
git config --unset core.hooksPath
```

GitHub contributor graphs use commit authors plus **`Co-authored-by:`** trailers — keep hooks enabled when using AI-assisted commits.
