package tui

import (
	"io"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// spinColor is gum spin's default spinner color.
const spinColor = 212

// Spin runs fn while showing msg, like lib/core.sh's spin. Plain prints
// Bash's info line ("→ msg") to t.Out and then runs fn. Rich draws gum spin's
// dot spinner with msg on t.Err until fn returns, then clears the line, so
// nothing of it is left behind. fn must not write to the terminal itself.
// The cursor stays visible (R24): a SIGINT mid-spin skips the deferred
// cleanup and would otherwise leave the user's cursor hidden.
func Spin(t Term, msg string, fn func()) {
	if !t.Rich {
		_, _ = io.WriteString(t.Out, EchoE(t.c(cyan)+"→"+t.c(reset)+" "+msg))
		fn()
		return
	}
	frames := spinner.Dot.Frames
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(spinner.Dot.FPS)
		defer tick.Stop()
		for i := 0; ; i++ {
			_, _ = io.WriteString(t.Err, "\r")
			_, _ = lipgloss.Fprint(t.Err, Accent(spinColor, frames[i%len(frames)])+msg)
			_, _ = io.WriteString(t.Err, "\x1b[K")
			select {
			case <-done:
				return
			case <-tick.C:
			}
		}
	}()
	defer func() {
		close(done)
		<-stopped
		_, _ = io.WriteString(t.Err, "\r\x1b[K") // clear the line
	}()
	fn()
}
