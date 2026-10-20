package testutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ExternalTools are commands ctx looks up that are not part of a base system
// but that CI runners install in the system dirs (ubuntu-latest has az,
// gcloud, kubectl and gh in /usr/bin). Run for real, cloud CLIs write logs
// and telemetry under HOME, call the network and can prompt, so tests must
// never reach them: SystemPath leaves them out, and a test that wants one
// puts a fake first on PATH.
var ExternalTools = []string{
	"az", "gcloud", "gsutil", "bq", "kubectl", "gh", "aws", "gum", "mise",
	"direnv", "pass", "brew", "ctx", "ctx-core",
}

// systemDirs are searched in PATH order: the first dir holding a name wins.
var systemDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

var (
	sysOnce sync.Once
	sysDir  string
	sysErr  error
)

// SystemPath returns a dir holding a symlink to every command in /usr/bin,
// /bin, /usr/sbin and /sbin (the first one, as PATH would find it) except
// ExternalTools. Use it in place of those dirs on a test's PATH so the result
// does not depend on which extra tools the machine has installed. The dir is
// built once per test binary and shared; do not write to it.
func SystemPath(t *testing.T) string {
	t.Helper()
	sysOnce.Do(func() { sysDir, sysErr = buildSystemPath() })
	if sysErr != nil {
		t.Fatalf("SystemPath: %v", sysErr)
	}
	return sysDir
}

func buildSystemPath() (string, error) {
	dir, err := os.MkdirTemp("", "ctx-syspath-")
	if err != nil {
		return "", err
	}
	skip := map[string]bool{}
	for _, n := range ExternalTools {
		skip[n] = true
	}
	for _, d := range systemDirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue // not every system has every dir
		}
		for _, e := range entries {
			name := e.Name()
			if skip[name] {
				continue
			}
			skip[name] = true
			if err := os.Symlink(filepath.Join(d, name), filepath.Join(dir, name)); err != nil {
				return "", err
			}
		}
	}
	return dir, nil
}
