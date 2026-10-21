package cli

import (
	"bytes"
	"testing"
)

func TestConfigColors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CTX_DIR", dir)
	t.Setenv("HOME", dir)
	var out, errb bytes.Buffer
	if code := configCmd(ui{out: &out, err: &errb, colors: true}, []string{"autoswitch-notify", "on"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := "\033[0;32m✓\033[0m autoswitch_notify: on — [ctx] ←/→ messages enabled\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	out.Reset()
	if code := configCmd(ui{out: &out, err: &errb, colors: true}, []string{"autoswitch-notify", "maybe"}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if want := "\033[0;31m✗\033[0m Usage: ctx config autoswitch-notify <on|off>\n"; errb.String() != want || out.Len() != 0 {
		t.Errorf("stderr = %q, want %q (stdout %q)", errb.String(), want, out.String())
	}
}
