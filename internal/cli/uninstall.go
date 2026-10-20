package cli

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/guard"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// installerLibLine is the CTX_LIB line install.sh writes into bin/ctx (line 9).
var installerLibLine = regexp.MustCompile(`^CTX_LIB="(/[^"]*)"$`)

// Uninstall runs `ctx uninstall [--purge]`, mirroring cmd_uninstall in
// lib/cmd_ops.sh (R8), and returns the exit code.
//
// Which install: the ctx on PATH (`command -v ctx`, as Bash), which must be
// the bin/ctx that is running, CTX_BIN_PATH as the forwarder exports it
// (compared after resolving symlinks). It must be a regular file, not a
// symlink, holding the installer's line CTX_LIB="/abs/path" (a source
// checkout has none), and that lib dir must be this ctx's CTX_LIB (also
// exported by the forwarder). Without CTX_BIN_PATH (ctx-core run directly)
// it refuses. os.Executable is not used: it is ctx-core, not ctx.
//
// Every rm -rf target passes rmRfOK before the prompt; any doubt is a
// refusal ("Refusing to …", exit 1) with nothing removed. After Yes it
// deletes, in order: with --purge each profile's secrets from the configured
// store (a failure stops everything), the guard (when installed), ctx, the
// ctx-core next to it, the lib dir, and with --purge $CTX_DIR. A removal that
// fails is reported and makes the exit code 1. --dry-run lists the actions.
func Uninstall(t tui.Term, args []string) int {
	u := termUI(t)
	purge := false
	for _, a := range args {
		if a != "--purge" {
			return u.die("Usage: ctx uninstall [--purge]")
		}
		purge = true
	}

	ctxBin, err := exec.LookPath("ctx")
	if err != nil && !errors.Is(err, exec.ErrDot) {
		ctxBin = ""
	}
	if ctxBin == "" {
		return u.die("ctx not found in PATH")
	}
	if st, err := os.Lstat(ctxBin); err == nil && st.Mode()&os.ModeSymlink != 0 {
		l, _ := os.Readlink(ctxBin)
		return u.die("Refusing to uninstall: " + ctxBin + " is a symlink (to " + l + "). Remove the link yourself, or put the installed ctx first on PATH.")
	}
	if !isRegular(ctxBin) {
		return u.die("Refusing to uninstall: " + ctxBin + " is not a regular file.")
	}
	self := os.Getenv("CTX_BIN_PATH")
	me, ok := physFile(self)
	if self == "" || !ok {
		me = ""
	}
	if got, _ := physFile(ctxBin); me == "" || got != me {
		return u.die("Refusing to uninstall: the ctx on PATH (" + ctxBin + ") is not the one running (" + self + ").")
	}
	lib := installedLib(ctxBin)
	if lib == "" {
		return u.die("Refusing to uninstall: " + ctxBin + " has no installer CTX_LIB line (a source checkout?). Remove it by hand.")
	}
	running := os.Getenv("CTX_LIB")
	a, _ := physDir(lib)
	b, ok := physDir(running)
	if !ok {
		b = "?"
	}
	if a != b {
		return u.die("Refusing to uninstall: " + ctxBin + " names " + lib + ", but this ctx runs from " + running + ".")
	}
	if why := rmRfOK(lib, "lib"); why != "" {
		return u.die("Refusing to remove " + lib + ": " + why + ".")
	}
	data := ctxDirRaw()
	if _, err := os.Lstat(data); purge && err == nil {
		if why := rmRfOK(data, "data"); why != "" {
			return u.die("Refusing to remove " + data + ": " + why + ".")
		}
	}
	core := bashDirname(ctxBin) + "/ctx-core"
	if _, err := os.Lstat(core); err != nil {
		core = ""
	}

	var store secret.Store
	var label string
	var profiles []string
	if purge {
		store, _ = secret.Open(secret.EffectiveProvider())
		label = secret.StoreLabel()
		profiles, _ = profile.List(profile.Dir())
	}
	keysOf := func(p string) []string {
		var keys []string
		if store != nil {
			all, _ := store.List(p)
			for _, k := range all {
				if k != "" {
					keys = append(keys, k)
				}
			}
		}
		return keys
	}
	_, statErr := os.Stat(data)
	dataExists := statErr == nil

	u.bold("\n  ctx uninstall\n")
	u.warn("This removes the ctx binary and library scripts from your install location.")
	if purge {
		u.warn("Also deletes " + data + " (profiles, backups) and every profile's secrets in the " + label + ".")
	}
	u.dim("  " + ctxBin)
	if core != "" {
		u.dim("  " + core)
	}
	u.dim("  " + lib)
	if purge && dataExists {
		u.dim("  " + data)
	}
	u.say("")

	if FlagsFromEnv().DryRun {
		for _, p := range profiles {
			if n := len(keysOf(p)); n > 0 {
				u.dim("  [dry-run] would delete " + strconv.Itoa(n) + " secret(s) of " + p + " from the " + label)
			}
		}
		if guard.Installed() {
			u.dim("  [dry-run] would run ctx guard uninstall")
		}
		u.dim("  [dry-run] would remove " + ctxBin)
		if core != "" {
			u.dim("  [dry-run] would remove " + core)
		}
		u.dim("  [dry-run] would remove " + lib)
		if purge && dataExists {
			u.dim("  [dry-run] would remove " + data)
		}
		u.say("")
		return 0
	}

	defer OnInterrupt(t, nil)()
	yes, err := t.Prompter().AskYN("Continue?", false)
	if err != nil {
		return t.Cancelled()
	}
	if !yes {
		return u.die("Aborted.")
	}

	// Secrets first: if one cannot be deleted, nothing else is removed.
	failed := false
	for _, p := range profiles {
		n := 0
		for _, k := range keysOf(p) {
			if store.Delete(p, k) == nil {
				n++
			} else {
				u.error("Could not delete secret " + k + " of " + p + " from the " + label)
				failed = true
			}
		}
		if n > 0 {
			u.success("Deleted " + strconv.Itoa(n) + " secret(s) of " + p + " from the " + label)
		}
	}
	if failed {
		return u.die("Some secrets could not be deleted; nothing else was removed.")
	}

	if guard.Installed() {
		guard.Uninstall(t.Out, t.Err)
	}
	remove := func(p string, rm func(string) error) {
		if rm(p) == nil {
			u.success("Removed " + p)
		} else {
			u.error("Could not remove " + p)
			failed = true
		}
	}
	remove(ctxBin, rmF)
	if core != "" {
		remove(core, rmF)
	}
	remove(lib, os.RemoveAll)
	if purge {
		if _, err := os.Stat(data); err == nil {
			remove(data, os.RemoveAll)
		}
	}

	u.say("")
	u.info("Remove the ctx blocks from your shell rc if desired (search for '# BEGIN ctx profile autoswitch').")
	u.say("")
	if failed {
		return 1
	}
	return 0
}

