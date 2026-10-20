package setup

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// termOut is a lineOut that also captures the streams git writes to.
type termOut struct {
	lineOut
	out, err bytes.Buffer
}

func (r *termOut) Colors() bool                        { return false }
func (r *termOut) Streams() (stdout, stderr io.Writer) { return &r.out, &r.err }

func TestMiseTOMLMatchesFixture(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "test", "fixtures", "mise_generated_minimal.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := MiseTOML("snap", "Test User", "test@example.com", "", "", "", "", "", ""); got != string(want) {
		t.Errorf("MiseTOML differs from the fixture:\n%s", got)
	}
}

// miseCases are generate_mise_toml argument lists (profile git_name git_email
// aws azure gcp kube secret_keys extra_envs).
var miseCases = [][9]string{
	{"snap", "Test User", "test@example.com", "", "", "", "", "", ""},
	{"acme", `Dev "Q" \ $HOME ` + "`x`", "dev@acme.com", "aws-p", "Sub \"One\"", "proj-1", "ctx-é", "GH_TOKEN NPM_TOKEN bad-key", "FOO=a\"b\\c\nBAR=x=y\nbad-key=1\nNOEQ\n\nTAB=a\tb\n"},
	{"x", "Name", "jdoe", "", "", "", "", "", ""},
	{"y", "N", "bad\ntouch PWNED\r\ttouch PWNED2", "", "", "", "", "", ""},
	{"z", "", "", "", "", "", "kube\x7f", "  A  ", ""},
}

// TestMiseTOMLMatchesBash compares MiseTOML with lib/core.sh generate_mise_toml.
func TestMiseTOMLMatchesBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	td := t.TempDir()
	script := `set -uo pipefail
source "$1/lib/core.sh"
shift
i=0
while [[ $# -gt 0 ]]; do
  generate_mise_toml "$1" "$TD/w$i" "$2" "$3" "$4" "$5" "$6" "$7" "$8" "$9"
  shift 9
  i=$((i + 1))
done`
	args := []string{"-c", script, "_", repoRoot(t)}
	for _, c := range miseCases {
		args = append(args, c[:]...)
	}
	cmd := exec.Command(bash, args...)
	cmd.Env = append(os.Environ(), "TD="+td, "PATH=/usr/bin:/bin", "CTX_DIR="+filepath.Join(td, "ctx"), "LC_ALL=en_US.UTF-8")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}
	for i, c := range miseCases {
		want, err := os.ReadFile(filepath.Join(td, "w"+string(rune('0'+i)), "mise.toml"))
		if err != nil {
			t.Fatal(err)
		}
		if got := MiseTOML(c[0], c[1], c[2], c[3], c[4], c[5], c[6], c[7], c[8]); got != string(want) {
			t.Errorf("case %d differs\n--- go\n%s\n--- bash\n%s", i, got, want)
		}
	}
}

// writeHome points HOME and CTX_DIR at a new temp dir with no mise on PATH.
func writeHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", filepath.Join(home, ".ctx"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("CTX_SECRET_PROVIDER", "file")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	onlyPath(t, filepath.Dir(git))
	return home
}

