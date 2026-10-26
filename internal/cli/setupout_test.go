package cli

import (
	"bytes"
	"testing"
)

func TestSetupOutMatchesUI(t *testing.T) {
	var a, b bytes.Buffer
	ua := ui{out: &a, err: &a}
	ub := ui{out: &b, err: &b}
	o := ua.setupOut()
	o.Info("i")
	o.Success("s")
	o.Warn("w")
	o.Dim("d")
	o.Bold("b")
	ub.info("i")
	ub.success("s")
	ub.warn("w")
	ub.dim("d")
	ub.bold("b")
	if a.String() != b.String() || a.Len() == 0 {
		t.Errorf("adapter bytes differ: %q vs %q", a.String(), b.String())
	}
}