// rmF is rm -f for one path: a directory is an error, not removed.
func rmF(p string) error {
	if st, err := os.Lstat(p); err == nil && st.IsDir() {
		return errors.New("is a directory")
	}
	return os.Remove(p)
}

// installedLib is the path in bin's first CTX_LIB= line when it is the
// installer's CTX_LIB="/abs/path" form, else "".
func installedLib(bin string) string {
	b, err := os.ReadFile(bin)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "CTX_LIB=") {
			if m := installerLibLine.FindStringSubmatch(line); m != nil {
				return m[1]
			}
			return ""
		}
	}
	return ""
}

// rmRfOK mirrors _ctx_rm_rf_ok: "" when rm -rf of dir is safe, else why not.
// kind is "lib" (the install's lib dir) or "data" (CTX_DIR for --purge).
// Refused: not absolute, a symlink, not a directory, /, $HOME or a directory
// holding it, CTX_DIR's parent; a lib dir not named ctx, holding CTX_DIR,
// without core.sh, or a git checkout; a data dir not named .ctx with neither a
// config file nor a profiles dir. R8 allows any name for an explicitly set
// CTX_DIR; ctx-core cannot tell (the forwarder always exports CTX_DIR) and the
// default always ends in .ctx, so a CTX_DIR of another name must look like ctx data.
func rmRfOK(dir, kind string) string {
	if !strings.HasPrefix(dir, "/") {
		return "it is not an absolute path"
	}
	if dir != "/" {
		if st, err := os.Lstat(strings.TrimSuffix(dir, "/")); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "it is a symlink"
		}
	}
	if !isDir(dir) {
		return "it is not a directory"
	}
	p, ok := physDir(dir)
	if !ok {
		return "it cannot be resolved"
	}
	if p == "/" {
		return "it is /"
	}
	home, ok := physDir(os.Getenv("HOME"))
	if !ok {
		home = os.Getenv("HOME")
	}
	if strings.HasPrefix(strings.TrimSuffix(home, "/")+"/", p+"/") {
		return "it is your home directory or holds it"
	}
	data, ok := physDir(ctxDirRaw())
	if !ok {
		data = ctxDirRaw()
	}
	if p == bashDirname(data) {
		return "it is the parent of CTX_DIR"
	}
	if kind == "lib" {
		if bashBasename(p) != "ctx" {
			return "its name is not 'ctx'"
		}
		if strings.HasPrefix(strings.TrimSuffix(data, "/")+"/", p+"/") {
			return "it holds CTX_DIR"
		}
		if !isRegular(p + "/core.sh") {
			return "it has no core.sh"
		}
		if _, err := os.Stat(p + "/.git"); err == nil {
			return "it is a git checkout"
		}
		return ""
	}
	if bashBasename(strings.TrimSuffix(dir, "/")) != ".ctx" && !isRegular(p+"/config") && !isDir(p+"/profiles") {
		return "it does not look like a ctx data directory (no config or profiles)"
	}
	return ""
}
