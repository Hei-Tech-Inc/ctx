package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func extendHome(t *testing.T, conf string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", home+"/.ctx")
	t.Setenv("CTX_WORK_ROOT", "")
	t.Setenv("CTX_SECRET_PROVIDER", "file")
	if conf != "" {
		p := filepath.Join(home, ".ctx", "profiles", "acme.conf")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(conf), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// TestExtendPrefill: every field comes from the .conf and is kept when its
// section is skipped; SECRET_KEYS splits like read -ra; EXTRA_ENVS stays text
// and new pairs follow it.
func TestExtendPrefill(t *testing.T) {
	home := extendHome(t, "PROFILE_NAME=other\nGIT_NAME=Dev\nGIT_EMAIL=dev@acme.com\nWORK_DIR=/w\n"+
		"GITHUB_USER=alice\nSSH_KEY_PATH=/nope\nAWS_PROFILE_NAME=''\nAZURE_TENANT=ten\n"+
		"SECRET_KEYS='GH_TOKEN\tOLD\nIGNORED'\nEXTRA_ENVS='A=x=y\nB=\"q\"'\nSIGNING_FORMAT=openpgp\nSIGNING_KEY=K1\n")
	answers := []string{
		"n",     // identity
		"n",     // SSH key (the path is missing)
		"y", "", // cloud: AWS blank
		"sub", "", "", "", // Azure sub, tenant kept, GCP blank, kube blank
		"n",               // secrets
		"y", "C", "3", "", // env vars
	}
	tw := fullWizard(t, home, strings.Join(answers, "\n")+"\n")
	in := Inputs{Profile: "acme", AutoYes: true}
	if err := tw.Extend(&in); err != nil {
		t.Fatalf("Extend: %v\n%s", err, strings.Join(tw.out.lines, "\n"))
	}
	want := Inputs{
		Scope: "full", Extend: true, AutoYes: true, Profile: "acme", GitName: "Dev", GitEmail: "dev@acme.com",
		WorkDir: "/w", GitHubUser: "alice", SSHKeyPath: "/nope", AzureSub: "sub", AzureTenant: "ten",
		SecretKeys: []string{"GH_TOKEN", "OLD"}, ExtraEnvsRaw: "A=x=y\nB=\"q\"", ExtraEnvs: []EnvPair{{"C", "3"}},
		SigningFormat: "openpgp", SigningKey: "K1",
	}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("inputs\n got %+v\nwant %+v", in, want)
	}
	if got := extraEnvsText(in); got != "A=x=y\nB=\"q\"C=3\n" {
		t.Errorf("EXTRA_ENVS = %q", got)
	}
	all := strings.Join(tw.out.lines, "\n")
	for _, m := range []string{"bold:Extend profile: acme", "dim:  SSH: /nope   WORK_DIR: /w   Git: Dev <dev@acme.com>"} {
		if !strings.Contains(all, m) {
			t.Errorf("output lacks %q:\n%s", m, all)
		}
	}
}

func TestExtendMissingProfile(t *testing.T) {
	home := extendHome(t, "")
	tw := fullWizard(t, home, "")
	in := Inputs{Profile: "nope"}
	err := tw.Extend(&in)
	if de, ok := err.(*DieError); !ok || de.Msg != "setup --extend: profile 'nope' not found" || de.Hint {
		t.Errorf("err = %#v", err)
	}
}

func TestReadWords(t *testing.T) {
	for in, want := range map[string][]string{
		"":              nil,
		"  A \tB  ":     {"A", "B"},
		"A B\nC":        {"A", "B"},
		"\nA":           nil,
		"A\u00a0B C\\D": {"A\u00a0B", "C\\D"},
	} {
		if got := readWords(in); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
			t.Errorf("readWords(%q) = %q, want %q", in, got, want)
		}
	}
}
