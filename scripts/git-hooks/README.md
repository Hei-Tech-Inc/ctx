# Git hooks (optional)

## `prepare-commit-msg`

Removes the trailer `Co-authored-by: Cursor <cursoragent@cursor.com>` if your editor adds it, so **Cursor does not appear as a contributor** on GitHub.

Enable for this clone:

```bash
git config core.hooksPath scripts/git-hooks
chmod +x scripts/git-hooks/prepare-commit-msg
```

Disable:

```bash
git config --unset core.hooksPath
```

GitHub contributor counts are driven by commit **authors** and **`Co-authored-by:`** lines in commit messages. This hook only strips that specific Cursor trailer.
