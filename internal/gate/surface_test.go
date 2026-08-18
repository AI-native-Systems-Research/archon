package gate

import (
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

func sym(kind, name string) graph.Symbol {
	return graph.Symbol{Kind: kind, Name: name}
}

func TestCheckSurface_NilPolicy(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/a", Added: []graph.Symbol{sym("func", "New")}},
	}
	got := CheckSurface(surface, nil)
	if len(got) != 0 {
		t.Fatalf("nil policy should produce no widenings, got %d", len(got))
	}
}

func TestCheckSurface_EmptyFixed(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/a", Added: []graph.Symbol{sym("func", "New")}},
	}
	got := CheckSurface(surface, &SurfacePolicy{})
	if len(got) != 0 {
		t.Fatalf("empty fixed should produce no widenings, got %d", len(got))
	}
}

func TestCheckSurface_FixedWithAdditions(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/a", Added: []graph.Symbol{sym("func", "New"), sym("type", "Thing")}},
	}
	policy := &SurfacePolicy{Fixed: map[string]bool{"example.com/m/a": true}}
	got := CheckSurface(surface, policy)
	if len(got) != 1 {
		t.Fatalf("want 1 widening, got %d", len(got))
	}
	if got[0].Package != "example.com/m/a" {
		t.Errorf("package = %q, want example.com/m/a", got[0].Package)
	}
	if len(got[0].Added) != 2 {
		t.Errorf("added count = %d, want 2", len(got[0].Added))
	}
}

func TestCheckSurface_FixedWithOnlyRemovals(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/a", Removed: []graph.Symbol{sym("func", "Old")}},
	}
	policy := &SurfacePolicy{Fixed: map[string]bool{"example.com/m/a": true}}
	got := CheckSurface(surface, policy)
	if len(got) != 0 {
		t.Fatalf("removal-only should produce no widenings, got %d", len(got))
	}
}

func TestCheckSurface_NonFixedWithAdditions(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/a", Added: []graph.Symbol{sym("func", "New")}},
	}
	policy := &SurfacePolicy{Fixed: map[string]bool{"example.com/m/b": true}}
	got := CheckSurface(surface, policy)
	if len(got) != 0 {
		t.Fatalf("non-fixed package should produce no widenings, got %d", len(got))
	}
}

func TestCheckSurface_EmptySurfaceChanges(t *testing.T) {
	policy := &SurfacePolicy{Fixed: map[string]bool{"example.com/m/a": true}}
	got := CheckSurface(nil, policy)
	if len(got) != 0 {
		t.Fatalf("empty surface should produce no widenings, got %d", len(got))
	}
}

func TestCheckSurface_MultiplePackagesSorted(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/z", Added: []graph.Symbol{sym("func", "Z")}},
		{Package: "example.com/m/a", Added: []graph.Symbol{sym("func", "A")}},
	}
	policy := &SurfacePolicy{Fixed: map[string]bool{
		"example.com/m/z": true,
		"example.com/m/a": true,
	}}
	got := CheckSurface(surface, policy)
	if len(got) != 2 {
		t.Fatalf("want 2 widenings, got %d", len(got))
	}
	if got[0].Package != "example.com/m/a" {
		t.Errorf("first package = %q, want example.com/m/a (sorted)", got[0].Package)
	}
	if got[1].Package != "example.com/m/z" {
		t.Errorf("second package = %q, want example.com/m/z (sorted)", got[1].Package)
	}
}

func TestCheckSurface_WidenSuppresses(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/graph", Added: []graph.Symbol{sym("type", "Hole"), sym("type", "Extra")}},
	}
	policy := &SurfacePolicy{
		Fixed: map[string]bool{"example.com/m/graph": true},
		Widen: map[string][]string{"example.com/m/graph": {"Hole"}},
	}
	got := CheckSurface(surface, policy)
	if len(got) != 1 {
		t.Fatalf("want 1 widening (Extra not suppressed), got %d", len(got))
	}
	if len(got[0].Added) != 1 || got[0].Added[0].Name != "Extra" {
		t.Errorf("want only Extra reported, got %v", got[0].Added)
	}
}

func TestCheckSurface_WidenSuppressesAll(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/graph", Added: []graph.Symbol{sym("type", "Hole")}},
	}
	policy := &SurfacePolicy{
		Fixed: map[string]bool{"example.com/m/graph": true},
		Widen: map[string][]string{"example.com/m/graph": {"Hole"}},
	}
	got := CheckSurface(surface, policy)
	if len(got) != 0 {
		t.Fatalf("all additions are widened, want 0 widenings, got %d", len(got))
	}
}

func TestCheckSurface_WidenWrongEntityDoesNotSuppress(t *testing.T) {
	surface := []delta.SurfaceChange{
		{Package: "example.com/m/graph", Added: []graph.Symbol{sym("type", "Hole")}},
	}
	policy := &SurfacePolicy{
		Fixed: map[string]bool{"example.com/m/graph": true},
		Widen: map[string][]string{"example.com/m/graph": {"Other"}},
	}
	got := CheckSurface(surface, policy)
	if len(got) != 1 {
		t.Fatalf("widen names wrong entity, want 1 widening, got %d", len(got))
	}
	if got[0].Added[0].Name != "Hole" {
		t.Errorf("want Hole reported, got %s", got[0].Added[0].Name)
	}
}
