//go:build !unix

package setup

import "os/exec"

// prepareKill is a no-op: the default context kill and WaitDelay apply.
func prepareKill(*exec.Cmd) {}

// killScan kills a started tool.
func killScan(cmd *exec.Cmd) error { return cmd.Process.Kill() }
