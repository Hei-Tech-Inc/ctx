//go:build !darwin && !linux

package testutil

// loadAvg is not wired up here: RunPTY keeps its base gaps.
func loadAvg() (float64, bool) { return 0, false }
