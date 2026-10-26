package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// StatusText prints `ctx status` (text mode), mirroring cmd_status in
// lib/cmd_ops.sh, and returns the exit code.
func StatusText(t tui.Term) int {
	u := termUI(t)
	initDirs()
	cfg, _ := ctxconfig.Load()
	if cfg == nil {
		cfg = &ctxconfig.Config{}
	}
	current := cfg.Active

	_, _ = io.WriteString(u.out, tui.Header(t, "ctx status"))

	if current == "" {
		u.warn("No active profile. Run: ctx use <profile>")
		u.say("")
		return 0
	}
	p, err := profile.Load(profile.Dir(), current)
	if err != nil {
		return u.die(fmt.Sprintf("Profile '%s' not found. Run: ctx list", current))
	}
	pwd, _ := os.Getwd()

	var note string
	if cfg.ActiveSource == "manual" {
		if cfg.ManualPWD != "" && pwd == cfg.ManualPWD {
			note = "Activation: manual (locked at this directory until you cd elsewhere)"
		} else {
			note = "Activation: manual anchor cleared — directory auto-switch applies on cd"
		}
	} else {
		note = "Activation: auto (directory hook + longest WORK_DIR prefix)"
	}
	if v := os.Getenv("CTX_ACTIVE_PROFILE"); v != "" {
		note += " · CTX_ACTIVE_PROFILE=" + v
	}

	sshKey := ""
	if p.SSHKeyPath != "" {
		sshKey = filepath.Base(p.SSHKeyPath)
	}
	secrets := p.SecretKeysRaw
	if secrets == "" {
		secrets = "none"
	}
	git := orDash(p.GitName) + " <" + orDash(p.GitEmail) + ">"
	if t.Rich {
		lines := []string{
			"Profile  : " + lipgloss.NewStyle().Bold(true).Render(current),
			"Git      : " + git,
			"Dir      : " + orDash(p.WorkDir),
			"GitHub   : " + orDash(p.GithubUser),
			"SSH key  : " + sshKey,
			"AWS      : " + orDash(p.AWSProfile),
			"Azure    : " + orDash(p.AzureSubscription),
			"GCP      : " + orDash(p.GCPProject),
			"kubectl  : " + orDash(p.KubeContext),
			"Secrets  : " + secrets,
		}
		_, _ = io.WriteString(u.out, tui.Box(lines))
		u.dim("  " + note)
	} else {
		hr := func() { u.line(u.out, u.c(ansiDim)+strings.Repeat("─", 40)+u.c(ansiReset)) }
		hr()
		u.line(u.out, "  Profile  : "+u.c(ansiBold)+current+u.c(ansiReset))
		u.say("  Git      : " + git)
		u.say("  Dir      : " + orDash(p.WorkDir))
		u.say("  GitHub   : " + orDash(p.GithubUser))
		u.say("  SSH key  : " + sshKey)
		u.say("  AWS      : " + orDash(p.AWSProfile))
		u.say("  Azure    : " + orDash(p.AzureSubscription))
		u.say("  GCP      : " + orDash(p.GCPProject))
		u.say("  kubectl  : " + orDash(p.KubeContext))
		u.say("  Secrets  : " + secrets)
		u.dim("  " + note)
		hr()
	}

	u.say("")
	u.bold("  Live checks")
	u.say("")
	liveChecks(u, current, p, pwd)
	u.say("")
	return 0
}

func liveChecks(u ui, current string, p *profile.Profile, pwd string) {
	// gh account
	if has("gh") && p.GithubUser != "" {
		out, _ := exec.Command("gh", "auth", "status").CombinedOutput()
		activeGH := ""
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, "Logged in to github.com account") {
				continue
			}
			// The word after "account", parentheses removed.
			f := strings.Fields(line)
			for i := 0; i+1 < len(f); i++ {
				if f[i] == "account" {
					activeGH = strings.NewReplacer("(", "", ")", "").Replace(f[i+1])
					break
				}
			}
			break
		}
		if activeGH == p.GithubUser {
			u.success("gh account matches (" + activeGH + ")")
		} else {
			u.warn("gh active: '" + activeGH + "', expected: '" + p.GithubUser + "' — run: ctx use " + current)
		}
	}

	// SSH
	if p.SSHKeyPath != "" {
		if !isRegular(p.SSHKeyPath) {
			u.warn("SSH key file not found: " + p.SSHKeyPath)
		} else {
			out, _ := exec.Command("ssh-add", "-l").Output()
			if strings.Contains(string(out), p.SSHKeyPath) {
				u.success("SSH key loaded in agent")
			} else {
				u.warn("SSH key not in agent — run: ctx use " + current)
			}
		}
	}

	// mise.toml
	if p.WorkDir != "" && has("mise") {
		if isRegular(p.WorkDir + "/mise.toml") {
			u.success("mise.toml present: " + p.WorkDir + "/mise.toml")
		} else {
			u.warn("mise.toml not found at " + p.WorkDir + "/mise.toml")
			u.info("Run: ctx use " + current + "  (to regenerate)")
		}
	}

	// AWS
	if has("aws") && p.AWSProfile != "" {
		if exec.Command("aws", "configure", "list", "--profile", p.AWSProfile).Run() == nil {
			u.success("AWS profile valid: " + p.AWSProfile)
		} else {
			u.warn("AWS profile not found: " + p.AWSProfile)
		}
	}

	// Git identity in the current directory
	if p.WorkDir != "" && strings.HasPrefix(pwd, p.WorkDir) {
		out, _ := exec.Command("git", "config", "user.email").Output()
		email := strings.TrimRight(string(out), "\n")
		if email == p.GitEmail {
			u.success("Git identity in current dir: " + email)
		} else {
			u.warn("Git email here: '" + email + "' (expected '" + p.GitEmail + "')")
			u.info("Are you inside " + p.WorkDir + ", or did you run: mise trust " + p.WorkDir + "/mise.toml?")
		}
	}
}
