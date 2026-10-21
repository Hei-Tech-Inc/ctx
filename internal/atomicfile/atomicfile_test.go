package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// keep is a mode callback that keeps an existing file's mode and gives a new
// one newMode, recording what it was called with.
func keep(newMode os.FileMode, calls *[]string) func(os.FileMode, bool) os.FileMode {
	return func(old os.FileMode, existed bool) os.FileMode {
		if existed {
			*calls = append(*calls, "existing "+old.String())
			return old
		}
		*calls = append(*calls, "new")
		return newMode
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func perm(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

// noTemps fails when dir holds anything but the names in want.
func noTemps(t *testing.T, dir string, want ...string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	ok := map[string]bool{}
	for _, w := range want {
		ok[w] = true
	}
	for _, e := range ents {
		if !ok[e.Name()] {
			t.Errorf("unexpected file left in %s: %s", dir, e.Name())
		}
	}
}

func TestWriteThroughNewFileGetsNewMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rc")
	var calls []string
	if err := WriteThrough(p, []byte("hello\n"), keep(0o640, &calls), nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "hello\n" {
		t.Errorf("content %q", got)
	}
	if m := perm(t, p); m != 0o640 {
		t.Errorf("mode %v, want 0640", m)
	}
	if strings.Join(calls, ",") != "new" {
		t.Errorf("mode callback calls %q, want [new]", calls)
	}
	noTemps(t, dir, "rc")
}

func TestWriteThroughExistingFileKeepsOrSetsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rc")
	if err := os.WriteFile(p, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o604); err != nil {
		t.Fatal(err)
	}
	var calls []string
	if err := WriteThrough(p, []byte("new\n"), keep(0o644, &calls), nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "new\n" {
		t.Errorf("content %q", got)
	}
	if m := perm(t, p); m != 0o604 {
		t.Errorf("mode %v, want 0604 kept", m)
	}
	if strings.Join(calls, ",") != "existing -rw----r--" {
		t.Errorf("mode callback calls %q", calls)
	}
	// The callback decides: here it tightens to 0600.
	if err := WriteThrough(p, []byte("newer\n"), func(os.FileMode, bool) os.FileMode { return 0o600 }, nil); err != nil {
		t.Fatal(err)
	}
	if m := perm(t, p); m != 0o600 {
		t.Errorf("mode %v, want 0600", m)
	}
	noTemps(t, dir, "rc")
}

func TestWriteThroughKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dots"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "dots", "zshrc")
	if err := os.WriteFile(target, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".zshrc")
	if err := os.Symlink("dots/zshrc", link); err != nil {
		t.Fatal(err)
	}
	// A second link in the chain: both stay.
	link2 := filepath.Join(dir, "rc")
	if err := os.Symlink(".zshrc", link2); err != nil {
		t.Fatal(err)
	}
	var calls []string
	if err := WriteThrough(link2, []byte("new\n"), keep(0o644, &calls), nil); err != nil {
		t.Fatal(err)
	}
	for l, want := range map[string]string{link: "dots/zshrc", link2: ".zshrc"} {
		if dest, err := os.Readlink(l); err != nil || dest != want {
			t.Errorf("%s: link %q %v, want %q", l, dest, err, want)
		}
	}
	if got := readFile(t, target); got != "new\n" {
		t.Errorf("target %q", got)
	}
	if m := perm(t, target); m != 0o640 {
		t.Errorf("target mode %v, want 0640", m)
	}
	noTemps(t, dir, "dots", ".zshrc", "rc")
	noTemps(t, filepath.Join(dir, "dots"), "zshrc")
}

func TestWriteThroughDanglingLinkCreatesTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dots"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".zshrc")
	if err := os.Symlink("dots/zshrc", link); err != nil {
		t.Fatal(err)
	}
	var calls []string
	if err := WriteThrough(link, []byte("x\n"), keep(0o640, &calls), nil); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link replaced: %v", err)
	}
	target := filepath.Join(dir, "dots", "zshrc")
	if got := readFile(t, target); got != "x\n" {
		t.Errorf("target %q", got)
	}
	if m := perm(t, target); m != 0o640 || strings.Join(calls, ",") != "new" {
		t.Errorf("mode %v, calls %q", m, calls)
	}
	noTemps(t, filepath.Join(dir, "dots"), "zshrc")
}

func TestWriteThroughLinkLoopIsAnError(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.Symlink("b", a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", b); err != nil {
		t.Fatal(err)
	}
	err := WriteThrough(a, []byte("x\n"), func(os.FileMode, bool) os.FileMode { return 0o644 }, nil)
	if !errors.Is(err, syscall.ELOOP) {
		t.Errorf("err %v, want ELOOP", err)
	}
	for l, want := range map[string]string{a: "b", b: "a"} {
		if dest, err := os.Readlink(l); err != nil || dest != want {
			t.Errorf("%s: link %q %v, want %q", l, dest, err, want)
		}
	}
	noTemps(t, dir, "a", "b")
}

func TestWriteThroughFailedWriteLeavesTarget(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rc")
	if err := os.WriteFile(p, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("disk full")
	err := WriteThrough(p, []byte("a much longer replacement\n"), func(os.FileMode, bool) os.FileMode { return 0o644 },
		func(f *os.File, data []byte) error { _, _ = f.Write(data[:3]); return boom })
	if !errors.Is(err, boom) {
		t.Errorf("err %v, want the write error", err)
	}
	if got := readFile(t, p); got != "mine\n" {
		t.Errorf("target changed: %q", got)
	}
	noTemps(t, dir, "rc")
}

func TestWriteThroughMissingDirIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := WriteThrough(filepath.Join(dir, "no", "rc"), []byte("x"), func(os.FileMode, bool) os.FileMode { return 0o644 }, nil); err == nil {
		t.Error("no error for a missing directory")
	}
	noTemps(t, dir)
}

func TestFollowLinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("sub/x", filepath.Join(dir, "l1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "l1"), filepath.Join(dir, "l2")); err != nil {
		t.Fatal(err)
	}
	if got, want := FollowLinks(filepath.Join(dir, "l2")), filepath.Join(dir, "sub", "x"); got != want {
		t.Errorf("FollowLinks = %q, want %q (dangling end followed)", got, want)
	}
	if got := FollowLinks(filepath.Join(dir, "plain")); got != filepath.Join(dir, "plain") {
		t.Errorf("FollowLinks of a non-link = %q", got)
	}
}