// TestWriteRoundTrip: what Write puts in the .conf reads back through
// profile.Load, and mise.toml parses as TOML with the values intact.
func TestWriteRoundTrip(t *testing.T) {
	home := writeHome(t)
	key := filepath.Join(home, ".ssh", "id_acme")
	write(t, key, "k")
	in := Inputs{
		Scope: "full", Profile: "acme", GitName: `~Jane #1, "Q" O'Neil \ $HOME ` + "`x` é", GitEmail: "jane@acme.com",
		WorkDir: filepath.Join(home, "w", "a b,c"), ImportExisting: true, SSHKeyPath: key, GitHubUser: "jane-gh",
		AWSProfile: "aws\tp", AzureSub: `sub "1"`, AzureTenant: "ten", GCPProject: "proj$1", GCPAccount: "g@x.io",
		KubeContext: "ctx-é", SecretKeys: []string{"GH_TOKEN", "NPM_TOKEN"},
		ExtraEnvs:     []EnvPair{{"FOO", `a"b\c`}, {"BAR", "x=y"}},
		SigningFormat: "ssh",
	}
	var u termOut
	if err := Write(in, &u); err != nil {
		t.Fatal(err)
	}
	p, err := profile.Load(filepath.Join(home, ".ctx", "profiles"), "acme")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{p.ProfileName, p.GitName, p.GitEmail, p.WorkDir, p.GithubUser, p.SSHKeyPath, p.AWSProfile,
		p.AzureSubscription, p.AzureTenant, p.GCPProject, p.GCPAccount, p.KubeContext, p.SecretKeysRaw, p.ExtraEnvsRaw,
		p.SigningFormat, p.SigningKey}
	want := []string{"acme", in.GitName, in.GitEmail, in.WorkDir, in.GitHubUser, key, in.AWSProfile, in.AzureSub,
		in.AzureTenant, in.GCPProject, in.GCPAccount, in.KubeContext, "GH_TOKEN NPM_TOKEN", "FOO=a\"b\\c\nBAR=x=y\n",
		"ssh", key}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("profile round trip\n got %q\nwant %q", got, want)
	}
	if st, err := os.Stat(filepath.Join(home, ".ctx", "profiles", "acme.conf")); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("profile mode: %v %v", st, err)
	}

	var doc struct {
		Env   map[string]string `toml:"env"`
		Hooks struct {
			Enter struct{ Shell, Script string } `toml:"enter"`
			Leave struct{ Shell, Script string } `toml:"leave"`
		} `toml:"hooks"`
	}
	if _, err := toml.DecodeFile(filepath.Join(in.WorkDir, "mise.toml"), &doc); err != nil {
		t.Fatalf("mise.toml does not parse: %v", err)
	}
	wantEnv := map[string]string{"AWS_PROFILE": in.AWSProfile, "AZURE_SUBSCRIPTION": in.AzureSub,
		"GCLOUD_PROJECT": in.GCPProject, "CTX_KUBE_CONTEXT": in.KubeContext, "FOO": `a"b\c`, "BAR": "x=y"}
	if !reflect.DeepEqual(doc.Env, wantEnv) {
		t.Errorf("env = %q, want %q", doc.Env, wantEnv)
	}
	// The hook line runs the name through bash unchanged.
	line := ""
	for _, l := range strings.Split(doc.Hooks.Enter.Script, "\n") {
		if strings.Contains(l, "git config user.name") {
			line = l
		}
	}
	echo := strings.Replace(line, "git config user.name ", "printf %s", 1)
	if out, err := exec.Command("bash", "-c", echo).Output(); err != nil || string(out) != in.GitName {
		t.Errorf("hook name = %q (%v), want %q", out, err, in.GitName)
	}
	for _, k := range in.SecretKeys {
		if !strings.Contains(doc.Hooks.Leave.Script, "unset "+k+" ") {
			t.Errorf("leave hook lacks %s", k)
		}
	}
	// The identity file holds the name, email and SSH signing.
	id := filepath.Join(home, ".config", "git", "ctx-acme")
	for k, v := range map[string]string{"user.name": in.GitName, "user.email": in.GitEmail, "gpg.format": "ssh",
		"user.signingkey": key, "commit.gpgsign": "true", "tag.gpgsign": "true"} {
		out, _ := exec.Command("git", "config", "--file", id, k).Output()
		if strings.TrimSuffix(string(out), "\n") != v {
			t.Errorf("identity %s = %q, want %q", k, out, v)
		}
	}
}

