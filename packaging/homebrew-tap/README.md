# Homebrew tap — `ctx`

Live tap repo: **`https://github.com/isaackumi/homebrew-tap`**

(`brew tap isaackumi/tap` maps to user/org **`isaackumi`** + repo **`homebrew-tap`**.)

Upstream CLI source: **`https://github.com/Hei-Tech-Inc/ctx`**

## First push from your machine

**A — Clone the empty GitHub repo** (remote `origin` is already set):

```bash
git clone git@github.com:isaackumi/homebrew-tap.git
cd homebrew-tap
mkdir -p Formula
cp /path/to/ctx/packaging/homebrew-tap/Formula/ctx.rb Formula/
cp /path/to/ctx/packaging/homebrew-tap/README.md ./README.md
git add Formula/ctx.rb README.md
git commit -m "Add ctx formula"
git branch -M main
git push -u origin main
```

**B — New folder, connect your remote** (matches GitHub’s suggested commands):

```bash
mkdir homebrew-tap && cd homebrew-tap
git init -b main
mkdir -p Formula
cp /path/to/ctx/packaging/homebrew-tap/Formula/ctx.rb Formula/
cp /path/to/ctx/packaging/homebrew-tap/README.md ./README.md
git add Formula/ctx.rb README.md
git commit -m "Add ctx formula"
git remote add origin git@github.com:isaackumi/homebrew-tap.git
git branch -M main
git push -u origin main
```

## Users install `ctx`

```bash
brew tap isaackumi/tap
brew install ctx
```

One-liner:

```bash
brew install isaackumi/tap/ctx
```

## Bump version when you tag **`Hei-Tech-Inc/ctx`**

When you release tag **`vX.Y.Z`** on **`Hei-Tech-Inc/ctx`**:

1. Tarball + checksum:

   ```bash
   curl -fsSL -o ctx.tgz "https://github.com/Hei-Tech-Inc/ctx/archive/refs/tags/vX.Y.Z.tar.gz"
   shasum -a 256 ctx.tgz
   ```

2. Edit **`Formula/ctx.rb`**: `url`, `sha256`, `version`.
3. Commit and push **`isaackumi/homebrew-tap`**.

Optional: `brew bump-formula-pr` after tagging upstream.
