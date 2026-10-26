package setup

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// fullWizard is a testWizard with a fixed scan and the captured-output tools
// Full reads (git config --global, ssh-keygen -lf).
func fullWizard(t *testing.T, home, answers string) *testWizard {
	t.Helper()
	tw := newTestWizard(t, home, answers)
	tw.Detect = func() Detected {
		return Detected{
			SSHKeys: []string{home + "/.ssh/id_a"}, GHAccounts: []string{"alice"},
			AWSProfiles: []string{"dev", "prod"}, KubeContexts: []string{"k1"},
			AzureSubs: []string{"sub1"}, GCPProjects: []string{"p1"},
		}
	}
	tw.Output = func(name string, args ...string) string {
		tw.ran = append(tw.ran, append([]string{"$(" + name}, args...))
		switch strings.Join(append([]string{name}, args...), " ") {
		case "git config --global user.name":
			return "Me"
		case "ssh-keygen -lf " + home + "/.ssh/id_a.pub":
			return "256 SHA256:abc ctx (ED25519)"
		}
		return ""
	}
	return tw
}

func TestFullWizardInputs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", home+"/.ctx")
	t.Setenv("CTX_WORK_ROOT", "")
	t.Setenv("CTX_SECRET_PROVIDER", "file")

	answers := []string{
		"2",               // mode: full
		"Acme", "", "bad", // name, git name (default Me), a bad email
		"dev@acme.com", "2", // a good email, SSH signing
		"3", "team/x", // folder under the work root
		"1", "2", "n", "2", // import, key [1] id_a, no gh login, alice
		"1", "typed-aws", // AWS: None, then typed
		"2", "t1", // Azure sub1 + tenant
		"1",                          // GCP: None (no typed ask)
		"2",                          // kube k1
		"2", "y", "s3cr3t", "n", "n", // secrets: GH_TOKEN only of the three
		"y", "BAD-KEY", "y", "K2", "v2", "y", "", // custom keys
		"y", "NODE_ENV", "dev", "", // env vars
	}
	tw := fullWizard(t, home, strings.Join(answers, "\n")+"\n")
	in := Inputs{Scope: "full"}
	if err := tw.Full(&in); err != nil {
		t.Fatalf("Full: %v\n%s", err, strings.Join(tw.out.lines, "\n"))
	}
	want := Inputs{
		Scope: "full", Profile: "acme", GitName: "Me", GitEmail: "dev@acme.com",
		WorkDir: home + "/clients/team-x", ImportExisting: true, SSHKeyPath: home + "/.ssh/id_a",
		GitHubUser: "alice", AWSProfile: "typed-aws", AzureSub: "sub1", AzureTenant: "t1", KubeContext: "k1",
		PendingSecrets: []Secret{{"GH_TOKEN", "s3cr3t"}, {"K2", "v2"}},
		ExtraEnvs:      []EnvPair{{"NODE_ENV", "dev"}},
		SigningFormat:  "ssh",
	}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("inputs\n got %+v\nwant %+v", in, want)
	}
	all := strings.Join(tw.out.lines, "\n")
	for _, m := range []string{
		"warn:That doesn't look like an email (expected user@domain.tld). You entered: 'bad'",
		"success:Using git email: dev@acme.com",
		"success:Using client folder: " + home + "/clients/team-x",
		"success:Using existing key: id_a",
		"success:GitHub account: alice",
		"success:kubectl context: k1",
		"warn:Invalid key 'BAD-KEY'. Use [A-Za-z_][A-Za-z0-9_]*.",
		"bold:  Storage: file store",
	} {
		if !strings.Contains(all, m) {
			t.Errorf("output lacks %q", m)
		}
	}
	if strings.Contains(all+tw.err.String(), "s3cr3t") {
		t.Errorf("secret value printed")
	}
	if !strings.Contains(tw.err.String(), "[1] id_a  (SHA256:abc)") {
		t.Errorf("key picker: %q", tw.err.String())
	}
	wantRan := [][]string{
		{"$(git", "config", "--global", "user.name"},
		{"$(git", "config", "--global", "user.email"},
		{"$(ssh-keygen", "-lf", home + "/.ssh/id_a.pub"},
	}
	if !reflect.DeepEqual(tw.ran, wantRan) {
		t.Errorf("ran %q", tw.ran)
	}
}

func TestFullWizardModePickerClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", home+"/.ctx")
	write(t, home+"/.ssh/id_ed25519", "PRIVATE")
	tw := fullWizard(t, home, "1\nacme\n1\n\n\n1\n")
	in := Inputs{}
	if err := tw.Full(&in); err != nil {
		t.Fatal(err)
	}
	if in.Scope != "clone" || in.Profile != "acme" || in.SSHKeyPath != home+"/.ssh/id_ed25519" {
		t.Errorf("inputs %+v", in)
	}
	// --scope clone skips the picker.
	tw = fullWizard(t, home, "acme\n1\n\n\n1\n")
	in = Inputs{Scope: "clone"}
	if err := tw.Full(&in); err != nil || in.Profile != "acme" {
		t.Errorf("--scope clone: %v %+v", err, in)
	}
}

func TestFullWizardGHLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", home+"/.ctx")
	// manual mode, generate, login (accounts re-detected), typed user, upload
	answers := "2\nacme\nDev\n\n1\n1\n2\n2\nn\n\ny\nbob\ny\n\n\n\n\n1\nn\n"
	tw := fullWizard(t, home, answers)
	redetected := false
	tw.DetectGH = func() []string { redetected = true; return []string{"alice", "bob"} }
	in := Inputs{Scope: "full"}
	if err := tw.Full(&in); err != nil {
		t.Fatalf("Full: %v\n%s", err, strings.Join(tw.out.lines, "\n"))
	}
	if !redetected || in.GitHubUser != "bob" || !in.SSHKeyGenerated || in.ImportExisting {
		t.Errorf("inputs %+v redetected=%v", in, redetected)
	}
	var gh [][]string
	for _, r := range tw.ran {
		if r[0] == "gh" {
			gh = append(gh, r)
		}
	}
	want := [][]string{
		{"gh", "auth", "login"},
		{"gh", "auth", "switch", "2>/dev/null"},
		{"gh", "auth", "switch", "-u", "bob", "2>/dev/null"},
		{"gh", "ssh-key", "add", home + "/.ssh/ctx_acme.pub", "--title", "ctx-acme-box-20261020", "2>/dev/null"},
	}
	if !reflect.DeepEqual(gh, want) {
		t.Errorf("gh %q", gh)
	}
}

func TestAskSigning(t *testing.T) {
	for _, c := range []struct {
		name, answers, curFmt, curKey, wantFmt, wantKey, menu string
	}{
		{"none", "1\n", "", "", "", "", "  1  No signing\n  2  SSH signing (uses this profile's SSH key)\n  3  GPG key\n"},
		{"skip is none", "0\n", "ssh", "/k", "", "", ""},
		{"ssh", "2\n", "", "", "ssh", "", ""},
		{"gpg", "3\nABCD1234\n", "", "", "openpgp", "ABCD1234", ""},
		{"gpg empty is none", "3\n\n", "", "", "", "", ""},
		{"keep current", "1\n", "openpgp", "K", "openpgp", "K", "  1  Keep current (openpgp)\n  2  No signing\n"},
		{"ssh replaces a gpg key", "3\n", "openpgp", "K", "ssh", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			tw := newTestWizard(t, t.TempDir(), c.answers)
			in := Inputs{SigningFormat: c.curFmt, SigningKey: c.curKey}
			if err := tw.AskSigning(&in); err != nil {
				t.Fatal(err)
			}
			if in.SigningFormat != c.wantFmt || in.SigningKey != c.wantKey {
				t.Errorf("got %q %q", in.SigningFormat, in.SigningKey)
			}
			if c.menu != "" && !strings.Contains(tw.err.String(), c.menu) {
				t.Errorf("menu %q", tw.err.String())
			}
		})
	}
	tw := newTestWizard(t, t.TempDir(), "")
	if err := tw.AskSigning(&Inputs{}); !errors.Is(err, tui.ErrCancelled) {
		t.Errorf("EOF: %v", err)
	}
}

func TestHelpPlain(t *testing.T) {
	got := Help(tui.Term{})
	if !strings.HasPrefix(got, "ctx setup — reference\n\nCommands:  ctx setup  |  ctx import  |  ctx add\n") ||
		!strings.HasSuffix(got, "  ctx use <name>    ctx verify <name>    ctx doctor\n\n") {
		t.Errorf("help:\n%s", got)
	}
	if c := Help(tui.Term{Colors: true}); !strings.HasPrefix(c, "\033[1mctx setup — reference\033[0m\n") {
		t.Errorf("colored help: %q", c[:40])
	}
	if r := Help(tui.Term{Rich: true}); !strings.Contains(r, "ctx setup — reference") || !strings.Contains(r, "╭") {
		t.Errorf("rich help:\n%s", r)
	}
}
