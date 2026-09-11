package plan

import "github.com/AI-native-Systems-Research/archon/internal/graph"

// RatchetResult reports whether a PR moved toward or away from the plan.
type RatchetResult struct {
	Before int  `json:"before"`
	After  int  `json:"after"`
	OK     bool `json:"ok"`

	// Drift is the surface drift this change INTRODUCED — present at head, absent
	// at base. Ratchet already computes both distances and used to discard
	// everything but their totals, which is how a stale declared signature stayed
	// invisible. Standing drift is deliberately excluded: repeating it on every PR
	// forever is what trains a reviewer to skip the section. Never part of
	// Before/After — drift does not move the ratchet.
	Drift []SurfaceDrift `json:"surfaceDrift,omitempty"`
}

// Ratchet computes plan distance before and after a change and reports whether
// the distance did not increase. ok=true means the PR moved toward (or held)
// the plan; ok=false means it moved away. Panics if plan is nil.
func Ratchet(p, base, head *graph.Graph) RatchetResult {
	if p == nil {
		panic("plan: Ratchet called with nil plan graph")
	}
	if base == nil {
		base = &graph.Graph{}
	}
	if head == nil {
		head = &graph.Graph{}
	}
	before := Dist(p, base)
	after := Dist(p, head)
	return RatchetResult{
		Before: before.Total,
		After:  after.Total,
		OK:     after.Total <= before.Total,
		Drift:  introducedDrift(before.Drift, after.Drift),
	}
}

// introducedDrift returns the head drift that the base did not already have,
// keyed by package and entity.
func introducedDrift(before, after []SurfaceDrift) []SurfaceDrift {
	if len(after) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(before))
	for _, d := range before {
		seen[d.Package+"\x00"+d.Entity] = true
	}
	var out []SurfaceDrift
	for _, d := range after {
		if !seen[d.Package+"\x00"+d.Entity] {
			out = append(out, d)
		}
	}
	return out
}
