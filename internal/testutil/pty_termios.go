//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package testutil

import (
	"os"

	"golang.org/x/sys/unix"
)

// echoOff turns off echo on terminal f, keeping line editing (ICANON) and the
// CR/NL translations, so answers are read as lines but not repeated in the
// transcript.
func echoOff(f *os.File) error {
	tio, err := unix.IoctlGetTermios(int(f.Fd()), ioctlGetTermios)
	if err != nil {
		return err
	}
	tio.Lflag &^= unix.ECHO | unix.ECHONL
	return unix.IoctlSetTermios(int(f.Fd()), ioctlSetTermios, tio)
}
