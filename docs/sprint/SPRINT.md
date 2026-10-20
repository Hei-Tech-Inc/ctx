# Engineering sprint backlog

Ordered by **dependency and risk** (tests and contracts before large features).  
Update this file when an item ships (check the box, link PR or commit).

## Legend

- **[ ]** not started · **[~]** in progress · **[x]** done  
- **S** = small (≤1 day) · **M** = medium · **L** = large / multi-sprint

---

## Phase A — Quality & contracts (industry baseline)

| ID | Item | Size | Status | Acceptance |
|----|------|------|--------|------------|
| A1 | Unit tests for **directory → profile** resolution (`ctx_resolve_path_profile`) | S | [x] | `test/test.sh` covers nested `WORK_DIR`, `.ctx` override, no match |
| A2 | **Golden / fixture** test for `generate_mise_toml` minimal output | S | [x] | Deterministic `mise.toml` matches `test/fixtures/mise_generated_minimal.toml` |
| A3 | Integration tests for **`ctx setup`** / real **`git clone`** | M | [ ] | CI job with temp `HOME`, scripted non-interactive setup, clone smoke |
| A4 | Snapshot or contract tests for **`~/.ctx/config`** writes and SSH include snippets | M | [ ] | Fixtures + diff on representative mutations |

## Phase B — CLI ergonomics & automation

| ID | Item | Size | Status | Acceptance |
|----|------|------|--------|------------|
| B1 | **`ctx --json list`** and **`ctx --json status`** (machine-readable) | S | [x] | Documented schema version field; stable keys; works with `-q` |
| B2 | Extend **`--json`** to **`ctx list`**-style data from **`verify`** / **`doctor`** (optional) | M | [ ] | Opt-in; no breaking change to default TTY output |
| B3 | **Hook hardening**: idempotent bash `PROMPT_COMMAND` (no duplicate autoswitch prefix) | S | [x] | Strip `_ctx_profile_autoswitch` / legacy `_ctx_auto_switch` before prepend |
| B4 | **`ctx guard`**: git hooks that block wrong-identity commits and wrong-alias pushes | S | [x] | `test_guard_git_hooks`: email mismatch blocked, chaining to `.git/hooks`, bypass, uninstall, refuses foreign `core.hooksPath` |
| B5 | **Per-terminal `GH_TOKEN`** in the autoswitch hook instead of global `gh auth switch` | S | [x] | Hook test: token per profile, cleared outside, user `GH_TOKEN` untouched |
| B6 | **`ctx run` / `ctx env` / `ctx agent-rules`** for AI agents and scripts | M | [x] | `test_ctx_run_env_agent_rules` |
| B7 | **Per-profile commit signing** (SSH / GPG) | S | [x] | `test_setup_commit_signing`: real signed commit in `WORK_DIR` |
| B8 | **`ctx mcp` ops agent** (Go `ctx-core`, MCP server, knowledge store, confirmed change tools) | L | [x] | `go test ./...`; `test_install_builds_ctx_core`, `test_doctor_reports_ctx_core`; manual acceptance on a real cluster |

## Phase C — Documentation & contributor experience

| ID | Item | Size | Status | Acceptance |
|----|------|------|--------|------------|
| C1 | **`AGENTS.md`** for AI / agent contributors | S | [x] | Points here + CONTRIBUTING + test/shellcheck commands |
| C2 | **Runbooks** (`docs/runbooks/`) | S | [x] | Common failures: SSH include, wrong profile, post–force-push clone |
| C3 | **Positioning** (`docs/positioning.md`): ctx vs direnv-only vs raw mise | S | [x] | Clear “when to use” table |
| C4 | README table: **Homebrew formula vs `install.sh`** (what each installs) | S | [ ] | Single obvious path for new users |

## Phase D — Platform & enterprise (explicit deferrals)

| ID | Item | Size | Status | Notes |
|----|------|------|--------|-------|
| D1 | Native **Windows** shell (non-WSL) | L | [ ] | Out of scope today; WSL2 documented |
| D2 | Built-in **Vault / 1Password** secret providers | L | [ ] | Requires auth model + UX; see README enterprise section |
| D3 | **`ctx` JSON** across all subcommands | M | [ ] | After B1 proves pattern |

## Phase E — Go port

| ID | Item | Size | Status | Notes |
|----|------|------|--------|-------|
| E1 | `ctx mcp` in Go (`ctx-core`) | L | [x] | Phase 1 |
| E2 | Read-only/hot-path commands in Go: `list --json`, `status --json`, `workdir-prompt`, `env`, `run`, `guard check` | M | [x] | Forwarding via `ctx-core implements`; `CTX_CORE=off` forces Bash |
| E3 | `use` (quiet), `deactivate`, `secret get/list/delete`, `config`, `guard install/uninstall/status` | M | [x] | Autoswitch path ~152 ms to ~96 ms (median of 20, 10 profiles, fake tools) |
| E4a | Text `list`/`status`, non-quiet `use`, `secret set`/`migrate`, `remove`/`rm`/`del`, with Charm (`internal/tui`) | M | [x] | Rich UI without gum; `ctx-core` 13.27 MB to 15.75 MB |
| E4b | `setup` wizard with its `import`/`add` aliases, plus `Ask`/`PickOne`/`Spin` | L | [x] | `internal/setup`, `internal/gitssh`, `internal/shquote`; `--extend` fixed |
| E4c | Remaining Bash-only commands: `agent-rules`, `doctor`, `verify`, `clone`, `init`, `install-hook`, `upgrade`, `uninstall`, `undo`, `edit`, `completion`, `help`, `version` | L | [x] | `internal/ghacct`, `internal/atomicfile`; fixes listed in CHANGELOG; Bash stays as fallback |
| E5 | Shell hook via `ctx hook-env` | M | [ ] | Move the autoswitch logic (`lib/ctx_autoswitch.bash`/`.fish`) into `ctx-core`; the rc stub calls it |
| E6 | Drop Bash | L | [ ] | Needs a new version source (today `CTX_VERSION` in `lib/core.sh`, also read by `upgrade --check`); `install.sh` file list (no `lib/*.sh`); `CTX_REPO` in `install.sh` line 11 must honor the environment (`${CTX_REPO:-…}`) so forks and `CTX_UPGRADE_REPO` reach the downloads |

Known follow-ups (found in E2): the Bash `prompt_extra_paths` handling never checks the last entry; Go mirrors it for parity. Fix both together in a later phase.

## Backlog (requested 2026-10-20)

| Item | Notes |
|------|-------|
| Profile dashboard | A comprehensive, intuitive visual view of profiles, opened by a command (for example a `ctx dashboard` TUI) or something better. |
| ctx logo | A logo for the project. |
| Project website | Modeled on https://go-tui.dev/: simple and clean, with docs and its color palette as the reference; ctx's own branding and copy. |

---

## Done in this slice (reference)

- A1, A2, B1, B3, C1, C2, C3, E1, E2, E3, E4a, E4b, E4c (see git history / CHANGELOG [Unreleased]).

## References

- [CONTRIBUTING.md](../../CONTRIBUTING.md) — tests, ShellCheck, hooks  
- [AGENTS.md](../../AGENTS.md) — agent workflow  
- [docs/runbooks/common-issues.md](../runbooks/common-issues.md)
