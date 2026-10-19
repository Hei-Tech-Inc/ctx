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

The formula ships the **`ctx` CLI and `ctx-core`** (the Go binary behind `ctx mcp`, built at install time); optional tools (`mise`, `gum`, `gh`, …) still come from the full **`install.sh`** flow or your own installs — run **`ctx doctor`** after setup.

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

### Clone-only profile (add the rest later)

If you only need **`ctx clone`** to rewrite `git@github.com:…` → `git@github-<profile>:…`:

```bash
ctx setup --scope clone
# or: ctx setup --config examples/setup.clone-only.conf.example --yes

ctx clone -p <profile> git@github.com:Org/repo.git
```

Add git identity, a client folder, cloud defaults, or secrets when you need them:

```bash
ctx setup --extend <profile>
```

---

## Commands

### Setup

| Command | Description |
|---------|-------------|
| `ctx init` | Check deps, install hooks |
| `ctx config [show]` | Show current ctx config values |
| `ctx config work-root <path>` | Set default root used by `ctx setup` for client folders |
| `ctx config autoswitch-notify <on\|off>` | Show `[ctx] ←/→` on profile change (default **off**) |
| `ctx config secret-provider <auto\|keychain\|file\|pass>` | Choose where `ctx secret` stores values (reuse existing OS-backed defaults) |
| `ctx config export <dir>` | Export portable profile/config bundle for laptop migration (no secrets) |
| `ctx config import <dir>` | Import portable profile/config bundle on a new machine |
| `ctx setup` | Configure a new client profile (recommended) |
| `ctx import` / `ctx add` | Alias for `ctx setup` |

### Daily use

| Command | Description |
|---------|-------------|
| `ctx use <name>` | Activate a profile (git, SSH, AWS, GCP, Azure, kubectl) and set `AWS_PROFILE`, secrets and `CTX_ACTIVE_PROFILE` in your shell (needs the `ctx` shell function, see below) |
| `ctx use <name> --eval bash\|fish` | Same, but stdout is only shell code: `eval "$(ctx use acme --eval bash)"` or `ctx use acme --eval fish \| source` |
| `ctx deactivate` | Clear `active=` in `~/.ctx/config` and unset the profile's variables in your shell (without the shell function: `eval "$(ctx deactivate)"`) |
| `ctx list` | List all profiles |
| `ctx status` | Show active profile + live service checks |
| `ctx clone [-p <profile>] <url> [-- git-args...]` | `git clone` with GitHub URL rewrite for `github-<profile>` (see below) |
| `ctx run [-p <profile>] -- <cmd>` | Run one command with the folder's profile identity (see **AI agents and scripts** below) |
| `ctx env [-p <profile>] [--fish] [--secrets]` | Print that identity as `export` lines for `eval` |
| `ctx agent-rules [--write [file]]` | Print, or write into `AGENTS.md`, instructions that tell AI agents how to use ctx |

**Why a shell function:** `ctx` is a separate program, and a program can't change the environment of the shell that started it. The hook installed by `ctx install-hook` therefore also defines a `ctx` shell function (bash, zsh, fish). For `use` and `deactivate` it runs `command ctx … --eval <shell>` and evaluates the result in your shell; every other subcommand goes straight to the `ctx` program. Global flags work as before (`ctx -q use acme`), and `ctx --dry-run use acme` doesn't touch your shell. If the function isn't loaded, `ctx use` still switches git, SSH, gh and cloud CLIs, and prints a warning listing the variables it couldn't set.

### AI agents and scripts

`ctx run -- <command>` starts one process with the identity of the profile that owns the current folder (or `-p <profile>`), without touching any other terminal:

```bash
cd ~/clients/acme
ctx run -- claude            # the agent and everything it spawns act as acme
ctx run -- gh pr create      # acme's GitHub account, even if another tab uses a different one
```

