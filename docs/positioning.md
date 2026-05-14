# Positioning — when to use `ctx`

`ctx` is a **multi-account client workstation** tool: one command (or directory autoswitch) aligns **Git identity**, **GitHub (`gh`)**, **SSH host aliases**, optional **cloud CLIs**, and **per-profile secrets** with **mise**-driven env in client trees.

## Comparison

| Approach | Best for | Tradeoff |
|----------|----------|----------|
| **`ctx`** | Several **client / employer** contexts on one laptop; different GitHub users and SSH keys; optional AWS/GCP/Azure/k8s defaults | Opinionated layout (`WORK_DIR`, `mise.toml`, `~/.ssh/ctx_config`); Bash-centric |
| **`mise` alone** | Tool versions per directory; simple env in `mise.toml` | No first-class multi-GitHub / per-client SSH story |
| **`direnv` + `.envrc`** | Project-local env; language-agnostic | You own SSH/`gh`/git identity switching yourself |
| **Manual SSH config** | Full control | High maintenance; easy to leak wrong key to wrong host |

## Rule of thumb

- **Multiple GitHub identities + isolated SSH keys + repeatable client folders** → use **`ctx`**.
- **Single identity, only need Node/Ruby versions** → **`mise`** (or **`direnv`**) may be enough.

## Related docs

- [README.md](../../README.md) — install, autoswitch, clone URLs  
- [docs/sprint/SPRINT.md](../sprint/SPRINT.md) — roadmap / backlog  
