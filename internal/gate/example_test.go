package gate

import (
	"encoding/json"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// TestExample_SurfaceGrowthDetected demonstrates the full input/output of the
// surface growth gate. Run with: go test -v -run TestExample_SurfaceGrowth ./internal/gate/
func TestExample_SurfaceGrowthDetected(t *testing.T) {
	mod := "example.com/m"

	// --- INPUT: base graph (before PR) ---
	// Package "graph" exports 2 types: Package and Edge.
	base := &graph.Graph{
		Module: mod,
		Packages: []graph.Package{
			{Path: mod + "/internal/graph", Name: "graph", Internal: true, Surface: []graph.Symbol{
				{Kind: "type", Name: "Package", Sig: "struct{Path string; Name string}"},
				{Kind: "type", Name: "Edge", Sig: "struct{From string; To string}"},
			}},
		},
	}
	base.Sort()

	// --- INPUT: head graph (after PR) ---
	// Package "graph" now exports 3 types: Package, Edge, and Hole (new).
	head := &graph.Graph{
		Module: mod,
		Packages: []graph.Package{
			{Path: mod + "/internal/graph", Name: "graph", Internal: true, Surface: []graph.Symbol{
				{Kind: "type", Name: "Package", Sig: "struct{Path string; Name string}"},
				{Kind: "type", Name: "Edge", Sig: "struct{From string; To string}"},
				{Kind: "type", Name: "Hole", Sig: "bool"},
			}},
		},
	}
	head.Sort()

	// --- INPUT: surface policy ---
	// "graph" is declared fixed. "Hole" is NOT in the widen list.
	policy := &SurfacePolicy{
		Fixed: map[string]bool{mod + "/internal/graph": true},
	}

	policyJSON, _ := json.MarshalIndent(struct {
		Fixed []string            `json:"fixed"`
		Widen map[string][]string `json:"widen"`
	}{
		Fixed: []string{mod + "/internal/graph"},
	}, "", "  ")
	t.Logf("INPUT fixed.json:\n%s", policyJSON)
	t.Logf("INPUT base surface: [type Package, type Edge]")
	t.Logf("INPUT head surface: [type Package, type Edge, type Hole] <-- Hole is NEW")

	// --- COMPUTE ---
	d := delta.Compute(base, head)
	widenings := CheckSurface(d.Surface, policy)

	// --- OUTPUT ---
	outJSON, _ := json.MarshalIndent(widenings, "", "  ")
	t.Logf("OUTPUT widenings:\n%s", outJSON)

	// --- ASSERTIONS ---
	if len(widenings) != 1 {
		t.Fatalf("want 1 widening, got %d", len(widenings))
	}
	if widenings[0].Package != mod+"/internal/graph" {
		t.Errorf("package = %q", widenings[0].Package)
	}
	if len(widenings[0].Added) != 1 || widenings[0].Added[0].Name != "Hole" {
		t.Errorf("added = %v, want [Hole]", widenings[0].Added)
	}
	t.Logf("VERDICT: G3 SURFACE GROWTH — %s gained 1 unauthorized symbol: Hole", widenings[0].Package)
}

// TestExample_WidenSuppressesAuthorized demonstrates that an authorized entity
// in the widen list does not trigger a widening.
func TestExample_WidenSuppressesAuthorized(t *testing.T) {
	mod := "example.com/m"

	base := &graph.Graph{
		Module: mod,
		Packages: []graph.Package{
			{Path: mod + "/internal/graph", Name: "graph", Internal: true, Surface: []graph.Symbol{
				{Kind: "type", Name: "Package"},
				{Kind: "type", Name: "Edge"},
			}},
		},
	}
	base.Sort()

	head := &graph.Graph{
		Module: mod,
		Packages: []graph.Package{
			{Path: mod + "/internal/graph", Name: "graph", Internal: true, Surface: []graph.Symbol{
				{Kind: "type", Name: "Package"},
				{Kind: "type", Name: "Edge"},
				{Kind: "type", Name: "Hole"},
			}},
		},
	}
	head.Sort()

	// "Hole" IS in the widen list — it's authorized.
	policy := &SurfacePolicy{
		Fixed: map[string]bool{mod + "/internal/graph": true},
		Widen: map[string][]string{mod + "/internal/graph": {"Hole"}},
	}

	policyJSON, _ := json.MarshalIndent(struct {
		Fixed []string            `json:"fixed"`
		Widen map[string][]string `json:"widen"`
	}{
		Fixed: []string{mod + "/internal/graph"},
		Widen: map[string][]string{mod + "/internal/graph": {"Hole"}},
	}, "", "  ")
	t.Logf("INPUT fixed.json:\n%s", policyJSON)
	t.Logf("INPUT head adds: type Hole (authorized in widen)")

	d := delta.Compute(base, head)
	widenings := CheckSurface(d.Surface, policy)

	outJSON, _ := json.MarshalIndent(widenings, "", "  ")
	t.Logf("OUTPUT widenings:\n%s", outJSON)

	if len(widenings) != 0 {
		t.Fatalf("want 0 widenings (Hole is authorized), got %d", len(widenings))
	}
	t.Logf("VERDICT: No unauthorized surface growth. Hole is in the widen list — approved.")
}
