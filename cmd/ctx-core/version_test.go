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

func TestStubsAndImplements(t *testing.T) {
	for _, c := range []string{"list", "status", "workdir-prompt", "env", "run", "guard"} {
		var out, errb strings.Builder
		if rc := run([]string{c}, &out, &errb); rc != 2 {
			t.Errorf("%s: rc=%d, want 2", c, rc)
		}
		if rc := run([]string{"implements", c}, &out, &errb); rc != 1 {
			t.Errorf("implements %s: rc=%d, want 1", c, rc)
		}
	}
}
