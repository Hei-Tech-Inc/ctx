package main

import (
	"bytes"
	"os"
	"path/filepath"
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
		if rc := run([]string{"implements", c}, &out, &errb); rc != 0 {
			t.Errorf("implements %s: rc=%d, want 0", c, rc)
		}
		if rc := run([]string{"implements", c, "bogus"}, &out, &errb); rc != 1 {
			t.Errorf("implements %s bogus: rc=%d, want 1", c, rc)
		}
		if rc := run([]string{c, "bogus"}, &out, &errb); rc != 127 {
			t.Errorf("%s bogus: rc=%d, want 127", c, rc)
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

func TestUseImplementsAlways(t *testing.T) {
	var out, errb strings.Builder
	for _, quiet := range []string{"1", "0", "", "yes"} {
		t.Setenv("CTX_QUIET", quiet)
		for _, sub := range [][]string{{"use", "demo"}, {"use", "--eval", "bash"}, {"use"}} {
			if rc := run(append([]string{"implements"}, sub...), &out, &errb); rc != 0 {
				t.Errorf("CTX_QUIET=%q implements %v: rc=%d, want 0", quiet, sub, rc)
			}
		}
	}
}

// TestImplementsSetup: setup (and its import/add aliases) is ctx-core's only
// for --config FILE with --yes/-y or AUTO_CONFIRM in FILE; implements sees
// every argument, not just the first.
func TestImplementsSetup(t *testing.T) {
	td := t.TempDir()
	auto := filepath.Join(td, "auto.conf")
	plain := filepath.Join(td, "plain.conf")
	if err := os.WriteFile(auto, []byte("AUTO_CONFIRM=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte("PROFILE_NAME=a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want int
	}{
		{[]string{"setup", "--config", plain, "--yes"}, 0},
		{[]string{"import", "-y", "--config", plain}, 0},
		{[]string{"add", "--config", auto}, 0},
		{[]string{"setup", "--config", plain}, 1},
		{[]string{"setup", "--config", plain, "--yes", "--help"}, 1},
		{[]string{"setup", "--config", filepath.Join(td, "missing"), "--yes"}, 1},
		{[]string{"setup"}, 1},
		{[]string{"setup", "--scope", "clone"}, 0},
		{[]string{"add", "--scope", "full"}, 1},
	} {
		var out, errb bytes.Buffer
		if rc := run(append([]string{"implements"}, c.args...), &out, &errb); rc != c.want {
			t.Errorf("implements %q: rc=%d, want %d", c.args, rc, c.want)
		}
	}
}
