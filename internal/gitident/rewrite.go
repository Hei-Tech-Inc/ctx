// Package gitident edits the per-profile git identity files
// (~/.config/git/ctx-<profile>) the way lib/core.sh does, through
// `git config --file`.
package gitident

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
)

// rewriteRE is grep -qx 'git@github.com:' (the dots match any character).
var rewriteRE = regexp.MustCompile(`^git@github.com:$`)

// hostLine reports whether sshConfig has a line matching grep "^Host <host>$".
// host is a profile slug; as in the grep pattern, a "." in it matches any character.
func hostLine(sshConfig, host string) bool {
	data, err := os.ReadFile(sshConfig)
	if err != nil {
		return false
	}
	re, err := regexp.Compile("^Host " + strings.ReplaceAll(regexp.QuoteMeta(host), `\.`, ".") + "$")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// SetRewrite mirrors ctx_git_identity_set_rewrite: make plain git@github.com:
// and ssh://git@github.com/ remotes use Host github-<profile> in the profile's
// identity file. It does nothing without the identity file or the SSH host,
// or when the rewrite is already there. git's output goes to stdout/stderr as
// in Bash (the --get-all lookup's stderr and --unset-all's stderr are dropped).
func SetRewrite(profile string, stdout, stderr io.Writer) {
	home := os.Getenv("HOME")
	host := "github-" + profile
	file := filepath.Join(home, ".config", "git", "ctx-"+profile)
	key := "url.git@github-" + profile + ":.insteadOf"
	if st, err := os.Stat(file); err != nil || !st.Mode().IsRegular() {
		return
	}
	if !hostLine(filepath.Join(home, ".ssh", "ctx_config"), host) {
		return
	}
	out, _ := exec.Command("git", "config", "--file", file, "--get-all", key).Output()
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if rewriteRE.MatchString(line) {
			return
		}
	}
	git := func(errw io.Writer, args ...string) {
		cmd := exec.Command("git", append([]string{"config", "--file", file}, args...)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, errw
		_ = cmd.Run()
	}
	git(nil, "--unset-all", key)
	git(stderr, "--add", key, "git@github.com:")
	git(stderr, "--add", key, "ssh://git@github.com/")
	ctxconfig.Log("Added github.com → " + host + " rewrite to " + file)
}
