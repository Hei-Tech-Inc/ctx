package tui

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

// ansi256 renders s the way a 256-color terminal would receive it.
func ansi256(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	w := &colorprofile.Writer{Forward: &b, Profile: colorprofile.ANSI256}
	if _, err := w.WriteString(s); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n got: %q\nwant: %q", name, got, want)
	}
}

func TestHeaderGolden(t *testing.T) {
	golden(t, "header_rich", ansi256(t, Header(Term{Rich: true, Colors: true}, "ctx status")))
	if got := Header(Term{}, "ctx status"); got != "\n  ctx status\n\n" {
		t.Errorf("plain header = %q", got)
	}
	if got := Header(Term{Colors: true}, "ctx status"); got != "\033[1m\n  ctx status\n\033[0m\n" {
		t.Errorf("plain color header = %q", got)
	}
}

func TestBoxGolden(t *testing.T) {
	golden(t, "box", ansi256(t, Box([]string{"Active context", "acme"})))
	golden(t, "accent", ansi256(t, Accent(78, "Active context")))
}
