package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Differential subtests run in parallel: their Bash half is a child process
// with an explicit environment and directory, so many can run at once. The
// command code they compare against (UseIn, Remove, SecretIn, StatusText…)
// reads the process environment and working directory, so the in-process Go
// half runs one subtest at a time under procMu (see ptest.inProc).
//
// Only subtests of one sequential top-level test run together, so the process
// environment a parallel subtest starts from is its parent's, plus whatever a
// sibling's inProc has set while holding procMu. Reading it under procMu
// (newPtest) sees the parent's.
var procMu sync.Mutex

// ptest is a parallel subtest's environment: overrides on top of the process
// environment as it was when the subtest started. Use it instead of
// t.Setenv/t.Chdir, which panic in parallel tests.
type ptest struct {
	t    *testing.T
	base []string
	env  []string // KEY=VALUE overrides in order; a later one wins
}

// newPtest snapshots the process environment and, for a subtest, marks t
// parallel. A top-level test stays sequential: the parallel top-level tests
// (the pty suites) read os.Environ without procMu, so inProc must only run
// while they are paused, that is inside a sequential top-level test.
func newPtest(t *testing.T) *ptest {
	t.Helper()
	if strings.Contains(t.Name(), "/") {
		t.Parallel()
	}
	procMu.Lock()
	base := os.Environ()
	procMu.Unlock()
	return &ptest{t: t, base: base}
}

func (p *ptest) Setenv(key, value string) { p.env = append(p.env, key+"="+value) }

// Getenv returns key's value as the subtest's processes see it.
func (p *ptest) Getenv(key string) string {
	for _, list := range [][]string{p.env, p.base} {
		for i := len(list) - 1; i >= 0; i-- {
			if k, v, _ := strings.Cut(list[i], "="); k == key {
				return v
			}
		}
	}
	return ""
}

// Environ is the environment for a child process: the base snapshot with the
// overrides appended (os/exec keeps the last value of a duplicate key).
func (p *ptest) Environ(extra ...string) []string {
	out := make([]string, 0, len(p.base)+len(p.env)+len(extra))
	out = append(append(append(out, p.base...), p.env...), extra...)
	return out
}

// inProc runs fn with the overrides set in the process environment and, when
// dir is not empty, dir as the working directory; then it restores both. Runs
// are serialized across parallel subtests.
func (p *ptest) inProc(dir string, fn func()) {
	p.t.Helper()
	procMu.Lock()
	defer procMu.Unlock()
	saved := os.Environ()
	wd, err := os.Getwd()
	if err != nil {
		p.t.Fatal(err)
	}
	defer func() {
		restoreEnv(saved)
		if err := os.Chdir(wd); err != nil {
			p.t.Fatal(err)
		}
	}()
	for _, kv := range p.env {
		k, v, _ := strings.Cut(kv, "=")
		if err := os.Setenv(k, v); err != nil {
			p.t.Fatal(err)
		}
	}
	if dir != "" {
		if err := os.Chdir(dir); err != nil {
			p.t.Fatal(err)
		}
	}
	fn()
}

// lookPath is exec.LookPath on the process PATH outside any inProc run (a
// sibling's inProc may have put its fakes first).
func lookPath(name string) (string, error) {
	procMu.Lock()
	defer procMu.Unlock()
	return exec.LookPath(name)
}

// locked runs fn under procMu, for helpers that read the process
// environment themselves (testutil.FixedStampDate looks up date).
func locked(fn func()) {
	procMu.Lock()
	defer procMu.Unlock()
	fn()
}

// newPFixture is newFixture for a parallel subtest: HOME, CTX_DIR and
// CTX_ACTIVE_PROFILE are set on p instead of the process.
func newPFixture(p *ptest) *fixture {
	p.t.Helper()
	home, err := filepath.EvalSymlinks(p.t.TempDir())
	if err != nil {
		p.t.Fatal(err)
	}
	f := &fixture{t: p.t, home: home, dir: filepath.Join(home, ".ctx"), p: p}
	p.Setenv("HOME", home)
	p.Setenv("CTX_DIR", f.dir)
	p.Setenv("CTX_ACTIVE_PROFILE", "")
	return f
}
