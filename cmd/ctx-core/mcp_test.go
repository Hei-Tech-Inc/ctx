package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ctxCoreBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ctx-core-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctxCoreBin = filepath.Join(dir, "ctx-core")
	if out, err := exec.Command("go", "build", "-o", ctxCoreBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build ctx-core: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestMcpCommandStdio(t *testing.T) {
	ctxDir := t.TempDir()
	writeProfile(t, ctxDir, "acme", filepath.Join(ctxDir, "nowhere"))

	cmd := exec.Command(ctxCoreBin, "mcp", "--profile", "acme")
	cmd.Env = append(os.Environ(), "CTX_DIR="+ctxDir)
	cmd.Dir = t.TempDir()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	for _, msg := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	} {
		if _, err := io.WriteString(stdin, msg+"\n"); err != nil {
			t.Fatal(err)
		}
	}

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("server exited; stderr: %s", stderr.String())
			}
			var resp struct {
				ID     int `json:"id"`
				Result struct {
					Tools []struct{ Name string } `json:"tools"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(line), &resp) != nil || resp.ID != 2 {
				continue
			}
			names := map[string]bool{}
			for _, tool := range resp.Result.Tools {
				names[tool.Name] = true
			}
			if !names["client_context"] || !names["logs"] {
				t.Fatalf("tools/list = %s", line)
			}
			return
		case <-deadline:
			t.Fatalf("no tools/list response; stderr: %s", stderr.String())
		}
	}
}

func TestMcpCommandUsage(t *testing.T) {
	for _, args := range [][]string{{"mcp", "--profile"}, {"mcp", "--bogus"}, {"mcp", "--profile", "a", "b"}} {
		var out, errOut strings.Builder
		if code := run(args, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: ctx-core mcp") {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut.String())
		}
	}
}
