package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// fixture is a temp HOME with a CTX_DIR inside it.
type fixture struct {
	t         *testing.T
	home, dir string
	p         *ptest // set for a parallel subtest (newPFixture)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, home: home, dir: filepath.Join(home, ".ctx")}
	t.Setenv("HOME", home)
	t.Setenv("CTX_DIR", f.dir)
	t.Setenv("CTX_ACTIVE_PROFILE", "")
	return f
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// profile writes profiles/name.conf with %q-quoted values, as ctx setup does.
func (f *fixture) profile(name string, kv ...string) {
	f.t.Helper()
	var b strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, "%s=%q\n", kv[i], kv[i+1])
	}
	f.write("profiles/"+name+".conf", b.String())
}

func bashBin(t *testing.T) string {
	t.Helper()
	if _, err := lookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	return filepath.Join(repoRoot, "bin", "ctx")
}

// repoRoot is absolute so tests may t.Chdir.
var repoRoot = func() string {
	p, err := filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	return p
}()

// bash runs the Bash implementation in dir.
func (f *fixture) bash(dir string, args ...string) (string, int) {
	f.t.Helper()
	cmd := exec.Command(bashBin(f.t), args...)
	cmd.Dir = dir
	cmd.Env = append(f.environ(), "CTX_CORE=off", "HOME="+f.home, "CTX_DIR="+f.dir)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		f.t.Fatal(err)
	}
	return out.String(), code
}

// environ is the environment for a child process of this fixture's test.
func (f *fixture) environ() []string {
	if f.p != nil {
		return f.p.Environ()
	}
	return os.Environ()
}

func ctxVersion(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, "lib", "core.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CTX_VERSION="([^"]+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatal("CTX_VERSION not found in lib/core.sh")
	}
	return string(m[1])
}

// keyOrder returns the keys of the JSON object in document order, recursing into arrays of objects.
func keyOrder(t *testing.T, raw string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var keys []string
	depthKey := []bool{}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' {
				depthKey = append(depthKey, true)
			} else if v == '}' {
				depthKey = depthKey[:len(depthKey)-1]
			}
			continue
		case string:
			// A string at an object's key position (every other token) is a key.
			if len(depthKey) > 0 && depthKey[len(depthKey)-1] {
				keys = append(keys, v)
			}
		}
		if len(depthKey) > 0 {
			depthKey[len(depthKey)-1] = !depthKey[len(depthKey)-1]
		}
	}
	return keys
}

func assertSameJSON(t *testing.T, bashOut, goOut string) {
	t.Helper()
	var b, g any
	if err := json.Unmarshal([]byte(bashOut), &b); err != nil {
		t.Fatalf("bash output is not JSON: %v\n%s", err, bashOut)
	}
	if err := json.Unmarshal([]byte(goOut), &g); err != nil {
		t.Fatalf("go output is not JSON: %v\n%s", err, goOut)
	}
	if !reflect.DeepEqual(b, g) {
		t.Errorf("JSON differs\nbash: %s\ngo:   %s", bashOut, goOut)
	}
	if !reflect.DeepEqual(keyOrder(t, bashOut), keyOrder(t, goOut)) {
		t.Errorf("key order differs\nbash: %v\ngo:   %v", keyOrder(t, bashOut), keyOrder(t, goOut))
	}
}

func TestListJSONMatchesBash(t *testing.T) {
	two := func(f *fixture) {
		f.profile("acme", "GIT_NAME", "Ann", "GIT_EMAIL", "a@x.io", "WORK_DIR", "/w/acme", "GITHUB_USER", "ann",
			"AWS_PROFILE_NAME", "acme-aws", "AZURE_SUBSCRIPTION", "sub", "GCP_PROJECT", "gp", "KUBE_CONTEXT", "kc", "SECRET_KEYS", "A B")
		f.profile("zed", "GIT_NAME", "Zed")
		f.write("config", "active=zed\n")
	}
	cases := map[string]func(*fixture){
		"no profiles":             func(f *fixture) {},
		"two profiles one active": two,
		"special characters": func(f *fixture) {
			f.profile("odd", "GIT_NAME", "Q \"uote\" back\\slash\ttab", "SECRET_KEYS", "a  b")
			f.write("config", "active=odd\n")
		},
		"no active": func(f *fixture) { f.profile("solo", "GIT_NAME", "S") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			setup(f)
			want, code := f.bash(f.home, "--json", "list")
			if code != 0 {
				t.Fatalf("bash exit %d", code)
			}
			var got bytes.Buffer
			if err := ListJSON(&got, ctxVersion(t)); err != nil {
				t.Fatal(err)
			}
			assertSameJSON(t, want, got.String())
		})
	}
}

func TestStatusJSONMatchesBash(t *testing.T) {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	cases := map[string]struct {
		config func(f *fixture) string
		env    string
	}{
		"auto": {config: func(f *fixture) string { return "active=acme\n" }},
		"manual at pwd": {config: func(f *fixture) string {
			return "active=acme\nactive_source=manual\nmanual_pwd=" + enc(f.home) + "\n"
		}},
		"manual elsewhere": {config: func(f *fixture) string {
			return "active=acme\nactive_source=manual\nmanual_pwd=" + enc("/somewhere/else") + "\n"
		}},
		"manual without anchor": {config: func(f *fixture) string { return "active=acme\nactive_source=manual\n" }},
		"env var set":           {config: func(f *fixture) string { return "active=acme\n" }, env: "acme"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.profile("acme", "GIT_NAME", "Q \"uote\" back\\slash\ttab", "GIT_EMAIL", "a@x.io", "WORK_DIR", "/w/acme",
				"GITHUB_USER", "ann", "SSH_KEY_PATH", "/k", "AWS_PROFILE_NAME", "aws", "AZURE_SUBSCRIPTION", "sub",
				"GCP_PROJECT", "gp", "KUBE_CONTEXT", "kc", "SECRET_KEYS", "A  B")
			f.write("config", c.config(f))
			t.Setenv("CTX_ACTIVE_PROFILE", c.env)
			want, code := f.bash(f.home, "--json", "status")
			if code != 0 {
				t.Fatalf("bash exit %d", code)
			}
			var got bytes.Buffer
			if err := StatusJSON(&got, ctxVersion(t), f.home); err != nil {
				t.Fatal(err)
			}
			assertSameJSON(t, want, got.String())
		})
	}
}

func TestStatusNoActive(t *testing.T) {
	f := newFixture(t)
	want, code := f.bash(f.home, "--json", "status")
	if code != 0 {
		t.Fatalf("bash exit %d", code)
	}
	var got bytes.Buffer
	if err := StatusJSON(&got, ctxVersion(t), f.home); err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, want, got.String())
	if !strings.Contains(got.String(), `"active":null`) {
		t.Errorf("want active:null, got %s", got.String())
	}
	for _, d := range []string{"", "profiles", "backups"} {
		if fi, err := os.Stat(filepath.Join(f.dir, d)); err != nil || fi.Mode().Perm() != 0o700 && d != "backups" {
			t.Errorf("dir %q not created with 0700: %v %v", d, fi, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir, "config")); err != nil {
		t.Errorf("config not created: %v", err)
	}
}

func TestStatusMissingProfileErrors(t *testing.T) {
	f := newFixture(t)
	f.write("config", "active=ghost\n")
	var got bytes.Buffer
	err := StatusJSON(&got, "x", f.home)
	if err == nil || !strings.Contains(err.Error(), "Profile 'ghost' not found") {
		t.Fatalf("want not-found error, got %v", err)
	}
	if got.Len() != 0 {
		t.Errorf("unexpected stdout: %s", got.String())
	}
}
