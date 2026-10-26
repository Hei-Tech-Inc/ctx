package cli

import (
	_ "embed"
	"io"
)

// The shell completion scripts of cmd_completion (bin/ctx), verbatim.
var (
	//go:embed text/completion.zsh
	completionZsh string
	//go:embed text/completion.bash
	completionBash string
	//go:embed text/completion.fish
	completionFish string
)

// Completion ports cmd_completion: it prints the script for args[0] (zsh when
// absent or empty); anything else is `✗ Unknown shell: …` on errw, exit 1.
// Only the first argument counts.
func Completion(w, errw io.Writer, args []string) int {
	shell := "zsh"
	if len(args) > 0 && args[0] != "" {
		shell = args[0]
	}
	switch shell {
	case "zsh":
		_, _ = io.WriteString(w, completionZsh)
	case "bash":
		_, _ = io.WriteString(w, completionBash)
	case "fish":
		_, _ = io.WriteString(w, completionFish)
	default:
		return ui{out: w, err: errw, colors: ttyColors(w)}.die("Unknown shell: " + shell + ". Supported: zsh, bash, fish")
	}
	return 0
}