It sets `GIT_AUTHOR_*`/`GIT_COMMITTER_*`, `GIT_SSH_COMMAND` (the profile's key only), `GH_TOKEN` (from `gh auth token -u <GITHUB_USER>`), `AWS_PROFILE`, `CLOUDSDK_CORE_PROJECT`/`ACCOUNT`, `AZURE_SUBSCRIPTION_ID`, `EXTRA_ENVS` and the profile's secrets. kubectl has no per-process context variable, so `KUBE_CONTEXT` is not applied; use `kubectl --context` in that case.

`ctx env` prints the same variables for `eval "$(ctx env)"` (or `ctx env --fish | source`). Secrets are left out unless you pass `--secrets`, so running it by accident doesn't print them.

`ctx agent-rules --write` adds a short section to the repo's `AGENTS.md` (between markers, safe to re-run). It tells Cursor, Claude Code, Codex and similar agents to check identity with `ctx status --json`, run credentialed commands through `ctx run`, and stop rather than use `--no-verify` when `ctx guard` blocks a commit.

### AI ops agent (`ctx mcp`)

`ctx mcp` is an [MCP](https://modelcontextprotocol.io) server that lets an AI agent (Claude Code, Cursor, and similar) do routine Kubernetes operations for **one client**: log in to a cluster, read and search logs, trace an ID across services, explain where a service gets a setting, restart a deployment, and remember what it learned. It uses the client's own login steps and conventions, which you write once.

It is scoped to the profile that owns the folder the agent was started in. Outside any client folder it offers a single `client_context` tool that says no client is active. It never falls back to the profile marked active in `~/.ctx/config`, and one client's clusters, services, recipes and activity are never visible from another client's folder. If the open folders change mid-session so they no longer resolve to that one client (for example `/add-dir` of another client's folder), ctx stops serving that session: only `client_context` stays, it explains why, and the session's port-forwards are stopped. Start a new session to continue.

It needs `ctx-core`, the Go binary that `install.sh`, `ctx upgrade` and the Homebrew formula install next to the ctx libs. Check with `ctx doctor` (`ctx-core: <version> (<path>)`). If no build exists for your OS and CPU the installer warns `ctx mcp unavailable` and the rest of ctx works as before.

#### ctx-core in everyday commands

`ctx mcp` and `ctx knowledge` always run in `ctx-core` and error if it is missing. These run in Go when `ctx-core` is installed, for speed, and fall back to the Bash implementation silently when it is not; their behavior is unchanged: `ctx list --json`, `ctx status --json`, `ctx workdir-prompt`, `ctx env`, `ctx run`, `ctx guard` (`check`, `install`, `uninstall`, `status`), `ctx secret get|list|delete`, every `ctx config` subcommand, `ctx deactivate`, and `ctx use` when `CTX_QUIET=1` (the shell autoswitch hook sets it). Set `CTX_CORE=off` to force Bash for all of them. Still Bash: `ctx secret set|migrate`, `ctx use` without `CTX_QUIET=1`, text `ctx list`/`ctx status`, `ctx setup`, `doctor`, `verify`, `clone`, `init`, `install-hook`, `upgrade`, `uninstall`, `remove`, `undo`, `edit`, `completion` and `help`.

#### 1. Describe the client

```bash
ctx knowledge init acme     # or run it inside the client's folder with no name
```

This creates `~/.ctx/knowledge/acme/` (mode `0700`, files `0600`) with three commented YAML files, plus `recipes/` and `scripts/`:

```yaml
# clusters.yaml: how to log in to each cluster. Two login types exist:
#   command    steps you write; run in order with KUBECONFIG set to a ctx-owned file
#   kubeconfig use an existing kubeconfig file and context; `path` (absolute or ~/)
#              and `context` are both required, ctx never uses current-context
clusters:
  nonprod:
    login:
      type: command
      secrets: [CLUSTER_PASSWORD]      # keys in the ctx secret store, passed to the steps as env vars
      run:
        - vendor-cli login -a api-nonprod.example.net -u jane.doe -p "$CLUSTER_PASSWORD" -k
        - vendor-cli get-credentials nonprod
  prod:
    login:
      type: command
      secrets: [CLUSTER_PASSWORD]
      run:
        - vendor-cli login -a api-prod.example.net -u jane.doe -p "$CLUSTER_PASSWORD" -k
        - vendor-cli get-credentials prod
```

```yaml
# environments.yaml: environment name -> cluster + namespace (+ aliases)
environments:
  dev:     { cluster: nonprod, namespace: app-dev }
  preprod: { cluster: nonprod, namespace: app-preprod, aliases: [pp, nonprod] }
  prod:    { cluster: prod,    namespace: app-prod,    protected: true }
network_tool:                     # optional: pod for network_test
  namespace: network-utility
  selector: app=network-tool
  container: network-tool
```

```yaml
# services.yaml: workload names and the nicknames you use for them
services:
  subscription-cis-system: { kind: deployment, aliases: [subs, subscription, cis] }
  customer-kyc-verification-aggregator: { kind: deployment, aliases: [kyc, kyc-verify] }
```

Another client's `clusters.yaml` might be a single `az aks get-credentials ...` step, or `type: kubeconfig` with a `path` and `context` that already exist. Nothing in ctx is specific to a cloud or vendor CLI.

Recipes are markdown runbooks in `recipes/*.md` (front matter with `name`, `description`, `tags`, `params`, `changes_state`) that the agent reads and follows with the tools. Scripts for `network_test` go in `scripts/*.sh`. The agent can add services, recipes and scripts through its save tools, each behind a confirmation; `clusters.yaml` and `environments.yaml` are only edited by you.

#### 2. Store login secrets

```bash
ctx secret set acme CLUSTER_PASSWORD
```

Login steps reference the secret as `$CLUSTER_PASSWORD`. ctx puts it in the step's environment; it is never written into a knowledge file or the step text.

#### 3. Register the server

```bash
ctx mcp install claude     # runs: claude mcp add --scope user ctx -- ctx mcp
ctx mcp install cursor     # merges ctx into ~/.cursor/mcp.json, keeping other servers
```

`ctx mcp install cursor` writes a backup (`~/.cursor/mcp.json.ctx-backup-<timestamp>`) before changing the file and does nothing on a second run. If `claude` is not on `PATH`, `ctx mcp install claude` prints the command for you to run and exits 1.

Then start the agent inside the client's folder and ask: "log in to preprod", "restart subs on preprod", "trace this MSISDN in kyc and subs on prod for the last hour and export it", "where does SUBSCRIPTION_URL in subs come from on prod?". To pin a profile instead of using the folder, register `ctx mcp --profile NAME`.

#### Confirmation and safety

- **Read tools** (`client_context`, `login`, `logs`, `trace`, `describe_service`, `kubectl_read`, `discover_services`, `search_knowledge`, `get_recipe`, `activity`) run without asking. `login` only changes ctx-owned files.
- **Change tools** (`restart`, `network_test`, `port_forward`, `kubectl_change`, `save_recipe`, `save_service`, `save_script`) never run on the agent's say-so alone. ctx itself asks you first, through an MCP elicitation that shows the profile, environment, cluster, namespace and the exact command, and runs it only if you accept. Decline, cancel or timeout means it did not run. ctx's confirmation dialog appears even in auto mode and with "always allow" rules, because it comes from ctx and not from the host's permission system.
- If the host does not support elicitation, change tools run nothing and return the exact command for you to run yourself.
- Script and recipe bodies are shown in the confirmation in full. A body over 400 lines or 64 KiB is refused rather than truncated; split it or run it yourself.
- `kubectl config`, `kuberc` and `plugin` are refused in both `kubectl_read` and `kubectl_change`, because ctx manages kubeconfig itself. kubectl runs with `KUBERC=off`, so personal kubectl preferences don't change what ctx runs.
- **Do not configure a Claude Code `Elicitation` hook that auto-accepts for ctx.** It would answer ctx's confirmation on your behalf and bypass it.
- Environments marked `protected: true` also require the canonical environment name for change tools, so a nickname cannot reach production by mistake.
- Every kubectl call ctx makes carries `--kubeconfig <ctx-owned file>` and `-n <namespace>` (plus `--context` for `kubeconfig` logins). `KUBECONFIG` from the environment is ignored and `~/.kube/config` is never written; kubeconfigs live in `~/.ctx/kube/<profile>/<cluster>.yaml`.
- **Secrets are never shown.** Every value in the profile's `SECRET_KEYS` and every key named in a cluster's `login.secrets` is replaced with `<redacted>` in tool output, errors and the activity log. Kubernetes Secret `data` and `stringData` values are always `<redacted>`, and `describe_service` shows Secret references by name and key only.
- Calls are recorded in `~/.ctx/knowledge/<profile>/activity.jsonl`; full log exports go to `exports/` in the same folder.

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

Secrets are exported into your shell session by `ctx use` (through the `ctx` shell function from `ctx install-hook`, or `eval "$(ctx use <name> --eval bash)"`) and loaded into your env by `mise.toml` hooks when you `cd` into the client directory.

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
| `ctx guard [status\|install\|uninstall]` | Block commits and pushes made with the wrong profile identity (see **Git guard** below) |
| `ctx completion <zsh\|bash\|fish>` | Print shell completion script |
| `ctx version` | Print version |

**doctor vs verify:** run **`ctx doctor`** after install or when something feels “broken globally” (missing tools, hooks, SSH include). Run **`ctx verify`** (optional profile name) when Git/SSH/`gh` misbehave for **that client** — it performs a live GitHub SSH auth check for `github-<profile>`.

### Git guard

`ctx guard install` stops the two most common multi-account mistakes before they reach a client:

- **Commit with the wrong email.** In a repo under a profile's `WORK_DIR` (or a `.ctx` override), the commit is blocked unless the author and committer email match that profile's `GIT_EMAIL`. This catches repo-local `user.email` overrides, `GIT_AUTHOR_EMAIL` in the environment, and repos outside the `includeIf` path.
- **Push with another profile's SSH key.** Pushing to `git@github-<other-profile>:…` from a repo that belongs to a different profile is blocked. Pushing via plain `github.com` prints a warning with the `git remote set-url` fix.

```
$ git commit
✗ ctx guard: commit blocked — author email is 'me@personal.com', but this repo belongs to profile 'acme' (dev@acme.com).
  Fix:       git config user.email 'dev@acme.com'
  Skip once: git commit --no-verify    Turn off: CTX_GUARD=off
```

How it works: it sets the global `core.hooksPath` to `~/.ctx/git-hooks`. Every hook there hands off to the repo's own `.git/hooks/<name>`, so pre-commit, Git LFS and other per-repo hooks keep running. Repos outside any profile are not checked.

Limits:
- It refuses to install if you already have a global `core.hooksPath`. Call `ctx guard check pre-commit` and `ctx guard check pre-push "$@"` from your own hooks instead (exit code 3 means block).
- Repos that set their own `core.hooksPath` (for example husky) skip the guard. `ctx guard status` tells you when that happens.
- The profile is resolved at the repo root, so a `.ctx` file in a monorepo subfolder is not used.
- `ctx guard uninstall` removes it.

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
- Prints **`[ctx] → name`**, **`[ctx] ← old → new`**, or **`[ctx] ← name`** on stderr when the active profile changes — **off by default** (screen-share safe). Enable with **`ctx config autoswitch-notify on`**, or for one session: **`export CTX_AUTOSWITCH_NOTIFY=1`**.
- Runs **`eval "$(ctx deactivate --eval bash)"`** (or fish equivalent) before switching, so secrets and env vars from the previous profile are cleared in your shell.
- Runs **`eval "$(CTX_AUTO_SWITCH=1 command ctx use --eval bash <name>)"`** (fish: `… --eval fish | source`) so `AWS_PROFILE`, secrets and the other profile variables land in your shell, and `~/.ctx/config` marks activation as **auto** (not manual-lock). A new terminal opened inside a profile's folder gets the same variables even when that profile is already active.
- Defines the **`ctx` shell function** described under **Daily use**, so a manual `ctx use` / `ctx deactivate` also changes your shell.
- Sets **`GH_TOKEN`** for this terminal from `gh auth token -u <GITHUB_USER>` instead of running `gh auth switch`, which changes the account for every terminal on the machine. Two tabs in two client folders use two GitHub accounts at the same time. A `GH_TOKEN` you exported yourself is left alone. Needs gh 2.40 or newer; a manual `ctx use` still runs `gh auth switch`.
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

You can also run **`ctx workdir-prompt`** (prints the same `work_dir='WORK_DIR=…'` line when in scope, or nothing) and embed it in **`PS1`** / **`PROMPT`** if you prefer a subprocess over env vars.

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

**Inside a profile's `WORK_DIR`, plain GitHub URLs just work.** The profile's git identity file (`~/.config/git/ctx-<profile>`, loaded by `includeIf`) rewrites `git@github.com:` and `ssh://git@github.com/` to `git@github-<profile>:`. That covers `git clone` run in the client folder and every existing repo there:

```bash
cd ~/clients/acme
git clone git@github.com:Acme-Corp/example-app.git   # uses github-acme's key
```

Existing profiles get the rewrite the next time you run `ctx use <profile>` (or `cd` into the folder with autoswitch on). HTTPS URLs are left alone.

**Outside `WORK_DIR`**, clone with the host alias directly:

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

Outside a profile's `WORK_DIR`, `git@github.com:...` uses your *default* SSH key, and GitHub responds with “repository not found” for private repos the default account can't see. `ctx use` adds the profile's key to your SSH agent without removing your own keys, and takes it out again on `ctx deactivate` (or when the hook leaves the folder), so a client key doesn't linger for plain `github.com` elsewhere.

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

Optional fields: `AZURE_SUBSCRIPTION`, `AZURE_TENANT`, `GCP_PROJECT`, `GCP_ACCOUNT`, `KUBE_CONTEXT`, `SECRET_KEYS`, `EXTRA_ENVS`, `SIGNING_FORMAT`, `SIGNING_KEY`.

**Commit signing:** `SIGNING_FORMAT=ssh` signs commits and tags in the profile's folder with its SSH key (or `SIGNING_KEY`, a key path); `SIGNING_FORMAT=openpgp` uses the GPG key ID in `SIGNING_KEY`. Setup asks after the git email, and `--config` accepts `SIGNING_FORMAT=ssh|gpg|none`. For GitHub's "Verified" badge, add the public key as a **signing** key: `gh ssh-key add ~/.ssh/<key>.pub --type signing`.

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
