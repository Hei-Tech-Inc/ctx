// Package tui is ctx-core's terminal UI: prompts and styled output.
//
// Plain mode reproduces lib/core.sh run with --no-gum byte for byte. Rich mode
// uses Charm's huh and lipgloss in place of gum, with the same wording.
package tui

import (
	"errors"
	"io"
	"os"

	"golang.org/x/term"
)

// ANSI codes from lib/core.sh, used only when Term.Colors is set.
const (
	cyan   = "\033[0;36m"
	yellow = "\033[0;33m"
	dim    = "\033[2m"
	bold   = "\033[1m"
	reset  = "\033[0m"
)

// accent is the gum flag color used across lib/core.sh (foreground 99).
const accent = 99

// ErrCancelled is returned when the user aborts a prompt (Ctrl+C or EOF).
var ErrCancelled = errors.New("cancelled")

// isTerminal reports whether fd is a terminal. Tests replace it.
var isTerminal = func(fd uintptr) bool { return term.IsTerminal(int(fd)) }

// Term holds the streams a command talks through and how to draw on them.
type Term struct {
	In       io.Reader
	Out, Err io.Writer
	// Rich selects the Charm UI; otherwise prompts match Bash's plain fallback.
	Rich bool
	// Colors enables ANSI codes in plain output (Bash's [[ -t 1 ]]).
	Colors bool
}

// FromOS returns the process's Term. It is rich exactly when stdin and stdout
// are terminals and CTX_NO_GUM is not 1; colors follow stdout alone.
func FromOS() Term {
	inTTY := isTerminal(os.Stdin.Fd())
	outTTY := isTerminal(os.Stdout.Fd())
	return Term{
		In:     os.Stdin,
		Out:    os.Stdout,
		Err:    os.Stderr,
		Rich:   inTTY && outTTY && os.Getenv("CTX_NO_GUM") != "1",
		Colors: outTTY,
	}
}

// Prompter asks the user for input.
type Prompter interface {
	// AskSecret reads a value without echoing it.
	AskSecret(prompt string) (string, error)
	// AskYN asks a yes/no question; def is the answer for a bare Enter.
	AskYN(prompt string, def bool) (bool, error)
}

// Prompter returns the rich or plain prompter for t.
func (t Term) Prompter() Prompter {
	if t.Rich {
		return rich{t}
	}
	return plain{t}
}

// Cancelled prints Bash's user_cancelled line to t.Err and returns its exit code.
func (t Term) Cancelled() int {
	mark := "!"
	if t.Colors {
		mark = yellow + "!" + reset
	}
	_, _ = io.WriteString(t.Err, mark+" Cancelled by user.\n")
	return 130
}

// c returns code when colors are on, else "".
func (t Term) c(code string) string {
	if t.Colors {
		return code
	}
	return ""
}
