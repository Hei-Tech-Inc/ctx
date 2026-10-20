package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

type lineOut struct{ lines []string }

func (r *lineOut) Info(m string)    { r.lines = append(r.lines, "info:"+m) }
func (r *lineOut) Success(m string) { r.lines = append(r.lines, "success:"+m) }
func (r *lineOut) Warn(m string)    { r.lines = append(r.lines, "warn:"+m) }
func (r *lineOut) Dim(m string)     { r.lines = append(r.lines, "dim:"+m) }
func (r *lineOut) Bold(m string)    { r.lines = append(r.lines, "bold:"+m) }
func (r *lineOut) Say(m string)     { r.lines = append(r.lines, "say:"+m) }
func (r *lineOut) Error(m string)   { r.lines = append(r.lines, "error:"+m) }

// onlyPath points PATH at the given dirs plus the system's basics.
func onlyPath(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("PATH", strings.Join(append(dirs, "/usr/bin", "/bin"), ":"))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectEmptyWhenToolsMissing(t *testing.T) {
	home := t.TempDir()
	onlyPath(t)
	d := Detect(home, time.Second)
	if !reflect.DeepEqual(d, Detected{}) {
		t.Errorf("want empty, got %#v", d)
	}
}

func TestDetectFiles(t *testing.T) {
	home := t.TempDir()
	onlyPath(t)
	write(t, filepath.Join(home, ".ssh", "id_b.pub"), "x")
	write(t, filepath.Join(home, ".ssh", "id_b"), "x")
	write(t, filepath.Join(home, ".ssh", "id_a.pub"), "x")
	write(t, filepath.Join(home, ".ssh", "id_a"), "x")
	write(t, filepath.Join(home, ".ssh", "lonely.pub"), "x")
	write(t, filepath.Join(home, ".ssh", "known_hosts"), "x")
	write(t, filepath.Join(home, ".aws", "credentials"), "[zeta]\nk=v\n[default]\n[Upper]\n")
	write(t, filepath.Join(home, ".aws", "config"), "[default]\n[profile beta]\n[profile zeta]\n")
	d := Detect(home, time.Second)
	if want := []string{filepath.Join(home, ".ssh", "id_a"), filepath.Join(home, ".ssh", "id_b")}; !reflect.DeepEqual(d.SSHKeys, want) {
		t.Errorf("ssh = %v", d.SSHKeys)
	}
	if want := []string{"Upper", "beta", "default", "zeta"}; !reflect.DeepEqual(d.AWSProfiles, want) {
		t.Errorf("aws = %v", d.AWSProfiles)
	}
}

func TestDetectGHBothFormats(t *testing.T) {
	home := t.TempDir()
	dir, _ := testutil.FakeBin(t, "gh", `echo "github.com"
echo "  ✓ Logged in to github.com account alice (keyring)"
echo "  X Failed to log in to github.com account broken (keyring)" >&2
echo "  ✓ Logged in to github.com as bob (keyring)"`)
	onlyPath(t, dir)
	d := Detect(home, 5*time.Second)
	if want := []string{"alice", "bob"}; !reflect.DeepEqual(d.GHAccounts, want) {
		t.Errorf("gh = %v", d.GHAccounts)
	}
}

func TestReport(t *testing.T) {
	r := &lineOut{}
	Detected{SSHKeys: []string{"a"}, KubeContexts: []string{"k", "j"}, GCPProjects: []string{"p"}}.Report(r)
	want := []string{
		"success:Found 1 SSH key(s)",
		"dim:  No gh accounts (run: gh auth login)",
		"success:Found 2 kubectl context(s)",
		"success:Found 1 GCP project(s)",
	}
	if !reflect.DeepEqual(r.lines, want) {
		t.Errorf("got %q", r.lines)
	}
	r = &lineOut{}
	Detected{}.Report(r)
	want = []string{"dim:  No SSH keys in ~/.ssh/", "dim:  No gh accounts (run: gh auth login)"}
	if !reflect.DeepEqual(r.lines, want) {
		t.Errorf("empty got %q", r.lines)
	}
}

func TestScanIntro(t *testing.T) {
	r := &lineOut{}
	ScanIntro(r, nil)
	want := []string{
		"info:Scanning local accounts/keys/profiles (this can take a few seconds)...",
		"dim:  Looking for SSH keys, gh auth, and cloud profile references...",
		"dim:  Detecting SSH keys / GitHub accounts / cloud profiles...",
	}
	if !reflect.DeepEqual(r.lines, want) {
		t.Errorf("got %q", r.lines)
	}
	r = &lineOut{}
	ran := false
	ScanIntro(r, func() { ran = true })
	if !ran || len(r.lines) != 2 {
		t.Errorf("spin: ran=%v lines=%q", ran, r.lines)
	}
}

func TestDetectTimeout(t *testing.T) {
	home := t.TempDir()
	// The fake spawns a grandchild that keeps stdout open, like a hung helper.
	body := `sleep 30 &
sleep 30`
	var dirs []string
	for _, n := range []string{"gh", "az", "gcloud"} {
		d, _ := testutil.FakeBin(t, n, body)
		dirs = append(dirs, d)
	}
	onlyPath(t, dirs...)
	start := time.Now()
	d := Detect(home, 200*time.Millisecond)
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("took %v", el)
	}
	if len(d.GHAccounts)+len(d.AzureSubs)+len(d.GCPProjects) != 0 {
		t.Errorf("want empty, got %#v", d)
	}
}
