package plan

import (
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

func TestRender_HolesDashed(t *testing.T) {
	g := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/existing", Internal: true},
			{Path: "m/newpkg", Internal: true, Hole: true},
		},
		Edges: []graph.Edge{{From: "m/existing", To: "m/newpkg", Kind: "import"}},
	}
	out := Render(g)
	if !strings.Contains(out, "graph LR") {
		t.Error("should be a Mermaid LR graph")
	}
	if !strings.Contains(out, "classDef hole") {
		t.Error("should define hole class with dashed style")
	}
	if !strings.Contains(out, "classDef box") {
		t.Error("should define box class for non-holes")
	}
	if !strings.Contains(out, "-->") {
		t.Error("should contain edges")
	}
}

func TestRender_Deterministic(t *testing.T) {
	g := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Internal: true},
			{Path: "m/b", Internal: true, Hole: true},
		},
		Edges: []graph.Edge{{From: "m/a", To: "m/b", Kind: "import"}},
	}
	out1 := Render(g)
	out2 := Render(g)
	if out1 != out2 {
		t.Error("render is not deterministic")
	}
}

func TestRender_EmptyGraph(t *testing.T) {
	g := &graph.Graph{}
	out := Render(g)
	if !strings.Contains(out, "graph LR") {
		t.Error("empty graph should still produce valid Mermaid")
	}
}
