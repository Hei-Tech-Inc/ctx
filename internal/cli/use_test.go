package cli

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// useFakes are the external tools ctx use runs. Each appends its name and argv
// to $CTX_TEST_CALLS (one call per line, fields separated by \x1f), prints to
// stdout and stderr so redirections show up, and fails on $CTX_TEST_<TOOL>_RC.
// mise and direnv also print the environment they were given.
var useFakes = map[string]string{
	"gh":      `echo gh-out; echo gh-err >&2; exit "${CTX_TEST_GH_RC:-0}"`,
	"az":      `echo az-out; echo az-err >&2; exit "${CTX_TEST_AZ_RC:-0}"`,
	"gcloud":  `echo gcloud-out; echo gcloud-err >&2; exit "${CTX_TEST_GCLOUD_RC:-0}"`,
	"kubectl": `echo kubectl-out; echo kubectl-err >&2; exit "${CTX_TEST_KUBE_RC:-0}"`,
	"mise":    `echo "mise-out AWS_PROFILE=${AWS_PROFILE-unset} API_KEY=${API_KEY-unset} GIT_NAME=${GIT_NAME-unset} GCLOUD_PROJECT=${GCLOUD_PROJECT-unset}"; echo mise-err >&2`,
	"direnv":  `echo "direnv-out AWS_PROFILE=${AWS_PROFILE-unset} WORK_DIR=${WORK_DIR-unset}"; echo direnv-err >&2`,
	"ssh-keygen": `[ "$1" = -lf ] || exit 2
echo "256 SHA256:fp-$(basename "$2" .pub) c (ED25519)"; echo keygen-err >&2`,
	"ssh-add": `case "$1" in
-l) if [ -s "$CTX_TEST_AGENT" ]; then cat "$CTX_TEST_AGENT"; exit "${CTX_TEST_SSHADD_L_RC:-0}"; fi
    echo "The agent has no identities."; exit 1 ;;
-d) echo removed; echo removed-err >&2 ;;
*) [ -z "${CTX_TEST_SSHADD_FAIL:-}" ] || exit 1
   echo "256 SHA256:fp-$(basename "$1") c (ED25519)" >>"$CTX_TEST_AGENT"
   echo ssh-add-out; echo "Identity added: $1" >&2 ;;
esac`,
}

// writeUseFakes writes useFakes (all, or only names) into a new dir.
func writeUseFakes(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	if names == nil {
		for n := range useFakes {
			names = append(names, n)
		}
	}
	for _, n := range names {
		body := "#!/bin/sh\n{ printf '%s' " + n + "; for a in \"$@\"; do printf '\\037%s' \"$a\"; done; printf '\\n'; } >>\"$CTX_TEST_CALLS\"\n" + useFakes[n] + "\n"
		testutil.Script(t, filepath.Join(dir, n), body)
	}
	return dir
}

