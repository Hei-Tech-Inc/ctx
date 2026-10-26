package cli

import (
	"os"
	"os/exec"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/ghacct"
	"github.com/Hei-Tech-Inc/ctx/internal/gitssh"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/setup"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// Verify runs `ctx verify [profile]`, mirroring cmd_verify in lib/cmd_ops.sh
// (with R3), and returns the exit code: 0 when every check passes, 1 when any
// fails or the profile cannot be found. Only args[0] is read. The profile
// defaults to the active one. The live check is
// `ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new
// -T git@github-<profile>`: it never waits for a passphrase or host-key
// answer. The gh account is the active one (ghacct.Active). Verify installs no
// interrupt handler: Ctrl+C ends it, and the tty stops ssh and gh with it.
func Verify(t tui.Term, args []string) int {
	u := termUI(t)
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	initDirs()
	if name == "" {
		if cfg, err := ctxconfig.Load(); err == nil {
			name = cfg.Active
		}
	}
	if name == "" {
		return u.die("Usage: ctx verify <profile> (or activate a profile first)")
	}
	if !profileExists(name) {
		return u.die("Profile '" + name + "' not found.")
	}
	p, err := profile.Load(profile.Dir(), name)
	if err != nil {
		return u.die("Profile '" + name + "' not found. Run: ctx list")
	}

	u.bold("\n  ctx verify: " + name + "\n")
	u.dim("  One profile — live ssh -T to github-" + name + ", gh vs profile, paths, email.")
	u.say("")

	ok := true
	host := "github-" + name

	if st, err := os.Stat(p.WorkDir); p.WorkDir != "" && err == nil && st.IsDir() {
		u.success("Work dir exists: " + p.WorkDir)
	} else {
		u.warn("Work dir missing: " + p.WorkDir)
		ok = false
	}

	if p.SSHKeyPath != "" {
		keyOK := false
		if st, err := os.Stat(p.SSHKeyPath); err == nil && st.Mode().IsRegular() {
			keyOK = true
			u.success("SSH private key: " + p.SSHKeyPath)
		} else {
			u.warn("SSH private key missing: " + p.SSHKeyPath)
			ok = false
		}
		if gitssh.HasHost(host) {
			u.success("SSH config has Host " + host)
		} else {
			u.warn("SSH config missing Host " + host + " in " + gitssh.SSHConfigPath())
			ok = false
		}
		if _, err := exec.LookPath("ssh"); err == nil && keyOK {
			out := runCombined(t, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
				"-o", "StrictHostKeyChecking=accept-new", "-T", "git@"+host)
			if strings.Contains(strings.ToLower(out), "successfully authenticated") {
				u.success("ssh -T git@" + host + ": OK")
			} else {
				u.warn("ssh -T git@" + host + " failed or unexpected: " + strings.ReplaceAll(out, "\n", " "))
				ok = false
			}
		}
	} else {
		u.warn("No SSH_KEY_PATH in profile")
		ok = false
	}

	if p.GithubUser != "" {
		if _, err := exec.LookPath("gh"); err == nil {
			active := ghacct.Active(runCombined(t, "gh", "auth", "status"))
			if active == p.GithubUser {
				u.success("gh active user matches: " + p.GithubUser)
			} else {
				u.warn("gh active: '" + active + "', profile expects: '" + p.GithubUser + "' — run: ctx use " + name)
				ok = false
			}
		}
	}

	if p.GitEmail != "" {
		if setup.SensibleEmail(p.GitEmail) {
			u.success("GIT_EMAIL looks valid: " + p.GitEmail)
		} else {
			u.warn("GIT_EMAIL looks invalid: " + p.GitEmail)
			ok = false
		}
	}

	if ok {
		u.success("Profile verification passed.")
	} else {
		u.warn("Profile verification found issues.")
	}
	u.say("")
	if !ok {
		return 1
	}
	return 0
}

// runCombined is `$(cmd 2>&1 || true)`: stdout and stderr in one stream, the
// exit status ignored, trailing newlines dropped. The command reads the
// Term's stdin like it would from the Bash script.
func runCombined(t tui.Term, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Stdin = t.In
	b, _ := cmd.CombinedOutput()
	return strings.TrimRight(string(b), "\n")
}
