// Package gitssh edits ~/.ssh/config, ~/.ssh/ctx_config and ~/.gitconfig the
// way lib/core.sh does (ensure_ssh_include, ctx_ssh_add_host,
// gitconfig_add_include). Both internal/cli and internal/setup use it.
package gitssh

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Hei-Tech-Inc/ctx/internal/atomicfile"
	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
)

// SSHConfigPath is CTX_SSH_CONFIG: $HOME/.ssh/ctx_config.
func SSHConfigPath() string { return filepath.Join(os.Getenv("HOME"), ".ssh", "ctx_config") }

// Warn reports a problem the caller keeps going after (Bash's warn, sent to
// stderr); nil drops it, like a Bash caller's 2>/dev/null.
type Warn func(msg string)

// backup is `backup_file "$path" || warn "Could not back up $path" >&2`.
func backup(path string, warn Warn) {
	if ctxconfig.BackupFile(path) != nil && warn != nil {
		warn("Could not back up " + path)
	}
}

// EnsureInclude mirrors ensure_ssh_include: make sure ~/.ssh/config starts
// with an Include of the ctx-managed file, backing it up before the first edit.
// The file is rewritten atomically through symlinks (R20): a link stays a
// link, a private mode (no group/other write) is kept, a group- or
// world-writable one becomes 600 with a warning (ssh refuses such a file), and
// a new file is 600. A file that cannot be read or written is left alone with
// a warning.
func EnsureInclude(warn Warn) {
	home := os.Getenv("HOME")
	sshMain := filepath.Join(home, ".ssh", "config")
	ctxSSH := SSHConfigPath()
	_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o777)
	_ = os.Chmod(filepath.Join(home, ".ssh"), 0o700)
	if f, err := os.OpenFile(ctxSSH, os.O_CREATE|os.O_WRONLY, 0o666); err == nil {
		f.Close()
	}
	_ = os.Chmod(ctxSSH, 0o600)

	existing, rerr := os.ReadFile(sshMain)
	if strings.Contains(string(existing), "Include "+ctxSSH) {
		return
	}
	say := func(msg string) {
		if warn != nil {
			warn(msg)
		}
	}
	failed := "Could not add Include " + ctxSSH + " to " + sshMain
	target := atomicfile.FollowLinks(sshMain)
	if _, err := os.Stat(target); err == nil {
		if rerr != nil || syscall.Access(target, 2) != nil { // W_OK
			say(failed)
			return
		}
	}
	backup(sshMain, warn)
	body := "# ctx managed hosts — added by ctx installer\nInclude " + ctxSSH + "\n\n" + string(existing)
	tightened := false
	err := atomicfile.WriteThrough(target, []byte(body), func(old os.FileMode, existed bool) os.FileMode {
		if !existed {
			return 0o600
		}
		if old&0o022 != 0 {
			tightened = true
			return 0o600
		}
		return old
	}, nil)
	if err != nil {
		say(failed)
		return
	}
	if tightened {
		say(sshMain + " was writable by group or others; it is now 600 (ssh refuses such a file)")
	}
	ctxconfig.Log("Added Include " + ctxSSH + " to " + sshMain)
}

// GitconfigAddInclude mirrors gitconfig_add_include: append an includeIf for
// workDir to ~/.gitconfig unless identityFile is already mentioned there.
// It reports whether it added the block; err is a failed append (Bash
// returns 2 then).
func GitconfigAddInclude(workDir, identityFile string, warn Warn) (added bool, err error) {
	gitconfig := filepath.Join(os.Getenv("HOME"), ".gitconfig")
	if data, err := os.ReadFile(gitconfig); err == nil && strings.Contains(string(data), identityFile) {
		return false, nil
	}
	backup(gitconfig, warn)
	f, err := os.OpenFile(gitconfig, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return false, err
	}
	_, err = f.WriteString("\n[includeIf \"gitdir:" + workDir + "/\"]\n  path = " + identityFile + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return false, err
	}
	ctxconfig.Log("Added includeIf for " + workDir + " to ~/.gitconfig")
	return true, nil
}

// HasHost reports whether ctx_config has the line "Host <host>" (grep
// "^Host $host$"; hosts are github-<profile slug>, which holds no regex
// metacharacters).
func HasHost(host string) bool {
	data, err := os.ReadFile(SSHConfigPath())
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "Host "+host {
			return true
		}
	}
	return false
}

// AddHost mirrors ctx_ssh_add_host: after EnsureInclude, append a host block
// for host using key to ctx_config (never ~/.ssh/config) and leave the file
// at mode 0600. added is false when the host is already there or the append
// failed (err; Bash returns 2 then, before the chmod and the log line).
func AddHost(host, key string, warn Warn) (added bool, err error) {
	EnsureInclude(warn)
	if HasHost(host) {
		return false, nil
	}
	path := SSHConfigPath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err == nil {
		_, err = f.WriteString("\nHost " + host + "\n  HostName github.com\n  User git\n  IdentityFile " + key + "\n  IdentitiesOnly yes\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return false, err
	}
	_ = os.Chmod(path, 0o600)
	ctxconfig.Log("Added SSH host " + host + " to " + path)
	return true, nil
}
