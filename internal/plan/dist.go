package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Unmet is one unmet obligation contributing to plan distance.
type Unmet struct {
	Class   string `json:"class"`   // C1, C2, C3, or C4
	Package string `json:"package"` // relevant package path
	Detail  string `json:"detail"`  // human-readable explanation
}

// SurfaceDrift is one declared entity that exists under the right name but with
// a different signature than the plan states.
type SurfaceDrift struct {
	Package  string `json:"package"`
	Entity   string `json:"entity"`   // symbol name, e.g. "Format"
	Declared string `json:"declared"` // signature the plan states
	Actual   string `json:"actual"`   // signature the code has
}

// DistResult holds the computed plan distance and its breakdown.
type DistResult struct {
	Total int     `json:"total"`
	C1    int     `json:"c1_unfilled_holes"`
	C2    int     `json:"c2_absent_boxes"`
	C3    int     `json:"c3_absent_arrows"`
	C4    int     `json:"c4_disallowed_arrows"`
	Unmet []Unmet `json:"unmet,omitempty"`

	// Drift records declared entities whose signature is not the one that shipped.
	// Deliberately NOT counted in Total: a parameter rename is not an
	// architectural regression, and making it one would break a plan for no
	// structural reason. So dist == 0 keeps meaning "structure realized", while
	// plan-vs-code divergence stops being invisible.
	Drift []SurfaceDrift `json:"surfaceDrift,omitempty"`
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
			// Plan-vs-plan is the easy case for drift: both sides are author-typed,
			// so the comparison is symmetric.
			res.Drift = append(res.Drift, surfaceDrift(pp.Path, pp.Surface, ap.Surface)...)
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
		// Names can all line up while the signatures do not. Reported separately
		// so it never changes Total.
		res.Drift = append(res.Drift, surfaceDrift(pp.Path, pp.Surface, ap.Surface)...)
	}

	// C2: absent declared boxes (non-hole packages in plan not present in actual)
	for _, pp := range plan.Packages {
		if pp.Hole {
			continue
		}
		ap, exists := actualPkgs[pp.Path]
		if !exists {
			res.C2++
			res.Unmet = append(res.Unmet, Unmet{
				Class:   "C2",
				Package: pp.Path,
				Detail:  "declared box absent from actual",
			})
			continue
		}
		// A box carrying a declared surface. Unreachable from a compiled .archon —
		// parseBox has no block form, so `box X { surface: ... }` is a parse error —
		// but plan dist also accepts hand-written or hand-edited plan JSON, and a
		// stale signature on a fixed surface is exactly what #41 is about.
		res.Drift = append(res.Drift, surfaceDrift(pp.Path, pp.Surface, ap.Surface)...)
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
	// Sorted here rather than per package: Dist must not depend on the caller
	// having sorted plan.Packages, since output is guaranteed byte-identical.
	sort.Slice(res.Drift, func(i, j int) bool {
		if res.Drift[i].Package != res.Drift[j].Package {
			return res.Drift[i].Package < res.Drift[j].Package
		}
		return res.Drift[i].Entity < res.Drift[j].Entity
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

func pairKey(e graph.Edge) string {
	return e.From + " -> " + e.To
}

// sigShape is the comparable skeleton of a signature: how many parameters, how
// many results, and whether the last parameter is variadic.
//
// Comparing signature TEXT cannot work here. A plan states what its author typed
// while extraction reports the go/types rendering, and they differ in at least
// four ways that carry no meaning:
//
//	plan "(token string) (*User, error)"  code "func(token string) (*pkg/user.User, error)"
//	plan "(a, b string) string"           code "func(a string, b string) string"
//	plan "(BlockKey, ReqCtx) Result"      code "func(k BlockKey, c ReqCtx) Result"
//	plan "(x T) T"                        code "func[T any](x T) T"
//
// Every one of those is a perfect match reported as drift, and the third is how
// demo/flow3-blis-design/kv-offload.archon is written throughout. A report whose
// rows are mostly false is a report nobody reads, so the comparison ignores type
// spelling and parameter names entirely.
//
// The cost is stated rather than hidden: a change with the same arity, variadicity
// and result count — string to int, say — is NOT detected. What is detected is the
// class of change #41 is about, where the parameter shape moved.
type sigShape struct {
	params   int
	results  int
	variadic bool
}

// shapeOf parses a signature into its skeleton. ok is false when no parameter
// list can be found, in which case the signature is not comparable and drift is
// not reported.
func shapeOf(sig string) (sigShape, bool) {
	s := strings.TrimPrefix(strings.TrimSpace(sig), "func")
	s = strings.TrimSpace(s)
	// Generic type-parameter list, e.g. "[T any](x T) T".
	if strings.HasPrefix(s, "[") {
		if end := matchingBracket(s, 0, '[', ']'); end > 0 {
			s = strings.TrimSpace(s[end+1:])
		}
	}
	if !strings.HasPrefix(s, "(") {
		return sigShape{}, false
	}
	closeIdx := matchingBracket(s, 0, '(', ')')
	if closeIdx < 0 {
		return sigShape{}, false
	}
	params := strings.TrimSpace(s[1:closeIdx])
	results := strings.TrimSpace(s[closeIdx+1:])
	// parseSurfaceEntry's grammar also allows "Name(args) -> Result", and without
	// stripping the arrow a tuple result falls through to the single-result branch
	// and reports drift for a perfect match.
	results = strings.TrimSpace(strings.TrimPrefix(results, "->"))

	shape := sigShape{
		params:   countTopLevel(params),
		variadic: lastParamVariadic(params),
	}
	switch {
	case results == "":
		shape.results = 0
	case strings.HasPrefix(results, "("):
		if end := matchingBracket(results, 0, '(', ')'); end > 0 {
			shape.results = countTopLevel(strings.TrimSpace(results[1:end]))
		} else {
			return sigShape{}, false
		}
	default:
		shape.results = 1
	}
	return shape, true
}

// lastParamVariadic reports whether the LAST top-level parameter is variadic.
//
// A substring test over the whole list is wrong: in "f func(...int) error" the
// "..." belongs to that parameter's own type, so scoring the signature variadic
// makes it shape-equal to "fns ...func(int) error" and hides a real arity change.
// The variadic bit is the only thing distinguishing "(s string)" from
// "(...string)", so its precision carries weight.
func lastParamVariadic(params string) bool {
	last, depth := params, 0
	for i := 0; i < len(params); i++ {
		switch params[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				last = params[i+1:]
			}
		}
	}
	depth = 0
	for i := 0; i < len(last); i++ {
		switch last[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '.':
			if depth == 0 && strings.HasPrefix(last[i:], "...") {
				return true
			}
		}
	}
	return false
}

// matchingBracket returns the index of the bracket closing the one at start, or
// -1 if unbalanced. Nested parens, brackets and braces are tracked so a func-typed
// parameter or a map type does not confuse the scan.
func matchingBracket(s string, start int, open, close byte) int {
	if start >= len(s) || s[start] != open {
		return -1
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				if s[i] != close {
					return -1
				}
				return i
			}
		}
	}
	return -1
}

