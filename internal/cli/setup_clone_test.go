//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// cloneFakes is a directory of fake tools shared by every clone case. Each
// fake appends its argv to $FAKE_LOGDIR/<name>.argv, so cases running in
// parallel keep separate logs:
//   - ssh-keygen writes a deterministic key pair for -f;
//   - gh: `auth status` reports alice logged in; `auth switch` and
//     `ssh-key add` print to stdout and stderr (Bash drops stderr), and
//     `ssh-key add` exits $FAKE_GH_ADD_RC;
//   - git records and runs the real git; mise prints; timeout runs its command;
//   - hostname (not logged) prints this machine's name, as os.Hostname does;
//   - date prints a fixed backup stamp.
func cloneFakes(t *testing.T) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	realBash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	if v, err := exec.Command(realBash, "-c", `echo "${BASH_VERSINFO[0]}"`).Output(); err != nil || strings.TrimSpace(string(v)) < "4" {
		t.Skipf("bash on PATH is older than 4 (%s)", v)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname")
	}
	dir := t.TempDir()
	logged := func(name, body string) {
		script := "#!/bin/sh\n" +
			"for a in \"$@\"; do printf '%s\\000' \"$a\" >> \"$FAKE_LOGDIR/" + name + ".argv\"; done\n" +
			"printf '\\n' >> \"$FAKE_LOGDIR/" + name + ".argv\"\n" + body + "\n"
		testutil.Script(t, filepath.Join(dir, name), script)
	}
	logged("ssh-keygen", `f=""
while [ $# -gt 0 ]; do
  case "$1" in
    -lf) echo "256 SHA256:fp-$(basename "$2") comment (ED25519)"; exit 0 ;;
    -f) f="$2"; shift ;;
  esac
  shift
done
if [ -n "$FAKE_KEYGEN_RC" ]; then echo "ssh-keygen: fake failure" >&2; exit "$FAKE_KEYGEN_RC"; fi
echo "PRIVATE $f" >"$f"
echo "ssh-ed25519 AAAAfake $f" >"$f.pub"`)
	logged("gh", `case "$1 $2" in
"auth status")
  [ -n "$FAKE_GH_NOACCT" ] && exit 1
  printf 'github.com\n  ✓ Logged in to github.com account alice (keyring)\n'
  [ -f "$FAKE_LOGDIR/gh_login" ] && printf '  ✓ Logged in to github.com account bob (keyring)\n'
  exit 0 ;;
"auth login") echo "gh login ran"; : >"$FAKE_LOGDIR/gh_login"; exit "${FAKE_GH_LOGIN_RC:-0}" ;;
"auth switch") echo "switched to $4"; echo "gh switch noise" >&2 ;;
"ssh-key add") echo "key added"; echo "gh add noise" >&2; exit "${FAKE_GH_ADD_RC:-0}" ;;
esac`)
	// Cloud CLIs list accounts only with FAKE_CLOUD set (not logged).
	for name, list := range map[string]string{"kubectl": "kctx-a\nkctx-b", "az": "Sub One\nSub Two", "gcloud": "proj-1"} {
		testutil.Script(t, filepath.Join(dir, name), "#!/bin/sh\n[ -n \"$FAKE_CLOUD\" ] && printf '"+list+"\\n'\nexit 0\n")
	}
	logged("git", `exec '`+realGit+`' "$@"`)
	logged("mise", `echo "mise output"`)
	testutil.Script(t, filepath.Join(dir, "timeout"), "#!/bin/sh\nshift; exec \"$@\"\n")
	testutil.Script(t, filepath.Join(dir, "hostname"), "#!/bin/sh\necho '"+host+"'\n")
	if err := os.Symlink(realBash, filepath.Join(dir, "bash")); err != nil {
		t.Fatal(err)
	}
	return dir
}

type cloneCase struct {
	name    string
	dry     bool
	gum     bool     // leave out --no-gum
	pipe    bool     // stdin, stdout and stderr are pipes, not a pty
	args    []string // after `setup` ({HOME} is replaced); nil means --scope clone
	answers []string
	env     []string
	noSSH   bool // HOME starts without ~/.ssh
	seed    func(t *testing.T, home string)
	check   func(t *testing.T, r cloneRun)
	// normalize, when set, rewrites both transcripts before they are compared.
	normalize func(string) string
}

type cloneRun struct {
	transcript string
	code       int
	tree       map[string]string
	argv       map[string][][]string
}

