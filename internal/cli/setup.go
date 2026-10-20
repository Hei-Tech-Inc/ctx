package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
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

// onInterrupt handles SIGINT (Ctrl+C at a plain prompt, a scan or a tool)
// until the returned stop runs: it puts the terminal back as it was (a hidden
// read may have echo off), prints the cancel line, kills the running
// detectors and exits 130, as Bash's INT trap does. Rich prompts read Ctrl+C
// as a key (tui.ErrCancelled), so it does not reach this handler.
func onInterrupt(t tui.Term) (stop func()) {
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
			setup.KillScans()
			os.Exit(code)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(sig)
		close(done)
	}
}

// SetupIn runs `ctx setup|import|add`, mirroring cmd_import in
// lib/cmd_import.sh: -h/--help prints the reference (setup.Help); without
// --config it runs the interactive wizard (setupWizard). With --config it
// follows the R17 order (every check before any side effect): header,
// preconditions, the account scan, resolution, mkdir of WORK_DIR and
// ssh-keygen, then the shared tail (finishSetup).
func SetupIn(t tui.Term, args []string) int {
	defer onInterrupt(t)()
	u := termUI(t)
	f, msg := parseSetup(args)
	if f.help {
		_, _ = io.WriteString(t.Out, setup.Help(t))
		return 0
	}
	if msg != "" {
		return u.die(msg)
	}
	_, _ = io.WriteString(u.out, tui.Header(t, "ctx v"+os.Getenv("CTX_VERSION")+" — Add a client profile"))
	if !f.batch {
		return setupWizard(t, u, f)
	}

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

	// Without --yes or AUTO_CONFIRM (a TTY stdin; Preconditions dies
	// otherwise) the review asks, as in Bash.
	in.AutoYes = autoYes
	return finishSetup(t, u, newWizard(t, u), &in)
}

// newWizard is the Wizard over t with the real tools and the scan.
func newWizard(t tui.Term, u ui) *setup.Wizard {
	return &setup.Wizard{
		T: t, P: t.Prompter(), U: u.setupOut(),
		DryRun: os.Getenv("CTX_DRY_RUN") == "1",
		PWD:    currentPWD(), Home: os.Getenv("HOME"),
		Run: toolRunner(t, false), RunQuiet: toolRunner(t, true),
	}
}

// toolRunner runs a tool the way a Bash command does: with the terminal's
// stdin, stdout and stderr, or with stderr discarded (2>/dev/null) when quiet.
func toolRunner(t tui.Term, quiet bool) setup.Runner {
	return func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = t.In, t.Out, t.Err
		if quiet {
			cmd.Stderr = nil // the null device
		}
		return cmd.Run()
	}
}

// wizardExit prints a wizard error and returns the exit code: 130 after
// Bash's "Cancelled by user." line for a cancelled prompt, else 1 with the die
// text (and the doctor hint for _dsetup_die errors).
func wizardExit(t tui.Term, u ui, err error) int {
	if errors.Is(err, tui.ErrCancelled) {
		return t.Cancelled()
	}
	return u.setupDie(err)
}

// setupWizard is the interactive part of cmd_import after the header: the
// welcome block, the account scan, then --extend NAME's questions
// (setup.Wizard.Extend) or the mode picker and the wizard steps
// (setup.Wizard.Full, which runs Clone for --scope clone or the picker's
// clone answer), then the shared tail.
func setupWizard(t tui.Term, u ui, f setupFlags) int {
	w := newWizard(t, u)
	in := setup.Inputs{Scope: f.scope, AutoYes: f.autoYes}
	run := w.Full
	if f.extend != "" {
		in.Profile, run = f.extend, w.Extend
	}
	if err := run(&in); err != nil {
		return wizardExit(t, u, err)
	}
	return finishSetup(t, u, w, &in)
}

// finishSetup is the tail cmd_import's batch, clone and full paths share:
// the review (skipped under --dry-run; in.AutoYes prints the "Confirming
// automatically" line instead), ~/.ctx (only after the review, R16), then
// the dry-run report, or the backups, the writes (R4: the secrets, stored
// after the backups — Bash stores them before; parked ruling) and the
// summary. A failed backup dies; a failed write exits 1 after its error line
// (R23).
func finishSetup(t tui.Term, u ui, w *setup.Wizard, in *setup.Inputs) int {
	if w.DryRun {
		setup.DryRunReport(*in, w.U)
		return 0
	}
	if err := w.Review(in); err != nil {
		return wizardExit(t, u, err)
	}
	_ = ctxconfig.InitDirs()
	if err := setup.Backups(*in); err != nil {
		return u.die(err.Error())
	}
	if err := setup.Write(*in, w.U); err != nil {
		return 1 // Write printed the failing step's error line
	}
	setup.Summary(*in, w.U, t.Rich)
	return 0
}
