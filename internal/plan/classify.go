package plan

import (
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Verdict is the classification of a PR's relationship to a plan.
type Verdict string

const (
	Realizes   Verdict = "REALIZES"
	Exceeds    Verdict = "EXCEEDS"
	Conflicts  Verdict = "CONFLICTS"
	Unrelated  Verdict = "UNRELATED"
)

// ClassifyResult holds the verdict and supporting detail.
type ClassifyResult struct {
	Verdict Verdict  `json:"verdict"`
	Reason  string   `json:"reason"`
}

// Classify determines a PR's relationship to a plan by comparing what changed
// against what the plan declares. Uses the delta between base and head graphs,
// evaluated against the plan's declared packages and edges.
//
// Precedence: Conflicts > Exceeds > Realizes > Unrelated.
// A PR that both fills a hole AND adds a disallowed arrow is Conflicts (worst wins).
func Classify(p, base, head *graph.Graph) ClassifyResult {
	if p == nil {
		return ClassifyResult{Verdict: Unrelated, Reason: "no plan provided"}
	}
	if base == nil {
		base = &graph.Graph{}
	}
	if head == nil {
		head = &graph.Graph{}
	}

	before := Dist(p, base)
	after := Dist(p, head)

	planPkgs := make(map[string]bool)
	for _, pkg := range p.Packages {
		planPkgs[pkg.Path] = true
	}
	planEdgeSet := make(map[string]bool)
	for _, e := range p.Edges {
		planEdgeSet[edgeKey(e)] = true
	}

	// Check for conflicts: C4 increased (disallowed arrow introduced)
	if after.C4 > before.C4 {
		return ClassifyResult{
			Verdict: Conflicts,
			Reason:  "introduced a dependency outside a declared Allow list",
		}
	}

	// Check for conflicts: declared element removed (dist increased for other reasons)
	if after.Total > before.Total {
		return ClassifyResult{
			Verdict: Conflicts,
			Reason:  "moved away from the plan (dist increased)",
		}
	}

	// Check if PR touches plan-declared packages at all
	headPkgs := make(map[string]bool)
	for _, pkg := range head.Packages {
		headPkgs[pkg.Path] = true
	}
	basePkgs := make(map[string]bool)
	for _, pkg := range base.Packages {
		basePkgs[pkg.Path] = true
	}

	touchesPlan := false
	addsUnplanned := false

	// New packages in head that weren't in base
	for path := range headPkgs {
		if basePkgs[path] {
			continue
		}
		if planPkgs[path] {
			touchesPlan = true
		}
	}

	// New edges in head
	headEdges := make(map[string]bool)
	for _, e := range head.Edges {
		headEdges[edgeKey(e)] = true
	}
	baseEdges := make(map[string]bool)
	for _, e := range base.Edges {
		baseEdges[edgeKey(e)] = true
	}
	for _, e := range head.Edges {
		if baseEdges[edgeKey(e)] {
			continue
		}
		if planEdgeSet[edgeKey(e)] {
			touchesPlan = true
		} else if planPkgs[e.From] || planPkgs[e.To] {
			addsUnplanned = true
		}
	}

	// Also check: did dist decrease? That means the PR filled something.
	if after.Total < before.Total {
		touchesPlan = true
	}

	// Apply precedence: Conflicts already handled above
	if touchesPlan && addsUnplanned {
		return ClassifyResult{
			Verdict: Exceeds,
			Reason:  "adds structure the plan does not declare",
		}
	}
	if touchesPlan {
		return ClassifyResult{
			Verdict: Realizes,
			Reason:  "discharges plan obligations without introducing new ones",
		}
	}
	return ClassifyResult{
		Verdict: Unrelated,
		Reason:  "touches nothing the plan declares",
	}
}
