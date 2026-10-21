// Package atomicfile replaces a user's file in place without ever leaving it
// half written: the new content goes to a temp file in the target's own
// directory, which is renamed over the target. Symlinks are followed, so a
// dotfile manager's link stays a link. It has no ctx dependencies, so any
// package (cli, gitssh) can use it.
package atomicfile

import (
	"os"
	"path/filepath"
	"syscall"
)

// FollowLinks resolves a chain of symlinks (at most 40), also when the last
// one dangles (the file is then created where it points), like the readlink
// loop of _ctx_link_target in lib/core.sh.
func FollowLinks(path string) string {
	for i := 0; i < 40; i++ {
		st, err := os.Lstat(path)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			break
		}
		dest, err := os.Readlink(path)
		if err != nil {
			break
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(path), dest)
		}
		path = dest
	}
	return path
}

// WriteThrough atomically replaces the file path points to (an existing
// symlink chain is followed, also a dangling one: the file is then created
// where it points) with data. mode picks the result's mode from the current
// one (existed false: there is no file yet); it is called once. write fills
// the temp file; nil writes data with f.Write (tests inject failures here).
// A chain that does not end (a link loop, or over 40 links) is an ELOOP
// error. On any error the target is as it was and the temp file is removed.
func WriteThrough(path string, data []byte, mode func(old os.FileMode, existed bool) os.FileMode, write func(f *os.File, data []byte) error) error {
	path = FollowLinks(path)
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return &os.PathError{Op: "write", Path: path, Err: syscall.ELOOP}
	}
	if real, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(real, filepath.Base(path))
	}
	var m os.FileMode
	if st, err := os.Stat(path); err == nil {
		m = mode(st.Mode().Perm(), true)
	} else {
		m = mode(0, false)
	}
	if write == nil {
		write = func(f *os.File, data []byte) error { _, err := f.Write(data); return err }
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".")
	if err != nil {
		return err
	}
	werr := write(tmp, data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), m)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
	}
	return werr
}
