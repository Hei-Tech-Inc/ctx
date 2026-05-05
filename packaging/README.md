# Packaging notes

## Pin a release (recommended for automation)

Instead of tracking `main`, pin `install.sh` to a [release tag](https://github.com/Hei-Tech-Inc/ctx/tags):

```bash
curl -fsSL https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/v3.1.1/install.sh | bash
```

Replace `v3.1.1` with the tag you want. Check `CTX_VERSION` in `lib/core.sh` on that tag if you need to confirm the advertised CLI version.

## Homebrew tap

Ready-made formula and publish steps live under **[`packaging/homebrew-tap/`](homebrew-tap/)**.

That directory is designed to be copied into its **own** repo **`Hei-Tech-Inc/homebrew-tap`** so users can run:

```bash
brew tap Hei-Tech-Inc/tap
brew install ctx
```

The formula installs **`bin/ctx`** and **`lib/*.sh`** from a **tagged tarball** (same layout as a git checkout). It does **not** run the full interactive `install.sh` (mise/gum/gh remain optional — see formula caveats).

See **`homebrew-tap/README.md`** for publishing and bumping versions when you tag **`ctx`**.

## Linux packages

Same idea: vendor the tagged `install.sh` or install scripts into `.deb`/`.rpm` build steps, or ship `bin` + `lib` into `/usr/local` with your packager’s layout.
