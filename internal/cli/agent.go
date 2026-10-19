package cli

import (
	"os"
	"os/exec"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
)

// The SSH-agent helpers mirror _ctx_agent_* in lib/cmd_ops.sh. ctx only
// removes agent keys it added itself (ssh_agent_added in the config).

// agentAddedKey is _ctx_agent_added_key: the last ssh_agent_added value.
func agentAddedKey() string {
	cfg, err := ctxconfig.Load()
	if err != nil {
		return ""
	}
	return cfg.Last["ssh_agent_added"]
}

// agentHasKey is _ctx_agent_has_key: whether the agent holds the key whose
// public half is key+".pub" (compared by fingerprint).
func agentHasKey(key string) bool {
	if st, err := os.Stat(key + ".pub"); err != nil || !st.Mode().IsRegular() {
		return false
	}
	out, err := exec.Command("ssh-keygen", "-lf", key+".pub").Output()
	if err != nil && len(out) == 0 {
		return false
	}
	want := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) >= 2 {
			want[f[1]] = true
		}
	}
	if len(want) == 0 {
		return false
	}
	out, _ = exec.Command("ssh-add", "-l").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && want[f[1]] {
			return true
		}
	}
	return false
}

// agentDropAddedKey is _ctx_agent_drop_added_key: ssh-add -d the key ctx
// added, ignoring failure, then forget it.
func agentDropAddedKey() {
	key := agentAddedKey()
	if key == "" {
		return
	}
	_ = exec.Command("ssh-add", "-d", key).Run()
	_ = ctxconfig.Remove("ssh_agent_added")
}
