package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

// ttyColors reports whether w is a terminal; Bash colors only then (`[[ -t 1 ]]`).
func ttyColors(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(st, null) {
		return false
	}
	return true
}

// echoE returns what `echo -e` prints for s, newline included. A \c in s stops
// all output, including the newline.
func echoE(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch d := s[i]; d {
		case 'a':
			b.WriteByte(7)
		case 'b':
			b.WriteByte(8)
		case 'c':
			return b.String()
		case 'e', 'E':
			b.WriteByte(27)
		case 'f':
			b.WriteByte(12)
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte(11)
		case '\\':
			b.WriteByte('\\')
		case '0': // \0nnn: up to three octal digits
			n, j := 0, i+1
			for ; j < len(s) && j < i+4 && s[j] >= '0' && s[j] <= '7'; j++ {
				n = n*8 + int(s[j]-'0')
			}
			b.WriteByte(byte(n))
			i = j - 1
		case 'x', 'u', 'U':
			max := map[byte]int{'x': 2, 'u': 4, 'U': 8}[d]
			j := i + 1
			for ; j < len(s) && j < i+1+max && isHex(s[j]); j++ {
			}
			if j == i+1 {
				b.WriteByte('\\')
				b.WriteByte(d)
				break
			}
			n, _ := strconv.ParseUint(s[i+1:j], 16, 32)
			if d == 'x' {
				b.WriteByte(byte(n))
			} else if r := rune(n); utf8.ValidRune(r) {
				b.WriteRune(r)
			} else {
				b.WriteString(s[i-1 : j])
			}
			i = j - 1
		default:
			b.WriteByte('\\')
			b.WriteByte(d)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// echoPlain returns what `echo "$s"` prints: a value that is only echo
// options (-n, -e, -E, in any combination) is swallowed by the builtin.
func echoPlain(s string) string {
	if len(s) > 1 && s[0] == '-' && strings.Trim(s[1:], "neE") == "" {
		if strings.Contains(s, "n") {
			return ""
		}
		return "\n"
	}
	return s + "\n"
}

const (
	ansiGreen = "\033[0;32m"
	ansiCyan  = "\033[0;36m"
	ansiRed   = "\033[0;31m"
	ansiBold  = "\033[1m"
	ansiReset = "\033[0m"
)

// Secret runs `ctx secret get|list|delete`, mirroring cmd_secret in
// lib/cmd_ops.sh. Failures return an *ExitError whose message is printed as is.
func Secret(stdout io.Writer, args []string) error {
	return secretCmd(stdout, args, ttyColors(stdout))
}

func secretCmd(stdout io.Writer, args []string, colors bool) error {
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	sub, prof, key := arg(0), arg(1), arg(2)
	color := func(c string) string {
		if colors {
			return c
		}
		return ""
	}
	// die prints like Bash's error: red mark on stderr, colored by stdout's TTY.
	die := func(msg string) error {
		return &ExitError{Code: 1, Msg: strings.TrimSuffix(echoE(color(ansiRed)+"✗"+color(ansiReset)+" "+msg), "\n")}
	}
	switch sub {
	case "get":
		if prof == "" || key == "" {
			return die("Usage: ctx secret get <profile> <KEY>")
		}
		val, err := secret.ProviderGetter{}.Get(prof, key)
		if err != nil {
			return die(fmt.Sprintf("No secret '%s' for profile '%s'", key, prof))
		}
		_, err = io.WriteString(stdout, echoPlain(val))
		return err
	case "list":
		if prof == "" {
			return die("Usage: ctx secret list <profile>")
		}
		if !profileExists(prof) {
			return die(fmt.Sprintf("Profile '%s' not found.", prof))
		}
		var b strings.Builder
		b.WriteString(echoE(color(ansiBold) + "\n  Secrets for: " + prof + "\n" + color(ansiReset)))
		var keys []string
		if st, err := secret.Open(secret.EffectiveProvider()); err == nil {
			keys, _ = st.List(prof)
		}
		var shown []string
		for _, k := range keys {
			if k != "" {
				shown = append(shown, k)
			}
		}
		if len(shown) == 0 {
			b.WriteString(echoE(color(ansiCyan) + "→" + color(ansiReset) + " No secrets stored for this profile."))
		}
		for _, k := range shown {
			b.WriteString(echoE("  " + color(ansiGreen) + "•" + color(ansiReset) + " " + k))
		}
		b.WriteString("\n")
		_, err := io.WriteString(stdout, b.String())
		return err
	case "delete":
		if prof == "" || key == "" {
			return die("Usage: ctx secret delete <profile> <KEY>")
		}
		label := secret.StoreLabel()
		st, err := secret.Open(secret.EffectiveProvider())
		if err == nil {
			err = st.Delete(prof, key)
		}
		if err != nil {
			return die(fmt.Sprintf("Failed to delete secret using provider '%s'. Check provider config and required CLI tools.", label))
		}
		_, err = io.WriteString(stdout, echoE(color(ansiGreen)+"✓"+color(ansiReset)+" Deleted "+key+" from "+label))
		return err
	}
	return fmt.Errorf("secret: unsupported subcommand %q", sub)
}

func profileExists(name string) bool {
	st, err := os.Stat(profile.Dir() + "/" + name + ".conf")
	return err == nil && st.Mode().IsRegular()
}

// EchoE is echoE for packages that print Bash-style messages.
func EchoE(s string) string { return echoE(s) }

// TTYColors is ttyColors for packages that print Bash-style messages.
func TTYColors(w io.Writer) bool { return ttyColors(w) }
