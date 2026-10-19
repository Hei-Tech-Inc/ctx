package main

import (
	"strings"
	"testing"
)

func TestCtxVersion(t *testing.T) {
	t.Setenv("CTX_VERSION", "")
	if got := ctxVersion(); got != version {
		t.Fatalf("unset: %q, want build version %q", got, version)
	}
	t.Setenv("CTX_VERSION", "9.9.9")
	if got := ctxVersion(); got != "9.9.9" {
		t.Fatalf("set: %q", got)
	}
}

func TestForwardedStubsExit127(t *testing.T) {
	for _, c := range []string{"list", "status", "workdir-prompt", "env", "run", "guard"} {
		var out, errb strings.Builder
		if rc := run([]string{c}, &out, &errb); rc != 127 {
			t.Errorf("%s: rc=%d, want 127", c, rc)
		}
	}
}
