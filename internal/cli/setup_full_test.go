//go:build !windows

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// runPiped runs cmd with the answers on a stdin pipe (one per line; a
// testutil.EOF answer ends the input there) and returns stdout and stderr,
// labelled, as the transcript.
func runPiped(t *testing.T, cmd *exec.Cmd, answers []string) (string, int) {
	t.Helper()
	var in strings.Builder
	for _, a := range answers {
		if a == testutil.EOF {
			break
		}
		in.WriteString(a + "\n")
	}
	var o, e bytes.Buffer
	cmd.Stdin = strings.NewReader(in.String())
	cmd.Stdout, cmd.Stderr = &o, &e
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return "--- stdout\n" + o.String() + "--- stderr\n" + e.String(), code
}

// runBoth runs c through Bash and through ctx-core (via bin/ctx) and compares
// the transcript, exit code, HOME and tool argv. It returns the Bash run.
func runBoth(t *testing.T, fakes, core string, c cloneCase) cloneRun {
	t.Helper()
	td, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	args := c.args
	if args == nil {
		args = []string{"--scope", "clone"}
	}
	// bin/ctx must hand the Go run to ctx-core, not fall back to Bash.
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
	return want
}

func expect(code int, must ...string) func(t *testing.T, r cloneRun) {
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

func fullCases() []cloneCase {
	full := []string{} // no --scope: the mode picker runs
	cloud := []string{"", "", "", ""}
	tail := func(a ...string) []string { return append(append(a, cloud...), "1", "n", "y") }
	seedProfile := func(t *testing.T, home string) {
		seedFile(t, home, ".ctx/profiles/acme.conf", "PROFILE_NAME=acme\n")
	}
	return []cloneCase{
		{name: "import, existing key, gh account", args: full,
			answers: tail("2", "acme", "", "dev@acme.com", "1", "1", "1", "2", "n", "2"),
			check:   expect(0, "Using existing key: id_ed25519", "GitHub account: alice", "Profile '", "SHA256:fp-id_ed25519.pub")},
		{name: "manual, generate, typed user and cloud, upload", args: full,
			answers: []string{"2", "acme", "Dev", "dev@acme.com", "1", "1", "2", "2", "n", "", "n", "bob", "y",
				"awsp", "sub1", "ten1", "proj1", "acct1", "kctx", "1", "n", "y"},
			check: expect(0, "SSH key generated", "SSH key uploaded to GitHub (bob)", "Manual mode selected")},
		{name: "import, no ~/.ssh, generate", args: full, noSSH: true,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "y", "n", "", "n", "2", "y"),
			check: func(t *testing.T, r cloneRun) {
				expect(0, "No SSH keys found. Generate one", "SSH key generated: ")(t, r)
				checkNewSSHDir(t, r)
			}},
		{name: "manual, existing tilde key", args: full,
			answers: tail("2", "acme", "Dev", "", "1", "1", "2", "1", "~/.ssh/id_work", "n", ""),
			check:   expect(0, "Using existing key: id_work")},
		{name: "manual, skip ssh key", args: full,
			answers: tail("2", "acme", "Dev", "", "1", "1", "2", "3", "n", ""),
			check:   expect(0, "(none)")},
		{name: "manual, key not found", args: full,
			answers: []string{"2", "acme", "Dev", "", "1", "1", "2", "1", "~/.ssh/nope"},
			check:   expect(1, "SSH key not found: ", "ctx doctor")},
		{name: "import, generate, gh login, new account, upload", args: full,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "1", "n", "", "y", "3", "y"),
			check:   expect(0, "gh login ran", "Tip: choose 'Skip'", "GitHub account: bob", "SSH key uploaded to GitHub (bob)")},
		{name: "gh login fails", args: full, env: []string{"FAKE_GH_LOGIN_RC=1"},
			answers: tail("2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "y", "2"),
			check:   expect(0, "gh auth login did not complete; continuing setup.")},
		{name: "gh none then typed user", args: full,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "1", "carol"),
			check:   expect(0, "GitHub user      carol")},
		{name: "no gh accounts, typed user", args: full, env: []string{"FAKE_GH_NOACCT=1"},
			answers: tail("2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "dave"),
			check:   expect(0, "No gh accounts")},
		{name: "cloud pickers: none then typed, pick, none, pick", args: full, env: []string{"FAKE_CLOUD=1"},
			seed: func(t *testing.T, home string) { seedFile(t, home, ".aws/credentials", "[dev]\n[prod]\n") },
			answers: []string{"2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "2",
				"1", "typed-aws", "2", "t1", "1", "3", "1", "n", "y"},
			check: expect(0, "Found 2 AWS profile(s)", "kubectl context: kctx-b", "AWS profile      typed-aws")},
		{name: "cloud pickers: pick, none, pick with account, none", args: full, env: []string{"FAKE_CLOUD=1"},
			seed: func(t *testing.T, home string) { seedFile(t, home, ".aws/config", "[profile dev]\n") },
			answers: []string{"2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "2",
				"2", "1", "2", "me@gcp", "1", "1", "n", "y"},
			check: expect(0, "AWS profile: dev", "GCP account      me@gcp")},
		{name: "secrets and env vars", args: full,
			answers: append(append([]string{"2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "2"}, cloud...),
				"2", "y", "s3cr3t", "n", "y", "dtok", "y", "bad-key", "y", "STRIPE_KEY", "sk", "y", "",
				"y", "1BAD", "NODE_ENV", "development", "", "y"),
			check: expect(0, "GH_TOKEN → file store", "STRIPE_KEY → file store", "Invalid key 'bad-key'", "Invalid key '1BAD'")},
		{name: "secret store fails", args: full,
			seed: func(t *testing.T, home string) { seedFile(t, home, ".ctx/secrets/acme", "x") },
			answers: append(append([]string{"2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "2"}, cloud...),
				"2", "y", "s3cr3t", "n", "n", "n", "n", "y"),
			check: expect(0, "Could not store GH_TOKEN (file store)"),
			// Bash's file store lets mkdir print its own error; Go's store
			// (E4a secret set) reports only the warning.
			normalize: func(s string) string { return mkdirErrRE.ReplaceAllString(s, "") }},
		{name: "signing ssh", args: full,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "2", "1", "1", "2", "n", "2"),
			check:   expect(0, "Commits signed with SSH key")},
		{name: "signing gpg", args: full,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "3", "ABCDEF12", "1", "1", "2", "n", "2"),
			check:   expect(0, "Commits signed with GPG key: ABCDEF12")},
		{name: "signing gpg empty is none", args: full,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "3", "", "1", "1", "2", "n", "2")},
		{name: "review no", args: full,
			answers: append(append([]string{"2", "acme", "", "dev@acme.com", "1", "1", "1", "2", "n", "2"}, cloud...), "1", "n", "n"),
			check:   expect(1, "Aborted before write.")},
		{name: "existing profile, keep", args: full, seed: seedProfile,
			answers: []string{"2", "acme", "n"}, check: expect(1, "Aborted.")},
		{name: "existing profile, overwrite", args: full, seed: seedProfile,
			answers: tail("2", "acme", "y", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "2")},
		{name: "existing path created", args: full,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "1", "2", "~/proj/new", "y", "1", "2", "n", "2"),
			check:   expect(0, "Using path: ")},
		{name: "existing path not created", args: full,
			answers: []string{"2", "acme", "Dev", "dev@acme.com", "1", "2", "~/proj/new", "n"},
			check:   expect(1, "Aborted.")},
		{name: "folder under work root", args: full,
			answers: tail("2", "acme", "Dev", "dev@acme.com", "1", "3", "team/acme x", "1", "2", "n", "2"),
			check:   expect(0, "clients/team-acme-x")},
		{name: "invalid email, then valid", args: full,
			answers: tail("2", "acme", "Dev", "not an email", "dev@acme.com", "1", "1", "1", "2", "n", "2"),
			check:   expect(0, "That doesn't look like an email")},
		{name: "mode picker clone", args: full, answers: []string{"1", "acme", "1", "", "", "1", "y"},
			check: expect(0, "Clone profile 'acme' ready.")},
		{name: "full -y skips the review", args: []string{"-y"},
			answers: append(append([]string{"2", "acme", "", "dev@acme.com", "1", "1", "1", "2", "n", "2"}, cloud...), "1", "n"),
			check:   expect(0, "Confirming automatically")},
		{name: "dry run full", dry: true, args: full,
			answers: append(append([]string{"2", "acme", "Dev", "dev@acme.com", "1", "2", "~/proj/new", "y", "1", "1", "y", "2", "y"}, cloud...),
				"2", "y", "s3cr3t", "n", "n", "n", "n"),
			check: expect(0, "[dry-run] would create directory", "[dry-run] would generate SSH key", "[dry-run] would run gh auth login",
				"[dry-run] would upload SSH key to GitHub (alice)", "[dry-run] would store secret GH_TOKEN", "[DRY RUN]")},
		{name: "ssh-keygen fails (R25)", args: full, env: []string{"FAKE_KEYGEN_RC=1"},
			answers: []string{"2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "1", "n"},
			check:   expect(1, "ssh-keygen failed for ", "ctx doctor")},
		{name: "clone ssh-keygen fails (R25)", env: []string{"FAKE_KEYGEN_RC=1"},
			answers: []string{"acme", "2", "n"}, check: expect(1, "ssh-keygen failed for ")},
		{name: "config without yes reviews on a tty", args: []string{"--config", "{HOME}/b.conf"},
			seed: seedBatchConf, answers: []string{"y"}, check: expect(0, "Step 8 / 8 — Review", "Profile '")},
		{name: "config without yes, review no", args: []string{"--config", "{HOME}/b.conf"},
			seed: seedBatchConf, answers: []string{"n"}, check: expect(1, "Aborted before write.")},
		{name: "unknown option", args: []string{"--bogus"}, check: expect(1, "unknown option '--bogus'")},
	}
}

var mkdirErrRE = regexp.MustCompile(`mkdir: [^\n]*: File exists\n`)

func seedBatchConf(t *testing.T, home string) {
	seedFile(t, home, "b.conf", "PROFILE_NAME=acme\nGIT_NAME=Dev\nGIT_EMAIL=dev@acme.com\nWORK_MODE=current\n"+
		"SSH_MODE=existing\nSSH_KEY_PATH=~/.ssh/id_ed25519\n")
}

// TestFullWizardMatchesBash drives `ctx --no-gum setup` (the mode picker and
// the eight-step wizard) on a pty through Bash (CTX_CORE=off) and through
// ctx-core with the same answers, and compares everything.
func TestFullWizardMatchesBash(t *testing.T) {
	t.Parallel()
	fakes := cloneFakes(t)
	core := buildCore(t)
	for _, c := range fullCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runBoth(t, fakes, core, c)
		})
	}
}

