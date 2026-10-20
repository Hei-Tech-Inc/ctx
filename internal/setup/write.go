package setup

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/gitident"
	"github.com/Hei-Tech-Inc/ctx/internal/gitssh"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/shquote"
	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

// ANSI codes the full summary embeds in its success message (lib/core.sh).
const (
	ansiBold  = "\033[1m"
	ansiReset = "\033[0m"
	ansiGreen = "\033[0;32m"
)

// ErrWriteFailed is Write's error when a step could not write its file. The
// step has printed its own error line; the caller exits 1 without more output.
var ErrWriteFailed = errors.New("setup: a file could not be written")

// errText is the OS reason in err ("permission denied"), without the path the
// message already names.
func errText(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err.Error()
	}
	return err.Error()
}

// couldNot prints "✗ Could not <what>: <reason>" (Bash prints the same
// without the reason, which a shell cannot get).
func couldNot(u Out, what string, err error) {
	u.Error("Could not " + what + ": " + errText(err))
}

func streams(u Out) (stdout, stderr io.Writer) {
	if t, ok := u.(Terminal); ok {
		return t.Streams()
	}
	return os.Stdout, os.Stderr
}

func colors(u Out) bool {
	t, ok := u.(Terminal)
	return ok && t.Colors()
}

// ctxDir is $CTX_DIR as lib/core.sh sets it (${CTX_DIR:-$HOME/.ctx}),
// unnormalized, for the paths the messages print.
func ctxDir() string {
	if d := os.Getenv("CTX_DIR"); d != "" {
		return d
	}
	return os.Getenv("HOME") + "/.ctx"
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func hasTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// git runs `git args...` with the given output streams (nil discards).
func git(stdout, stderr io.Writer, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

// storeSecrets is _ctx_setup_store_secrets (R4): each queued secret is stored
// with the effective provider (keychain_set: an unavailable provider fails),
// followed by its success or warn line. Only stored keys are added to
// SecretKeys. The values are dropped and never printed.
func storeSecrets(in *Inputs, u Out) {
	if len(in.PendingSecrets) == 0 {
		return
	}
	where := secret.StoreLabel()
	st, openErr := secret.OpenWith(secret.EffectiveProvider())
	keys := append([]string(nil), in.SecretKeys...)
	for _, s := range in.PendingSecrets {
		if openErr == nil && st.Set(in.Profile, s.Key, s.Value) == nil {
			u.Success(s.Key + " → " + where)
			keys = append(keys, s.Key)
		} else {
			u.Warn("Could not store " + s.Key + " (" + where + ")")
		}
	}
	in.SecretKeys = keys
	in.PendingSecrets = nil
}

func extraEnvsText(pairs []EnvPair) string {
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(p.Key + "=" + p.Value + "\n")
	}
	return b.String()
}

// confBody is the profile .conf _write_all writes (header, then the keys in
// order, each value quoted like printf %q).
func confBody(in Inputs, signingFormat, signingKey string) string {
	var b strings.Builder
	b.WriteString("# ctx profile: " + in.Profile + "\n")
	b.WriteString("# Generated: " + ctxconfig.Now().Format("Mon Jan _2 15:04:05 MST 2006") + "\n")
	b.WriteString("# Secrets: configured provider (auto/keychain/file/pass). Env vars are in mise.toml.\n\n")
	for _, kv := range [][2]string{
		{"PROFILE_NAME", in.Profile}, {"GIT_NAME", in.GitName}, {"GIT_EMAIL", in.GitEmail},
		{"WORK_DIR", in.WorkDir}, {"GITHUB_USER", in.GitHubUser}, {"SSH_KEY_PATH", in.SSHKeyPath},
		{"AWS_PROFILE_NAME", in.AWSProfile}, {"AZURE_SUBSCRIPTION", in.AzureSub}, {"AZURE_TENANT", in.AzureTenant},
		{"GCP_PROJECT", in.GCPProject}, {"GCP_ACCOUNT", in.GCPAccount}, {"KUBE_CONTEXT", in.KubeContext},
		{"SECRET_KEYS", strings.Join(in.SecretKeys, " ")}, {"EXTRA_ENVS", extraEnvsText(in.ExtraEnvs)},
		{"SIGNING_FORMAT", signingFormat}, {"SIGNING_KEY", signingKey},
	} {
		b.WriteString(kv[0] + "=" + shquote.Quote(kv[1]) + "\n")
	}
	return b.String()
}

// writePrivate replaces path with data, at mode 0600 from the start, through
// a temp file in the same directory.
func writePrivate(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// setSigning is ctx_git_identity_set_signing: clear the four signing keys,
// then set them when both format and key are given.
func setSigning(stdout, stderr io.Writer, file, format, key string) {
	for _, k := range []string{"gpg.format", "user.signingkey", "commit.gpgsign", "tag.gpgsign"} {
		_ = git(stdout, nil, "config", "--file", file, "--unset-all", k)
	}
	if format == "" || key == "" {
		return
	}
	_ = git(stdout, stderr, "config", "--file", file, "gpg.format", format)
	_ = git(stdout, stderr, "config", "--file", file, "user.signingkey", key)
	_ = git(stdout, stderr, "config", "--file", file, "commit.gpgsign", "true")
	_ = git(stdout, stderr, "config", "--file", file, "tag.gpgsign", "true")
}

// Backups is the three backup_file calls cmd_import makes before _write_all:
// ~/.gitconfig, the profile's git identity file and ~/.ssh/ctx_config.
// Missing files are skipped. ctxconfig.InitDirs must have run. The first
// failure stops it with the error "Could not back up <path>: <reason>"; Bash
// dies with that text (without the reason) before writing anything.
func Backups(in Inputs) error {
	home := os.Getenv("HOME")
	for _, f := range []string{
		home + "/.gitconfig",
		home + "/.config/git/ctx-" + in.Profile,
		gitssh.SSHConfigPath(),
	} {
		if err := ctxconfig.BackupFile(f); err != nil {
			return errors.New("Could not back up " + f + ": " + errText(err))
		}
	}
	return nil
}

// Write ports _write_all, preceded by the storage of in.PendingSecrets (R4;
// Bash stores them just before the backups, which print nothing):
//  1. the profile .conf (mode 0600, written atomically), after the signing
//     normalization (ssh without a key uses the SSH key; no key turns it off);
//  2. the git identity file through `git config --file`, and its signing keys;
//  3. the ~/.gitconfig includeIf for WorkDir;
//  4. the SSH host block (AddHost) and, with WorkDir, the insteadOf rewrite;
//  5. WorkDir/mise.toml (kept when it exists) and `mise trust`.
//
// git writes its own output to u's Streams. A step that cannot write its file
// prints "✗ Could not write <path>: <reason>" instead of its success line;
// the .conf stops Write at once, the other steps let the rest run. Either way
// Write returns ErrWriteFailed (Bash: exit 1).
func Write(in Inputs, u Out) error {
	storeSecrets(&in, u)
	stdout, stderr := streams(u)
	home := os.Getenv("HOME")
	p, wd, sshKey := in.Profile, in.WorkDir, in.SSHKeyPath

	sf, sk := in.SigningFormat, in.SigningKey
	if sf == "ssh" && sk == "" {
		sk = sshKey
	}
	if sf != "" && sk == "" {
		u.Warn("Commit signing skipped: no key (SSH signing needs an SSH key for this profile)")
		sf = ""
	}

	// 1. Profile .conf — metadata only, no secrets.
	profDir := ctxDir() + "/profiles"
	_ = os.MkdirAll(profDir, 0o777)
	if err := writePrivate(profDir+"/"+p+".conf", []byte(confBody(in, sf, sk))); err != nil {
		couldNot(u, "write "+profDir+"/"+p+".conf", err)
		return ErrWriteFailed
	}
	failed := false
	u.Success("Profile config written")

	// 2. Git identity file (used by includeIf).
	_ = os.MkdirAll(filepath.Join(home, ".config", "git"), 0o777)
	id := home + "/.config/git/ctx-" + p
	if f, err := os.OpenFile(id, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666); err == nil {
		f.Close()
	}
	_ = git(stdout, stderr, "config", "--file", id, "user.name", in.GitName)
	if SensibleEmail(in.GitEmail) {
		_ = git(stdout, stderr, "config", "--file", id, "user.email", in.GitEmail)
		u.Success("Git identity file: ~/.config/git/ctx-" + p)
	} else {
		u.Warn("Skipped git email in identity file (invalid): " + in.GitEmail)
		u.Success("Git identity file: ~/.config/git/ctx-" + p + " (name only — fix email and re-run setup)")
	}
	setSigning(stdout, stderr, id, sf, sk)
	switch sf {
	case "ssh":
		base := strings.TrimSuffix(sk, ".pub")
		u.Success("Commits signed with SSH key: " + sk)
		u.Info("  Add " + base + ".pub on GitHub as a *signing* key: gh ssh-key add " + base + ".pub --type signing")
	case "openpgp":
		u.Success("Commits signed with GPG key: " + sk)
	}

	// 3. ~/.gitconfig includeIf — one block, deduped.
	if wd != "" {
		switch added, err := gitssh.GitconfigAddInclude(wd, id); {
		case err != nil:
			couldNot(u, "write "+home+"/.gitconfig", err)
			failed = true
		case added:
			u.Success("gitconfig includeIf added for " + wd)
		default:
			u.Dim("  gitconfig includeIf already present")
		}
	}

	// 4. SSH host block in ~/.ssh/ctx_config (never ~/.ssh/config).
	if sshKey != "" && isFile(sshKey) {
		host := "github-" + p
		switch added, err := gitssh.AddHost(host, sshKey); {
		case err != nil:
			couldNot(u, "write "+gitssh.SSHConfigPath(), err)
			failed = true
		case added:
			u.Success("SSH host '" + host + "' → ~/.ssh/ctx_config")
		default:
			u.Dim("  SSH host '" + host + "' already in ctx_config")
		}
		if wd != "" {
			gitident.SetRewrite(p, stdout, stderr)
			u.Info("  Inside " + wd + ", git@github.com: URLs use " + host + " automatically")
		} else {
			u.Info("  Clone repos: git clone git@github-" + p + ":org/repo.git")
		}
	}

	// 5. mise.toml in the work dir.
	if wd != "" {
		_ = os.MkdirAll(wd, 0o777)
		toml := wd + "/mise.toml"
		if isFile(toml) {
			u.Dim("  mise.toml kept (already exists): " + toml)
		} else if err := generateMise(in, toml); err != nil {
			couldNot(u, "write "+toml, err)
			failed = true
		} else {
			u.Success("mise.toml generated: " + toml)
		}
		if isFile(toml) {
			if hasTool("mise") {
				u.Info("  mise will auto-activate when you cd into " + wd)
			} else {
				u.Warn("  mise not installed — run 'ctx doctor' for install help")
			}
		}
	}
	if failed {
		return ErrWriteFailed
	}
	return nil
}

// generateMise is generate_mise_toml for a missing file: write it, run
// `mise trust` (output discarded) when mise is installed, and log. A failed
// write returns before trust and log, as Bash does.
func generateMise(in Inputs, toml string) error {
	body := MiseTOML(in.Profile, in.GitName, in.GitEmail, in.AWSProfile, in.AzureSub, in.GCPProject,
		in.KubeContext, strings.Join(in.SecretKeys, " "), extraEnvsText(in.ExtraEnvs))
	if err := os.WriteFile(toml, []byte(body), 0o666); err != nil {
		return err
	}
	if hasTool("mise") {
		_ = exec.Command("mise", "trust", toml).Run()
	}
	ctxconfig.Log("Generated mise.toml at " + toml)
	return nil
}

// DryRunReport prints what a --dry-run setup shows once the review is skipped:
// the `[dry-run] would create <CTX_DIR>` line when the ctx dirs or config are
// missing, one `[dry-run] would store secret KEY` line per queued secret, then
// the "Would create" list (only files that would be written: mise.toml only
// when missing, the host block only with a key that exists or would be generated).
func DryRunReport(in Inputs, u Out) {
	dir := ctxDir()
	if !isDir(dir) || !isDir(dir+"/profiles") || !isDir(dir+"/backups") || !isFile(dir+"/config") {
		u.Dim("  [dry-run] would create " + dir)
	}
	for _, s := range in.PendingSecrets {
		u.Dim("  [dry-run] would store secret " + s.Key)
	}
	home := os.Getenv("HOME")
	u.Bold("  [DRY RUN] — nothing written\n")
	u.Say("  Would create:")
	u.Dim("    " + dir + "/profiles/" + in.Profile + ".conf")
	u.Dim("    " + home + "/.config/git/ctx-" + in.Profile)
	if in.WorkDir != "" && !isFile(in.WorkDir+"/mise.toml") {
		u.Dim("    " + in.WorkDir + "/mise.toml")
	}
	if in.SSHKeyPath != "" && (isFile(in.SSHKeyPath) || in.SSHKeyGenerated) {
		u.Dim("    " + gitssh.SSHConfigPath() + "  (host block: github-" + in.Profile + ")")
	}
	if in.WorkDir != "" {
		u.Dim("    ~/.gitconfig  (includeIf block)")
	}
	u.Say("")
}

// Summary prints the final summary of cmd_import: clone (scope clone, not
// extend), extend, or full, between blank lines. rich is the gum card
// (rounded, color 78) with the detail lines dimmed; plain is the
// success/info lines.
func Summary(in Inputs, u Out, rich bool) {
	p := in.Profile
	u.Say("")
	switch {
	case in.Scope == "clone" && !in.Extend:
		if rich {
			u.Say(tui.Card(78, []string{
				"Clone profile '" + p + "' ready",
				tui.Faint("Clone:    ctx clone -p " + p + " git@github.com:ORG/REPO.git"),
				tui.Faint("Or:       git clone git@github-" + p + ":ORG/REPO.git"),
				tui.Faint("Extend:   ctx setup --extend " + p + "  (git, folder, cloud, secrets)"),
				tui.Faint("Verify:   ctx verify " + p),
			}))
		} else {
			u.Success("Clone profile '" + p + "' ready.")
			u.Info("Clone:  ctx clone -p " + p + " git@github.com:ORG/REPO.git")
			u.Info("Or:     git clone git@github-" + p + ":ORG/REPO.git")
			u.Info("Later:  ctx setup --extend " + p)
			u.Info("Verify: ctx verify " + p)
		}
	case in.Extend:
		u.Success("Profile '" + p + "' updated.")
		u.Info("Check:  ctx status")
		u.Info("Verify: ctx verify " + p)
	default:
		wd := in.WorkDir
		if wd == "" {
			wd = "its folder"
		}
		note := "Outside " + wd + ", git@github.com won't use this profile's key."
		id := os.Getenv("HOME") + "/.config/git/ctx-" + p
		if git(nil, nil, "config", "--file", id, "--get-all", "url.git@github-"+p+":.insteadOf") == nil {
			note = "Inside " + in.WorkDir + ", plain git@github.com URLs use this profile's key too."
		}
		if rich {
			u.Say(tui.Card(78, []string{
				"Profile '" + p + "' created",
				tui.Faint("Activate: ctx use " + p),
				tui.Faint("Check:    ctx status"),
				tui.Faint("Clone:    git clone git@github-" + p + ":OWNER/REPO.git"),
				tui.Faint("          " + note),
				tui.Faint("Verify:   ctx verify " + p),
				tui.Faint("Issues:   ctx doctor"),
			}))
		} else {
			b, r, g := "", "", ""
			if colors(u) {
				b, r, g = ansiBold, ansiReset, ansiGreen
			}
			u.Success("Profile '" + b + p + r + g + "' created.")
			u.Info("Activate: ctx use " + p)
			u.Info("Check:    ctx status")
			u.Info("Clone:    git clone git@github-" + p + ":OWNER/REPO.git")
			u.Dim("  " + note)
			u.Info("Verify:   ctx verify " + p)
			u.Info("Issues:   ctx doctor")
		}
	}
	u.Say("")
}
