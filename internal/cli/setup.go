package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"golang.org/x/term"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/setup"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// setupFlags is what cmd_import's argument loop collects.
type setupFlags struct {
	config  string // --config FILE
	batch   bool   // --config was given
	autoYes bool   // -y / --yes
	scope   string // --scope clone|full (default full)
	extend  string // --extend NAME
	help    bool   // -h / --help (Bash prints the help and exits 0)
}

const setupUsage = "usage: ctx setup [--scope clone|full] [--extend NAME] [--config FILE] [-y|--yes]"

// parseSetup mirrors the argument loop of cmd_import (lib/cmd_import.sh) and
// the two checks after it. It stops at -h/--help (help) or at the first
// error, returning Bash's die text.
func parseSetup(args []string) (f setupFlags, dieMsg string) {
	f.scope = "full"
	for i := 0; i < len(args); i++ {
		next := ""
		if i+1 < len(args) {
			next = args[i+1]
		}
		switch a := args[i]; a {
		case "-h", "--help":
			f.help = true
			return f, ""
		case "--config":
			if next == "" {
				return f, "setup: --config requires a file path"
			}
			f.config, f.batch = next, true
			i++
		case "--scope":
			if next == "" {
				return f, "setup: --scope requires clone or full"
			}
			if next != "clone" && next != "full" {
				return f, "setup: --scope must be clone or full"
			}
			f.scope = next
			i++
		case "--extend":
			if next == "" {
				return f, "setup: --extend requires a profile name"
			}
			f.extend = next
			i++
		case "-y", "--yes":
			f.autoYes = true
		default:
			return f, "setup: unknown option '" + a + "' — " + setupUsage
		}
	}
	if f.extend != "" && f.batch {
		return f, "setup: --extend cannot be used with --config (use ctx edit or a full --config run)"
	}
	if f.scope == "clone" && f.extend != "" {
		return f, "setup: --extend cannot be combined with --scope clone"
	}
	return f, ""
}

// SetupImplemented reports whether ctx-core runs `ctx setup args...`: only
// --config FILE together with --yes/-y or a truthy AUTO_CONFIRM in FILE (the
// interactive review is not ported yet). Help, argument errors and an
// unreadable FILE are left to Bash, which reports them.
func SetupImplemented(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return false
		}
	}
	f, msg := parseSetup(args)
	if msg != "" || f.help || !f.batch {
		return false
	}
	c, err := setup.ReadConfig(f.config)
	if err != nil {
		return false
	}
	return f.autoYes || setup.Truthy(c.Get("AUTO_CONFIRM", "false"))
}

// isTTYReader is [[ -t 0 ]] for t.In.
func isTTYReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// hintDoctor is ctx_setup_hint_doctor (stderr).
func (u ui) hintDoctor() {
	u.line(u.err, u.c(ansiDim)+"  Tools, hooks, SSH layout: run "+u.c(ansiBold)+"ctx doctor"+u.c(ansiReset)+u.c(ansiDim)+"."+u.c(ansiReset)+u.c(ansiReset))
}

// setupDie prints err like die (or _dsetup_die for a DieError with Hint) and
// returns 1.
func (u ui) setupDie(err error) int {
	var de *setup.DieError
	if errors.As(err, &de) && de.Hint {
		u.error(de.Msg)
		u.hintDoctor()
		return 1
	}
	return u.die(err.Error())
}

func notPorted(t tui.Term) int {
	fmt.Fprintln(t.Err, "ctx-core: setup supports only --config FILE with --yes or AUTO_CONFIRM")
	return 127
}

// SetupIn runs `ctx setup|import|add` for --config FILE, mirroring cmd_import
// in lib/cmd_import.sh with the R17 order (every check before any side
// effect): header, preconditions, the account scan, resolution, mkdir of
// WORK_DIR and ssh-keygen, the auto-confirmed review, ~/.ctx, then either the
// dry-run report or the backups, the writes and the summary. Help and the
// interactive wizard are not ported (exit 127; `implements` keeps them in Bash).
func SetupIn(t tui.Term, args []string) int {
	u := termUI(t)
	f, msg := parseSetup(args)
	if f.help {
		return notPorted(t)
	}
	if msg != "" {
		return u.die(msg)
	}
	if !f.batch {
		return notPorted(t)
	}
	_, _ = io.WriteString(u.out, tui.Header(t, "ctx v"+os.Getenv("CTX_VERSION")+" — Add a client profile"))

	c, err := setup.ReadConfig(f.config)
	if err != nil {
		return u.setupDie(err)
	}
	dry := os.Getenv("CTX_DRY_RUN") == "1"
	home := os.Getenv("HOME")
	opts := setup.BatchOpts{
		AutoYes: f.autoYes, DryRun: dry, PWD: currentPWD(), Home: home, WorkRoot: ctxconfig.WorkRoot(),
		StdinIsTTY: isTTYReader(t.In), Exists: profileExists, FileExists: isRegular,
	}
	scope, autoYes, err := setup.Preconditions(c, opts)
	if scope != "" {
		u.info("Non-interactive setup from: " + f.config + " (scope: " + scope + ")")
	}
	if err != nil {
		return u.setupDie(err)
	}
	if !autoYes && !dry {
		return notPorted(t) // the interactive review (TTY stdin) is still Bash
	}

	so := u.setupOut()
	var spin func()
	if t.Rich {
		spin = func() {} // gum spin leaves nothing on screen
	}
	setup.ScanIntro(so, spin)
	found := setup.Detect(home, 8*time.Second)
	u.say("")
	found.Report(so)
	u.say("")

	in, err := setup.Resolve(c, opts)
	if err != nil {
		return u.setupDie(err)
	}
	if in.NeedsMkdirWorkDir {
		cmd := exec.Command("mkdir", "-p", in.WorkDir)
		cmd.Stdout, cmd.Stderr = t.Out, t.Err
		if cmd.Run() != nil {
			return u.setupDie(&setup.DieError{Msg: "Could not create WORK_DIR: " + in.WorkDir, Hint: true})
		}
	}
	if in.NeedsKeygen {
		sshDir := home + "/.ssh"
		_ = os.MkdirAll(sshDir, 0o777)
		_ = os.Chmod(sshDir, 0o700)
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-C", setup.KeyComment(in.Profile, in.GitEmail),
			"-f", in.SSHKeyPath, "-N", "", "-q")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = t.In, t.Out, t.Err
		if cmd.Run() != nil {
			return u.setupDie(&setup.DieError{Msg: "ssh-keygen failed for " + in.SSHKeyPath, Hint: true})
		}
		_ = os.Chmod(in.SSHKeyPath, 0o600)
	}

	if !dry {
		u.info("Confirming automatically (--yes or AUTO_CONFIRM in config).")
		_ = ctxconfig.InitDirs()
	} else {
		setup.DryRunReport(in, so)
		return 0
	}
	if err := setup.Backups(in); err != nil {
		return u.die(err.Error())
	}
	if err := setup.Write(in, so); err != nil {
		return 1 // Write printed the failing step's error line
	}
	setup.Summary(in, so, t.Rich)
	return 0
}
