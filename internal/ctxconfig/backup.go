package ctxconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

// Now is the clock for backup stamps and log lines; tests replace it.
var Now = time.Now

func umask() os.FileMode {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return os.FileMode(m)
}

// CopyFile copies like cp src dst, atomically: an existing dst keeps its mode,
// a new one gets src's mode minus the umask. A symlink dst is written through.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", src)
	}
	if real, err := filepath.EvalSymlinks(dst); err == nil {
		dst = real
	}
	mode := st.Mode().Perm() &^ umask()
	if dstSt, err := os.Stat(dst); err == nil {
		if dstSt.IsDir() {
			return fmt.Errorf("%s: is a directory", dst)
		}
		if os.SameFile(st, dstSt) {
			return fmt.Errorf("'%s' and '%s' are the same file", src, dst)
		}
		mode = dstSt.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.ReadFrom(in)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err == nil {
		err = os.Rename(name, dst)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// BackupFile mirrors backup_file: copy src into $CTX_DIR/backups/<stamp>/ and
// record it in that directory's manifest.txt. A missing src is not an error.
// When the directory or the copy fails it returns the error at once, with
// nothing recorded or logged, as backup_file returns 1.
func BackupFile(src string) error {
	if st, err := os.Stat(src); err != nil || !st.Mode().IsRegular() {
		return nil
	}
	bdir := filepath.Join(Dir(), "backups", Now().Format("20060102-150405"))
	if err := os.MkdirAll(bdir, 0o777); err != nil {
		return err
	}
	if err := CopyFile(src, filepath.Join(bdir, filepath.Base(src))); err != nil {
		return err
	}
	var first error
	f, err := os.OpenFile(filepath.Join(bdir, "manifest.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err == nil {
		fmt.Fprintln(f, src)
		f.Close()
	} else {
		first = err
	}
	Log("Backed up " + src + " → " + bdir)
	return first
}

// LatestBackup mirrors latest_backup: the newest entry of $CTX_DIR/backups
// (`ls -1t | head -1`), "" when there is none.
func LatestBackup() string {
	ents, err := os.ReadDir(filepath.Join(Dir(), "backups"))
	if err != nil || len(ents) == 0 {
		return ""
	}
	type ent struct {
		name string
		mod  time.Time
	}
	var es []ent
	for _, e := range ents {
		if info, err := e.Info(); err == nil {
			es = append(es, ent{e.Name(), info.ModTime()})
		}
	}
	sort.SliceStable(es, func(i, j int) bool {
		if !es[i].mod.Equal(es[j].mod) {
			return es[i].mod.After(es[j].mod)
		}
		return es[i].name < es[j].name
	})
	if len(es) == 0 {
		return ""
	}
	return es[0].name
}
