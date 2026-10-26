package setup

import (
	"os/exec"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// Extend ports `ctx setup --extend NAME` (_ctx_setup_interactive_extend in
// lib/cmd_import.sh): the welcome block and the account scan, then, for the
// existing profile in.Profile, every field loaded from its .conf (missing
// keys are empty, R13) and the sections it lacks or the user asks to update:
// git identity and signing, work directory, SSH key, GitHub user, cloud
// defaults, secrets (queued, R4) and env vars. Each ask defaults to the
// profile's value. The review, the writes and the "updated" summary are the
// caller's (in.Extend is set). A missing profile is a *DieError with Bash's
// text.
func (w *Wizard) Extend(in *Inputs) error {
	w.Welcome()
	w.Scan()
	p := in.Profile
	if !profileExists(p) {
		return &DieError{Msg: "setup --extend: profile '" + p + "' not found"}
	}
	if err := loadInputs(in); err != nil {
		return err
	}

	w.U.Bold("Extend profile: " + p)
	w.U.Dim("  SSH: " + orDash(in.SSHKeyPath) + "   WORK_DIR: " + orDash(in.WorkDir) +
		"   Git: " + orDash(in.GitName) + " <" + orDash(in.GitEmail) + ">")
	w.U.Say("")

	for _, section := range []func(*Inputs) error{
		w.extendIdentity, w.extendWorkDir, w.extendSSH, w.extendGitHub, w.extendCloud, w.extendSecrets, w.extendEnvs,
	} {
		if err := section(in); err != nil {
			return err
		}
	}
	w.U.Say("")
	return nil
}

// loadInputs is load_profile plus the extend prefill: every field from the
// profile's .conf, PROFILE_NAME the name given, SECRET_KEYS split like
// `read -ra` (first line, on blanks), EXTRA_ENVS kept as text.
func loadInputs(in *Inputs) error {
	pr, err := profile.Load(profile.Dir(), in.Profile)
	if err != nil {
		return &DieError{Msg: "setup --extend: " + err.Error()}
	}
	*in = Inputs{
		Scope: "full", Extend: true, AutoYes: in.AutoYes, Profile: in.Profile,
		GitName: pr.GitName, GitEmail: pr.GitEmail, WorkDir: pr.WorkDir, GitHubUser: pr.GithubUser,
		SSHKeyPath: pr.SSHKeyPath, AWSProfile: pr.AWSProfile, AzureSub: pr.AzureSubscription,
		AzureTenant: pr.AzureTenant, GCPProject: pr.GCPProject, GCPAccount: pr.GCPAccount,
		KubeContext: pr.KubeContext, SigningFormat: pr.SigningFormat, SigningKey: pr.SigningKey,
		SecretKeys: readWords(pr.SecretKeysRaw), ExtraEnvsRaw: pr.ExtraEnvsRaw,
	}
	return nil
}

// readWords is `read -ra arr <<< "$s"` with the default IFS: the first line,
// split on spaces and tabs.
func readWords(s string) []string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' })
}

// extendIdentity asks for the git name and email (defaults: the profile's,
// else git's global ones) and signing, when the profile has neither or the
// user wants to update them.
func (w *Wizard) extendIdentity(in *Inputs) error {
	if in.GitName != "" || in.GitEmail != "" {
		ok, err := w.P.AskYN("Set or update git commit identity for this profile?", true)
		if err != nil || !ok {
			return err
		}
	}
	curName := w.output("git", "config", "--global", "user.name")
	curEmail := w.output("git", "config", "--global", "user.email")
	var err error
	if in.GitName, err = w.P.Ask("Git commit author name", or(in.GitName, curName), "Jane Doe"); err != nil {
		return err
	}
	for {
		// The default is the last answer, as Bash re-reads GIT_EMAIL.
		e, err := w.P.Ask("Git commit email", or(in.GitEmail, curEmail), "you@company.com")
		if err != nil {
			return err
		}
		in.GitEmail = trimSpace(e)
		if in.GitEmail == "" || SensibleEmail(in.GitEmail) {
			break
		}
		w.U.Warn("Expected user@domain.tld — try again or leave blank.")
	}
	return w.AskSigning(in)
}

