package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
)

// sshConfigPath is CTX_SSH_CONFIG: $HOME/.ssh/ctx_config.
func sshConfigPath() string { return filepath.Join(os.Getenv("HOME"), ".ssh", "ctx_config") }

// ensureSSHInclude mirrors ensure_ssh_include: make sure ~/.ssh/config starts
// with an Include of the ctx-managed file, backing it up before the first edit.
func ensureSSHInclude() {
	home := os.Getenv("HOME")
	sshMain := filepath.Join(home, ".ssh", "config")
	ctxSSH := sshConfigPath()
	_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o777)
	_ = os.Chmod(filepath.Join(home, ".ssh"), 0o700)
	if f, err := os.OpenFile(ctxSSH, os.O_CREATE|os.O_WRONLY, 0o666); err == nil {
		f.Close()
	}
	_ = os.Chmod(ctxSSH, 0o600)

	existing, _ := os.ReadFile(sshMain)
	if strings.Contains(string(existing), "Include "+ctxSSH) {
		return
	}
	_ = ctxconfig.BackupFile(sshMain)
	body := "# ctx managed hosts — added by ctx installer\nInclude " + ctxSSH + "\n\n" + string(existing)
	tmp, err := os.CreateTemp(filepath.Dir(sshMain), "config.")
	if err != nil {
		return
	}
	_, werr := tmp.WriteString(body)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil || os.Rename(tmp.Name(), sshMain) != nil {
		os.Remove(tmp.Name())
		return
	}
	_ = os.Chmod(sshMain, 0o600)
	ctxconfig.Log("Added Include " + ctxSSH + " to " + sshMain)
}

// gitconfigAddInclude mirrors gitconfig_add_include: append an includeIf for
// workDir to ~/.gitconfig unless identityFile is already mentioned there.
// It reports whether it added the block.
func gitconfigAddInclude(workDir, identityFile string) bool {
	gitconfig := filepath.Join(os.Getenv("HOME"), ".gitconfig")
	if data, err := os.ReadFile(gitconfig); err == nil && strings.Contains(string(data), identityFile) {
		return false
	}
	_ = ctxconfig.BackupFile(gitconfig)
	f, err := os.OpenFile(gitconfig, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.WriteString("\n[includeIf \"gitdir:" + workDir + "/\"]\n  path = " + identityFile + "\n"); err != nil {
		return false
	}
	ctxconfig.Log("Added includeIf for " + workDir + " to ~/.gitconfig")
	return true
}

// sourcedWorkDir returns WORK_DIR as `source profile.conf` leaves it (no ~
// expansion beyond what the file itself does), "" when unset.
func sourcedWorkDir(conf string) string {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return ""
	}
	out, err := exec.Command(bash, "-c", `source "$1" >/dev/null 2>&1; printf %s "${WORK_DIR:-}"`, "_", conf).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