// cancelRun is a full run that touches everything a cancelled run may leave
// behind: a user-confirmed folder (answer 7) and a generated key (answer 10).
var cancelRun = []string{
	"2", "acme", "Dev", "dev@acme.com", "1", "2", "~/proj/new", "y", "1", "1",
	"n", "", "y", "3", "y", "", "", "", "", "2",
	"y", "s3cr3t", "n", "n", "n", "y", "FOO", "bar", "", "y",
}

// TestWizardCancelEachStep sends EOF at every prompt of cancelRun: each run
// prints one cancel line, exits 130, and changes HOME only by the folder the
// user confirmed and the key generated before the cancel. A handful of points
// are compared with Bash too.
func TestWizardCancelEachStep(t *testing.T) {
	t.Parallel()
	fakes := cloneFakes(t)
	core := buildCore(t)
	withBash := map[int]bool{0: true, 7: true, 8: true, 11: true, 13: true, 21: true, 29: true}
	td, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := runClone(t, td, fakes, core, cloneCase{args: []string{}, answers: []string{testutil.EOF}})
	for k := 0; k < len(cancelRun); k++ {
		t.Run("eof at "+strconv.Itoa(k), func(t *testing.T) {
			t.Parallel()
			c := cloneCase{args: []string{}, answers: append(append([]string{}, cancelRun[:k]...), testutil.EOF)}
			var r cloneRun
			if withBash[k] {
				r = runBoth(t, fakes, core, c)
			} else {
				td, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				r = runClone(t, td, fakes, core, c)
			}
			if r.code != 130 || strings.Count(r.transcript, "Cancelled by user.") != 1 {
				t.Errorf("exit %d, transcript:\n%s", r.code, r.transcript)
			}
			allowed := map[string]bool{}
			if k > 7 {
				allowed["proj"], allowed["proj/new"] = true, true
			}
			if k > 10 {
				// GenKey also sets ~/.ssh to 0700.
				allowed[".ssh"], allowed[".ssh/ctx_acme"], allowed[".ssh/ctx_acme.pub"] = true, true, true
			}
			for p := range union(base.tree, r.tree) {
				if base.tree[p] != r.tree[p] && !allowed[p] {
					t.Errorf("%s changed: %q -> %q", p, base.tree[p], r.tree[p])
				}
			}
			for p := range allowed {
				if _, ok := r.tree[p]; !ok {
					t.Errorf("%s missing", p)
				}
			}
		})
	}
}

