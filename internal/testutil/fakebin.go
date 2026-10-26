// Package testutil holds helpers shared by tests: fake executables that
// record how they were called.
package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// FakeBin writes an executable /bin/sh script called name into a new temp
// dir. Each run appends its argv (each argument NUL-terminated, one
// invocation per line) to argvLog, then runs script. Put dir on PATH to use it.
func FakeBin(t *testing.T, name string, script string) (dir string, argvLog string) {
	t.Helper()
	dir = t.TempDir()
	argvLog = filepath.Join(dir, name+".argv")
	// The log is "$0.argv": $0 is the path the script was run by (dir/name),
	// so the body does not depend on dir and Script can share it.
	body := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\000' \"$a\" >> \"$0.argv\"; done\n" +
		"printf '\\n' >> \"$0.argv\"\n" +
		script + "\n"
	Script(t, filepath.Join(dir, name), body)
	return dir, argvLog
}

// Script makes path an executable with content body: a symlink to a copy of
// body in a per-user cache shared by every test process, keyed by its hash.
//
// macOS assesses a newly written executable on its first exec (about 0.3 s,
// serialized system-wide and far slower under load), while a new symlink to
// an executable that already ran costs nothing. Writing each distinct body
// once keeps fake tools cheap in parallel tests. When the cache cannot be
// used, body is written to path directly.
func Script(t *testing.T, path, body string) {
	t.Helper()
	if target, err := cachedScript(body); err == nil {
		if err := os.Symlink(target, path); err == nil {
			return
		}
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

var (
	cacheOnce sync.Once
	cacheDir  string
	cacheErr  error
)

// cachedScript returns the cache file holding body, creating it if needed.
func cachedScript(body string) (string, error) {
	cacheOnce.Do(func() {
		cacheDir = filepath.Join(os.TempDir(), fmt.Sprintf("ctx-test-scripts-%d", os.Getuid()))
		if err := os.Mkdir(cacheDir, 0o700); err != nil && !os.IsExist(err) {
			cacheErr = err
			return
		}
		// Only a private dir: nobody else may plant scripts in it (one made
		// by another user is unreadable to us, and the writes below fail).
		st, err := os.Lstat(cacheDir)
		if err != nil {
			cacheErr = err
			return
		}
		if !st.IsDir() || st.Mode().Perm()&0o077 != 0 {
			cacheErr = fmt.Errorf("%s is not a private directory", cacheDir)
		}
	})
	if cacheErr != nil {
		return "", cacheErr
	}
	sum := sha256.Sum256([]byte(body))
	p := filepath.Join(cacheDir, hex.EncodeToString(sum[:16]))
	if data, err := os.ReadFile(p); err == nil && string(data) == body {
		return p, nil
	}
	// Write to a temp file and rename, so concurrent test processes never
	// exec a half-written script.
	tmp, err := os.CreateTemp(cacheDir, "tmp-")
	if err != nil {
		return "", err
	}
	_, werr := tmp.WriteString(body)
	cerr := tmp.Close()
	if err := firstErr(werr, cerr, os.Chmod(tmp.Name(), 0o755)); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return p, nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ReadArgv returns one []string per recorded invocation, oldest first.
// It returns nil when the fake was never run.
func ReadArgv(t *testing.T, argvLog string) [][]string {
	t.Helper()
	data, err := os.ReadFile(argvLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		args := []string{}
		if line != "" {
			args = strings.Split(strings.TrimSuffix(line, "\x00"), "\x00")
		}
		out = append(out, args)
	}
	return out
}
