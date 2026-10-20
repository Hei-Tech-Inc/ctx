package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretListColors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CTX_DIR", dir)
	t.Setenv("CTX_SECRET_PROVIDER", "file")
	for p, body := range map[string]string{"profiles/p.conf": "", "secrets/p/B": "1", "secrets/p/A": "2"} {
		path := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := secretCmd(&out, []string{"list", "p"}, true); err != nil {
		t.Fatal(err)
	}
	// What lib/cmd_ops.sh prints on a terminal: bold header, green bullets.
	want := "\033[1m\n  Secrets for: p\n\033[0m\n" +
		"  \033[0;32m\u2022\033[0m A\n  \033[0;32m\u2022\033[0m B\n\n"
	if out.String() != want {
		t.Errorf("colored list = %q, want %q", out.String(), want)
	}
	err := secretCmd(&out, []string{"get", "p", "NOPE"}, true)
	if ee, ok := err.(*ExitError); !ok || ee.Msg != "\033[0;31m\u2717\033[0m No secret 'NOPE' for profile 'p'" {
		t.Errorf("colored die = %#v", err)
	}
	out.Reset()
	if err := secretCmd(&out, []string{"delete", "p", "A"}, true); err != nil || out.String() != "\033[0;32m\u2713\033[0m Deleted A from file store\n" {
		t.Errorf("colored delete = %q, %v", out.String(), err)
	}
}

func TestEchoE(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:      "plain\n",
		`a\tb\\c`:    "a\tb\\c\n",
		`x\0101y`:    "xAy\n",
		`\x41\u00e9`: "A\u00e9\n",
		`stop\cnot`:  "stop",
		`\q\x`:       "\\q\\x\n",
		`end\`:       "end\\\n",
	} {
		if got := echoE(in); got != want {
			t.Errorf("echoE(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"-n": "", "-e": "\n", "-neE": "", "-x": "-x\n", "-": "-\n", "ok": "ok\n"} {
		if got := echoPlain(in); got != want {
			t.Errorf("echoPlain(%q) = %q, want %q", in, got, want)
		}
	}
}
