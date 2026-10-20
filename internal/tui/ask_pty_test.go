//go:build !windows

package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// On a pty, Bash's stdout is a terminal, so lib/core.sh turns its colors on.

func TestPlainAskMatchesBashColors(t *testing.T) {
	for _, c := range askCases {
		for _, ans := range []string{"acme", "", "EOF"} {
			bt, brc := testutil.RunPTY(t, bashCoreCmd(t, askScript, c.prompt, c.def, c.hint), []string{ans}, 10*time.Second)
			in := ans + "\n"
			if ans == "EOF" {
				in = ""
			}
			gout, gerr, grc := goAsk(t, c, in, true)
			if bt != gerr+gout || brc != grc {
				t.Errorf("%+v answer %q:\nbash (%q, %d)\n  go (%q, %d)", c, ans, bt, brc, gerr+gout, grc)
			}
		}
	}
}

func TestPlainPickOneMatchesBashColors(t *testing.T) {
	items := pickItems[1]
	for _, ans := range []string{"1", "0", "EOF"} {
		bt, brc := testutil.RunPTY(t, bashCoreCmd(t, pickScript, append([]string{"Pick one"}, items...)...), []string{ans}, 10*time.Second)
		in := ans + "\n"
		if ans == "EOF" {
			in = ""
		}
		gout, gerr, grc := goPick(t, "Pick one", items, in, true)
		if bt != gerr+gout || brc != grc {
			t.Errorf("answer %q:\nbash (%q, %d)\n  go (%q, %d)", ans, bt, brc, gerr+gout, grc)
		}
	}
}

// TestRichOnPTY drives the rich prompts on a real terminal: the test binary
// re-runs itself as the helper below.
func TestRichOnPTY(t *testing.T) {
	run := func(mode string, answers ...string) (string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRichHelper$")
		cmd.Env = append(os.Environ(), "CTX_TUI_HELPER="+mode, "TERM=xterm-256color")
		return testutil.RunPTY(t, cmd, answers, 20*time.Second)
	}
	out, rc := run("pick", "j")
	if rc != 0 || !strings.Contains(out, "Pick one (↑/↓ move, Enter picks, Ctrl+C cancel)") || !strings.HasSuffix(out, "result=beta\n") {
		t.Errorf("pick: rc %d, transcript %q", rc, out)
	}
	out, rc = run("ask", "x")
	if rc != 0 || !strings.Contains(out, "Git name") || !strings.HasSuffix(out, "result=Janex\n") {
		t.Errorf("ask: rc %d, transcript %q", rc, out)
	}
	out, rc = run("ask", "\x03")
	if rc != 130 || !strings.HasSuffix(out, "! Cancelled by user.\n") {
		t.Errorf("ask ctrl+c: rc %d, transcript %q", rc, out)
	}
}

func TestRichHelper(t *testing.T) {
	mode := os.Getenv("CTX_TUI_HELPER")
	if mode == "" {
		t.Skip("helper for TestRichOnPTY")
	}
	term := Term{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Rich: true, Colors: true}
	var got string
	var err error
	switch mode {
	case "pick":
		got, err = term.Prompter().PickOne("Pick one", []string{"alpha", "beta", "gamma"})
	case "ask":
		got, err = term.Prompter().Ask("Git name", "Jane", "")
	}
	if err != nil {
		os.Exit(Term{Err: os.Stderr}.Cancelled())
	}
	fmt.Printf("result=%s\n", got)
	os.Exit(0)
}
