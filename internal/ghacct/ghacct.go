// Package ghacct parses `gh auth status` output.
package ghacct

import "strings"

type entry struct {
	user   string
	active int // 0 unknown, 1 true, -1 false
}

func scan(out string) []entry {
	var es []entry
	cur := -1 // index in es of the current block's entry; -1 for a block that is not one
	for _, l := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "- Active account:") {
			if cur >= 0 {
				if strings.TrimSpace(strings.TrimPrefix(t, "- Active account:")) == "true" {
					es[cur].active = 1
				} else {
					es[cur].active = -1
				}
			}
			continue
		}
		if !strings.Contains(l, "Logged in to ") && !strings.Contains(l, "Failed to log in to ") {
			continue
		}
		cur = -1 // every login line starts a block
		f := strings.Fields(l)
		for i := 1; i+2 < len(f); i++ {
			if f[i] == "github.com" && f[i-1] == "to" && (f[i+1] == "account" || f[i+1] == "as") {
				// awk's pattern: "Logged in to github.com (account|as) ".
				if strings.Contains(l, "Logged in to github.com account ") || strings.Contains(l, "Logged in to github.com as ") {
					es = append(es, entry{user: f[i+2]})
					cur = len(es) - 1
				}
				break
			}
		}
	}
	return es
}

// Parse returns every logged-in github.com account in output order
// ("Logged in to github.com account USER" or the older "... as USER"; a
// "Failed to log in" line is not a login).
func Parse(statusOutput string) []string {
	var out []string
	for _, e := range scan(statusOutput) {
		out = append(out, e.user)
	}
	return out
}

// Active returns the active account: the one followed by "- Active account:
// true". Older gh versions print no such line; then the first account is the
// active one. "" when there is none.
func Active(statusOutput string) string {
	es := scan(statusOutput)
	known := false
	for _, e := range es {
		if e.active == 1 {
			return e.user
		}
		if e.active != 0 {
			known = true
		}
	}
	if known || len(es) == 0 {
		return ""
	}
	return es[0].user
}
