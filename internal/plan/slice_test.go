package plan

import (
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

func TestSlice_Found(t *testing.T) {
	g := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/existing", Internal: true},
			{Path: "m/newpkg", Internal: true, Hole: true,
				Surface:    []graph.Symbol{{Kind: "func", Name: "Run", Sig: "(ctx) error"}},
				Allow:      []string{"m/existing"},
				Invariants: []graph.Invariant{{Name: "BC-N1", File: "plan"}},
			},
		},
	}
	out, err := Slice(g, "m/newpkg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "m/newpkg") {
		t.Error("should contain hole path")
	}
	if !strings.Contains(out, "Run") {
		t.Error("should contain surface entry")
	}
	if !strings.Contains(out, "m/existing") {
		t.Error("should contain allow entry")
	}
	if !strings.Contains(out, "BC-N1") {
		t.Error("should contain contract clause")
	}
}

func TestSlice_NotFound(t *testing.T) {
	g := &graph.Graph{
		Packages: []graph.Package{{Path: "m/a", Hole: true}},
	}
	_, err := Slice(g, "m/nonexistent")
	if err == nil {
		t.Fatal("expected error for missing hole")
	}
}

func TestSlice_NotAHole(t *testing.T) {
	g := &graph.Graph{
		Packages: []graph.Package{{Path: "m/a", Internal: true}},
	}
	_, err := Slice(g, "m/a")
	if err == nil {
		t.Fatal("expected error for non-hole package")
	}
}

func TestSlice_Deterministic(t *testing.T) {
	g := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/pkg", Hole: true,
				Surface: []graph.Symbol{{Kind: "func", Name: "A"}, {Kind: "func", Name: "B"}},
				Allow:   []string{"m/x"},
			},
		},
	}
	out1, _ := Slice(g, "m/pkg")
	out2, _ := Slice(g, "m/pkg")
	if out1 != out2 {
		t.Error("slice is not deterministic")
	}
}
