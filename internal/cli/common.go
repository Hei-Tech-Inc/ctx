// Package cli implements the ctx commands that ctx-core runs instead of Bash.
package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// jsonEscape mirrors ctx_json_escape in lib/core.sh: only backslash, double
// quote, newline, CR and tab are escaped; other bytes pass through.
func jsonEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(s)
}

// initDirs mirrors ctx_init_dirs: create the ctx dirs and config, chmod 700.
func initDirs() {
	dir := profile.Dir()
	root := filepath.Dir(dir)
	for _, d := range []string{root, dir, root + "/backups"} {
		_ = os.MkdirAll(d, 0o755)
	}
	if f, err := os.OpenFile(ctxconfig.Path(), os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Close()
	}
	_ = os.Chmod(root, 0o700)
	_ = os.Chmod(dir, 0o700)
}
