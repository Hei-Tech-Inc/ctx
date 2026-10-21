// Package testutil holds helpers shared by tests: fake executables that
// record how they were called.
package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FakeBin writes an executable /bin/sh script called name into a new temp
// dir. Each run appends its argv (each argument NUL-terminated, one
// invocation per line) to argvLog, then runs script. Put dir on PATH to use it.
func FakeBin(t *testing.T, name string, script string) (dir string, argvLog string) {
	t.Helper()
	dir = t.TempDir()
	argvLog = filepath.Join(dir, name+".argv")
	body := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\000' \"$a\" >> " + shQuote(argvLog) + "; done\n" +
		"printf '\\n' >> " + shQuote(argvLog) + "\n" +
		script + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, argvLog
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

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
