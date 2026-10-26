//go:build darwin

package testutil

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// loadAvg returns the one-minute load average.
func loadAvg() (float64, bool) {
	// struct loadavg { fixpt_t ldavg[3]; long fscale; }: three uint32, padding, int64.
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil || len(b) < 24 {
		return 0, false
	}
	scale := binary.NativeEndian.Uint64(b[16:24])
	if scale == 0 {
		return 0, false
	}
	return float64(binary.NativeEndian.Uint32(b[0:4])) / float64(scale), true
}
