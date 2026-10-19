# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Changed
- **Go port, phase 2:** `ctx list --json`, `ctx status --json`, `ctx workdir-prompt`, `ctx env`, `ctx run` and `ctx guard check` now run in `ctx-core` (Go) when it is installed. `bin/ctx` asks `ctx-core implements <cmd>` and execs it on success; otherwise, or with `CTX_CORE=off`, the Bash implementation runs. Output and exit codes are unchanged (`env` is eval-equivalent). Text `list`/`status` stay in Bash. `ctx mcp` and `ctx knowledge` remain Go-only (no Bash fallback). Median of 20 runs, 10 profiles: `ctx guard check pre-commit` 195 ms (Bash) to 71 ms (Go); `ctx --json list` 195 ms to 49 ms.

### Fixed
- **Trailing slash in `WORK_DIR`:** the Bash resolver, `workdir-prompt` helpers and the bash/fish autoswitch hook now ignore one trailing `/` in a profile's `WORK_DIR` (for example `~/clients/acme/`), so the profile matches its folder instead of being silently skipped. This matches `ctx-core`. Guard and `env` can now act in folders where such a profile was previously ignored.

### Added
- **`ctx mcp` AI ops agent:** an MCP server (Go binary `ctx-core`) scoped to the profile that owns the current folder. Per-profile knowledge store (`ctx knowledge init`: `clusters.yaml`, `environments.yaml`, `services.yaml`, recipes, scripts), one-step cluster login using the client's own steps and `ctx secret` values, read tools (logs, trace, describe, kubectl read, search) and change tools (restart, network test, port-forward, kubectl change, save recipe/service/script) that ctx confirms with the user itself before running. Secrets are redacted everywhere. `ctx mcp install claude|cursor` registers it; `install.sh`, `ctx upgrade` and the Homebrew formula install `ctx-core`; `ctx doctor` reports it; releases attach `ctx-core_{darwin,linux}_{amd64,arm64}` and `checksums.txt`. See README "AI ops agent".
- **`ctx run [-p NAME] -- <cmd>`:** runs one command (for example an AI agent, `gh`, `terraform`) with the folder's profile identity: git author/committer, `GIT_SSH_COMMAND` with only the profile's key, `GH_TOKEN`, AWS/GCP/Azure variables, `EXTRA_ENVS` and secrets. Nothing outside that process changes.
- **`ctx env [-p NAME] [--fish] [--secrets]`:** prints the same identity as `export` lines for `eval`; secrets only with `--secrets`.
- **`ctx agent-rules [--write [FILE]]`:** prints, or upserts into `AGENTS.md` between markers, instructions for AI coding agents (check identity, use `ctx run`, don't bypass `ctx guard`).
- **Per-profile commit signing:** setup asks whether to sign commits (SSH key or GPG key ID); `--config` takes `SIGNING_FORMAT=ssh|gpg|none` and `SIGNING_KEY`. Written to the profile's git identity file as `gpg.format`, `user.signingkey`, `commit.gpgsign` and `tag.gpgsign`.
- **Plain GitHub URLs inside client folders:** each profile's git identity file now rewrites `git@github.com:` and `ssh://git@github.com/` to `git@github-<profile>:`, so `git clone`, fetch and push in that profile's `WORK_DIR` use its SSH key without `ctx clone` or editing remotes. Written by `ctx setup`; existing profiles pick it up on the next `ctx use`. Only added when the profile has a `github-<profile>` SSH host; HTTPS is untouched.
- **`ctx guard install|uninstall|status`:** opt-in global git hooks that block commits whose author or committer email doesn't match the repo's profile, and pushes to another profile's `github-<name>` SSH alias. Hooks chain to each repo's own `.git/hooks`, can be skipped with `--no-verify` or `CTX_GUARD=off`, and never overwrite an existing global `core.hooksPath`. `ctx doctor` shows whether the guard is on.
- **`ctx config autoswitch-notify on|off`:** `[ctx] ←/→` transition lines from the shell autoswitch hook are **off by default** (screen-share / new-tab privacy). Opt in with config or session env **`CTX_AUTOSWITCH_NOTIFY=1`**. Re-run **`ctx install-hook`** (or **`ctx upgrade`**) so the installed rc hook picks up the change.
- **Autoswitch hook sourcing:** `ctx install-hook` / installer now write a small **source** stub in the shell rc (not a full paste of `ctx_autoswitch.bash`), so upgrades refresh mute/notify behavior without re-editing the rc. Re-run **`ctx install-hook`** once to replace an old embedded copy.
- **`ctx workdir-prompt`:** prints `work_dir='WORK_DIR=<root>'` when the current directory is in the same prompt scope as **`CTX_PROMPT_*`** (depth + **`prompt_extra_paths`**); otherwise prints nothing — useful for **`PS1`** without grepping profile files.
- **zsh `precmd`** registration for the auto-switch hook (in addition to **`chpwd`**) so **`CTX_PROMPT_*`** refresh after commands that do not change **`$PWD`** (e.g. aliases that only run **`ctx use`**).
- **fish `fish_postexec`** hook calling the same auto-switch function for the same reason.
- **`ctx_resolve_path_with_root`** in **`lib/core.sh`**: tab-separated **`profile<TAB>resolved WORK_DIR`** after **`.ctx`** override; **`ctx_resolve_path_profile`** reads the first field only. Shell hooks re-sync **`best_work_dir`** from the final profile’s **`WORK_DIR`** after **`.ctx`** so **`CTX_PROMPT_WORK_DIR`** matches the active tree profile.
- **Prompt scope env vars** from the auto-switch hook: **`CTX_PROMPT_SHOW`** (0 or 1), **`CTX_PROMPT_PROFILE`**, **`CTX_PROMPT_WORK_DIR`** — only under a profile’s `WORK_DIR` within **`prompt_workdir_max_depth`** (default **2** path segments), or under optional **`prompt_extra_paths`** (colon-separated). Configure with **`ctx config prompt-workdir-depth`** / **`prompt-extra-paths`**. Use in zsh/bash/fish instead of grepping profile files for every `cd` (e.g. aliases to non-ctx folders).
- **`ctx --json` / `ctx list --json` / `ctx status --json`:** machine-readable output with a **`version`** field and stable keys for scripting and CI; completions advertise `--json` where relevant.
- **Golden fixture** for minimal **`generate_mise_toml`** output under `test/fixtures/mise_generated_minimal.toml`.
- **Bash auto-switch hook:** idempotent **`PROMPT_COMMAND`** wiring — strips duplicate **`_ctx_profile_autoswitch`** and legacy **`_ctx_auto_switch`** tokens before prepending once.
- **Setup reference in the CLI:** `ctx setup --help`, `ctx import --help`, and `ctx help setup` show the same reference (flags, `--config` keys, dry-run, example file URL). Completions advertise `help setup` and setup `-h` / `--help`.
- **Example config:** header comment in `examples/setup.noninteractive.conf.example` points to `ctx help setup`.
- **ctx doctor:** warns if `~/.ssh` is group- or world-writable and suggests `chmod 700` (OpenSSH may otherwise ignore keys).
- **`ctx deactivate`:** clears `active=` / manual-lock fields in `~/.ctx/config` and prints eval-able unsets for secrets and profile env (`--eval bash|fish` for the hook).
- **Directory-scoped auto-switch:** shell hook (`lib/ctx_autoswitch.bash` / `lib/ctx_autoswitch.fish`) picks the profile with the **longest `WORK_DIR` prefix** of `$PWD`, applies **`.ctx` / `profile=`** repo overrides, prints **`[ctx] ←/→`** transitions, runs **`eval "$(ctx deactivate --eval …)"`** before switching, and sets **`CTX_AUTO_SWITCH=1`** on hook-driven `ctx use`. **Manual `ctx use`** sets a **manual lock** until you `cd` away from that directory.
- **`ctx clone`:** prints **`[ctx] cloning as <profile> (email)`**; with **`CTX_QUIET=1`** (auto-switch), HTTPS GitHub URLs default to SSH rewrite without prompting.
- **`ctx status`:** shows **activation** mode (manual lock vs directory auto) and **`CTX_ACTIVE_PROFILE`** when set by the hook.

### Changed
- **SSH agent:** `ctx use` no longer empties the agent (`ssh-add -D`). It adds the profile's key, remembers that it did, and removes only that key on `ctx deactivate`, when the hook leaves the folder, or when switching profile. Keys you loaded yourself stay, so plain `git@github.com` outside client folders keeps using your own account instead of the last client's.
- **Installer** “next steps” after `install.sh` lists `ctx doctor`, `ctx help setup`, and `ctx verify` so the flow matches the docs and post-setup help text.
- **README:** added **Enterprise secrets (Vault / 1Password)** — built-in backends vs external vaults, how `mise` hooks inject values, and practical patterns (`op run`, Vault Agent, `pass`).

### Fixed
- **`ctx use` now reaches your shell:** `AWS_PROFILE`, `AZURE_SUBSCRIPTION`/`AZURE_TENANT` (without `az`), `GCLOUD_PROJECT` (without `gcloud`), the profile's secrets and `CTX_ACTIVE_PROFILE` were exported only inside the `ctx` child process, so they never reached the terminal. New **`ctx use <name> --eval bash|fish`** prints them as shell code on stdout (messages go to stderr). The shell hook now defines a **`ctx` shell function** (bash, zsh, fish) that evals `ctx use` and `ctx deactivate` for you and passes every other subcommand straight through; the autoswitch hook evals the same output, including in a new terminal opened inside an already-active profile's folder. Without the function loaded, `ctx use` prints a one-line warning naming the variables it could not apply. Re-run **`ctx install-hook`** or open a new shell to pick it up.
- **GitHub account per terminal:** the autoswitch hook (bash, zsh, fish) now sets `GH_TOKEN` for its own terminal from `gh auth token -u <GITHUB_USER>` instead of `gh auth switch`, which changed the account for every open terminal. A user-set `GH_TOKEN` is left alone; manual `ctx use` still switches globally.
- **Global flags after `--`:** `--json`, `-q`, `--dry-run` and `--no-gum` after `--` are passed to the wrapped command (`ctx run`, `ctx clone`) instead of being consumed by ctx.
- **Autoswitch hook latency:** the bash/zsh hook now resolves profiles with shell builtins only (no `grep`/`cut`/`awk`/`git` subprocesses per profile), cutting per-prompt cost from ~100 ms to ~1 ms with 10 profiles. Profile `WORK_DIR` values that need shell expansion beyond `$HOME`/`~` still fall back to sourcing the file.
- **Config writes:** `~/.ctx/config` updates (`active`, `work-root`, `secret-provider`) no longer use `sed`, so values containing `&`, `|`, or `\` are stored literally; writes are atomic (temp file + `mv`) and multi-line values are rejected.
- **Auto-switch (sibling clients):** when one profile’s **`WORK_DIR`** is a **parent** of several client dirs (e.g. `…/clients`), ctx now prefers **`…/clients/<segment>.conf`** named after the first path segment under that prefix when that profile exists and **`…/clients/<segment>`** is a directory (even if that `.conf` omits **`WORK_DIR`**).
- **Installer safety check:** `install.sh` now validates the installed `ctx` script with `bash -n` and aborts fast if syntax is invalid (prevents silent broken upgrades).
- **Atomic installer writes:** `install.sh` now stages `ctx` files in a temp directory, validates syntax, then swaps into place to avoid partially-written binaries during upgrade.
- **Doctor checks:** fixed SSH include detection, and updated command version probes so `kubectl` reports correctly instead of showing a false unknown-flag error.
- **Prompt UX + cancel behavior:** interactive gum prompts now explicitly indicate when input is expected, and `Ctrl+C` cleanly aborts setup instead of continuing to the next question.
- **Setup loading feedback:** `ctx setup` now announces credential/profile discovery up front and bounds `gh auth status` detection with a timeout to avoid appearing hung.
- **Ctrl+C reliability:** non-gum prompt paths now treat interrupted reads as cancellation (`exit 130`) so setup cannot continue after user aborts.
- **Ctrl+C propagation fix:** cancellation inside command-substitution prompts now signals the parent `ctx` process, preventing `"Cancelled by user"` text from being captured as field input.

## [3.2.0] - 2026-05-07

### Fixed
- **Secrets backend adapters:** added provider-aware storage with `ctx config secret-provider <auto|keychain|file|pass>` and a first community-backed adapter for `pass` (`ctx/<profile>/<KEY>`), while preserving existing defaults.
- **Provider operations:** added `ctx secret migrate` to copy profile secrets between backends and extended `ctx doctor` to validate provider availability (`keychain`/`file`/`pass`).
- **Laptop migration:** added `ctx config export/import` for safe portable config transfer (profiles/git identities/SSH config), explicitly excluding secrets.
- **Chezmoi docs:** added `docs/chezmoi.md` for reuse-first cross-machine config management without syncing secrets.
- **`ctx setup`:** onboarding now provides three location modes (current directory default, existing path, or new client folder), supports manual-vs-imported reference selection, and defaults secrets to explicit opt-in with clearer local-compromise warnings.
- **Docs:** added a Windows section clarifying WSL2 install/usage (`ctx` runs inside Linux shell, not native PowerShell/CMD).
- **`ctx upgrade`:** runs an **in-place** install (`CTX_UPGRADE_ONLY`) — updates only the `ctx` binary and `lib/*.sh`; skips Homebrew/mise/gum/gh steps, shell-rc snippets, and the auto-switch hook installer so existing profiles and client directories are not disturbed by noise or side effects.
- **`ctx clone`:** stopped passing the repository URL twice to `git clone` (second argument was treated as the target directory, producing folders named like `git@github.com:…`).
- **`ctx list`:** dim lines use `echo -e` so ANSI styling renders instead of literal `\033[…`.
- **Installer:** patch `CTX_LIB` only on the bootstrap line of `bin/ctx` (not a broad `CTX_LIB=.*` replace) so the installed script cannot be corrupted during upgrade.

### Changed
- Git hooks: stronger vendor co-author stripping; **`post-commit`** amends `HEAD` if a trailer still appears; `install-git-hooks.sh` installs all hook scripts.

### Added
- **`packaging/homebrew-tap/`** — Homebrew formula (`Formula/ctx.rb`) + README; tap lives at **`isaackumi/homebrew-tap`** (`brew tap isaackumi/tap`).
- CI job **git-history-hygiene** rejects vendor-bot emails or `Co-authored-by:` lines mentioning the IDE on `main`.
- Smoke test: `bin/ctx version` matches `CTX_VERSION` from `lib/core.sh`.
- GitHub Actions publishes a GitHub Release when a `v*` tag is pushed.
- `commit-msg` hook rejects IDE vendor substrings in messages (with gum/path allowlists); `prepare-commit-msg` strips problematic `Co-authored-by:` lines; `./scripts/install-git-hooks.sh`; README badges for CI, release, license.

### Changed
- Rewrote git messages that referenced IDE tooling or gum pointer flags so `git log` no longer surfaces banned substrings; CONTRIBUTING updated for hooks + hygiene.

## [3.1.1] - 2026-05-05

### Added
- CI runs `test/test.sh` on Ubuntu alongside ShellCheck.
- `SECURITY.md`; fish completions (`ctx completion fish`); `packaging/README.md`.

### Changed
- README: security/threat-model summary, pinned release install URL, doctor vs verify guidance; CONTRIBUTING/PR template aligned with CI.

## [3.1.0] - 2026-05-05

### Added
- `ctx clone`, `ctx verify`, `ctx uninstall`, and `ctx upgrade --check`.
- Documented multi-account clone flows and secret storage behavior across macOS vs Linux.
