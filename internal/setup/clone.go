package setup

import (
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// profileExists is lib/core.sh's profile_exists.
func profileExists(name string) bool {
	return isFile(ctxDir() + "/profiles/" + name + ".conf")
}

// Clone ports _ctx_setup_interactive_clone, the three steps of the clone-only
// wizard: the profile name, the SSH key (an existing one, or a new one through
// GenKey), and the optional GitHub user, folder and key upload. It fills in
// (Scope "clone"; git, cloud and secrets stay empty). Errors are
// tui.ErrCancelled or a *DieError with Bash's text.
func (w *Wizard) Clone(in *Inputs) error {
	const steps = 3
	in.Scope = "clone"

	w.block(tui.Step(w.T, 1, steps, "Profile name", "Used for ctx clone -p and the github-<name> SSH host."))
	name, err := w.P.Ask("Profile name (short slug)", "", "e.g. acme, side-project")
	if err != nil {
		return err
	}
	if name == "" {
		return &DieError{Msg: "Profile name required."}
	}
	p := Slug(name)
	in.Profile = p
	if profileExists(p) {
		ok, err := w.P.AskYN("Profile '"+p+"' already exists. Overwrite?", false)
		if err != nil {
			return err
		}
		if !ok {
			return &DieError{Msg: "Aborted."}
		}
	}
	in.GitName, in.GitEmail = "", ""
	w.U.Say("")

	w.block(tui.Step(w.T, 2, steps, "SSH key", "Required so git@github-<profile>:… uses the right key."))
	mode, err := w.P.PickOne("SSH key for github-"+p+"?",
		[]string{"Use an existing private key", "Generate a new key for this profile"})
	if err != nil {
		return err
	}
	in.SSHKeyGenerated = false
	if mode == "Use an existing private key" {
		def := w.Home + "/.ssh/id_ed25519"
		key, err := w.P.Ask("Path to private SSH key", def, def)
		if err != nil {
			return err
		}
		key = expandTilde(key, w.Home)
		in.SSHKeyPath = key
		if key != "" && !isFile(key) {
			return &DieError{Msg: "SSH key not found: " + key, Hint: true}
		}
		if key != "" {
			w.U.Success("Using key: " + filepath.Base(key)) // basename; key is a file, so no trailing slash
		}
	} else { // anything else, a skipped pick too, generates (Bash's case *)
		in.SSHKeyPath = w.Home + "/.ssh/ctx_" + p
		if err := w.GenKey(in.SSHKeyPath, KeyComment(p, in.GitEmail)); err != nil {
			return err
		}
		in.SSHKeyGenerated = true
	}
	w.U.Say("")

	w.block(tui.Step(w.T, 3, steps, "Optional extras", "Skip folder linking if you only need ctx clone."))
	user, err := w.P.Ask("GitHub username (blank to skip)", "", "")
	if err != nil {
		return err
	}
	in.GitHubUser = user
	current := "Use current directory: " + w.PWD
	loc, err := w.P.PickOne("Link a client folder for autoswitch later?",
		[]string{"Skip for now — clone helper only", current, "Create folder under " + ctxconfig.WorkRoot()})
	if err != nil {
		return err
	}
	in.WorkDir = ""
	switch {
	case loc == current:
		in.WorkDir = w.PWD
		w.U.Success("Work directory: " + in.WorkDir)
	case strings.HasPrefix(loc, "Create folder under "):
		root := ctxconfig.WorkRoot()
		slug, err := w.P.Ask("Folder name under "+root, p, p)
		if err != nil {
			return err
		}
		in.WorkDir = root + "/" + strings.ReplaceAll(slug, "/", "-")
		w.U.Success("Work directory: " + in.WorkDir)
	default:
		w.U.Dim("  No WORK_DIR — use: ctx clone -p " + p + " <url>")
	}

	if user != "" && in.SSHKeyGenerated && w.pubKeyReady(in) && w.hasTool("gh") {
		up, err := w.P.AskYN("Upload this SSH key to GitHub ("+user+") now?", false)
		if err != nil {
			return err
		}
		if up {
			if w.DryRun {
				w.dry("upload SSH key to GitHub (" + user + ")")
			} else if err := w.UploadKey(user, in.SSHKeyPath+".pub", p); err != nil {
				return err
			}
		}
	}

	in.AWSProfile, in.AzureSub, in.AzureTenant = "", "", ""
	in.GCPProject, in.GCPAccount, in.KubeContext = "", "", ""
	in.SecretKeys, in.PendingSecrets, in.ExtraEnvs = nil, nil, nil
	in.ImportExisting = false
	w.U.Say("")
	return nil
}
