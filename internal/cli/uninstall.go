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
// lib/cmd_ops.sh (R8, R15, R16), and returns the exit code.
//
// Which install: the ctx on PATH (`command -v ctx`, as Bash), which must be
// the bin/ctx that is running, CTX_BIN_PATH as the forwarder exports it
// (compared after resolving symlinks). It must be a regular file, not a
// symlink, holding the installer's line CTX_LIB="/abs/path" (a source
// checkout has none), and that lib dir must be this ctx's CTX_LIB (also
// exported by the forwarder). Without CTX_BIN_PATH (ctx-core run directly)
// it refuses. os.Executable is not used: it is ctx-core, not ctx.
//
// Every rm -rf target passes rmRfOK before the prompt, and what is deleted is
// the physical path rmRfOK checked, never the string as given. Any doubt is a
// refusal ("Refusing to …", exit 1) with nothing removed, followed by the
// list of what to remove by hand. --purge also refuses when the secret store
// is not available. After Yes it deletes, in order: with --purge each
// profile's secrets (listed again afterwards: a key left stops everything,
// since the stores' delete commands do not report failures), the guard (when
// installed), ctx, the ctx-core next to it, the lib dir, and with --purge
// $CTX_DIR. A removal that fails is reported and makes the exit code 1.
// --dry-run lists the actions.
func Uninstall(t tui.Term, args []string) int {
	u := termUI(t)
	purge := false
	for _, a := range args {
		if a != "--purge" {
			return u.die("Usage: ctx uninstall [--purge]")
		}
		purge = true
	}

	// Ctrl+C anywhere (also while the list prints, before the prompt) cancels
	// with the cancel line and 130.
	defer OnInterrupt(t, nil)()
	ctxBin, err := exec.LookPath("ctx")
	if err != nil && !errors.Is(err, exec.ErrDot) {
		ctxBin = ""
	}
	if ctxBin == "" {
		return u.die("ctx not found in PATH")
	}
	data := ctxDirRaw()
	installLib := ""
	refuse := func(msg string) int {
		u.error(msg)
		uninstallManual(u, ctxBin, installLib, data, purge)
		return 1
	}
	if st, err := os.Lstat(ctxBin); err == nil && st.Mode()&os.ModeSymlink != 0 {
		l, _ := os.Readlink(ctxBin)
		return refuse("Refusing to uninstall: " + ctxBin + " is a symlink (to " + l + "). Remove the link yourself, or put the installed ctx first on PATH.")
	}
	if !isRegular(ctxBin) {
		return refuse("Refusing to uninstall: " + ctxBin + " is not a regular file.")
	}
	self := os.Getenv("CTX_BIN_PATH")
	me, ok := physFile(self)
	if self == "" || !ok {
		me = ""
	}
	bin, _ := physFile(ctxBin)
	if me == "" || bin != me || !sameFile(bin, ctxBin) {
		return refuse("Refusing to uninstall: the ctx on PATH (" + ctxBin + ") is not the one running (" + self + ").")
	}
	installLib = installedLib(ctxBin)
	if installLib == "" {
		return refuse("Refusing to uninstall: " + ctxBin + " has no installer CTX_LIB line (a source checkout?). Remove it by hand.")
	}
	running := os.Getenv("CTX_LIB")
	a, _ := physDir(installLib)
	b, ok := physDir(running)
	if !ok {
		b = "?"
	}
	if a != b {
		return refuse("Refusing to uninstall: " + ctxBin + " names " + installLib + ", but this ctx runs from " + running + ".")
	}
	lib, why := rmRfOK(installLib, "lib")
	if why != "" {
		return refuse("Refusing to remove " + installLib + ": " + why + ".")
	}
	dataPhys := ""
	if _, err := os.Lstat(trimSlashes(data)); purge && err == nil {
		if dataPhys, why = rmRfOK(data, "data", bashDirname(bin), lib); why != "" {
			return refuse("Refusing to remove " + data + ": " + why + ".")
		}
	}
	core := bashDirname(bin) + "/ctx-core"
	if _, err := os.Lstat(core); err != nil {
		core = ""
	}

	var store secret.Store
	var label string
	var profiles []string
	if purge {
		provider := secret.EffectiveProvider()
		label = secret.StoreLabel()
		if store, err = secret.OpenWith(provider); err != nil {
			return refuse("Refusing to purge: the " + label + " (" + provider + ") is not available here, so the profiles' secrets cannot be deleted.")
		}
		profiles, _ = profile.List(profile.Dir())
	}
	// keysOf lists p's keys; ok is false when the store could not be listed.
	keysOf := func(p string) (keys []string, ok bool) {
		if store == nil {
			return nil, true
		}
		all, err := store.List(p)
		if err != nil {
			return nil, false
		}
		for _, k := range all {
			if k != "" {
				keys = append(keys, k)
			}
		}
		return keys, true
	}

	u.bold("\n  ctx uninstall\n")
	u.warn("This removes the ctx binary and library scripts from your install location.")
	if purge {
		u.warn("Also deletes " + data + " (profiles, backups) and every profile's secrets in the " + label + ".")
	}
	u.dim("  " + bin)
	if core != "" {
		u.dim("  " + core)
	}
	u.dim("  " + lib)
	if dataPhys != "" {
		u.dim("  " + dataPhys)
	}
	u.say("")

	if FlagsFromEnv().DryRun {
		for _, p := range profiles {
			keys, ok := keysOf(p)
			if !ok {
				u.dim("  [dry-run] could not list the secrets of " + p + " in the " + label)
			} else if n := len(keys); n > 0 {
				u.dim("  [dry-run] would delete " + strconv.Itoa(n) + " secret(s) of " + p + " from the " + label)
			}
		}
		if guard.Installed() {
			u.dim("  [dry-run] would run ctx guard uninstall")
		}
		u.dim("  [dry-run] would remove " + bin)
		if core != "" {
			u.dim("  [dry-run] would remove " + core)
		}
		u.dim("  [dry-run] would remove " + lib)
		if dataPhys != "" {
			u.dim("  [dry-run] would remove " + dataPhys)
		}
		u.say("")
		return 0
	}

	yes, err := t.Prompter().AskYN("Continue?", false)
	if err != nil {
		return t.Cancelled()
	}
	if !yes {
		return u.die("Aborted.")
	}

	// Secrets first; any key left after deleting stops everything.
	failed := false
	for _, p := range profiles {
		keys, ok := keysOf(p)
		if !ok {
			u.error("Could not list the secrets of " + p + " in the " + label)
			failed = true
			continue
		}
		if len(keys) == 0 {
			continue
		}
		for _, k := range keys {
			_ = store.Delete(p, k)
		}
		left, ok := keysOf(p)
		if !ok {
			u.error("Could not list the secrets of " + p + " in the " + label + " after deleting; cannot verify they are gone")
			failed = true
			continue
		}
		if n := len(keys) - len(left); n > 0 {
			u.success("Deleted " + strconv.Itoa(n) + " secret(s) of " + p + " from the " + label)
		}
		if len(left) > 0 {
			u.error("Could not delete from the " + label + " for " + p + ": " + strings.Join(left, " "))
			failed = true
		}
	}
	if failed {
		u.info("Delete them with: ctx secret delete <profile> <KEY> (or in the " + label + "), then run ctx uninstall --purge again.")
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
	remove(bin, rmF)
	if core != "" {
		remove(core, rmF)
	}
	remove(lib, os.RemoveAll)
	if dataPhys != "" {
		remove(dataPhys, os.RemoveAll)
	}

	u.say("")
	u.info("Remove the ctx blocks from your shell rc if desired (search for '# BEGIN ctx profile autoswitch').")
	u.say("")
	if failed {
		return 1
	}
	return 0
}

// uninstallManual mirrors _ctx_uninstall_manual: what to remove by hand
// after a refusal (lib "" when not known yet).
func uninstallManual(u ui, bin, lib, data string, purge bool) {
	u.info("To uninstall by hand, remove:")
	u.dim("  " + bin)
	if _, err := os.Lstat(bashDirname(bin) + "/ctx-core"); err == nil {
		u.dim("  " + bashDirname(bin) + "/ctx-core")
	}
	if lib != "" {
		u.dim("  " + lib)
	} else {
		u.dim("  its lib dir (the CTX_LIB= line in " + bin + ", if any)")
	}
	if purge {
		u.dim("  " + data)
		u.dim("  Delete each profile's secrets first: ctx secret list <profile>, then ctx secret delete <profile> <KEY>.")
	}
}

// trimSlashes drops every trailing slash ("/" stays "/").
func trimSlashes(p string) string {
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
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

// sameFile mirrors [[ a -ef b ]]: both exist and are the same file.
func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	return err == nil && os.SameFile(sa, sb)
}

// holds mirrors _ctx_holds: whether directory cand is path or one of its
// ancestors, by file identity (never by name: case-insensitive file systems
// and symlinks spell one directory many ways). A path that does not exist is
// walked from its nearest existing ancestor, over physical paths.
func holds(cand, path string) bool {
	d := path
	for d != "/" && d != "." {
		if _, err := os.Stat(d); err == nil {
			break
		}
		d = bashDirname(d)
	}
	ph, ok := physDir(d)
	if !ok {
		ph = d
	}
	for {
		if sameFile(ph, cand) {
			return true
		}
		if ph == "/" || ph == "." {
			return false
		}
		ph = bashDirname(ph)
	}
}

// rmRfOK mirrors _ctx_rm_rf_ok: the physical path to delete when rm -rf of
// dir is safe (why ""), else why not. kind is "lib" (the install's lib dir) or
// "data" (CTX_DIR for --purge); keep are install dirs a data dir must not
// hold. Refused: a newline in it, not absolute, a . or .. component, a
// symlink (trailing slashes ignored), not a directory, /, $HOME or a
// directory holding it, CTX_DIR's parent; a lib dir not named ctx, holding
// CTX_DIR, without core.sh, or a git checkout; a data dir not named .ctx with
// neither a config file nor a profiles dir (R15: ctx-core cannot tell an
// explicit CTX_DIR from the default, which always ends in .ctx), or holding
// the install. Directories are compared by identity, not by name.
func rmRfOK(dir, kind string, keep ...string) (phys, why string) {
	if strings.Contains(dir, "\n") {
		return "", "it contains a newline"
	}
	if !strings.HasPrefix(dir, "/") {
		return "", "it is not an absolute path"
	}
	if hasDotComponent(dir) {
		return "", "it has a . or .. component"
	}
	t := trimSlashes(dir)
	if t != "/" {
		if st, err := os.Lstat(t); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", "it is a symlink"
		}
	}
	if !isDir(t) {
		return "", "it is not a directory"
	}
	p, ok := physDir(t)
	if !ok {
		return "", "it cannot be resolved"
	}
	if strings.Contains(p, "\n") {
		return "", "it resolves to a path with a newline"
	}
	if !sameFile(p, t) {
		return "", "it cannot be resolved"
	}
	if sameFile(p, "/") {
		return "", "it is /"
	}
	if holds(p, os.Getenv("HOME")) {
		return "", "it is your home directory or holds it"
	}
	data := ctxDirRaw()
	if sameFile(p, data+"/..") || sameFile(p, bashDirname(data)) {
		return "", "it is the parent of CTX_DIR"
	}
	if kind == "lib" {
		if bashBasename(p) != "ctx" {
			return "", "its name is not 'ctx'"
		}
		if holds(p, data) {
			return "", "it holds CTX_DIR"
		}
		if !isRegular(p + "/core.sh") {
			return "", "it has no core.sh"
		}
		if _, err := os.Stat(p + "/.git"); err == nil {
			return "", "it is a git checkout"
		}
		return p, ""
	}
	if bashBasename(t) != ".ctx" && !isRegular(p+"/config") && !isDir(p+"/profiles") {
		return "", "it does not look like a ctx data directory (no config or profiles)"
	}
	for _, k := range keep {
		if holds(p, k) {
			return "", "it holds the ctx install (" + k + ")"
		}
	}
	return p, ""
}
