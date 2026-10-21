//go:build !darwin && !linux

package tui

type ttyState struct{}

var (
	ttyGet = func(int) (*ttyState, error) { return nil, errNoTermios }
	ttySet = func(int, *ttyState) error { return errNoTermios }
)

func noEcho(s ttyState) *ttyState    { return &s }
func noEchoApplied(s *ttyState) bool { return true }
