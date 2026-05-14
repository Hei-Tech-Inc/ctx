# Runbook — common issues

## Wrong GitHub account or SSH key after `cd`

**Symptoms:** `git push` / `gh` uses the wrong user; `ssh -T git@github.com` picks the default key.

**Checks:**

1. `ctx status` — activation mode (manual lock vs auto) and `CTX_ACTIVE_PROFILE`.
2. `ctx doctor` — `~/.ssh/config` includes `ctx_config`; `mise` / autoswitch hooks present.
3. Clone with **`git@github-<profile>:org/repo.git`** (not bare `github.com`) for per-client keys.

**Fix:** `ctx use <profile>` from the client tree, or fix **`WORK_DIR`** / **`.ctx`**. Put **`profile=<name>`** in the **nearest** `.ctx` walking from your folder up to the git root (nested clients under one repo used to read only the root `.ctx`). Reload shell after `ctx install-hook` / upgrade so the hook picks up nearest-`.ctx` behavior.

---

## Wrong profile / `WORK_DIR` while the folder looks right

**Cause:** Common with **multiple client folders under one git root**: the old hook only read **`.ctx` at the repo root**, so `profile=deladetech` (for example) applied under `…/hubtel` too. Your prompt or aliases may still show that profile’s `WORK_DIR` while `ls` shows the correct tree.

**Also common:** one profile uses a **broad `WORK_DIR`** (e.g. `~/clients`) so every subfolder matched that profile until you add **`~/clients/<name>.conf`** with a longer `WORK_DIR` or leave `WORK_DIR` unset — the hook then prefers the **sibling profile** named after the first directory under that prefix (`hubtel`, `acs`, …) when that `.conf` exists and `~/clients/<name>` is a real directory.

**Fix:** Upgrade ctx, **`source ~/.zshrc`**, and either add a **nearest** `.ctx` (e.g. `clients/hubtel/.ctx` with `profile=hubtel`) or remove the root override if you do not want repo-wide defaults.

---

## Autoswitch not firing (bash)

**Symptoms:** No `[ctx] →` lines; profile stuck.

**Checks:**

1. `echo "$PROMPT_COMMAND"` contains **`_ctx_profile_autoswitch`** (bash runs the hook each prompt).
2. `type ctx` resolves to the installed binary on `PATH`.

**Fix:** Re-run **`ctx install-hook`** (or reinstall hook block from `install.sh`). Remove duplicate legacy **`_ctx_auto_switch`** blocks if both exist.

---

## `ctx deactivate` did not clear my shell env

**Cause:** `ctx deactivate` runs in a **subprocess**; parent shell needs **`eval`**.

**Fix (bash/zsh):**

```bash
eval "$(ctx deactivate)"
```

**Fix (fish):**

```bash
ctx deactivate --eval fish | source
```

---

## After a force-pushed `main`, local clone is “ahead” or diverged

**Fix:**

```bash
git fetch origin
git reset --hard origin/main
```

(Re-clone if you prefer a clean object store.)

---

## Contributor graph still shows a bot

GitHub can lag. Verify history is clean:

```bash
git fetch origin
git shortlog -sne origin/main
git log origin/main --format='%B' | grep -iE '^co-authored-by:.*(cursor|anthropic|openai)' || echo 'ok'
```

If CI **`git-history-hygiene`** is green but UI is wrong, wait or contact GitHub Support.
