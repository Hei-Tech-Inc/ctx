package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

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
// the current directory); --dry-run (CTX_DRY_RUN=1) only says what it would do.
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
	tmp, err := mktemp(file + ".XXXXXX")
	if err != nil {
		_, _ = fmt.Fprintln(t.Err, mktempError(file+".XXXXXX", err))
		return u.die("Cannot write next to " + file)
	}
	var buf bytes.Buffer
	if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() {
		// An unreadable file reads as empty, as with the failed redirect in
		// Bash: the block then replaces it.
		if data, err := os.ReadFile(file); err == nil {
			buf.Write(stripAgentRules(data))
		}
		// A non-empty file whose last line is not empty gets a blank line.
		if b := buf.Bytes(); len(b) > 0 && !bytes.Equal(b, []byte("\n")) && !bytes.HasSuffix(b, []byte("\n\n")) {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString(agentRulesText)
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		u.error("write " + tmp + ": " + err.Error())
	}
	dest := file
	if st, err := os.Stat(file); err == nil && st.IsDir() {
		dest = filepath.Join(file, filepath.Base(tmp)) // mv into a directory
	}
	if err := os.Rename(tmp, dest); err != nil {
		u.error("mv: " + err.Error()) // Bash goes on to report success
	}
	u.success("ctx agent rules written to " + file)
	return 0
}

// stripAgentRules copies data's lines except the BEGIN..END block (the marker
// lines included; an END alone is dropped too, an unterminated BEGIN drops the
// rest). Every kept line ends in a newline, a missing one is added.
func stripAgentRules(data []byte) []byte {
	var out bytes.Buffer
	skip := false
	s := string(data)
	for len(s) > 0 {
		line, rest, _ := strings.Cut(s, "\n")
		s = rest
		switch {
		case line == agentRulesBegin:
			skip = true
		case line == agentRulesEnd:
			skip = false
		case !skip:
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

var errMktempOption = errors.New("invalid option")

// mktemp creates a file for template (ending in XXXXXX) with mode 0600.
func mktemp(template string) (string, error) {
	if strings.HasPrefix(template, "-") { // mktemp(1) reads it as an option
		return "", errMktempOption
	}
	prefix := strings.TrimSuffix(template, "XXXXXX")
	dir, pat := filepath.Split(prefix)
	f, err := os.CreateTemp(dirOrDot(dir), pat+"*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_ = f.Close()
	if dir == "" { // CreateTemp(".") returns "./name"
		name = filepath.Base(name)
	}
	return name, nil
}

func dirOrDot(d string) string {
	if d == "" {
		return "."
	}
	return d
}

// mktempError is the message mktemp(1) prints on this platform when it fails.
func mktempError(template string, err error) string {
	if err == errMktempOption {
		opt := template[1:2]
		if runtime.GOOS == "darwin" {
			return "mktemp: invalid option -- " + opt + "\nusage: mktemp [-d] [-p tmpdir] [-q] [-t prefix] [-u] template ...\n       mktemp [-d] [-p tmpdir] [-q] [-u] -t prefix "
		}
		return "mktemp: invalid option -- '" + opt + "'\nTry 'mktemp --help' for more information."
	}
	reason := err.Error()
	var pe *os.PathError
	if errors.As(err, &pe) {
		var en syscall.Errno
		if errors.As(pe.Err, &en) {
			reason = en.Error()
			if reason != "" {
				reason = strings.ToUpper(reason[:1]) + reason[1:]
			}
		}
	}
	if runtime.GOOS == "darwin" {
		return "mktemp: mkstemp failed on " + template + ": " + reason
	}
	return "mktemp: failed to create file via template ‘" + template + "’: " + reason
}
