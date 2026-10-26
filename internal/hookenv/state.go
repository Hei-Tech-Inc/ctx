package hookenv

import (
	"strings"
)

// State is what hook-env remembers in the shell-local __CTX_STATE between
// runs (R2): the pwd and target of the last run that got past the cache
// guard, and the names (never the values) of the variables hook-env
// exported for that target, so leaving or switching unsets exactly those.
type State struct {
	PWD, Target string
	Keys        []string
}

const stateVersion = "v1"

// Encode returns "v1|pwd|target|keys": keys space-separated, and in pwd and
// target the bytes "%", "|" and control characters percent-encoded (%XX), so
// any path survives. The result is printable and holds no "|" but the three
// separators.
func (s State) Encode() string {
	return stateVersion + "|" + escape(s.PWD) + "|" + escape(s.Target) + "|" + strings.Join(s.Keys, " ")
}

// Decode parses Encode's output. It reports false for anything else (an
// unknown version, a malformed field, an invalid key name), which callers
// treat as a fresh shell.
func Decode(s string) (State, bool) {
	parts := strings.Split(s, "|")
	if len(parts) != 4 || parts[0] != stateVersion {
		return State{}, false
	}
	pwd, ok1 := unescape(parts[1])
	target, ok2 := unescape(parts[2])
	if !ok1 || !ok2 {
		return State{}, false
	}
	keys := strings.Fields(parts[3])
	for _, k := range keys {
		if !validKey(k) {
			return State{}, false
		}
	}
	if len(keys) == 0 {
		keys = nil
	}
	return State{PWD: pwd, Target: target, Keys: keys}, true
}

const hexDigits = "0123456789ABCDEF"

func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' || c == '|' || c < 0x20 || c == 0x7f {
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&15])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

func unescape(s string) (string, bool) {
	if !strings.Contains(s, "%") {
		return s, true
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		hi, ok1 := unhex(s[i+1])
		lo, ok2 := unhex(s[i+2])
		if !ok1 || !ok2 {
			return "", false
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), true
}

// validKey is a shell variable name (isValidEnvKey in internal/cli).
func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
