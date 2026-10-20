package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/gitident"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

const useUsage = "Usage: ctx use <profile> [--eval bash|fish]"

// profileVars are the variables load_profile unsets before sourcing a profile,
// so tools ctx use runs never inherit them from the caller.
var profileVars = []string{
	"PROFILE_NAME", "GIT_NAME", "GIT_EMAIL", "WORK_DIR", "GITHUB_USER", "SSH_KEY_PATH",
	"AWS_PROFILE_NAME", "AZURE_SUBSCRIPTION", "AZURE_TENANT", "GCP_PROJECT", "GCP_ACCOUNT",
	"KUBE_CONTEXT", "SECRET_KEYS", "EXTRA_ENVS", "SIGNING_FORMAT", "SIGNING_KEY",
}

// Use runs quiet `ctx use <profile> [--eval bash|fish]` (CTX_QUIET=1),
// mirroring cmd_use and _ctx_use_apply in lib/cmd_ops.sh, and returns the
// exit code: the number of failed switches. With --eval, stdout carries only
// the exports and everything else goes to stderr. The non-quiet output (and
// the "not applied to this shell" warning, which quiet suppresses) stays in Bash.
func Use(stdout, stderr io.Writer, args []string) int {
	colors := ttyColors(stdout)
	name, evalShell := "", ""
	usage := ui{out: stdout, err: stderr, colors: colors}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--eval":
			evalShell = "bash"
			if i+1 < len(args) && (args[i+1] == "bash" || args[i+1] == "zsh" || args[i+1] == "fish") {
				evalShell = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--eval="):
			evalShell = strings.TrimPrefix(a, "--eval=")
		case strings.HasPrefix(a, "-"):
			return usage.die(useUsage)
		default:
			if name != "" {
				return usage.die(useUsage)
			}
			name = a
		}
	}
	if name == "" {
		return usage.die(useUsage)
	}

	if evalShell == "" {
		a := &useApply{u: ui{out: stdout, err: stderr, colors: colors}, out: stdout}
		code, _ := a.run(name)
		return code
	}
	a := &useApply{u: ui{out: stderr, err: stderr, colors: colors}, out: stderr}
	code, ok := a.run(name)
	if !ok {
		return code
	}
	if evalShell == "fish" {
		_, _ = io.WriteString(stdout, FormatEnv(a.exports, "fish"))
	} else {
		var b strings.Builder
		for _, kv := range a.exports {
			fmt.Fprintf(&b, "export %s=%s\n", kv[0], bashQuote(kv[1]))
		}
		_, _ = io.WriteString(stdout, b.String())
	}
	return code
}

// useApply holds one quiet _ctx_use_apply run.
type useApply struct {
	u       ui
	out     io.Writer // where the tools' stdout goes (stderr under --eval)
	exports [][2]string
	vars    map[string]string // the profile's shell variables (exports overwrite them)
}

// export is _ctx_use_export: export into this process (so later tools see
// it, as in Bash) and record it for the caller's shell.
func (a *useApply) export(k, v string) {
	_ = os.Setenv(k, v)
	a.vars[k] = v
	a.exports = append(a.exports, [2]string{k, v})
}

// tool runs name with the caller's stdin; nil writers discard.
func (a *useApply) tool(stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	return cmd.Run()
}

// has is `command -v name`.
func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// upsert is _ctx_config_upsert_line, whose only reported failure is a multi-line value.
func (a *useApply) upsert(key, value string) {
	var ml *ctxconfig.ErrMultiLine
	if err := ctxconfig.Upsert(key, value); errors.As(err, &ml) {
		a.u.error(err.Error())
	}
}

