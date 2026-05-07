# Git hooks (required for AI-assisted commits)

Run once per clone:

```bash
./scripts/install-git-hooks.sh
```

Sets `core.hooksPath` to `scripts/git-hooks` and marks hooks executable.

**In your editor:** turn **off** “add co-author”, “sign commits as assistant”, or any Git integration that appends third-party trailers. Hooks cannot win every hook order against some clients — disabling that setting is mandatory.

## Hooks

| Hook | Role |
|------|------|
| `prepare-commit-msg` | Deletes `Co-authored-by:` / `Co-Authored-By:` lines that credit coding agents (Cursor, Claude/Anthropic, OpenAI-style noreply, Copilot, Gemini, Codeium, Windsurf, etc.) — see `scripts/git-history-strip-agent-trailers.pl`. |
| `commit-msg` | Rejects disallowed agent `Co-authored-by:` lines and any remaining blocked IDE vendor substring (after allowlisting gum `--cursor.*` flags and `.cursor/` paths). |
| `post-commit` | If a trailer still landed on `HEAD`, immediately **`git commit --amend`** with the same tree and a cleaned message. |

Disable hooks (not recommended):

```bash
git config --unset core.hooksPath
```

GitHub counts **`Co-authored-by:`** lines toward contributors — keep hooks enabled and disable editor trailers.
