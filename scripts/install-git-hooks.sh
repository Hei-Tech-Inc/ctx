#!/bin/sh
# Point this repo at scripts/git-hooks (commit-msg + prepare-commit-msg).
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
chmod +x "$ROOT/scripts/git-hooks/commit-msg" "$ROOT/scripts/git-hooks/prepare-commit-msg"
git -C "$ROOT" config core.hooksPath scripts/git-hooks
echo "Configured: git config core.hooksPath scripts/git-hooks"
echo "Hooks active for $(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null || echo "$ROOT")"
