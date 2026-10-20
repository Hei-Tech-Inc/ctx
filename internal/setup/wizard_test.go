package setup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// testWizard is a plain, colorless Wizard reading answers (one per line) and
// recording the tools it runs. A run of ssh-keygen writes the key files.
type testWizard struct {
	*Wizard
	out      *lineOut
	raw, err *bytes.Buffer
	ran      [][]string
}

func newTestWizard(t *testing.T, home, answers string) *testWizard {
	t.Helper()
	tw := &testWizard{out: &lineOut{}, raw: &bytes.Buffer{}, err: &bytes.Buffer{}}
	term := tui.Term{In: strings.NewReader(answers), Out: tw.raw, Err: tw.err}
	run := func(quiet bool) Runner {
		return func(name string, args ...string) error {
			rec := append([]string{name}, args...)
			if quiet {
				rec = append(rec, "2>/dev/null")
			}
			tw.ran = append(tw.ran, rec)
			if name == "ssh-keygen" {
				f := args[5]
				write(t, f, "PRIVATE")
				write(t, f+".pub", "ssh-ed25519 AAAA "+f+"\n")
			}
			if name == "gh" && len(args) > 1 && args[1] == "add" && strings.Contains(args[2], "fail") {
				return errors.New("exit status 1")
			}
			return nil
		}
	}
	tw.Wizard = &Wizard{
		T: term, P: term.Prompter(), U: tw.out, Home: home, PWD: home + "/cur",
		Run: run(false), RunQuiet: run(true),
		Hostname: func() string { return "box" },
		Now:      func() time.Time { return time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC) },
		HasTool:  func(string) bool { return true },
	}
	return tw
}

func TestGenKey(t *testing.T) {
	home := t.TempDir()
	key := home + "/.ssh/ctx_acme"

	// Dry run: nothing asked, nothing run.
	tw := newTestWizard(t, home, "")
	tw.DryRun = true
	if err := tw.GenKey(key, "ctx-acme@local"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"dim:  [dry-run] would generate SSH key " + key}; !reflect.DeepEqual(tw.out.lines, want) || tw.ran != nil {
		t.Errorf("dry run: %q ran %q", tw.out.lines, tw.ran)
	}

	// Passphrase, then Enter at the GitHub pause (echoed like Bash's bare ask).
	tw = newTestWizard(t, home, "y\n s3cret \n\n")
	if err := tw.GenKey(key, "ctx-acme@local"); err != nil {
		t.Fatal(err)
	}
	wantRan := [][]string{{"ssh-keygen", "-t", "ed25519", "-C", "ctx-acme@local", "-f", key, "-N", "s3cret", "-q"}}
	if !reflect.DeepEqual(tw.ran, wantRan) {
		t.Errorf("ran %q", tw.ran)
	}
	wantLines := []string{
		"success:SSH key generated: " + key, "say:", "bold:  Add this public key to GitHub:", "say:",
		"say:ssh-ed25519 AAAA " + key, "say:", "info:Open: https://github.com/settings/ssh/new", "say:",
	}
	if !reflect.DeepEqual(tw.out.lines, wantLines) {
		t.Errorf("lines %q", tw.out.lines)
	}
	wantErr := "? Protect this SSH key with a passphrase? [Y/n]: ? Passphrase (leave blank to use none) (hidden): \n" +
		"? Press Enter after adding the key to GitHub...: "
	if tw.err.String() != wantErr {
		t.Errorf("prompts %q", tw.err.String())
	}
	if st, err := os.Stat(key); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("key mode: %v %v", st, err)
	}

	// The key exists now: warn and skip.
	tw = newTestWizard(t, home, "")
	if err := tw.GenKey(key, "c"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"warn:Key exists at " + key + " — skipping generation."}; !reflect.DeepEqual(tw.out.lines, want) || tw.ran != nil {
		t.Errorf("exists: %q ran %q", tw.out.lines, tw.ran)
	}

	// EOF at the passphrase question cancels.
	tw = newTestWizard(t, home, "")
	if err := tw.GenKey(home+"/.ssh/other", "c"); !errors.Is(err, tui.ErrCancelled) {
		t.Errorf("EOF: %v", err)
	}
}

func TestUploadKey(t *testing.T) {
	tw := newTestWizard(t, t.TempDir(), "")
	if tw.UploadKey("alice", "/k.pub", "acme") != nil || tw.UploadKey("bob", "/fail.pub", "acme") != nil {
		t.Error("UploadKey returned an error")
	}
	want := [][]string{
		{"gh", "auth", "switch", "-u", "alice", "2>/dev/null"},
		{"gh", "ssh-key", "add", "/k.pub", "--title", "ctx-acme-box-20261020", "2>/dev/null"},
		{"gh", "auth", "switch", "-u", "bob", "2>/dev/null"},
		{"gh", "ssh-key", "add", "/fail.pub", "--title", "ctx-acme-box-20261020", "2>/dev/null"},
	}
	if !reflect.DeepEqual(tw.ran, want) {
		t.Errorf("ran %q", tw.ran)
	}
	wantLines := []string{
		"success:SSH key uploaded to GitHub (alice)",
		"warn:Could not upload key automatically. Add it at https://github.com/settings/keys",
	}
	if !reflect.DeepEqual(tw.out.lines, wantLines) {
		t.Errorf("lines %q", tw.out.lines)
	}
}

