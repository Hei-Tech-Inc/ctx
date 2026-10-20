package setup

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ConfigFile holds a setup --config file: KEY=value lines, last KEY wins.
type ConfigFile map[string]string

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func trimSpace(s string) string {
	for len(s) > 0 && isSpace(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && isSpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

func isKeyChar(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}

// ReadConfig parses path with the semantics of ctx_setup_cfg_get: a line is
// `[ws]KEY=value`; the last occurrence wins; the value is trimmed and one
// pair of matching surrounding quotes is removed. A path that is not a regular
// file fails with the Bash text "setup --config: not a file: PATH".
func ReadConfig(path string) (ConfigFile, error) {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return nil, &DieError{Msg: "setup --config: not a file: " + path}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := ConfigFile{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimLeft(line, " \t\n\v\f\r")
		i := 0
		for i < len(line) && isKeyChar(line[i]) {
			i++
		}
		if i == 0 || i >= len(line) || line[i] != '=' {
			continue
		}
		val := trimSpace(line[i+1:])
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		c[line[:i]] = val
	}
	return c, nil
}

// Get returns the value of key, or def when it is missing or empty.
func (c ConfigFile) Get(key, def string) string {
	if v := c[key]; v != "" {
		return v
	}
	return def
}

// Truthy is ctx_setup_cfg_truthy: 1, true, yes, y, on (any case).
func Truthy(s string) bool {
	switch lowerASCII(s) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

// DieError is a message Bash prints with die (or _dsetup_die when Hint is
// set, which also prints the "ctx doctor" hint). The caller prints it.
type DieError struct {
	Msg  string
	Hint bool
}

func (e *DieError) Error() string { return e.Msg }

func die(format string, a ...any) error { return &DieError{Msg: fmt.Sprintf(format, a...)} }

// BatchOpts is the environment Preconditions and Resolve need, injected so
// they stay pure.
type BatchOpts struct {
	AutoYes    bool // --yes
	DryRun     bool // CTX_DRY_RUN=1
	PWD, Home  string
	WorkRoot   string                    // ctxconfig.WorkRoot()
	StdinIsTTY bool                      // the "needs --yes" check
	Exists     func(profile string) bool // profile_exists; required by Resolve
	FileExists func(path string) bool    // [[ -f path ]]; required by Resolve
}

func expandTilde(v, home string) string {
	if strings.HasPrefix(v, "~") {
		return home + v[1:]
	}
	return v
}

// Preconditions ports the start of cmd_import's batch block: the SETUP_SCOPE
// check and the "needs --yes" check. Bash prints the "Non-interactive setup
// from: F (scope: S)" line between the two, so scope is returned whenever
// SETUP_SCOPE is valid, even together with the needs-yes error: the caller
// prints that line when scope != "" and then reports err. autoYes is --yes or
// AUTO_CONFIRM. Errors are *DieError with the verbatim Bash text.
func Preconditions(c ConfigFile, o BatchOpts) (scope string, autoYes bool, err error) {
	scope = c.Get("SETUP_SCOPE", "full")
	if scope != "clone" && scope != "full" {
		return "", false, die("setup --config: SETUP_SCOPE must be clone or full")
	}
	autoYes = o.AutoYes || Truthy(c.Get("AUTO_CONFIRM", "false"))
	if !autoYes && !o.StdinIsTTY {
		return scope, false, die("setup --config on non-interactive stdin needs --yes or AUTO_CONFIRM=true")
	}
	return scope, autoYes, nil
}

// Resolve ports the rest of the batch block (after the scan): it validates c
// and resolves Inputs without touching the filesystem. Side effects Bash
// performs once every check has passed (R17) are reported instead:
// Inputs.NeedsMkdirWorkDir (mkdir -p WorkDir) and Inputs.NeedsKeygen
// (ssh-keygen for SSHKeyPath); the caller performs them in that order.
// It assumes Preconditions passed. o.Exists and o.FileExists are required.
// Errors are *DieError carrying the verbatim Bash text.
func Resolve(c ConfigFile, o BatchOpts) (Inputs, error) {
	var in Inputs
	if o.Exists == nil || o.FileExists == nil {
		return in, errors.New("setup: BatchOpts.Exists and BatchOpts.FileExists are required")
	}
	scope := c.Get("SETUP_SCOPE", "full")
	clone := scope == "clone"
	in.Scope = scope
	in.AutoYes = o.AutoYes || Truthy(c.Get("AUTO_CONFIRM", "false"))

	name := c.Get("PROFILE_NAME", "")
	if name == "" {
		return in, die("setup --config: PROFILE_NAME is required")
	}
	in.Profile = Slug(name)
	if o.Exists(in.Profile) {
		if !Truthy(c.Get("OVERWRITE", "false")) {
			return in, die("setup --config: profile '%s' exists — set OVERWRITE=true to replace", in.Profile)
		}
		in.Overwrite = true
	}
	in.GitName = c.Get("GIT_NAME", "")
	in.GitEmail = trimSpace(c.Get("GIT_EMAIL", ""))
	if !clone {
		if in.GitName == "" {
			return in, die("setup --config: GIT_NAME is required (or SETUP_SCOPE=clone)")
		}
		if in.GitEmail != "" && !SensibleEmail(in.GitEmail) {
			return in, die("setup --config: GIT_EMAIL must look like user@domain.tld")
		}
	}

	in.WorkDir = expandTilde(c.Get("WORK_DIR", ""), o.Home)
	if in.WorkDir == "" {
		wm := c.Get("WORK_MODE", "")
		if !(clone && wm == "") {
			if wm == "" {
				wm = "new"
			}
			switch wm {
			case "current", "pwd":
				in.WorkDir = o.PWD
			case "path", "existing":
				ep := expandTilde(c.Get("EXISTING_PATH", ""), o.Home)
				if ep == "" {
					return in, die("setup --config: EXISTING_PATH required when WORK_MODE=path")
				}
				in.WorkDir = ep
			case "new", "client":
				slug := FolderSlug(c.Get("WORK_SLUG", in.Profile))
				if slug == "" {
					return in, die("setup --config: WORK_SLUG invalid for WORK_MODE=new")
				}
				in.WorkDir = o.WorkRoot + "/" + slug
			default:
				return in, die("setup --config: set WORK_DIR=... or WORK_MODE=current|path|new")
			}
		}
	}
	if in.WorkDir != "" {
		in.NeedsMkdirWorkDir = !o.DryRun
	} else if !clone {
		return in, die("setup --config: could not resolve WORK_DIR (or use SETUP_SCOPE=clone without WORK_DIR)")
	}

	in.ImportExisting = c.Get("IMPORT_MODE", "auto") != "manual"

	fileExists := o.FileExists
	switch c.Get("SSH_MODE", "generate") {
	case "generate":
		in.SSHKeyPath = o.Home + "/.ssh/ctx_" + in.Profile
		if o.DryRun {
			in.SSHKeyGenerated = true
		} else if !fileExists(in.SSHKeyPath) {
			in.NeedsKeygen = true
			in.SSHKeyGenerated = true
		}
	case "existing":
		in.SSHKeyPath = expandTilde(c.Get("SSH_KEY_PATH", ""), o.Home)
		if !o.DryRun && !fileExists(in.SSHKeyPath) {
			return in, &DieError{Msg: "setup --config: SSH_KEY_PATH not found: " + in.SSHKeyPath, Hint: true}
		}
	case "skip", "none", "":
		if clone {
			return in, die("setup --config: SETUP_SCOPE=clone requires SSH_MODE=generate or existing (not skip)")
		}
		in.SSHKeyPath = ""
	default:
		return in, die("setup --config: SSH_MODE must be generate|existing|skip")
	}

	in.GitHubUser = c.Get("GITHUB_USER", "")
	in.AWSProfile = c.Get("AWS_PROFILE_NAME", "")
	in.AzureSub = c.Get("AZURE_SUBSCRIPTION", "")
	in.AzureTenant = c.Get("AZURE_TENANT", "")
	in.GCPProject = c.Get("GCP_PROJECT", "")
	in.GCPAccount = c.Get("GCP_ACCOUNT", "")
	in.KubeContext = c.Get("KUBE_CONTEXT", "")
	sf, err := NormalizeSigning(c.Get("SIGNING_FORMAT", ""))
	if err != nil {
		return in, die("setup --config: SIGNING_FORMAT must be ssh|gpg|none")
	}
	in.SigningFormat = sf
	in.SigningKey = expandTilde(c.Get("SIGNING_KEY", ""), o.Home)
	if in.SigningFormat == "openpgp" && in.SigningKey == "" {
		return in, die("setup --config: SIGNING_FORMAT=gpg needs SIGNING_KEY (GPG key ID)")
	}

	switch c.Get("SECRETS_MODE", "skip") {
	case "skip", "none", "":
	default:
		return in, die("setup --config: only SECRETS_MODE=skip — add secrets later with: ctx secret set %s <KEY>", in.Profile)
	}
	return in, nil
}
