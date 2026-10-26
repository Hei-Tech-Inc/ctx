package hookenv

import (
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
)

// expandLeadingTilde mirrors ${s/#\~/$HOME}: a leading "~" becomes home.
func expandLeadingTilde(s, home string) string {
	if strings.HasPrefix(s, "~") {
		return home + s[1:]
	}
	return s
}

// RelDepthUnder mirrors _ctx_rel_depth_under / ctx_rel_path_depth_under: 0
// for an exact match, the number of path segments below root, or -1 when p
// is not under root. A leading "~" in either is home.
func RelDepthUnder(p, r, home string) int {
	p = strings.TrimSuffix(expandLeadingTilde(p, home), "/")
	r = strings.TrimSuffix(expandLeadingTilde(r, home), "/")
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
// resolve.Path returned for pwd; home expands a leading "~". It reports
// whether the prompt shows the work dir (pwd within prompt_workdir_max_depth
// of wd, or under a prompt_extra_paths entry), the profile for
// CTX_PROMPT_PROFILE (the resolved one, else cfg.Active) and the root for
// CTX_PROMPT_WORK_DIR.
func PromptScope(pwd, home string, cfg *ctxconfig.Config, name, wd string) (show bool, profileName, root string) {
	if name != "" && wd != "" {
		if d := RelDepthUnder(pwd, wd, home); d >= 0 && d <= cfg.PromptWorkdirMaxDepth {
			show, root = true, wd
		}
	}
	if !show {
		// Every piece counts, the last included (R5; Bash used to drop it).
		for _, piece := range strings.Split(cfg.PromptExtraPaths, ":") {
			if piece == "" {
				continue
			}
			piece = strings.TrimSuffix(expandLeadingTilde(piece, home), "/")
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
