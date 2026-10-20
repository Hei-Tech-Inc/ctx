package tui

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// bashColors sets lib/core.sh's color variables the way `[[ -t 1 ]]` would.
const bashColors = `RED='\033[0;31m' GREEN='\033[0;32m' YELLOW='\033[0;33m' CYAN='\033[0;36m' DIM='\033[2m' BOLD='\033[1m' RESET='\033[0m'; `

// bashCore runs script after sourcing lib/core.sh with gum off, as bin/ctx
// does (set -uo pipefail, INT trap exiting 130).
func bashCore(t *testing.T, script, stdin string, args ...string) (stdout, stderr string, rc int) {
	t.Helper()
	cmd := bashCoreCmd(t, script, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		rc = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), rc
}

func bashCoreCmd(t *testing.T, script string, args ...string) *exec.Cmd {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	core, err := filepath.Abs("../../lib/core.sh")
	if err != nil {
		t.Fatal(err)
	}
	return exec.Command("bash", append([]string{"-c", `set -uo pipefail; source "$1"; HAS_GUM=false; trap 'exit 130' INT; shift; ` + script, "bash", core}, args...)...)
}

type askCase struct{ prompt, def, hint string }

var askCases = []askCase{
	{"Profile name", "", ""},
	{"Git name", "Jane Doe", "Press Enter to keep"},
	{"Work dir", `C:\temp\new`, ""},
}

var askInputs = []string{"acme\n", "\n", "  sp ace \t\n", "back\\slash\n", "-n\n", "-e\n", "", "partial"}

// goAsk runs Ask and renders what the Bash script prints: the captured answer
// on stdout, or the cancel line and 130.
func goAsk(t *testing.T, c askCase, in string, colors bool) (stdout, stderr string, rc int) {
	term, out, errb := plainTerm(in, colors)
	got, err := term.Prompter().Ask(c.prompt, c.def, c.hint)
	if out.Len() != 0 {
		t.Errorf("Ask wrote stdout: %q", out.String())
	}
	if errors.Is(err, ErrCancelled) {
		return "", errb.String() + cancelLine(colors), 130
	}
	if err != nil {
		t.Fatal(err)
	}
	return "ans=" + got + "\n", errb.String(), 0
}

func cancelLine(colors bool) string {
	var b bytes.Buffer
	Term{Err: &b, Colors: colors}.Cancelled()
	return b.String()
}

const askScript = `ans=$(ask "$1" "$2" "$3"); printf 'ans=%s\n' "$ans"`

func TestPlainAskMatchesBash(t *testing.T) {
	for _, c := range askCases {
		for _, in := range askInputs {
			bout, berr, brc := bashCore(t, askScript, in, c.prompt, c.def, c.hint)
			gout, gerr, grc := goAsk(t, c, in, false)
			if bout != gout || berr != gerr || brc != grc {
				t.Errorf("%+v in %q:\nbash (%q, %q, %d)\n  go (%q, %q, %d)", c, in, bout, berr, brc, gout, gerr, grc)
			}
		}
	}
}

var pickItems = [][]string{
	{"alpha", "beta", "gamma"},
	{`C:\new`, "two words", "-n"},
	{"1", "2", "3", "4", "5", "6", "7", "eight", "9", "10"},
}

var pickInputs = []string{"1\n", "2\n", "3\n", "0\n", "4\n", "x\n", "\n", " 2 \n", "01\n", "010\n", "+1\n", "-1\n", "", "1"}

func goPick(t *testing.T, prompt string, items []string, in string, colors bool) (stdout, stderr string, rc int) {
	term, out, errb := plainTerm(in, colors)
	got, err := term.Prompter().PickOne(prompt, items)
	if out.Len() != 0 {
		t.Errorf("PickOne wrote stdout: %q", out.String())
	}
	if errors.Is(err, ErrCancelled) {
		return "", errb.String() + cancelLine(colors), 130
	}
	if err != nil {
		t.Fatal(err)
	}
	return "ans=" + got + "\n", errb.String(), 0
}

const pickScript = `p=$1; shift; ans=$(pick_one "$p" "$@"); printf 'ans=%s\n' "$ans"`

func TestPlainPickOneMatchesBash(t *testing.T) {
	for _, items := range pickItems {
		for _, in := range pickInputs {
			bout, berr, brc := bashCore(t, pickScript, in, append([]string{"Pick one"}, items...)...)
			gout, gerr, grc := goPick(t, "Pick one", items, in, false)
			if bout != gout || berr != gerr || brc != grc {
				t.Errorf("%q in %q:\nbash (%q, %q, %d)\n  go (%q, %q, %d)", items, in, bout, berr, brc, gout, gerr, grc)
			}
		}
	}
	// An empty list prints nothing and reads nothing.
	bout, berr, brc := bashCore(t, pickScript, "", "Pick one")
	gout, gerr, grc := goPick(t, "Pick one", nil, "", false)
	if bout != gout || berr != gerr || brc != grc {
		t.Errorf("empty list: bash (%q, %q, %d) go (%q, %q, %d)", bout, berr, brc, gout, gerr, grc)
	}
}

// richKeys is a rich Term whose input stays open after keys, so a slow form
// is not cancelled by richTerm's EOF (see eofCancel) before it reads them.
func richKeys(t *testing.T, keys string) Term {
	r, w := io.Pipe()
	go func() { _, _ = io.WriteString(w, keys) }()
	t.Cleanup(func() { _ = w.Close() })
	return Term{In: r, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Rich: true}
}

