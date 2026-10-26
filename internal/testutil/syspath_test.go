package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// A system dir holding curl and wget (as /usr/bin does on ubuntu-latest)
// does not put them on SystemPath.
func TestSystemPathLeavesOutDownloaders(t *testing.T) {
	sys := t.TempDir()
	for _, n := range []string{"curl", "wget", "gh", "true"} {
		if err := os.WriteFile(filepath.Join(sys, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := buildSystemPath([]string{sys})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, n := range []string{"curl", "wget", "gh"} {
		if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s is on SystemPath", n)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "true")); err != nil {
		t.Errorf("true is missing from SystemPath: %v", err)
	}
}
