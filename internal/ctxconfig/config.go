// Package ctxconfig reads $CTX_DIR/config, the key=value file bin/ctx keeps
// its activation state in. Every reader matches the Bash helper of the same
// purpose in lib/core.sh, quirks included.
package ctxconfig

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultMaxDepth is prompt_workdir_max_depth when unset or invalid.
const DefaultMaxDepth = 2

// Config holds the keys ctx-core needs from the config file.
type Config struct {
	// Active mirrors active_profile: grep "^active=" | cut -d= -f2. Bash takes
	// every match (not the last), and cut -f2 stops at a second "=". With
	// duplicate lines the values are joined with "\n", exactly like the
	// command substitution in Bash; a CR from CRLF files is kept.
	Active string
	// ActiveSource is "manual" or "auto" (the default), like ctx_activation_get_source.
	ActiveSource string
	// ManualPWD is the decoded manual_pwd, like ctx_activation_get_manual_pwd.
	ManualPWD string
	// PromptExtraPaths is the last prompt_extra_paths value, CR stripped.
	PromptExtraPaths string
	// PromptWorkdirMaxDepth is prompt_workdir_max_depth, DefaultMaxDepth unless a non-negative integer.
	PromptWorkdirMaxDepth int
	// SSHAgentAdded is the last ssh_agent_added value, CR stripped.
	SSHAgentAdded string
	// SecretProvider is the last secret_provider value, raw (like ctx_secret_provider's
	// grep | tail -1 | cut -d= -f2-); normalizing is the caller's job.
	SecretProvider string
	// PromptWorkdirDepthText is the depth exactly as ctx_config_prompt_workdir_max_depth
	// prints it (digits as written, e.g. "007"), "2" when unset or invalid.
	PromptWorkdirDepthText string
	// Last holds the raw last value of every key (like grep | tail -1 | cut -d= -f2-).
	Last map[string]string
}

// Path returns $CTX_DIR/config, defaulting to $HOME/.ctx/config.
func Path() string {
	if d := os.Getenv("CTX_DIR"); d != "" {
		return filepath.Join(d, "config")
	}
	return filepath.Join(os.Getenv("HOME"), ".ctx", "config")
}

// Load reads Path(). A missing file yields the defaults and no error.
func Load() (*Config, error) {
	cfg := &Config{ActiveSource: "auto", PromptWorkdirMaxDepth: DefaultMaxDepth, PromptWorkdirDepthText: "2", Last: map[string]string{}}
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	var actives []string
	last := cfg.Last
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "active":
			// cut -d= -f2: the text between the first and second "=".
			f, _, _ := strings.Cut(v, "=")
			actives = append(actives, f)
		default:
			last[k] = v
		}
	}
	cfg.Active = strings.TrimRight(strings.Join(actives, "\n"), "\n")
	if strings.ToLower(last["active_source"]) == "manual" {
		cfg.ActiveSource = "manual"
	}
	cfg.ManualPWD = decodeManualPWD(last["manual_pwd"])
	cfg.PromptExtraPaths = stripCR(last["prompt_extra_paths"])
	cfg.SSHAgentAdded = stripCR(last["ssh_agent_added"])
	cfg.SecretProvider = last["secret_provider"]
	cfg.PromptWorkdirDepthText = depthText(last["prompt_workdir_max_depth"])
	cfg.PromptWorkdirMaxDepth = parseDepth(last["prompt_workdir_max_depth"])
	return cfg, nil
}

func stripCR(s string) string { return strings.ReplaceAll(s, "\r", "") }

// depthText mirrors the printed value of ctx_config_prompt_workdir_max_depth.
func depthText(v string) string {
	v = strings.ReplaceAll(stripCR(v), " ", "")
	if v == "" {
		return "2"
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return "2"
		}
	}
	return v
}

// parseDepth mirrors ctx_config_prompt_workdir_max_depth: drop CR and spaces,
// require digits only, else the default.
func parseDepth(v string) int {
	v = strings.ReplaceAll(stripCR(v), " ", "")
	if v == "" {
		return DefaultMaxDepth
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return DefaultMaxDepth
		}
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return DefaultMaxDepth
	}
	return n
}

// decodeManualPWD mirrors ctx_activation_get_manual_pwd: base64-decode, and
// fall back to the raw text when decoding fails or yields nothing.
func decodeManualPWD(raw string) string {
	if raw == "" {
		return ""
	}
	// base64(1) tools differ on padding strictness; accept any amount of it.
	if dec, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(raw, "=")); err == nil && len(dec) > 0 {
		return strings.TrimRight(string(dec), "\n")
	}
	return raw
}

// WorkRoot is `$(ctx_work_root)`: $CTX_WORK_ROOT, else work_root from the
// config, else $HOME/clients, with a leading "~" expanded.
func WorkRoot() string {
	configured := os.Getenv("CTX_WORK_ROOT")
	if configured == "" {
		if cfg, err := Load(); err == nil && cfg != nil {
			configured = cfg.Last["work_root"]
		}
	}
	if configured == "" {
		configured = os.Getenv("HOME") + "/clients"
	}
	if strings.HasPrefix(configured, "~") {
		configured = os.Getenv("HOME") + configured[1:]
	}
	return strings.TrimRight(echoText(configured), "\n")
}

// echoText is what `echo "$s"` prints (a value that is only echo options is
// swallowed by the builtin).
func echoText(s string) string {
	if len(s) > 1 && s[0] == '-' && strings.Trim(s[1:], "neE") == "" {
		if strings.Contains(s, "n") {
			return ""
		}
		return "\n"
	}
	return s + "\n"
}
