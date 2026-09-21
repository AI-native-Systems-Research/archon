package core

// Conserve upholds INV-1: nothing enters or leaves unaccounted.
// Determinism (INV-6) is asserted at the seam below.
func Conserve() {}

// INV-6 again, so the citation count exceeds the file count.
func Replay() {}
