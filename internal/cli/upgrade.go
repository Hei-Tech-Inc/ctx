package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Hei-Tech-Inc/ctx/internal/shquote"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// defaultUpgradeRepo is where upgrade fetches install.sh and lib/core.sh when
// CTX_UPGRADE_REPO (the test seam) is not set.
const defaultUpgradeRepo = "https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main"

// Upgrade runs `ctx upgrade [--check]`, mirroring cmd_upgrade in
// lib/cmd_ops.sh (R11: download install.sh and run it with bash; --check
// reads CTX_VERSION from the remote lib/core.sh).
//
// The target is the ctx on PATH (`command -v ctx`), aimed at by
// CTX_INSTALL_BIN/CTX_INSTALL_LIB only when its first CTX_LIB= line is the
// installer's CTX_LIB="/abs" form. Downloads go through curl, else wget, as in
// Bash, so proxies and certificates behave the same; CRLF is normalized here
// (perl's rule when perl is on PATH, else every \r dropped, as Bash does).
// --dry-run (R1) prints what would be downloaded and run and does nothing.
// The temp file is removed on every path. Ctrl+C at the prompt cancels (130);
// a SIGINT or SIGTERM while curl, wget or the installer runs is handled as
// Bash's traps do: wait for the child, remove the temp file, exit 130 or 143
// without a message. The installer may replace this binary; it is not run
// again afterwards.
func Upgrade(t tui.Term, args []string) int {
	u := termUI(t)
	repo := os.Getenv("CTX_UPGRADE_REPO")
	if repo == "" {
		repo = defaultUpgradeRepo
	}
	url := repo + "/install.sh"

	ctxBin, err := exec.LookPath("ctx")
	if err != nil && !errors.Is(err, exec.ErrDot) {
		ctxBin = ""
	}
	if ctxBin == "" {
		return u.die("ctx not found in PATH — install first: curl -fsSL " + url + " | bash")
	}
	installBin, installLib := "", ""
	if isRegular(ctxBin) {
		if installLib = installedLib(ctxBin); installLib != "" {
			installBin = bashDirname(ctxBin)
		}
	}

	if len(args) > 0 && args[0] == "--check" {
		cur := os.Getenv("CTX_VERSION")
		u.bold("\n  ctx upgrade --check\n")
		u.dim("  Installed: ctx version " + cur)
		// From before the temp file exists until it is gone, SIGINT and
		// SIGTERM wait (curl gets Ctrl+C itself), so they cannot leak it.
		sigs := catchStop()
		defer sigs.stop()
		f, err := os.CreateTemp("", "ctx-upgrade.")
		if err != nil {
			return u.die("Could not create temp file")
		}
		tmp := f.Name()
		_ = f.Close()
		defer func() { _ = os.Remove(tmp) }()
		if tool := fetchTool(); tool != "" {
			_ = fetch(t, tool, repo+"/lib/core.sh", tmp, true)
		}
		remote := ""
		if b, err := os.ReadFile(tmp); err == nil && len(b) > 0 {
			remote = parseCtxVersion(b)
		}
		_ = os.Remove(tmp)
		sigs.stop()
		if c := sigs.caught(); c != 0 {
			return c
		}
		if remote != "" {
			u.dim("  Remote main: ctx version " + remote)
			if cur == remote {
				u.success("Already on latest advertised version (" + remote + ").")
			} else {
				u.info("Update available: " + cur + " → " + remote + " (run: ctx upgrade)")
			}
		} else {
			u.warn("Could not fetch remote version (offline?).")
		}
		u.say("")
		return 0
	}

	u.bold("\n  ctx upgrade\n")
	u.dim("  Source: " + url)
	if installBin != "" && installLib != "" {
		u.dim("  Target: " + ctxBin + " + " + installLib)
	} else {
		u.dim("  Target: default install paths (see installer output)")
	}
	u.say("")
	u.info("Updates ctx binary + libraries only — ~/.ctx profiles, secrets, client dirs, and shell hooks are not modified.")

	tool := fetchTool()
	env := []string{"CTX_REPO=" + repo, "CTX_UPGRADE_ONLY=1"}
	desc := "CTX_REPO=" + shquote.Quote(repo) + " CTX_UPGRADE_ONLY=1"
	if installBin != "" {
		env = append(env, "CTX_INSTALL_BIN="+installBin)
		desc += " CTX_INSTALL_BIN=" + shquote.Quote(installBin)
	}
	if installLib != "" {
		env = append(env, "CTX_INSTALL_LIB="+installLib)
		desc += " CTX_INSTALL_LIB=" + shquote.Quote(installLib)
	}

	if FlagsFromEnv().DryRun {
		if tool == "" {
			return u.die("Need curl or wget to download install.sh")
		}
		u.dim("  [dry-run] would download " + url + " with " + tool)
		u.dim("  [dry-run] would run it with bash: " + desc)
		u.say("")
		return 0
	}

	stopInt := OnInterrupt(t, nil)
	yes, err := t.Prompter().AskYN("Continue?", true)
	stopInt()
	if err != nil {
		return t.Cancelled()
	}
	if !yes {
		return u.die("Aborted.")
	}
	sigs := catchStop()
	defer sigs.stop()

	if tool == "" {
		return u.die("Need curl or wget to download install.sh")
	}
	f, err := os.CreateTemp("", "ctx-upgrade.")
	if err != nil {
		return u.die("Could not create temp file")
	}
	tmp := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(tmp) }()

	ok := fetch(t, tool, url, tmp, false)
	if c := sigs.caught(); c != 0 {
		return c
	}
	if !ok {
		return u.die("Download failed: " + url)
	}
	normalizeNewlines(tmp)
	if c := sigs.caught(); c != 0 {
		return c
	}

	cmd := exec.Command("bash", tmp)
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil // Bash runs a bash found through a relative PATH entry too
	}
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = t.In, t.Out, t.Err
	err = cmd.Run()
	if c := sigs.caught(); c != 0 {
		return c
	}
	if err != nil {
		return u.die("Upgrade installer failed")
	}
	_ = os.Remove(tmp)

	u.say("")
	u.success("Upgrade finished.")
	u.dim("  Open a new terminal or reload your shell if PATH looks stale.")
	u.say("")
	return 0
}

