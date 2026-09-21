package core

import "testing"

// TestINV1_Conservation is named for the conservation invariant.
func TestINV1_Conservation(t *testing.T) {}

// TestFooE2E must never be attributed to the lifecycle invariant. Matching an
// ID by its numeric part alone is the bare-number bug; this test cites no ID.
func TestFooE2E(t *testing.T) {}

// TestINV13_RunReplayParity is named for the run/replay parity invariant, and
// for no shorter-numbered one.
func TestINV13_RunReplayParity(t *testing.T) {}
