package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// ctxDirRaw is $CTX_DIR as Bash sees it (${CTX_DIR:-$HOME/.ctx}), uncleaned,
// for messages and paths that Bash builds by concatenation.
func ctxDirRaw() string {
	if d := os.Getenv("CTX_DIR"); d != "" {
		return d
	}
	return os.Getenv("HOME") + "/.ctx"
}

// Undo runs `ctx undo`, mirroring cmd_undo in lib/cmd_ops.sh (R7), and
// returns the exit code. Arguments are ignored. It restores the files listed
// in the latest backup's manifest.txt:
//   - The backup holds one copy per basename, so when two manifest paths share
//     a basename (or the basename is manifest.txt) neither is restored
//     ("Ambiguous backup"). A path that is not absolute, has a . or ..
//     component, is not in the backup, or whose directory no longer exists is
//     skipped too. A repeated line counts once.
//   - The restorable files are listed before the prompt; with none it dies.
//   - The current versions go into a new backup dir first (never the one being
//     restored, even in the same second), so `ctx undo` again undoes the undo.
//   - A restored file gets the backup's mode and is written through a symlink,
//     atomically (temp file in the target's dir, renamed over it).
//   - Any skipped or failed file makes the exit code 1.
//
// --dry-run (CTX_DRY_RUN=1) lists the actions and changes nothing. No exits 1
// ("Aborted."); Ctrl+C or EOF at the prompt exits 130.
func Undo(t tui.Term, args []string) int {
	u := termUI(t)
	dir := ctxDirRaw()
	latest := ctxconfig.LatestBackup()
	if latest == "" {
		return u.die("No backups found in " + dir + "/backups")
	}
	bdir := dir + "/backups/" + latest
	u.bold("\n  Restoring from backup: " + latest + "\n")

	manifest := bdir + "/manifest.txt"
	if st, err := os.Stat(manifest); err != nil || !st.Mode().IsRegular() {
		return u.die("Manifest not found at " + manifest)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return u.die("Cannot read " + manifest)
	}

	var origs []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		origs = append(origs, line)
	}
	names := map[string]int{}
	for _, o := range origs {
		names[bashBasename(o)]++
	}

	failed := false
	var todo []string
	for _, o := range origs {
		name := bashBasename(o)
		switch {
		case names[name] > 1 || name == "manifest.txt":
			u.warn("Ambiguous backup for " + o + ": another file in it has the same name; skipped")
			failed = true
		case !strings.HasPrefix(o, "/"):
			u.warn("Cannot restore " + o + ": not an absolute path; skipped")
			failed = true
		case hasDotComponent(o):
			u.warn("Cannot restore " + o + ": it has a . or .. component; skipped")
			failed = true
		case !isRegular(bdir + "/" + name):
			u.warn("Not in backup: " + o)
			failed = true
		case !isDir(bashDirname(o)):
			u.warn("Cannot restore " + o + ": " + bashDirname(o) + " no longer exists; skipped")
			failed = true
		default:
			u.dim("  " + o)
			todo = append(todo, o)
		}
	}
	if len(todo) == 0 {
		return u.die("Nothing to restore from " + latest + ".")
	}
	u.say("")

	if FlagsFromEnv().DryRun {
		for _, o := range todo {
			if isRegular(o) {
				u.dim("  [dry-run] would back up the current " + o)
			}
			u.dim("  [dry-run] would restore " + o)
		}
		u.say("")
		return 0
	}

	defer OnInterrupt(t, nil)()
	yes, err := t.Prompter().AskYN("Restore these files?", true)
	if err != nil {
		return t.Cancelled()
	}
	if !yes {
		return u.die("Aborted.")
	}

	// The backup dir is made before anything is restored.
	nb := ""
	for _, o := range todo {
		if isRegular(o) {
			if nb, err = ctxconfig.NewBackupDir(); err != nil {
				return u.die("Could not create a backup directory; nothing restored.")
			}
			break
		}
	}
	for _, o := range todo {
		if isRegular(o) {
			if ctxconfig.BackupFileIn(nb, o) != nil {
				u.error("Could not back up the current " + o + "; not restored")
				failed = true
				continue
			}
		}
		if restoreFile(bdir+"/"+bashBasename(o), o) == nil {
			u.success("Restored: " + o)
		} else {
			u.error("Could not restore " + o)
			failed = true
		}
	}

	if nb != "" {
		u.dim("  The replaced versions are in " + nb + " (run ctx undo again to put them back).")
	}
	if failed {
		u.warn("Undo finished, but some files were not restored.")
		u.say("")
		return 1
	}
	u.success("Undo complete.")
	u.say("")
	return 0
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// bashBasename is basename(1): trailing slashes dropped, "/" for "/".
func bashBasename(p string) string {
	t := strings.TrimRight(p, "/")
	if t == "" {
		if p == "" {
			return ""
		}
		return "/"
	}
	return t[strings.LastIndex(t, "/")+1:]
}

// bashDirname is dirname(1).
func bashDirname(p string) string {
	t := strings.TrimRight(p, "/")
	if t == "" {
		if p == "" {
			return "."
		}
		return "/"
	}
	i := strings.LastIndex(t, "/")
	if i < 0 {
		return "."
	}
	d := strings.TrimRight(t[:i], "/")
	if d == "" {
		return "/"
	}
	return d
}

// physFile mirrors _ctx_phys_file: symlinks of the last component followed
// (even to a missing target, at most 40), the directory resolved.
func physFile(p string) (string, bool) {
	for i := 0; ; i++ {
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			break
		}
		if i >= 40 {
			return "", false
		}
		l, err := os.Readlink(p)
		if err != nil {
			return "", false
		}
		if !strings.HasPrefix(l, "/") {
			l = bashDirname(p) + "/" + l
		}
		p = l
	}
	d, ok := physDir(bashDirname(p))
	if !ok {
		return "", false
	}
	return strings.TrimRight(d, "/") + "/" + bashBasename(p), true
}

// physDir mirrors _ctx_phys_dir, (cd -P DIR && pwd -P): symlinks are
// resolved before any .. that follows them, as the kernel does (the path is
// not cleaned lexically first).
func physDir(p string) (string, bool) {
	if p == "" || !isDir(p) {
		return "", false
	}
	if !strings.HasPrefix(p, "/") {
		wd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		p = wd + "/" + p
	}
	d, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	return d, true
}

// hasDotComponent mirrors _ctx_has_dot_component: p has a . or .. component.
func hasDotComponent(p string) bool {
	for _, c := range strings.Split(p, "/") {
		if c == "." || c == ".." {
			return true
		}
	}
	return false
}

// restoreFile mirrors _ctx_restore_file: src's content and permission bits at
// dst, written through symlinks via a temp file renamed over the target.
func restoreFile(src, dst string) error {
	real, ok := physFile(dst)
	if !ok {
		return os.ErrNotExist
	}
	if isDir(real) {
		return os.ErrExist
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(bashDirname(real), ".ctx-undo.")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(in)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, st.Mode().Perm())
	}
	if err == nil {
		err = os.Rename(name, real)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
