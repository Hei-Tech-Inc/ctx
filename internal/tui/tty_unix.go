//go:build darwin || linux

package tui

import "golang.org/x/sys/unix"

type ttyState = unix.Termios

// ttyGet and ttySet read and write the terminal state; tests replace them.
var (
	ttyGet = func(fd int) (*ttyState, error) { return unix.IoctlGetTermios(fd, ioctlGetTermios) }
	ttySet = func(fd int, s *ttyState) error { return unix.IoctlSetTermios(fd, ioctlSetTermios, s) }
)

const noEchoFlags = unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN

// noEcho returns s with echo, line editing and signal keys off, reading one
// byte at a time.
func noEcho(s ttyState) *ttyState {
	s.Lflag &^= noEchoFlags
	s.Cc[unix.VMIN] = 1
	s.Cc[unix.VTIME] = 0
	return &s
}

func noEchoApplied(s *ttyState) bool { return s.Lflag&noEchoFlags == 0 }