func union(a, b map[string]string) map[string]bool {
	u := map[string]bool{}
	for k := range a {
		u[k] = true
	}
	for k := range b {
		u[k] = true
	}
	return u
}

// TestWizardPipedStdin: `ctx setup` (no --no-gum) with stdin a pipe uses the
// plain prompts and reads its answers from stdin; EOF exits 130 and writes
// nothing. Go and Bash print the same bytes.
func TestWizardPipedStdin(t *testing.T) {
	t.Parallel()
	fakes := cloneFakes(t)
	core := buildCore(t)
	answers := append(append([]string{"2", "acme", "Dev", "dev@acme.com", "1", "1", "1", "2", "n", "2"}, "", "", "", ""), "1", "n", "y")
	for _, c := range []cloneCase{
		{name: "full run", gum: true, pipe: true, args: []string{}, answers: answers,
			check: expect(0, "Profile '")},
		{name: "eof", gum: true, pipe: true, args: []string{}, answers: append(append([]string{}, answers[:9]...), testutil.EOF),
			check: func(t *testing.T, r cloneRun) {
				expect(130, "Cancelled by user.")(t, r)
				if _, ok := r.tree[".ctx"]; ok {
					t.Error("~/.ctx created")
				}
			}},
		{name: "clone eof at name", gum: true, pipe: true, answers: []string{testutil.EOF}, check: expect(130)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runBoth(t, fakes, core, c)
		})
	}
}

