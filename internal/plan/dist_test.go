package plan

import (
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

func TestNormalizeSig(t *testing.T) {
	cases := map[string]string{
		"func(s string) string":    "(s string) string",
		"(s string) string":        "(s string) string",
		"  func(s string)  string": "(s string) string",
		"":                         "",
		"func() int":               "() int",
	}
	for in, want := range cases {
		if got := normalizeSig(in); got != want {
			t.Errorf("normalizeSig(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRatchetCarriesDrift(t *testing.T) {
	p := filledHolePlan("(s string) string")
	base := filledActual("func(s string) string")        // agrees
	head := filledActual("func(parts ...string) string") // drifted

	r := Ratchet(p, base, head)

	if len(r.Drift) != 1 {
		t.Fatalf("ratchet dropped the drift: %+v", r)
	}
	// Drift must not move the ratchet.
	if r.Before != r.After || !r.OK {
		t.Errorf("drift moved the ratchet: before=%d after=%d ok=%v", r.Before, r.After, r.OK)
	}
}
