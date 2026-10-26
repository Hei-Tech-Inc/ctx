package main

import (
	"io"
	"os"
	"os/signal"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// parseGlobal strips the global flags (--dry-run, --quiet|-q, --no-gum,
// --json) from args up to the first "--", which stays in rest, as bin/ctx's
// flag loop does. env holds what bin/ctx exports: CTX_DRY_RUN and CTX_JSON
// always ("0" or "1"), CTX_QUIET ("1" with -q, else the inherited value or
// "0") and CTX_NO_GUM=1 only with --no-gum.
func parseGlobal(args []string) (rest []string, env map[string]string) {
	quiet := os.Getenv("CTX_QUIET")
	if quiet == "" {
		quiet = "0"
	}
	env = map[string]string{"CTX_DRY_RUN": "0", "CTX_JSON": "0", "CTX_QUIET": quiet}
	for i, a := range args {
		switch a {
		case "--":
			return append(rest, args[i:]...), env
		case "--dry-run":
			env["CTX_DRY_RUN"] = "1"
		case "--quiet", "-q":
			env["CTX_QUIET"] = "1"
		case "--no-gum":
			env["CTX_NO_GUM"] = "1"
		case "--json":
			env["CTX_JSON"] = "1"
		default:
			rest = append(rest, a)
		}
	}
	return rest, env
}

// ctxAliases maps the alternative command names bin/ctx accepts.
var ctxAliases = map[string]string{
	"ls": "list", "st": "status", "rm": "remove", "del": "remove",
	"import": "setup", "add": "setup",
	"-v": "version", "--version": "version", "-h": "help", "--help": "help",
}

// ctxCommands are the commands passed to run with all their arguments, as
// bin/ctx forwards them.
var ctxCommands = map[string]bool{
	"setup": true, "use": true, "deactivate": true, "activate": true, "hook-env": true,
	"clone": true, "workdir-prompt": true, "config": true, "secret": true, "verify": true,
	"edit": true, "remove": true, "undo": true, "install-hook": true, "upgrade": true,
	"uninstall": true, "doctor": true, "guard": true, "env": true, "run": true,
	"agent-rules": true, "completion": true, "help": true, "mcp": true, "knowledge": true,
}

// ownsInterrupt are the commands that handle SIGINT themselves (a cancel
// line, waiting for a child, or exec); every other command exits 130 on it.
var ownsInterrupt = map[string]bool{
	"setup": true, "clone": true, "init": true, "install-hook": true, "uninstall": true,
	"undo": true, "upgrade": true, "edit": true, "mcp": true, "env": true, "run": true,
}

// runCtx is the user-facing `ctx`: what bin/ctx does before and during
// dispatch (global flags, aliases, the unknown-command error), then run.
func runCtx(args []string, stdout, stderr io.Writer) int {
	rest, env := parseGlobal(args)
	for k, v := range env {
		_ = os.Setenv(k, v)
	}
	cmd := "help"
	if len(rest) > 0 {
		cmd, rest = rest[0], rest[1:]
	}
	if a, ok := ctxAliases[cmd]; ok {
		cmd = a
	}
	if !ownsInterrupt[cmd] {
		defer exitOnInterrupt()()
	}
	switch {
	case cmd == "list" || cmd == "status":
		// bin/ctx passes neither command its arguments; --json is global.
		if env["CTX_JSON"] == "1" {
			return run([]string{cmd, "--json"}, stdout, stderr)
		}
		return run([]string{cmd}, stdout, stderr)
	case cmd == "init":
		return run([]string{"init"}, stdout, stderr)
	case cmd == "version":
		return run([]string{"ctx-version"}, stdout, stderr)
	case ctxCommands[cmd]:
		return run(append([]string{cmd}, rest...), stdout, stderr)
	}
	// bin/ctx's error helper: red mark when stdout is a terminal, echo -e.
	red, reset := "", ""
	if tui.TTYColors(stdout) {
		red, reset = "\033[0;31m", "\033[0m"
	}
	_, _ = io.WriteString(stderr, tui.EchoE(red+"✗"+reset+" Unknown command: "+cmd))
	_, _ = io.WriteString(stdout, "\n  Run 'ctx help' for usage.\n\n")
	return 1
}

// exitOnInterrupt makes SIGINT exit 130, as bin/ctx's `trap 'exit 130' INT`,
// until stop runs.
func exitOnInterrupt() (stop func()) {
	sig := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sig, os.Interrupt)
	go func() {
		select {
		case <-sig:
			os.Exit(130)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(sig)
		close(done)
	}
}