// TestHelpMatchesBash: ctx setup|import|add --help (Go) print the bytes of
// Bash's `ctx help setup` and `ctx setup --help`, plain and colored.
func TestHelpMatchesBash(t *testing.T) {
	t.Parallel()
	core := buildCore(t)
	ctx := filepath.Join(repoRoot, "bin", "ctx")
	run := func(t *testing.T, bashSide bool, args ...string) (string, int) {
		cmd := exec.Command(ctx, args...)
		cmd.Env = append(os.Environ(), "CTX_NO_GUM=", "PATH=/usr/bin:/bin")
		if bashSide {
			cmd.Env = append(cmd.Env, "CTX_CORE=off")
		} else {
			cmd.Env = append(cmd.Env, "CTX_CORE_BIN="+core)
		}
		var o, e bytes.Buffer
		cmd.Stdout, cmd.Stderr = &o, &e
		code := 0
		if err := cmd.Run(); err != nil {
			code = err.(*exec.ExitError).ExitCode()
		}
		return o.String() + "|stderr:" + e.String(), code
	}
	want, code := run(t, true, "--no-gum", "help", "setup")
	if code != 0 || !strings.Contains(want, "ctx setup — reference") {
		t.Fatalf("bash help: %d %q", code, want)
	}
	if b, _ := run(t, true, "--no-gum", "setup", "--help"); b != want {
		t.Fatalf("bash setup --help differs from help setup")
	}
	for _, args := range [][]string{
		{"--no-gum", "setup", "--help"}, {"import", "-h"}, {"--no-gum", "add", "--help"},
		{"setup", "--scope", "clone", "--help"}, {"--dry-run", "setup", "--config", "/nonexistent", "-h"},
	} {
		if out, err := exec.Command(core, append([]string{"implements", "setup"}, args[len(args)-1])...).CombinedOutput(); err != nil {
			t.Fatalf("implements setup %s: %v %s", args[len(args)-1], err, out)
		}
		got, code := run(t, false, args...)
		if got != want || code != 0 {
			t.Errorf("%q: exit %d\n--- go\n%s--- bash\n%s", args, code, got, want)
		}
	}

	// Colors on a pty.
	pty := func(bashSide bool, args ...string) string {
		cmd := exec.Command(ctx, args...)
		cmd.Env = append(os.Environ(), "CTX_NO_GUM=", "TERM=xterm", "PATH=/usr/bin:/bin")
		if bashSide {
			cmd.Env = append(cmd.Env, "CTX_CORE=off")
		} else {
			cmd.Env = append(cmd.Env, "CTX_CORE_BIN="+core)
		}
		out, code := testutil.RunPTY(t, cmd, nil, 30*time.Second)
		if code != 0 {
			t.Errorf("%q exit %d", args, code)
		}
		return out
	}
	wantC := pty(true, "--no-gum", "help", "setup")
	if !strings.Contains(wantC, "\033[1mctx setup — reference\033[0m") {
		t.Fatalf("bash help has no colors: %q", wantC)
	}
	if gotC := pty(false, "--no-gum", "setup", "--help"); gotC != wantC {
		t.Errorf("colored help differs\n--- go\n%q\n--- bash\n%q", gotC, wantC)
	}
}

