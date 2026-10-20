//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

func TestFlagsFromEnv(t *testing.T) {
	t.Setenv("CTX_DRY_RUN", "1")
	t.Setenv("CTX_QUIET", "1")
	t.Setenv("CTX_JSON", "")
	t.Setenv("CTX_NO_GUM", "1")
	if got, want := FlagsFromEnv(), (Flags{DryRun: true, Quiet: true, NoGum: true}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	t.Setenv("CTX_QUIET", "0")
	if FlagsFromEnv().Quiet {
		t.Error("CTX_QUIET=0 is not quiet")
	}
}

// TestOnInterruptAnyCommand runs OnInterrupt around a plain AskYN in a child
// (this test binary, re-executed) on a pty: Ctrl+C prints one cancel line and
// exits 130, and the kill hook runs.
func TestOnInterruptAnyCommand(t *testing.T) {
	if os.Getenv("CTX_TEST_ONINTERRUPT") == "1" {
		term := tui.FromOS()
		defer OnInterrupt(term, func() { _, _ = os.Stderr.WriteString("killed\n") })()
		_, _ = term.Prompter().AskYN("Go on?", true)
		os.Exit(0) // not reached by a signal
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOnInterruptAnyCommand$")
	cmd.Env = append(os.Environ(), "CTX_TEST_ONINTERRUPT=1", "CTX_NO_GUM=1")
	out, code := testutil.RunPTY(t, cmd, []string{testutil.CtrlC}, 30*time.Second)
	if code != 130 || strings.Count(out, "Cancelled by user.") != 1 || strings.Count(out, "killed") != 1 {
		t.Errorf("exit %d, want 130 with one cancel line and the kill hook; transcript:\n%s", code, out)
	}
}
