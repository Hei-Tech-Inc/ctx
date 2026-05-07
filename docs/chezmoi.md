# Using `ctx` with chezmoi

This guide shows how to manage `ctx` config across machines with [chezmoi](https://www.chezmoi.io/) without reinventing config sync.

## What to manage with chezmoi

Good candidates (portable, non-secret):

- `~/.ctx/config`
- `~/.ctx/profiles/*.conf`
- `~/.config/git/ctx-*`
- `~/.ssh/ctx_config`

Do **not** sync secret values in plain text:

- `~/.ctx/secrets/**`
- macOS Keychain contents
- `pass` private key material / GPG keychain

## Recommended workflow

1. Export current non-secret `ctx` state:

```bash
ctx config export ~/ctx-migration-$(date +%Y%m%d)
```

2. Add exported files to your chezmoi source state:

```bash
chezmoi add ~/.ctx/config
chezmoi add ~/.ctx/profiles
chezmoi add ~/.config/git/ctx-*
chezmoi add ~/.ssh/ctx_config
```

3. Apply on a new machine:

```bash
chezmoi init <your-repo>
chezmoi apply
ctx doctor
```

4. Rehydrate secrets on the new machine:

- Keychain users: re-add with `ctx secret set ...`
- `pass` users: restore your password store + GPG keys, or migrate from another provider

## Optional `.chezmoiignore` hardening

Use `.chezmoiignore` to prevent accidental secret sync:

```text
.ctx/secrets/*
```

## Notes

- `ctx config import <dir>` is useful when you are transferring a one-off migration bundle.
- `chezmoi` is best for day-to-day dotfile/config lifecycle; `ctx` remains the runtime context switcher.
