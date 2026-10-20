package cli

import (
	"os"
	"path/filepath"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// Remove runs `ctx remove <profile>`, mirroring cmd_remove in lib/cmd_ops.sh,
// and returns the exit code. Like Bash it looks only at the first argument.
// Declining the confirmation exits 1 ("Aborted."); Ctrl+C or EOF exits 130.
// Either way nothing has been changed by then.
func Remove(t tui.Term, args []string) int {
	u := termUI(t)
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return u.die("Usage: ctx remove <profile>")
	}
	if !profileExists(name) {
		return u.die("Profile '" + name + "' not found.")
	}
	p, err := profile.Load(profile.Dir(), name)
	if err != nil {
		return u.die(err.Error())
	}

	u.bold("\n  Remove profile: " + name + "\n")
	u.say("  Git    : " + p.GitName + " <" + p.GitEmail + ">")
	u.say("  Dir    : " + orDash(p.WorkDir))
	u.say("  GitHub : " + orDash(p.GithubUser))
	u.say("")
	u.warn("This removes the profile config, git identity file, SSH host block, and stored secrets.")
	u.warn("Your code, SSH key files, and mise.toml are NOT deleted.")
	u.say("")

	yes, err := t.Prompter().AskYN("Remove profile '"+name+"'?", false)
	if err != nil {
		return t.Cancelled()
	}
	if !yes {
		return u.die("Aborted.")
	}

	// Stored secrets (a provider that is unavailable is skipped, as in Bash).
	if len(p.SecretKeys) > 0 {
		if st, err := secret.Open(secret.EffectiveProvider()); err == nil {
			for _, key := range p.SecretKeys {
				_ = st.Delete(name, key)
			}
		}
		u.success("Profile secrets removed")
	}
	_ = os.RemoveAll(filepath.Dir(profile.Dir()) + "/secrets/" + name)

	// SSH host block from ctx_config (not from ~/.ssh/config).
	_ = sshRemoveHost("github-" + name)
	u.success("SSH host removed from ctx_config")

	// Git identity file + gitconfig include.
	identity := os.Getenv("HOME") + "/.config/git/ctx-" + name
	_ = gitconfigRemoveInclude(identity, u.warnErr)
	_ = os.Remove(identity)
	u.success("Git identity removed")

	if cfg, err := ctxconfig.Load(); err == nil && cfg.Active == name {
		_ = ctxconfig.ClearActivation()
		u.dim("Active profile was removed — run: eval \"$(ctx deactivate)\" to clear shell env (or open a new terminal).")
	}

	_ = os.Remove(profile.Dir() + "/" + name + ".conf")
	u.success("Profile '" + name + "' removed.")
	u.say("")
	return 0
}
