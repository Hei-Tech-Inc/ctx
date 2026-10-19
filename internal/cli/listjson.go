package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// ListJSON prints the `ctx --json list` envelope, matching cmd_list in lib/cmd_ops.sh.
func ListJSON(w io.Writer, version string) error {
	initDirs()
	cfg, err := ctxconfig.Load()
	if err != nil {
		return err
	}
	dir := profile.Dir()
	names, _ := profile.List(dir) // a missing dir lists nothing, like the empty glob
	var b strings.Builder
	fmt.Fprintf(&b, `{"version":"%s","command":"list","profiles":[`, jsonEscape(version))
	sep := ""
	for _, n := range names {
		p, err := profile.Load(dir, n)
		if err != nil {
			continue
		}
		active := "false"
		if n == cfg.Active {
			active = "true"
		}
		fmt.Fprintf(&b, `%s{"name":"%s","active":%s,"git_name":"%s","git_email":"%s","work_dir":"%s","github_user":"%s","aws_profile":"%s","azure_subscription":"%s","gcp_project":"%s","kube_context":"%s","secret_keys":"%s"}`,
			sep, jsonEscape(n), active, jsonEscape(p.GitName), jsonEscape(p.GitEmail), jsonEscape(p.WorkDir),
			jsonEscape(p.GithubUser), jsonEscape(p.AWSProfile), jsonEscape(p.AzureSubscription),
			jsonEscape(p.GCPProject), jsonEscape(p.KubeContext), jsonEscape(p.SecretKeysRaw))
		sep = ","
	}
	b.WriteString("]}\n")
	_, err = io.WriteString(w, b.String())
	return err
}
