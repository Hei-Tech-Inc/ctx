//go:build !windows && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd && !linux

package testutil

import "os"

// echoOff is a no-op where termios is not wired up; answers then show up in
// the transcript.
func echoOff(*os.File) error { return nil }
