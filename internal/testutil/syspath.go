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
// puts a fake first on PATH. The downloaders curl and wget are left out too,
// so no test reaches the network by accident; a fake that must download for
// real (from a local server) runs the one SystemTool finds.
var ExternalTools = []string{
	"az", "gcloud", "gsutil", "bq", "kubectl", "gh", "aws", "gum", "mise",
	"direnv", "pass", "brew", "ctx", "ctx-core", "curl", "wget",
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
// built once per test binary and shared; do not write to it. A package that
// uses it calls CleanupSystemPath from its TestMain after m.Run.
func SystemPath(t *testing.T) string {
	t.Helper()
	sysOnce.Do(func() { sysDir, sysErr = buildSystemPath(systemDirs) })
	if sysErr != nil {
		t.Fatalf("SystemPath: %v", sysErr)
	}
	return sysDir
}

// SystemTool is the path of name in the system dirs (the first one, as PATH
// would find it), even when SystemPath leaves it out, or "" when there is
// none. Only fakes that guard what they pass on should run it.
func SystemTool(name string) string {
	for _, d := range systemDirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func buildSystemPath(systemDirs []string) (string, error) {
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

// CleanupSystemPath removes the dir SystemPath built, if any. Call it from
// TestMain once every test has finished.
func CleanupSystemPath() {
	sysOnce.Do(func() {}) // nothing built yet: build nothing later either
	if sysDir != "" {
		_ = os.RemoveAll(sysDir)
	}
}
