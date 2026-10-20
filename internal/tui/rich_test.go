package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func richTerm(in string) Term {
	return Term{In: strings.NewReader(in), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Rich: true}
}

func TestRichPrompts(t *testing.T) {
	got, err := richTerm("s3cret\r").Prompter().AskSecret("Value for K")
	if err != nil || got != "s3cret" {
		t.Errorf("AskSecret = %q, %v", got, err)
	}
	// Rich input keeps exactly what was typed, surrounding spaces included.
	got, err = richTerm(" a b \r").Prompter().AskSecret("Value for K")
	if err != nil || got != " a b " {
		t.Errorf("AskSecret spaces = %q, %v", got, err)
	}
	for _, c := range []struct {
		in        string
		def, want bool
	}{
		{"\r", true, true},
		{"\r", false, false},
		{"y", false, true},
		{"n", true, false},
		{"\x1b[D\r", false, true}, // left arrow: from the focused "No" to "Yes"
	} {
		got, err := richTerm(c.in).Prompter().AskYN("Delete it?", c.def)
		if err != nil || got != c.want {
			t.Errorf("AskYN(%q, def=%v) = %v, %v; want %v", c.in, c.def, got, err, c.want)
		}
	}
	if _, err := richTerm("\x03").Prompter().AskSecret("K"); !errors.Is(err, ErrCancelled) {
		t.Errorf("AskSecret ctrl+c err = %v", err)
	}
	if _, err := richTerm("\x03").Prompter().AskYN("Q?", true); !errors.Is(err, ErrCancelled) {
		t.Errorf("AskYN ctrl+c err = %v", err)
	}
	// EOF on non-terminal input cancels instead of hanging.
	if _, err := richTerm("abc").Prompter().AskSecret("K"); !errors.Is(err, ErrCancelled) {
		t.Errorf("AskSecret EOF err = %v", err)
	}
	if _, err := richTerm("").Prompter().AskYN("Q?", true); !errors.Is(err, ErrCancelled) {
		t.Errorf("AskYN EOF err = %v", err)
	}
}
