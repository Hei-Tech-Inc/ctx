package profile

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func load(t *testing.T, name string) *Profile {
	t.Helper()
	p, err := Load("testdata", name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadQuoted(t *testing.T) {
	p := load(t, "quoted")
	if p.GitName != "last" || p.GitEmail != "sq@x" || p.GithubUser != "bare" || p.AWSProfile != "aws" {
		t.Fatalf("got %+v", p)
	}
	if !reflect.DeepEqual(p.SecretKeys, []string{"A", "B", "C"}) {
		t.Fatalf("secret keys %v", p.SecretKeys)
	}
	if p.Name != "quoted" {
		t.Fatalf("name %q", p.Name)
	}
	c := load(t, "crlf")
	if c.GitName != "crlf" || c.GithubUser != "bare" || c.WorkDir != "/w" {
		t.Fatalf("crlf got %+v", c)
	}
}

func TestLoadHomeExpansion(t *testing.T) {
	t.Setenv("HOME", "/h/me")
	for _, n := range []string{"home1", "home2", "home3"} {
		if got := load(t, n).WorkDir; got != "/h/me/c" {
			t.Errorf("%s: %q", n, got)
		}
	}
}

func TestLoadMultilineExtraEnvs(t *testing.T) {
	p := load(t, "extra")
	want := map[string]string{"A": "1", "B": "two words"}
	if !reflect.DeepEqual(p.ExtraEnvs, want) {
		t.Fatalf("got %v", p.ExtraEnvs)
	}
	if p.GitName != "x" {
		t.Fatalf("assignment after multiline value lost: %q", p.GitName)
	}
}

func TestLoadFallsBackToBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	t.Setenv("HOME", "/h/me")
	if got := load(t, "bash").WorkDir; got != "/h/me/x" {
		t.Fatalf("got %q", got)
	}
	// What ctx itself writes (printf %q): $'..' strings and backslash escapes.
	p := load(t, "printfq")
	if p.WorkDir != "/a b" || p.GitName != "two words" ||
		!reflect.DeepEqual(p.ExtraEnvs, map[string]string{"A": "1", "B": "two words"}) {
		t.Fatalf("got %+v", p)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(t.TempDir(), "nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err %v", err)
	}
}

func TestListSorted(t *testing.T) {
	d := t.TempDir()
	for _, f := range []string{"b.conf", "a.conf", "c.txt"} {
		if err := os.WriteFile(filepath.Join(d, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List(d)
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestDir(t *testing.T) {
	t.Setenv("CTX_DIR", "/x")
	if Dir() != "/x/profiles" {
		t.Fatal(Dir())
	}
	t.Setenv("CTX_DIR", "")
	t.Setenv("HOME", "/h")
	if Dir() != "/h/.ctx/profiles" {
		t.Fatal(Dir())
	}
}
