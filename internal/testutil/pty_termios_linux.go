//go:build linux

package testutil

import "golang.org/x/sys/unix"

const (
	ioctlGetTermios = unix.TCGETS
	ioctlSetTermios = unix.TCSETS
)
