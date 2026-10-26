package shquote

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
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
		"a=~b", "a:~b", "=~", ":~x", "/p:~/x", "KUBECONFIG=~/.kube/a:~/.kube/b", "a~=b", "x=y~", "~/a:~/b", "a=~", "~=~:~",
	}
	for c := 1; c < 128; c++ {
		v = append(v, "a"+string(rune(c))+"b", string(rune(c)))
	}
	return v
}

// bashVersion returns bash's major and minor version.
func bashVersion(t *testing.T, bash string) (int, int) {
	t.Helper()
	out, err := exec.Command(bash, "-c", `echo "${BASH_VERSINFO[0]} ${BASH_VERSINFO[1]}"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	var major, minor int
	if _, err := fmt.Sscan(string(out), &major, &minor); err != nil {
		t.Fatal(err)
	}
	return major, minor
}

// unquoteTilde drops the backslash Quote puts before a tilde that follows
// = or : (what bash before 5.3 may leave unquoted).
func unquoteTilde(q string) string {
	return strings.NewReplacer(`=\~`, "=~", `:\~`, ":~").Replace(q)
}

func TestQuoteMatchesBashPrintfQ(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	major, minor := bashVersion(t, bash)
	if major < 4 {
		t.Skipf("bash on PATH is %d.%d; Quote follows bash 4+", major, minor)
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
				got := Quote(s)
				if got == want[i] {
					continue
				}
				// R21: bash < 5.3 may not quote ~ after = or :; Quote always
				// does (eval-safe). Accept exactly that difference there.
				if (major < 5 || major == 5 && minor < 3) && unquoteTilde(got) == want[i] {
					continue
				}
				t.Errorf("Quote(%q) = %q, bash %d.%d printf %%q = %q", s, got, major, minor, want[i])
			}
		})
	}
}

// TestQuoteEvalRoundTrip: X=<Quote(v)> assigns exactly v in bash and zsh
// (with HOME pointing elsewhere, so a stray tilde expansion would show).
func TestQuoteEvalRoundTrip(t *testing.T) {
	vals := quoteValues()
	for _, sh := range []string{"bash", "zsh"} {
		path, err := exec.LookPath(sh)
		if err != nil {
			testutil.ShellMissing(t, sh)
			continue
		}
		for _, loc := range []string{"C", "en_US.UTF-8"} {
			t.Run(sh+"/"+loc, func(t *testing.T) {
				t.Setenv("LC_ALL", loc)
				var script strings.Builder
				script.WriteString("set -o magicequalsubst 2>/dev/null\n")
				vals := vals
				if sh == "zsh" {
					// zsh expands a word starting with = (EQUALS: =cmd is
					// the path of cmd) even in an assignment value. bash
					// printf %q does not guard against it, nor does Quote.
					vals = nil
					for _, v := range quoteValues() {
						if !strings.HasPrefix(v, "=") {
							vals = append(vals, v)
						}
					}
				}
				for _, v := range vals {
					script.WriteString("X=" + Quote(v) + "; printf '%s\\0' \"$X\"\n")
				}
				cmd := exec.Command(path, "-c", script.String())
				cmd.Env = append(os.Environ(), "HOME=/HOMEX", "LC_ALL="+loc)
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("%s: %v", sh, err)
				}
				got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
				if len(got) != len(vals) {
					t.Fatalf("%s returned %d values for %d inputs", sh, len(got), len(vals))
				}
				for i, v := range vals {
					if v == "" && got[i] == "" {
						continue
					}
					if got[i] != v {
						t.Errorf("%s: X=%s gives %q, want %q", sh, Quote(v), got[i], v)
					}
				}
			})
		}
	}
}
