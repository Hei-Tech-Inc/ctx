package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// StatusJSON prints the `ctx --json status` envelope, matching cmd_status in lib/cmd_ops.sh.
// A missing active profile file is an error, like load_profile's die.
func StatusJSON(w io.Writer, version, pwd string) error {
	initDirs()
	cfg, err := ctxconfig.Load()
	if err != nil {
		return err
	}
	if cfg.Active == "" {
		_, err = fmt.Fprintf(w, `{"version":"%s","command":"status","active":null}`+"\n", jsonEscape(version))
		return err
	}
	p, err := profile.Load(profile.Dir(), cfg.Active)
	if err != nil {
		return fmt.Errorf("Profile '%s' not found. Run: ctx list", cfg.Active) //nolint:staticcheck // user-facing message copied from Bash
	}
	note := "auto"
	if cfg.ActiveSource == "manual" {
		if cfg.ManualPWD != "" && pwd == cfg.ManualPWD {
			note = "manual_locked_at_pwd"
		} else {
			note = "manual_cleared_auto_applies"
		}
	}
	_, err = fmt.Fprintf(w, `{"version":"%s","command":"status","active":"%s","activation":"%s","activation_note":"%s","git_name":"%s","git_email":"%s","work_dir":"%s","github_user":"%s","ssh_key_path":"%s","aws_profile":"%s","azure_subscription":"%s","gcp_project":"%s","kube_context":"%s","secret_keys":"%s","ctx_active_profile":"%s"}`+"\n",
		jsonEscape(version), jsonEscape(cfg.Active), jsonEscape(cfg.ActiveSource), note,
		jsonEscape(p.GitName), jsonEscape(p.GitEmail), jsonEscape(p.WorkDir), jsonEscape(p.GithubUser),
		jsonEscape(p.SSHKeyPath), jsonEscape(p.AWSProfile), jsonEscape(p.AzureSubscription),
		jsonEscape(p.GCPProject), jsonEscape(p.KubeContext), jsonEscape(p.SecretKeysRaw),
		jsonEscape(os.Getenv("CTX_ACTIVE_PROFILE")))
	return err
}
