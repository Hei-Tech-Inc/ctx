package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/hookenv"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
)

// PromptScope is hookenv.PromptScope with $HOME for a leading "~".
func PromptScope(pwd string, cfg *ctxconfig.Config, name, wd string) (show bool, profileName, root string) {
	return hookenv.PromptScope(pwd, os.Getenv("HOME"), cfg, name, wd)
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