// TestWriteStoresPendingSecrets (R4): queued secrets are stored first, each
// followed by its line; only stored keys reach SECRET_KEYS; values are never printed.
func TestWriteStoresPendingSecrets(t *testing.T) {
	home := writeHome(t)
	// B cannot be stored: its path is a directory.
	if err := os.MkdirAll(filepath.Join(home, ".ctx", "secrets", "acme", "B_KEY"), 0o700); err != nil {
		t.Fatal(err)
	}
	in := Inputs{Scope: "full", Profile: "acme", GitName: "N", GitEmail: "n@x.io", SecretKeys: []string{"OLD"},
		PendingSecrets: []Secret{{"A_KEY", "value-a"}, {"B_KEY", "value-b"}, {"C_KEY", "value-c"}}}
	var u termOut
	if err := Write(in, &u); err != nil {
		t.Fatal(err)
	}
	head := u.lines[:3]
	wantHead := []string{"success:A_KEY → file store", "warn:Could not store B_KEY (file store)", "success:C_KEY → file store"}
	if !reflect.DeepEqual(head, wantHead) {
		t.Errorf("first lines = %q, want %q", head, wantHead)
	}
	for _, f := range []string{"A_KEY", "C_KEY"} {
		data, err := os.ReadFile(filepath.Join(home, ".ctx", "secrets", "acme", f))
		if err != nil || !strings.HasPrefix(string(data), "value-") {
			t.Errorf("secret %s: %q %v", f, data, err)
		}
	}
	p, err := profile.Load(filepath.Join(home, ".ctx", "profiles"), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if p.SecretKeysRaw != "OLD A_KEY C_KEY" {
		t.Errorf("SECRET_KEYS = %q", p.SecretKeysRaw)
	}
	all := strings.Join(u.lines, "\n") + u.out.String() + u.err.String()
	if strings.Contains(all, "value-") {
		t.Errorf("a secret value was printed: %s", all)
	}
}

func TestDryRunReportAndSummary(t *testing.T) {
	home := writeHome(t)
	key := filepath.Join(home, ".ssh", "ctx_acme")
	in := Inputs{Scope: "full", Profile: "acme", WorkDir: filepath.Join(home, "w"), SSHKeyPath: key, SSHKeyGenerated: true,
		PendingSecrets: []Secret{{"GH_TOKEN", "v"}}}
	var u lineOut
	DryRunReport(in, &u)
	ctxDir := filepath.Join(home, ".ctx")
	want := []string{
		"dim:  [dry-run] would create " + ctxDir,
		"dim:  [dry-run] would store secret GH_TOKEN",
		"bold:  [DRY RUN] — nothing written\n",
		"say:  Would create:",
		"dim:    " + ctxDir + "/profiles/acme.conf",
		"dim:    " + home + "/.config/git/ctx-acme",
		"dim:    " + home + "/w/mise.toml",
		"dim:    " + home + "/.ssh/ctx_config  (host block: github-acme)",
		"dim:    ~/.gitconfig  (includeIf block)",
		"say:",
	}
	if !reflect.DeepEqual(u.lines, want) {
		t.Errorf("dry run\n got %q\nwant %q", u.lines, want)
	}

	u = lineOut{}
	Summary(Inputs{Scope: "clone", Profile: "c"}, &u, false)
	wantClone := []string{"say:", "success:Clone profile 'c' ready.", "info:Clone:  ctx clone -p c git@github.com:ORG/REPO.git",
		"info:Or:     git clone git@github-c:ORG/REPO.git", "info:Later:  ctx setup --extend c", "info:Verify: ctx verify c", "say:"}
	if !reflect.DeepEqual(u.lines, wantClone) {
		t.Errorf("clone summary\n got %q\nwant %q", u.lines, wantClone)
	}
	u = lineOut{}
	Summary(Inputs{Scope: "clone", Profile: "c"}, &u, true)
	if len(u.lines) != 3 || !strings.Contains(u.lines[1], "╭") || !strings.Contains(u.lines[1], "Clone profile 'c' ready") ||
		!strings.Contains(u.lines[1], "ctx setup --extend c  (git, folder, cloud, secrets)") {
		t.Errorf("rich clone summary: %q", u.lines)
	}
	u = lineOut{}
	Summary(Inputs{Scope: "full", Profile: "e", Extend: true}, &u, false)
	wantExt := []string{"say:", "success:Profile 'e' updated.", "info:Check:  ctx status", "info:Verify: ctx verify e", "say:"}
	if !reflect.DeepEqual(u.lines, wantExt) {
		t.Errorf("extend summary\n got %q\nwant %q", u.lines, wantExt)
	}
}

// TestWriteReportsFailedSteps: a step that cannot write prints its error
// instead of its success line, the rest still runs, and Write fails.
func TestWriteReportsFailedSteps(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needs a non-root user")
	}
	home := writeHome(t)
	write(t, filepath.Join(home, ".gitconfig"), "[user]\n")
	if err := os.Chmod(filepath.Join(home, ".gitconfig"), 0o444); err != nil {
		t.Fatal(err)
	}
	in := Inputs{Scope: "full", Profile: "acme", GitName: "N", GitEmail: "n@x.io", WorkDir: filepath.Join(home, "w")}
	var u termOut
	if err := Write(in, &u); err != ErrWriteFailed {
		t.Fatalf("err = %v", err)
	}
	all := strings.Join(u.lines, "\n")
	if !strings.Contains(all, "error:Could not write "+home+"/.gitconfig: permission denied") ||
		strings.Contains(all, "includeIf added") || !strings.Contains(all, "mise.toml generated") {
		t.Errorf("lines:\n%s", all)
	}
}
