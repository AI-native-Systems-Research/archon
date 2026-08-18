package plan

import (
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

func TestStats_CountsByClass(t *testing.T) {
	g := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Invariants: []graph.Invariant{
				{Name: "BC-1", Hash: "checked: G0"},
				{Name: "BC-2", Hash: "evidenced: property_test"},
				{Name: "BC-3", Hash: "evidenced: fuzz"},
				{Name: "BC-4", Hash: "attested:design"},
			}},
		},
	}
	out := Stats(g)
	if !strings.Contains(out, "4 clauses") {
		t.Errorf("want '4 clauses', got: %s", out)
	}
	if !strings.Contains(out, "1 checked") {
		t.Errorf("want '1 checked', got: %s", out)
	}
	if !strings.Contains(out, "2 evidenced") {
		t.Errorf("want '2 evidenced', got: %s", out)
	}
	if !strings.Contains(out, "1 attested:design") {
		t.Errorf("want '1 attested:design', got: %s", out)
	}
}

func TestStats_EmptyGraph(t *testing.T) {
	g := &graph.Graph{}
	out := Stats(g)
	if !strings.Contains(out, "0 clauses") {
		t.Errorf("empty graph should have 0 clauses, got: %s", out)
	}
}
