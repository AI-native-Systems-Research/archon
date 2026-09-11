package plan

import (
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

func TestDist_AllFilled(t *testing.T) {
	plan := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	actual := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	res := Dist(plan, actual)
	if res.Total != 0 {
		t.Fatalf("want dist=0 (all filled), got %d: %+v", res.Total, res.Unmet)
	}
}

func TestDist_UnfilledHole(t *testing.T) {
	plan := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	actual := &graph.Graph{
		Packages: []graph.Package{},
		Edges:    []graph.Edge{},
	}
	res := Dist(plan, actual)
	// C1=1 (unfilled hole) + C3=1 (absent arrow) = 2
	if res.Total != 2 {
		t.Fatalf("want dist=2, got %d: %+v", res.Total, res.Unmet)
	}
	if res.C1 != 1 {
		t.Errorf("C1 want 1, got %d", res.C1)
	}
	if res.C3 != 1 {
		t.Errorf("C3 want 1, got %d", res.C3)
	}
}

func TestDist_FillDecreases(t *testing.T) {
	plan := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
			{Path: "m/b", Hole: true, Surface: []graph.Symbol{{Kind: "func", Name: "Do"}}},
		},
		Edges: []graph.Edge{
			{From: "m/cmd", To: "m/a", Kind: "import"},
			{From: "m/cmd", To: "m/b", Kind: "import"},
		},
	}

	// Before: nothing exists
	before := &graph.Graph{}
	resBefore := Dist(plan, before)

	// After: fill m/a (but not m/b)
	after := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	resAfter := Dist(plan, after)

	if resAfter.Total >= resBefore.Total {
		t.Fatalf("filling a hole should decrease dist: before=%d after=%d", resBefore.Total, resAfter.Total)
	}
	// Specifically: before=4 (2 holes + 2 arrows), after=2 (1 hole + 1 arrow)
	if resBefore.Total != 4 {
		t.Errorf("before: want 4, got %d", resBefore.Total)
	}
	if resAfter.Total != 2 {
		t.Errorf("after: want 2, got %d", resAfter.Total)
	}
}

func TestDist_DisallowedArrowIncreases(t *testing.T) {
	plan := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Allow: []string{"m/b"}},
			{Path: "m/b"},
			{Path: "m/c"},
		},
	}
	// Actual has an edge m/a -> m/c which is NOT in m/a's Allow list
	actual := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
			{Path: "m/c", Files: []string{"c.go"}},
		},
		Edges: []graph.Edge{{From: "m/a", To: "m/c", Kind: "import"}},
	}
	res := Dist(plan, actual)
	if res.C4 != 1 {
		t.Fatalf("want C4=1 (disallowed arrow), got %d: %+v", res.C4, res.Unmet)
	}
}

func TestDist_PlanVsPlan_HoleInBothNotCounted(t *testing.T) {
	plan1 := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true},
			{Path: "m/b", Hole: true},
		},
	}
	plan2 := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true},
			{Path: "m/b", Hole: true},
		},
	}
	res := Dist(plan1, plan2)
	if res.C1 != 0 {
		t.Fatalf("holes present in both plans should not be counted as unfilled, got C1=%d", res.C1)
	}
}

func TestDist_AbsentDeclaredBox(t *testing.T) {
	plan := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/required"},
		},
	}
	actual := &graph.Graph{}
	res := Dist(plan, actual)
	if res.C2 != 1 {
		t.Fatalf("want C2=1 (absent declared box), got %d", res.C2)
	}
}

// --- Surface drift (#41) ---

func sym(name, sig string) graph.Symbol {
	return graph.Symbol{Kind: "func", Name: name, Sig: sig}
}

// filledHolePlan declares one hole whose surface states the given signature.
func filledHolePlan(sig string) *graph.Graph {
	return &graph.Graph{Module: "example.com/y", Packages: []graph.Package{
		{Path: "example.com/y/pkg", Name: "pkg", Internal: true, Hole: true,
			Surface: []graph.Symbol{sym("Do", sig)}},
	}}
}

// filledActual is a realized package with the given signature.
func filledActual(sig string) *graph.Graph {
	return &graph.Graph{Module: "example.com/y", Packages: []graph.Package{
		{Path: "example.com/y/pkg", Name: "pkg", Internal: true,
			Files:   []string{"pkg.go"},
			Surface: []graph.Symbol{sym("Do", sig)}},
	}}
}

func TestDistReportsSurfaceDrift(t *testing.T) {
	res := Dist(filledHolePlan("(s string) string"), filledActual("func(parts ...string) string"))

	if len(res.Drift) != 1 {
		t.Fatalf("got %d drift entries, want 1: %+v", len(res.Drift), res.Drift)
	}
	d := res.Drift[0]
	if d.Entity != "Do" || d.Declared != "(s string) string" || d.Actual != "func(parts ...string) string" {
		t.Errorf("drift = %+v, want Do with both signatures verbatim", d)
	}
	// The whole point: the structure IS realized, so distance stays 0 and the
	// drift is what carries the signal.
	if res.Total != 0 || res.C1 != 0 {
		t.Errorf("drift changed the distance: total=%d c1=%d, want 0/0", res.Total, res.C1)
	}
}

