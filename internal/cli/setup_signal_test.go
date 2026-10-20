//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// TestSetupCtrlC types Ctrl+C (a real SIGINT from the terminal) into the Go
// wizard: it prints one cancel line, exits 130 and writes nothing, as Bash
// (whose INT trap exits 130) leaves HOME untouched too.
func TestSetupCtrlC(t *testing.T) {
	t.Parallel()
	fakes := cloneFakes(t)
	core := buildCore(t)
	tempDir := func(t *testing.T) string {
		td, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return td
	}
	cancelled := func(t *testing.T, r cloneRun) {
		t.Helper()
		if r.code != 130 || strings.Count(r.transcript, "Cancelled by user.") != 1 {
			t.Errorf("exit %d, want 130 and one cancel line; transcript:\n%s", r.code, r.transcript)
		}
		if _, ok := r.tree[".ctx"]; ok {
			t.Error("~/.ctx created")
		}
	}

	t.Run("plain prompt", func(t *testing.T) {
		t.Parallel()
		c := cloneCase{args: []string{}, answers: []string{testutil.CtrlC}}
		want := runClone(t, tempDir(t), fakes, "", c)
		if want.code != 130 {
			t.Errorf("bash exit %d, want 130", want.code)
		}
		got := runClone(t, tempDir(t), fakes, core, c)
		cancelled(t, got)
		if d := testutil.DiffSnapshots(want.tree, got.tree); d != "" {
			t.Errorf("HOME differs (first = bash, second = go):\n%s", d)
		}
	})

	t.Run("slow gh scan", func(t *testing.T) {
		t.Parallel()
		td := tempDir(t)
		start := time.Now()
		r := runClone(t, td, fakes, core, cloneCase{args: []string{}, env: []string{"FAKE_GH_SLOW=1"},
			answers: []string{testutil.CtrlC}})
		took := time.Since(start)
		cancelled(t, r)
		// Ctrl+C is typed after about a second of quiet (RunPTY's slowGap,
		// stretched on a loaded machine); the scan would take its 8s timeout
		// and the fake gh 30s.
		if took > 6*time.Second {
			t.Errorf("took %v to exit", took)
		}
		b, err := os.ReadFile(filepath.Join(td, "logs", "gh.pid"))
		if err != nil {
			t.Fatalf("fake gh did not start: %v\n%s", err, r.transcript)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
		// SIGKILL is sent before exit; allow the orphan a moment to be reaped.
		for deadline := time.Now().Add(time.Second); syscall.Kill(pid, 0) == nil; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Errorf("fake gh (pid %d) still running after Ctrl+C", pid)
				break
			}
		}
	})
}
