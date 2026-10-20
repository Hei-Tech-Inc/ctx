package setup

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// Runner runs an external tool (ssh-keygen, gh) and returns its error, the way
// a Bash command runs it.
type Runner func(name string, args ...string) error

// Wizard holds what the interactive setup steps share: the terminal and its
// prompter, the styled output, the account scan, and the injected world.
type Wizard struct {
	T      tui.Term
	P      tui.Prompter
	U      Out
	Det    Detected
	DryRun bool
	// PWD is Bash's $PWD; Home is $HOME.
	PWD, Home string
	// Run runs a tool with the terminal's stdin, stdout and stderr. RunQuiet
	// is the same with stderr discarded (Bash's 2>/dev/null); nil means Run.
	Run, RunQuiet Runner
	// Hostname and Now name the uploaded GitHub key (`hostname`, `date`);
	// nil means os.Hostname and time.Now.
	Hostname func() string
	Now      func() time.Time
	// HasTool is Bash's `has` (command -v); nil means a PATH lookup.
	HasTool func(name string) bool
	// Output is a tool's stdout as `$(name args 2>/dev/null)` captures it
	// (trailing newlines dropped; a failure gives what was printed). nil
	// runs the tool with no stdin.
	Output func(name string, args ...string) string
	// Detect is the account scan and DetectGH its gh part (re-run after
	// `gh auth login`); nil means Detect(Home, 8s) and the gh detector with
	// the same timeout.
	Detect   func() Detected
	DetectGH func() []string
}

// scanTimeout bounds gh, az and gcloud during the scan (run_with_timeout 8).
const scanTimeout = 8 * time.Second

func (w *Wizard) output(name string, args ...string) string {
	if w.Output != nil {
		return w.Output(name, args...)
	}
	cmd := exec.Command(name, args...)
	out, _ := cmd.Output()
	return strings.TrimRight(string(out), "\n")
}

func (w *Wizard) detectGH() []string {
	if w.DetectGH != nil {
		return w.DetectGH()
	}
	return detectGH(scanTimeout)
}

// Scan is cmd_import's account scan: the intro lines (rich mode spins while
// detecting, where Bash runs gum spin), the detection, and the Found/No
// lines between blank lines. The result is in w.Det.
func (w *Wizard) Scan() {
	detect := w.Detect
	if detect == nil {
		detect = func() Detected { return Detect(w.Home, scanTimeout) }
	}
	scanned := false
	var spin func()
	if w.T.Rich {
		spin = func() {
			tui.Spin(w.T, "Scanning your machine for existing credentials...", func() {
				w.Det, scanned = detect(), true
			})
		}
	}
	ScanIntro(w.U, spin)
	if !scanned {
		w.Det = detect()
	}
	w.U.Say("")
	w.Det.Report(w.U)
	w.U.Say("")
}

func (w *Wizard) runQuiet(name string, args ...string) error {
	if w.RunQuiet != nil {
		return w.RunQuiet(name, args...)
	}
	return w.Run(name, args...)
}

func (w *Wizard) hostname() string {
	if w.Hostname != nil {
		return w.Hostname()
	}
	h, _ := os.Hostname()
	return h
}

func (w *Wizard) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Wizard) hasTool(name string) bool {
	if w.HasTool != nil {
		return w.HasTool(name)
	}
	return hasTool(name)
}

// dry prints _ctx_setup_dry's "  [dry-run] would <action>" line.
func (w *Wizard) dry(action string) { w.U.Dim("  [dry-run] would " + action) }

// block writes s, a newline-terminated block (a step banner, the review
// table), through Out.
func (w *Wizard) block(s string) { w.U.Say(strings.TrimSuffix(s, "\n")) }

// hr is lib/core.sh's hr.
func (w *Wizard) hr() { w.U.Dim(strings.Repeat("─", 40)) }

// Welcome prints cmd_import's welcome block (interactive setup only).
func (w *Wizard) Welcome() {
	b, r, d := "", "", ""
	if colors(w.U) {
		b, r, d = ansiBold, ansiReset, "\033[2m"
	}
	w.hr()
	w.U.Bold("  Welcome")
	w.U.Dim("  Full setup: identity → folder → SSH/GitHub → cloud → secrets → review.")
	w.U.Dim("  Clone-only: " + b + "ctx setup --scope clone" + r + d + " — profile + SSH host for " + b + "ctx clone" + r + d +
		"; add git/cloud later with " + b + "ctx setup --extend <name>" + r + d + ".")
	w.U.Dim("  Stuck mid-flow? Run " + b + "ctx doctor" + r + d + " (tools + shell hooks + SSH)." + r)
	w.U.Dim("  Scanning runs once so we can suggest keys and accounts.")
	w.U.Say("")
}

// echoCaptured is what `$(echo "$s")` captures.
func echoCaptured(s string) string {
	if echoSwallowed(s) {
		return ""
	}
	return strings.TrimRight(s, "\n")
}

