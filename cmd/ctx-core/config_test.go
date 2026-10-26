package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// cfgScenario is one `ctx config ...` run, executed against Bash and Go in
// separate but identically seeded roots.
type cfgScenario struct {
	name string
	args []string
	env  map[string]string
	seed func(t *testing.T, root string) // optional
	// errHas, when set, are lines Bash's stderr must contain.
	errHas []string
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func seedConfigFile(body string) func(*testing.T, string) {
	return func(t *testing.T, root string) { writeFile(t, filepath.Join(root, ".ctx", "config"), body, 0o644) }
}

// newRoot returns a fresh symlink-resolved temp root.
func newRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// setRootEnv points HOME and CTX_DIR at root and clears the variables the
// config command reads, then applies extra.
func setRootEnv(t *testing.T, root string, extra map[string]string) {
	t.Helper()
	t.Setenv("HOME", root)
	t.Setenv("CTX_DIR", filepath.Join(root, ".ctx"))
	t.Setenv("TZ", "UTC")
	for _, k := range []string{"CTX_WORK_ROOT", "CTX_SECRET_PROVIDER", "CTX_AUTOSWITCH_NOTIFY"} {
		t.Setenv(k, "")
	}
	for k, v := range extra {
		t.Setenv(k, v)
	}
}

type runResult struct {
	out, err string
	code     int
}

// dropToolDiag removes mkdir:/cp: diagnostics, whose wording differs between
// coreutils implementations.
func dropToolDiag(s string) string {
	var keep []string
	for _, l := range strings.SplitAfter(s, "\n") {
		if !strings.HasPrefix(l, "cp: ") && !strings.HasPrefix(l, "mkdir: ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "")
}

// runBoth runs sc.args on Bash then Go and fails on any difference in
// stdout, stderr, exit code or resulting tree.
func runBoth(t *testing.T, subcmd []string, sc cfgScenario) {
	t.Helper()
	broot, groot := newRoot(t), newRoot(t)
	// Freeze the backup stamp for both sides so a second boundary cannot split a run.
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	oldNow := ctxconfig.Now
	ctxconfig.Now = func() time.Time { return fixed }
	defer func() { ctxconfig.Now = oldNow }()
	fakeDate := testutil.FixedStampDate(t, fixed.Format("20060102-150405"))
	var got [2]runResult
	var snaps [2]map[string]string
	for i, root := range []string{broot, groot} {
		setRootEnv(t, root, sc.env)
		if sc.seed != nil {
			sc.seed(t, root)
		}
		args := append(append([]string{}, subcmd...), sc.args...)
		if i == 0 {
			t.Setenv("PATH", fakeDate+string(os.PathListSeparator)+os.Getenv("PATH"))
			o, e, c := bashCtx(t, args...)
			got[i] = runResult{strings.ReplaceAll(o, root, "<ROOT>"), dropToolDiag(strings.ReplaceAll(e, root, "<ROOT>")), c}
		} else {
			var o, e bytes.Buffer
			c := run(args, &o, &e)
			got[i] = runResult{strings.ReplaceAll(o.String(), root, "<ROOT>"), dropToolDiag(strings.ReplaceAll(e.String(), root, "<ROOT>")), c}
		}
		snaps[i] = testutil.Snapshot(t, root)
	}
	if got[0] != got[1] {
		t.Errorf("%s %q:\n bash exit %d out %q err %q\n go   exit %d out %q err %q", sc.name, sc.args,
			got[0].code, got[0].out, got[0].err, got[1].code, got[1].out, got[1].err)
	}
	if d := testutil.DiffSnapshots(snaps[0], snaps[1]); d != "" {
		t.Errorf("%s %q: trees differ (first = bash):\n%s", sc.name, sc.args, d)
	}
	for _, l := range sc.errHas {
		if !strings.Contains(got[0].err, l+"\n") {
			t.Errorf("%s: bash stderr lacks %q:\n%s", sc.name, l, got[0].err)
		}
	}
}

func TestConfigCLIMatchesBash(t *testing.T) {
	full := "work_root=/srv/w\nsecret_provider=FILE\nprompt_workdir_max_depth=007\nprompt_extra_paths=/a:/b\nautoswitch_notify=ON\nactive=p\n"
	asFile := func(t *testing.T, root string) { writeFile(t, filepath.Join(root, "blocker"), "x", 0o644) }
	scenarios := []cfgScenario{
		{name: "no args", args: nil},
		{name: "show", args: []string{"show"}},
		{name: "show full", args: []string{"show"}, seed: seedConfigFile(full)},
		{name: "show crlf", args: []string{"show"}, seed: seedConfigFile("work_root=/w\r\nprompt_extra_paths=/a\r\nprompt_workdir_max_depth=3\r\nautoswitch_notify=on\r\n")},
		{name: "show bad depth", args: []string{"show"}, seed: seedConfigFile("prompt_workdir_max_depth=x1\nsecret_provider=nope\n")},
		{name: "show env", args: []string{"show"}, env: map[string]string{"CTX_WORK_ROOT": "~/envroot", "CTX_SECRET_PROVIDER": "Pass", "CTX_AUTOSWITCH_NOTIFY": "TRUE"}, seed: seedConfigFile(full)},
		{name: "show duplicate keys", args: []string{"show"}, seed: seedConfigFile("work_root=/one\nwork_root=/two\nsecret_provider=file\nsecret_provider=pass\n")},

		{name: "work-root show", args: []string{"work-root"}},
		{name: "work-root set", args: []string{"work-root", "/tmp/ctx-cfg-test-unused"}},
		{name: "work-root tilde", args: []string{"work-root", "~/clients2"}},
		{name: "work-root spaces", args: []string{"work-root", "~/a b/c"}},
		{name: "work-root backslash", args: []string{"work-root", `~/a\tb`}},
		{name: "work-root under file", args: []string{"work-root", "~/blocker/x"}, seed: asFile},
		{name: "work-root newline", args: []string{"work-root", "~/n\nl"}},

		{name: "secret-provider show", args: []string{"secret-provider"}},
		{name: "secret-provider keychain", args: []string{"secret-provider", "Keychain"}},
		{name: "secret-provider file", args: []string{"secret-provider", "FILE"}},
		{name: "secret-provider pass", args: []string{"secret-provider", "pass"}, seed: seedConfigFile("secret_provider=file\n")},
		{name: "secret-provider auto", args: []string{"secret-provider", "auto"}},
		{name: "secret-provider bogus", args: []string{"secret-provider", "Bogus"}},
		{name: "secret-provider -n", args: []string{"secret-provider", "-n"}},
		{name: "secret-provider trailing newline", args: []string{"secret-provider", "file\n"}},
		{name: "secret-provider backslash", args: []string{"secret-provider", `a\tb`}},

		{name: "depth show", args: []string{"prompt-workdir-depth"}, seed: seedConfigFile(full)},
		{name: "depth set", args: []string{"prompt-workdir-depth", "3"}},
		{name: "depth leading zeros", args: []string{"prompt-workdir-depth", "007"}},
		{name: "depth zero", args: []string{"prompt-workdir-depth", "0"}},
		{name: "depth letters", args: []string{"prompt-workdir-depth", "abc"}},
		{name: "depth negative", args: []string{"prompt-workdir-depth", "-1"}},
		{name: "depth newline", args: []string{"prompt-workdir-depth", "1\n2"}},

		{name: "extra show none", args: []string{"prompt-extra-paths"}},
		{name: "extra show set", args: []string{"prompt-extra-paths"}, seed: seedConfigFile(full)},
		{name: "extra set", args: []string{"prompt-extra-paths", "/a:/b"}},
		{name: "extra set backslash", args: []string{"prompt-extra-paths", `/a\n:/b&|`}},
		{name: "extra clear", args: []string{"prompt-extra-paths", "clear"}, seed: seedConfigFile(full)},
		{name: "extra newline", args: []string{"prompt-extra-paths", "a\nb"}},

		{name: "notify show off", args: []string{"autoswitch-notify"}},
		{name: "notify show on", args: []string{"autoswitch-notify"}, seed: seedConfigFile("autoswitch_notify=on\n")},
		{name: "notify show env", args: []string{"autoswitch-notify"}, env: map[string]string{"CTX_AUTOSWITCH_NOTIFY": "1"}},
		{name: "notify on", args: []string{"autoswitch-notify", "ON"}},
		{name: "notify yes", args: []string{"autoswitch-notify", "yes"}},
		{name: "notify 1", args: []string{"autoswitch-notify", "1"}},
		{name: "notify off", args: []string{"autoswitch-notify", "Off"}, seed: seedConfigFile("autoswitch_notify=on\nx=1\n")},
		{name: "notify no", args: []string{"autoswitch-notify", "no"}},
		{name: "notify maybe", args: []string{"autoswitch-notify", "maybe"}},
		{name: "notify -e", args: []string{"autoswitch-notify", "-e"}},

		{name: "unknown", args: []string{"bogus"}},
		{name: "export no dir", args: []string{"export"}},
		{name: "import no dir", args: []string{"import"}},
		{name: "import missing dir", args: []string{"import", "~/nope"}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) { runBoth(t, []string{"config"}, sc) })
	}
}

func TestConfigImplements(t *testing.T) {
	for _, sub := range []string{"", "show", "work-root", "secret-provider", "prompt-workdir-depth", "prompt-extra-paths", "autoswitch-notify", "export", "import"} {
		if !implements("config", []string{sub}) {
			t.Errorf("implements config %q = false", sub)
		}
	}
	if !implements("config", nil) {
		t.Error("implements config (no sub) = false")
	}
	if implements("config", []string{"bogus"}) {
		t.Error("implements config bogus = true")
	}
}

// exportImportScenario seeds a populated source setup.
func seedFull(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ".ctx", "config"), "work_root=/w\nactive=a\nmanual_pwd=eA==\n", 0o600)
	writeFile(t, filepath.Join(root, ".ctx", "profiles", "a.conf"), "PROFILE_NAME=\"a\"\nWORK_DIR=\""+filepath.Join(root, "work", "a")+"\"\n", 0o600)
	writeFile(t, filepath.Join(root, ".ctx", "profiles", "b.conf"), "PROFILE_NAME=\"b\"\nWORK_DIR=\"~/work/b\"\n", 0o644)
	writeFile(t, filepath.Join(root, ".ctx", "profiles", ".hidden.conf"), "WORK_DIR=\"/hidden\"\n", 0o600)
	writeFile(t, filepath.Join(root, ".ctx", "profiles", "notes.txt"), "no\n", 0o600)
	if err := os.MkdirAll(filepath.Join(root, ".ctx", "profiles", "dir.conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".config", "git", "ctx-a"), "[user]\n  name = A\n", 0o600)
	writeFile(t, filepath.Join(root, ".config", "git", "ctx-b"), "[user]\n  name = B\n", 0o644)
	writeFile(t, filepath.Join(root, ".config", "git", "other"), "no\n", 0o644)
	writeFile(t, filepath.Join(root, ".ssh", "ctx_config"), "\nHost gh-a\n  HostName github.com\n", 0o600)
	writeFile(t, filepath.Join(root, ".gitconfig"), "[user]\n  name = me\n", 0o644)
}

func TestExportImportMatchesBash(t *testing.T) {
	exportScenarios := []cfgScenario{
		{name: "export full", args: []string{"export", "~/out"}, seed: seedFull},
		{name: "export empty setup", args: []string{"export", "~/out"}},
		{name: "export existing dir keeps modes", args: []string{"export", "~/out"}, seed: func(t *testing.T, root string) {
			seedFull(t, root)
			writeFile(t, filepath.Join(root, "out", "config"), "stale\n", 0o666)
			writeFile(t, filepath.Join(root, "out", "profiles", "a.conf"), "stale\n", 0o640)
		}},
		{name: "export dir is a file", args: []string{"export", "~/blocker/out"}, seed: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "blocker"), "x", 0o644)
		}},
	}
	for _, sc := range exportScenarios {
		t.Run(sc.name, func(t *testing.T) { runBoth(t, []string{"config"}, sc) })
	}

	// Import: build a bundle with the export of a seeded source, then import it into
	// a different setup. The bundle is created by Bash once and copied into both roots.
	bundleRoot := newRoot(t)
	setRootEnv(t, bundleRoot, nil)
	seedFull(t, bundleRoot)
	if _, e, c := bashCtx(t, "config", "export", filepath.Join(bundleRoot, "bundle")); c != 0 {
		t.Fatalf("bundle export: %d %s", c, e)
	}
	withBundle := func(extra func(*testing.T, string)) func(*testing.T, string) {
		return func(t *testing.T, root string) {
			src := filepath.Join(bundleRoot, "bundle")
			err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(src, p)
				b, _ := os.ReadFile(p)
				body := strings.ReplaceAll(string(b), bundleRoot, root)
				writeFile(t, filepath.Join(root, "bundle", rel), body, info.Mode().Perm())
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if extra != nil {
				extra(t, root)
			}
		}
	}
	importScenarios := []cfgScenario{
		{name: "import into empty", args: []string{"import", "~/bundle"}, seed: withBundle(nil)},
		{name: "import no manifest", args: []string{"import", "~/bundle"}, seed: withBundle(func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "bundle", "manifest.env")); err != nil {
				t.Fatal(err)
			}
		})},
		{name: "import over existing setup", args: []string{"import", "~/bundle"}, seed: withBundle(func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".ctx", "config"), "old=1\n", 0o640)
			writeFile(t, filepath.Join(root, ".ctx", "profiles", "a.conf"), "old\n", 0o640)
			writeFile(t, filepath.Join(root, ".ssh", "ctx_config"), "old\n", 0o600)
			writeFile(t, filepath.Join(root, ".ssh", "config"), "Host x\n  User y", 0o644)
			writeFile(t, filepath.Join(root, ".gitconfig"), "[user]\n  name = me\n", 0o644)
		})},
		{name: "import ssh include already present", args: []string{"import", "~/bundle"}, seed: withBundle(func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".ssh", "config"), "# mine\nInclude "+filepath.Join(root, ".ssh", "ctx_config")+"\n", 0o600)
			writeFile(t, filepath.Join(root, ".gitconfig"), "[includeIf \"gitdir:/x/\"]\n  path = "+filepath.Join(root, ".config", "git", "ctx-a")+"\n", 0o644)
		})},
		{name: "import profile without identity", args: []string{"import", "~/bundle"}, seed: withBundle(func(t *testing.T, root string) {
			_ = os.Remove(filepath.Join(root, "bundle", "git-identities", "ctx-b"))
		})},
		{name: "import config only", args: []string{"import", "~/bundle"}, seed: withBundle(func(t *testing.T, root string) {
			_ = os.RemoveAll(filepath.Join(root, "bundle", "profiles"))
			_ = os.RemoveAll(filepath.Join(root, "bundle", "git-identities"))
			_ = os.Remove(filepath.Join(root, "bundle", "ctx_ssh_config"))
		})},
		// $CTX_DIR/backups is a file: every backup fails and is reported on
		// stderr, and the import goes on.
		{name: "import with backups failing", args: []string{"import", "~/bundle"}, seed: withBundle(func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, ".ctx", "config"), "old=1\n", 0o640)
			writeFile(t, filepath.Join(root, ".ctx", "backups"), "not a dir\n", 0o644)
			writeFile(t, filepath.Join(root, ".ssh", "ctx_config"), "old\n", 0o600)
			writeFile(t, filepath.Join(root, ".ssh", "config"), "Host x\n  User y\n", 0o644)
			writeFile(t, filepath.Join(root, ".gitconfig"), "[user]\n  name = me\n", 0o644)
		}), errHas: []string{"! Could not back up <ROOT>/.ctx/config", "! Could not back up <ROOT>/.ssh/ctx_config",
			"! Could not back up <ROOT>/.gitconfig", "! Could not back up <ROOT>/.ssh/config"}},
		{name: "import dir is a file", args: []string{"import", "~/blocker"}, seed: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "blocker"), "x", 0o644)
		}},
	}
	for _, sc := range importScenarios {
		t.Run(sc.name, func(t *testing.T) { runBoth(t, []string{"config"}, sc) })
	}
}
