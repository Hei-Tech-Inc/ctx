package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// batchEnv is one fixture HOME plus fakes built once: ssh-keygen (writes
// deterministic key files; fails for paths containing "fail"), mise, gh (not
// logged in), timeout (runs its command; keeps Bash's detection fast) and a
// git wrapper that records argv and runs the real git. date prints a fixed
// backup stamp.
type batchEnv struct {
	p                   *ptest
	td, home, cwd, path string
	bash5               string            // the bash on the caller's PATH
	version             string            // CTX_VERSION from lib/core.sh
	logs                map[string]string // tool -> argv log
}

const batchStamp = "20261020-120000"

// newBatchEnv marks a subtest parallel (see ptest).
func newBatchEnv(t *testing.T) *batchEnv {
	t.Helper()
	p := newPtest(t)
	realGit, err := lookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	realBash, err := lookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	// The fixtures' quoting (é, %q) follows bash 4+; macOS /bin/bash 3.2 differs.
	if v, err := exec.Command(realBash, "-c", `echo "${BASH_VERSINFO[0]}"`).Output(); err != nil || strings.TrimSpace(string(v)) < "4" {
		t.Skipf("bash on PATH is older than 4 (%s)", v)
	}
	td, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &batchEnv{p: p, td: td, home: filepath.Join(td, "home"), logs: map[string]string{}, bash5: realBash, version: ctxVersion(t)}
	e.cwd = filepath.Join(e.home, "cur")
	var dirs []string
	locked(func() { dirs = []string{testutil.FixedStampDate(t, batchStamp)} })
	fake := func(name, body string) {
		d, log := testutil.FakeBin(t, name, body)
		dirs = append(dirs, d)
		e.logs[name] = log
	}
	fake("ssh-keygen", `f=""
while [ $# -gt 0 ]; do
  case "$1" in -f) f="$2"; shift ;; esac
  shift
done
case "$f" in *fail*) echo "ssh-keygen: fake failure" >&2; exit 1 ;; esac
echo "PRIVATE $f" >"$f"
echo "ssh-ed25519 AAAAfake $f" >"$f.pub"`)
	fake("mise", `echo "mise output"; echo "mise err" >&2`)
	fake("gh", `exit 1`)
	fake("git", `exec '`+realGit+`' "$@"`)
	fake("timeout", `shift; exec "$@"`)
	// bin/ctx runs `env bash`: the bash found on the caller's PATH, not /bin/bash.
	if err := os.Symlink(realBash, filepath.Join(dirs[1], "bash")); err != nil {
		t.Fatal(err)
	}
	e.path = strings.Join(append(dirs, "/usr/bin", "/bin", "/usr/sbin", "/sbin"), ":")
	return e
}

// reset recreates HOME: two existing SSH keys, a ~/.ssh/config and a
// ~/.gitconfig, the cwd for WORK_MODE=current, then seed's extras.
func (e *batchEnv) reset(t *testing.T, seed func(e *batchEnv)) {
	t.Helper()
	if err := os.RemoveAll(e.home); err != nil {
		t.Fatal(err)
	}
	for _, l := range e.logs {
		_ = os.Remove(l)
	}
	e.write(t, ".ssh/id_ed25519", "PRIVATE")
	e.write(t, ".ssh/id_ed25519.pub", "ssh-ed25519 AAAA me")
	e.write(t, ".ssh/id_work", "PRIVATE")
	e.write(t, ".ssh/config", "Host *\n  AddKeysToAgent yes\n")
	e.write(t, ".gitconfig", "[user]\n\tname = Me\n")
	if err := os.MkdirAll(e.cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		seed(e)
	}
}

func (e *batchEnv) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(e.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *batchEnv) env(dry bool) map[string]string {
	d := "0"
	if dry {
		d = "1"
	}
	return map[string]string{
		"HOME": e.home, "CTX_DIR": filepath.Join(e.home, ".ctx"), "PATH": e.path, "PWD": e.cwd,
		"CTX_DRY_RUN": d, "LC_ALL": "en_US.UTF-8", "GIT_CONFIG_NOSYSTEM": "1", "CTX_SECRET_PROVIDER": "file",
		"CTX_ACTIVE_PROFILE": "", "CTX_WORK_ROOT": "", "XDG_CONFIG_HOME": "", "NO_COLOR": "",
	}
}

