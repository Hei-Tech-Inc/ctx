package cli

import (
	"github.com/charmbracelet/colorprofile"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// termUI is the Bash info/success/warn/bold/dim helpers writing to t. In rich
// mode every stream goes through richWriter, so NO_COLOR and dumb terminals
// downgrade all of the output, not just the Charm-rendered parts.
func termUI(t tui.Term) ui {
	if t.Rich {
		return ui{out: richWriter(t.Out), err: richWriter(t.Err), colors: t.Colors}
	}
	return ui{out: t.Out, err: t.Err, colors: t.Colors}
}

// richWriter downgrades Charm output for NO_COLOR and dumb terminals when w is
// the real stdout; other writers (tests, pipes of tests) get the bytes as is.
func richWriter(w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok {
		return colorprofile.NewWriter(f, os.Environ())
	}
	return w
}

// tildeHome is ${s/$HOME/~}: the first occurrence of $HOME, anywhere in s,
// becomes a literal "~" (Bash keeps the tilde in a variable so every bash agrees).
func tildeHome(s string) string {
	home := os.Getenv("HOME")
	if home == "" {
		return s
	}
	return strings.Replace(s, home, "~", 1)
}

// orDash is ${v:-—}.
func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// ListText prints `ctx list` (text mode), mirroring cmd_list in lib/cmd_ops.sh.
// Bash does not use ctx_header here, so there is no rich variant.
func ListText(t tui.Term) int {
	u := termUI(t)
	initDirs()
	active := ""
	if cfg, err := ctxconfig.Load(); err == nil {
		active = cfg.Active
	}
	u.bold("\n  ctx profiles\n")

	dir := profile.Dir()
	names, _ := profile.List(dir)
	found := false
	for _, n := range names {
		p, err := profile.Load(dir, n)
		if err != nil {
			continue
		}
		found = true
		marker := ""
		if n == active {
			marker = " " + u.c(ansiGreen) + "← active" + u.c(ansiReset)
		}
		var tags []string
		add := func(cond bool, s string) {
			if cond {
				tags = append(tags, s)
			}
		}
		add(p.WorkDir != "", tildeHome(p.WorkDir))
		add(p.GithubUser != "", "gh:"+p.GithubUser)
		add(p.AWSProfile != "", "aws:"+p.AWSProfile)
		add(p.AzureSubscription != "", "azure")
		add(p.GCPProject != "", "gcp:"+p.GCPProject)
		add(p.KubeContext != "", "k8s:"+p.KubeContext)
		add(p.SecretKeysRaw != "", strconv.Itoa(len(strings.Fields(p.SecretKeysRaw)))+" secrets")

		u.line(u.out, "  "+u.c(ansiBold)+n+u.c(ansiReset)+marker)
		u.line(u.out, "  "+u.c(ansiDim)+orDash(p.GitName)+" <"+orDash(p.GitEmail)+">"+u.c(ansiReset))
		if len(tags) > 0 {
			u.line(u.out, "  "+u.c(ansiDim)+strings.Join(tags, " | ")+u.c(ansiReset))
		}
		u.say("")
	}
	if !found {
		u.warn("No profiles yet.")
		u.info("Run: ctx setup")
	}
	return 0
}
