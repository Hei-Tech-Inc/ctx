# ctx — client context switcher

[![CI](https://github.com/Hei-Tech-Inc/ctx/actions/workflows/ci.yml/badge.svg)](https://github.com/Hei-Tech-Inc/ctx/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/Hei-Tech-Inc/ctx?label=release)](https://github.com/Hei-Tech-Inc/ctx/releases)
[![GitHub stars](https://img.shields.io/github/stars/Hei-Tech-Inc/ctx?style=flat&logo=github)](https://github.com/Hei-Tech-Inc/ctx/stargazers)
[![license](https://img.shields.io/github/license/Hei-Tech-Inc/ctx)](LICENSE)

Switch between client environments in one command. `ctx use acme` rotates your git identity, GitHub account, SSH key, AWS profile, GCP project, Azure subscription, kubectl context, and Keychain secrets — instantly.

```
ctx use acme
✓ GitHub → acme-bot
✓ SSH key → id_ed25519_acme
✓ AWS_PROFILE → acme-prod
✓ kubectl → acme-cluster
```

Profiles auto-activate when you `cd` into a client directory.

---

## How it works

1. `ctx setup` — interactive wizard with onboarding modes: use current directory (default), provide an existing path, or create a new client folder; it can import existing SSH/GitHub/cloud references or run fully manual
2. `ctx` writes:
   - `~/.ctx/profiles/<name>.conf` — profile metadata
   - `~/.config/git/ctx-<name>` — git identity
   - `~/.gitconfig` — one `includeIf` line per profile (deduped)
   - `~/.ssh/ctx_config` — SSH host aliases (never touches `~/.ssh/config` directly)
   - `~/clients/<name>/mise.toml` — env vars + enter/leave hooks (auto-loads on `cd`)
3. Secrets → explicit opt-in during setup. On macOS they go to Keychain; on Linux/other Unix, `~/.ctx/secrets/<profile>/` (file per key, `0600`) — encrypt your disk.

---

## Requirements

- macOS or Linux
- bash or zsh
- Internet connection (for the one-liner install)

Everything else (`mise`, `gum`, `gh`, `git`, `awscli`) is installed automatically.

### Windows support

`ctx` does not currently support native PowerShell/CMD usage. On Windows, use **WSL2** (Ubuntu recommended) and run `ctx` inside WSL.

Quick start on Windows:

```powershell
wsl --install -d Ubuntu
```

Then in the Ubuntu shell:

```bash
curl -fsSL https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main/install.sh | bash
source ~/.bashrc
ctx init
ctx setup
```

Notes for WSL users:

- Keep your `ctx` workflow in WSL (`git`, `ssh`, `gh`, `ctx`) for consistent identity/key behavior.
- `~/.ctx`, `~/.ssh`, and shell hooks live in the WSL Linux home, not Windows user directories.
- Secret storage follows Linux behavior (`~/.ctx/secrets/...`, file mode `0600`) — use device/disk encryption.

---

## Installation

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main/install.sh | bash
```

### Pin a release (recommended for CI / production)

`main` moves; for reproducible installs use a [release tag](https://github.com/Hei-Tech-Inc/ctx/tags):

```bash
curl -fsSL https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/v3.2.0/install.sh | bash
```

Replace `v3.2.0` with the tag you trust (see [`lib/core.sh`](lib/core.sh) for `CTX_VERSION` on that ref). See [`packaging/README.md`](packaging/README.md) for Homebrew/Linux packaging notes.

### Homebrew (tap)

After the tap repo is pushed (**[`isaackumi/homebrew-tap`](https://github.com/isaackumi/homebrew-tap)** — see [`packaging/homebrew-tap/README.md`](packaging/homebrew-tap/README.md)):

```bash
brew tap isaackumi/tap
brew install ctx
```

The formula ships the **`ctx` CLI only**; optional tools (`mise`, `gum`, `gh`, …) still come from the full **`install.sh`** flow or your own installs — run **`ctx doctor`** after setup.

### From source

```bash
git clone https://github.com/Hei-Tech-Inc/ctx.git
cd ctx
bash install.sh
```

### After install, reload your shell

```bash
source ~/.zshrc   # or ~/.bashrc
```

### Upgrade

After `ctx` is installed, you can update to the latest `main` release with:

```bash
ctx upgrade
```

This re-runs `install.sh` from GitHub and refreshes the `ctx` binary + library scripts. Your `~/.ctx` profiles/config are preserved.

---

## Security

**Threat model (short):** `ctx` is a shell-based installer and CLI. It writes configuration under your home directory (`~/.ctx`, `~/.ssh/ctx_config` plus an `Include` line in `~/.ssh/config`, `~/.gitconfig` includes, `~/.config/git/`, client work trees). It can store secrets in the macOS Keychain or in `~/.ctx/secrets/` on other OSes. It runs external tools you already use (`git`, `gh`, `ssh`, cloud CLIs).

The default one-liner trusts **TLS to GitHub** and executes **`install.sh` from the ref you choose** (`main` or a tag). Pinning a **tagged** URL reduces moving-target risk.

Report vulnerabilities privately: see [`SECURITY.md`](SECURITY.md).

---

## Getting started

```bash
# 1. Verify deps and wire up hooks
ctx init

# Optional: set one default root for all client folders
ctx config work-root ~/clients

# 2. Build a profile from your existing machine setup
ctx setup

# 3. Activate a profile
ctx use <name>
```

---

## Commands

### Setup

| Command | Description |
|---------|-------------|
| `ctx init` | Check deps, install hooks |
| `ctx config [show]` | Show current ctx config values |
| `ctx config work-root <path>` | Set default root used by `ctx setup` for client folders |
| `ctx config secret-provider <auto\|keychain\|file\|pass>` | Choose where `ctx secret` stores values (reuse existing OS-backed defaults) |
| `ctx config export <dir>` | Export portable profile/config bundle for laptop migration (no secrets) |
| `ctx config import <dir>` | Import portable profile/config bundle on a new machine |
| `ctx setup` | Configure a new client profile (recommended) |
| `ctx import` / `ctx add` | Alias for `ctx setup` |

### Daily use

| Command | Description |
|---------|-------------|
| `ctx use <name>` | Activate a profile (git, SSH, AWS, GCP, Azure, kubectl) |
| `ctx deactivate` | Clear `active=` in `~/.ctx/config`; stdout is `eval`-able unsets for secrets/env (run `eval "$(ctx deactivate)"` in bash/zsh) |
| `ctx list` | List all profiles |
| `ctx status` | Show active profile + live service checks |
| `ctx clone [-p <profile>] <url> [-- git-args...]` | `git clone` with GitHub URL rewrite for `github-<profile>` (see below) |

### Secrets (Keychain or `~/.ctx/secrets`)

| Command | Description |
|---------|-------------|
| `ctx secret set <profile> <KEY>` | Store a secret |
| `ctx secret get <profile> <KEY>` | Retrieve a secret |
| `ctx secret list <profile>` | List secret keys for a profile |
| `ctx secret delete <profile> <KEY>` | Delete a secret |
| `ctx secret migrate <profile> <to-provider> [from-provider]` | Copy secrets between backends (`keychain`, `file`, `pass`) |

On macOS, secrets prefer the Keychain. On Linux and other Unixes, values live under `~/.ctx/secrets/<profile>/` (file per key, `0600`) — use full-disk encryption.
Setup defaults to **Skip secrets for now** and requires explicit opt-in to store any secret value.
You can force this behavior with `ctx config secret-provider <auto|keychain|file|pass>`.
When using `pass`, entries are stored as `ctx/<profile>/<KEY>` in your password store.
Run `ctx doctor` to verify your selected provider is available on the machine.

### Enterprise secrets (Vault / 1Password)

ctx integrates **four storage backends only**: `auto`, `keychain`, `file`, and `pass` (see `ctx config secret-provider`). There are **no** built-in providers for HashiCorp Vault, 1Password CLI (`op`), Redis, SQLite, or other databases — Redis and SQLite are also poor fits as primary secret stores for typical dev workflows.

**How env injection works:** For each name in `SECRET_KEYS`, the generated `mise.toml` **[hooks.enter]** script reads the **stored value** from the active provider (Keychain, `~/.ctx/secrets/…`, or `pass`) and runs `export KEY=value`. The shell therefore receives the **literal string** in the backend — not a Vault path or `op://` reference unless you deliberately stored such a string as the value (most apps expect a real secret in `$VAR`; they do not resolve 1Password URIs for you).

**Pointers vs committed files:** Profile files list **key names** only; secret material is meant to live in the provider above, not in git. ctx does **not** resolve indirection like `vault kv get` or `op read` at hook time today.

**Practical patterns without changing ctx:**

- **1Password:** Run commands under `op run -- …`, or use **1Password Connect** / sidecars where your stack already injects env. You can still use ctx for Git, SSH, and profile switching while API keys come from `op`.
- **Vault:** Use **Vault Agent** templates, `vault kv get` in project scripts/CI, or env from your orchestrator; pair with ctx for shell identity and repo layout.
- **Unix-native secret store:** `pass` (GPG-backed paths under `ctx/<profile>/<KEY>`) is the closest built-in “centralized” option among ctx providers.

**Operations note:** `ctx secret migrate` copies **plaintext** between `keychain`, `file`, and `pass` — run only on trusted machines. For audit, rotation, and team policy, standardize on Vault or 1Password at the organization layer and treat ctx as **local shell + SSH + Git orchestration**, not the system of record for every API key.

### Moving to a new laptop safely

Use config export/import for non-secret state:

```bash
# old machine
ctx config export ~/ctx-migration-$(date +%Y%m%d)

# new machine
ctx config import ~/ctx-migration-YYYYMMDD
ctx doctor
```

Export includes profiles, git identities, and ctx SSH host config.  
Secrets are intentionally excluded; restore with your provider (`keychain`/`pass`/file) or `ctx secret set`.

For ongoing cross-machine sync, see [`docs/chezmoi.md`](docs/chezmoi.md) for a `chezmoi`-based workflow.

Secrets are exported into your shell session by `ctx use` and loaded into your env by `mise.toml` hooks when you `cd` into the client directory.

### Maintenance

| Command | Description |
|---------|-------------|
| `ctx verify [name]` | **One profile:** live `ssh -T` to `github-<profile>`, `gh` account vs profile, work dir, key file, email shape |
| `ctx doctor` | **Whole machine:** required tools on `PATH`, mise/ctx shell hooks, SSH `Include` for `ctx_config`, quick sanity on every profile’s paths (no per-host `ssh -T`) |
| `ctx edit <name>` | Open profile config in `$EDITOR` |
| `ctx remove <name>` | Delete profile + stored secrets |
| `ctx undo` | Restore last backup |
| `ctx install-hook [rc]` | Install mise + ctx hooks into shell rc |
| `ctx upgrade` | Update `ctx` binary + libs in place — leaves `~/.ctx` profiles and client dirs alone (no full installer pass) |
| `ctx upgrade --check` | Compare installed version to `main` on GitHub (no install) |
| `ctx uninstall [--purge]` | Remove `ctx` binary + lib from install location; `--purge` deletes `~/.ctx` |
| `ctx doctor` | Full health check (see **doctor vs verify** below) |
| `ctx completion <zsh\|bash\|fish>` | Print shell completion script |
| `ctx version` | Print version |

**doctor vs verify:** run **`ctx doctor`** after install or when something feels “broken globally” (missing tools, hooks, SSH include). Run **`ctx verify`** (optional profile name) when Git/SSH/`gh` misbehave for **that client** — it performs a live GitHub SSH auth check for `github-<profile>`.

### Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview changes without writing files |
| `--quiet` / `-q` | Suppress output (used by auto-switch) |
| `--no-gum` | Force plain text prompts |

---

## Auto-switch

The shell hook (`ctx install-hook` / `install.sh`) appends **`lib/ctx_autoswitch.bash`** (bash/zsh) or **`lib/ctx_autoswitch.fish`** (fish) to your rc file. It:

- Picks the profile whose **`WORK_DIR`** is the **longest path prefix** of `$PWD` (nested clients supported).
- Prefers a **sibling profile** when a broad `WORK_DIR` covers `…/clients` and `…/clients/<name>.conf` exists.
- Applies **nearest** `.ctx` with `profile=<name>` when walking from `$PWD` up to the git root (must be a valid profile).
- Prints **`[ctx] → name`**, **`[ctx] ← old → new`**, or **`[ctx] ← name`** on stderr when the active profile changes.
- Runs **`eval "$(ctx deactivate --eval bash)"`** (or fish equivalent) before switching, so secrets and env vars from the previous profile are cleared in your shell.
- Calls **`CTX_AUTO_SWITCH=1 ctx use <name>`** so `~/.ctx/config` marks activation as **auto** (not manual-lock).
- Exports **`CTX_PROMPT_SHOW`**, **`CTX_PROMPT_PROFILE`**, and **`CTX_PROMPT_WORK_DIR`** for your prompt: by default the prompt scope is **only** under each profile’s `WORK_DIR` for **up to two extra path segments** (configurable). Folders outside any profile (e.g. a random `cd` or alias) get **`CTX_PROMPT_SHOW=0`**. See **`ctx config prompt-workdir-depth`** and **`prompt_extra_paths`**.

**Manual `ctx use`:** a normal `ctx use` sets **`active_source=manual`** and anchors to **`$PWD`** until you **`cd`** anywhere else; until then the hook will not replace your choice with directory inference.

**Prompt / Starship:** the hook sets **`CTX_ACTIVE_PROFILE`** (and **`CTX_ACTIVATION_TRIGGER=auto`**) when a profile is active. For **custom prompts** (e.g. printing `WORK_DIR=…`), use **`CTX_PROMPT_SHOW`** so you only show context when `PWD` is actually under a configured profile tree (default: **two** path segments below that profile’s `WORK_DIR`). Example zsh:

```zsh
# In precmd or RPROMPT — do not grep profile files directly unless CTX_PROMPT_SHOW is 1
if [[ ${CTX_PROMPT_SHOW:-0} == 1 && -n ${CTX_PROMPT_WORK_DIR:-} ]]; then
  work_dir="WORK_DIR=${CTX_PROMPT_WORK_DIR}"
else
  work_dir=""
fi
```

Tune depth: **`ctx config prompt-workdir-depth 2`** (use `0` for exact `WORK_DIR` only). Add more roots: **`ctx config prompt-extra-paths '/other/abs:/another'`** or **`ctx config prompt-extra-paths clear`**.

Example Starship snippet (profile name only):

```toml
[env_var]
variable = "CTX_ACTIVE_PROFILE"
format = "via [ctx:$env_value]($style) "
```

**Deactivate:** `ctx deactivate` prints shell code to stdout — run **`eval "$(ctx deactivate)"`** (bash/zsh) or **`ctx deactivate --eval fish | source`** to clear the active marker and unset exported secrets for the **current** profile in that shell.

To override for a specific repo, add a `.ctx` file at the repo root:

```
profile=acme-staging
```

### Git clone URLs (important)

Each profile gets its own SSH host alias in `~/.ssh/ctx_config`, shaped like:

```text
github-<profile>
```

**Clone private repos using that host**, so Git uses the correct key:

```bash
git clone git@github-acme:Acme-Corp/example-app.git
```

Or use the helper (uses the active profile, or `-p`):

```bash
ctx clone git@github.com:Acme-Corp/example-app.git
# same effect as git@github-<active-profile>:...

ctx clone -p acme https://github.com/Acme-Corp/example-app.git
# optional HTTPS → SSH rewrite (prompted)
```

For arbitrary `git clone` flags, pass them after `--`:

```bash
ctx clone -- -b main --depth 1 git@github.com:Acme-Corp/example-app.git ./example-app
```

Avoid `git@github.com:...` for client work — it often picks your *default* SSH key and GitHub will respond with “repository not found” for private repos.

Your git author identity for repos under the client folder comes from `~/.gitconfig` `includeIf` → `~/.config/git/ctx-<profile>`, and `mise.toml` hooks also set local `git config` when `mise` activates.

---

## Shell completions

### zsh

```bash
ctx completion zsh > "${fpath[1]}/_ctx"
```

### bash

```bash
ctx completion bash >> ~/.bashrc
```

### fish

```fish
mkdir -p ~/.config/fish/completions
ctx completion fish > ~/.config/fish/completions/ctx.fish
```

---

## Profile structure

Profiles live in `~/.ctx/profiles/<name>.conf`. A minimal profile looks like:

```bash
PROFILE_NAME="acme"
GIT_NAME="Jane Smith"
GIT_EMAIL="jane@acme.io"
WORK_DIR="$HOME/clients/acme"
GITHUB_USER="jane-acme"
SSH_KEY_PATH="$HOME/.ssh/id_ed25519_acme"
AWS_PROFILE_NAME="acme-prod"
```

Optional fields: `AZURE_SUBSCRIPTION`, `AZURE_TENANT`, `GCP_PROJECT`, `GCP_ACCOUNT`, `KUBE_CONTEXT`, `SECRET_KEYS`, `EXTRA_ENVS`.

---

## mise.toml

Each profile gets a `mise.toml` in `~/clients/<name>/`. It is safe to commit — secrets are never written there. The `hooks.enter` block runs when you `cd` in; `hooks.leave` cleans up when you leave.

```toml
[env]
AWS_PROFILE = "acme-prod"

[hooks.enter]
shell = "bash"
script = """
  git config user.name  "Jane Smith"
  git config user.email "jane@acme.io"

  _val=$(security find-generic-password -a "$USER" -s "ctx-acme-ACME_TOKEN" -w 2>/dev/null || true)
  [[ -n "$_val" ]] && export ACME_TOKEN="$_val"
"""

[hooks.leave]
shell = "bash"
script = """
  git config --unset user.name  2>/dev/null || true
  git config --unset user.email 2>/dev/null || true
  unset ACME_TOKEN 2>/dev/null || true
"""
```

---

## Uninstall

```bash
rm -f /usr/local/bin/ctx
rm -rf /usr/local/lib/ctx
rm -rf ~/.ctx
```

Remove the `ctx profile autoswitch` block (or legacy `# ── ctx auto-switch`) and `mise activate` blocks from your shell rc file, then reload.

---

## License

MIT
