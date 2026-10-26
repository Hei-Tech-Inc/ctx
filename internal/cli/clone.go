package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/gitssh"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/shquote"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// cloneTokenIsURL is _clone_token_is_url: *://*, git@*:*/* or *:*/* (the
// last subsumes the second).
func cloneTokenIsURL(s string) bool {
	if strings.Contains(s, "://") {
		return true
	}
	c := strings.Index(s, ":")
	return c >= 0 && strings.Contains(s[c+1:], "/")
}

// Clone runs `ctx clone [-p|--profile NAME] [--] [git-clone-args...] <repo-url>
// [dir...]`, mirroring cmd_clone in lib/cmd_ops.sh (R1), and returns git's
// exit code.
//
//   - -p/--profile NAME is consumed anywhere before `--`; the --profile=NAME
//     form is a git argument.
//   - After `--` the URL is the first token that looks like one and git gets
//     the other arguments around it; without `--` the first argument is the URL.
//   - GitHub SSH URLs, and https://github.com/ ones when the user agrees (always
//     with CTX_QUIET=1), are rewritten to host github-<profile>.
//   - git runs with the Term's stdin, stdout and stderr, so credential and ssh
//     prompts work. While it runs ctx-core ignores Ctrl+C: the terminal sends it
//     to git too, and ctx-core waits and returns git's code (130 when git is
//     killed by SIGINT).
//   - --dry-run (CTX_DRY_RUN=1) prints the git command (shell-quoted) and runs
//     nothing, creating no ctx directories. It still asks the https question,
//     which decides the command.
func Clone(t tui.Term, args []string) int {
	u := termUI(t)
	// Ctrl+C at the https question cancels (130); stopped before git starts.
	var once sync.Once
	stop := OnInterrupt(t, nil)
	stopInt := func() { once.Do(stop) }
	defer stopInt()

	prof := ""
	var rest []string
	seenDD := false
	for len(args) > 0 {
		switch args[0] {
		case "-p", "--profile":
			if len(args) < 2 || args[1] == "" {
				return u.die("Usage: ctx clone [-p|--profile <name>] [--] [<git-clone-args>...] <repo-url> [<dir>]")
			}
			prof = args[1]
			args = args[2:]
		case "--":
			seenDD = true
			rest = append(rest, args[1:]...)
			args = nil
		default:
			rest = append(rest, args[0])
			args = args[1:]
		}
	}

	idx := 0
	if seenDD {
		idx = -1
		for i, tok := range rest {
			if cloneTokenIsURL(tok) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return u.die("Could not find a repo URL in: ctx clone ... -- " + strings.Join(rest, " "))
		}
	} else if len(rest) < 1 {
		return u.die("Usage: ctx clone [-p|--profile <name>] <repo-url> [git-clone-args...]")
	}
	url := rest[idx]

	dry := FlagsFromEnv().DryRun
	if !dry {
		initDirs()
	}
	if prof == "" {
		if cfg, err := ctxconfig.Load(); err == nil {
			prof = cfg.Active
		}
	}
	if prof == "" {
		return u.die("No active profile. Run: ctx use <profile> or ctx clone -p <profile> <url>")
	}
	if !profileExists(prof) {
		return u.die("Profile '" + prof + "' not found. Run: ctx list")
	}
	p, err := profile.Load(profile.Dir(), prof)
	if err != nil {
		return u.die("Profile '" + prof + "' not found. Run: ctx list")
	}

	quiet := os.Getenv("CTX_QUIET")
	if quiet == "" {
		quiet = "0"
	}
	if quiet == "0" {
		who := p.GitEmail
		if who == "" {
			who = p.GitName
		}
		if who == "" {
			who = "unknown"
		}
		u.dim("[ctx] cloning as " + prof + " (" + who + ")")
	}

	host := "github-" + prof
	httpsAsSSH := false
	isHTTPS := strings.HasPrefix(url, "https://github.com/")
	if quiet == "1" && isHTTPS {
		httpsAsSSH = true
	} else if isHTTPS {
		yes, err := t.Prompter().AskYN("Use SSH clone via "+host+" instead of HTTPS?", true)
		if err != nil {
			return t.Cancelled()
		}
		if yes {
			httpsAsSSH = true
		} else {
			u.info("Keeping HTTPS URL (uses gh/credential helper, not ctx SSH host).")
		}
	}

	cloneURL := gitssh.CloneURLForProfileOpt(prof, url, httpsAsSSH)
	if cloneURL != url {
		if strings.HasPrefix(url, "git@github.com:") || strings.HasPrefix(url, "ssh://git@github.com/") {
			u.info("Rewrote GitHub SSH URL → " + cloneURL)
		} else if isHTTPS && httpsAsSSH {
			u.info("Using: " + cloneURL)
		}
	}

	// Do not pass the original <repo-url> to git again: a second argument is
	// the target directory.
	var gargs []string
	if seenDD {
		gargs = append(gargs, rest[:idx]...)
		gargs = append(gargs, cloneURL)
		gargs = append(gargs, rest[idx+1:]...)
	} else {
		gargs = append(gargs, cloneURL)
		gargs = append(gargs, rest[1:]...)
	}

	if dry {
		shown := "git clone"
		for _, a := range gargs {
			shown += " " + shquote.Quote(a)
		}
		u.dim("  [dry-run] would run: " + shown)
		return 0
	}

	git, err := exec.LookPath("git")
	if err != nil {
		u.line(u.err, "ctx: git: command not found")
		return 127
	}
	cmd := exec.Command(git, append([]string{"clone"}, gargs...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = t.In, t.Out, t.Err

	// Hand Ctrl+C to git: Notify (not SIG_IGN, which git would inherit) first,
	// then drop the prompt-time handler that would exit before git does.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)
	defer signal.Stop(sig)
	stopInt()

	return childExitCode(u, cmd.Run())
}

// childExitCode is the shell's status for a finished child: its exit code,
// 128+signal when a signal killed it, 1 (after printing the error) when it
// could not be started.
func childExitCode(u ui, err error) int {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	default:
		return u.die(err.Error())
	}
}
