//go:build !unix

package setup

import "os/exec"

// prepareKill is a no-op: the default context kill and WaitDelay apply.
func prepareKill(*exec.Cmd) {}
