package setup

import (
	"errors"
	"regexp"
	"strings"
)

// echoSwallowed reports whether `echo "$s"` prints no text because s is only
// echo options (-n, -e, -E in any combination).
func echoSwallowed(s string) bool {
	return len(s) > 1 && s[0] == '-' && strings.Trim(s[1:], "neE") == ""
}

// sedReplace is `echo "$s" | sed 's/[^<allowed>]/-/g'` under a UTF-8 locale:
// every character (not byte) outside allowed becomes one '-'. Trailing
// newlines are dropped like $(...) does.
func sedReplace(s string, allowed func(r rune) bool) string {
	if echoSwallowed(s) {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || allowed(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// Slug is the profile slug: lower-case, anything but [a-z0-9_-] becomes '-'.
// Bash: tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/-/g' (UTF-8 locale:
// one '-' per multibyte character).
func Slug(name string) string {
	if echoSwallowed(name) {
		return ""
	}
	return sedReplace(lowerASCII(name), func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-'
	})
}

// FolderSlug is the WORK_SLUG/folder slug: '/' becomes '-', then anything
// but [a-zA-Z0-9._-] becomes '-'.
func FolderSlug(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	return sedReplace(s, func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
	})
}

var emailRE = regexp.MustCompile(`^[^\t-\r \x{85}\p{Z}]+@[^\t-\r \x{85}\p{Z}]+\.[^\t-\r \x{85}\p{Z}]+`)

// SensibleEmail is is_sensible_git_email: has '@', no "..", and looks like
// user@host.tld (no whitespace, Unicode spaces included as in a UTF-8 locale).
func SensibleEmail(s string) bool {
	return strings.Contains(s, "@") && !strings.Contains(s, "..") && emailRE.MatchString(s)
}

// ValidEnvKey is is_valid_env_key: ^[A-Za-z_][A-Za-z0-9_]*$.
func ValidEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' || i > 0 && c >= '0' && c <= '9'
		if !ok {
			return false
		}
	}
	return true
}

// NormalizeSigning is ctx_signing_format_normalize: "ssh", "openpgp" or ""
// (off); any other value is an error (Bash returns 1).
func NormalizeSigning(s string) (string, error) {
	switch lowerASCII(s) {
	case "ssh":
		return "ssh", nil
	case "gpg", "openpgp", "pgp":
		return "openpgp", nil
	case "", "none", "off", "no":
		return "", nil
	}
	return "", errors.New("unknown signing format")
}

// KeyComment is _ctx_setup_ssh_key_comment: the ssh-keygen -C comment.
func KeyComment(profile, email string) string {
	if email != "" && SensibleEmail(email) {
		return email
	}
	return "ctx-" + profile + "@local"
}
