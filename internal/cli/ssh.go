package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/atomicfile"
	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/gitssh"
)

// sshConfigPath is CTX_SSH_CONFIG: $HOME/.ssh/ctx_config.
func sshConfigPath() string { return gitssh.SSHConfigPath() }

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
func gitconfigRemoveInclude(identityFile string, warn gitssh.Warn) error {
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
	if ctxconfig.BackupFile(path) != nil && warn != nil {
		warn("Could not back up " + path)
	}
	return writeThrough(path, []byte(out.String()))
}

// writeThrough atomically replaces the file path points to (following a
// symlink) with data, keeping that file's mode, like `cat tmp > file` does.
// A file that does not exist yet gets mode 0600.
func writeThrough(path string, data []byte) error {
	return writeThroughMode(path, data, 0o600)
}

// writeTemp writes data into the temp file; tests replace it to inject failures.
var writeTemp = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// writeThroughMode is writeThrough with newMode for a file that does not exist
// yet (atomicfile.WriteThrough: the target is replaced only by a rename of a
// complete temp file in its directory, so a failed write leaves it as it was).
func writeThroughMode(path string, data []byte, newMode os.FileMode) error {
	return atomicfile.WriteThrough(path, data, func(old os.FileMode, existed bool) os.FileMode {
		if existed {
			return old
		}
		return newMode
	}, writeTemp)
}
