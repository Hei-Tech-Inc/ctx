package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, b)
	}
	return m
}

func TestInstallCursorMerges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mcp.json")
	orig := `{"mcpServers":{"other":{"command":"x","args":["a"]}},"extra":1}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"mcp", "install", "cursor"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	m := readJSON(t, path)
	servers := m["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("other server dropped: %v", servers)
	}
	c := servers["ctx"].(map[string]any)
	if c["command"] != "ctx" || len(c["args"].([]any)) != 1 || c["args"].([]any)[0] != "mcp" {
		t.Fatalf("ctx entry = %v", c)
	}
	if m["extra"] != float64(1) {
		t.Fatalf("unrelated key lost: %v", m)
	}
	backups, _ := filepath.Glob(path + ".ctx-backup-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != orig {
		t.Fatalf("backup content = %q", b)
	}

	before, _ := os.ReadFile(path)
	out.Reset()
	errOut.Reset()
	if code := run([]string{"mcp", "install", "cursor"}, &out, &errOut); code != 0 {
		t.Fatalf("second run exit %d: %s", code, errOut.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("second run changed the file")
	}
	if b, _ := filepath.Glob(path + ".ctx-backup-*"); len(b) != 1 {
		t.Fatalf("second run wrote another backup: %v", b)
	}
}

func TestInstallCursorCreatesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, errOut bytes.Buffer
	if code := run([]string{"mcp", "install", "cursor"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	m := readJSON(t, filepath.Join(home, ".cursor", "mcp.json"))
	if _, ok := m["mcpServers"].(map[string]any)["ctx"]; !ok {
		t.Fatalf("no ctx entry: %v", m)
	}
}

func TestInstallCursorRejectsInvalidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".cursor")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "mcp.json")
	_ = os.WriteFile(path, []byte("{not json"), 0o600)
	var out, errOut bytes.Buffer
	if code := run([]string{"mcp", "install", "cursor"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatalf("file modified: %q", b)
	}
}

func TestInstallClaudeMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, errOut bytes.Buffer
	code := run([]string{"mcp", "install", "claude"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	all := out.String() + errOut.String()
	if !strings.Contains(all, "claude mcp add --scope user ctx -- ctx mcp") {
		t.Fatalf("output = %q", all)
	}
}

func TestInstallClaudeRuns(t *testing.T) {
	bin := t.TempDir()
	rec := filepath.Join(bin, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + rec + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	var out, errOut bytes.Buffer
	if code := run([]string{"mcp", "install", "claude"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(rec)
	if got := strings.Join(strings.Fields(string(b)), " "); got != "mcp add --scope user ctx -- ctx mcp" {
		t.Fatalf("argv = %q", got)
	}
}

func TestInstallUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"mcp", "install", "vim"}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestInstallCursorNullFileIsEmptyObject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"mcp", "install", "cursor"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	m := readJSON(t, filepath.Join(dir, "mcp.json"))
	if _, ok := m["mcpServers"].(map[string]any)["ctx"]; !ok {
		t.Fatalf("no ctx entry: %v", m)
	}
}
