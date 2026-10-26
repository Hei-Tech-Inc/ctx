package tui

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// plain mirrors the non-gum branches of ask, ask_secret, ask_yn and pick_one in
// lib/core.sh: prompts go to stderr, and lines are read the way `read -r`
// reads them.
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

// Ask mirrors ask's plain branch. hint is the rich placeholder and is not shown
// here. The answer is what `$(ask …)` captures (see echoed).
func (p plain) Ask(prompt, def, hint string) (string, error) {
	t := p.t
	s := t.c(cyan) + "?" + t.c(reset) + " " + prompt
	if def != "" {
		s += " " + t.c(dim) + "[" + def + "]" + t.c(reset)
	}
	_, _ = io.WriteString(t.Err, EchoE(s+`: \c`))
	ans, err := p.readLine(false)
	if err != nil {
		return "", err
	}
	if ans == "" {
		ans = def
	}
	return echoed(ans), nil
}

// PickOne mirrors pick_one's plain branch: a numbered menu, then a choice read
// as a line. Anything but a number from 1 to len(items) (in bash arithmetic,
// so "010" is octal 8) picks nothing and returns "".
func (p plain) PickOne(prompt string, items []string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	t := p.t
	var b strings.Builder
	b.WriteString("\n")
	for i, it := range items {
		b.WriteString(EchoE("  " + t.c(dim) + strconv.Itoa(i+1) + t.c(reset) + "  " + it))
	}
	b.WriteString("\n")
	b.WriteString(EchoE(t.c(cyan) + "?" + t.c(reset) + " " + prompt + " (1-" + strconv.Itoa(len(items)) + `, 0 to skip): \c`))
	_, _ = io.WriteString(t.Err, b.String())
	choice, err := p.readLine(false)
	if err != nil {
		return "", err
	}
	n, ok := bashNumber(choice)
	if !ok || n < 1 || n > int64(len(items)) {
		return "", nil
	}
	return echoed(items[n-1]), nil
}

// bashNumber evaluates s, which must match ^[0-9]+$, the way [[ s -ge 1 ]]
// does: a leading 0 means octal, and overflow wraps. An invalid octal digit is
// an error in bash (which also prints a diagnostic Go does not mirror).
func bashNumber(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	base := int64(10)
	if len(s) > 1 && s[0] == '0' {
		base = 8
	}
	var n int64
	for i := 0; i < len(s); i++ {
		d := int64(s[i]) - '0'
		if d < 0 || d > 9 || d >= base {
			return 0, false
		}
		n = n*base + d
	}
	return n, true
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
		line, err := readSecretTTY(int(f.Fd()), f)
		if !errors.Is(err, errNoTermios) {
			return line, err
		}
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
