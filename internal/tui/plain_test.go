package tui

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func plainTerm(in string, colors bool) (Term, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return Term{In: strings.NewReader(in), Out: &out, Err: &errb, Colors: colors}, &out, &errb
}

func TestPlainAskYN(t *testing.T) {
	cases := []struct {
		in   string
		def  bool
		want bool
	}{
		{"\n", true, true},
		{"\n", false, false},
		{"y\n", true, true},
		{"y\n", false, true},
		{"Y\n", true, true},
		{"Y\n", false, true},
		{"yes\n", true, false},
		{"yes\n", false, false},
		{"n\n", true, false},
		{"n\n", false, false},
		{"  y\t\n", false, true},
		{"  \n", true, true},
	}
	for _, c := range cases {
		for _, colors := range []bool{false, true} {
			term, out, errb := plainTerm(c.in, colors)
			got, err := term.Prompter().AskYN("Delete it?", c.def)
			if err != nil {
				t.Fatalf("%q def=%v: %v", c.in, c.def, err)
			}
			if got != c.want {
				t.Errorf("%q def=%v: got %v, want %v", c.in, c.def, got, c.want)
			}
			hint := "y/N"
			if c.def {
				hint = "Y/n"
			}
			want := "? Delete it? [" + hint + "]: "
			if colors {
				want = "\033[0;36m?\033[0m Delete it? \033[2m[" + hint + "]\033[0m: "
			}
			if errb.String() != want {
				t.Errorf("%q colors=%v: stderr = %q, want %q", c.in, colors, errb.String(), want)
			}
			if out.Len() != 0 {
				t.Errorf("stdout written: %q", out.String())
			}
		}
	}
	for _, in := range []string{"", "y"} {
		term, _, _ := plainTerm(in, false)
		if _, err := term.Prompter().AskYN("Q?", true); !errors.Is(err, ErrCancelled) {
			t.Errorf("EOF %q: err = %v, want ErrCancelled", in, err)
		}
	}
}

func TestPlainAskSecret(t *testing.T) {
	for _, colors := range []bool{false, true} {
		term, out, errb := plainTerm("  a b\\c$x  \n", colors)
		got, err := term.Prompter().AskSecret("Value for TOKEN")
		if err != nil {
			t.Fatal(err)
		}
		if got != `a b\c$x` {
			t.Errorf("got %q", got)
		}
		want := "? Value for TOKEN (hidden): \n"
		if colors {
			want = "\033[0;36m?\033[0m Value for TOKEN \033[2m(hidden)\033[0m: \n"
		}
		if errb.String() != want {
			t.Errorf("colors=%v: stderr = %q, want %q", colors, errb.String(), want)
		}
		if out.Len() != 0 {
			t.Errorf("stdout written: %q", out.String())
		}
	}
	for _, in := range []string{"", "partial"} {
		term, _, errb := plainTerm(in, false)
		if _, err := term.Prompter().AskSecret("K"); !errors.Is(err, ErrCancelled) {
			t.Errorf("EOF %q: err = %v, want ErrCancelled", in, err)
		}
		if errb.String() != "? K (hidden): " {
			t.Errorf("EOF stderr = %q", errb.String())
		}
	}
}

func TestPlainSuccessiveLines(t *testing.T) {
	term, _, _ := plainTerm("first\ny\nsecond\n", false)
	p := term.Prompter()
	a, _ := p.AskSecret("A")
	b, _ := p.AskYN("B?", false)
	// A fresh Prompter on the same Term continues where the last one stopped.
	c, _ := term.Prompter().AskSecret("C")
	if a != "first" || !b || c != "second" {
		t.Errorf("got %q %v %q", a, b, c)
	}
}

func bashPrompt(t *testing.T, script, stdin string) (stdout, stderr string) {
	t.Helper()
	core, err := filepath.Abs("../../lib/core.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `source "$1"; HAS_GUM=false; `+script, "bash", core)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	_ = cmd.Run()
	return out.String(), errb.String()
}

func TestPlainMatchesBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	for _, in := range []string{"\n", "y\n", "Y\n", "yes\n", "n\n", " y \n", ""} {
		for _, def := range []string{"y", "n"} {
			bout, berr := bashPrompt(t, `ask_yn "Q?" `+def+`; echo rc=$?`, in)
			term, _, errb := plainTerm(in, false)
			got, err := term.Prompter().AskYN("Q?", def == "y")
			if errors.Is(err, ErrCancelled) {
				term.Cancelled()
				if bout != "" || berr != errb.String() {
					t.Errorf("cancel %q def=%s: bash (%q,%q) go stderr %q", in, def, bout, berr, errb.String())
				}
				continue
			}
			rc := "rc=1\n"
			if got {
				rc = "rc=0\n"
			}
			if bout != rc || berr != errb.String() {
				t.Errorf("%q def=%s: bash (%q,%q) go (%q,%q)", in, def, bout, berr, rc, errb.String())
			}
		}
	}
	for _, in := range []string{"  a b\\c$x  \n", "plain\n", "\t-x\t\n", "'q\" \n", ""} {
		// Not inside $(...): there user_cancelled sends INT to $PPID, the test.
		bout, berr := bashPrompt(t, `ask_secret "Value for K"`, in)
		term, _, errb := plainTerm(in, false)
		got, err := term.Prompter().AskSecret("Value for K")
		if errors.Is(err, ErrCancelled) {
			term.Cancelled()
		} else {
			got += "\n"
		}
		if bout != got || berr != errb.String() {
			t.Errorf("secret %q: bash (%q,%q) go (%q,%q)", in, bout, berr, got, errb.String())
		}
	}
}
