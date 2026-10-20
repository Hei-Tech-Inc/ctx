// Package setup is the pure, prompt-free core of `ctx setup`: the wizard's
// data model, the validators ported from lib/core.sh and the --config reader.
package setup

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
	NeedsKeygen                         bool // FromConfig only: ssh-keygen must run for SSHKeyPath
	NeedsMkdirWorkDir                   bool // FromConfig only: mkdir -p WorkDir must run
	GitHubUser, AWSProfile              string
	AzureSub, AzureTenant               string
	GCPProject, GCPAccount, KubeContext string
	SecretKeys                          []string
	PendingSecrets                      []Secret
	ExtraEnvs                           []EnvPair
	SigningFormat, SigningKey           string
	Overwrite, AutoYes, Extend          bool
}

// Out is the styled output the setup code needs. internal/cli adapts it to
// its Bash-identical ui helpers; errors are printed by the caller.
type Out interface {
	Info(string)
	Success(string)
	Warn(string)
	Dim(string)
	Bold(string)
}