// minimalPath is a dir with only the system tools bin/ctx needs, so gh, az,
// ssh-add and friends are absent.
func minimalPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"bash", "env", "base64", "date", "grep", "cut", "tail", "head", "tr", "awk", "sed",
		"mktemp", "mv", "mkdir", "touch", "chmod", "cat", "uname", "basename", "dirname", "rm", "ls", "cp", "tput", "sort"} {
		p, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type useRun struct {
	stdout, stderr string
	code           int
	calls          []string
	tree           map[string]string
}

var exportNameRE = regexp.MustCompile(`(?m)^(?:export |set -gx )([A-Za-z_][A-Za-z0-9_]*)[= ]`)

// evalBash evals bash export lines and prints NAME=%q for each exported name.
func evalBash(t *testing.T, bash, code string) string {
	t.Helper()
	var names []string
	for _, m := range exportNameRE.FindAllStringSubmatch(code, -1) {
		names = append(names, m[1])
	}
	script := `eval "$1"; shift; for k; do printf '%s=%q\n' "$k" "${!k}"; done`
	out, err := exec.Command(bash, "--norc", "--noprofile", "-c", script, "_", code).CombinedOutput()
	if err != nil {
		t.Fatalf("eval of %q: %v\n%s", code, err, out)
	}
	return strings.Join(names, " ") + "\n" + string(out)
}

func TestUseMatchesBash(t *testing.T) { runUseDiff(t, true) }

// TestUseNonQuietMatchesBash runs the same scenarios without CTX_QUIET, so the
// progress lines, the closing "Active context" block and the "not applied to
// this shell" warning are compared against Bash (plain mode, as without gum).
func TestUseNonQuietMatchesBash(t *testing.T) { runUseDiff(t, false) }

func runUseDiff(t *testing.T, quiet bool) {
	realBash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	const secret = "a b 'q' $x `id` \"dq\" \\\nline2"
	type scenario struct {
		name    string
		args    []string
		auto    bool
		minimal bool              // only system tools on PATH
		only    []string          // fakes to install (nil: all)
		config  string            // initial ~/.ctx/config after secret_provider=file
		agent   string            // initial fake agent listing
		env     map[string]string // extra environment
		conf    func(s string) string
		home    func(f *fixture)
		cwdHome bool // run from $HOME instead of inside WORK_DIR
	}
	evalBashArgs := []string{"use", "demo", "--eval", "bash"}
	scenarios := []scenario{
		{name: "full profile auto switch", args: evalBashArgs, auto: true},
		{name: "manual fish", args: []string{"use", "--eval", "fish", "demo"}},
		{name: "manual gh fails", args: evalBashArgs, env: map[string]string{"CTX_TEST_GH_RC": "1"}},
		{name: "cloud tools fail", args: evalBashArgs, auto: true,
			env: map[string]string{"CTX_TEST_AZ_RC": "3", "CTX_TEST_GCLOUD_RC": "1", "CTX_TEST_KUBE_RC": "1"}},
		{name: "tools absent", args: evalBashArgs, minimal: true},
		{name: "tools absent auto fish", args: []string{"use", "demo", "--eval", "fish"}, minimal: true, auto: true},
		{name: "previous agent key different", args: evalBashArgs, auto: true, config: "ssh_agent_added=/old/id_old\n"},
		{name: "previous agent key same", args: evalBashArgs, auto: true, config: "ssh_agent_added=<HOME>/.ssh/id_demo\n"},
		{name: "key already loaded", args: evalBashArgs, auto: true, agent: "256 SHA256:fp-other c (ED25519)\n256 SHA256:fp-id_demo c (ED25519)\n"},
		{name: "agent listing fails", args: evalBashArgs, auto: true, agent: "256 SHA256:fp-id_demo c (ED25519)\n",
			env: map[string]string{"CTX_TEST_SSHADD_L_RC": "2"}},
		{name: "ssh-add fails", args: evalBashArgs, auto: true, env: map[string]string{"CTX_TEST_SSHADD_FAIL": "1"}},
		{name: "no ssh key drops added", args: evalBashArgs, auto: true, config: "ssh_agent_added=/old/id_old\n",
			conf: func(s string) string { return dropLine(s, "SSH_KEY_PATH") }},
		{name: "ssh key file missing", args: evalBashArgs, auto: true, config: "ssh_agent_added=/old/id_old\n",
			home: func(f *fixture) { _ = os.Remove(filepath.Join(f.home, ".ssh", "id_demo")) }},
		{name: "manual lock kept on re-apply", args: evalBashArgs, auto: true,
			config: "active=demo\nactive_source=Manual\nmanual_pwd=" + base64.StdEncoding.EncodeToString([]byte("/x")) + "\n"},
		{name: "manual lock of other profile dropped", args: evalBashArgs, auto: true,
			config: "active=other\nactive_source=manual\nmanual_pwd=L3g=\nkeep=1\n"},
		{name: "auto already active", args: evalBashArgs, auto: true, config: "active=demo\n"},
		{name: "manual re-apply", args: evalBashArgs, config: "active=demo\nactive_source=manual\nmanual_pwd=L3g=\n"},
		{name: "missing secret", args: evalBashArgs, auto: true,
			home: func(f *fixture) { _ = os.Remove(filepath.Join(f.dir, "secrets", "demo", "DB_PASS")) }},
		{name: "invalid secret key name", args: evalBashArgs, auto: true,
			conf: func(s string) string {
				return dropLine(s, "SECRET_KEYS") + "SECRET_KEYS=\"API_KEY 1bad BAD-NAME\tDB_PASS\"\n"
			}},
		{name: "rewrite already present", args: evalBashArgs, auto: true,
			home: func(f *fixture) {
				f.writeHome(".config/git/ctx-demo", "[url \"git@github-demo:\"]\n\tinsteadOf = git@github.com:\n")
			}},
		{name: "no ssh host", args: evalBashArgs, auto: true, home: func(f *fixture) { _ = os.Remove(filepath.Join(f.home, ".ssh", "ctx_config")) }},
		{name: "direnv", args: evalBashArgs, auto: true, home: func(f *fixture) {
			_ = os.Remove(filepath.Join(f.home, "w", "demo", "mise.toml"))
			f.writeHome("w/demo/.envrc", "")
		}},
		{name: "mise absent falls back to direnv", args: evalBashArgs, auto: true,
			only: []string{"gh", "az", "gcloud", "kubectl", "direnv", "ssh-add", "ssh-keygen"},
			home: func(f *fixture) { f.writeHome("w/demo/.envrc", "") }},
		{name: "outside work dir", args: evalBashArgs, auto: true, cwdHome: true},
		{name: "minimal profile", args: evalBashArgs, conf: func(string) string { return "PROFILE_NAME=demo\n" }},
		{name: "no eval", args: []string{"use", "demo"}},
		{name: "no eval auto", args: []string{"use", "demo"}, auto: true},
		{name: "eval=zsh", args: []string{"use", "--eval=zsh", "demo"}, auto: true},
		{name: "eval zsh", args: []string{"use", "--eval", "zsh", "demo"}, auto: true},
		{name: "eval= empty", args: []string{"use", "--eval=", "demo"}, auto: true},
		{name: "eval without shell", args: []string{"use", "demo", "--eval"}, auto: true},
		{name: "eval=fish", args: []string{"use", "demo", "--eval=fish"}, auto: true},
		{name: "missing profile", args: []string{"use", "ghost", "--eval", "bash"}},
		{name: "no name", args: []string{"use", "--eval", "bash"}},
		{name: "unknown flag", args: []string{"use", "-x", "demo"}},
		{name: "two names", args: []string{"use", "demo", "other"}},
		{name: "secrets under PROFILE_NAME", args: evalBashArgs, auto: true,
			conf: func(s string) string { return dropLine(s, "PROFILE_NAME") + "PROFILE_NAME=alias\n" },
			home: func(f *fixture) { f.write("secrets/alias/API_KEY", "aliased") }},
		{name: "secret named like profile var", args: evalBashArgs, auto: true,
			conf: func(s string) string { return dropLine(s, "SECRET_KEYS") + "SECRET_KEYS=\"API_KEY WORK_DIR\"\n" },
			home: func(f *fixture) {
				_ = os.Remove(filepath.Join(f.home, "w", "demo", "mise.toml"))
				f.writeHome("w/demo/.envrc", "")
				f.write("secrets/demo/WORK_DIR", "from-secret")
			}},
	}
	// Built once and shared by every scenario (read-only): the git wrapper,
	// the fake tool sets and the system-tools-only dir.
	gitDir := t.TempDir()
	testutil.Script(t, filepath.Join(gitDir, "git"), "#!/bin/sh\n{ printf git; for a in \"$@\"; do printf '\\037%s' \"$a\"; done; printf '\\n'; } >>\"$CTX_TEST_CALLS\"\nexec '"+realGit+"' \"$@\"\n")
	minimal := minimalPath(t)
	fakeDirs := map[string]string{}
	for _, sc := range scenarios {
		key := strings.Join(sc.only, " ")
		if _, ok := fakeDirs[key]; !ok {
			fakeDirs[key] = writeUseFakes(t, sc.only...)
		}
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			p := newPtest(t)
			f := newPFixture(p)
			work := filepath.Join(f.home, "w", "demo")
			cwd := filepath.Join(work, "sub")
			if sc.cwdHome {
				cwd = f.home
			}
			state := t.TempDir()
			calls, agent := filepath.Join(state, "calls"), filepath.Join(state, "agent")
			path := gitDir + string(os.PathListSeparator)
			switch {
			case sc.minimal:
				path += minimal
			case sc.only != nil:
				// Only these fakes and system tools: a real mise (or other
				// tool) on the caller's PATH must not stand in for an absent one.
				path += fakeDirs[strings.Join(sc.only, " ")] + string(os.PathListSeparator) + minimal
			default:
				path += fakeDirs[""] + string(os.PathListSeparator) + p.Getenv("PATH")
			}
			conf := fmt.Sprintf("PROFILE_NAME=demo\nGIT_NAME=\"Dev\"\nGIT_EMAIL=\"dev@demo.com\"\nWORK_DIR=\"%s\"\n"+
				"GITHUB_USER=\"devgh\"\nSSH_KEY_PATH=\"%s\"\nAWS_PROFILE_NAME=\"demo-prod\"\n"+
				"AZURE_SUBSCRIPTION=\"sub-1\"\nAZURE_TENANT=\"ten-1\"\nGCP_PROJECT=\"proj-1\"\nGCP_ACCOUNT=\"me@x.com\"\n"+
				"KUBE_CONTEXT=\"kc-1\"\nSECRET_KEYS=\"API_KEY DB_PASS\"\n", work, filepath.Join(f.home, ".ssh", "id_demo"))
			if sc.conf != nil {
				conf = sc.conf(conf)
			}
			setup := func() {
				for _, p := range []string{f.home, state} {
					if err := os.RemoveAll(p); err != nil {
						t.Fatal(err)
					}
				}
				for _, d := range []string{cwd, state} {
					if err := os.MkdirAll(d, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				f.write("config", "secret_provider=file\n"+strings.ReplaceAll(sc.config, "<HOME>", f.home))
				f.write("profiles/demo.conf", conf)
				f.write("secrets/demo/API_KEY", secret)
				f.write("secrets/demo/DB_PASS", "pw\n")
				f.writeHome(".ssh/id_demo", "private\n")
				f.writeHome(".ssh/id_demo.pub", "ssh-ed25519 AAAA demo\n")
				f.writeHome(".ssh/ctx_config", "Host github-demo\n  HostName github.com\n")
				f.writeHome(".config/git/ctx-demo", "[user]\n\tname = Dev\n")
				f.writeHome("w/demo/mise.toml", "[env]\n")
				if sc.agent != "" {
					if err := os.WriteFile(agent, []byte(sc.agent), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if sc.home != nil {
					sc.home(f)
				}
			}
			p.Setenv("PATH", path)
			p.Setenv("PWD", cwd)
			if quiet {
				p.Setenv("CTX_QUIET", "1")
			} else {
				p.Setenv("CTX_QUIET", "")
			}
			p.Setenv("CTX_AUTO_SWITCH", "")
			if sc.auto {
				p.Setenv("CTX_AUTO_SWITCH", "1")
			}
			p.Setenv("CTX_SECRET_PROVIDER", "")
			p.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			p.Setenv("GIT_NAME", "leaked-from-env") // load_profile unsets it before tools run
			p.Setenv("AWS_PROFILE", "")
			p.Setenv("CTX_TEST_CALLS", calls)
			p.Setenv("CTX_TEST_AGENT", agent)
			for k, v := range sc.env {
				p.Setenv(k, v)
			}
			collect := func(out, errb string, code int) useRun {
				var lines []string
				if data, err := os.ReadFile(calls); err == nil {
					lines = strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(data), f.home, "<HOME>"), "\n"), "\n")
				}
				return useRun{out, errb, code, lines, testutil.Snapshot(t, f.home)}
			}

			setup()
			cmd := exec.Command(filepath.Join(repoRoot, "bin", "ctx"), sc.args...)
			cmd.Dir, cmd.Env = cwd, p.Environ("CTX_CORE=off")
			var bo, be bytes.Buffer
			cmd.Stdout, cmd.Stderr = &bo, &be
			code := 0
			if err := cmd.Run(); err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				code = ee.ExitCode()
			}
			want := collect(bo.String(), be.String(), code)

			setup()
			var gout, gerr bytes.Buffer
			var gcode int
			p.inProc(cwd, func() { gcode = UseIn(tui.Term{Out: &gout, Err: &gerr}, sc.args[1:]) })
			got := collect(gout.String(), gerr.String(), gcode)

			isEval, shell := false, "bash"
			for _, a := range sc.args {
				if a == "--eval" || strings.HasPrefix(a, "--eval=") && a != "--eval=" {
					isEval = true
				}
				if a == "fish" || a == "--eval=fish" {
					shell = "fish"
				}
			}
			if isEval && shell == "bash" && want.code == got.code && want.stdout != "" {
				// Eval-equivalent: the same names in the same order with the same values.
				if g, w := evalBash(t, realBash, got.stdout), evalBash(t, realBash, want.stdout); g != w {
					t.Errorf("eval output differs\n go:   %s\n bash: %s", g, w)
				}
				got.stdout, want.stdout = "", ""
			}
			if got.stdout != want.stdout {
				t.Errorf("stdout differs\n go:   %q\n bash: %q", got.stdout, want.stdout)
			}
			if got.stderr != want.stderr {
				t.Errorf("stderr differs\n go:   %q\n bash: %q", got.stderr, want.stderr)
			}
			if got.code != want.code {
				t.Errorf("exit code: go %d, bash %d", got.code, want.code)
			}
			if !reflect.DeepEqual(got.calls, want.calls) {
				t.Errorf("tool calls differ\n go:   %q\n bash: %q", got.calls, want.calls)
			}
			if d := testutil.DiffSnapshots(want.tree, got.tree); d != "" {
				t.Errorf("files differ (first=bash):\n%s", d)
			}
			if !quiet && sc.name == "no eval" && (!strings.Contains(want.stdout, "Active context: demo") || !strings.Contains(want.stdout, "SSH key → id_demo") || !strings.Contains(want.stdout, "Secret → API_KEY") || !strings.Contains(want.stderr, "not applied to this shell")) {
				t.Errorf("bash non-quiet run looks vacuous: %q", want.stdout)
			}
			if sc.name == "mise absent falls back to direnv" && !strings.Contains(strings.Join(want.calls, "\n"), "direnv") {
				t.Errorf("bash run did not fall back to direnv: calls %q", want.calls)
			}
			if sc.name == "full profile auto switch" && (len(want.calls) < 8 || !strings.Contains(want.tree[".ctx/ctx.log"], "Switched to profile: demo")) {
				t.Errorf("bash run looks vacuous: calls %q", want.calls)
			}
		})
	}
}

