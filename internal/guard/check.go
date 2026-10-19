// Package guard implements `ctx guard check`, the git-hook identity guard.
// It ports _ctx_guard_check and friends in lib/cmd_ops.sh.
package guard

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
)

// blockCode is CTX_GUARD_BLOCK: the exit code that makes the hook abort.
const blockCode = 3

// colors mirrors lib/core.sh, which colors by whether stdout (not stderr) is a TTY.
type colors struct{ red, yellow, reset string }

func stdoutColors() colors {
	if stdoutIsTTY() {
		return colors{"\033[0;31m", "\033[0;33m", "\033[0m"}
	}
	return colors{}
}

func stdoutIsTTY() bool {
	st, err := os.Stdout.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(st, null) {
		return false
	}
	return true
}

// git runs git and returns stdout with trailing newlines removed, like $(...).
// Errors yield "" and false.
func git(args ...string) (string, bool) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(out), "\n"), true
}

// Check runs the guard for a git hook ("pre-commit" or "pre-push" with
// remote and URL in args) and returns the exit code: 3 to block, else 0.
// Unknown hooks, non-repos and paths outside any profile's WORK_DIR pass.
func Check(hook string, args []string, stderr io.Writer) int {
	top, ok := git("rev-parse", "--show-toplevel")
	if !ok {
		return 0
	}
	dir := profile.Dir()
	prof, _, err := resolve.Path(top, dir)
	if err != nil || prof == "" {
		return 0
	}
	c := stdoutColors()
	switch hook {
	case "pre-commit":
		return checkCommit(prof, dir, c, stderr)
	case "pre-push":
		var remote, url string
		if len(args) > 0 {
			remote = args[0]
		}
		if len(args) > 1 {
			url = args[1]
		}
		return checkPush(prof, remote, url, dir, c, stderr)
	}
	return 0
}

// identEmail turns "Name <email> 1700000000 +0000" into email.
func identEmail(id string) string {
	if i := strings.Index(id, "<"); i >= 0 {
		id = id[i+1:]
	}
	if i := strings.Index(id, ">"); i >= 0 {
		id = id[:i]
	}
	return id
}

func lc(s string) string { return strings.ToLower(s) }

func checkCommit(prof, dir string, c colors, stderr io.Writer) int {
	p, err := profile.Load(dir, prof)
	if err != nil || p.GitEmail == "" {
		return 0
	}
	want := p.GitEmail
	for _, who := range []string{"AUTHOR", "COMMITTER"} {
		ident, _ := git("var", "GIT_"+who+"_IDENT")
		got := identEmail(ident)
		if lc(got) == lc(want) {
			continue
		}
		cfg, _ := git("config", "--get", "user.email")
		local, _ := git("config", "--local", "--get", "user.email")
		var fix string
		switch {
		case lc(cfg) != lc(got):
			// git exports GIT_AUTHOR_EMAIL to hooks, so an env override shows up as a config mismatch.
			fix = "unset GIT_AUTHOR_EMAIL GIT_COMMITTER_EMAIL EMAIL   (set in your environment)"
		case local != "":
			fix = "git config --local --unset user.email   (repo-local override)"
		default:
			fix = "git config user.email '" + want + "'"
		}
		shown := got
		if shown == "" {
			shown = "unset"
		}
		fmt.Fprintf(stderr, "%s✗%s ctx guard: commit blocked — %s email is '%s', but this repo belongs to profile '%s' (%s).\n",
			c.red, c.reset, lc(who), shown, prof, want)
		fmt.Fprintf(stderr, "  Fix:       %s\n", fix)
		fmt.Fprint(stderr, "  Skip once: git commit --no-verify    Turn off: CTX_GUARD=off\n")
		return blockCode
	}
	return 0
}

// urlHost extracts the host from git@host:path, ssh://user@host:port/path or https://host/path.
func urlHost(u string) string {
	switch {
	case strings.Contains(u, "://"):
		u = u[strings.Index(u, "://")+3:]
		if i := strings.Index(u, "/"); i >= 0 {
			u = u[:i]
		}
		if i := strings.LastIndex(u, "@"); i >= 0 {
			u = u[i+1:]
		}
		if i := strings.Index(u, ":"); i >= 0 {
			u = u[:i]
		}
		return u
	case strings.Contains(u, ":"):
		u = u[:strings.Index(u, ":")]
		if i := strings.LastIndex(u, "@"); i >= 0 {
			u = u[i+1:]
		}
		return u
	}
	return ""
}

// cloneURLForProfile mirrors github_clone_url_for_profile with https_as_ssh=y.
func cloneURLForProfile(prof, url string) string {
	host := "github-" + prof
	dotGit := func(p string) string { return strings.TrimSuffix(p, ".git") + ".git" }
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		return "git@" + host + ":" + strings.TrimPrefix(url, "git@github.com:")
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		p := strings.TrimPrefix(strings.TrimPrefix(url, "ssh://git@github.com/"), "/")
		return "git@" + host + ":" + dotGit(p)
	case strings.HasPrefix(url, "https://github.com/"):
		p := strings.TrimPrefix(strings.TrimPrefix(url, "https://github.com/"), "/")
		return "git@" + host + ":" + dotGit(p)
	}
	return url
}

func checkPush(prof, remote, url, dir string, c colors, stderr io.Writer) int {
	host := urlHost(url)
	switch {
	case strings.HasPrefix(host, "github-"):
		other := strings.TrimPrefix(host, "github-")
		if other == prof {
			return 0
		}
		if st, err := os.Stat(filepath.Join(dir, other+".conf")); err != nil || !st.Mode().IsRegular() {
			return 0
		}
		var path string
		if i := strings.Index(url, "://"); i >= 0 {
			path = url[i+3:]
			if j := strings.Index(path, "/"); j >= 0 {
				path = path[j+1:]
			}
		} else {
			path = url[strings.Index(url, ":")+1:]
		}
		fmt.Fprintf(stderr, "%s✗%s ctx guard: push blocked — '%s' uses profile '%s' (host %s), but this repo belongs to profile '%s'.\n",
			c.red, c.reset, remote, other, host, prof)
		fmt.Fprintf(stderr, "  Fix:       git remote set-url %s git@github-%s:%s\n", remote, prof, path)
		fmt.Fprint(stderr, "  Skip once: git push --no-verify    Turn off: CTX_GUARD=off\n")
		return blockCode
	case host == "github.com":
		if !hasSSHHost(prof) {
			return 0
		}
		fmt.Fprintf(stderr, "%s!%s ctx guard: '%s' pushes via github.com, not github-%s, so profile '%s's SSH key may not be used.\n",
			c.yellow, c.reset, remote, prof, prof)
		fmt.Fprintf(stderr, "  Fix: ctx use %s   (routes github.com through github-%s for repos in its WORK_DIR)\n", prof, prof)
		fmt.Fprintf(stderr, "   or: git remote set-url %s %s\n", remote, cloneURLForProfile(prof, url))
	}
	return 0
}

// hasSSHHost reports whether ~/.ssh/ctx_config has a "Host github-<prof>" line.
func hasSSHHost(prof string) bool {
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".ssh", "ctx_config"))
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`^Host[[:space:]]+github-` + regexp.QuoteMeta(prof) + `([[:space:]]|$)`)
	for _, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
