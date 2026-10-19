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

func TestListStatusImplements(t *testing.T) {
	for _, c := range []string{"list", "status"} {
		var out, errb strings.Builder
		if rc := run([]string{"implements", c, "--json"}, &out, &errb); rc != 0 {
			t.Errorf("implements %s --json: rc=%d, want 0", c, rc)
		}
		if rc := run([]string{"implements", c}, &out, &errb); rc != 1 {
			t.Errorf("implements %s: rc=%d, want 1", c, rc)
		}
		if rc := run([]string{c}, &out, &errb); rc != 127 {
			t.Errorf("%s without --json: rc=%d, want 127", c, rc)
		}
	}
}

func TestStubsAndImplements(t *testing.T) {
	for _, c := range []string{"env", "run", "guard"} {
		var out, errb strings.Builder
		if rc := run([]string{c}, &out, &errb); rc != 2 {
			t.Errorf("%s: rc=%d, want 2", c, rc)
		}
		if rc := run([]string{"implements", c}, &out, &errb); rc != 1 {
			t.Errorf("implements %s: rc=%d, want 1", c, rc)
		}
	}
}

func TestWorkdirPromptImplements(t *testing.T) {
	var out, errb strings.Builder
	if rc := run([]string{"implements", "workdir-prompt"}, &out, &errb); rc != 0 {
		t.Errorf("implements workdir-prompt: rc=%d, want 0", rc)
	}
}
