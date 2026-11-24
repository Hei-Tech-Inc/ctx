# Agent / AI contributor guide

This repository is a **Bash** CLI (`bin/ctx` + `lib/*.sh`) plus a small Go companion binary (`ctx-core`, behind `ctx mcp`). Treat it like production shell: **ShellCheck-clean**, **small diffs**, and **tests for behavior changes**.

## Before you change code

1. Read **[docs/sprint/SPRINT.md](docs/sprint/SPRINT.md)** — pick the next unchecked item or ask the maintainer which phase to execute.
2. Read **[CONTRIBUTING.md](CONTRIBUTING.md)** — hooks, no vendor `Co-authored-by`, PR size.
3. Run **`bash test/test.sh`** and **`shellcheck`** on touched scripts (same glob as [`.github/workflows/ci.yml`](.github/workflows/ci.yml)).

## Commands (copy-paste)

```bash
go vet ./... && go test ./...
bash test/test.sh
CTX_CORE=off bash test/test.sh   # Bash-only path (no Go forwarding)
go build -ldflags "-X main.version=$(cat VERSION)" -o bin/ctx-core ./cmd/ctx   # test.sh fails without it; CTX_CORE_OPTIONAL=1 bash test/test.sh skips that check for local runs without Go (never set in CI)
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
| Shell autoswitch hook (shims: run `ctx activate` when `ctx-core` is found, else the legacy hook) | `lib/ctx_autoswitch.bash`, `lib/ctx_autoswitch.fish` |
| Autoswitch decisions for `ctx hook-env` (enter/leave/switch, manual lock, prompt scope, state); the `activate` glue and `hook-env` command | `internal/hookenv/`; `internal/cli/text/activate.*`, `internal/cli/` |
| CLI entry | `bin/ctx` |
| Installer (also builds/downloads `ctx-core`) | `install.sh` |
| `ctx-core` entry, `mcp install` | `cmd/ctx/` |
| Terminal UI (rich/plain prompts, lipgloss/huh) | `internal/tui/` |
| MCP server and tools | `internal/mcpserver/` |
| Knowledge store, kubectl/login runner, ops | `internal/knowledge/`, `internal/kube/`, `internal/ops/` |
| Profiles, folder resolution, secrets | `internal/profile/`, `internal/resolve/`, `internal/secret/` |
| Setup wizard, `--config`, `--extend` (`setup`, `import`, `add`) | `internal/setup/` |
| SSH keys, hosts, gitconfig includes for setup | `internal/gitssh/` |
| Shell quoting (bash `printf %q`, mise.toml escaping) | `internal/shquote/` |
| Active GitHub account detection (`gh auth status` parsing; used by `setup`, `verify`) | `internal/ghacct/` |
| Atomic file replace that writes through symlinks and keeps mode | `internal/atomicfile/` |
| Config, git guard, shared command code (`use`, `deactivate`, `secret`, `config`, `guard`) | `internal/ctxconfig/`, `internal/guard/`, `internal/cli/` |
| Other commands (`agent-rules`, `doctor`, `verify`, `clone`, `init`, `install-hook`, `upgrade`, `uninstall`, `undo`, `edit`, `help`, `completion`); embedded help/completion text | `internal/cli/`, `internal/cli/text/` |

Commands implemented in Go are listed in the `implemented` table in `cmd/ctx/main.go`; `bin/ctx` forwards to them. A behavior change to one of them must be made in both the Bash and Go implementations (`CTX_CORE=off` runs Bash).

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- When graphify-out/graph.json exists, run `graphify query "<question>"` before your first grep, search or source-file read for a codebase question, and put this rule in every subagent prompt that explores code. Read raw files only for lines the graph or an earlier read already located. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. When the answer is under a Markdown heading, `graphify section "<heading>"` prints that section's text, so the .md need not be opened. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
- A committed graphify-out/graph.json is rewritten whole on every build, so a large diff to it in a pull request is expected. Do not flag it as churn or suggest skipping the graph rebuild or its hooks.
