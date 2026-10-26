package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// Init runs `ctx init`, mirroring cmd_init in lib/cmd_ops.sh (R1). Arguments
// are ignored. It creates the ctx dirs, then:
//   - when mise, gum or gh is missing, asks "Install missing tools now?" and on
//     yes runs `bash <CTX_LIB>/../install.sh` (stdio passed through, its exit
//     status ignored, as Bash does) or, without that file, `brew install <tool>`
//     for each (output discarded; "installed", else "install manually");
//   - when the rc file for $SHELL (substring zsh, bash, fish; the last match
//     wins) lacks the mise activation or the autoswitch block, asks to install
//     the shell hooks and runs InstallHook on it; a refusal there is init's
//     exit code, as Bash's die ends both.
//
// --dry-run (CTX_DRY_RUN=1) asks nothing and runs nothing: it says what each
// yes would do (install-hook prints its own dry-run lines). Without CTX_LIB
// it refuses before doing anything. Ctrl+C or EOF at a prompt exits 130.
func Init(t tui.Term, args []string) int {
	defer OnInterrupt(t, nil)()
	u := termUI(t)
	lib := os.Getenv("CTX_LIB")
	if lib == "" {
		return u.die("CTX_LIB is not set, so the ctx lib dir is unknown; run this through ctx: ctx init")
	}
	dry := FlagsFromEnv().DryRun
	if !dry {
		initDirs()
	} else if !isDir(profile.Dir()) || !isDir(ctxDirRaw()+"/backups") || !isRegular(ctxconfig.Path()) {
		u.dim("  [dry-run] would create " + ctxDirRaw())
	}
	_, _ = io.WriteString(u.out, tui.Header(t, "ctx v"+os.Getenv("CTX_VERSION")+" — Setup check"))

	u.say("")
	u.info("Checking your installation...")
	u.say("")

	var missing []string
	for _, tool := range []string{"mise", "gum", "gh"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		u.warn("Missing tools: " + strings.Join(missing, " "))
		yes := dry
		if !dry {
			var err error
			if yes, err = t.Prompter().AskYN("Install missing tools now?", true); err != nil {
				return t.Cancelled()
			}
		}
		if yes {
			installMissing(t, u, lib, missing, dry)
		}
	}

	home := os.Getenv("HOME")
	sh := os.Getenv("SHELL")
	rc := ""
	if strings.Contains(sh, "zsh") {
		rc = home + "/.zshrc"
	}
	if strings.Contains(sh, "bash") {
		rc = home + "/.bashrc"
	}
	if strings.Contains(sh, "fish") {
		rc = home + "/.config/fish/config.fish"
	}
	if rc != "" {
		data, _ := os.ReadFile(rc)
		has := func(s string) bool { return bytes.Contains(data, []byte(s)) }
		activate, lib, paste := autoswitchForms(string(data))
		if !has("mise activate") || (!activate && !lib && !paste) {
			yes := dry
			if !dry {
				var err error
				if yes, err = t.Prompter().AskYN("Install shell hooks (mise + ctx auto-switch) to "+rc+"?", true); err != nil {
					return t.Cancelled()
				}
			}
			if yes {
				if code := installHook(t, []string{rc}); code != 0 {
					return code
				}
			} else {
				u.info("Skipped. Run: ctx install-hook")
			}
		} else {
			u.success("Shell hooks already installed")
		}
	}

	u.say("")
	u.info("Config dir: " + ctxDirRaw())
	u.info("SSH config: " + sshConfigPath())
	u.say("")
	u.bold("  Next steps:")
	u.say("")
	u.say("  ctx setup     Configure your first client profile")
	u.say("  ctx doctor    Tools, hooks, SSH layout (see also: ctx verify <profile>)")
	u.say("")
	return 0
}

// installMissing is the "Install missing tools now?" yes branch of cmd_init:
// install.sh next to the lib dir ($(cd lib/.. && pwd)/install.sh), else brew
// per tool. Under dry-run it only says what it would run.
func installMissing(t tui.Term, u ui, lib string, missing []string, dry bool) {
	installer := filepath.Join(filepath.Dir(lib), "install.sh")
	if isRegular(installer) {
		if dry {
			u.dim("  [dry-run] would run bash " + installer)
			return
		}
		cmd := exec.Command("bash", installer)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = t.In, t.Out, t.Err
		_ = cmd.Run() // like Bash, init goes on whatever the installer exits with
		return
	}
	_, err := exec.LookPath("brew")
	hasBrew := err == nil
	for _, tool := range missing {
		switch {
		case dry && hasBrew:
			u.dim("  [dry-run] would run brew install " + tool)
		case !dry && hasBrew && exec.Command("brew", "install", tool).Run() == nil:
			u.success(tool + " installed")
		default:
			u.warn(tool + ": install manually")
		}
	}
}
