//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestInstallHookCtrlCLeavesNoTemp: Ctrl+C (SIGINT) while install-hook writes
// the rc leaves no temp file beside it, keeps the rc as it was and exits 130.
// The child (this test binary) blocks inside the write.
func TestInstallHookCtrlCLeavesNoTemp(t *testing.T) {
	if os.Getenv("CTX_TEST_IH_SIGINT") == "1" {
		writeTemp = func(f *os.File, data []byte) error {
			_, _ = f.Write(data[:3])
			_ = os.WriteFile(os.Getenv("CTX_TEST_READY"), nil, 0o644)
			time.Sleep(30 * time.Second)
			return nil
		}
		os.Exit(InstallHook(tui.Term{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, nil))
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstallHookCtrlCLeavesNoTemp$")
	cmd.Env = append(os.Environ(), "CTX_TEST_IH_SIGINT=1", "CTX_TEST_READY="+ready, "HOME="+home,
		"CTX_DIR="+filepath.Join(home, ".ctx"), "SHELL=/bin/zsh", "CTX_LIB="+filepath.Join(repoRoot, "lib"),
		"CTX_NO_GUM=1", "CTX_DRY_RUN=", "PATH="+testutil.SystemPath(t))
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("child never reached the write:\n%s", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = cmd.Process.Signal(os.Interrupt)
	err = cmd.Wait()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	if code != 130 || strings.Count(out.String(), "Cancelled by user.") != 1 {
		t.Errorf("exit %d (%v), want 130 with one cancel line:\n%s", code, err, out.String())
	}
	if b, _ := os.ReadFile(rc); string(b) != "mine\n" {
		t.Errorf(".zshrc = %q", b)
	}
	ents, _ := os.ReadDir(home)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".zshrc.") {
			t.Errorf("temp file left beside the rc: %s", e.Name())
		}
	}
}

// TestSignaledCode: a child killed by SIGINT or SIGTERM gives 130 or 143 (R24);
// a plain failure or success gives 0.
func TestSignaledCode(t *testing.T) {
	for script, want := range map[string]int{"kill -INT $$": 130, "kill -TERM $$": 143, "exit 1": 0, "exit 0": 0} {
		err := exec.Command("/bin/sh", "-c", script).Run()
		if got := signaledCode(err); got != want {
			t.Errorf("%s: signaledCode(%v) = %d, want %d", script, err, got, want)
		}
	}
}