func TestReview(t *testing.T) {
	in := &Inputs{Profile: "acme", SSHKeyPath: "/k", GitHubUser: "alice"}
	tw := newTestWizard(t, t.TempDir(), "n\n")
	err := tw.Review(in)
	var de *DieError
	if !errors.As(err, &de) || de.Msg != "Aborted before write." || de.Hint {
		t.Fatalf("review no: %v", err)
	}
	wantLines := []string{
		"dim:" + strings.Repeat("─", 40), "bold:  Step 8 / 8 — Review",
		"dim:  Nothing is written until you confirm. No = exit without changes.", "say:",
		"say:  Profile          acme\n" +
			"  Git name         \n" +
			"  Git email        —\n" +
			"  Work directory   \n" +
			"  Config import    Manual entry only\n" +
			"  SSH key          /k\n" +
			"  GitHub user      alice\n" +
			"  AWS profile      —\n" +
			"  Azure sub        —\n" +
			"  Azure tenant     —\n" +
			"  GCP project      —\n" +
			"  GCP account      —\n" +
			"  kubectl          —",
		"say:",
	}
	if !reflect.DeepEqual(tw.out.lines, wantLines) {
		t.Errorf("lines:\n%s", strings.Join(tw.out.lines, "\n"))
	}
	if tw.err.String() != "? Write this profile to disk now? [Y/n]: " {
		t.Errorf("prompt %q", tw.err.String())
	}

	tw = newTestWizard(t, t.TempDir(), "\n")
	if err := tw.Review(&Inputs{Profile: "acme"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(tw.out.lines, "\n"), "  SSH key          (none)\n") {
		t.Errorf("no key row: %q", tw.out.lines)
	}

	tw = newTestWizard(t, t.TempDir(), "")
	if err := tw.Review(&Inputs{Profile: "acme", AutoYes: true}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"info:Confirming automatically (--yes or AUTO_CONFIRM in config)."}; !reflect.DeepEqual(tw.out.lines, want) {
		t.Errorf("auto: %q", tw.out.lines)
	}
}

func TestCloneWizard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", filepath.Join(home, ".ctx"))
	t.Setenv("CTX_WORK_ROOT", "")
	write(t, home+"/.ssh/id_work", "PRIVATE")

	tw := newTestWizard(t, home, "Acme Corp\n1\n~/.ssh/id_work\nbob\n3\nteam/x\n")
	var in Inputs
	if err := tw.Clone(&in); err != nil {
		t.Fatal(err)
	}
	want := Inputs{Scope: "clone", Profile: "acme-corp", SSHKeyPath: home + "/.ssh/id_work", GitHubUser: "bob", WorkDir: home + "/clients/team-x"}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("inputs %+v", in)
	}

	// Generate, upload; the key mode pick's 0 (skip) generates too.
	tw = newTestWizard(t, home, "acme\n0\nn\n\nalice\n1\ny\n")
	in = Inputs{}
	if err := tw.Clone(&in); err != nil {
		t.Fatal(err)
	}
	key := home + "/.ssh/ctx_acme"
	if in.SSHKeyPath != key || !in.SSHKeyGenerated || in.WorkDir != "" {
		t.Errorf("inputs %+v", in)
	}
	if len(tw.ran) != 3 || tw.ran[0][0] != "ssh-keygen" || tw.ran[0][4] != "ctx-acme@local" || tw.ran[2][3] != key+".pub" {
		t.Errorf("ran %q", tw.ran)
	}

	// A missing existing key dies with the doctor hint.
	tw = newTestWizard(t, home, "acme\n1\n/nope\n")
	err := tw.Clone(&Inputs{})
	var de *DieError
	if !errors.As(err, &de) || de.Msg != "SSH key not found: /nope" || !de.Hint {
		t.Errorf("missing key: %v", err)
	}

	// An empty profile name dies; EOF cancels.
	tw = newTestWizard(t, home, "\n")
	if err := tw.Clone(&Inputs{}); err == nil || err.Error() != "Profile name required." {
		t.Errorf("empty name: %v", err)
	}
	tw = newTestWizard(t, home, "")
	if err := tw.Clone(&Inputs{}); !errors.Is(err, tui.ErrCancelled) {
		t.Errorf("EOF: %v", err)
	}
}
