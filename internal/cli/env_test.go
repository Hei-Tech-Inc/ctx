package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// envFixture has a profile "acme" with tricky values, a file-provider secret,
// an ssh key and a fake gh on PATH.
func envFixture(t *testing.T) *fixture {
	t.Helper()
	buildCore(t) // before HOME moves, so the Go caches stay put
	f := newFixture(t)
	ghDir, _ := testutil.FakeBin(t, "gh", `[ "$1 $2 $3" = "auth token -u" ] && printf "tok-%s\n" "$4"`)
	t.Setenv("PATH", ghDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	key := filepath.Join(f.home, "my keys", "id acme")
	if err := os.MkdirAll(filepath.Dir(key), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(f.home, "w", "acme")
	if err := os.MkdirAll(filepath.Join(work, "r"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Tricky values, written the way ctx does (printf %q).
	conf := bashQ(t, map[string]string{
		"GIT_NAME":           "O'Brien \"q\" $x \\ `b` \u00e9",
		"GIT_EMAIL":          "dev@acme.com",
		"WORK_DIR":           work,
		"GITHUB_USER":        "acme-dev",
		"SSH_KEY_PATH":       key,
		"AWS_PROFILE_NAME":   "acme-prod",
		"GCP_PROJECT":        "proj",
		"GCP_ACCOUNT":        "me@acme",
		"AZURE_SUBSCRIPTION": "sub",
		"AZURE_TENANT":       "ten",
		"SECRET_KEYS":        "API_KEY BAD-KEY OTHER",
		"EXTRA_ENVS":         "FOO=bar baz\nQ=it's $HOME \\n\nBAD KEY=1\nNOEQ\nFOO=again\n9X=no\nEMPTY=\n",
	})
	f.write("profiles/acme.conf", conf)
	f.write("config", "secret_provider=file\n")
	f.write("secrets/acme/API_KEY", "s3'cret $v\nline2")
	f.write("secrets/acme/OTHER", "x")
	t.Setenv("CTX_BIN_PATH", bashBin(t))
	t.Setenv("CTX_CORE", "off") // `ctx secret get` stays in Bash
	t.Chdir(filepath.Join(work, "r"))
	return f
}

func bashQ(t *testing.T, kv map[string]string) string {
	t.Helper()
	var b strings.Builder
	for k, v := range kv {
		out, err := exec.Command("bash", "-c", `printf '%s=%q\n' "$1" "$2"`, "_", k, v).Output()
		if err != nil {
			t.Fatal(err)
		}
		b.Write(out)
	}
	return b.String()
}

func evalDump(t *testing.T, script string, keys []string) string {
	t.Helper()
	dump := ""
	for _, k := range keys {
		dump += `printf '%s\0' "$` + k + `";`
	}
	cmd := exec.Command("bash", "-c", `unset `+strings.Join(keys, " ")+`; eval "$1"; `+dump, "_", script)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("eval: %v\n%s", err, script)
	}
	return string(out)
}

func goEnv(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Env(&buf, args); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

var envKeys = []string{"CTX_ACTIVE_PROFILE", "GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME", "GIT_AUTHOR_EMAIL",
	"GIT_COMMITTER_EMAIL", "GIT_SSH_COMMAND", "GH_TOKEN", "AWS_PROFILE", "CLOUDSDK_CORE_PROJECT",
	"GOOGLE_CLOUD_PROJECT", "CLOUDSDK_CORE_ACCOUNT", "AZURE_SUBSCRIPTION", "AZURE_SUBSCRIPTION_ID",
	"AZURE_TENANT_ID", "FOO", "Q", "NOEQ", "EMPTY", "API_KEY", "OTHER"}

func TestEnvEvalEquivalent(t *testing.T) {
	f := envFixture(t)
	for _, args := range [][]string{{}, {"--secrets"}, {"-p", "acme", "--secrets"}} {
		want, code := f.bash(f.repoDir(), append([]string{"env"}, args...)...)
		if code != 0 {
			t.Fatalf("bash env %v exit %d", args, code)
		}
		got := goEnv(t, args...)
		w, g := evalDump(t, want, envKeys), evalDump(t, got, envKeys)
		if w != g {
			t.Errorf("args %v\nbash: %q\ngo:   %q", args, w, g)
		}
		if len(args) == 0 && strings.Contains(got, "API_KEY") {
			t.Error("secrets printed without --secrets")
		}
	}
	// The sanity check that the comparison is not vacuous.
	if d := evalDump(t, goEnv(t, "--secrets"), []string{"API_KEY", "NOEQ", "FOO"}); !strings.Contains(d, "s3'cret $v\nline2") || !strings.Contains(d, "NOEQ\x00again") {
		t.Errorf("dump %q", d)
	}
}

func TestEnvFish(t *testing.T) {
	f := envFixture(t)
	want, _ := f.bash(f.repoDir(), "env", "--fish", "--secrets")
	got := goEnv(t, "--fish", "--secrets")
	// Fish quoting is a text transform of the same shape Bash uses (no per-line
	// quirks for values without a trailing newline), so compare it as text.
	if strings.TrimSpace(want) != strings.TrimSpace(got) {
		t.Errorf("fish text differs\nbash:\n%s\ngo:\n%s", want, got)
	}
	if !strings.Contains(got, `set -gx GIT_AUTHOR_NAME 'O\'Brien "q" $x \\ `+"`b`") {
		t.Errorf("fish escaping: %s", got)
	}
	if _, err := exec.LookPath("fish"); err != nil {
		t.Log("fish not installed; text check only")
		return
	}
	out, err := exec.Command("fish", "-c", got+`printf '%s\0' $GIT_AUTHOR_NAME $FOO`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "O'Brien \"q\" $x \\ `b` \u00e9\x00bar baz") && !strings.Contains(string(out), "again") {
		t.Errorf("fish eval %q", out)
	}
}

func TestEnvOrderMatchesBash(t *testing.T) {
	f := envFixture(t)
	order := func(s string) []string {
		var ks []string
		for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
			l = strings.TrimPrefix(l, "export ")
			if k, _, ok := strings.Cut(l, "="); ok && !strings.Contains(k, " ") {
				ks = append(ks, k)
			}
		}
		return ks
	}
	want, _ := f.bash(f.repoDir(), "env", "--secrets")
	got := goEnv(t, "--secrets")
	if a, b := strings.Join(order(want), ","), strings.Join(order(got), ","); a != b {
		t.Errorf("order\nbash: %s\ngo:   %s", a, b)
	}
}

func TestCollectEnvConditions(t *testing.T) {
	p := &profile.Profile{Name: "n", SSHKeyPath: "/nonexistent/key", GithubUser: "u", SecretKeys: []string{"A", "B-X"}}
	got, err := CollectEnv(p, true, getterFunc(func(_, k string) (string, error) { return "v" + k, nil }),
		func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != [2]string{"CTX_ACTIVE_PROFILE", "n"} || got[1] != [2]string{"A", "vA"} {
		t.Errorf("got %v", got)
	}
}

type getterFunc func(profile, key string) (string, error)

func (g getterFunc) Get(p, k string) (string, error) { return g(p, k) }

var _ secret.Getter = getterFunc(nil)

func TestFormatEnvBash(t *testing.T) {
	got := FormatEnv([][2]string{{"A", "it's"}, {"B", "plain-1/x"}, {"C", ""}}, "bash")
	if got != "export A='it'\\''s'\nexport B=plain-1/x\nexport C=''\n" {
		t.Errorf("got %q", got)
	}
}

var (
	coreOnce sync.Once
	coreBin  string
	coreErr  error
)

func buildCore(t *testing.T) string {
	t.Helper()
	coreOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ctx-core-test")
		if err != nil {
			coreErr = err
			return
		}
		coreBin = filepath.Join(dir, "ctx-core")
		cmd := exec.Command("go", "build", "-o", coreBin, "./cmd/ctx-core")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			coreErr = fmt.Errorf("%v: %s", err, out)
		}
	})
	if coreErr != nil {
		t.Fatalf("build ctx-core: %v", coreErr)
	}
	return coreBin
}

func runCore(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(buildCore(t), args...)
	cmd.Dir = dir
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

func TestRunExecsWithIdentity(t *testing.T) {
	f := envFixture(t)
	out, errs, code := runCore(t, f.home, "run", "-p", "acme", "--", "sh", "-c", `printf %s "$GIT_AUTHOR_EMAIL|$AWS_PROFILE|$API_KEY|$GH_TOKEN"`)
	if code != 0 || out != "dev@acme.com|acme-prod|s3'cret $v\nline2|tok-acme-dev" {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	// Exit codes and flags pass straight through; cwd profile resolution works.
	out, _, code = runCore(t, filepath.Join(f.home, "w", "acme", "r"), "run", "--", "sh", "-c", `printf %s,%s "$CTX_ACTIVE_PROFILE" "$1"; exit 7`, "x", "--json")
	if code != 7 || out != "acme,--json" {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestRunErrors(t *testing.T) {
	f := envFixture(t)
	empty := t.TempDir()
	_, errs, code := runCore(t, empty, "run", "--", "true")
	if code != 1 || !strings.Contains(errs, "No profile for ") || !strings.Contains(errs, "none active. Use -p <profile>.") {
		t.Errorf("no profile: code %d err %q", code, errs)
	}
	_, errs, code = runCore(t, f.home, "run", "-p", "acme")
	if code != 1 || !strings.Contains(errs, "Usage: ctx run [-p profile] -- <command> [args...]") {
		t.Errorf("usage: code %d err %q", code, errs)
	}
	_, errs, code = runCore(t, f.home, "run", "-p", "nope", "--", "true")
	if code != 1 || !strings.Contains(errs, "Profile 'nope' not found. Run: ctx list") {
		t.Errorf("missing: code %d err %q", code, errs)
	}
	_, errs, code = runCore(t, f.home, "run", "-p", "acme", "--", "no-such-cmd-xyz")
	if code != 127 || !strings.Contains(errs, "no-such-cmd-xyz: not found") {
		t.Errorf("notfound: code %d err %q", code, errs)
	}
	_, errs, code = runCore(t, f.home, "env", "-p", "acme", "--bogus")
	if code != 1 || !strings.Contains(errs, "Usage: ctx env [-p profile] [--fish] [--secrets]") {
		t.Errorf("env usage: code %d err %q", code, errs)
	}
}

func (f *fixture) repoDir() string { return filepath.Join(f.home, "w", "acme", "r") }
