package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// Header renders a section title, newline-terminated, for the caller to write.
// Rich is gum style --border rounded --border-foreground 99 --padding "0 2"
// --margin "1 0" --bold; plain is Bash's bold "\n  <title>\n".
// Write rich output through a lipgloss/colorprofile writer (lipgloss.Fprint)
// so NO_COLOR and dumb terminals are downgraded.
func Header(t Term, title string) string {
	if !t.Rich {
		return t.c(bold) + "\n  " + title + "\n" + t.c(reset) + "\n"
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(strconv.Itoa(accent))).
		Padding(0, 2).
		Margin(1, 0).
		Bold(true).
		Render(title) + "\n"
}

// Box renders lines in a normal border, like gum style --border normal
// --padding "0 1" with one argument per line. It is newline-terminated.
func Box(lines []string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		Render(strings.Join(lines, "\n")) + "\n"
}

// Accent renders s in ANSI-256 foreground color fg.
func Accent(fg int, s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(strconv.Itoa(fg))).Render(s)
}
