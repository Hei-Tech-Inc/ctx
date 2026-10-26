package setup

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

const (
	fullSteps   = 8
	noneSkip    = "None / skip"
	addSecrets  = "Add selected secrets now (manual, one by one)"
	invalidKey  = "'. Use [A-Za-z_][A-Za-z0-9_]*."
	manualEntry = "Manual entry only (skip auto-detected references)"
)

// Full ports the interactive part of cmd_import: the welcome block, the
// account scan, and then, unless --scope clone chose already, the mode picker.
// "Clone helper only" runs Clone; otherwise the eight-step wizard's steps 1-7
// fill in (Scope "full"). The review (step 8) and the writes are the
// caller's. Errors are tui.ErrCancelled or a *DieError with Bash's text.
func (w *Wizard) Full(in *Inputs) error {
	w.Welcome()
	w.Scan()
	if in.Scope != "clone" {
		pick, err := w.P.PickOne("What do you need for this profile?", []string{
			"Clone helper only (GitHub SSH + ctx clone)",
			"Full client profile (git, folder, cloud, secrets, …)",
		})
		if err != nil {
			return err
		}
		if strings.Contains(pick, "Clone") {
			in.Scope = "clone"
		}
	}
	if in.Scope == "clone" {
		return w.Clone(in)
	}
	in.Scope = "full"
	in.ImportExisting = true
	for _, step := range []func(*Inputs) error{
		w.stepIdentity, w.stepLocation, w.stepImport, w.stepSSH, w.stepCloud, w.stepSecrets, w.stepEnvs,
	} {
		if err := step(in); err != nil {
			return err
		}
	}
	return nil
}

func (w *Wizard) step(n int, title, why string) {
	w.block(tui.Step(w.T, n, fullSteps, title, why))
}

// stepIdentity is step 1: profile name, git name and email, signing.
func (w *Wizard) stepIdentity(in *Inputs) error {
	w.step(1, "Profile & Git identity", "This name is the ctx handle, SSH host alias, and default folder slug.")
	name, err := w.P.Ask("Profile name (short slug for this client)", "", "e.g. acme, globex, northwind")
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

	curName := w.output("git", "config", "--global", "user.name")
	curEmail := w.output("git", "config", "--global", "user.email")
	if in.GitName, err = w.P.Ask("Git commit author name for this client", curName, "e.g. Jane Doe"); err != nil {
		return err
	}
	w.U.Dim("  Email must look like you@company.com (real mailbox, not your GitHub handle).")
	for {
		e, err := w.P.Ask("Git commit email for this client", curEmail, "you@company.com")
		if err != nil {
			return err
		}
		in.GitEmail = trimSpace(e)
		if in.GitEmail == "" {
			break
		}
		if SensibleEmail(in.GitEmail) {
			w.U.Success("Using git email: " + in.GitEmail)
			break
		}
		w.U.Warn("That doesn't look like an email (expected user@domain.tld). You entered: '" + in.GitEmail + "'")
		w.U.Dim("  Edit and try again, or clear the field to skip for now.")
	}
	if err := w.AskSigning(in); err != nil {
		return err
	}
	w.U.Say("")
	return nil
}

// AskSigning ports _ctx_setup_ask_signing: "Keep current (<format>)" is
// offered when in has a signing format; SSH signing clears the key (it
// defaults to the profile's SSH key when written); GPG asks for the key ID
// and an empty ID turns signing off; anything else (a skipped pick too) is
// no signing.
func (w *Wizard) AskSigning(in *Inputs) error {
	var items []string
	if in.SigningFormat != "" {
		items = append(items, "Keep current ("+in.SigningFormat+")")
	}
	items = append(items, "No signing", "SSH signing (uses this profile's SSH key)", "GPG key")
	choice, err := w.P.PickOne("Sign commits for this profile?", items)
	if err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(choice, "Keep current"):
	case strings.HasPrefix(choice, "SSH signing"):
		in.SigningFormat, in.SigningKey = "ssh", ""
	case choice == "GPG key":
		in.SigningFormat = "openpgp"
		if in.SigningKey, err = w.P.Ask("GPG key ID (from: gpg --list-secret-keys --keyid-format=long)", "", ""); err != nil {
			return err
		}
		if in.SigningKey == "" {
			in.SigningFormat = ""
		}
	default:
		in.SigningFormat, in.SigningKey = "", ""
	}
	return nil
}

