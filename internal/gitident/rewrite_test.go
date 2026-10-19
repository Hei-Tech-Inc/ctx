package gitident

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

var repoRoot = func() string {
	p, err := filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	return p
}()

// TestSetRewriteMatchesBash runs ctx_git_identity_set_rewrite (sourced from
// lib/core.sh) and SetRewrite against the same HOME and compares the output,
// the git calls, the identity file and ctx.log.
func TestSetRewriteMatchesBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	const key = "url.git@github-demo:.insteadOf"
	cases := []struct {
		name      string
		identity  string // "" = no identity file
		sshConfig string
	}{
		{name: "no identity file", sshConfig: "Host github-demo\n"},
		{name: "no ssh host", identity: "[user]\n\tname = Dev\n", sshConfig: "Host github-other\n"},
		{name: "no ssh config", identity: "[user]\n\tname = Dev\n"},
		{name: "host prefix only", identity: "[user]\n\tname = Dev\n", sshConfig: "Host github-demo2\n"},
		{name: "adds rewrite", identity: "[user]\n\tname = Dev\n", sshConfig: "Include x\nHost github-demo\n  HostName github.com\n"},
		{name: "already present", identity: "[user]\n\tname = Dev\n[url \"git@github-demo:\"]\n\tinsteadOf = git@github.com:\n", sshConfig: "Host github-demo\n"},
		{name: "partial rewrite replaced", identity: "[url \"git@github-demo:\"]\n\tinsteadOf = ssh://git@github.com/\n\tinsteadOf = other\n", sshConfig: "Host github-demo\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctxDir := filepath.Join(home, ".ctx")
			gitDir, gitLog := testutil.FakeBin(t, "git", `exec '`+realGit+`' "$@"`)
			t.Setenv("HOME", home)
			t.Setenv("CTX_DIR", ctxDir)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("PATH", gitDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			setup := func() {
				if err := os.RemoveAll(home); err != nil {
					t.Fatal(err)
				}
				_ = os.Remove(gitLog)
				for _, d := range []string{filepath.Join(home, ".config", "git"), filepath.Join(home, ".ssh")} {
					if err := os.MkdirAll(d, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				if c.identity != "" {
					if err := os.WriteFile(filepath.Join(home, ".config", "git", "ctx-demo"), []byte(c.identity), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if c.sshConfig != "" {
					if err := os.WriteFile(filepath.Join(home, ".ssh", "ctx_config"), []byte(c.sshConfig), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			type result struct {
				out, err string
				calls    [][]string
				tree     map[string]string
			}
			collect := func(o, e string) result {
				return result{o, e, testutil.ReadArgv(t, gitLog), testutil.Snapshot(t, home)}
			}

			setup()
			cmd := exec.Command(bash, "-c", `source "$1/lib/core.sh"; set -uo pipefail; ctx_git_identity_set_rewrite demo`, "_", repoRoot)
			var bo, be bytes.Buffer
			cmd.Stdout, cmd.Stderr = &bo, &be
			_ = cmd.Run()
			want := collect(bo.String(), be.String())

			setup()
			var gotOut, gotErr bytes.Buffer
			SetRewrite("demo", &gotOut, &gotErr)
			got := collect(gotOut.String(), gotErr.String())

			if got.out != want.out || got.err != want.err {
				t.Errorf("output differs\n go:   %q %q\n bash: %q %q", got.out, got.err, want.out, want.err)
			}
			if d := testutil.DiffSnapshots(want.tree, got.tree); d != "" {
				t.Errorf("files differ (first=bash):\n%s", d)
			}
			if !equalCalls(got.calls, want.calls) {
				t.Errorf("git calls differ\n go:   %q\n bash: %q", got.calls, want.calls)
			}
			if c.name == "adds rewrite" {
				out, _ := exec.Command(realGit, "config", "--file", filepath.Join(home, ".config", "git", "ctx-demo"), "--get-all", key).Output()
				if string(out) != "git@github.com:\nssh://git@github.com/\n" {
					t.Errorf("rewrite not written: %q", out)
				}
			}
		})
	}
}

func equalCalls(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
