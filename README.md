# ctx — client context switcher

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

1. `ctx setup` — wizard detects your existing SSH keys, `gh` accounts, AWS/GCP/Azure configs and configures a profile
2. `ctx` writes:
   - `~/.ctx/profiles/<name>.conf` — profile metadata
   - `~/.config/git/ctx-<name>` — git identity
   - `~/.gitconfig` — one `includeIf` line per profile (deduped)
   - `~/.ssh/ctx_config` — SSH host aliases (never touches `~/.ssh/config` directly)
   - `~/clients/<name>/mise.toml` — env vars + enter/leave hooks (auto-loads on `cd`)
3. Secrets → macOS Keychain only, never on disk

---

## Requirements

- macOS or Linux
- bash or zsh
- Internet connection (for the one-liner install)

Everything else (`mise`, `gum`, `gh`, `git`, `awscli`) is installed automatically.

---

## Installation

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main/install.sh | bash
```

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
| `ctx setup` | Configure a new client profile (recommended) |
| `ctx import` / `ctx add` | Alias for `ctx setup` |

### Daily use

| Command | Description |
|---------|-------------|
| `ctx use <name>` | Activate a profile (git, SSH, AWS, GCP, Azure, kubectl) |
| `ctx list` | List all profiles |
| `ctx status` | Show active profile + live service checks |

### Secrets (macOS Keychain)

| Command | Description |
|---------|-------------|
| `ctx secret set <profile> <KEY>` | Store a secret |
| `ctx secret get <profile> <KEY>` | Retrieve a secret |
| `ctx secret list <profile>` | List secret keys for a profile |
| `ctx secret delete <profile> <KEY>` | Delete a secret |

Secrets are exported into your shell session by `ctx use` and loaded into your env by `mise.toml` hooks when you `cd` into the client directory.

### Maintenance

| Command | Description |
|---------|-------------|
| `ctx edit <name>` | Open profile config in `$EDITOR` |
| `ctx remove <name>` | Delete profile + Keychain secrets |
| `ctx undo` | Restore last backup |
| `ctx install-hook [rc]` | Install mise + ctx hooks into shell rc |
| `ctx doctor` | Full health check |
| `ctx completion <zsh\|bash>` | Print shell completion script |
| `ctx version` | Print version |

### Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview changes without writing files |
| `--quiet` / `-q` | Suppress output (used by auto-switch) |
| `--no-gum` | Force plain text prompts |

---

## Auto-switch

After installation, `ctx` automatically activates the right profile when you `cd` into a client directory. You'll see a dim indicator in your terminal:

```
[ctx] acme
```

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
git clone git@github-points-africa:Points-Africa/pa-pale.git
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

Remove the `ctx auto-switch` and `mise activate` blocks from your shell rc file, then reload.

---

## License

MIT