// writeHome writes a file under $HOME.
func (f *fixture) writeHome(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func dropLine(s, key string) string {
	var b strings.Builder
	for _, l := range strings.SplitAfter(s, "\n") {
		if !strings.HasPrefix(l, key+"=") {
			b.WriteString(l)
		}
	}
	return b.String()
}

// restoreEnv resets the process environment to saved (Use exports into it).
func restoreEnv(saved []string) {
	os.Clearenv()
	for _, kv := range saved {
		k, v, _ := strings.Cut(kv, "=")
		_ = os.Setenv(k, v)
	}
}

// TestUseMinimalProfile: a profile with nothing to switch prints only the
// active-profile export and no messages.
func TestUseMinimalProfile(t *testing.T) {
	f := newFixture(t)
	f.write("profiles/demo.conf", "PROFILE_NAME=demo\n")
	t.Setenv("CTX_QUIET", "1")
	t.Setenv("CTX_AUTO_SWITCH", "1")
	saved := os.Environ()
	defer restoreEnv(saved)
	var out, errb bytes.Buffer
	if code := UseIn(tui.Term{Out: &out, Err: &errb}, []string{"demo", "--eval", "bash"}); code != 0 || errb.String() != "" {
		t.Fatalf("code %d stderr %q", code, errb.String())
	}
	if out.String() != "export CTX_ACTIVE_PROFILE=demo\n" {
		t.Errorf("stdout %q", out.String())
	}
}

// TestUseRichActiveContext: rich mode keeps the wording of the closing block
// but styles the "Active context" line with Accent; --eval keeps it on stderr.
func TestUseRichActiveContext(t *testing.T) {
	f := newFixture(t)
	f.write("profiles/demo.conf", "PROFILE_NAME=demo\n")
	t.Setenv("CTX_QUIET", "")
	t.Setenv("CTX_AUTO_SWITCH", "1")
	saved := os.Environ()
	defer restoreEnv(saved)
	var out, errb bytes.Buffer
	if code := UseIn(tui.Term{Out: &out, Err: &errb, Rich: true}, []string{"demo", "--eval", "bash"}); code != 0 {
		t.Fatalf("code %d stderr %q", code, errb.String())
	}
	if out.String() != "export CTX_ACTIVE_PROFILE=demo\n" {
		t.Errorf("stdout %q", out.String())
	}
	if want := "\n" + tui.Accent(78, "Active context: demo") + "\n\n"; !strings.HasSuffix(errb.String(), want) {
		t.Errorf("stderr %q does not end with %q", errb.String(), want)
	}
	if !strings.HasPrefix(errb.String(), "→ Switching to: demo\n") {
		t.Errorf("stderr %q", errb.String())
	}
}
