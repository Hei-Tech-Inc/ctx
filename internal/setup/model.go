// Package setup is the pure, prompt-free core of `ctx setup`: the wizard's
// data model, the validators ported from lib/core.sh and the --config reader.
package setup

import "io"

// Secret is a key/value queued during the wizard and stored only after the
// review is confirmed.
type Secret struct{ Key, Value string }

// EnvPair is a non-secret KEY=value written to the work dir's mise.toml.
type EnvPair struct{ Key, Value string }

// Inputs is everything the wizard collects before anything is written.
type Inputs struct {
	Scope                               string // "clone" or "full"
	Profile, GitName, GitEmail, WorkDir string
	ImportExisting                      bool
	SSHKeyPath                          string
	SSHKeyGenerated                     bool // a key was (or, in dry-run, would be) generated
	NeedsKeygen                         bool // Resolve only (wizards leave it false): ssh-keygen must run for SSHKeyPath
	NeedsMkdirWorkDir                   bool // Resolve only (wizards leave it false): mkdir -p WorkDir must run
	GitHubUser, AWSProfile              string
	AzureSub, AzureTenant               string
	GCPProject, GCPAccount, KubeContext string
	SecretKeys                          []string
	PendingSecrets                      []Secret
	ExtraEnvs                           []EnvPair
	// ExtraEnvsRaw is EXTRA_ENVS as an extended profile has it, kept byte for
	// byte; ExtraEnvs are appended to it (Bash: EXTRA_ENVS+="K=V"$'\n').
	ExtraEnvsRaw               string
	SigningFormat, SigningKey  string
	Overwrite, AutoYes, Extend bool
}

// Out is the styled output the setup code needs. internal/cli adapts it to
// its Bash-identical ui helpers. Error is Bash's error (stderr); dies are
// returned as *DieError for the caller to print.
type Out interface {
	Info(string)
	Success(string)
	Warn(string)
	Dim(string)
	Bold(string)
	Say(string)   // plain echo "$s"
	Error(string) // ✗ line on stderr
}

// Terminal is optionally implemented by an Out. Colors reports whether ANSI
// colors are on (Bash's [[ -t 1 ]]; the full summary embeds BOLD/RESET/GREEN
// in its message). Streams is where external tools (git) write their own
// output, as they inherit the Bash process's stdout and stderr. Without it
// there are no colors and tools write to os.Stdout and os.Stderr.
type Terminal interface {
	Colors() bool
	Streams() (stdout, stderr io.Writer)
}
