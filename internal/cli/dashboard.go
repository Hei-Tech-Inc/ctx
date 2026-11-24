package cli

import (
	"fmt"
	"io"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// Dashboard groups each profile's identity, workspace and integrations together.
func Dashboard(w io.Writer) int {
	initDirs()
	cfg, err := ctxconfig.Load()
	if err != nil {
		fmt.Fprintln(w, err)
		return 1
	}
	dir := profile.Dir()
	names, err := profile.List(dir)
	if err != nil {
		fmt.Fprintln(w, err)
		return 1
	}
	fmt.Fprintln(w, "PROFILE DASHBOARD")
	if len(names) == 0 {
		fmt.Fprintln(w, "No profiles yet. Run: ctx setup")
		return 0
	}
	for _, name := range names {
		p, err := profile.Load(dir, name)
		if err != nil {
			fmt.Fprintln(w, err)
			return 1
		}
		marker := ""
		if name == cfg.Active {
			marker = "  [active]"
		}
		fmt.Fprintf(w, "\n%s%s\n", name, marker)
		fmt.Fprintf(w, "  Workspace   %s\n  Git         %s <%s>\n  GitHub      %s\n  SSH key     %s\n", orDash(p.WorkDir), orDash(p.GitName), orDash(p.GitEmail), orDash(p.GithubUser), orDash(p.SSHKeyPath))
		fmt.Fprintf(w, "  AWS         %s\n  Azure       %s\n  GCP         %s\n  Kubernetes  %s\n", orDash(p.AWSProfile), orDash(p.AzureSubscription), orDash(p.GCPProject), orDash(p.KubeContext))
	}
	return 0
}
