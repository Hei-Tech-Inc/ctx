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

// writeReplacing replaces path with data at mode 0600 through a temp file in
// the same directory, as the Bash `mktemp … mv … chmod 600` sequence does.
func writeReplacing(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return werr
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// sshRemoveHost mirrors ctx_ssh_remove_host: in ctx_config, drop each line
// "Host <host>" and the five lines after it (the four lines of the block plus
// the separator before the next one), as the Bash awk does, then leave the
// file at mode 0600. A missing file is not an error.
func sshRemoveHost(host string) error {
	path := sshConfigPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var out strings.Builder
	skip := 0
	for _, line := range awkLines(string(data)) {
		switch {
		case line == "Host "+host:
			skip = 5
		case skip > 0:
			skip--
		default:
			out.WriteString(line + "\n")
		}
	}
	return writeReplacing(path, []byte(out.String()))
}

// awkLines splits like awk reads records: a final line without a newline
// still counts, and text ending in a newline has no empty last record.
func awkLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// gitconfigRemoveInclude removes what gitconfig_add_include wrote for
// identityFile from ~/.gitconfig: the exact line "  path = <identityFile>"
// together with the [includeIf …] header directly above it and the blank
// separator line directly above that header. Other profiles' includes (even
// ones whose identity file name merely starts with this one's) and every other
// line are kept. A path line not directly under an [includeIf header is left alone.
// The file is only rewritten (after a backup) when a line is dropped, and a
// symlinked ~/.gitconfig is written through with its mode kept.
func gitconfigRemoveInclude(identityFile string) error {
	path := filepath.Join(os.Getenv("HOME"), ".gitconfig")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := awkLines(string(data))
	drop := make([]bool, len(lines))
	want := "  path = " + identityFile
	for i := 1; i < len(lines); i++ {
		if lines[i] == want && strings.HasPrefix(lines[i-1], "[includeIf") {
			drop[i], drop[i-1] = true, true
			if i >= 2 && lines[i-2] == "" {
				drop[i-2] = true
			}
		}
	}
	var out strings.Builder
	dropped := false
	for i, l := range lines {
		if drop[i] {
			dropped = true
			continue
		}
		out.WriteString(l + "\n")
	}
	if !dropped {
		return nil
	}
	_ = ctxconfig.BackupFile(path)
	return writeThrough(path, []byte(out.String()))
}

// writeThrough atomically replaces the file path points to (following a
// symlink) with data, keeping that file's mode, like `cat tmp > file` does.
func writeThrough(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), mode)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
	}
	return werr
}
