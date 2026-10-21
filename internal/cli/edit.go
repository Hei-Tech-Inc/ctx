package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// Edit runs `ctx edit <profile>`: it opens the profile's .conf in $VISUAL,
// else $EDITOR, else nano, run like git runs it (sh -c '<editor> "$@"'), so
// EDITOR="code -w" works. The sh catches SIGINT (trap : INT, which is default
// again in the editor): Debian's dash stays the editor's parent and would
// otherwise die by a Ctrl+C the editor handles. Not `exec <editor>`, which
// breaks EDITOR="FOO=1 vim". The editor gets the Term's stdin, stdout and stderr,
// and Edit returns its exit code. --dry-run does not apply: opening an editor
// is the user's action. Edit does not prompt, so it installs no interrupt
// handler; it only keeps Ctrl+C for the editor while it runs.
func Edit(t tui.Term, args []string) int {
	u := termUI(t)
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return u.die("Usage: ctx edit <profile>")
	}
	if !profileExists(name) {
		return u.die("Profile '" + name + "' not found.")
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "nano"
	}
	cmd := exec.Command("sh", "-c", "trap : INT; "+editor+` "$@"`, "sh", profile.Dir()+"/"+name+".conf")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = t.In, t.Out, t.Err

	// The terminal sends Ctrl+C to the editor too; ctx-core waits for it
	// instead of dying first (a handler, not SIG_IGN, which the editor would inherit).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)
	defer signal.Stop(sig)

	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	default: // sh could not be started
		return u.die(err.Error())
	}
}