// stepLocation is step 2: the work directory (current, an existing path that
// may be created after a confirm, or a folder under the work root).
func (w *Wizard) stepLocation(in *Inputs) error {
	root := ctxconfig.WorkRoot()
	w.step(2, "Project location", "Git includeIf and mise hooks use this directory.")
	w.U.Dim("  Choose (↑/↓ + Enter, or numbers without gum): current dir, existing path, or new folder under " + root + ".")
	w.U.Say("")
	current := "Use current working directory (default): " + w.PWD
	sel, err := w.P.PickOne("How do you want to onboard this client/project?",
		[]string{current, "Use an existing path (you provide it)", "Create/select a client folder under " + root})
	if err != nil {
		return err
	}
	if sel == "" {
		sel = current
	}
	switch sel {
	case current:
		in.WorkDir = w.PWD
		w.U.Success("Using current directory: " + in.WorkDir)
	case "Use an existing path (you provide it)":
		w.U.Say("")
		w.U.Dim("  Next: type or paste the full path (shown as you type). Examples: " + w.Home + "/clients/acme")
		p, err := w.P.Ask("Existing project path", w.PWD, filepath.Dir(w.PWD)+"/my-project")
		if err != nil {
			return err
		}
		p = expandTilde(p, w.Home)
		if p == "" {
			return &DieError{Msg: "Path required."}
		}
		if !isDir(p) {
			ok, err := w.P.AskYN("Path does not exist. Create it now?", true)
			if err != nil {
				return err
			}
			if !ok {
				return &DieError{Msg: "Aborted."}
			}
			if w.DryRun {
				w.dry("create directory " + p)
			} else {
				mk := exec.Command("mkdir", "-p", p)
				mk.Stdout, mk.Stderr = w.T.Out, w.T.Err
				if mk.Run() != nil {
					return &DieError{Msg: "Could not create: " + p, Hint: true}
				}
			}
		}
		in.WorkDir = p
		w.U.Success("Using path: " + in.WorkDir)
	default:
		w.U.Say("")
		w.U.Info("Profiles default under: " + root)
		w.U.Dim("  Folder name only (no slashes). Example: acme → " + root + "/acme")
		slug, err := w.P.Ask("Client folder name (not full path)", in.Profile, "e.g. "+in.Profile)
		if err != nil {
			return err
		}
		if slug = FolderSlug(slug); slug == "" {
			return &DieError{Msg: "Client folder name required."}
		}
		in.WorkDir = root + "/" + slug
		w.U.Success("Using client folder: " + in.WorkDir)
	}
	w.U.Say("")
	return nil
}

// stepImport is step 3: import detected references, or type everything.
func (w *Wizard) stepImport(in *Inputs) error {
	w.step(3, "Config import mode", "Auto picks from what we scanned; manual means you type each reference.")
	choice, err := w.P.PickOne("Config import mode for this profile?",
		[]string{"Import detected existing config references (recommended)", manualEntry})
	if err != nil {
		return err
	}
	in.ImportExisting = choice != manualEntry
	if in.ImportExisting {
		w.U.Dim("  Importing references only (SSH key paths, account/profile names).")
		w.U.Dim("  Secret values are never auto-imported.")
	} else {
		w.U.Dim("  Manual mode selected. You will type all values explicitly.")
	}
	w.U.Say("")
	return nil
}

var keyIndexRE = regexp.MustCompile(`^\[([0-9]+)\]`)

