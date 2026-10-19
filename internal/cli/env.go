package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

// ExitError is a failure that carries its own exit code and complete message
// (printed as is, without the "✗ " prefix that plain errors get).
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

const (
	envUsage = "Usage: ctx env [-p profile] [--fish] [--secrets]"
	runUsage = "Usage: ctx run [-p profile] -- <command> [args...]"
)

// isValidEnvKey mirrors is_valid_env_key: ^[A-Za-z_][A-Za-z0-9_]*$.
func isValidEnvKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// bashQuote is printf %q for the value of GIT_SSH_COMMAND's key path.
func bashQuote(s string) string {
	if s == "" {
		return "''"
	}
	ctrl := false
	for _, c := range []byte(s) {
		if c < 0x20 || c == 0x7f {
			ctrl = true
		}
	}
	var b strings.Builder
	if ctrl {
		b.WriteString("$'")
		for _, c := range []byte(s) {
			switch {
			case c == '\n':
				b.WriteString(`\n`)
			case c == '\t':
				b.WriteString(`\t`)
			case c == '\r':
				b.WriteString(`\r`)
			case c == '\\' || c == '\'':
				b.WriteByte('\\')
				b.WriteByte(c)
			case c < 0x20 || c == 0x7f:
				fmt.Fprintf(&b, `\%03o`, c)
			default:
				b.WriteByte(c)
			}
		}
		b.WriteByte('\'')
		return b.String()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80 || strings.IndexByte("_@%+=:,./-", c) >= 0) {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// GHToken runs `gh auth token -u user` (5 s limit). Any failure, or gh not
// being on PATH, means no token.
func GHToken(user string) string {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, gh, "auth", "token", "-u", user).Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// CollectEnv returns one process's identity for p as ordered KEY/value pairs,
// matching _ctx_profile_env_collect in lib/cmd_ops.sh.
func CollectEnv(p *profile.Profile, withSecrets bool, g secret.Getter, ghToken func(user string) string) ([][2]string, error) {
	var out [][2]string
	add := func(k, v string) { out = append(out, [2]string{k, v}) }
	add("CTX_ACTIVE_PROFILE", p.Name)
	if p.GitName != "" {
		add("GIT_AUTHOR_NAME", p.GitName)
		add("GIT_COMMITTER_NAME", p.GitName)
	}
	if p.GitEmail != "" {
		add("GIT_AUTHOR_EMAIL", p.GitEmail)
		add("GIT_COMMITTER_EMAIL", p.GitEmail)
	}
	if p.SSHKeyPath != "" {
		if st, err := os.Stat(p.SSHKeyPath); err == nil && st.Mode().IsRegular() {
			add("GIT_SSH_COMMAND", "ssh -i "+bashQuote(p.SSHKeyPath)+" -o IdentitiesOnly=yes")
		}
	}
	if p.GithubUser != "" && ghToken != nil {
		if tok := ghToken(p.GithubUser); tok != "" {
			add("GH_TOKEN", tok)
		}
	}
	if p.AWSProfile != "" {
		add("AWS_PROFILE", p.AWSProfile)
	}
	if p.GCPProject != "" {
		add("CLOUDSDK_CORE_PROJECT", p.GCPProject)
		add("GOOGLE_CLOUD_PROJECT", p.GCPProject)
	}
	if p.GCPAccount != "" {
		add("CLOUDSDK_CORE_ACCOUNT", p.GCPAccount)
	}
	if p.AzureSubscription != "" {
		add("AZURE_SUBSCRIPTION", p.AzureSubscription)
		add("AZURE_SUBSCRIPTION_ID", p.AzureSubscription)
	}
	if p.AzureTenant != "" {
		add("AZURE_TENANT_ID", p.AzureTenant)
	}
	// Lines are used as written (no trimming), like `IFS= read -r`; a line
	// without "=" is both key and value.
	for _, line := range strings.Split(p.ExtraEnvsRaw, "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			v = line
		}
		if isValidEnvKey(k) {
			add(k, v)
		}
	}
	if withSecrets {
		for _, k := range p.SecretKeys {
			if !isValidEnvKey(k) {
				continue
			}
			v, err := g.Get(p.Name, k)
			if err != nil {
				continue
			}
			if v = strings.TrimRight(v, "\n"); v != "" {
				add(k, v)
			}
		}
	}
	return out, nil
}

// shellWord quotes v for POSIX sh: bare when it only has plain characters
// (as printf %q does), else single-quoted with ' written as '\”.
func shellWord(v string) string {
	plain := v != ""
	for i := 0; i < len(v) && plain; i++ {
		c := v[i]
		plain = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_@%+=:,./-", c) >= 0
	}
	if plain {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// FormatEnv renders pairs for eval in bash or fish.
func FormatEnv(pairs [][2]string, shell string) string {
	var b strings.Builder
	for _, kv := range pairs {
		if shell == "fish" {
			v := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(kv[1])
			fmt.Fprintf(&b, "set -gx %s '%s'\n", kv[0], strings.TrimRight(v, "\n"))
		} else {
			fmt.Fprintf(&b, "export %s=%s\n", kv[0], shellWord(kv[1]))
		}
	}
	return b.String()
}

// loadForCommand mirrors _ctx_cmd_load_profile: it parses [-p NAME] and loads
// the profile (flag, else the folder's, else the active one).
func loadForCommand(args []string) (*profile.Profile, []string, error) {
	p := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p", "--profile":
			p = ""
			if i+1 < len(args) {
				p = args[i+1]
			}
			i++
		case "--":
			rest = append(rest, args[i+1:]...)
			i = len(args)
		default:
			rest = append(rest, args[i])
		}
	}
	dir := profile.Dir()
	if p == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		p, _, _ = resolve.Path(pwd, dir)
		if p == "" {
			if cfg, err := ctxconfig.Load(); err == nil {
				p = cfg.Active
			}
		}
		if p == "" {
			return nil, nil, fmt.Errorf("No profile for %s and none active. Use -p <profile>.", pwd) //nolint:staticcheck // copied from Bash
		}
	}
	if st, err := os.Stat(filepath.Join(dir, p+".conf")); err != nil || !st.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("Profile '%s' not found. Run: ctx list", p) //nolint:staticcheck // copied from Bash
	}
	prof, err := profile.Load(dir, p)
	if err != nil {
		return nil, nil, err
	}
	return prof, rest, nil
}