// or is Bash's ${a:-b}.
func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// extendWorkDir offers a work directory when the profile has none: the
// current one, an existing path (created without asking) or a folder under
// the work root (the name is not slugged).
func (w *Wizard) extendWorkDir(in *Inputs) error {
	if in.WorkDir != "" {
		return nil
	}
	ok, err := w.P.AskYN("Set a work directory (autoswitch + git includeIf + mise)?", true)
	if err != nil || !ok {
		return err
	}
	root := ctxconfig.WorkRoot()
	current := "Use current directory: " + w.PWD
	sel, err := w.P.PickOne("Where is this client/project?",
		[]string{current, "Use an existing path", "New folder under " + root})
	if err != nil {
		return err
	}
	switch sel {
	case current:
		in.WorkDir = w.PWD
	case "Use an existing path":
		p, err := w.P.Ask("Existing project path", w.PWD, "")
		if err != nil {
			return err
		}
		p = expandTilde(p, w.Home)
		if !isDir(p) {
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
	default:
		slug, err := w.P.Ask("Folder name under "+root, in.Profile, "")
		if err != nil {
			return err
		}
		in.WorkDir = root + "/" + slug
	}
	w.U.Success("Work directory: " + in.WorkDir)
	return nil
}

// extendSSH offers an SSH key when the profile's is unset or missing: an
// existing private key (it must exist) or a new one.
func (w *Wizard) extendSSH(in *Inputs) error {
	if in.SSHKeyPath != "" && isFile(in.SSHKeyPath) {
		return nil
	}
	ok, err := w.P.AskYN("Configure SSH key and github-"+in.Profile+" host alias?", true)
	if err != nil || !ok {
		return err
	}
	mode, err := w.P.PickOne("SSH key setup", []string{"Use an existing private key", "Generate a new key"})
	if err != nil {
		return err
	}
	if mode != "Use an existing private key" {
		return w.generate(in)
	}
	k, err := w.P.Ask("Path to private key", w.Home+"/.ssh/id_ed25519", "")
	if err != nil {
		return err
	}
	in.SSHKeyPath = expandTilde(k, w.Home)
	if !isFile(in.SSHKeyPath) {
		return &DieError{Msg: "SSH key not found: " + in.SSHKeyPath, Hint: true}
	}
	return nil
}

func (w *Wizard) extendGitHub(in *Inputs) error {
	if in.GitHubUser != "" || !w.hasTool("gh") {
		return nil
	}
	var err error
	in.GitHubUser, err = w.P.Ask("GitHub username (blank to skip)", "", "")
	return err
}

// extendCloud asks for every cloud default, each defaulting to the profile's,
// when AWS, Azure subscription, GCP project and kube context are all unset.
func (w *Wizard) extendCloud(in *Inputs) error {
	if in.AWSProfile+in.AzureSub+in.GCPProject+in.KubeContext != "" {
		return nil
	}
	ok, err := w.P.AskYN("Add cloud / kubectl defaults (AWS, Azure, GCP, kube)?", false)
	if err != nil || !ok {
		return err
	}
	for _, q := range []struct {
		prompt string
		v      *string
		cond   *string // asked only when *cond is set
	}{
		{"AWS profile name (blank to skip)", &in.AWSProfile, nil},
		{"Azure subscription ID (blank to skip)", &in.AzureSub, nil},
		{"Azure tenant ID (blank to skip)", &in.AzureTenant, &in.AzureSub},
		{"GCP project ID (blank to skip)", &in.GCPProject, nil},
		{"GCP account email (blank to skip)", &in.GCPAccount, &in.GCPProject},
		{"kubectl context (blank to skip)", &in.KubeContext, nil},
	} {
		if q.cond != nil && *q.cond == "" {
			continue
		}
		if *q.v, err = w.P.Ask(q.prompt, *q.v, ""); err != nil {
			return err
		}
	}
	return nil
}

// extendSecrets queues GH_TOKEN and any custom keys (R4: stored after the
// review; a key the profile lists already is not listed twice, R26).
func (w *Wizard) extendSecrets(in *Inputs) error {
	add, err := w.P.AskYN("Add or update secrets for this profile?", false)
	if err != nil || !add {
		return err
	}
	queue := func(key string) error {
		v, err := w.askSecretValue(key)
		if err == nil && v != "" {
			in.PendingSecrets = append(in.PendingSecrets, Secret{key, v})
		}
		return err
	}
	gh, err := w.P.AskYN("Store a GitHub personal access token?", false)
	if err != nil {
		return err
	}
	if gh {
		if err := queue("GH_TOKEN"); err != nil {
			return err
		}
	}
	for {
		more, err := w.P.AskYN("Add another secret key?", false)
		if err != nil || !more {
			return err
		}
		key, err := w.P.Ask("Key name (e.g. NPM_TOKEN)", "", "")
		if err != nil {
			return err
		}
		if key == "" {
			return nil
		}
		if !ValidEnvKey(key) {
			w.U.Warn("Invalid key '" + key + "'")
			continue
		}
		if err := queue(key); err != nil {
			return err
		}
	}
}

// extendEnvs appends non-secret KEY=value pairs when there is a work dir.
func (w *Wizard) extendEnvs(in *Inputs) error {
	if in.WorkDir == "" {
		return nil
	}
	add, err := w.P.AskYN("Add non-secret env vars to mise.toml?", false)
	if err != nil || !add {
		return err
	}
	for {
		key, err := w.P.Ask("Env key (blank to stop)", "", "")
		if err != nil || key == "" {
			return err
		}
		if !ValidEnvKey(key) {
			w.U.Warn("Invalid key '" + key + "'")
			continue
		}
		v, err := w.P.Ask("Value for "+key, "", "")
		if err != nil {
			return err
		}
		in.ExtraEnvs = append(in.ExtraEnvs, EnvPair{key, v})
	}
}
