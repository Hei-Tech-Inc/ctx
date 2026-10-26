package cli

import (
	_ "embed"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/shquote"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

const activateUsage = "Usage: ctx activate bash|zsh|fish"

// The shell glue `ctx activate` prints; @CTX_CORE@ is the quoted path of
// ctx-core.
var (
	//go:embed text/activate.bash
	activateBash string
	//go:embed text/activate.zsh
	activateZsh string
	//go:embed text/activate.fish
	activateFish string
)

// Activate runs `ctx activate bash|zsh|fish`: it prints the shell glue to
// eval in an interactive shell (`eval "$(ctx activate zsh)"`, fish:
// `ctx activate fish | source`). The glue
//
//   - defines the ctx wrapper function: use and deactivate are run through
//     ctx-core with --eval and applied to the shell, everything else goes to
//     the ctx binary (bin/ctx) unchanged;
//   - defines _ctx_hook, which runs `ctx-core hook-env <shell>` (see HookEnv)
//     only when $PWD changed, this shell's stamp is missing, or the config or
//     the profiles dir is newer than the stamp; an unchanged prompt runs
//     builtins only;
//   - registers the hook (zsh chpwd and precmd, bash PROMPT_COMMAND or
//     bash-preexec's precmd_functions, fish PWD and fish_postexec), removing
//     the legacy hook's registrations, idempotently;
//   - runs the hook once.
//
// The path of this ctx-core is baked in; when it is gone the hook does
// nothing and the wrapper falls back to the ctx binary.
func Activate(t tui.Term, args []string) int {
	u := ui{out: t.Err, err: t.Err}
	if len(args) != 1 || (args[0] != "bash" && args[0] != "zsh" && args[0] != "fish") {
		u.error(activateUsage)
		return 2
	}
	core, err := activateCorePath()
	if err != nil {
		return u.die("ctx activate: cannot find ctx-core: " + err.Error())
	}
	_, _ = io.WriteString(t.Out, activateScript(args[0], core))
	return 0
}

// activateScript is the glue for shell with core baked in.
func activateScript(shell, core string) string {
	switch shell {
	case "fish":
		return strings.ReplaceAll(activateFish, "@CTX_CORE@", fishQuote(core))
	case "zsh":
		return strings.ReplaceAll(activateZsh, "@CTX_CORE@", shquote.Quote(core))
	default:
		return strings.ReplaceAll(activateBash, "@CTX_CORE@", shquote.Quote(core))
	}
}

// activateCorePath is the absolute path ctx-core was started by: argv[0]
// (through PATH for a bare name) when it is this executable, so a symlink
// (a package manager's bin link) stays a symlink and survives upgrades that
// replace its target; else os.Executable.
func activateCorePath() (string, error) {
	exe, exeErr := os.Executable()
	if p := os.Args[0]; p != "" {
		if !strings.Contains(p, "/") {
			p, _ = exec.LookPath(p)
		}
		if p != "" {
			if abs, err := filepath.Abs(p); err == nil && exeErr == nil && sameFile(abs, exe) {
				return abs, nil
			}
		}
	}
	if exeErr != nil {
		return "", exeErr
	}
	if exe == "" {
		return "", errors.New("empty executable path")
	}
	return filepath.Abs(exe)
}