// TestRichWizardSmoke runs the rich (huh) wizard on a pty, without --no-gum:
// the mode picker's first item (clone), a typed name, the default key, no
// GitHub user, the second folder choice (arrow down), and the review's
// default. It checks the files written, not the screen.
func TestRichWizardSmoke(t *testing.T) {
	t.Parallel()
	fakes := cloneFakes(t)
	core := buildCore(t)
	td, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const down = "\x1b[B"
	start := time.Now()
	r := runClone(t, td, fakes, core, cloneCase{gum: true, args: []string{},
		answers: []string{"", "acme", "", "", "", down, ""}})
	t.Logf("rich run took %v", time.Since(start))
	const home = "<ROOT>" // Snapshot's name for the HOME it walked
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.transcript)
	}
	conf := r.tree[".ctx/profiles/acme.conf"]
	for _, want := range []string{"PROFILE_NAME=acme\n", "SSH_KEY_PATH=" + home + "/.ssh/id_ed25519\n", "WORK_DIR=" + home + "/cur\n"} {
		if !strings.Contains(conf, want) {
			t.Errorf("acme.conf lacks %q:\n%s", want, conf)
		}
	}
	if !strings.HasPrefix(conf, "-rw-------") {
		t.Errorf("acme.conf mode: %q", conf[:10])
	}
	if h := r.tree[".ssh/ctx_config"]; !strings.Contains(h, "Host github-acme\n") || !strings.Contains(h, "IdentityFile "+home+"/.ssh/id_ed25519") {
		t.Errorf("ctx_config:\n%s", h)
	}
	if _, ok := r.tree[".config/git/ctx-acme"]; !ok {
		t.Error("no identity file")
	}
	if g := r.tree[".gitconfig"]; !strings.Contains(g, "gitdir:"+home+"/cur/") {
		t.Errorf(".gitconfig:\n%s", g)
	}
}
