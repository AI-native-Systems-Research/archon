package plan

import "github.com/AI-native-Systems-Research/archon/internal/graph"

// RatchetResult reports whether a PR moved toward or away from the plan.
type RatchetResult struct {
	Before int  `json:"before"`
	After  int  `json:"after"`
	OK     bool `json:"ok"`
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
	}
}
