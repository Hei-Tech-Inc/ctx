package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"time"
)

const installUsage = "usage: ctx-core mcp install <claude|cursor>\n"

const claudeAddCmd = "claude mcp add --scope user ctx -- ctx mcp"

// runMCPInstall registers `ctx mcp` with an MCP host.
func runMCPInstall(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, installUsage)
		return 2
	}
	switch args[0] {
	case "claude":
		return installClaude(stdout, stderr)
	case "cursor":
		return installCursor(stdout, stderr)
	default:
		fmt.Fprint(stderr, installUsage)
		return 2
	}
}

func installClaude(stdout, stderr io.Writer) int {
	path, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(stderr, "ctx-core: claude is not on PATH. Run this yourself:\n  %s\n", claudeAddCmd)
		return 1
	}
	cmd := exec.Command(path, "mcp", "add", "--scope", "user", "ctx", "--", "ctx", "mcp")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "ctx-core: %s failed: %v\n", claudeAddCmd, err)
		return 1
	}
	return 0
}

func installCursor(stdout, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "ctx-core: %v\n", err)
		return 1
	}
	path := filepath.Join(home, ".cursor", "mcp.json")
	changed, err := mergeCursorConfig(path, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "ctx-core: %v\n", err)
		return 1
	}
	if changed {
		fmt.Fprintf(stdout, "registered ctx in %s\n", path)
	} else {
		fmt.Fprintf(stdout, "ctx already registered in %s; nothing changed\n", path)
	}
	return 0
}

// mergeCursorConfig adds the ctx server to a Cursor mcp.json, keeping every
// other key. An existing file is backed up first; an unchanged file is left alone.
func mergeCursorConfig(path string, now time.Time) (bool, error) {
	want := map[string]any{"command": "ctx", "args": []any{"mcp"}}
	doc := map[string]any{}
	mode := os.FileMode(0o600)
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if st, serr := os.Stat(path); serr == nil {
			mode = st.Mode().Perm()
		}
		if len(bytes.TrimSpace(existing)) > 0 {
			if err := json.Unmarshal(existing, &doc); err != nil {
				return false, fmt.Errorf("%s is not valid JSON, leaving it alone: %w", path, err)
			}
		}
	case errors.Is(err, os.ErrNotExist):
		existing = nil
	default:
		return false, err
	}
	servers := map[string]any{}
	if raw, ok := doc["mcpServers"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return false, fmt.Errorf("%s: mcpServers is not an object, leaving it alone", path)
		}
		servers = m
	}
	if reflect.DeepEqual(servers["ctx"], want) {
		return false, nil
	}
	servers["ctx"] = want
	doc["mcpServers"] = servers
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if existing != nil {
		backup := path + ".ctx-backup-" + now.Format("20060102-150405")
		if err := os.WriteFile(backup, existing, mode); err != nil {
			return false, fmt.Errorf("backup: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp.json.ctx-*")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}
