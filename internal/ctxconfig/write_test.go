package ctxconfig

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// bashFn runs fn(args...) from lib/core.sh with HOME=root and CTX_DIR=root/.ctx.
func bashFn(t *testing.T, root, fn string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	core, err := filepath.Abs("../../lib/core.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{"-c", `source "$1" >/dev/null 2>&1; shift; "$@"`, "bash", core, fn}, args...)...)
	cmd.Env = append(os.Environ(), "CTX_DIR="+filepath.Join(root, ".ctx"), "HOME="+root, "TZ=UTC")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

// goRoot points the process at a fresh root and returns it.
func goRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("CTX_DIR", filepath.Join(root, ".ctx"))
	t.Setenv("TZ", "UTC")
	return root
}

func seedConfig(t *testing.T, root string, content *string) {
	t.Helper()
	if content == nil {
		return
	}
	dir := filepath.Join(root, ".ctx")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(*content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strp(s string) *string { return &s }

func TestUpsertMatchesBash(t *testing.T) {
	type op struct{ key, val string }
	cases := []struct {
		name string
		seed *string
		ops  []op
	}{
		{"missing file", nil, []op{{"work_root", "/x"}}},
		{"empty file", strp(""), []op{{"a", "1"}}},
		{"new key appended", strp("a=1\nb=2\n"), []op{{"c", "3"}}},
		{"existing key replaced in place", strp("a=1\nb=2\nc=3\n"), []op{{"b", "new"}}},
		{"duplicate keys collapse to first position", strp("a=1\nb=2\na=3\nc=4\na=5\n"), []op{{"a", "z"}}},
		{"no trailing newline", strp("a=1\nb=2"), []op{{"b", "9"}}},
		{"no trailing newline, new key", strp("a=1\nb=2"), []op{{"c", "9"}}},
		{"prefix key untouched", strp("active_source=manual\nactive=p\n"), []op{{"active", "q"}}},
		{"blank lines and comments kept", strp("# c\n\na=1\n\n"), []op{{"a", "2"}}},
		{"crlf lines kept", strp("a=1\r\nb=2\r\n"), []op{{"b", "3"}}},
		{"special chars", strp("a=1\n"), []op{{"a", `x&y|z\1\n&`}, {"b", `$HOME "q" 'r' ` + "`t`"}}},
		{"empty value", strp("a=1\n"), []op{{"a", ""}}},
		{"value with equals", strp(""), []op{{"a", "b=c=d"}}},
		{"multi-line rejected", strp("a=1\n"), []op{{"a", "x\ny"}}},
		{"multi-line rejected on missing file", nil, []op{{"a", "x\n"}}},
		{"sequence", strp("a=1\n"), []op{{"a", "2"}, {"b", "3"}, {"a", "4"}, {"b", "x\ny"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			groot := goRoot(t)
			broot, _ := filepath.EvalSymlinks(t.TempDir())
			seedConfig(t, groot, c.seed)
			seedConfig(t, broot, c.seed)
			for _, o := range c.ops {
				gerr := Upsert(o.key, o.val)
				_, berr, bcode := bashFn(t, broot, "_ctx_config_upsert_line", o.key, o.val)
				if (gerr != nil) != (bcode != 0) {
					t.Fatalf("%v: go err %v, bash exit %d", o, gerr, bcode)
				}
				if gerr != nil {
					if want := "✗ " + gerr.Error() + "\n"; berr != want {
						t.Errorf("%v: bash stderr %q, go error text implies %q", o, berr, want)
					}
				} else if berr != "" {
					t.Errorf("%v: bash stderr %q", o, berr)
				}
			}
			if d := testutil.DiffSnapshots(testutil.Snapshot(t, groot), testutil.Snapshot(t, broot)); d != "" {
				t.Errorf("trees differ:\n%s", d)
			}
		})
	}
}

func TestRemoveMatchesBash(t *testing.T) {
	for name, seed := range map[string]*string{
		"missing":      nil,
		"keys":         strp("active=p\nactive_source=manual\nmanual_pwd=eA==\nother=1\nactive=q\n"),
		"no trailing":  strp("a=1\nactive=p"),
		"only removed": strp("active=p\n"),
		"prefix":       strp("active_x=1\nxactive=2\nactive=3\n"),
	} {
		t.Run(name, func(t *testing.T) {
			groot := goRoot(t)
			broot, _ := filepath.EvalSymlinks(t.TempDir())
			seedConfig(t, groot, seed)
			seedConfig(t, broot, seed)
			if err := Remove("active"); err != nil {
				t.Fatal(err)
			}
			bashFn(t, broot, "_ctx_config_rm_key", "active")
			if err := Remove("active_source", "manual_pwd"); err != nil {
				t.Fatal(err)
			}
			bashFn(t, broot, "_ctx_config_rm_key", "active_source")
			bashFn(t, broot, "_ctx_config_rm_key", "manual_pwd")
			if d := testutil.DiffSnapshots(testutil.Snapshot(t, groot), testutil.Snapshot(t, broot)); d != "" {
				t.Errorf("trees differ:\n%s", d)
			}
		})
	}
}

func TestActivationMatchesBash(t *testing.T) {
	seed := strp("work_root=/w\nactive=old\nactive=old2\n")
	for _, pwd := range []string{"/Users/me/clients/acme", "/tmp/with space/é", "", strings.Repeat("/long-segment", 20)} {
		t.Run(fmt.Sprintf("pwd=%.12q", pwd), func(t *testing.T) {
			groot := goRoot(t)
			broot, _ := filepath.EvalSymlinks(t.TempDir())
			seedConfig(t, groot, seed)
			seedConfig(t, broot, seed)
			if err := SetActive("acme"); err != nil {
				t.Fatal(err)
			}
			bashFn(t, broot, "set_active_profile", "acme")
			if err := SetManual(pwd); err != nil {
				t.Fatal(err)
			}
			bashFn(t, broot, "ctx_activation_set_manual", pwd)
			if d := testutil.DiffSnapshots(testutil.Snapshot(t, groot), testutil.Snapshot(t, broot)); d != "" {
				t.Errorf("after set: trees differ:\n%s", d)
			}
			cfg, err := Load()
			if err != nil || cfg.ActiveSource != "manual" || (pwd != "" && cfg.ManualPWD != pwd) {
				t.Errorf("Load after SetManual = %+v, %v", cfg, err)
			}
			if err := SetAuto(); err != nil {
				t.Fatal(err)
			}
			bashFn(t, broot, "ctx_activation_set_auto")
			if d := testutil.DiffSnapshots(testutil.Snapshot(t, groot), testutil.Snapshot(t, broot)); d != "" {
				t.Errorf("after auto: trees differ:\n%s", d)
			}
			if err := SetManual(pwd); err != nil {
				t.Fatal(err)
			}
			bashFn(t, broot, "ctx_activation_set_manual", pwd)
			if err := ClearActivation(); err != nil {
				t.Fatal(err)
			}
			bashFn(t, broot, "clear_active_profile_activation")
			if d := testutil.DiffSnapshots(testutil.Snapshot(t, groot), testutil.Snapshot(t, broot)); d != "" {
				t.Errorf("after clear: trees differ:\n%s", d)
			}
		})
	}
}

func TestSetManualEncoding(t *testing.T) {
	root := goRoot(t)
	if err := SetManual("/a b"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".ctx", "config"))
	want := "active_source=manual\nmanual_pwd=" + base64.StdEncoding.EncodeToString([]byte("/a b")) + "\n"
	if string(data) != want {
		t.Errorf("config = %q, want %q", data, want)
	}
}

func TestInitDirsMatchesBash(t *testing.T) {
	groot := goRoot(t)
	broot, _ := filepath.EvalSymlinks(t.TempDir())
	if err := InitDirs(); err != nil {
		t.Fatal(err)
	}
	bashFn(t, broot, "ctx_init_dirs")
	if d := testutil.DiffSnapshots(testutil.Snapshot(t, groot), testutil.Snapshot(t, broot)); d != "" {
		t.Errorf("trees differ:\n%s", d)
	}
}

func TestConfigUpsertConcurrent(t *testing.T) {
	root := goRoot(t)
	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := Upsert(fmt.Sprintf("k%d", i), fmt.Sprint(i)); err != nil {
				t.Errorf("Upsert k%d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	data, _ := os.ReadFile(filepath.Join(root, ".ctx", "config"))
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d (lost updates):\n%s", len(lines), n, data)
	}
}

func TestBackupFileMatchesBash(t *testing.T) {
	groot := goRoot(t)
	broot, _ := filepath.EvalSymlinks(t.TempDir())
	// Freeze both clocks to one stamp so a second boundary cannot split the run.
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	oldNow := Now
	Now = func() time.Time { return fixed }
	defer func() { Now = oldNow }()
	t.Setenv("PATH", testutil.FixedStampDate(t, fixed.Format("20060102-150405"))+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, r := range []string{groot, broot} {
		if err := os.MkdirAll(filepath.Join(r, ".ctx"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r, ".ctx", "config"), []byte("a=1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r, ".gitconfig"), []byte("[user]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".ctx/config", ".gitconfig", "missing"} {
		if err := BackupFile(filepath.Join(groot, name)); err != nil {
			t.Fatal(err)
		}
		bashFn(t, broot, "backup_file", filepath.Join(broot, name))
	}
	if d := testutil.DiffSnapshots(testutil.Snapshot(t, groot), testutil.Snapshot(t, broot)); d != "" {
		t.Errorf("trees differ:\n%s", d)
	}
}

func TestBackupStampAndLatest(t *testing.T) {
	root := goRoot(t)
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := Now
	defer func() { Now = old }()
	for i, ts := range []string{"2026-01-02T03:04:05Z", "2026-01-02T03:04:06Z"} {
		tm, _ := time.Parse(time.RFC3339, ts)
		Now = func() time.Time { return tm.In(time.Local) }
		if err := BackupFile(filepath.Join(root, "f")); err != nil {
			t.Fatal(err)
		}
		d := filepath.Join(root, ".ctx", "backups", tm.In(time.Local).Format("20060102-150405"))
		if _, err := os.Stat(filepath.Join(d, "f")); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
		if err := os.Chtimes(d, tm, tm); err != nil {
			t.Fatal(err)
		}
	}
	want := time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC).In(time.Local).Format("20060102-150405")
	if got := LatestBackup(); got != want {
		t.Errorf("LatestBackup = %q, want %q", got, want)
	}
	_ = os.RemoveAll(filepath.Join(root, ".ctx", "backups"))
	if got := LatestBackup(); got != "" {
		t.Errorf("LatestBackup with no backups = %q", got)
	}
}

// TestNewBackupDirFresh: a stamp dir that exists is never reused (undo backs
// up into a new dir even in the second of the backup it restores).
func TestNewBackupDirFresh(t *testing.T) {
	root := goRoot(t)
	old := Now
	defer func() { Now = old }()
	fixed := time.Date(2026, 10, 20, 10, 10, 10, 0, time.Local)
	Now = func() time.Time { return fixed }
	base := filepath.Join(os.Getenv("CTX_DIR"), "backups")
	if err := os.MkdirAll(filepath.Join(base, "20261020-101010"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"20261020-101010-2", "20261020-101010-3"} {
		d, err := NewBackupDir()
		if err != nil {
			t.Fatal(err)
		}
		if d != filepath.Join(base, want) {
			t.Errorf("NewBackupDir = %q, want %s", d, want)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(base, "20261020-101010-2")
	if err := BackupFileIn(d, filepath.Join(root, "f")); err != nil {
		t.Fatal(err)
	}
	if m, _ := os.ReadFile(filepath.Join(d, "manifest.txt")); string(m) != filepath.Join(root, "f")+"\n" {
		t.Errorf("manifest %q", m)
	}
	if st, err := os.Stat(filepath.Join(d, "f")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("backup copy: %v %v", st, err)
	}
}