// The trap this feature nearly shipped with: a plan writes "(s string) string"
// while extraction writes "func(s string) string", so comparing raw reports drift
// for a PERFECT match and the report becomes noise nobody reads.
func TestDistNoDriftWhenSignaturesAgreeModuloRendering(t *testing.T) {
	for _, actual := range []string{
		"func(s string) string", // go/types rendering
		"(s string) string",     // already plan-shaped
		"func(s string)  string",
		"  func(s string) string  ",
	} {
		if res := Dist(filledHolePlan("(s string) string"), filledActual(actual)); len(res.Drift) != 0 {
			t.Errorf("actual %q reported drift against an equivalent declaration: %+v", actual, res.Drift)
		}
	}
}

// A signature absent on either side means "not recorded", not "different".
// Hand-written and older graph JSON carry names with no sig at all.
func TestDistNoDriftWhenEitherSignatureMissing(t *testing.T) {
	if res := Dist(filledHolePlan("(s string) string"), filledActual("")); len(res.Drift) != 0 {
		t.Errorf("missing actual signature reported as drift: %+v", res.Drift)
	}
	if res := Dist(filledHolePlan(""), filledActual("func(s string) string")); len(res.Drift) != 0 {
		t.Errorf("missing declared signature reported as drift: %+v", res.Drift)
	}
}

// An entity that is absent entirely is C1's business, not drift's.
func TestDistAbsentEntityIsNotDrift(t *testing.T) {
	actual := &graph.Graph{Module: "example.com/y", Packages: []graph.Package{
		{Path: "example.com/y/pkg", Name: "pkg", Internal: true,
			Files:   []string{"pkg.go"},
			Surface: []graph.Symbol{sym("Other", "func() int")}},
	}}
	res := Dist(filledHolePlan("(s string) string"), actual)

	if len(res.Drift) != 0 {
		t.Errorf("absent entity reported as drift instead of C1: %+v", res.Drift)
	}
	if res.C1 != 1 {
		t.Errorf("C1 = %d, want 1 (declared entity is missing)", res.C1)
	}
}

// An unfilled hole has nothing to compare against.
func TestDistNoDriftForUnfilledHole(t *testing.T) {
	if res := Dist(filledHolePlan("(s string) string"), &graph.Graph{}); len(res.Drift) != 0 {
		t.Errorf("unfilled hole produced drift: %+v", res.Drift)
	}
}

// The renderings that matter, taken from this repo's own docs and demo plan. The
// previous string-based comparison reported drift for EVERY one of these despite
// them being perfect matches — which is why the comparison is now structural.
func TestNoDriftAcrossRealisticRenderings(t *testing.T) {
	cases := []struct{ what, declared, actual string }{
		{"go/types func prefix", "(s string) string", "func(s string) string"},
		{"cross-package result type (docs/plan-syntax.md style)",
			"(token string) (*User, error)",
			"func(token string) (*example.com/m/internal/user.User, error)"},
		{"grouped parameters", "(a, b string) string", "func(a string, b string) string"},
		{"unnamed parameter types (flow3 demo plan style)",
			"(BlockKey, ReqCtx) LookupResult",
			"func(k BlockKey, c ReqCtx) LookupResult"},
		{"generic type parameters", "(x T) T", "func[T any](x T) T"},
		{"func-typed parameter", "(cb func(int) error) error", "func(cb func(int) error) error"},
		{"no whitespace", "(s string)string", "func(s string) string"},
		{"multiple results", "() (int, error)", "func() (int, error)"},
		{"no results", "(s string)", "func(s string)"},
		{"map type containing a comma", "(m map[string]int) int", "func(m map[string]int) int"},
		// parseSurfaceEntry's grammar also allows an arrow before the result.
		{"arrow result form, single", "() -> string", "func() string"},
		{"arrow result form, tuple", "(t string) -> (*User, error)",
			"func(t string) (*example.com/m/user.User, error)"},
		// A "..." inside a parameter's own type is part of that type.
		{"nested variadic in a func-typed param",
			"(fn func(...int) error) error", "func(fn func(...int) error) error"},
		{"generic constraint containing a pipe",
			"(x T) (T, error)", "func[T interface{~int | ~string}](x T) (T, error)"},
	}
	for _, tc := range cases {
		res := Dist(filledHolePlan(tc.declared), filledActual(tc.actual))
		if len(res.Drift) != 0 {
			t.Errorf("%s: reported drift for an equivalent signature\n  plan: %s\n  code: %s\n  got: %+v",
				tc.what, tc.declared, tc.actual, res.Drift)
		}
	}
}

