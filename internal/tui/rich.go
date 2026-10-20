package tui

import (
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// rich draws prompts with huh in place of the gum calls in lib/core.sh.
type rich struct{ t Term }

func (r rich) AskSecret(prompt string) (string, error) {
	t := r.t
	_, _ = io.WriteString(t.Err, t.c(dim)+"  Waiting for secret input — press Enter to confirm (Ctrl+C to cancel)."+t.c(reset)+"\n")
	var v string
	in := huh.NewInput().
		EchoMode(huh.EchoModePassword).
		Placeholder(prompt).
		Prompt("> ").
		Value(&v)
	if err := r.run(huh.NewForm(huh.NewGroup(in)).WithWidth(60)); err != nil {
		return "", err
	}
	return v, nil
}

func (r rich) AskYN(prompt string, def bool) (bool, error) {
	// ask_yn swaps the labels when the default is "n" so the focused, left
	// button is the default. Here the focused button is always "affirmative";
	// when the labels are swapped, so are the y/n keys, and the answer is
	// inverted back.
	affirm, deny := "Yes", "No"
	km := huh.NewDefaultKeyMap()
	if !def {
		affirm, deny = "No", "Yes"
		km.Confirm.Accept, km.Confirm.Reject = km.Confirm.Reject, km.Confirm.Accept
	}
	v := true
	c := huh.NewConfirm().
		Title(prompt + " (Ctrl+C to cancel)").
		Affirmative(affirm).
		Negative(deny).
		Value(&v)
	if err := r.run(huh.NewForm(huh.NewGroup(c)).WithKeyMap(km)); err != nil {
		return false, err
	}
	if !def {
		return !v, nil
	}
	return v, nil
}

func (r rich) run(f *huh.Form) error {
	err := f.WithTheme(huh.ThemeFunc(theme)).
		WithShowHelp(false).
		WithInput(r.input()).
		WithOutput(r.t.Err).
		Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}

// input is t.In, except that input which is not a terminal (where bubbletea
// would wait forever at EOF) sends Ctrl+C shortly after EOF, so the form is
// cancelled unless the keys before EOF already finished it. FromOS never makes
// a rich Term without a terminal on stdin; this keeps other callers and tests
// from hanging.
func (r rich) input() io.Reader {
	if f, ok := r.t.In.(*os.File); ok && isTerminal(f.Fd()) {
		return f
	}
	return &eofCancel{r: r.t.In}
}

// eofGrace lets bubbletea handle the keys read before EOF.
const eofGrace = 250 * time.Millisecond

type eofCancel struct {
	r    io.Reader
	sent bool
}

func (e *eofCancel) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if n == 0 && errors.Is(err, io.EOF) && !e.sent && len(p) > 0 {
		e.sent = true
		time.Sleep(eofGrace)
		p[0] = 0x03 // Ctrl+C
		return 1, nil
	}
	return n, err
}

// theme is huh's Charm theme with the gum accent color (99) on titles,
// prompts and cursors.
func theme(isDark bool) *huh.Styles {
	s := huh.ThemeCharm(isDark)
	c := lipgloss.Color(strconv.Itoa(accent))
	s.Focused.Title = s.Focused.Title.Foreground(c)
	s.Focused.TextInput.Prompt = s.Focused.TextInput.Prompt.Foreground(c)
	s.Focused.TextInput.Cursor = s.Focused.TextInput.Cursor.Foreground(c)
	s.Blurred = s.Focused
	s.Blurred.Base = s.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	return s
}
