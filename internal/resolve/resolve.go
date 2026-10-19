// Package resolve maps a directory to a ctx profile. It ports
// ctx_resolve_path_with_root (lib/core.sh) without spawning git.
package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

func under(pwd, dir string) bool {
	return pwd == dir || strings.HasPrefix(pwd, dir+"/")
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		return os.Getenv("HOME") + p[1:]
	}
	return p
}

// workDir returns the profile's expanded WORK_DIR without one trailing slash (like Bash; "/" is kept) ("" if unset or unloadable).
func workDir(dir, name string) string {
	p, err := profile.Load(dir, name)
	if err != nil {
		return ""
	}
	wd := expandTilde(p.WorkDir)
	if wd != "/" {
		wd = strings.TrimSuffix(wd, "/")
	}
	return wd
}

// Path resolves pwd to a profile in profilesDir: longest WORK_DIR prefix, then the
// sibling rule, then the nearest .ctx "profile=" override up to the repo root.
// An empty name with nil error means no match.
func Path(pwd, profilesDir string) (name, wd string, err error) {
	pwd = expandTilde(pwd)
	names, err := profile.List(profilesDir)
	if err != nil {
		return "", "", fmt.Errorf("profiles dir: %w", err)
	}
	best, bestWD := "", ""
	for _, n := range names {
		w := workDir(profilesDir, n)
		if w == "" || !under(pwd, w) {
			continue
		}
		if len(w) > len(bestWD) {
			best, bestWD = n, w
		}
	}
	if best == "" {
		return "", "", nil
	}

	if strings.HasPrefix(pwd, bestWD+"/") {
		rel := pwd[len(bestWD)+1:]
		first, _, _ := strings.Cut(rel, "/")
		if _, serr := os.Stat(filepath.Join(profilesDir, first+".conf")); first != "" && serr == nil {
			wdc := workDir(profilesDir, first)
			if wdc == "" {
				wdc = bestWD + "/" + first
			}
			if fi, derr := os.Stat(wdc); derr == nil && fi.IsDir() && under(pwd, wdc) && len(wdc) > len(bestWD) {
				best, bestWD = first, wdc
			}
		}
	}

	if fi, derr := os.Stat(pwd); derr == nil && fi.IsDir() {
		if ov := ctxOverride(pwd, profilesDir); ov != "" {
			best = ov
		}
	}

	if w := workDir(profilesDir, best); w != "" {
		bestWD = w
	}
	return best, bestWD, nil
}

// ctxOverride walks from pwd up to the nearest directory containing .git and returns
// the first existing profile named by a .ctx file on the way.
func ctxOverride(pwd, profilesDir string) string {
	root := ""
	for d := strings.TrimRight(pwd, "/"); d != ""; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			root = d
			break
		}
		parent := d[:max(strings.LastIndex(d, "/"), 0)]
		if parent == d {
			break
		}
		d = parent
	}
	if root == "" {
		return ""
	}
	for d := strings.TrimRight(pwd, "/"); ; {
		if data, err := os.ReadFile(filepath.Join(d, ".ctx")); err == nil {
			ov := ""
			for _, line := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(line, "profile="); ok {
					ov = v
				}
			}
			ov = strings.TrimSpace(strings.ReplaceAll(ov, "\r", ""))
			if ov != "" {
				if _, err := os.Stat(filepath.Join(profilesDir, ov+".conf")); err == nil {
					return ov
				}
			}
		}
		if d == root {
			break
		}
		i := strings.LastIndex(d, "/")
		if i <= 0 {
			break
		}
		d = d[:i]
	}
	return ""
}
