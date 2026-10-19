package ctxconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bashReaders runs the Bash config readers from lib/core.sh against dir and
// returns active, source, manual_pwd, max depth, in that order.
func bashReaders(t *testing.T, dir string) []string {
	t.Helper()
	core, err := filepath.Abs("../../lib/core.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := `source "$1" >/dev/null 2>&1
printf '%s\0' "$(active_profile)"
printf '%s\0' "$(ctx_activation_get_source)"
printf '%s\0' "$(ctx_activation_get_manual_pwd || true)"
printf '%s\0' "$(ctx_config_prompt_workdir_max_depth)"
`
	cmd := exec.Command("bash", "-c", script, "bash", core)
	cmd.Env = append(os.Environ(), "CTX_DIR="+dir, "HOME="+dir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash readers: %v", err)
	}
	parts := strings.Split(string(out), "\x00")
	if len(parts) != 5 {
		t.Fatalf("bash output %q", out)
	}
	return parts[:4]
}

func TestConfigMatchesBashReaders(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	cases := []struct {
		name string
		body *string // nil: no config file
	}{
		{"missing", nil},
		{"empty", sp("")},
		{"simple", sp("active=alpha\nactive_source=Manual\nmanual_pwd=L3RtcC94\nprompt_workdir_max_depth=3\n")},
		{"duplicates", sp("active=a\nactive=b\nactive_source=manual\nactive_source=auto\nmanual_pwd=L2E=\nmanual_pwd=L2I=\nprompt_workdir_max_depth=1\nprompt_workdir_max_depth=4\n")},
		{"crlf", sp("active=alpha\r\nactive_source=manual\r\nmanual_pwd=L3RtcC94\r\nprompt_workdir_max_depth=5\r\n")},
		{"no trailing newline", sp("active=alpha\nprompt_workdir_max_depth=6")},
		{"bad depth", sp("prompt_workdir_max_depth=abc\n")},
		{"negative depth", sp("prompt_workdir_max_depth=-1\n")},
		{"spaced depth", sp("prompt_workdir_max_depth= 7 \n")},
		{"empty depth", sp("prompt_workdir_max_depth=\n")},
		{"equals in values", sp("active=a=b\nactive_source=x=y\nmanual_pwd=L3RtcC94==\n")},
		{"raw manual pwd", sp("active_source=manual\nmanual_pwd=a\n")},
		{"empty values", sp("active=\nactive_source=\nmanual_pwd=\n")},
		{"prefix lookalikes", sp("xactive=no\nactive_source_x=no\n active=no\nactive=yes\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.body != nil {
				if err := os.WriteFile(filepath.Join(dir, "config"), []byte(*tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CTX_DIR", dir)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			want := bashReaders(t, dir)
			got := []string{cfg.Active, cfg.ActiveSource, cfg.ManualPWD, strconv.Itoa(cfg.PromptWorkdirMaxDepth)}
			// Bash prints the depth verbatim ("007"); the integer value is what matters.
			if n, err := strconv.Atoi(want[3]); err == nil {
				want[3] = strconv.Itoa(n)
			}
			names := []string{"active", "active_source", "manual_pwd", "prompt_workdir_max_depth"}
			for i := range names {
				if got[i] != want[i] {
					t.Errorf("%s: go %q, bash %q", names[i], got[i], want[i])
				}
			}
		})
	}
}

func TestOtherKeys(t *testing.T) {
	dir := t.TempDir()
	body := "prompt_extra_paths=/a:/b\r\nprompt_extra_paths=/c:/d=e\r\nssh_agent_added=/k1\nssh_agent_added=/k2\r\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CTX_DIR", dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PromptExtraPaths != "/c:/d=e" || cfg.SSHAgentAdded != "/k2" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestPathDefaultsToHome(t *testing.T) {
	t.Setenv("CTX_DIR", "")
	t.Setenv("HOME", "/h")
	if got := Path(); got != "/h/.ctx/config" {
		t.Fatalf("Path() = %q", got)
	}
	cfg, err := Load()
	if err != nil || cfg.PromptWorkdirMaxDepth != 2 || cfg.ActiveSource != "auto" {
		t.Fatalf("missing file: %+v %v", cfg, err)
	}
}

func sp(s string) *string { return &s }
