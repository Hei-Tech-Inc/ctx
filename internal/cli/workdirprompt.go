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

// PromptScope is the prompt decision shared by `ctx workdir-prompt` and the
// autoswitch hook (_ctx_prompt_update_exports). name and wd are what
// resolve.Path returned for pwd. It reports whether the prompt shows the
// work dir (pwd within prompt_workdir_max_depth of wd, or under a
// prompt_extra_paths entry), the profile for CTX_PROMPT_PROFILE (the resolved
// one, else the config's active profile) and the root for CTX_PROMPT_WORK_DIR.
func PromptScope(pwd string, cfg *ctxconfig.Config, name, wd string) (show bool, profileName, root string) {
	if name != "" && wd != "" {
		if d := relDepthUnder(pwd, wd); d >= 0 && d <= cfg.PromptWorkdirMaxDepth {
			show, root = true, wd
		}
	}
	if !show {
		// Every piece counts, the last included (R5; Bash used to drop it).
		for _, piece := range strings.Split(cfg.PromptExtraPaths, ":") {
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
	if !show {
		return false, "", ""
	}
	profileName = name
	if profileName == "" {
		profileName = cfg.Active
	}
	return true, profileName, root
}

// WorkdirPrompt ports cmd_workdir_prompt: it prints work_dir='WORK_DIR=<root>'
// when PromptScope says the prompt shows the work dir, and nothing otherwise.
func WorkdirPrompt(w io.Writer, pwd string) error {
	initDirs()
	if pwd == "" {
		pwd = os.Getenv("HOME")
	}
	name, root, err := resolve.Path(pwd, profile.Dir())
	if err != nil {
		return nil // Bash returns silently when resolution fails
	}
	cfg, err := ctxconfig.Load()
	if err != nil {
		return err
	}
	if show, _, r := PromptScope(pwd, cfg, name, root); show && r != "" {
		_, err = fmt.Fprintf(w, "work_dir='WORK_DIR=%s'\n", r)
		return err
	}
	return nil
}
