package cli

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Hei-Tech-Inc/ctx/internal/setup"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// helpPlain is the text of cmd_help's non-gum branch (bin/ctx), with
// {{VERSION}}, {{BOLD}} and {{RESET}} where the heredoc expanded variables.
//
//go:embed text/help_plain.txt
var helpPlain string

// Version prints `ctx version <CTX_VERSION>`, the user-facing version line
// (`ctx-core version` keeps printing `ctx-core <ver>`). CTX_VERSION comes from
// the environment: bin/ctx exports it, and ctx-core's main fills it from the
// build version when it is unset.
func Version(w io.Writer) int {
	_, _ = fmt.Fprintf(w, "ctx version %s\n", os.Getenv("CTX_VERSION"))
	return 0
}

// Help ports cmd_help. `help setup|import|add|wizard` prints the setup
// reference; any other topic (or none) prints the overview. Plain mode is
// byte-identical to Bash's non-gum heredoc, bold escapes included on a terminal.
// Rich mode is a lipgloss card carrying the gum rendering's content.
func Help(t tui.Term, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "setup", "import", "add", "wizard":
			_, _ = io.WriteString(t.Out, setup.Help(t))
			return 0
		}
	}
	ver := os.Getenv("CTX_VERSION")
	if t.Rich {
		_, _ = io.WriteString(richWriter(t.Out), helpCard(ver)+"\n")
		return 0
	}
	b, r := "", ""
	if t.Colors {
		b, r = ansiBold, ansiReset
	}
	_, _ = io.WriteString(t.Out, strings.NewReplacer("{{VERSION}}", ver, "{{BOLD}}", b, "{{RESET}}", r).Replace(helpPlain))
	return 0
}

// helpCard is the gum help card: rounded border color 99, padding "1 3",
// margin "1 0".
func helpCard(ver string) string {
	title := func(s string) string { return tui.Faint(s) }
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("ctx v"+ver) + " — client context switcher",
		"",
		title("SETUP"),
		"  init             Check deps, install hooks",
		"  setup            Configure a new client profile (see: --config FILE)",
		"  import / add     Alias for setup",
		"",
		title("DAILY USE"),
		"  use  <name>      Activate a profile (--eval bash|fish: print exports for eval)",
		"  deactivate       Clear active profile (prints eval for your shell)",
		"  list (ls)        List all profiles",
		"  status (st)      Active profile + live checks",
		"  clone            git clone with GitHub host rewrite for this profile",
		"  workdir-prompt   Print work_dir='WORK_DIR=…' when cwd is in prompt scope (for PS1)",
		"  run [-p name] -- <cmd>  Run one command (e.g. an AI agent) with this folder's profile identity",
		"  env [-p name]    Print the profile's identity as exports (eval; --secrets, --fish)",
		"  agent-rules      Print or --write [file] AI agent instructions (AGENTS.md)",
		"  mcp              Run the ctx MCP server (stdio) for AI agents; --profile NAME",
		"  mcp install <claude|cursor>  Register the MCP server with Claude Code or Cursor",
		"  knowledge init [name]  Create per-client knowledge notes",
		"",
		title("SECRETS  (Keychain or ~/.ctx/secrets)"),
		"  secret set   <profile> <KEY>",
		"  secret get   <profile> <KEY>",
		"  secret list  <profile>",
		"  secret delete <profile> <KEY>",
		"",
		title("MAINTENANCE"),
		"  config [show]    Show ctx config values",
		"  config work-root Set default client directory root",
		"  config autoswitch-notify  on|off — [ctx] ←/→ lines (default off)",
		"  config export    Export portable config bundle (no secrets)",
		"  config import    Import portable config bundle",
		"  verify [name]    One profile: live ssh -T, gh match, paths, email",
		"  edit   <name>    Open profile in $EDITOR",
		"  remove <name>    Delete profile + secrets (rm, del)",
		"  undo             Restore last backup",
		"  install-hook     Install mise + ctx hooks into shell rc",
		"  upgrade [--check] Re-run installer, or check for newer version",
		"  uninstall [--purge] Remove ctx install; optional ~/.ctx wipe",
		"  doctor           Machine-wide: tools, hooks, SSH layout, profiles",
		"  guard [status|install|uninstall|check]  Block commits/pushes with the wrong profile identity",
		"  completion <zsh|bash|fish>  Print shell completion script",
		"  version          Print version (-v, --version)",
		"  help setup       Full setup wizard + --config reference",
		"",
		title("FLAGS"),
		"  --dry-run        Preview changes without writing files",
		"  --quiet, -q      Suppress output (used by auto-switch)",
		"  --no-gum         Force plain text prompts",
		"  --json           Machine-readable JSON (list, status)",
		"",
		title("SETUP FLAGS"),
		"  --scope clone|full   Clone-only profile or full wizard",
		"  --extend NAME        Add git/folder/cloud/secrets to existing profile",
		"  --config FILE    Non-interactive wizard (see examples/)",
		"  -y / --yes       With --config: skip final confirmation prompt",
		"  setup --help     Same as: ctx help setup",
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("99")).
		Padding(1, 3).
		Margin(1, 0).
		Render(strings.Join(lines, "\n"))
}