// run is _ctx_use_apply with CTX_QUIET=1. ok is false when the profile could
// not be loaded (Bash's die: nothing is exported).
func (a *useApply) run(name string) (code int, ok bool) {
	u := a.u
	dir := profile.Dir()
	if !isRegular(filepath.Join(dir, name+".conf")) {
		return u.die(fmt.Sprintf("Profile '%s' not found. Run: ctx list", name)), false
	}
	p, err := profile.Load(dir, name)
	if err != nil {
		return u.die(err.Error()), false
	}
	for _, k := range profileVars {
		_ = os.Unsetenv(k)
	}
	a.vars = map[string]string{"WORK_DIR": p.WorkDir, "PROFILE_NAME": p.ProfileName}
	if a.vars["PROFILE_NAME"] == "" {
		a.vars["PROFILE_NAME"] = name // Bash stops on the unbound variable; keep going
	}

	errs := 0

	// 1. GitHub — the autoswitch hook sets GH_TOKEN per terminal instead.
	if p.GithubUser != "" && os.Getenv("CTX_AUTO_SWITCH") != "1" && has("gh") {
		if a.tool(a.out, nil, "gh", "auth", "switch", "-u", p.GithubUser) != nil {
			u.warn(fmt.Sprintf("gh switch failed for '%s' — run: gh auth login", p.GithubUser))
			errs++
		}
	}

	// 2. SSH key — add this profile's key; keep the user's own agent keys.
	if p.SSHKeyPath != "" && isRegular(p.SSHKeyPath) {
		if agentAddedKey() != p.SSHKeyPath {
			agentDropAddedKey()
		}
		if !agentHasKey(p.SSHKeyPath) {
			if a.tool(a.out, nil, "ssh-add", p.SSHKeyPath) == nil {
				a.upsert("ssh_agent_added", p.SSHKeyPath)
			} else {
				u.warn("SSH key not loaded into the agent — github-" + name + " still reads it from disk")
			}
		}
	} else {
		agentDropAddedKey()
	}
	gitident.SetRewrite(name, a.out, u.err)

	// 3. AWS
	if p.AWSProfile != "" {
		a.export("AWS_PROFILE", p.AWSProfile)
	}

	// 4. Azure
	if p.AzureSubscription != "" {
		if has("az") {
			if a.tool(nil, nil, "az", "account", "set", "--subscription", p.AzureSubscription) != nil {
				u.warn("Azure subscription switch failed")
				errs++
			}
		} else {
			a.export("AZURE_SUBSCRIPTION", p.AzureSubscription)
			if p.AzureTenant != "" {
				a.export("AZURE_TENANT", p.AzureTenant)
			}
		}
	}

	// 5. GCP
	if p.GCPProject != "" {
		if has("gcloud") {
			if a.tool(a.out, nil, "gcloud", "config", "set", "project", p.GCPProject, "--quiet") != nil {
				u.warn("GCP project switch failed")
			}
			if p.GCPAccount != "" {
				_ = a.tool(a.out, nil, "gcloud", "config", "set", "account", p.GCPAccount, "--quiet")
			}
		} else {
			a.export("GCLOUD_PROJECT", p.GCPProject)
		}
	}

	// 6. kubectl
	if p.KubeContext != "" && has("kubectl") {
		if a.tool(nil, nil, "kubectl", "config", "use-context", p.KubeContext) != nil {
			u.warn(fmt.Sprintf("kubectl context '%s' not found", p.KubeContext))
		}
	}

	// 7. Secrets → export to shell. `for key in $SECRET_KEYS` splits on IFS.
	if p.SecretKeysRaw != "" {
		label := "secret store"
		if runtime.GOOS == "darwin" {
			label = "Keychain"
		}
		isSep := func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }
		for _, key := range strings.FieldsFunc(p.SecretKeysRaw, isSep) {
			if !isValidEnvKey(key) {
				u.warn(fmt.Sprintf("Skipping invalid secret key name '%s'", key))
				continue
			}
			val := ""
			if st, err := secret.Open(secret.EffectiveProvider()); err == nil {
				val, _ = st.Get(a.vars["PROFILE_NAME"], key)
			}
			if val != "" {
				a.export(key, val)
			} else {
				u.warn(fmt.Sprintf("Secret '%s' missing (%s) — run: ctx secret set %s %s", key, label, a.vars["PROFILE_NAME"], key))
			}
		}
	}

	// 8. Reload mise/direnv if in the work dir.
	if wd := a.vars["WORK_DIR"]; wd != "" {
		inWork := strings.HasPrefix(currentPWD(), wd)
		if has("mise") && isRegular(wd+"/mise.toml") && inWork {
			_ = a.tool(a.out, nil, "mise", "hook-env")
		} else if has("direnv") && isRegular(wd+"/.envrc") && inWork {
			_ = a.tool(a.out, nil, "direnv", "reload")
		}
	}

	prevActive := ""
	if cfg, err := ctxconfig.Load(); err == nil {
		prevActive = cfg.Active
	}
	a.upsert("active", name)
	ctxconfig.Log("Switched to profile: " + name)
	a.export("CTX_ACTIVE_PROFILE", name)

	if os.Getenv("CTX_AUTO_SWITCH") == "1" {
		// The hook re-applying a profile already locked by hand keeps the lock.
		source := "auto"
		if cfg, err := ctxconfig.Load(); err == nil {
			source = cfg.ActiveSource
		}
		if prevActive != name || source != "manual" {
			_ = ctxconfig.SetAuto()
		}
	} else {
		pwd := currentPWD()
		if pwd == "" {
			pwd = os.Getenv("HOME")
		}
		_ = ctxconfig.SetManual(pwd)
	}
	return errs, true
}
