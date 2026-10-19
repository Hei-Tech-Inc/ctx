package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
)

// expandLeadingTilde mirrors ${s/#\~/$HOME}: a leading "~" becomes $HOME.
func expandLeadingTilde(s string) string {
	if strings.HasPrefix(s, "~") {
		return os.Getenv("HOME") + s[1:]
	}
	return s
}

// relDepthUnder mirrors ctx_rel_path_depth_under: 0 for an exact match, the
// number of path segments below root, or -1 when p is not under root.
func relDepthUnder(p, r string) int {
	p = strings.TrimSuffix(expandLeadingTilde(p), "/")
	r = strings.TrimSuffix(expandLeadingTilde(r), "/")
	if p == r {
		return 0
	}
	if !strings.HasPrefix(p, r+"/") {
		return -1
	}
	x := p[len(r)+1:]
	if x == "" {
		return 0
	}
	return len(strings.Split(x, "/"))
}

// WorkdirPrompt ports cmd_workdir_prompt: it prints work_dir='WORK_DIR=<root>'
// when pwd is within the configured depth of the resolved WORK_DIR, or under a
// prompt_extra_paths entry, and nothing otherwise.
func WorkdirPrompt(w io.Writer, pwd string) error {
	initDirs()
	if pwd == "" {
		pwd = os.Getenv("HOME")
	}
	_, root, err := resolve.Path(pwd, profile.Dir())
	if err != nil {
		return nil // Bash returns silently when resolution fails
	}
	cfg, err := ctxconfig.Load()
	if err != nil {
		return err
	}
	show := false
	if root != "" {
		if d := relDepthUnder(pwd, root); d >= 0 && d <= cfg.PromptWorkdirMaxDepth {
			show = true
		}
	}
	if !show && cfg.PromptExtraPaths != "" {
		// Bash feeds `printf '%s' "$pex" | tr ':' '\n'` to `while read -r`, and read
		// drops a final line that has no trailing newline, so the last piece never
		// counts (a trailing ":" makes it an empty, ignored piece). Kept for parity.
		pieces := strings.Split(cfg.PromptExtraPaths, ":")
		for _, piece := range pieces[:len(pieces)-1] {
			if piece == "" {
				continue
			}
			piece = strings.TrimSuffix(expandLeadingTilde(piece), "/")
			if piece != "" && (pwd == piece || strings.HasPrefix(pwd, piece+"/")) {
				show, root = true, piece
				break
			}
		}
	}
	if show && root != "" {
		_, err = fmt.Fprintf(w, "work_dir='WORK_DIR=%s'\n", root)
		return err
	}
	return nil
}
