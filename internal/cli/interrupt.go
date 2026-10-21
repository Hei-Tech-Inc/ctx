package cli

import (
	"io"
	"os"
	"os/signal"

	"github.com/Hei-Tech-Inc/ctx/internal/atomicfile"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// OnInterrupt handles SIGINT (Ctrl+C at a plain prompt, a scan or a tool)
// until the returned stop runs: it puts the terminal back as it was (a hidden
// read may have echo off), prints the cancel line, runs kill (when non-nil) to
// stop child processes and exits 130, as Bash's INT trap does. Rich prompts
// read Ctrl+C as a key (tui.ErrCancelled), so it does not reach this handler.
// Commands that exec (run, env) and non-interactive paths do not install it.
// Before exiting it removes the temp file of a write in progress
// (atomicfile.RemoveTemps); the file being written stays as it was.
func OnInterrupt(t tui.Term, kill func()) (stop func()) {
	restore := tui.SaveTTY(t.In)
	sig := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sig, os.Interrupt)
	go func() {
		select {
		case <-sig:
			restore()
			_, _ = io.WriteString(t.Err, "\n")
			code := t.Cancelled()
			if kill != nil {
				kill()
			}
			atomicfile.RemoveTemps()
			os.Exit(code)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(sig)
		close(done)
	}
}

// Flags are the global flags bin/ctx strips from argv and exports.
type Flags struct{ DryRun, Quiet, JSON, NoGum bool }

// FlagsFromEnv reads CTX_DRY_RUN, CTX_QUIET, CTX_JSON and CTX_NO_GUM the way
// bin/ctx exports them (set to 1). CTX_QUIET also follows use.go's rule: set
// and not "0".
func FlagsFromEnv() Flags {
	return Flags{
		DryRun: os.Getenv("CTX_DRY_RUN") == "1",
		Quiet:  os.Getenv("CTX_QUIET") != "" && os.Getenv("CTX_QUIET") != "0",
		JSON:   os.Getenv("CTX_JSON") == "1",
		NoGum:  os.Getenv("CTX_NO_GUM") == "1",
	}
}
