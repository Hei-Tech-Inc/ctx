# Agent / AI contributor guide

This repository is a **Bash** CLI (`bin/ctx` + `lib/*.sh`). Treat it like production shell: **ShellCheck-clean**, **small diffs**, and **tests for behavior changes**.

## Before you change code

1. Read **[docs/sprint/SPRINT.md](docs/sprint/SPRINT.md)** — pick the next unchecked item or ask the maintainer which phase to execute.
2. Read **[CONTRIBUTING.md](CONTRIBUTING.md)** — hooks, no vendor `Co-authored-by`, PR size.
3. Run **`bash test/test.sh`** and **`shellcheck`** on touched scripts (same glob as [`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

## Commands (copy-paste)

```bash
bash test/test.sh
shellcheck -x -S warning bin/ctx lib/*.sh lib/*.bash install.sh test/*.sh
./bin/ctx help
```

## Git

- **Do not** add `Co-authored-by: Cursor …` (or other vendor agent trailers). They pollute GitHub’s contributor graph; this repo uses **local hooks** to strip/reject them — see CONTRIBUTING.
- Prefer **`git commit`** from a terminal with hooks installed (`./scripts/install-git-hooks.sh`).

## JSON output

When adding scripting fields, extend **`ctx --json`** handlers in `lib/cmd_ops.sh` and bump the documented **`version`** integer in the JSON envelope if the shape changes.

## Where things live

| Area | Files |
|------|--------|
| Core helpers, mise/SSH/gitconfig | `lib/core.sh` |
| Commands | `lib/cmd_ops.sh`, `lib/cmd_import.sh` |
| Shell autoswitch hook | `lib/ctx_autoswitch.bash`, `lib/ctx_autoswitch.fish` |
| CLI entry | `bin/ctx` |
| Installer | `install.sh` |
