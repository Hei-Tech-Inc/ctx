# Packaging notes

## Pin a release (recommended for automation)

Instead of tracking `main`, pin `install.sh` to a [release tag](https://github.com/Hei-Tech-Inc/ctx/tags):

```bash
curl -fsSL https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/v3.1.1/install.sh | bash
```

Replace `v3.1.1` with the tag you want. Check `CTX_VERSION` in `lib/core.sh` on that tag if you need to confirm the advertised CLI version.

## Homebrew (community / bring-your-own tap)

There is no official Homebrew tap yet. A tap typically:

1. Downloads a tarball or checks out a tag.
2. Runs `bash install.sh` **or** installs `bin/` and `lib/` into prefix manually.

Because `install.sh` expects an interactive environment (Homebrew, tools), many teams wrap the **pinned raw URL** above in an internal formula or script instead.

If you publish a formula, pin the `url`/`tag` and verify checksums for reproducible installs.

## Linux packages

Same idea: vendor the tagged `install.sh` or install scripts into `.deb`/`.rpm` build steps, or ship `bin` + `lib` into `/usr/local` with your packager’s layout.
