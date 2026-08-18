package plan

import (
	"fmt"
	"sort"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Unmet is one unmet obligation contributing to plan distance.
type Unmet struct {
	Class   string `json:"class"`   // C1, C2, C3, or C4
	Package string `json:"package"` // relevant package path
	Detail  string `json:"detail"`  // human-readable explanation
}

// DistResult holds the computed plan distance and its breakdown.
type DistResult struct {
	Total int     `json:"total"`
	C1    int     `json:"c1_unfilled_holes"`
	C2    int     `json:"c2_absent_boxes"`
	C3    int     `json:"c3_absent_arrows"`
	C4    int     `json:"c4_disallowed_arrows"`
	Unmet []Unmet `json:"unmet,omitempty"`
}

// Dist computes plan distance: the number of unmet obligations between a plan
// and an actual (or another plan) graph. Implements Def. 5.4's four classes.
//
// Both operands are *graph.Graph. When comparing two plans (Review A), a hole
// present in both is NOT counted as unfilled.
func Dist(plan, actual *graph.Graph) DistResult {
	if plan == nil {
		return DistResult{}
	}
	if actual == nil {
		actual = &graph.Graph{}
	}
	var res DistResult

	actualPkgs := indexPackages(actual)
	planPkgs := indexPackages(plan)

	// C1: unfilled holes — declared holes with no filled implementation in actual
	for _, pp := range plan.Packages {
		if !pp.Hole {
			continue
		}
		ap, exists := actualPkgs[pp.Path]
		if !exists {
			res.C1++
			res.Unmet = append(res.Unmet, Unmet{
				Class:   "C1",
				Package: pp.Path,
				Detail:  "hole declared, package absent in actual",
			})
			continue
		}
		// Plan-vs-plan: both declare the same hole
		if ap.Hole {
			if !surfaceMatch(pp.Surface, ap.Surface) {
				res.C1++
				res.Unmet = append(res.Unmet, Unmet{
					Class:   "C1",
					Package: pp.Path,
					Detail:  fmt.Sprintf("hole in both but surface mismatch: declared %d, actual %d", len(pp.Surface), len(ap.Surface)),
				})
			}
			continue
		}
		// Plan-vs-code: package exists but has no files (still unfilled)
		if len(ap.Files) == 0 && len(ap.Surface) == 0 {
			res.C1++
			res.Unmet = append(res.Unmet, Unmet{
				Class:   "C1",
				Package: pp.Path,
				Detail:  "hole declared, package exists but has no interior",
			})
			continue
		}
		// Package is filled — check surface match
		if !surfaceMatch(pp.Surface, ap.Surface) {
			res.C1++
			res.Unmet = append(res.Unmet, Unmet{
				Class:   "C1",
				Package: pp.Path,
				Detail:  fmt.Sprintf("hole filled but surface mismatch: declared %d, actual %d", len(pp.Surface), len(ap.Surface)),
			})
		}
	}

	// C2: absent declared boxes (non-hole packages in plan not present in actual)
	for _, pp := range plan.Packages {
		if pp.Hole {
			continue
		}
		if _, exists := actualPkgs[pp.Path]; !exists {
			res.C2++
			res.Unmet = append(res.Unmet, Unmet{
				Class:   "C2",
				Package: pp.Path,
				Detail:  "declared box absent from actual",
			})
		}
	}

	// C3: absent declared arrows (edges in plan not present in actual)
	actualEdges := indexEdges(actual)
	for _, e := range plan.Edges {
		key := edgeKey(e)
		if !actualEdges[key] {
			res.C3++
			res.Unmet = append(res.Unmet, Unmet{
				Class:   "C3",
				Package: e.From,
				Detail:  fmt.Sprintf("declared arrow %s -> %s (%s) absent", e.From, e.To, e.Kind),
			})
		}
	}

	// C4: disallowed arrows (import edges in actual between plan-declared packages,
	// outside the Allow list). Only import edges are checked because Allow
	// declarations only cover imports.
	planAllow := indexAllow(plan)
	for _, e := range actual.Edges {
		if e.Kind != "import" {
			continue
		}
		// Only check edges between packages that the plan mentions
		_, fromInPlan := planPkgs[e.From]
		_, toInPlan := planPkgs[e.To]
		if !fromInPlan || !toInPlan {
			continue
		}
		allowed, declared := planAllow[e.From]
		if !declared {
			continue
		}
		if !containsStr(allowed, e.To) {
			res.C4++
			res.Unmet = append(res.Unmet, Unmet{
				Class:   "C4",
				Package: e.From,
				Detail:  fmt.Sprintf("arrow %s -> %s (%s) not in Allow", e.From, e.To, e.Kind),
			})
		}
	}

	res.Total = res.C1 + res.C2 + res.C3 + res.C4
	sort.Slice(res.Unmet, func(i, j int) bool {
		if res.Unmet[i].Class != res.Unmet[j].Class {
			return res.Unmet[i].Class < res.Unmet[j].Class
		}
		return res.Unmet[i].Package < res.Unmet[j].Package
	})
	return res
}

func indexPackages(g *graph.Graph) map[string]graph.Package {
	m := make(map[string]graph.Package, len(g.Packages))
	for _, p := range g.Packages {
		m[p.Path] = p
	}
	return m
}

func indexEdges(g *graph.Graph) map[string]bool {
	m := make(map[string]bool, len(g.Edges))
	for _, e := range g.Edges {
		m[edgeKey(e)] = true
	}
	return m
}

func indexAllow(g *graph.Graph) map[string][]string {
	m := make(map[string][]string)
	for _, p := range g.Packages {
		if len(p.Allow) > 0 {
			m[p.Path] = p.Allow
		}
	}
	return m
}

func edgeKey(e graph.Edge) string {
	return e.From + " -> " + e.To + " : " + e.Kind
}

func surfaceMatch(declared, actual []graph.Symbol) bool {
	if len(declared) == 0 {
		return true
	}
	actualNames := make(map[string]bool, len(actual))
	for _, s := range actual {
		actualNames[s.Name] = true
	}
	for _, s := range declared {
		if !actualNames[s.Name] {
			return false
		}
	}
	return true
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
