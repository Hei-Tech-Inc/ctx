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

func TestEnvRunImplements(t *testing.T) {
	for _, c := range []string{"env", "run"} {
		var out, errb strings.Builder
		if rc := run([]string{"implements", c}, &out, &errb); rc != 0 {
			t.Errorf("implements %s: rc=%d, want 0", c, rc)
		}
	}
}

func TestGuardImplementsOnlyCheck(t *testing.T) {
	var out, errb strings.Builder
	for sub, want := range map[string]int{"check": 0, "install": 1, "status": 1} {
		if rc := run([]string{"implements", "guard", sub}, &out, &errb); rc != want {
			t.Errorf("implements guard %s: rc=%d, want %d", sub, rc, want)
		}
	}
	if rc := run([]string{"implements", "guard"}, &out, &errb); rc != 1 {
		t.Errorf("implements guard: rc=%d, want 1", rc)
	}
	for _, args := range [][]string{{"guard"}, {"guard", "install"}} {
		if rc := run(args, &out, &errb); rc != 127 {
			t.Errorf("%v: rc=%d, want 127", args, rc)
		}
	}
}

func TestWorkdirPromptImplements(t *testing.T) {
	var out, errb strings.Builder
	if rc := run([]string{"implements", "workdir-prompt"}, &out, &errb); rc != 0 {
		t.Errorf("implements workdir-prompt: rc=%d, want 0", rc)
	}
}

func TestUseImplementsOnlyWhenQuiet(t *testing.T) {
	var out, errb strings.Builder
	for quiet, want := range map[string]int{"1": 0, "0": 1, "": 1, "yes": 1} {
		t.Setenv("CTX_QUIET", quiet)
		for _, sub := range [][]string{{"use", "demo"}, {"use", "--eval", "bash"}, {"use"}} {
			if rc := run(append([]string{"implements"}, sub...), &out, &errb); rc != want {
				t.Errorf("CTX_QUIET=%q implements %v: rc=%d, want %d", quiet, sub, rc, want)
			}
		}
	}
	t.Setenv("CTX_QUIET", "0")
	if rc := run([]string{"use", "demo"}, &out, &errb); rc != 127 {
		t.Errorf("use without CTX_QUIET=1: rc=%d, want 127", rc)
	}
}
