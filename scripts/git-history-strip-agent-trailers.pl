#!/usr/bin/env perl
# Strip coding-agent Co-authored-by / Co-Authored-By lines from a commit message (stdin → stdout).
# Used by: git filter-branch --msg-filter, and kept in-repo so the regex stays documented.
use strict;
use warnings;
local $/;
$_ = <>;
# Coding assistants / IDE bots (GitHub Contributors counts these trailers).
s/^Co-[Aa]uthored-[Bb]y:[^\n]*(cursoragent|\@cursor\.com|\@anthropic|anthropic|noreply\@anthropic|openai\.com|\@openai\.|chatgpt|\bgemini\b|codeium|windsurf|github[^\n]*copilot|copilot\@|\bclaude\s+sonnet|\bclaude\s+opus|\bclaude\s+haiku|\bcursor\s+<)[^\n]*\n//gmi;
s/\n{3,}/\n\n/g;
print;
