package ctxconfig

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Dir returns the ctx directory ($CTX_DIR, default $HOME/.ctx).
func Dir() string { return filepath.Dir(Path()) }

// ErrMultiLine is Upsert's error for a value containing a newline; its text is
// what _ctx_config_upsert_line prints after "✗ ".
type ErrMultiLine struct{ Key string }

func (e *ErrMultiLine) Error() string {
	return fmt.Sprintf("config value for '%s' must be a single line", e.Key)
}

// InitDirs mirrors ctx_init_dirs: mkdir -p the ctx, profiles and backups dirs,
// touch the config, chmod 700 the first two.
func InitDirs() error {
	dir := Dir()
	var first error
	keep := func(err error) {
		if first == nil {
			first = err
		}
	}
	for _, d := range []string{dir, filepath.Join(dir, "profiles"), filepath.Join(dir, "backups")} {
		keep(os.MkdirAll(d, 0o777))
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_WRONLY, 0o666)
	if err == nil {
		f.Close()
		now := time.Now()
		_ = os.Chtimes(Path(), now, now) // touch
	} else {
		keep(err)
	}
	keep(os.Chmod(dir, 0o700))
	keep(os.Chmod(filepath.Join(dir, "profiles"), 0o700))
	return first
}

// WithLock runs fn holding an exclusive flock on $CTX_DIR/config.lock. The lock
// is Go-only (Bash does not take it) and best effort: if the lock file cannot
// be opened fn still runs.
func WithLock(fn func() error) error {
	f, err := os.OpenFile(filepath.Join(Dir(), "config.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fn()
	}
	defer f.Close()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

// splitLines splits like `while read -r line || [[ -n $line ]]`: a final line
// without newline counts, a trailing newline does not add an empty line.
func splitLines(data string) []string {
	if data == "" {
		return nil
	}
	lines := strings.Split(data, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// writeAtomic replaces path with data through a 0600 temp file, like mktemp + mv.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// Upsert mirrors _ctx_config_upsert_line: key=value replaces the first line
// that starts with "key=", later duplicates are dropped, and a missing key is
// appended. Values are literal. It returns *ErrMultiLine for a value with a newline.
func Upsert(key, value string) error {
	if strings.Contains(value, "\n") {
		return &ErrMultiLine{Key: key}
	}
	_ = InitDirs() // Bash ignores its failure; the write below reports real problems
	return WithLock(func() error {
		path := Path()
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		var b strings.Builder
		found := false
		prefix := key + "="
		for _, line := range splitLines(string(data)) {
			if strings.HasPrefix(line, prefix) {
				if !found {
					b.WriteString(prefix + value + "\n")
				}
				found = true
				continue
			}
			b.WriteString(line + "\n")
		}
		if !found {
			b.WriteString(prefix + value + "\n")
		}
		return writeAtomic(path, []byte(b.String()))
	})
}

// Remove mirrors _ctx_config_rm_key for each key: drop every "key=" line. A
// missing config is not an error and is not created.
func Remove(keys ...string) error {
	if _, err := os.Stat(Path()); err != nil {
		return nil
	}
	return WithLock(func() error {
		data, err := os.ReadFile(Path())
		if err != nil {
			return err
		}
		var b strings.Builder
	lines:
		for _, line := range splitLines(string(data)) {
			for _, k := range keys {
				if strings.HasPrefix(line, k+"=") {
					continue lines
				}
			}
			b.WriteString(line + "\n")
		}
		return writeAtomic(Path(), []byte(b.String()))
	})
}

// Log mirrors log() in lib/core.sh: append "[time] msg" to $CTX_DIR/ctx.log.
func Log(msg string) {
	dir := Dir()
	_ = os.MkdirAll(dir, 0o777)
	f, err := os.OpenFile(filepath.Join(dir, "ctx.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", Now().Format("2006-01-02 15:04:05"), msg)
}

// SetActive mirrors set_active_profile.
func SetActive(name string) error {
	if err := Upsert("active", name); err != nil {
		return err
	}
	Log("Switched to profile: " + name)
	return nil
}

// SetManual mirrors ctx_activation_set_manual: lock auto-switch to pwd.
func SetManual(pwd string) error {
	b64 := base64.StdEncoding.EncodeToString([]byte(pwd))
	if err := Upsert("active_source", "manual"); err != nil {
		return err
	}
	return Upsert("manual_pwd", b64)
}

// SetAuto mirrors ctx_activation_set_auto.
func SetAuto() error { return Remove("active_source", "manual_pwd") }

// ClearActivation mirrors clear_active_profile_activation.
func ClearActivation() error {
	_ = InitDirs()
	return Remove("active", "active_source", "manual_pwd")
}
