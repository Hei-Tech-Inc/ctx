package testutil

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	stampRE = regexp.MustCompile(`\b\d{8}-\d{6}\b`)
	isoRE   = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ`)
	logRE   = regexp.MustCompile(`\[\d{4}-\d\d-\d\d \d\d:\d\d:\d\d\] `)
)

// Snapshot describes every file and directory under root as
// "path" -> "mode content", so two runs of the same operation (Go and Bash)
// can be compared. Root paths, backup stamps, ISO timestamps and ctx.log time
// prefixes are normalized, and config.lock (a Go-only file) is skipped.
func Snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	raw := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." || filepath.Base(path) == "config.lock" {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		body := ""
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			body = string(b)
		} else if info.Mode()&os.ModeSymlink != 0 {
			body, _ = os.Readlink(path)
		}
		raw[rel] = fmt.Sprintf("%v %s", info.Mode(), body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Number the distinct backup stamps in order of appearance.
	var stamps []string
	for k := range raw {
		for _, s := range stampRE.FindAllString(k, -1) {
			stamps = append(stamps, s)
		}
	}
	sort.Strings(stamps)
	idx := map[string]string{}
	for _, s := range stamps {
		if _, ok := idx[s]; !ok {
			idx[s] = fmt.Sprintf("STAMP%d", len(idx)+1)
		}
	}
	norm := func(s string) string {
		s = strings.ReplaceAll(s, root, "<ROOT>")
		s = stampRE.ReplaceAllStringFunc(s, func(m string) string {
			if v, ok := idx[m]; ok {
				return v
			}
			return "STAMP?"
		})
		s = isoRE.ReplaceAllString(s, "<ISO>")
		return logRE.ReplaceAllString(s, "[<TIME>] ")
	}
	out := map[string]string{}
	for k, v := range raw {
		out[norm(k)] = norm(v)
	}
	return out
}

// DiffSnapshots returns a readable difference, "" when equal.
func DiffSnapshots(a, b map[string]string) string {
	var keys []string
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		av, aok := a[k]
		bv, bok := b[k]
		switch {
		case !aok:
			fmt.Fprintf(&sb, "only in second: %s (%q)\n", k, bv)
		case !bok:
			fmt.Fprintf(&sb, "only in first: %s (%q)\n", k, av)
		case av != bv:
			fmt.Fprintf(&sb, "differs: %s\n  first:  %q\n  second: %q\n", k, av, bv)
		}
	}
	return sb.String()
}

// FixedStampDate returns a directory holding a `date` that prints stamp for
// the backup format +%Y%m%d-%H%M%S and defers to the real date otherwise. Put
// it first on PATH so Bash backups share one directory like a frozen clock.
func FixedStampDate(t *testing.T, stamp string) string {
	t.Helper()
	real, err := exec.LookPath("date")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = '+%Y%m%d-%H%M%S' ]; then printf '%s\\n' '" + stamp + "'; exit 0; fi\n" +
		"exec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "date"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
