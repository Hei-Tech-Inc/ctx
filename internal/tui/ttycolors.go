package tui

import (
	"io"
	"os"
)

// TTYColors reports whether w is a terminal (not /dev/null); Bash colors its
// messages only then (`[[ -t 1 ]]`).
func TTYColors(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(st, null) {
		return false
	}
	return true
}