// fetchTool is the downloader Bash picks: curl, else wget, else "".
func fetchTool() string {
	for _, name := range []string{"curl", "wget"} {
		if _, err := exec.LookPath(name); err == nil || errors.Is(err, exec.ErrDot) {
			return name
		}
	}
	return ""
}

// fetch downloads src into dst with tool, as Bash runs it: `curl -fsSL SRC -o
// DST` or `wget -qO DST SRC`, stdout passed through, stderr too unless quiet
// (--check discards it). It reports whether the tool exited 0.
func fetch(t tui.Term, tool, src, dst string, quiet bool) bool {
	var cmd *exec.Cmd
	if tool == "curl" {
		cmd = exec.Command("curl", "-fsSL", src, "-o", dst)
	} else {
		cmd = exec.Command("wget", "-qO", dst, src)
	}
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil // Bash runs a tool found through a relative PATH entry too
	}
	cmd.Stdin, cmd.Stdout = t.In, t.Out
	if !quiet {
		cmd.Stderr = t.Err
	}
	return cmd.Run() == nil
}

// normalizeNewlines rewrites file's line endings as cmd_upgrade does: with
// perl on PATH `s/\r\n/\n/g; s/\r/\n/g`, else `tr -d '\r'`. Failures leave
// the file as it is.
func normalizeNewlines(file string) {
	b, err := os.ReadFile(file)
	if err != nil || bytes.IndexByte(b, '\r') < 0 {
		return
	}
	if _, err := exec.LookPath("perl"); err == nil || errors.Is(err, exec.ErrDot) {
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		b = bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
	} else {
		b = bytes.ReplaceAll(b, []byte("\r"), nil)
	}
	_ = os.WriteFile(file, b, 0o600)
}

// parseCtxVersion mirrors parse_ctx_version_from_core_sh_file: \r dropped,
// the first line starting with CTX_VERSION=, without that prefix and without
// any double quote.
func parseCtxVersion(b []byte) string {
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		if strings.HasPrefix(line, "CTX_VERSION=") {
			return strings.ReplaceAll(strings.TrimPrefix(line, "CTX_VERSION="), `"`, "")
		}
	}
	return ""
}

// stopSignals holds SIGINT and SIGTERM while a child runs: the child (in the
// terminal's process group) gets Ctrl+C itself, and this process exits only
// after it, with Bash's codes (130 for INT, 143 for TERM).
type stopSignals struct {
	ch   chan os.Signal
	code int
}

func catchStop() *stopSignals {
	s := &stopSignals{ch: make(chan os.Signal, 2)}
	signal.Notify(s.ch, os.Interrupt, syscall.SIGTERM)
	return s
}

// caught is 130 or 143 when SIGINT or SIGTERM arrived (the first one wins),
// else 0.
func (s *stopSignals) caught() int {
	for s.code == 0 {
		select {
		case sig := <-s.ch:
			if sig == syscall.SIGTERM {
				s.code = 143
			} else {
				s.code = 130
			}
		default:
			return 0
		}
	}
	return s.code
}

func (s *stopSignals) stop() { signal.Stop(s.ch) }
