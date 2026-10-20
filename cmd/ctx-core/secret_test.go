package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// secretFixture is a CTX_DIR with profile p (two file-provider secrets) and
// profile empty (none).
func secretFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CTX_DIR", dir)
	t.Setenv("HOME", dir)
	t.Setenv("CTX_SECRET_PROVIDER", "file")
	for _, n := range []string{"p", "empty"} {
		writeProfile(t, dir, n, dir)
	}
	for k, v := range map[string]string{"TOKEN": "s3cret\n", "A_B": "x y"} {
		path := filepath.Join(dir, "secrets", "p", k)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func bashCtx(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	bin, err := filepath.Abs("../../bin/ctx")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "CTX_CORE=off")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

func TestSecretCLIMatchesBash(t *testing.T) {
	secretFixture(t)
	for _, args := range [][]string{
		{"get", "p", "TOKEN"},
		{"get", "p", "MISSING"},
		{"get", "nope", "TOKEN"},
		{"get", "p"},
		{"get"},
		{"list", "p"},
		{"list", "empty"},
		{"list", "nope"},
		{"list"},
		{"delete", "p", "A_B"},
		{"delete", "p", "A_B"}, // already gone
		{"delete", "p"},
		{"list", "p"},
	} {
		wantOut, wantErr, wantCode := bashCtx(t, append([]string{"secret"}, args...)...)
		var out, errb bytes.Buffer
		code := run(append([]string{"secret"}, args...), &out, &errb)
		if out.String() != wantOut || errb.String() != wantErr || code != wantCode {
			t.Errorf("secret %v:\n go   exit %d out %q err %q\n bash exit %d out %q err %q",
				args, code, out.String(), errb.String(), wantCode, wantOut, wantErr)
		}
	}
}

func TestSecretImplements(t *testing.T) {
	for sub, want := range map[string]bool{"get": true, "list": true, "delete": true, "set": true, "migrate": true, "": false, "bogus": false} {
		if got := implements("secret", []string{sub}); got != want {
			t.Errorf("implements secret %q = %v, want %v", sub, got, want)
		}
	}
}