// The shapes that SHOULD drift: the parameter or result shape actually moved.
func TestDriftDetectedWhenShapeDiffers(t *testing.T) {
	cases := []struct{ what, declared, actual string }{
		{"variadic vs fixed (#41's case)", "(s string) string", "func(parts ...string) string"},
		{"arity", "(s string) string", "func(a, b string) string"},
		{"result count", "(s string) string", "func(s string) (string, error)"},
		{"gained a result", "(s string)", "func(s string) error"},
		// The false negative a whole-string "..." test produced: one callback
		// parameter becoming a variadic slice of callbacks is an arity change, and
		// the nested "..." must not mask it.
		{"single callback -> variadic callbacks",
			"(fn func(...int) error) error", "func(fns ...func(int) error) error"},
	}
	for _, tc := range cases {
		res := Dist(filledHolePlan(tc.declared), filledActual(tc.actual))
		if len(res.Drift) != 1 {
			t.Errorf("%s: expected drift\n  plan: %s\n  code: %s\n  got: %+v",
				tc.what, tc.declared, tc.actual, res.Drift)
		}
	}
}

// Documented limitation, pinned so it is a decision rather than a surprise: a
// same-shape type change is not detected, because comparing type SPELLING is what
// made the report mostly-false.
func TestSameShapeTypeChangeIsNotDetected(t *testing.T) {
	res := Dist(filledHolePlan("(s string) string"), filledActual("func(s int) string"))
	if len(res.Drift) != 0 {
		t.Errorf("string->int now reported; update the docs limitation if this is intended: %+v", res.Drift)
	}
}

func TestShapeOfUnparseableIsNotComparable(t *testing.T) {
	for _, sig := range []string{"", "notasignature", "(unbalanced"} {
		if _, ok := shapeOf(sig); ok {
			t.Errorf("shapeOf(%q) reported comparable; an unparseable signature must be skipped", sig)
		}
	}
}

func TestDriftIsSortedAcrossPackages(t *testing.T) {
	mk := func(path string, hole bool, sig string) graph.Package {
		p := graph.Package{Path: path, Name: lastSegOf(path), Internal: true, Hole: hole,
			Surface: []graph.Symbol{sym("B", sig), sym("A", sig)}}
		if !hole {
			p.Files = []string{"x.go"}
		}
		return p
	}
	// Deliberately NOT in path order, to prove Dist does not rely on the caller.
	p := &graph.Graph{Module: "example.com/y", Packages: []graph.Package{
		mk("example.com/y/zeta", true, "(s string) string"),
		mk("example.com/y/alpha", true, "(s string) string"),
	}}
	actual := &graph.Graph{Module: "example.com/y", Packages: []graph.Package{
		mk("example.com/y/zeta", false, "func(x ...string) string"),
		mk("example.com/y/alpha", false, "func(x ...string) string"),
	}}

	res := Dist(p, actual)
	var got []string
	for _, d := range res.Drift {
		got = append(got, lastSegOf(d.Package)+"."+d.Entity)
	}
	want := []string{"alpha.A", "alpha.B", "zeta.A", "zeta.B"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("drift order = %v, want %v", got, want)
		}
	}
	// Package is printed by plan dist and must be populated.
	for _, d := range res.Drift {
		if d.Package == "" {
			t.Errorf("drift entry has no Package: %+v", d)
		}
	}
}

func lastSegOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// A box (non-hole) with a declared surface is treated as FIXED by review's
// surface policy, so a stale signature there matters as much as on a hole.
func TestDriftOnNonHoleBoxWithDeclaredSurface(t *testing.T) {
	p := &graph.Graph{Module: "example.com/y", Packages: []graph.Package{
		{Path: "example.com/y/pkg", Name: "pkg", Internal: true, Hole: false,
			Surface: []graph.Symbol{sym("Do", "(s string) string")}},
	}}
	res := Dist(p, filledActual("func(parts ...string) string"))
	if len(res.Drift) != 1 {
		t.Errorf("no drift reported for a fixed-surface box: %+v", res.Drift)
	}
	if res.Total != 0 {
		t.Errorf("total = %d, want 0", res.Total)
	}
}

// Plan-vs-plan: both sides are author-typed, so drift is the symmetric easy case
// and must still be reported (it is the same names-only gap #41 is about).
func TestDriftOnPlanVsPlan(t *testing.T) {
	a := filledHolePlan("(s string) string")
	b := filledHolePlan("(parts ...string) string")
	res := Dist(a, b)
	if len(res.Drift) != 1 {
		t.Errorf("plan-vs-plan drift not reported: %+v", res.Drift)
	}
}

func TestRatchetReportsIntroducedDrift(t *testing.T) {
	p := filledHolePlan("(s string) string")
	base := filledActual("func(s string) string")        // agrees
	head := filledActual("func(parts ...string) string") // drifted

	r := Ratchet(p, base, head)

	if len(r.Drift) != 1 {
		t.Fatalf("ratchet dropped the drift it introduced: %+v", r)
	}
	// Drift must not move the ratchet.
	if r.Before != r.After || !r.OK {
		t.Errorf("drift moved the ratchet: before=%d after=%d ok=%v", r.Before, r.After, r.OK)
	}
}

// Standing drift — already present at base — is not the PR's doing and must not
// be reported on every PR forever.
func TestRatchetOmitsStandingDrift(t *testing.T) {
	p := filledHolePlan("(s string) string")
	drifted := filledActual("func(parts ...string) string")

	r := Ratchet(p, drifted, drifted)
	if len(r.Drift) != 0 {
		t.Errorf("standing drift reported as introduced: %+v", r.Drift)
	}
}
