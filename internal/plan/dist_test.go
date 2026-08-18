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