type batchRun struct {
	stdout, stderr string
	code           int
	tree           map[string]string
	argv           map[string][][]string
}

var (
	generatedRE = regexp.MustCompile(`# Generated: [^\n]*`)
	// Go adds the OS reason to a failed write; a shell cannot get it.
	couldNotRE = regexp.MustCompile(`(Could not (?:write|back up) \S+): [^\n]*`)
)

func (e *batchEnv) collect(t *testing.T, stdout, stderr string, code int) batchRun {
	t.Helper()
	tree := testutil.Snapshot(t, e.home)
	for k, v := range tree {
		tree[k] = generatedRE.ReplaceAllString(v, "# Generated: <DATE>")
	}
	argv := map[string][][]string{}
	for name, log := range e.logs {
		if name != "timeout" {
			argv[name] = testutil.ReadArgv(t, log)
		}
	}
	stderr = couldNotRE.ReplaceAllString(stderr, "$1")
	return batchRun{stdout, stderr, code, tree, argv}
}

// bash runs CTX_CORE=off bin/ctx (with the bash on the caller's PATH) --no-gum [--dry-run] setup args... in cwd.
func (e *batchEnv) bash(t *testing.T, dry bool, stdin string, args ...string) batchRun {
	t.Helper()
	full := []string{filepath.Join(repoRoot, "bin", "ctx"), "--no-gum"}
	if dry {
		full = append(full, "--dry-run")
	}
	cmd := exec.Command(e.bash5, append(append(full, "setup"), args...)...)
	cmd.Dir = e.cwd
	env := []string{"CTX_CORE=off", "CTX_VERSION="}
	for k, v := range e.env(dry) {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var o, eb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &eb
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return e.collect(t, o.String(), eb.String(), code)
}

// goRun runs SetupIn in process with the same environment.
func (e *batchEnv) goRun(t *testing.T, dry bool, stdin string, args ...string) batchRun {
	t.Helper()
	for k, v := range e.env(dry) {
		e.p.Setenv(k, v)
	}
	e.p.Setenv("CTX_VERSION", e.version)
	var o, eb bytes.Buffer
	var code int
	e.p.inProc(e.cwd, func() { code = SetupIn(tui.Term{In: strings.NewReader(stdin), Out: &o, Err: &eb}, args) })
	return e.collect(t, o.String(), eb.String(), code)
}

func compareBatch(t *testing.T, want, got batchRun) {
	t.Helper()
	if got.stdout != want.stdout {
		t.Errorf("stdout differs\n--- go\n%s--- bash\n%s", got.stdout, want.stdout)
	}
	if got.stderr != want.stderr {
		t.Errorf("stderr differs\n--- go\n%s--- bash\n%s", got.stderr, want.stderr)
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
}

func readExample(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// seedAcme leaves behind what an earlier `ctx setup` for acme wrote.
func seedAcme(e *batchEnv) {
	w := filepath.Join(e.home, "clients", "acme")
	files := map[string]string{
		".ctx/profiles/acme.conf":  "PROFILE_NAME=acme\nGIT_NAME=Old\n",
		".ctx/config":              "",
		".config/git/ctx-acme":     "[user]\n\tname = Old\n",
		".ssh/ctx_config":          "\nHost github-acme\n  HostName github.com\n  User git\n  IdentityFile " + e.home + "/.ssh/ctx_acme\n  IdentitiesOnly yes\n",
		".ssh/ctx_acme":            "PRIVATE",
		".ssh/ctx_acme.pub":        "ssh-ed25519 AAAA old",
		"clients/acme/mise.toml":   "# mine\n",
		".gitconfig":               "[user]\n\tname = Me\n\n[includeIf \"gitdir:" + w + "/\"]\n  path = " + e.home + "/.config/git/ctx-acme\n",
		".ctx/backups/keep/x":      "",
		".ctx/profiles/other.conf": "PROFILE_NAME=other\n",
		".config/git/ctx-other":    "",
	}
	for rel, body := range files {
		p := filepath.Join(e.home, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o600)
	}
}

// seedUnwritable makes ~/.gitconfig read-only, ~/.ssh/ctx_config a directory
// and ~/ro a read-only folder, so the includeIf, host block and mise.toml
// writes fail.
func seedUnwritable(e *batchEnv) {
	_ = os.Chmod(filepath.Join(e.home, ".gitconfig"), 0o444)
	_ = os.MkdirAll(filepath.Join(e.home, ".ssh", "ctx_config"), 0o755)
	_ = os.MkdirAll(filepath.Join(e.home, "ro"), 0o555)
	_ = os.Chmod(filepath.Join(e.home, "ro"), 0o555)
}

// checkUnwritable: each failed step prints its error instead of its success
// line, the other steps still run, and setup exits 1 without a summary.
func checkUnwritable(t *testing.T, r batchRun) {
	t.Helper()
	if r.code != 1 || strings.Count(r.stderr, "✗ Could not write ") != 3 {
		t.Errorf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, s := range []string{"includeIf added", "→ ~/.ssh/ctx_config", "mise.toml generated", "created."} {
		if strings.Contains(r.stdout, s) {
			t.Errorf("stdout has %q:\n%s", s, r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "Git identity file") {
		t.Errorf("the identity step should still run:\n%s", r.stdout)
	}
}

type batchCase struct {
	name  string
	conf  string // @HOME@ is replaced
	args  []string
	dry   bool
	seed  func(e *batchEnv)
	stdin string
	want  string // a substring Bash must print (stdout+stderr), so a case cannot pass vacuously
	check func(t *testing.T, r batchRun)
}

func batchCases(t *testing.T) []batchCase {
	full := readExample(t, "setup.noninteractive.conf.example")
	clone := readExample(t, "setup.clone-only.conf.example")
	return []batchCase{
		{name: "example full", conf: full, args: []string{"--yes"}, want: "Profile 'acme' created."},
		{name: "example clone", conf: clone, args: nil, want: "Clone profile 'side-client' ready."},
		{name: "example full dry-run", conf: full, args: []string{"--yes"}, dry: true, want: "[DRY RUN]"},
		{name: "example clone dry-run, dirs exist", conf: clone, dry: true, want: "(host block: github-side-client)",
			seed: func(e *batchEnv) { seedAcme(e) }},
		{name: "existing key, ssh signing, cloud, quoting", args: []string{"-y"}, want: "Commits signed with SSH key",
			conf: "PROFILE_NAME=Big Co\nGIT_NAME=\"Jane \\\"Q\\\" O'Neil, Jr. #1 $HOME `x` é\"\nGIT_EMAIL= jane@big.co \n" +
				"WORK_DIR=~/work/big co\nSSH_MODE=existing\nSSH_KEY_PATH=~/.ssh/id_work\nGITHUB_USER=jane\n" +
				"AWS_PROFILE_NAME=aws\tp\nAZURE_SUBSCRIPTION=sub \"1\"\nAZURE_TENANT=ten\nGCP_PROJECT=proj$1\nGCP_ACCOUNT=g@x.io\n" +
				"KUBE_CONTEXT=ctx-é\nSIGNING_FORMAT=SSH\nIMPORT_MODE=manual\n"},
		{name: "ssh skip, gpg signing, WORK_MODE=current", args: []string{"--yes"}, want: "Commits signed with GPG key: ABC123",
			conf: "PROFILE_NAME=gpg\nGIT_NAME=G\nGIT_EMAIL=g@x.io\nWORK_MODE=current\nSSH_MODE=skip\nSIGNING_FORMAT=gpg\nSIGNING_KEY=ABC123\n"},
		{name: "WORK_MODE=path, generate, no email", args: []string{"--yes"}, want: "Skipped git email in identity file",
			conf: "PROFILE_NAME=pth\nGIT_NAME=P\nWORK_MODE=path\nEXISTING_PATH=~/proj/x\n"},
		{name: "WORK_MODE=client with slug, ssh signing without key", args: []string{"--yes"}, want: "Commit signing skipped",
			conf: "PROFILE_NAME=cl\nGIT_NAME=C\nGIT_EMAIL=c@x.io\nWORK_MODE=client\nWORK_SLUG=A/b c\nSSH_MODE=none\nSIGNING_FORMAT=ssh\n"},
		{name: "clone scope with folder and generate", args: []string{"--scope", "full", "--yes"}, want: "Clone profile 'cg' ready.",
			conf: "SETUP_SCOPE=clone\nPROFILE_NAME=cg\nWORK_DIR=@HOME@/cg\n"},
		{name: "OVERWRITE missing", args: []string{"--yes"}, seed: seedAcme, want: "set OVERWRITE=true to replace",
			conf: "PROFILE_NAME=acme\nGIT_NAME=A\n"},
		{name: "OVERWRITE=true rerun keeps blocks", args: []string{"--yes"}, seed: seedAcme, want: "mise.toml kept (already exists)",
			conf: "PROFILE_NAME=acme\nGIT_NAME=A\nGIT_EMAIL=a@acme.com\nOVERWRITE=true\nWORK_SLUG=acme\n"},
		{name: "OVERWRITE=true dry-run", args: []string{"--yes"}, seed: seedAcme, dry: true, want: "Would create:",
			conf: "PROFILE_NAME=acme\nGIT_NAME=A\nOVERWRITE=true\n"},
		{name: "unwritable gitconfig, ctx_config and folder", args: []string{"--yes"}, seed: seedUnwritable,
			want: "Could not write ", check: checkUnwritable, conf: "PROFILE_NAME=ro\nGIT_NAME=R\nGIT_EMAIL=r@x.io\nWORK_DIR=~/ro\nSSH_MODE=existing\nSSH_KEY_PATH=~/.ssh/id_work\n"},
		{name: "ssh-keygen fails", args: []string{"--yes"}, want: "ssh-keygen failed for",
			conf: "PROFILE_NAME=keyfail\nGIT_NAME=K\n"},
		{name: "bad signing leaves nothing", args: []string{"--yes"}, want: "SIGNING_FORMAT must be ssh|gpg|none",
			conf: "PROFILE_NAME=bad\nGIT_NAME=B\nSIGNING_FORMAT=zzz\n"},
		{name: "existing key missing", args: []string{"--yes"}, want: "SSH_KEY_PATH not found",
			conf: "PROFILE_NAME=nk\nGIT_NAME=N\nSSH_MODE=existing\nSSH_KEY_PATH=~/.ssh/nope\n"},
		{name: "needs yes", want: "needs --yes or AUTO_CONFIRM=true", conf: "PROFILE_NAME=ny\nGIT_NAME=N\n"},
		{name: "not a file", args: []string{"--yes"}, want: "not a file", conf: "@DIR@"},
		{name: "unknown option", args: []string{"--bogus"}, want: "unknown option '--bogus'", conf: "@NONE@"},
		{name: "extend with config", args: []string{"--extend", "acme", "--yes"}, want: "--extend cannot be used with --config", conf: "PROFILE_NAME=x\n"},
	}
}

// confArgs writes c.conf and returns the setup arguments.
func (e *batchEnv) confArgs(t *testing.T, c batchCase) []string {
	t.Helper()
	switch c.conf {
	case "@NONE@":
		return c.args
	case "@DIR@":
		return append([]string{"--config", e.td}, c.args...)
	}
	p := filepath.Join(e.td, "setup.conf")
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(c.conf, "@HOME@", e.home)), 0o600); err != nil {
		t.Fatal(err)
	}
	return append([]string{"--config", p}, c.args...)
}

// TestBatchMatchesBash compares `ctx setup --config` in Go with the Bash
// command: output, exit code, every file under HOME (backups included, stamps
// normalized) and the argv of git, ssh-keygen, mise and gh.
func TestBatchMatchesBash(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based cases need a non-root user")
	}
	start := time.Now()
	stamp, _ := time.ParseInLocation("20060102-150405", batchStamp, time.Local)
	old := ctxconfig.Now
	ctxconfig.Now = func() time.Time { return stamp }
	t.Cleanup(func() { ctxconfig.Now = old })
	for _, c := range batchCases(t) {
		t.Run(c.name, func(t *testing.T) {
			e := newBatchEnv(t)
			e.reset(t, c.seed)
			want := e.bash(t, c.dry, c.stdin, e.confArgs(t, c)...)
			if !strings.Contains(want.stdout+want.stderr, c.want) {
				t.Fatalf("bash output lacks %q:\n%s%s", c.want, want.stdout, want.stderr)
			}
			if c.check != nil {
				c.check(t, want)
			}
			e.reset(t, c.seed)
			got := e.goRun(t, c.dry, c.stdin, e.confArgs(t, c)...)
			compareBatch(t, want, got)
		})
	}
	t.Logf("batch differential suite took %v", time.Since(start).Round(time.Millisecond))
}

// TestBatchRerunIdempotent: running the same config twice (OVERWRITE=true)
// leaves one includeIf, one host block, one rewrite pair and the first mise.toml.
func TestBatchRerunIdempotent(t *testing.T) {
	e := newBatchEnv(t)
	e.reset(t, nil)
	conf := "PROFILE_NAME=acme\nGIT_NAME=A\nGIT_EMAIL=a@acme.com\nOVERWRITE=true\nWORK_SLUG=acme\n"
	c := batchCase{conf: conf, args: []string{"--yes"}}
	first := e.goRun(t, false, "", e.confArgs(t, c)...)
	if first.code != 0 {
		t.Fatalf("first run: %d %s", first.code, first.stderr)
	}
	second := e.goRun(t, false, "", e.confArgs(t, c)...)
	if second.code != 0 {
		t.Fatalf("second run: %d %s", second.code, second.stderr)
	}
	for _, s := range []string{"gitconfig includeIf already present", "SSH host 'github-acme' already in ctx_config", "mise.toml kept (already exists)"} {
		if !strings.Contains(second.stdout, s) {
			t.Errorf("second run lacks %q:\n%s", s, second.stdout)
		}
	}
	for k, v := range first.tree {
		if strings.Contains(k, "backups") || strings.HasSuffix(k, "ctx.log") {
			continue
		}
		if second.tree[k] != v {
			t.Errorf("%s changed on rerun:\n%q\n%q", k, v, second.tree[k])
		}
	}
	gc, _ := os.ReadFile(filepath.Join(e.home, ".gitconfig"))
	sc, _ := os.ReadFile(filepath.Join(e.home, ".ssh", "ctx_config"))
	id, _ := os.ReadFile(filepath.Join(e.home, ".config", "git", "ctx-acme"))
	if strings.Count(string(gc), "[includeIf") != 1 || strings.Count(string(sc), "Host github-acme\n") != 1 ||
		strings.Count(string(id), "insteadOf") != 2 {
		t.Errorf("duplicated blocks:\n%s\n%s\n%s", gc, sc, id)
	}
}

// TestBatchNeedsYes: --config without --yes/-y or AUTO_CONFIRM on a non-TTY
// stdin dies like Bash.
func TestBatchNeedsYes(t *testing.T) {
	td := t.TempDir()
	yes := filepath.Join(td, "yes.conf")
	no := filepath.Join(td, "no.conf")
	if err := os.WriteFile(yes, []byte("PROFILE_NAME=a\nAUTO_CONFIRM=On\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(no, []byte("PROFILE_NAME=a\nAUTO_CONFIRM=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newBatchEnv(t)
	e.reset(t, nil)
	r := e.goRun(t, false, "", "--config", no)
	if r.code != 1 || !strings.HasSuffix(r.stdout, "→ Non-interactive setup from: "+no+" (scope: full)\n") ||
		r.stderr != "✗ setup --config on non-interactive stdin needs --yes or AUTO_CONFIRM=true\n" {
		t.Errorf("needs yes: %d %q %q", r.code, r.stdout, r.stderr)
	}
}
