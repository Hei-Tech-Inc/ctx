package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// bashEval sources the Bash libraries and runs script with args as $1...
// The locale is pinned so tr/sed treat multibyte input the way the Go port
// mirrors. C.UTF-8, not en_US.UTF-8: glibc's en_US.UTF-8 collates ranges, so
// [a-z] and [A-Za-z] match é, À and ß there (Bash would keep "Café" in a slug);
// C.UTF-8 compares code points on glibc and macOS alike.
func bashEval(t *testing.T, script string, args ...string) (string, int) {
	t.Helper()
	root := repoRoot(t)
	full := "set -uo pipefail; source " + root + "/lib/core.sh; source " + root + "/lib/cmd_import.sh; " + script
	cmd := exec.Command("bash", append([]string{"-c", full, "bash"}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8", "HOME="+t.TempDir(), "CTX_DIR="+t.TempDir())
	out, err := cmd.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

var pureInputs = []string{
	"", "acme", "Acme Corp", "ACME_corp-1", "Café", "日本語", "a/b/c", "..", "../x", "a..b",
	"a@b", "x@y.z", "x@y.", "@y.z", "a b@c.d", "me@co.uk", "A.B@C.D", "-n", "-e", "-neE", "-x",
	"a\\nb", "tab\there", "x@y..z", "ÀB", "ssh", "SSH", "Gpg", "OpenPGP", "pgp", "None", "OFF", "no", "yes", "a@b.c d",
	"a@b\u00a0.c", "a@b\u2007.c", "a@b\u202f.c", "a@b\u2003.c", "a@b\u0085.c", "a@b\u3000.c", "_ok", "1abc", "A_1", "A-1", "k=v",
}

// bashEach runs body once per input inside a single bash process and returns
// the NUL-separated records body printed (keeps the suite fast).
func bashEach(t *testing.T, body string, inputs ...string) []string {
	t.Helper()
	out, _ := bashEval(t, `for s in "$@"; do `+body+`; printf '\0'; done`, inputs...)
	recs := strings.Split(out, "\x00")
	if len(recs) != len(inputs)+1 {
		t.Fatalf("bash returned %d records for %d inputs", len(recs)-1, len(inputs))
	}
	return recs[:len(inputs)]
}

func TestPureMatchesBash(t *testing.T) {
	slug := bashEach(t, `printf %s "$(echo "$s" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/-/g')"`, pureInputs...)
	folder := bashEach(t, `x="${s//\//-}"; printf %s "$(echo "$x" | sed 's/[^a-zA-Z0-9._-]/-/g')"`, pureInputs...)
	email := bashEach(t, `if is_sensible_git_email "$s"; then printf 1; else printf 0; fi`, pureInputs...)
	envkey := bashEach(t, `if is_valid_env_key "$s"; then printf 1; else printf 0; fi`, pureInputs...)
	signing := bashEach(t, `o="$(ctx_signing_format_normalize "$s")"; printf '%s:%s' "$?" "$o"`, pureInputs...)
	truthy := bashEach(t, `printf %s "$(ctx_setup_cfg_truthy "$s")"`, pureInputs...)
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	for i, in := range pureInputs {
		if g := Slug(in); g != slug[i] {
			t.Errorf("Slug(%q) = %q, bash %q", in, g, slug[i])
		}
		if g := FolderSlug(in); g != folder[i] {
			t.Errorf("FolderSlug(%q) = %q, bash %q", in, g, folder[i])
		}
		if g := b(SensibleEmail(in)); g != email[i] {
			t.Errorf("SensibleEmail(%q) = %s, bash %s", in, g, email[i])
		}
		if g := b(ValidEnvKey(in)); g != envkey[i] {
			t.Errorf("ValidEnvKey(%q) = %s, bash %s", in, g, envkey[i])
		}
		g, err := NormalizeSigning(in)
		code := "0"
		if err != nil {
			code = "1"
		}
		if got := code + ":" + g; got != signing[i] {
			t.Errorf("NormalizeSigning(%q) = %q, bash %q", in, got, signing[i])
		}
		if g := b(Truthy(in)); g != truthy[i] {
			t.Errorf("Truthy(%q) = %s, bash %s", in, g, truthy[i])
		}
	}
	for _, email := range []string{"", "me@co.uk", "bad", "a..b@c.d"} {
		got := bashEach(t, `GIT_EMAIL="`+email+`"; printf %s "$(_ctx_setup_ssh_key_comment "$s")"`, pureInputs...)
		for i, in := range pureInputs {
			if g := KeyComment(in, email); g != got[i] {
				t.Errorf("KeyComment(%q,%q) = %q, bash %q", in, email, g, got[i])
			}
		}
	}
}

func TestReadConfigMatchesBash(t *testing.T) {
	body := "# comment\nA=1\nA=2\nB = spaced\n  C=  padded  \nD=\"quoted val\"\nE='single'\nF=\"unterminated\nG=\"\"\nH=\nI=a=b=c\n" +
		"#J=hidden\nJ=real\nK=x\r\nL=\"crlf\"\r\nM=\"\r\nN=\"a\"b\"\nO='\"'\nP=\"\n  Q=indented\nR=é日\nlast=1"
	path := filepath.Join(t.TempDir(), "c.conf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "last", "MISSING"}
	for _, def := range []string{"", "dflt"} {
		out, _ := bashEval(t, `d="$1"; shift; for k in "$@"; do printf '%s\0' "$(ctx_setup_cfg_get "`+path+`" "$k" "$d")"; done`, append([]string{def}, keys...)...)
		want := strings.Split(out, "\x00")
		for i, key := range keys {
			if got := c.Get(key, def); got != want[i] {
				t.Errorf("Get(%q,%q) = %q, bash %q", key, def, got, want[i])
			}
		}
	}
	if _, err := ReadConfig(filepath.Join(t.TempDir(), "nope")); err == nil || !strings.HasPrefix(err.Error(), "setup --config: not a file: ") {
		t.Errorf("ReadConfig missing = %v", err)
	}
}
