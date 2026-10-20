// Package shquote quotes strings the way Bash's `printf %q` does, so files
// ctx-core writes (profile .conf, export lines) are byte-identical to the
// ones lib/*.sh writes.
package shquote

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// utf8Locale reports whether the locale Bash would use (LC_ALL, else
// LC_CTYPE, else LANG) is a UTF-8 one. It decides whether bytes >= 0x80 that
// form valid UTF-8 are printable (kept as is) or need $'\ooo' quoting.
func utf8Locale() bool {
	loc := os.Getenv("LC_ALL")
	if loc == "" {
		loc = os.Getenv("LC_CTYPE")
	}
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	l := strings.ToLower(loc)
	return strings.Contains(l, "utf-8") || strings.Contains(l, "utf8")
}

// Quote returns what bash 4+/5 `printf %q` prints for s:
//   - the empty string is a pair of single quotes;
//   - a string with a byte Bash cannot print (a control character, or a byte
//     >= 0x80 that is not part of valid UTF-8 in a UTF-8 locale, or any byte
//     >= 0x80 otherwise) is $'…' with C escapes (\a \b \t \n \v \f \r \E,
//     \\ and \', other bytes as \ooo);
//   - otherwise shell metacharacters get a backslash: space, tab, newline,
//     ' " \ | & ; ( ) < > ! { } * [ ? ] ^ $ ` and comma anywhere, # only as
//     the first character, ~ as the first character or after = or :.
//
// The result is eval-safe in assignments (no tilde expansion). Older bash
// differs only in quoting fewer tildes: 3.2 none, bash before 5.3 possibly
// not the ones after = or : (R21).
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	u8 := utf8Locale()
	if needsANSI(s, u8) {
		return ansi(s, u8)
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '\t', '\n', '\'', '"', '\\', '|', '&', ';', '(', ')', '<', '>',
			'!', '{', '}', '*', '[', '?', ']', '^', '$', '`', ',':
			b.WriteByte('\\')
		case '#':
			if i == 0 {
				b.WriteByte('\\')
			}
		case '~':
			// bash 5.3 also quotes a tilde after = or :, where an assignment
			// would tilde-expand it (bash; zsh with MAGIC_EQUAL_SUBST).
			if i == 0 || s[i-1] == '=' || s[i-1] == ':' {
				b.WriteByte('\\')
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func needsANSI(s string, u8 bool) bool {
	for i := 0; i < len(s); {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			return true
		}
		if c < 0x80 {
			i++
			continue
		}
		if !u8 {
			return true
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n <= 1 {
			return true
		}
		i += n
	}
	return false
}

func ansi(s string, u8 bool) string {
	var b strings.Builder
	b.WriteString("$'")
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x80 {
			if u8 {
				if r, n := utf8.DecodeRuneInString(s[i:]); !(r == utf8.RuneError && n <= 1) {
					b.WriteString(s[i : i+n])
					i += n
					continue
				}
			}
			fmt.Fprintf(&b, `\%03o`, c)
			i++
			continue
		}
		switch c {
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\v':
			b.WriteString(`\v`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		case 0x1b:
			b.WriteString(`\E`)
		case '\\', '\'':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, `\%03o`, c)
			} else {
				b.WriteByte(c)
			}
		}
		i++
	}
	b.WriteByte('\'')
	return b.String()
}
