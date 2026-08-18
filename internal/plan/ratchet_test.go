package plan

import (
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

func TestRatchet_Decreases_OK(t *testing.T) {
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	base := &graph.Graph{} // nothing exists
	head := &graph.Graph{  // hole filled
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	r := Ratchet(p, base, head)
	if !r.OK {
		t.Fatalf("dist decreased (%d→%d), should be OK", r.Before, r.After)
	}
	if r.Before <= r.After {
		t.Errorf("before=%d should be > after=%d", r.Before, r.After)
	}
}

func TestRatchet_Increases_NotOK(t *testing.T) {
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Allow: []string{"m/b"}},
			{Path: "m/b"},
			{Path: "m/c"},
		},
	}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
			{Path: "m/c", Files: []string{"c.go"}},
		},
	}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
			{Path: "m/c", Files: []string{"c.go"}},
		},
		Edges: []graph.Edge{{From: "m/a", To: "m/c", Kind: "import"}}, // disallowed
	}
	r := Ratchet(p, base, head)
	if r.OK {
		t.Fatalf("dist increased (%d→%d), should NOT be OK", r.Before, r.After)
	}
}

func TestRatchet_NoChange_OK(t *testing.T) {
	p := &graph.Graph{
		Packages: []graph.Package{{Path: "m/a", Hole: true}},
	}
	same := &graph.Graph{}
	r := Ratchet(p, same, same)
	if !r.OK {
		t.Fatal("no change should be OK")
	}
	if r.Before != r.After {
		t.Errorf("before=%d != after=%d for identical graphs", r.Before, r.After)
	}
}
