package plan

import (
	"sort"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Verdict is the classification of a PR's relationship to a plan.
type Verdict string

const (
	Realizes  Verdict = "REALIZES"
	Exceeds   Verdict = "EXCEEDS"
	Conflicts Verdict = "CONFLICTS"
	Unrelated Verdict = "UNRELATED"
)

// ClassifyResult holds the verdict and supporting detail.
type ClassifyResult struct {
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason"`
	// Offending names the structure behind the verdict, each entry formatted as
	// "<label>: <detail>". Empty for REALIZES and UNRELATED.
	Offending []string `json:"offending,omitempty"`
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
	planPairs := make(map[string]bool)
	for _, e := range p.Edges {
		planPairs[pairKey(e)] = true
	}

	// Check for conflicts: C4 increased (disallowed arrow introduced)
	if after.C4 > before.C4 {
		return ClassifyResult{
			Verdict:   Conflicts,
			Reason:    "introduced a dependency outside a declared Allow list",
			Offending: newUnmet(before.Unmet, after.Unmet, "C4"),
		}
	}

	// Check for conflicts: declared element removed (dist increased for other reasons)
	if after.Total > before.Total {
		return ClassifyResult{
			Verdict:   Conflicts,
			Reason:    "moved away from the plan (dist increased)",
			Offending: newUnmet(before.Unmet, after.Unmet, ""),
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
	var unplanned []string

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
	baseEdges := make(map[string]bool)
	for _, e := range base.Edges {
		baseEdges[edgeKey(e)] = true
	}
	for _, e := range head.Edges {
		if baseEdges[edgeKey(e)] {
			continue
		}
		switch {
		// Matched on the pair, not on edgeKey: extraction derives call and
		// implements edges from a dependency the plan may declare only as an
		// import, and a derived edge is not structure the plan failed to sanction.
		// Kind is still enforced for declared arrows, by C3 in Dist.
		case planPairs[pairKey(e)]:
			touchesPlan = true
		case planPkgs[e.From] || planPkgs[e.To]:
			unplanned = append(unplanned, "undeclared: "+edgeKey(e))
		}
	}
	sort.Strings(unplanned)

	// Also check: did dist decrease? That means the PR filled something.
	if after.Total < before.Total {
		touchesPlan = true
	}

	// Apply precedence: Conflicts already handled above
	if len(unplanned) > 0 {
		return ClassifyResult{
			Verdict:   Exceeds,
			Reason:    "adds structure the plan does not declare",
			Offending: unplanned,
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

// newUnmet returns the obligations present in after but not before, restricted
// to class when class is non-empty. Package is part of the identity because C1
// and C2 details are identical across packages, so matching on the detail alone
// would attribute a newly unmet package to the wrong one.
func newUnmet(before, after []Unmet, class string) []string {
	had := make(map[string]bool, len(before))
	for _, u := range before {
		had[unmetKey(u)] = true
	}
	var out []string
	for _, u := range after {
		if class != "" && u.Class != class {
			continue
		}
		if had[unmetKey(u)] {
			continue
		}
		out = append(out, u.Class+" "+u.Package+": "+u.Detail)
	}
	sort.Strings(out)
	return out
}

func unmetKey(u Unmet) string { return u.Class + "|" + u.Package + "|" + u.Detail }
