package secret

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func TestRedactor(t *testing.T) {
	r := NewRedactor([]string{"abc", "pass", "password1", "", "pass"})
	cases := map[string]string{
		"password1":           "<redacted>", // longest first, not "<redacted>word1"
		"pass and password1":  "<redacted> and <redacted>",
		"abc is short":        "abc is short",          // < 4 chars ignored
		"token=xxpassxx;":     "token=xx<redacted>xx;", // inside a larger token
		"nothing secret here": "nothing secret here",
	}
	for in, want := range cases {
		if got := r.String(in); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
		if got := string(r.Bytes([]byte(in))); got != want {
			t.Errorf("Bytes(%q) = %q, want %q", in, got, want)
		}
	}
	var nilR *Redactor
	if got := nilR.String("password1"); got != "password1" {
		t.Errorf("nil redactor changed input: %q", got)
	}
	if got := NewRedactor(nil).String("x"); got != "x" {
		t.Errorf("empty redactor changed input: %q", got)
	}
	if got := r.With("s3cr3t-extra").String("s3cr3t-extra pass"); got != "<redacted> <redacted>" {
		t.Errorf("With: %q", got)
	}
}

func TestCLIGetter(t *testing.T) {
	dir, log := testutil.FakeBin(t, "ctx", `printf 's3cr3t\n'`)
	g := CLIGetter{CtxBin: filepath.Join(dir, "ctx")}
	v, err := g.Get("mtn", "TKGI_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if v != "s3cr3t" {
		t.Errorf("Get = %q, want s3cr3t", v)
	}
	want := [][]string{{"secret", "get", "mtn", "TKGI_PASSWORD"}}
	if got := testutil.ReadArgv(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestCLIGetterDefaultsToPATH(t *testing.T) {
	dir, _ := testutil.FakeBin(t, "ctx", `printf 'v1v1v1\n\n'`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	v, err := CLIGetter{}.Get("p", "K")
	if err != nil {
		t.Fatal(err)
	}
	if v != "v1v1v1\n" { // only one trailing newline is trimmed
		t.Errorf("Get = %q", v)
	}
}

func TestCLIGetterMissing(t *testing.T) {
	dir, _ := testutil.FakeBin(t, "ctx", `echo "error: no secret K for p" >&2; exit 1`)
	_, err := CLIGetter{CtxBin: filepath.Join(dir, "ctx")}.Get("p", "K")
	if err == nil || !strings.Contains(err.Error(), "no secret K for p") {
		t.Fatalf("err = %v", err)
	}
}

type mapGetter map[string]string

func (m mapGetter) Get(_, key string) (string, error) {
	if v, ok := m[key]; ok {
		return v, nil
	}
	return "", errors.New("missing")
}

func TestLoadRedactor(t *testing.T) {
	p := &profile.Profile{Name: "mtn", SecretKeys: []string{"API_TOKEN", "GONE"}}
	s := &knowledge.Store{Clusters: map[string]knowledge.Cluster{
		"nonprod": {Login: knowledge.Login{Type: "command", Secrets: []string{"TKGI_PASSWORD"}}},
	}}
	r := LoadRedactor(p, s, mapGetter{"API_TOKEN": "tok-123456", "TKGI_PASSWORD": "hunter22"})
	got := r.String("tok-123456 hunter22 GONE")
	if got != "<redacted> <redacted> GONE" {
		t.Errorf("got %q", got)
	}
	if LoadRedactor(nil, nil, mapGetter{}) == nil {
		t.Error("LoadRedactor(nil, nil) returned nil")
	}
}
