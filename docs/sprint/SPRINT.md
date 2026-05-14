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

---

## Done in this slice (reference)

- A1, A2, B1, B3, C1, C2, C3 (see git history / CHANGELOG [Unreleased]).

## References

- [CONTRIBUTING.md](../../CONTRIBUTING.md) — tests, ShellCheck, hooks  
- [AGENTS.md](../../AGENTS.md) — agent workflow  
- [docs/runbooks/common-issues.md](../runbooks/common-issues.md)
