package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"version"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if out.String() != "ctx-core dev\n" {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"bogus"}, &out, &errOut)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage: ctx-core") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestNoArgsIsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func writeProfile(t *testing.T, ctxDir, name, workDir string) {
	t.Helper()
	dir := filepath.Join(ctxDir, "profiles")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "PROFILE_NAME=\"" + name + "\"\nWORK_DIR=\"" + workDir + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, name+".conf"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeInitCommand(t *testing.T) {
	ctxDir := t.TempDir()
	t.Setenv("CTX_DIR", ctxDir)
	work := filepath.Join(t.TempDir(), "acme-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	work, err := filepath.EvalSymlinks(work) // macOS /var -> /private/var
	if err != nil {
		t.Fatal(err)
	}
	writeProfile(t, ctxDir, "acme", work)

	var out, errOut bytes.Buffer
	if code := run([]string{"knowledge", "init", "acme"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	root := filepath.Join(ctxDir, "knowledge", "acme")
	for _, f := range []string{"clusters.yaml", "environments.yaml", "services.yaml"} {
		p := filepath.Join(root, f)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
		if !strings.Contains(out.String(), p) {
			t.Errorf("stdout lacks %s: %q", p, out.String())
		}
	}

	// no profile arg: resolve from cwd
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	out.Reset()
	errOut.Reset()
	if code := run([]string{"knowledge", "init"}, &out, &errOut); code != 0 {
		t.Fatalf("resolve exit = %d, stderr = %q", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, "clusters.yaml")); err != nil {
		t.Fatalf("not created via cwd: %v", err)
	}
}

func TestKnowledgeInitErrors(t *testing.T) {
	ctxDir := t.TempDir()
	t.Setenv("CTX_DIR", ctxDir)
	writeProfile(t, ctxDir, "acme", filepath.Join(ctxDir, "nowhere"))
	var out, errOut bytes.Buffer
	if code := run([]string{"knowledge", "init", "ghost"}, &out, &errOut); code != 1 {
		t.Fatalf("unknown profile exit = %d", code)
	}
	if code := run([]string{"knowledge", "init", "../evil"}, &out, &errOut); code != 1 {
		t.Fatalf("bad profile exit = %d", code)
	}
	if code := run([]string{"knowledge"}, &out, &errOut); code != 2 {
		t.Fatalf("usage exit = %d", code)
	}
	if code := run([]string{"knowledge", "bogus"}, &out, &errOut); code != 2 {
		t.Fatalf("bogus exit = %d", code)
	}
	t.Chdir(t.TempDir())
	if code := run([]string{"knowledge", "init"}, &out, &errOut); code != 1 {
		t.Fatalf("no match exit = %d", code)
	}
}
