package setup

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

const helpExampleURL = "https://raw.githubusercontent.com/Hei-Tech-Inc/ctx/main/examples/setup.noninteractive.conf.example"

// Help ports cmd_setup_help (lib/cmd_import.sh), the text of
// `ctx setup|import|add --help`. Plain mode is byte-identical to Bash's
// non-gum branch (bold section titles when t.Colors), which `ctx help setup`
// still prints from Bash; rich mode is the gum card (rounded border, color
// 99, padding 1 2, margin 1 0) with bold and faint titles. Both end with the
// trailing blank line.
func Help(t tui.Term) string {
	if t.Rich {
		bold := lipgloss.NewStyle().Bold(true)
		lines := []string{
			bold.Render("ctx setup — reference"),
			"",
			tui.Faint("Commands"),
			"  ctx setup  ·  ctx import  ·  ctx add   (same wizard)",
			"  ctx setup --help    ctx help setup   (this screen)",
			"",
			tui.Faint("Interactive"),
			"  • 8 steps ending in a review table; nothing is written until you confirm.",
			"  • Ctrl+C cancels where supported.",
			"",
			tui.Faint("Flags"),
			"  --scope clone|full   clone = SSH host + ctx clone only; add rest with --extend",
			"  --extend NAME        Add git / folder / cloud / secrets to an existing profile",
			"  --config FILE     KEY=value file (non-interactive)",
			"  -y  --yes         Skip final confirmation (typical with --config in CI)",
			"  ctx --dry-run setup …   Preview only (no profile files or SSH keys)",
			"",
			tui.Faint("Config keys (FILE)"),
			"  SETUP_SCOPE=clone|full   (clone: GIT_* optional; WORK_DIR optional; SSH required)",
			"  PROFILE_NAME  GIT_NAME  GIT_EMAIL",
			"  WORK_DIR=/abs/path   OR   WORK_MODE=current | path | new",
			"     path → EXISTING_PATH=…     new → WORK_SLUG=… (under ctx work-root)",
			"  IMPORT_MODE=auto | manual      SSH_MODE=generate | existing | skip",
			"  Optional: GITHUB_USER  AWS_PROFILE_NAME  AZURE_*  GCP_*  KUBE_CONTEXT",
			"  SIGNING_FORMAT=ssh | gpg | none   SIGNING_KEY=…  (ssh defaults to the profile's SSH key)",
			"  SECRETS_MODE=skip  (only batch value — then: ctx secret set <profile> KEY)",
			"  AUTO_CONFIRM=true   OVERWRITE=true  (when replacing an existing profile)",
			"",
			tui.Faint("Example"),
			"  Repo: examples/setup.noninteractive.conf.example",
			"  Raw:  " + helpExampleURL,
			"",
			tui.Faint("After setup"),
			"  ctx use <name>    ctx verify <name>    ctx doctor",
		}
		card := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(1, 2).
			Margin(1, 0).
			Render(strings.Join(lines, "\n"))
		return card + "\n\n"
	}

	b, r := "", ""
	if t.Colors {
		b, r = ansiBold, ansiReset
	}
	title := func(s string) string { return b + s + r + "\n" }
	return title("ctx setup — reference") +
		"\n" +
		"Commands:  ctx setup  |  ctx import  |  ctx add\n" +
		"           ctx setup --help    ctx help setup\n" +
		"\n" +
		title("Interactive") +
		"  • 8 steps with a review; nothing is written until you confirm.\n" +
		"\n" +
		title("Flags") +
		"  --scope clone|full   clone = SSH + ctx clone; extend later with --extend NAME\n" +
		"  --extend NAME        Add features to an existing profile (interactive)\n" +
		"  --config FILE     Non-interactive KEY=value file\n" +
		"  -y  --yes         Skip final confirmation\n" +
		"  ctx --dry-run setup …   Preview only\n" +
		"\n" +
		title("Config file keys") +
		"  SETUP_SCOPE=clone|full, PROFILE_NAME, GIT_NAME, GIT_EMAIL\n" +
		"  WORK_DIR=… or WORK_MODE=current|path|new (+ EXISTING_PATH or WORK_SLUG)\n" +
		"  IMPORT_MODE, SSH_MODE, optional cloud + GITHUB_USER\n" +
		"  SIGNING_FORMAT=ssh|gpg|none, SIGNING_KEY (ssh defaults to the profile's SSH key)\n" +
		"  SECRETS_MODE=skip, AUTO_CONFIRM, OVERWRITE\n" +
		"\n" +
		title("Example") +
		"  examples/setup.noninteractive.conf.example\n" +
		"  " + helpExampleURL + "\n" +
		"\n" +
		title("After setup") +
		"  ctx use <name>    ctx verify <name>    ctx doctor\n" +
		"\n"
}
