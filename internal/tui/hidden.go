package tui

import (
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// errNoTermios means the platform has no termios support here; the caller
// falls back to term.ReadPassword.
var errNoTermios = errors.New("termios unsupported")

// readSecretTTY reads a hidden line from terminal fd. It turns off echo,
// canonical mode and signal keys, so Ctrl+C arrives as a byte and becomes
// ErrCancelled instead of killing the process with echo still off. The
// original terminal state is restored on every return.
func readSecretTTY(fd int, in io.Reader) (string, error) {
	old, err := ttyGet(fd)
	if err != nil {
		return "", err
	}
	if err := ttySet(fd, noEcho(*old)); err != nil {
		return "", err
	}
	defer func() { _ = ttySet(fd, old) }()
	return readHidden(in)
}

// readHidden is the line editor for readSecretTTY. Enter (CR or LF) ends the
// line; DEL/BS erase a rune; Ctrl+C, Ctrl+D on an empty line and EOF cancel.
// The result is trimmed of spaces and tabs, like `read -rs`.
func readHidden(in io.Reader) (string, error) {
	var line []byte
	var buf [1]byte
	for {
		n, err := in.Read(buf[:])
		if n == 0 {
			if err != nil {
				return "", ErrCancelled
			}
			continue
		}
		switch b := buf[0]; b {
		case '\r', '\n':
			return strings.Trim(string(line), " \t"), nil
		case 0x03: // Ctrl+C
			return "", ErrCancelled
		case 0x04: // Ctrl+D
			if len(line) == 0 {
				return "", ErrCancelled
			}
		case 0x7f, 0x08: // DEL, BS
			if len(line) > 0 {
				_, size := utf8.DecodeLastRune(line)
				line = line[:len(line)-size]
			}
		default:
			line = append(line, b)
		}
	}
}