// fingerprint is `ssh-keygen -lf <key>.pub 2>/dev/null | awk '{print $2}'`.
func (w *Wizard) fingerprint(key string) string {
	if !w.hasTool("ssh-keygen") {
		return ""
	}
	var out []string
	for _, l := range strings.Split(w.output("ssh-keygen", "-lf", key+".pub"), "\n") {
		f := strings.Fields(l)
		if len(f) > 1 {
			out = append(out, f[1])
		} else {
			out = append(out, "")
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// generate is the full wizard's "generate a key for this profile".
func (w *Wizard) generate(in *Inputs) error {
	in.SSHKeyPath = w.Home + "/.ssh/ctx_" + in.Profile
	if err := w.GenKey(in.SSHKeyPath, KeyComment(in.Profile, in.GitEmail)); err != nil {
		return err
	}
	in.SSHKeyGenerated = true
	return nil
}

// stepSSH is step 4: the SSH key (a scanned one, a path, a new one or none),
// then the GitHub account (optional gh login, the scanned accounts or a typed
// name) and the upload of a new key.
func (w *Wizard) stepSSH(in *Inputs) error {
	w.step(4, "SSH & GitHub", "Per-profile key and github.com SSH host alias.")
	w.U.Say("")
	keys := w.Det.SSHKeys
	switch {
	case in.ImportExisting && len(keys) > 0:
		items := []string{"Generate new key"}
		for i, k := range keys {
			label := "[" + strconv.Itoa(i+1) + "] " + filepath.Base(k)
			if fp := w.fingerprint(k); fp != "" {
				label += "  (" + fp + ")"
			}
			items = append(items, label)
		}
		choice, err := w.P.PickOne("Choose SSH key (recommended: Generate new key)", items)
		if err != nil {
			return err
		}
		picked := ""
		if m := keyIndexRE.FindStringSubmatch(choice); choice != "Generate new key" && m != nil {
			if i, err := strconv.Atoi(m[1]); err == nil && i >= 1 && i <= len(keys) {
				picked = keys[i-1]
			}
		}
		if picked == "" {
			if err := w.generate(in); err != nil {
				return err
			}
		} else {
			in.SSHKeyPath = picked
			w.U.Success("Using existing key: " + filepath.Base(picked))
		}
	case !in.ImportExisting:
		mode, err := w.P.PickOne("SSH key setup?",
			[]string{"Use an existing key path", "Generate a new key for this profile", "Skip SSH key for now"})
		if err != nil {
			return err
		}
		switch mode {
		case "Use an existing key path":
			k, err := w.P.Ask("Path to private SSH key (e.g. ~/.ssh/id_ed25519)", "", "")
			if err != nil {
				return err
			}
			k = expandTilde(k, w.Home)
			in.SSHKeyPath = k
			if k != "" && !isFile(k) {
				return &DieError{Msg: "SSH key not found: " + k, Hint: true}
			}
			if k != "" {
				w.U.Success("Using existing key: " + filepath.Base(k))
			}
		case "Generate a new key for this profile":
			if err := w.generate(in); err != nil {
				return err
			}
		default:
			in.SSHKeyPath = ""
		}
	default:
		gen, err := w.P.AskYN("No SSH keys found. Generate one for this profile?", true)
		if err != nil {
			return err
		}
		if gen {
			if err := w.generate(in); err != nil {
				return err
			}
		}
	}
	return w.stepGitHub(in)
}

func (w *Wizard) stepGitHub(in *Inputs) error {
	p := in.Profile
	in.GitHubUser = ""
	if w.hasTool("gh") {
		login, err := w.P.AskYN("Need to login/switch GitHub account now?", false)
		if err != nil {
			return err
		}
		if login {
			if w.DryRun {
				w.dry("run gh auth login")
			} else {
				w.U.Info("Tip: choose 'Skip' for SSH key upload here; ctx manages per-profile keys.")
				if w.Run("gh", "auth", "login") != nil {
					w.U.Warn("gh auth login did not complete; continuing setup.")
				}
				_ = w.runQuiet("gh", "auth", "switch")
				w.Det.GHAccounts = w.detectGH()
			}
		}
	}
	if in.ImportExisting && len(w.Det.GHAccounts) > 0 {
		u, err := w.P.PickOne("Which GitHub account for '"+p+"'?", append([]string{noneSkip}, w.Det.GHAccounts...))
		if err != nil {
			return err
		}
		if u == noneSkip {
			u = ""
		}
		in.GitHubUser = u
		if u != "" {
			w.U.Success("GitHub account: " + u)
		}
	}
	if in.GitHubUser == "" {
		u, err := w.P.Ask("GitHub username for this client (leave blank to skip)", "", "")
		if err != nil {
			return err
		}
		in.GitHubUser = u
	}
	if in.GitHubUser != "" && in.SSHKeyGenerated && w.pubKeyReady(in) && w.hasTool("gh") {
		up, err := w.P.AskYN("Upload this new SSH key to GitHub account '"+in.GitHubUser+"' now?", true)
		if err != nil {
			return err
		}
		if up {
			if w.DryRun {
				w.dry("upload SSH key to GitHub (" + in.GitHubUser + ")")
			} else {
				w.upload(in.GitHubUser, in.SSHKeyPath+".pub", p, "Add it manually at https://github.com/settings/keys")
			}
		}
	}
	w.U.Say("")
	return nil
}

// pickOrNone is the cloud pickers' pick_one with "None / skip" first; None
// and a skipped pick give "".
func (w *Wizard) pickOrNone(prompt string, items []string) (string, error) {
	v, err := w.P.PickOne(prompt, append([]string{noneSkip}, items...))
	if v == noneSkip {
		v = ""
	}
	return v, err
}

// stepCloud is step 5: AWS profile, Azure subscription and tenant, GCP
// project and account, kubectl context — picked from the scan in import
// mode, typed otherwise. Only AWS asks again after "None / skip".
func (w *Wizard) stepCloud(in *Inputs) error {
	p := in.Profile
	imp := in.ImportExisting
	d := w.Det
	ask := func(prompt string) (string, error) { return w.P.Ask(prompt, "", "") }
	var err error
	w.step(5, "Cloud credentials", "Optional — stored in profile + mise env when you use this client.")
	w.U.Say("")

	if imp && len(d.AWSProfiles) > 0 {
		if in.AWSProfile, err = w.pickOrNone("AWS profile for '"+p+"'?", d.AWSProfiles); err != nil {
			return err
		}
		if in.AWSProfile != "" {
			w.U.Success("AWS profile: " + in.AWSProfile)
		}
	}
	if in.AWSProfile == "" {
		if in.AWSProfile, err = ask("AWS profile name (leave blank to skip)"); err != nil {
			return err
		}
	}

	if imp && len(d.AzureSubs) > 0 {
		in.AzureSub, err = w.pickOrNone("Azure subscription for '"+p+"'?", d.AzureSubs)
	} else {
		in.AzureSub, err = ask("Azure subscription ID (leave blank to skip)")
	}
	if err != nil {
		return err
	}
	if in.AzureSub != "" {
		if in.AzureTenant, err = ask("Azure tenant ID (leave blank to skip)"); err != nil {
			return err
		}
	}

	if imp && len(d.GCPProjects) > 0 {
		in.GCPProject, err = w.pickOrNone("GCP project for '"+p+"'?", d.GCPProjects)
	} else {
		in.GCPProject, err = ask("GCP project ID (leave blank to skip)")
	}
	if err != nil {
		return err
	}
	if in.GCPProject != "" {
		if in.GCPAccount, err = ask("GCP account email (leave blank to skip)"); err != nil {
			return err
		}
	}

	if imp && len(d.KubeContexts) > 0 {
		if in.KubeContext, err = w.pickOrNone("kubectl context for '"+p+"'?", d.KubeContexts); err != nil {
			return err
		}
		if in.KubeContext != "" {
			w.U.Success("kubectl context: " + in.KubeContext)
		}
	} else if in.KubeContext, err = ask("kubectl context (leave blank to skip)"); err != nil {
		return err
	}
	w.U.Say("")
	return nil
}

// askSecretValue is `val=$(ask_secret "Value for KEY")`.
func (w *Wizard) askSecretValue(key string) (string, error) {
	v, err := w.P.AskSecret("Value for " + key)
	return echoCaptured(v), err
}

// stepSecrets is step 6: where secrets go, then either skip or the three
// common tokens and any custom keys. Values are queued (R4) and stored only
// after the review.
func (w *Wizard) stepSecrets(in *Inputs) error {
	w.step(6, "Secrets", "Stored in Keychain, pass, or ~/.ctx/secrets — never in the .conf file.")
	w.U.Bold("  Storage: " + secret.StoreLabel())
	w.U.Say("")
	w.U.Dim("  Security note: if your machine is compromised while unlocked, local secrets can still be exfiltrated.")
	w.U.Dim("  Best practice: add only what this client/project needs, rotate regularly, and avoid broad long-lived tokens.")
	w.U.Say("")
	provider := secret.EffectiveProvider()
	switch provider {
	case secret.ProviderKeychain:
		w.U.Dim("  Values you enter here go into Keychain.")
	case secret.ProviderPass:
		w.U.Dim("  Values you enter here go into pass (ctx/<profile>/<KEY>).")
	default:
		w.U.Dim("  Values you enter here go into ~/.ctx/secrets/<profile>/<KEY> (0600).")
	}
	w.U.Dim("  They are exported by ctx use and by mise hooks.enter (when mise runs).")
	if provider == secret.ProviderPass && !w.hasTool("pass") {
		w.U.Warn("  'pass' CLI not found. Install pass or set: ctx config secret-provider auto")
	}
	w.U.Say("")

	mode, err := w.P.PickOne("How should secrets be handled for this setup?",
		[]string{"Skip for now (recommended on shared/untrusted machines)", addSecrets})
	if err != nil {
		return err
	}
	if mode != addSecrets {
		w.U.Dim("  Skipping secret storage for now.")
		w.U.Dim("  You can add later with: ctx secret set " + in.Profile + " <KEY>")
		w.U.Say("")
		return nil
	}
	for _, s := range [][2]string{
		{"GH_TOKEN", "Store a GitHub personal access token"},
		{"NPM_TOKEN", "Store an npm registry token"},
		{"DOCKER_TOKEN", "Store a Docker Hub token"},
	} {
		yes, err := w.P.AskYN(s[1]+"?", false)
		if err != nil {
			return err
		}
		if yes {
			v, err := w.askSecretValue(s[0])
			if err != nil {
				return err
			}
			if v != "" {
				in.PendingSecrets = append(in.PendingSecrets, Secret{s[0], v})
			}
		}
	}
	for {
		more, err := w.P.AskYN("Add another secret key?", false)
		if err != nil {
			return err
		}
		if !more {
			break
		}
		key, err := w.P.Ask("Key name (e.g. STRIPE_SECRET_KEY)", "", "")
		if err != nil {
			return err
		}
		if key == "" {
			break
		}
		if !ValidEnvKey(key) {
			w.U.Warn("Invalid key '" + key + invalidKey)
			continue
		}
		v, err := w.askSecretValue(key)
		if err != nil {
			return err
		}
		if v != "" {
			in.PendingSecrets = append(in.PendingSecrets, Secret{key, v})
		}
	}
	w.U.Say("")
	return nil
}

// stepEnvs is step 7: non-secret KEY=value pairs for mise.toml.
func (w *Wizard) stepEnvs(in *Inputs) error {
	w.step(7, "Non-secret env vars", "Written into mise.toml (visible in the repo if committed — no API keys).")
	add, err := w.P.AskYN("Add non-secret env vars (e.g. NODE_ENV=development)?", false)
	if err != nil {
		return err
	}
	for add {
		key, err := w.P.Ask("Env key (leave blank to stop)", "", "")
		if err != nil {
			return err
		}
		if key == "" {
			break
		}
		if !ValidEnvKey(key) {
			w.U.Warn("Invalid key '" + key + invalidKey)
			continue
		}
		v, err := w.P.Ask("Value for "+key, "", "")
		if err != nil {
			return err
		}
		in.ExtraEnvs = append(in.ExtraEnvs, EnvPair{key, v})
	}
	w.U.Say("")
	return nil
}
