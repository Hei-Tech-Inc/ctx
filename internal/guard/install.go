package guard

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/cli"
	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
)

// hookNames is CTX_GUARD_HOOK_NAMES. reference-transaction is left out: it
// fires on every ref update.
var hookNames = []string{
	"applypatch-msg", "pre-applypatch", "post-applypatch", "pre-commit",
	"pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-commit", "pre-rebase",
	"post-checkout", "post-merge", "pre-push", "post-rewrite", "pre-auto-gc",
	"sendemail-validate", "post-index-change",
}

// ui prints like the Bash helpers info/success/warn/dim (stdout) and error (stderr).
type ui struct {
	out, err io.Writer
	colors   bool
}

func newUI(stdout, stderr io.Writer) ui { return ui{stdout, stderr, cli.TTYColors(stdout)} }

func (u ui) c(code string) string {
	if u.colors {
		return code
	}
	return ""
}

func (u ui) say(w io.Writer, s string) { _, _ = io.WriteString(w, cli.EchoE(s)) }

func (u ui) info(m string)    { u.say(u.out, u.c("\033[0;36m")+"→"+u.c("\033[0m")+" "+m) }
func (u ui) success(m string) { u.say(u.out, u.c("\033[0;32m")+"✓"+u.c("\033[0m")+" "+m) }
func (u ui) warn(m string)    { u.say(u.out, u.c("\033[0;33m")+"!"+u.c("\033[0m")+" "+m) }
func (u ui) dim(m string)     { u.say(u.out, u.c("\033[2m")+m+u.c("\033[0m")) }
func (u ui) error(m string)   { u.say(u.err, u.c("\033[0;31m")+"✗"+u.c("\033[0m")+" "+m) }

func hooksDir() string { return filepath.Join(ctxconfig.Dir(), "git-hooks") }

// expandTilde mirrors ${v/#\~/$HOME}.
func expandTilde(v string) string {
	if strings.HasPrefix(v, "~") {
		return os.Getenv("HOME") + v[1:]
	}
	return v
}

// globalHooksPath is `git config --global --get core.hooksPath` ("" when unset).
func globalHooksPath() string {
	out, _ := git("config", "--global", "--get", "core.hooksPath")
	return out
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

// Installed mirrors _ctx_guard_installed.
func Installed() bool {
	dir := hooksDir()
	return expandTilde(globalHooksPath()) == dir && isExecutable(filepath.Join(dir, "pre-commit"))
}

// hookStub is the file _ctx_guard_write_hook writes for name.
func hookStub(name string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Written by \"ctx guard install\"; changes are overwritten.\n")
	if name == "pre-commit" || name == "pre-push" {
		fmt.Fprintf(&b, "if [ \"${CTX_GUARD:-on}\" != off ] && command -v ctx >/dev/null 2>&1; then\n")
		fmt.Fprintf(&b, "  ctx guard check %s \"$@\" </dev/null\n", name)
		fmt.Fprintf(&b, "  [ $? -eq %d ] && exit 1\n", blockCode)
		b.WriteString("fi\n")
	}
	fmt.Fprintf(&b, "h=\"$(git rev-parse --git-common-dir 2>/dev/null)/hooks/%s\"\n", name)
	b.WriteString("[ -x \"$h\" ] && exec \"$h\" \"$@\"\n")
	b.WriteString("exit 0\n")
	return b.String()
}

func writeHook(name, file string) {
	_ = os.WriteFile(file, []byte(hookStub(name)), 0o666)
	_ = os.Chmod(file, 0o755)
}

func dryRun() bool { return os.Getenv("CTX_DRY_RUN") == "1" }

// Install mirrors _ctx_guard_install and returns the exit code.
func Install(stdout, stderr io.Writer) int {
	u := newUI(stdout, stderr)
	if _, err := exec.LookPath("git"); err != nil {
		u.error("git not found")
		return 1
	}
	dir := hooksDir()
	cur := globalHooksPath()
	if cur != "" && expandTilde(cur) != dir {
		u.error("git already has a global core.hooksPath: " + cur)
		u.info("Either unset it (git config --global --unset core.hooksPath) and re-run,")
		u.info(fmt.Sprintf("or call these from your own hooks (exit %d means block):", blockCode))
		u.info("  pre-commit: ctx guard check pre-commit")
		u.info("  pre-push:   ctx guard check pre-push \"$@\"")
		return 1
	}
	if dryRun() {
		u.info(fmt.Sprintf("[dry-run] Would write %d hooks to %s", len(hookNames), dir))
		u.info("[dry-run] Would run: git config --global core.hooksPath " + dir)
		return 0
	}
	_ = os.MkdirAll(dir, 0o777)
	for _, name := range hookNames {
		writeHook(name, filepath.Join(dir, name))
	}
	gitRun("config", "--global", "core.hooksPath", dir)
	ctxconfig.Log("guard installed: " + dir)
	u.success("ctx guard on: commits and pushes in client repos are checked against the profile")
	u.dim("  Repo hooks in .git/hooks still run. Repos that set their own core.hooksPath (e.g. husky) skip the guard.")
	u.dim("  Skip once: --no-verify   Off for a session: CTX_GUARD=off   Remove: ctx guard uninstall")
	return 0
}

// gitRun runs git for its effect, passing its stderr through like Bash would.
func gitRun(args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// Uninstall mirrors _ctx_guard_uninstall and returns the exit code.
func Uninstall(stdout, stderr io.Writer) int {
	u := newUI(stdout, stderr)
	dir := hooksDir()
	st, err := os.Stat(dir)
	if !Installed() && (err != nil || !st.IsDir()) {
		u.info("ctx guard is not installed")
		return 0
	}
	if dryRun() {
		u.info("[dry-run] Would unset core.hooksPath and remove " + dir)
		return 0
	}
	if Installed() {
		gitRun("config", "--global", "--unset", "core.hooksPath")
	}
	for _, name := range hookNames {
		_ = os.Remove(filepath.Join(dir, name))
	}
	_ = os.Remove(dir) // rmdir: only succeeds when empty
	ctxconfig.Log("guard removed")
	u.success("ctx guard off")
	return 0
}

// Status mirrors _ctx_guard_status and returns the exit code.
func Status(stdout, stderr io.Writer) int {
	u := newUI(stdout, stderr)
	if Installed() {
		u.success("ctx guard: on (" + hooksDir() + ")")
	} else {
		u.warn("ctx guard: off  →  ctx guard install")
	}
	top, ok := git("rev-parse", "--show-toplevel")
	if !ok {
		return 0
	}
	pdir := profile.Dir()
	prof, _, err := resolve.Path(top, pdir)
	if err != nil || prof == "" {
		u.dim("  This repo is not under any profile — nothing to check.")
		return 0
	}
	if lhp, _ := git("config", "--local", "--get", "core.hooksPath"); lhp != "" {
		u.warn("This repo sets core.hooksPath=" + lhp + ", so the guard does not run here.")
	}
	want := ""
	if p, err := profile.Load(pdir, prof); err == nil {
		want = p.GitEmail
	}
	ident, _ := git("var", "GIT_AUTHOR_IDENT")
	got := identEmail(ident)
	switch {
	case want == "":
		u.info("Profile '" + prof + "' has no GIT_EMAIL — email check skipped.")
	case lc(got) == lc(want):
		u.success("Profile '" + prof + "': commits here use " + got)
	default:
		shown := got
		if shown == "" {
			shown = "no email"
		}
		u.warn("Profile '" + prof + "' expects " + want + ", but commits here would use " + shown)
	}
	return 0
}
