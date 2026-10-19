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

func TestGuardImplements(t *testing.T) {
	var out, errb strings.Builder
	for sub, want := range map[string]int{"check": 0, "install": 0, "uninstall": 0, "remove": 0, "status": 0, "": 0, "bogus": 1} {
		args := []string{"implements", "guard"}
		if sub != "" {
			args = append(args, sub)
		}
		if rc := run(args, &out, &errb); rc != want {
			t.Errorf("implements guard %q: rc=%d, want %d", sub, rc, want)
		}
	}
	if rc := run([]string{"guard", "bogus"}, &out, &errb); rc != 127 {
		t.Errorf("guard bogus: rc=%d, want 127", rc)
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