// GenKey ports _gen_ssh_key: an existing key is kept (warning); under
// --dry-run only the dry-run line prints (R2, R12: no passphrase or GitHub
// pause). Otherwise it asks for an optional passphrase, creates ~/.ssh (mode
// 700) as the batch path does, runs
// `ssh-keygen -t ed25519 -C comment -f path -N pass -q` (the passphrase is on
// argv, as in Bash), sets mode 600, shows the public key and waits for Enter.
// The answer to that pause is echoed to stdout, as Bash's uncaptured ask does.
// A failing ssh-keygen is a *DieError with the doctor hint (R25).
func (w *Wizard) GenKey(path, comment string) error {
	if isFile(path) {
		w.U.Warn("Key exists at " + path + " — skipping generation.")
		return nil
	}
	if w.DryRun {
		w.dry("generate SSH key " + path)
		return nil
	}
	pass := ""
	protect, err := w.P.AskYN("Protect this SSH key with a passphrase?", true)
	if err != nil {
		return err
	}
	if protect {
		p, err := w.P.AskSecret("Passphrase (leave blank to use none)")
		if err != nil {
			return err
		}
		pass = echoCaptured(p)
	}
	// ssh-keygen does not create ~/.ssh (mkdir -p; chmod 700, as the batch path).
	sshDir := w.Home + "/.ssh"
	_ = os.MkdirAll(sshDir, 0o777)
	_ = os.Chmod(sshDir, 0o700)
	if w.Run("ssh-keygen", "-t", "ed25519", "-C", comment, "-f", path, "-N", pass, "-q") != nil {
		return &DieError{Msg: "ssh-keygen failed for " + path, Hint: true}
	}
	chmod := exec.Command("chmod", "600", path)
	chmod.Stdout, chmod.Stderr = w.T.Out, w.T.Err
	_ = chmod.Run()
	w.U.Success("SSH key generated: " + path)
	w.U.Say("")
	w.U.Bold("  Add this public key to GitHub:")
	w.U.Say("")
	w.cat(path + ".pub")
	w.U.Say("")
	w.U.Info("Open: https://github.com/settings/ssh/new")
	ans, err := w.P.Ask("Press Enter after adding the key to GitHub...", "", "")
	if err != nil {
		return err
	}
	w.U.Say(ans)
	return nil
}

// cat writes the file like cat: its bytes to stdout, or cat's error to stderr.
func (w *Wizard) cat(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		msg := "No such file or directory"
		if !errors.Is(err, os.ErrNotExist) {
			msg = errText(err)
		}
		_, _ = io.WriteString(w.T.Err, "cat: "+path+": "+msg+"\n")
		return
	}
	if s := string(data); strings.HasSuffix(s, "\n") {
		w.U.Say(strings.TrimSuffix(s, "\n"))
	} else {
		_, _ = io.WriteString(w.T.Out, s)
	}
}

// UploadKey switches gh to user and adds pub as
// ctx-<profile>-<hostname>-<YYYYMMDD>, both with stderr discarded, then
// reports the result (the clone wizard's wording). A failed upload is a
// warning, not an error, as in Bash; the error is reserved for callers that
// need one.
func (w *Wizard) UploadKey(user, pub, profile string) error {
	w.upload(user, pub, profile, "Add it at https://github.com/settings/keys")
	return nil
}

// upload is UploadKey with the failure hint of the calling flow (the full
// wizard says "Add it manually at …").
func (w *Wizard) upload(user, pub, profile, hint string) {
	_ = w.runQuiet("gh", "auth", "switch", "-u", user)
	title := "ctx-" + profile + "-" + w.hostname() + "-" + w.now().Format("20060102")
	if w.runQuiet("gh", "ssh-key", "add", pub, "--title", title) == nil {
		w.U.Success("SSH key uploaded to GitHub (" + user + ")")
	} else {
		w.U.Warn("Could not upload key automatically. " + hint)
	}
}

// pubKeyReady is _ctx_setup_pub_key_ready: the profile's public key exists,
// or would exist (generated under --dry-run).
func (w *Wizard) pubKeyReady(in *Inputs) bool {
	return isFile(in.SSHKeyPath+".pub") || w.DryRun && in.SSHKeyGenerated
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// Review ports ctx_setup_review_confirm: with in.AutoYes the "Confirming
// automatically" line, otherwise the "Step 8 / 8 — Review" banner, the table
// (plain and rich labels differ, as in Bash) and "Write this profile to disk
// now?". No is a *DieError "Aborted before write." (Bash: die, exit 1).
func (w *Wizard) Review(in *Inputs) error {
	if in.AutoYes {
		w.U.Info("Confirming automatically (--yes or AUTO_CONFIRM in config).")
		return nil
	}
	imp := "Import detected references"
	if !in.ImportExisting {
		imp = "Manual entry only"
	}
	key := in.SSHKeyPath
	if key == "" {
		key = "(none)"
	}
	labels := [13]string{"Profile", "Git name", "Git email", "Work directory", "Config import", "SSH key",
		"GitHub user", "AWS profile", "Azure sub", "Azure tenant", "GCP project", "GCP account", "kubectl"}
	if w.T.Rich {
		labels[5], labels[8], labels[12] = "SSH private key", "Azure subscription", "kubectl context"
	}
	values := [13]string{in.Profile, in.GitName, orDash(in.GitEmail), in.WorkDir, imp, key,
		orDash(in.GitHubUser), orDash(in.AWSProfile), orDash(in.AzureSub), orDash(in.AzureTenant),
		orDash(in.GCPProject), orDash(in.GCPAccount), orDash(in.KubeContext)}
	rows := make([][2]string, len(labels))
	for i := range labels {
		rows[i] = [2]string{labels[i], values[i]}
	}

	w.hr()
	w.U.Bold("  Step 8 / 8 — Review")
	w.U.Dim("  Nothing is written until you confirm. No = exit without changes.")
	w.U.Say("")
	w.block(tui.ReviewTable(w.T, rows))
	w.U.Say("")
	ok, err := w.P.AskYN("Write this profile to disk now?", true)
	if err != nil {
		return err
	}
	if !ok {
		return &DieError{Msg: "Aborted before write."}
	}
	return nil
}
