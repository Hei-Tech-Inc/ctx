package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

const (
	agentRulesBegin = "<!-- BEGIN ctx agent rules -->"
	agentRulesEnd   = "<!-- END ctx agent rules -->"
)

// agentRulesText is _ctx_agent_rules_text: the marked block, ending in a newline.
const agentRulesText = agentRulesBegin + "\n" + `## Client identity (ctx)

This machine uses ` + "`ctx`" + ` to keep each client's git, GitHub, SSH and cloud identity separate.

- Check which identity applies here: ` + "`ctx status --json`, or `ctx env`" + ` (prints the variables).
- Run commands that need this client's credentials through ` + "`ctx run -- <command>`" + `
  (for example ` + "`ctx run -- gh pr create` or `ctx run -- terraform plan`" + `).
- Do not change ` + "`git config user.email`" + `, rewrite remote URLs, or run ` + "`gh auth switch`" + `.
- If a commit or push is blocked by "ctx guard", stop and report it. Do not use ` + "`--no-verify`" + `.
` + agentRulesEnd + "\n"

// AgentRules runs `ctx agent-rules [--write [FILE]]`, mirroring cmd_agent_rules
// in lib/cmd_ops.sh. Without --write it prints the block. With --write it
// upserts the block into FILE (default: AGENTS.md at the git top level, else
// the current directory), in place: symlinks and mode stay, a new file gets the
// umask default. An unterminated BEGIN, an unreadable file or a directory is
// an error and nothing changes. --dry-run (CTX_DRY_RUN=1) only says what it would do.
// Like Bash it looks only at the first argument and at the second as the file.
// Neither this nor edit prompts, so there is no interrupt handler to install.
func AgentRules(t tui.Term, args []string) int {
	if len(args) == 0 || args[0] != "--write" {
		_, _ = io.WriteString(t.Out, agentRulesText)
		return 0
	}
	u := termUI(t)
	file := ""
	if len(args) > 1 {
		file = args[1]
	}
	if file == "" {
		top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		dir := strings.TrimRight(string(top), "\n")
		if err != nil || dir == "" {
			// $(git ... || pwd): git's stdout is kept only on success.
			dir, _ = os.Getwd()
		}
		file = dir + "/AGENTS.md"
	}
	if os.Getenv("CTX_DRY_RUN") == "1" {
		u.info("[dry-run] Would write ctx agent rules to " + file)
		return 0
	}
	if strings.HasPrefix(file, "-") {
		file = "./" + file
	}
	var buf bytes.Buffer
	if st, err := os.Stat(file); err == nil {
		if st.IsDir() {
			return u.die(file + " is a directory")
		}
		data, err := os.ReadFile(file)
		if err != nil || !st.Mode().IsRegular() {
			return u.die("Cannot read " + file)
		}
		kept, ok := stripAgentRules(data)
		if !ok {
			return u.die("Unterminated ctx block in " + file + "; add the END marker or remove the BEGIN line")
		}
		buf.Write(kept)
		// A non-empty file whose last line is not empty gets a blank line.
		if b := buf.Bytes(); len(b) > 0 && !bytes.Equal(b, []byte("\n")) && !bytes.HasSuffix(b, []byte("\n\n")) {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString(agentRulesText)
	// Write through: symlinks and the file's mode stay; a new file gets the umask default.
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err == nil {
		_, err = f.Write(buf.Bytes())
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return u.die("Cannot write " + file)
	}
	u.success("ctx agent rules written to " + file)
	return 0
}

// stripAgentRules copies data's lines except the BEGIN..END block (marker
// lines included). A lone END line is kept. ok is false when a BEGIN has no
// later END. Every kept line ends in a newline; a missing one is added.
func stripAgentRules(data []byte) (out []byte, ok bool) {
	var b bytes.Buffer
	skip := false
	s := string(data)
	for len(s) > 0 {
		line, rest, _ := strings.Cut(s, "\n")
		s = rest
		switch {
		case line == agentRulesBegin:
			skip = true
		case skip:
			if line == agentRulesEnd {
				skip = false
			}
		default:
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.Bytes(), !skip
}