func TestRichAskPickOne(t *testing.T) {
	for _, c := range []struct {
		in, def, want string
	}{
		{"\r", "keep", "keep"},
		{"x\r", "keep", "keepx"},
		{"abc\r", "", "abc"},
		{"\x7f\x7f\x7f\x7f\r", "keep", "keep"}, // erased to empty: the default
		{" a \r", "", " a "},
	} {
		got, err := richKeys(t, c.in).Prompter().Ask("Name", c.def, "")
		if err != nil || got != c.want {
			t.Errorf("Ask(%q, def %q) = %q, %v; want %q", c.in, c.def, got, err, c.want)
		}
	}
	for _, in := range []string{"\x03", "abc"} {
		if _, err := richTerm(in).Prompter().Ask("Name", "d", ""); !errors.Is(err, ErrCancelled) {
			t.Errorf("Ask(%q) err = %v, want ErrCancelled", in, err)
		}
	}
	items := []string{"alpha", "beta", "gamma"}
	for in, want := range map[string]string{
		"\r":             "alpha",
		"\x1b[B\r":       "beta",
		"\x1b[B\x1b[B\r": "gamma",
		"\x1b[B\x1b[A\r": "alpha",
		"j\r":            "beta",
	} {
		got, err := richKeys(t, in).Prompter().PickOne("Pick", items)
		if err != nil || got != want {
			t.Errorf("PickOne(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"\x03", ""} {
		if _, err := richTerm(in).Prompter().PickOne("Pick", items); !errors.Is(err, ErrCancelled) {
			t.Errorf("PickOne(%q) err = %v, want ErrCancelled", in, err)
		}
	}
	// An empty list returns "" without drawing or reading anything.
	term := richTerm("")
	got, err := term.Prompter().PickOne("Pick", nil)
	if err != nil || got != "" || term.Err.(*bytes.Buffer).Len() != 0 {
		t.Errorf("PickOne(nil) = %q, %v, stderr %q", got, err, term.Err.(*bytes.Buffer).String())
	}
}

var stepCases = []struct {
	n, total   int
	title, why string
}{
	{1, 8, "Profile & Git identity", "This name is the ctx handle, SSH host alias, and default folder slug."},
	{2, 3, "SSH key", ""},
	{5, 8, `Cloud \t creds`, `Optional — a\nb`},
}

func TestStepAndReviewGolden(t *testing.T) {
	for _, colors := range []bool{false, true} {
		pre := ""
		if colors {
			pre = bashColors
		}
		for _, c := range stepCases {
			bout, berr, _ := bashCore(t, pre+`ctx_setup_step "$@"`, "", strconv.Itoa(c.n), strconv.Itoa(c.total), c.title, c.why)
			if got := Step(Term{Colors: colors}, c.n, c.total, c.title, c.why); got != bout || berr != "" {
				t.Errorf("Step%+v colors=%v:\nbash %q (stderr %q)\n  go %q", c, colors, bout, berr, got)
			}
		}
	}
	rows := [][2]string{
		{"Profile", "acme"},
		{"Git email", "—"},
		{"Work directory", "/x y/%s\\n"},
		{"A label longer than sixteen", "v"},
		{"Empty", ""},
	}
	var args []string
	for _, r := range rows {
		args = append(args, r[0], r[1])
	}
	bout, _, _ := bashCore(t, `printf '  %-16s %s\n' "$@"`, "", args...)
	if got := ReviewTable(Term{}, rows); got != bout {
		t.Errorf("ReviewTable plain:\nbash %q\n  go %q", bout, got)
	}
	if got := ReviewTable(Term{Colors: true}, rows); got != bout {
		t.Errorf("ReviewTable plain with colors = %q, want %q", got, bout)
	}

	rich := Term{Rich: true, Colors: true}
	golden(t, "step_rich", ansi256(t, Step(rich, 1, 8, "Profile & Git identity", "This name is the ctx handle.")))
	golden(t, "step_rich_nowhy", ansi256(t, Step(rich, 2, 3, "SSH key", "")))
	golden(t, "review_rich", ansi256(t, ReviewTable(rich, rows)))
}

func TestSpin(t *testing.T) {
	for _, colors := range []bool{false, true} {
		pre := ""
		if colors {
			pre = bashColors
		}
		bout, _, _ := bashCore(t, pre+`spin "Fetching keys" echo ran`, "")
		var out, errb bytes.Buffer
		Spin(Term{Out: &out, Err: &errb, Colors: colors}, "Fetching keys", func() { out.WriteString("ran\n") })
		if out.String() != bout || errb.Len() != 0 {
			t.Errorf("plain colors=%v: out %q err %q, want out %q", colors, out.String(), errb.String(), bout)
		}
	}

	var out, errb bytes.Buffer
	ran := false
	Spin(Term{Out: &out, Err: &errb, Rich: true, Colors: true}, "Fetching keys", func() {
		time.Sleep(250 * time.Millisecond)
		ran = true
	})
	s := errb.String()
	if !ran || out.Len() != 0 {
		t.Fatalf("ran=%v out=%q", ran, out.String())
	}
	if !strings.Contains(s, "Fetching keys") || !strings.Contains(s, "⣾") {
		t.Errorf("rich spinner did not draw: %q", s)
	}
	if !strings.HasSuffix(s, "\r\x1b[K\x1b[?25h") {
		t.Errorf("rich spinner did not clear its line: %q", s)
	}
}
