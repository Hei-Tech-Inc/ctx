package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
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

// Card renders lines in a rounded border of ANSI-256 color fg, like gum style
// --border rounded --border-foreground <fg> --padding "0 2" --margin "1 0"
// with one argument per line. It is not newline-terminated.
func Card(fg int, lines []string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(strconv.Itoa(fg))).
		Padding(0, 2).
		Margin(1, 0).
		Render(strings.Join(lines, "\n"))
}

// Faint renders s dimmed, like Bash's dim inside a gum argument.
func Faint(s string) string { return lipgloss.NewStyle().Faint(true).Render(s) }

// hr is lib/core.sh's hr: a dim 40-character rule.
func hr(t Term) string {
	return EchoE(t.c(dim) + strings.Repeat("─", 40) + t.c(reset))
}

// Step renders ctx_setup_step's banner for the caller to write to t.Out: the
// rule, the "Step n / total — title" line, the dim why line when why is not
// empty, and a blank line. Plain is Bash's bytes with HAS_GUM=false; rich puts
// the title in gum's normal border (color 240, padding "0 1"), bold.
func Step(t Term, n, total int, title, why string) string {
	head := "Step " + strconv.Itoa(n) + " / " + strconv.Itoa(total) + " — " + title
	var b strings.Builder
	b.WriteString(hr(t))
	if t.Rich {
		b.WriteString(lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1).
			Render(lipgloss.NewStyle().Bold(true).Render(head)) + "\n")
	} else {
		b.WriteString(EchoE(t.c(bold) + "  " + head + t.c(reset)))
	}
	if why != "" {
		b.WriteString(EchoE(t.c(dim) + "  " + why + t.c(reset)))
	}
	b.WriteString("\n")
	return b.String()
}

// ReviewTable renders setup's review rows (label, value), newline-terminated.
// Plain is ctx_setup_review_confirm's printf '  %-16s %s\n', padding labels by
// bytes as bash's printf does. Rich is gum table --print -c "Setting,Value"
// -w 20,58 --border rounded. Callers pass the labels for the mode, which
// differ in Bash.
func ReviewTable(t Term, rows [][2]string) string {
	var b strings.Builder
	if !t.Rich {
		for _, r := range rows {
			b.WriteString("  " + r[0])
			if pad := 16 - len(r[0]); pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
			b.WriteString(" " + r[1] + "\n")
		}
		return b.String()
	}
	data := make([][]string, len(rows))
	for i, r := range rows {
		data[i] = []string{r[0], r[1]}
	}
	widths := [2]int{20, 58}
	tb := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers("Setting", "Value").
		Rows(data...).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().Width(widths[col]).Padding(0, 1)
			if row == table.HeaderRow {
				s = s.Bold(true).Foreground(lipgloss.Color(strconv.Itoa(accent)))
			}
			return s
		})
	return tb.String() + "\n"
}
