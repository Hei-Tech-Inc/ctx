package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/setup"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectMatchesBash(t *testing.T) {
	home := t.TempDir()
	for _, n := range []string{"id_ed25519", "id_work", "zz"} {
		writeFile(t, filepath.Join(home, ".ssh", n), "k")
		writeFile(t, filepath.Join(home, ".ssh", n+".pub"), "k")
	}
	writeFile(t, filepath.Join(home, ".ssh", "orphan.pub"), "k")
	writeFile(t, filepath.Join(home, ".ssh", "known_hosts"), "k")
	writeFile(t, filepath.Join(home, ".ssh", "config"), "k")
	writeFile(t, filepath.Join(home, ".aws", "credentials"), "[zeta]\nk=v\n[default]\n[Upper]\n[default]\n")
	writeFile(t, filepath.Join(home, ".aws", "config"), "[default]\n[profile beta]\n[profile zeta]\n[sso-session s]\n")

	var dirs []string
	fake := func(name, body string) {
		d, _ := testutil.FakeBin(t, name, body)
		dirs = append(dirs, d)
	}
	fake("gh", `echo github.com
echo "  ✓ Logged in to github.com account alice (keyring)"
echo "  X Failed to log in to github.com account broken (keyring)" >&2
echo "  ✓ Logged in to github.com as bob (keyring)"`)
	fake("kubectl", `printf 'ctx-b\nctx-a\n'`)
	fake("az", `printf 'Sub One\nSub Two\n'`)
	fake("gcloud", `printf 'proj-1\n'`)
	// Bash's run_with_timeout polls with `sleep 1` when there is no timeout(1)
	// (macOS); this one just runs the command.
	fake("timeout", `shift; exec "$@"`)
	path := strings.Join(append(dirs, testutil.SystemPath(t)), ":")
	t.Setenv("PATH", path)
	// The production sort is whatever locale the user has; the test pins the
	// byte order (C locale) on both sides.
	t.Setenv("LC_ALL", "C")
	t.Setenv("HOME", home)

	got := setup.Detect(home, 8*time.Second)
	var goOut bytes.Buffer
	goOut.WriteString("[summary]\n")
	got.Report(ui{out: &goOut, err: &goOut}.setupOut())

	script := `set -uo pipefail
source "$1/lib/core.sh"
for f in detect_ssh_keys detect_gh_accounts detect_aws_profiles detect_kube_contexts detect_azure_subs detect_gcp_projects; do
  echo "[$f]"; $f
done`
	cmd := exec.Command(bashOld(), "-c", script, "_", repoRoot)
	bashLists, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var goLists strings.Builder
	for _, s := range []struct {
		name string
		v    []string
	}{
		{"detect_ssh_keys", got.SSHKeys}, {"detect_gh_accounts", got.GHAccounts},
		{"detect_aws_profiles", got.AWSProfiles}, {"detect_kube_contexts", got.KubeContexts},
		{"detect_azure_subs", got.AzureSubs}, {"detect_gcp_projects", got.GCPProjects},
	} {
		goLists.WriteString("[" + s.name + "]\n")
		for _, l := range s.v {
			goLists.WriteString(l + "\n")
		}
	}
	if goLists.String() != string(bashLists) {
		t.Errorf("lists differ\n--- go\n%s--- bash\n%s", goLists.String(), bashLists)
	}
	if got.GHAccounts == nil || len(got.GHAccounts) != 2 {
		t.Errorf("fixture should find gh accounts: %v", got.GHAccounts)
	}

	// The real scan block: the interactive wizard up to its first prompt (EOF
	// cancels). The Found/No lines sit between the blank lines after the
	// "Detecting" line.
	scan := exec.Command(bashOld(), filepath.Join(repoRoot, "bin", "ctx"), "--no-gum", "--dry-run", "setup")
	scan.Env = append(os.Environ(), "CTX_CORE=off", "HOME="+home, "CTX_DIR="+filepath.Join(home, ".ctx"))
	scan.Stdin = strings.NewReader("")
	so, _ := scan.Output()
	const marker = "  Detecting SSH keys / GitHub accounts / cloud profiles...\n\n"
	i := strings.Index(string(so), marker)
	if i < 0 {
		t.Fatalf("scan block not found in %q", so)
	}
	rest := string(so)[i+len(marker):]
	j := strings.Index(rest, "\n\n")
	if j < 0 {
		t.Fatalf("scan end not found in %q", rest)
	}
	block := rest[:j+1]
	if want := "[summary]\n" + block; goOut.String() != want {
		t.Errorf("report differs\n--- go\n%s--- bash\n%s", goOut.String(), want)
	}

	// Intro lines, plain (no gum).
	var intro bytes.Buffer
	setup.ScanIntro(ui{out: &intro, err: &intro}.setupOut(), nil)
	wantIntro := "→ Scanning local accounts/keys/profiles (this can take a few seconds)...\n" +
		"  Looking for SSH keys, gh auth, and cloud profile references...\n" +
		"  Detecting SSH keys / GitHub accounts / cloud profiles...\n"
	if !strings.Contains(string(so), wantIntro) || intro.String() != wantIntro {
		t.Errorf("intro differs: %q", intro.String())
	}
}
