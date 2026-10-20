package shquote

import (
	"os/exec"
	"strings"
	"testing"
)

// quoteValues covers every ASCII byte class, leading-only escapes, UTF-8 and
// invalid bytes. NUL is left out: Bash strings cannot hold it.
func quoteValues() []string {
	v := []string{
		"", "plain", "a b", "Jane Doe", "~", "~a", "a~", "a~b", "#a", "a#b", "a,b", "=a", "a=b",
		"-n", "-e", `a\b`, `a"b`, "a'b", "x$y", "a`b`", "a!b", "a^b", "a{b}", "a*b", "a?b", "a[b]",
		"a|b", "a&b", "a;b", "a(b)", "a<b>", "a%b+c@d:e/f.g_h-i", "é", "ctx-é", "日本",
		"a\tb", "a\nb", "a\rb", "\a\b\f\v\x1bx", "a\x01b c", "\x7f", "a'b\nc", "é\tx",
		"\xff", "\xff a", "\xc3", "a b\xff", `~Jane #1, "Q" \ $HOME`, "/Users/x y/.ssh/id",
	}
	for c := 1; c < 128; c++ {
		v = append(v, "a"+string(rune(c))+"b", string(rune(c)))
	}
	return v
}

func TestQuoteMatchesBashPrintfQ(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	ver, _ := exec.Command(bash, "-c", `echo "${BASH_VERSINFO[0]}"`).Output()
	if strings.TrimSpace(string(ver)) < "4" {
		t.Skipf("bash on PATH is %s; Quote follows bash 4+", ver)
	}
	vals := quoteValues()
	for _, loc := range []string{"C", "en_US.UTF-8"} {
		t.Run(loc, func(t *testing.T) {
			t.Setenv("LC_ALL", loc)
			cmd := exec.Command(bash, append([]string{"-c", `for s in "$@"; do printf '%q\0' "$s"; done`, "_"}, vals...)...)
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
			if len(want) != len(vals) {
				t.Fatalf("bash returned %d values for %d inputs", len(want), len(vals))
			}
			for i, s := range vals {
				if got := Quote(s); got != want[i] {
					t.Errorf("Quote(%q) = %q, bash printf %%q = %q", s, got, want[i])
				}
			}
		})
	}
}
