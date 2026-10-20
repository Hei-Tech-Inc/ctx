package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/gitssh"
	"github.com/Hei-Tech-Inc/ctx/internal/guard"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// CoreVersion is the ctx-core build version doctor reports (set by main from
// its -ldflags version).
var CoreVersion = "dev"

// coreExecutable is the path doctor reports for ctx-core: the running binary.
// Tests replace it.
var coreExecutable = os.Executable

// sshIncludeRe is grep -E '^[[:space:]]*Include[[:space:]]+.*ctx_config', per line.
var sshIncludeRe = regexp.MustCompile(`^[ \t\n\v\f\r]*Include[ \t\n\v\f\r]+.*ctx_config`)

// Doctor runs `ctx doctor`, mirroring cmd_doctor in lib/cmd_ops.sh (with R4),
// and returns the exit code, always 0: problems are reported, not signalled.
// Arguments are ignored. doctor only reads (tool versions, rc files, SSH
// config, profiles) and asks nothing, so it installs no interrupt handler:
// Ctrl+C simply ends it.
//
// Tool versions are read the way Bash does: `bash -lc "<tool> --version"`
// (az and kubectl have their own command), first output line, stdout and
// stderr together. ctx-core is the running binary, so the "missing" warning
// of the Bash version cannot occur here. Profiles are read with profile.Load,
// not sourced (Bash executes the .conf in-process).
func Doctor(t tui.Term, _ []string) int {
	u := termUI(t)
	_, _ = io.WriteString(u.out, tui.Header(t, "ctx doctor"))
	u.dim("  Machine-wide — tools on PATH, shell hooks, SSH Include → ctx_config, profile files.")
	u.dim("  For live GitHub SSH auth for one client, run: ctx verify <name>")
	u.say("")

	allOK := true
	hr := func() { u.line(u.out, u.c(ansiDim)+strings.Repeat("─", 40)+u.c(ansiReset)) }

	chk := func(cmd, label, install, verCmd string) {
		if verCmd == "" {
			verCmd = cmd + " --version"
		}
		if _, err := exec.LookPath(cmd); err == nil {
			c := exec.Command("bash", "-lc", verCmd)
			c.Stdin = t.In
			b, _ := c.CombinedOutput()
			ver, _, _ := strings.Cut(string(b), "\n")
			if ver == "" {
				ver = "installed"
			}
			u.success(label + ": " + ver)
		} else {
			u.warn(label + ": not found  →  " + install)
			allOK = false
		}
	}

	hints := map[string]string{
		"git": "brew install git", "gum": "brew install gum", "gh": "brew install gh",
		"aws": "brew install awscli", "az": "brew install azure-cli",
		"gcloud": "brew install --cask google-cloud-sdk", "kubectl": "brew install kubectl",
	}
	if runtime.GOOS == "linux" {
		hints = map[string]string{
			"git": "sudo apt-get install -y git", "gum": "brew install gum (or install from github.com/charmbracelet/gum)",
			"gh": "sudo apt-get install -y gh", "aws": "pipx install awscli",
			"az":     "curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash",
			"gcloud": "See: https://cloud.google.com/sdk/docs/install", "kubectl": "sudo apt-get install -y kubectl",
		}
	}
	chk("git", "git", hints["git"], "")
	chk("mise", "mise", "curl https://mise.run | sh", "")
	chk("gum", "gum", hints["gum"], "")
	chk("gh", "gh", hints["gh"], "")
	chk("aws", "AWS CLI", hints["aws"], "")
	chk("az", "Azure CLI", hints["az"], "az --version")
	chk("gcloud", "gcloud", hints["gcloud"], "")
	chk("kubectl", "kubectl", hints["kubectl"], "kubectl version --client=true")

	coreVer := CoreVersion
	if coreVer == "" {
		coreVer = "installed"
	}
	corePath, _ := coreExecutable()
	u.success("ctx-core: " + coreVer + " (" + corePath + ")")

	u.say("")
	hr()

	sp, eff := secret.ConfiguredProvider(), secret.EffectiveProvider()
	if secretProviderAvailable(eff) {
		u.success("secret provider: " + sp + " (effective: " + eff + ")")
	} else {
		u.warn("secret provider unavailable: " + sp + " (effective: " + eff + ")")
		switch eff {
		case "keychain":
			u.info("Install/enable macOS security Keychain tooling, or set: ctx config secret-provider file")
		case "pass":
			u.info("Install pass (and initialize it), or set: ctx config secret-provider auto")
		}
		allOK = false
	}

	u.say("")
	hr()

	home := os.Getenv("HOME")
	shell := os.Getenv("SHELL")
	rc := ""
	if strings.Contains(shell, "zsh") {
		rc = home + "/.zshrc"
	}
	if strings.Contains(shell, "bash") {
		rc = home + "/.bashrc"
	}
	if strings.Contains(shell, "fish") {
		rc = home + "/.config/fish/config.fish"
	}
	if rc != "" {
		text := ""
		if b, err := os.ReadFile(rc); err == nil {
			text = string(b)
		}
		if strings.Contains(text, "mise activate") {
			u.success("mise shell hook: installed")
		} else {
			u.warn("mise activate: not in " + rc)
			u.info("Run: ctx install-hook")
			allOK = false
		}
		if strings.Contains(text, "_ctx_profile_autoswitch") ||
			strings.Contains(text, "# BEGIN ctx profile autoswitch") ||
			strings.Contains(text, "ctx auto-switch") {
			u.success("ctx profile autoswitch: installed in " + rc)
		} else {
			u.warn("ctx profile autoswitch: not in " + rc)
			u.info("Run: ctx install-hook")
			allOK = false
		}
	}

	u.say("")
	hr()

	if sshConfigIncludesCtx(home + "/.ssh/config") {
		u.success("SSH isolation: ctx_config is included")
	} else {
		u.warn("SSH isolation: ~/.ssh/ctx_config not included in ~/.ssh/config")
		u.info("Run: ctx setup (or any command that writes an SSH host) to auto-add Include, or add manually:")
		u.info("  Include " + gitssh.SSHConfigPath())
		allOK = false
	}

	if st, err := os.Stat(home + "/.ssh"); err == nil && st.IsDir() {
		// find -maxdepth 0 tests the path itself, without following a symlink.
		if lst, err := os.Lstat(home + "/.ssh"); err == nil && lst.Mode().Perm()&0o022 != 0 {
			u.warn("SSH: ~/.ssh is group- or world-writable (OpenSSH may refuse to use keys here)")
			u.info("Run: chmod 700 " + home + "/.ssh")
			allOK = false
		} else {
			u.success("SSH: ~/.ssh directory permissions are strict enough")
		}
	} else {
		u.dim("  SSH: ~/.ssh not present yet (normal before first key or setup)")
	}

	if guard.Installed() {
		u.success("Git guard: on (wrong-identity commits/pushes are blocked)")
	} else {
		u.dim("  Git guard: off (optional) — ctx guard install")
	}

	u.say("")
	hr()

	pc := 0
	for _, pname := range doctorProfiles() {
		pc++
		p, err := profile.Load(profile.Dir(), pname)
		if err != nil {
			p = &profile.Profile{}
		}
		ok := true
		if p.WorkDir != "" && !isDir(p.WorkDir) {
			u.warn("Profile '" + pname + "': work dir missing: " + p.WorkDir)
			ok = false
		}
		if p.WorkDir != "" && !isFile(filepath.Join(p.WorkDir, "mise.toml")) {
			u.warn("Profile '" + pname + "': mise.toml missing in " + p.WorkDir)
			ok = false
		}
		if p.SSHKeyPath != "" && !isFile(p.SSHKeyPath) {
			u.warn("Profile '" + pname + "': SSH key missing: " + p.SSHKeyPath)
			ok = false
		}
		if ok {
			u.success("Profile '" + pname + "': ok")
		} else {
			allOK = false // R4: Bash only set a local flag here
		}
	}
	if pc == 0 {
		u.info("No profiles yet. Run: ctx setup")
	}

	u.say("")
	if allOK {
		u.success("Everything looks good.")
	} else {
		u.warn("Fix the issues above.")
	}
	u.say("")
	return 0
}

// secretProviderAvailable mirrors ctx_secret_provider_available.
func secretProviderAvailable(eff string) bool {
	switch eff {
	case "keychain":
		_, err := exec.LookPath("security")
		return runtime.GOOS == "darwin" && err == nil
	case "pass":
		_, err := exec.LookPath("pass")
		return err == nil
	}
	return true
}

func sshConfigIncludesCtx(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if sshIncludeRe.MatchString(line) {
			return true
		}
	}
	return false
}

// doctorProfiles is the glob "$CTX_PROFILES_DIR"/*.conf: existing entries
// whose name ends in .conf and does not start with a dot, sorted.
func doctorProfiles() []string {
	dir := profile.Dir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".conf") || strings.HasPrefix(n, ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			continue
		}
		out = append(out, strings.TrimSuffix(n, ".conf"))
	}
	sort.Strings(out)
	return out
}

func isFile(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }
