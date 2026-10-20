package tui

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// plain mirrors the non-gum branches of ask_secret and ask_yn in lib/core.sh:
// prompts go to stderr, and lines are read the way `read -r` reads them.
type plain struct{ t Term }

func (p plain) AskSecret(prompt string) (string, error) {
	t := p.t
	_, _ = io.WriteString(t.Err, t.c(cyan)+"?"+t.c(reset)+" "+prompt+" "+t.c(dim)+"(hidden)"+t.c(reset)+": ")
	line, err := p.readLine(true)
	if err != nil {
		return "", err
	}
	_, _ = io.WriteString(t.Err, "\n")
	return line, nil
}

func (p plain) AskYN(prompt string, def bool) (bool, error) {
	t := p.t
	hint, defAns := "y/N", "n"
	if def {
		hint, defAns = "Y/n", "y"
	}
	_, _ = io.WriteString(t.Err, t.c(cyan)+"?"+t.c(reset)+" "+prompt+" "+t.c(dim)+"["+hint+"]"+t.c(reset)+": ")
	ans, err := p.readLine(false)
	if err != nil {
		return false, err
	}
	if ans == "" {
		ans = defAns
	}
	return ans == "y" || ans == "Y", nil
}

// readLine reads one line like `read -r` (or `read -rs` when hidden): the line
// without its newline, trimmed of surrounding spaces and tabs (default IFS),
// with backslashes kept. EOF before a newline is ErrCancelled, as `read` fails
// then too.
//
// Input is read a byte at a time, as bash does on pipes, so nothing past the
// newline is consumed: successive prompts, from one Prompter or several, read
// successive lines.
func (p plain) readLine(hidden bool) (string, error) {
	if f, ok := p.t.In.(*os.File); ok && hidden && isTerminal(f.Fd()) {
		b, err := term.ReadPassword(int(f.Fd()))
		if err != nil {
			return "", ErrCancelled
		}
		return strings.Trim(string(b), " \t"), nil
	}
	var sb strings.Builder
	var buf [1]byte
	for {
		n, err := p.t.In.Read(buf[:])
		if n == 1 {
			if buf[0] == '\n' {
				return strings.Trim(sb.String(), " \t"), nil
			}
			sb.WriteByte(buf[0])
			continue
		}
		if err != nil {
			return "", ErrCancelled
		}
	}
}
