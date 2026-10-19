// Package profile reads ctx profile .conf files (written by lib/cmd_import.sh _write_all).
package profile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Profile is one ctx profile. Name is the file basename, not PROFILE_NAME.
type Profile struct {
	Name, GitName, GitEmail, WorkDir, GithubUser, SSHKeyPath, AWSProfile string
	AzureSubscription, AzureTenant, GCPProject, GCPAccount, KubeContext  string
	SigningFormat, SigningKey                                            string
	SecretKeys                                                           []string
	// SecretKeysRaw is SECRET_KEYS exactly as the profile sets it (SecretKeys is its fields).
	SecretKeysRaw string
	ExtraEnvs     map[string]string
	// ExtraEnvsRaw is EXTRA_ENVS exactly as the profile sets it (ordered, duplicates kept).
	ExtraEnvsRaw string
}

var keys = []string{
	"PROFILE_NAME", "GIT_NAME", "GIT_EMAIL", "WORK_DIR", "GITHUB_USER", "SSH_KEY_PATH",
	"AWS_PROFILE_NAME", "AZURE_SUBSCRIPTION", "AZURE_TENANT", "GCP_PROJECT", "GCP_ACCOUNT",
	"KUBE_CONTEXT", "SIGNING_FORMAT", "SIGNING_KEY", "SECRET_KEYS", "EXTRA_ENVS",
}

// Dir returns $CTX_DIR/profiles, defaulting to $HOME/.ctx/profiles.
func Dir() string {
	if d := os.Getenv("CTX_DIR"); d != "" {
		return filepath.Join(d, "profiles")
	}
	return filepath.Join(os.Getenv("HOME"), ".ctx", "profiles")
}

// List returns the sorted basenames (without .conf) of profiles in dir.
func List(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".conf") {
			out = append(out, strings.TrimSuffix(e.Name(), ".conf"))
		}
	}
	sort.Strings(out)
	return out, nil
}

// Load reads dir/name.conf.
func Load(dir, name string) (*Profile, error) {
	path := filepath.Join(dir, name+".conf")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile %q: %w", name, err)
	}
	vals := parse(string(data))
	if needsBash(vals) {
		if bv, ok := bashValues(path); ok {
			vals = bv
			vals["WORK_DIR"] = expandHome(vals["WORK_DIR"], os.Getenv("HOME"))
		}
	}
	p := &Profile{
		Name: name, GitName: vals["GIT_NAME"], GitEmail: vals["GIT_EMAIL"], WorkDir: vals["WORK_DIR"],
		GithubUser: vals["GITHUB_USER"], SSHKeyPath: vals["SSH_KEY_PATH"], AWSProfile: vals["AWS_PROFILE_NAME"],
		AzureSubscription: vals["AZURE_SUBSCRIPTION"], AzureTenant: vals["AZURE_TENANT"],
		GCPProject: vals["GCP_PROJECT"], GCPAccount: vals["GCP_ACCOUNT"], KubeContext: vals["KUBE_CONTEXT"],
		SigningFormat: vals["SIGNING_FORMAT"], SigningKey: vals["SIGNING_KEY"],
		SecretKeys: strings.Fields(vals["SECRET_KEYS"]), SecretKeysRaw: vals["SECRET_KEYS"],
		ExtraEnvs: map[string]string{}, ExtraEnvsRaw: vals["EXTRA_ENVS"],
	}
	for _, line := range strings.Split(vals["EXTRA_ENVS"], "\n") {
		line = strings.TrimSpace(line)
		if k, v, ok := strings.Cut(line, "="); ok && k != "" {
			p.ExtraEnvs[k] = v
		}
	}
	return p, nil
}

func needsBash(vals map[string]string) bool {
	for _, v := range vals {
		if strings.ContainsAny(v, "$`\\") {
			return true
		}
	}
	return false
}

func bashValues(path string) (map[string]string, bool) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return nil, false
	}
	script := `unset ` + strings.Join(keys, " ") + `; source "$1" >/dev/null 2>&1; for k in ` + strings.Join(keys, " ") +
		`; do printf "%s\0%s\0" "$k" "${!k}"; done`
	out, err := exec.Command(bash, "-c", script, "_", path).Output()
	if err != nil {
		return nil, false
	}
	parts := bytes.Split(out, []byte{0})
	vals := map[string]string{}
	for i := 0; i+1 < len(parts); i += 2 {
		if v := string(parts[i+1]); v != "" {
			vals[string(parts[i])] = v
		}
	}
	return vals, true
}

func isKey(k string) bool {
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

// parse reads KEY=value assignments (bare, 'single' or "double" quoted, quoted values may
// span lines); the last assignment wins. Only known keys are kept.
func parse(src string) map[string]string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	known := map[string]bool{}
	for _, k := range keys {
		known[k] = true
	}
	vals := map[string]string{}
	home := os.Getenv("HOME")
	for len(src) > 0 {
		line := src
		if i := strings.IndexByte(src, '\n'); i >= 0 {
			line, src = src[:i], src[i+1:]
		} else {
			src = ""
		}
		line = strings.TrimLeft(line, " \t")
		k, rest, ok := strings.Cut(line, "=")
		if !ok || !isKey(k) {
			continue
		}
		var v string
		quote := byte(0)
		if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
			quote = rest[0]
			buf := rest[1:]
			var sb strings.Builder
			for {
				end := -1
				for j := 0; j < len(buf); j++ {
					if quote == '"' && buf[j] == '\\' && j+1 < len(buf) {
						j++
						continue
					}
					if buf[j] == quote {
						end = j
						break
					}
				}
				if end >= 0 {
					sb.WriteString(buf[:end])
					break
				}
				sb.WriteString(buf)
				if src == "" {
					break
				}
				sb.WriteByte('\n')
				i := strings.IndexByte(src, '\n')
				if i < 0 {
					buf, src = src, ""
				} else {
					buf, src = src[:i], src[i+1:]
				}
			}
			v = sb.String()
		} else {
			v = strings.TrimRight(rest, " \t")
		}
		if quote != '\'' {
			v = expandHome(v, home)
		}
		if known[k] {
			vals[k] = v
		}
	}
	return vals
}

func expandHome(v, home string) string {
	for _, p := range []string{"${HOME}", "$HOME"} {
		if v == p || strings.HasPrefix(v, p+"/") {
			return home + v[len(p):]
		}
	}
	if v == "~" || strings.HasPrefix(v, "~/") {
		return home + v[1:]
	}
	return v
}
