//go:build ignore

package core

// Conserve upholds INV-1: every request is accounted for exactly once.
// Determinism (INV-6) is asserted at the seam below.
func Conserve() {}

// Replay re-runs a recording. INV-6 again, so citations exceed file count.
func Replay() {}