func seedFile(t *testing.T, home, rel, body string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runClone recreates HOME under td and runs bin/ctx --no-gum [--dry-run]
// setup on a pty: Bash with CTX_CORE=off, Go through ctx-core.
func runClone(t *testing.T, td, fakes, core string, c cloneCase) cloneRun {
	t.Helper()
	home := filepath.Join(td, "home")
	logs := filepath.Join(td, "logs")
	for _, d := range []string{home, logs} {
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cwd := filepath.Join(home, "cur")
	if !c.noSSH {
		seedFile(t, home, ".ssh/id_ed25519", "PRIVATE")
		seedFile(t, home, ".ssh/id_ed25519.pub", "ssh-ed25519 AAAA me")
		seedFile(t, home, ".ssh/id_work", "PRIVATE")
		seedFile(t, home, ".ssh/config", "Host *\n  AddKeysToAgent yes\n")
	}
	seedFile(t, home, ".gitconfig", "[user]\n\tname = Me\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if c.seed != nil {
		c.seed(t, home)
	}

	args := []string{filepath.Join(repoRoot, "bin", "ctx")}
	if !c.gum {
		args = append(args, "--no-gum")
	}
	if c.dry {
		args = append(args, "--dry-run")
	}
	var setupArgs []string
	for _, a := range c.args {
		setupArgs = append(setupArgs, strings.ReplaceAll(a, "{HOME}", home))
	}
	if c.args == nil {
		setupArgs = []string{"--scope", "clone"}
	}
	cmd := exec.Command(filepath.Join(fakes, "bash"), append(append(args, "setup"), setupArgs...)...)
	cmd.Dir = cwd
	stamp := testutil.FixedStampDate(t, batchStamp)
	cmd.Env = append([]string{
		"HOME=" + home, "CTX_DIR=" + filepath.Join(home, ".ctx"), "PWD=" + cwd, "TERM=xterm",
		"PATH=" + stamp + ":" + fakes + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"LC_ALL=en_US.UTF-8", "GIT_CONFIG_NOSYSTEM=1", "CTX_SECRET_PROVIDER=file", "FAKE_LOGDIR=" + logs,
	}, c.env...)
	if core == "" {
		cmd.Env = append(cmd.Env, "CTX_CORE=off")
	} else {
		cmd.Env = append(cmd.Env, "CTX_CORE_BIN="+core)
	}
	var transcript string
	var code int
	if c.pipe {
		transcript, code = runPiped(t, cmd, c.answers)
	} else {
		transcript, code = testutil.RunPTY(t, cmd, c.answers, 120*time.Second)
	}

	tree := mergeBackupStamps(testutil.Snapshot(t, home))
	for k, v := range tree {
		tree[k] = generatedRE.ReplaceAllString(v, "# Generated: <DATE>")
	}
	argv := map[string][][]string{}
	for _, name := range []string{"ssh-keygen", "gh", "git", "mise"} {
		argv[name] = testutil.ReadArgv(t, filepath.Join(logs, name+".argv"))
	}
	transcript = couldNotRE.ReplaceAllString(transcript, "$1")
	if c.normalize != nil {
		transcript = c.normalize(transcript)
	}
	return cloneRun{transcript, code, tree, argv}
}

var stampKeyRE = regexp.MustCompile(`STAMP\d+`)

// mergeBackupStamps folds every backup stamp into one: Bash's fake date
// freezes the stamp, while ctx-core's real clock can tick between the
// backups of one run. Files keep their content; manifests are concatenated in
// stamp order; stamp references elsewhere (ctx.log) become STAMP.
func mergeBackupStamps(tree map[string]string) map[string]string {
	keys := make([]string, 0, len(tree))
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := map[string]string{}
	for _, k := range keys {
		v := stampKeyRE.ReplaceAllString(tree[k], "STAMP")
		nk := stampKeyRE.ReplaceAllString(k, "STAMP")
		if prev, ok := out[nk]; ok && strings.HasSuffix(nk, "/manifest.txt") {
			v = prev + v[strings.Index(v, " ")+1:]
		}
		out[nk] = v
	}
	return out
}

func cloneCases() []cloneCase {
	eof := testutil.EOF
	exit := func(code int, must ...string) func(t *testing.T, r cloneRun) {
		return func(t *testing.T, r cloneRun) {
			t.Helper()
			if r.code != code {
				t.Errorf("exit %d, want %d", r.code, code)
			}
			for _, m := range must {
				if !strings.Contains(r.transcript, m) {
					t.Errorf("transcript lacks %q", m)
				}
			}
		}
	}
	return []cloneCase{
		{name: "existing default key, no folder", answers: []string{"acme", "1", "", "", "1", "y"},
			check: exit(0, "Using key: id_ed25519", "No WORK_DIR — use: ctx clone -p acme <url>", "Clone profile 'acme' ready.")},
		{name: "existing tilde key, current folder, user", answers: []string{"Acme Corp", "1", "~/.ssh/id_work", "bob", "2", "y"},
			check: exit(0, "Using key: id_work", "Work directory: ", "GitHub user      bob")},
		{name: "existing key missing", answers: []string{"acme", "1", "~/.ssh/nope"},
			check: exit(1, "SSH key not found: ", "ctx doctor")},
		{name: "generate, new folder, upload", answers: []string{"acme", "2", "n", "", "alice", "3", "", "y", "y"},
			check: exit(0, "SSH key generated: ", "ssh-ed25519 AAAAfake", "SSH key uploaded to GitHub (alice)", "switched to alice")},
		{name: "generate with passphrase, slug, no upload", answers: []string{"acme", "2", "y", "s3 cret", "", "alice", "3", "team/acme", "n", "y"},
			check: exit(0, "clients/team-acme")},
		{name: "upload fails", env: []string{"FAKE_GH_ADD_RC=1"}, answers: []string{"acme", "2", "n", "", "alice", "1", "y", "y"},
			check: exit(0, "Could not upload key automatically")},
		{name: "generate with no ~/.ssh", noSSH: true, answers: []string{"acme", "2", "n", "", "alice", "1", "y", "y"},
			check: func(t *testing.T, r cloneRun) {
				exit(0, "SSH key generated: ")(t, r)
				checkNewSSHDir(t, r)
			}},
		{name: "key mode skipped generates", answers: []string{"acme", "0", "n", "", "", "1", "y"},
			check: exit(0, "SSH key generated: ")},
		{name: "key exists already", seed: func(t *testing.T, home string) {
			seedFile(t, home, ".ssh/ctx_acme", "PRIVATE")
			seedFile(t, home, ".ssh/ctx_acme.pub", "ssh-ed25519 AAAA old")
		}, answers: []string{"acme", "2", "alice", "1", "y", "y"},
			check: exit(0, "skipping generation", "SSH key uploaded")},
		{name: "review no", answers: []string{"acme", "1", "", "", "1", "n"},
			check: exit(1, "Aborted before write.")},
		{name: "yes skips review", args: []string{"--scope", "clone", "-y"}, answers: []string{"acme", "1", "", "", "1"},
			check: exit(0, "Confirming automatically")},
		{name: "profile exists, keep", seed: func(t *testing.T, home string) {
			seedFile(t, home, ".ctx/profiles/acme.conf", "PROFILE_NAME=acme\n")
		}, answers: []string{"acme", "n"}, check: exit(1, "Aborted.")},
		{name: "profile exists, overwrite", seed: func(t *testing.T, home string) {
			seedFile(t, home, ".ctx/profiles/acme.conf", "PROFILE_NAME=acme\n")
			seedFile(t, home, ".config/git/ctx-acme", "[user]\n\tname = Old\n")
		}, answers: []string{"acme", "y", "1", "", "", "1", "y"}, check: exit(0, "ready.")},
		{name: "dry run generate upload", dry: true, answers: []string{"acme", "2", "alice", "3", "", "y"},
			check: exit(0, "[dry-run] would generate SSH key", "[dry-run] would upload SSH key to GitHub (alice)", "[DRY RUN]")},
		{name: "dry run existing key", dry: true, answers: []string{"acme", "1", "", "", "2"},
			check: exit(0, "[DRY RUN]")},
		{name: "empty profile name", answers: []string{""}, check: exit(1, "Profile name required.")},
		{name: "cancel at profile name", answers: []string{eof}, check: exit(130, "Cancelled by user.")},
		{name: "cancel at passphrase", answers: []string{"acme", "2", eof}, check: exit(130, "Cancelled by user.")},
		{name: "cancel at review", answers: []string{"acme", "1", "", "", "1", eof}, check: exit(130, "Cancelled by user.")},
	}
}

// TestCloneWizardMatchesBash drives `ctx --no-gum setup --scope clone` on a
// pty with the same answers through Bash (CTX_CORE=off) and through ctx-core,
// and compares the transcript, exit code, tool argv and HOME.
func TestCloneWizardMatchesBash(t *testing.T) {
	fakes := cloneFakes(t)
	core := buildCore(t)
	for _, c := range cloneCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			td, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			// bin/ctx must hand the Go run to ctx-core, not fall back to Bash.
			args := c.args
			if args == nil {
				args = []string{"--scope", "clone"}
			}
			if out, err := exec.Command(core, append([]string{"implements", "setup"}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("ctx-core implements setup %q: %v %s", args, err, out)
			}
			want := runClone(t, td, fakes, "", c)
			got := runClone(t, td, fakes, core, c)
			if got.transcript != want.transcript {
				t.Errorf("transcript differs\n--- go\n%s\n--- bash\n%s", got.transcript, want.transcript)
			}
			if got.code != want.code {
				t.Errorf("exit code: go %d, bash %d", got.code, want.code)
			}
			if d := testutil.DiffSnapshots(want.tree, got.tree); d != "" {
				t.Errorf("HOME differs (first = bash, second = go):\n%s", d)
			}
			if !reflect.DeepEqual(got.argv, want.argv) {
				t.Errorf("tool argv differs\n go:   %q\n bash: %q", got.argv, want.argv)
			}
			if c.check != nil {
				c.check(t, want)
			}
		})
	}
}

// checkNewSSHDir: the wizard created ~/.ssh with mode 0700 and generated
// ~/.ssh/ctx_acme in it.
func checkNewSSHDir(t *testing.T, r cloneRun) {
	t.Helper()
	if d := r.tree[".ssh"]; !strings.HasPrefix(d, "drwx------ ") {
		t.Errorf("~/.ssh: %q, want drwx------", d)
	}
	if _, ok := r.tree[".ssh/ctx_acme"]; !ok {
		t.Errorf("no ~/.ssh/ctx_acme\n%s", r.transcript)
	}
}
