package gitssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureIncludeWritesThrough (R20): a symlinked ~/.ssh/config stays a
// link, the target gets the Include first, its private mode is kept; a
// world-writable one becomes 600 with a warning.
func TestEnsureIncludeWritesThrough(t *testing.T) {
	for _, c := range []struct {
		mode, want os.FileMode
		warn       bool
	}{{0o644, 0o644, false}, {0o600, 0o600, false}, {0o666, 0o600, true}} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("CTX_DIR", filepath.Join(home, ".ctx"))
		_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
		target := filepath.Join(home, "dots", "ssh_config")
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		if err := os.WriteFile(target, []byte("Host mine\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(target, c.mode)
		if err := os.Symlink(target, filepath.Join(home, ".ssh", "config")); err != nil {
			t.Fatal(err)
		}
		var warned []string
		if !EnsureInclude(func(m string) { warned = append(warned, m) }) {
			t.Errorf("%v: EnsureInclude reported a failure", c.mode)
		}
		if st, err := os.Lstat(filepath.Join(home, ".ssh", "config")); err != nil || st.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%v: link replaced", c.mode)
		}
		b, _ := os.ReadFile(target)
		if !strings.HasPrefix(string(b), "# ctx managed hosts") || !strings.HasSuffix(string(b), "\n\nHost mine\n") {
			t.Errorf("%v: target %q", c.mode, b)
		}
		if st, _ := os.Stat(target); st.Mode().Perm() != c.want {
			t.Errorf("%v: mode %v, want %v", c.mode, st.Mode().Perm(), c.want)
		}
		if got := len(warned) == 1 && strings.Contains(warned[0], "it is now 600"); got != c.warn {
			t.Errorf("%v: warnings %q", c.mode, warned)
		}
	}
}

// TestEnsureIncludeReportsFailure: a read-only ~/.ssh/config is left alone
// and EnsureInclude reports false; once the Include is there it reports true.
func TestEnsureIncludeReportsFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only file")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", filepath.Join(home, ".ctx"))
	cfg := filepath.Join(home, ".ssh", "config")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o700)
	if err := os.WriteFile(cfg, []byte("Host a\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	var warned []string
	if EnsureInclude(func(m string) { warned = append(warned, m) }) {
		t.Error("read-only config: EnsureInclude reported success")
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "Could not add Include") {
		t.Errorf("warnings %q", warned)
	}
	_ = os.Chmod(cfg, 0o600)
	if !EnsureInclude(nil) || !EnsureInclude(nil) {
		t.Error("writable config: EnsureInclude reported a failure (added, then already there)")
	}
}
