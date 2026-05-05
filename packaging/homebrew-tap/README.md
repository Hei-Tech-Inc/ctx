# Homebrew tap — `ctx`

This folder is meant to live in its **own** GitHub repository:

**`https://github.com/Hei-Tech-Inc/homebrew-tap`**

(`brew tap Hei-Tech-Inc/tap` expects a repo named `homebrew-tap`.)

## Publish this tap (one-time)

1. Create an empty repo **`Hei-Tech-Inc/homebrew-tap`** on GitHub (public recommended).
2. Copy **`Formula/ctx.rb`** (and this README) into that repo at the same paths.
3. Push:

```bash
git init -b main
git add Formula README.md
git commit -m "Add ctx formula"
git remote add origin git@github.com:Hei-Tech-Inc/homebrew-tap.git
git push -u origin main
```

## Users install `ctx`

```bash
brew tap Hei-Tech-Inc/tap
brew install ctx
```

One-liner:

```bash
brew install Hei-Tech-Inc/tap/ctx
```

## Bump version when you tag `ctx`

When you release **`Hei-Tech-Inc/ctx`** tag **`vX.Y.Z`**:

1. Download tarball and SHA-256:

   ```bash
   curl -fsSL -o ctx.tgz "https://github.com/Hei-Tech-Inc/ctx/archive/refs/tags/vX.Y.Z.tar.gz"
   shasum -a 256 ctx.tgz
   ```

2. Edit **`Formula/ctx.rb`**: set `url`, `sha256`, and `version "X.Y.Z"`.
3. Commit and push **`homebrew-tap`**.

Optional: from a machine with Homebrew, run `brew bump-formula-pr` against this formula after tagging upstream.
