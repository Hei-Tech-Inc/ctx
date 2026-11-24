package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Hei-Tech-Inc/ctx/internal/tui"
)

type diagnosticCheck struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type diagnosticUI struct {
	ui
	checks *[]diagnosticCheck
}

func (u diagnosticUI) success(msg string) {
	if u.checks == nil {
		u.ui.success(msg)
		return
	}
	*u.checks = append(*u.checks, diagnosticCheck{"pass", msg})
}

func (u diagnosticUI) warn(msg string) {
	if u.checks == nil {
		u.ui.warn(msg)
		return
	}
	*u.checks = append(*u.checks, diagnosticCheck{"warning", msg})
}

func (u diagnosticUI) die(msg string) int {
	if u.checks == nil {
		return u.ui.die(msg)
	}
	*u.checks = append(*u.checks, diagnosticCheck{"error", msg})
	return 1
}

func runDiagnostic(t tui.Term, name string, run func(diagnosticUI) int) int {
	if os.Getenv("CTX_JSON") != "1" {
		return run(diagnosticUI{ui: termUI(t)})
	}
	checks := []diagnosticCheck{}
	code := run(diagnosticUI{ui: ui{out: io.Discard, err: io.Discard}, checks: &checks})
	result := struct {
		Version int               `json:"version"`
		Command string            `json:"command"`
		Checks  []diagnosticCheck `json:"checks"`
	}{1, name, checks}
	if err := json.NewEncoder(t.Out).Encode(result); err != nil {
		fmt.Fprintln(t.Err, err)
		return 1
	}
	return code
}