func newGetter() secret.Getter { return secret.CLIGetter{CtxBin: os.Getenv("CTX_BIN_PATH")} }

// Env prints exports for eval: ctx env [-p NAME] [--fish] [--secrets].
func Env(w io.Writer, args []string) error {
	prof, rest, err := loadForCommand(args)
	if err != nil {
		return err
	}
	shell, secrets := "bash", false
	for _, a := range rest {
		switch a {
		case "--fish":
			shell = "fish"
		case "--secrets":
			secrets = true
		default:
			return fmt.Errorf("%s", envUsage)
		}
	}
	pairs, err := CollectEnv(prof, secrets, newGetter(), GHToken)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, FormatEnv(pairs, shell))
	return err
}

// Run replaces this process with the command, running with the profile's
// identity (secrets included): ctx run [-p NAME] -- <command> [args...].
// It returns only on failure.
func Run(args []string) error {
	prof, rest, err := loadForCommand(args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("%s", runUsage)
	}
	pairs, err := CollectEnv(prof, true, newGetter(), GHToken)
	if err != nil {
		return err
	}
	env := mergeEnv(os.Environ(), pairs)
	path, xerr := lookCommand(rest[0])
	if xerr != nil {
		return xerr
	}
	if err := syscall.Exec(path, rest, env); err != nil {
		code := 126
		if os.IsNotExist(err) {
			code = 127
		}
		return &ExitError{Code: code, Msg: fmt.Sprintf("ctx: %s: %v", rest[0], err)}
	}
	return nil
}

// lookCommand resolves name like Bash's exec, with the same exit codes
// (127 not found, 126 not executable).
func lookCommand(name string) (string, error) {
	if !strings.Contains(name, "/") {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", &ExitError{Code: 127, Msg: fmt.Sprintf("ctx: exec: %s: not found", name)}
		}
		return path, nil
	}
	st, err := os.Stat(name)
	switch {
	case err != nil && os.IsNotExist(err):
		return "", &ExitError{Code: 127, Msg: fmt.Sprintf("ctx: %s: No such file or directory", name)}
	case err != nil:
		return "", &ExitError{Code: 126, Msg: fmt.Sprintf("ctx: %s: Permission denied", name)}
	case st.IsDir():
		return "", &ExitError{Code: 126, Msg: fmt.Sprintf("ctx: %s: Is a directory", name)}
	case st.Mode()&0o111 == 0:
		return "", &ExitError{Code: 126, Msg: fmt.Sprintf("ctx: %s: Permission denied", name)}
	}
	return name, nil
}

// mergeEnv returns base with pairs set, replacing existing entries of the same key.
func mergeEnv(base []string, pairs [][2]string) []string {
	set := map[string]bool{}
	for _, kv := range pairs {
		set[kv[0]] = true
	}
	out := make([]string, 0, len(base)+len(pairs))
	for _, e := range base {
		if k, _, _ := strings.Cut(e, "="); !set[k] {
			out = append(out, e)
		}
	}
	// Later duplicates win in Bash (each export overwrites); apply in order.
	last := map[string]string{}
	for _, kv := range pairs {
		last[kv[0]] = kv[1]
	}
	seen := map[string]bool{}
	for _, kv := range pairs {
		if !seen[kv[0]] {
			seen[kv[0]] = true
			out = append(out, kv[0]+"="+last[kv[0]])
		}
	}
	return out
}
