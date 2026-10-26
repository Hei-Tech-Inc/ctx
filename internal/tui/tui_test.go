package tui

import (
	"bytes"
	"os"
	"testing"
)

func TestRichDetection(t *testing.T) {
	cases := []struct {
		name          string
		inTTY, outTTY bool
		noGum         string
		rich          bool
	}{
		{"both tty", true, true, "", true},
		{"both tty no-gum", true, true, "1", false},
		{"both tty no-gum other value", true, true, "0", true},
		{"stdin piped", false, true, "", false},
		{"stdout piped", true, false, "", false},
		{"neither", false, false, "", false},
		{"stdin piped no-gum", false, true, "1", false},
		{"stdout piped no-gum", true, false, "1", false},
		{"neither no-gum", false, false, "1", false},
	}
	orig := isTerminal
	t.Cleanup(func() { isTerminal = orig })
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isTerminal = func(fd uintptr) bool {
				switch fd {
				case os.Stdin.Fd():
					return c.inTTY
				case os.Stdout.Fd():
					return c.outTTY
				}
				return false
			}
			t.Setenv("CTX_NO_GUM", c.noGum)
			term := FromOS()
			if term.Rich != c.rich {
				t.Errorf("Rich = %v, want %v", term.Rich, c.rich)
			}
			if term.Colors != c.outTTY {
				t.Errorf("Colors = %v, want %v", term.Colors, c.outTTY)
			}
			if term.In != os.Stdin || term.Out != os.Stdout || term.Err != os.Stderr {
				t.Error("streams are not the os files")
			}
		})
	}
}

func TestCancelled(t *testing.T) {
	for _, colors := range []bool{false, true} {
		var errb bytes.Buffer
		rc := Term{Err: &errb, Colors: colors}.Cancelled()
		if rc != 130 {
			t.Errorf("rc = %d, want 130", rc)
		}
		want := "! Cancelled by user.\n"
		if colors {
			want = "\033[0;33m!\033[0m Cancelled by user.\n"
		}
		if errb.String() != want {
			t.Errorf("colors=%v: stderr = %q, want %q", colors, errb.String(), want)
		}
	}
}