// countTopLevel counts comma-separated entries at nesting depth zero. Grouped
// parameters count once each ("a, b string" is two), which is what makes the
// plan's grouped form agree with the expanded rendering.
func countTopLevel(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	n, depth := 1, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				n++
			}
		}
	}
	return n
}

// surfaceDrift reports declared entities that exist under the right name but with
// a different parameter/result shape.
//
// A signature missing on EITHER side means "not recorded", not "different": a
// hand-written or older graph JSON carries names with no signature at all, and the
// extractor emits none for a bare type (Kind "type"), so those can never drift.
// Both sides must state a signature before they can disagree.
//
// Callers append to a shared slice; ordering is normalized once in Dist.
func surfaceDrift(pkgPath string, declared, actual []graph.Symbol) []SurfaceDrift {
	actualSigs := make(map[string]string, len(actual))
	for _, s := range actual {
		actualSigs[s.Name] = s.Sig
	}
	var out []SurfaceDrift
	for _, d := range declared {
		got, exists := actualSigs[d.Name]
		if !exists {
			continue // absent entirely — that is the name check's business (C1)
		}
		if d.Sig == "" || got == "" {
			continue
		}
		declShape, ok1 := shapeOf(d.Sig)
		gotShape, ok2 := shapeOf(got)
		if !ok1 || !ok2 {
			continue // not comparable — never guess
		}
		if declShape != gotShape {
			out = append(out, SurfaceDrift{
				Package:  pkgPath,
				Entity:   d.Name,
				Declared: d.Sig,
				Actual:   got,
			})
		}
	}
	return out
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
