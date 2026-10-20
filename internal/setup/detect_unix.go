//go:build unix

package setup

import (
	"os/exec"
	"syscall"
)

// prepareKill puts the tool in its own process group and, on timeout, kills
// the whole group so a hung grandchild does not keep the output pipe open.
func prepareKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
