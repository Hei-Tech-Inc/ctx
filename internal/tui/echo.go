package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// EchoE returns what `echo -e` prints for s, newline included. A \c in s stops
// all output, including the newline.
func EchoE(s string) string {
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

// echoed is what `$(echo "$s")` captures: a value that is only echo options
// (-n, -e, -E in any combination) is swallowed by the builtin, and trailing
// newlines are dropped.
func echoed(s string) string {
	if len(s) > 1 && s[0] == '-' && strings.Trim(s[1:], "neE") == "" {
		return ""
	}
	return strings.TrimRight(s, "\n")
}
