package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestReadHidden(t *testing.T) {
	cases := []struct {
		in, want string
		cancel   bool
	}{
		{in: "s3cret\n", want: "s3cret"},
		{in: "s3cret\r", want: "s3cret"},
		{in: "  a b\\c$x  \r", want: `a b\c$x`},
		{in: "abd\x7fc\n", want: "abc"},    // DEL erases
		{in: "ab\x08\x08xy\n", want: "xy"}, // BS erases
		{in: "\x7fok\n", want: "ok"},       // erase on empty line is a no-op
		{in: "é\x7fe\n", want: "e"},        // erase removes a whole rune
		{in: "a\x04b\n", want: "ab"},       // Ctrl+D mid-line is ignored
		{in: "\x03", cancel: true},
		{in: "abc\x03def\n", cancel: true},
		{in: "\x04", cancel: true},
		{in: "abc", cancel: true}, // EOF before Enter
		{in: "", cancel: true},
	}
	for _, c := range cases {
		got, err := readHidden(strings.NewReader(c.in))
		if c.cancel {
			if !errors.Is(err, ErrCancelled) {
				t.Errorf("%q: err = %v, want ErrCancelled", c.in, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestReadSecretTTYRestores(t *testing.T) {
	origGet, origSet := ttyGet, ttySet
	t.Cleanup(func() { ttyGet, ttySet = origGet, origSet })
	for _, c := range []struct {
		in     string
		cancel bool
	}{{"pw\r", false}, {"p\x03", true}, {"\x04", true}, {"", true}} {
		old := &ttyState{}
		var sets []*ttyState
		ttyGet = func(int) (*ttyState, error) { return old, nil }
		ttySet = func(_ int, s *ttyState) error { sets = append(sets, s); return nil }
		got, err := readSecretTTY(7, strings.NewReader(c.in))
		if c.cancel != errors.Is(err, ErrCancelled) || (!c.cancel && got != "pw") {
			t.Errorf("%q: got %q, %v", c.in, got, err)
		}
		if len(sets) != 2 || sets[0] == old || sets[1] != old {
			t.Errorf("%q: want set(no-echo) then set(original), got %v", c.in, sets)
		}
		if !noEchoApplied(sets[0]) {
			t.Errorf("%q: first set does not clear echo/canonical/signals", c.in)
		}
	}
}

// SaveTTY's restore puts back the state read when it was called, even after a
// hidden read turned echo off, and is a no-op without a terminal.
func TestSaveTTYRestores(t *testing.T) {
	origGet, origSet := ttyGet, ttySet
	t.Cleanup(func() { ttyGet, ttySet = origGet, origSet })
	saved := &ttyState{}
	var sets []*ttyState
	ttyGet = func(int) (*ttyState, error) { return saved, nil }
	ttySet = func(_ int, s *ttyState) error { sets = append(sets, s); return nil }
	restore := saveTTYFd(7)
	restore()
	if len(sets) != 1 || sets[0] != saved {
		t.Errorf("want one set(saved), got %v", sets)
	}
	ttyGet = func(int) (*ttyState, error) { return nil, errNoTermios }
	sets = nil
	saveTTYFd(7)()
	if len(sets) != 0 {
		t.Errorf("no terminal: want no set, got %v", sets)
	}
	SaveTTY(strings.NewReader(""))() // not a terminal: a no-op
}
