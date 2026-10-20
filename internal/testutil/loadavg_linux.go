//go:build linux

package testutil

import "golang.org/x/sys/unix"

// loadAvg returns the one-minute load average.
func loadAvg() (float64, bool) {
	var si unix.Sysinfo_t
	if err := unix.Sysinfo(&si); err != nil {
		return 0, false
	}
	return float64(si.Loads[0]) / (1 << unix.SI_LOAD_SHIFT), true
}
