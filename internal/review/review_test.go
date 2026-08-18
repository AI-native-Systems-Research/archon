package review

import (
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

const mod = "example.com/m"

func pkg(path string, internal bool) graph.Package {
	return graph.Package{Path: mod + "/" + path, Name: lastSeg(path), Internal: internal}
}

func lastSeg(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func edge(from, to, kind string, witnesses ...string) graph.Edge {
	return graph.Edge{From: mod + "/" + from, To: mod + "/" + to, Kind: kind, Witnesses: witnesses}
}

// baseGraph: a -> b (import + call), a implements iface in b.
func baseGraph() *graph.Graph {
	g := &graph.Graph{
		Module:   mod,
		Packages: []graph.Package{pkg("a", true), pkg("b", true)},
		Edges: []graph.Edge{
			edge("a", "b", "import", "a/x.go"),
			edge("a", "b", "call", "NewThing", "DefaultThing"),
			edge("a", "b", "implements", "Bank |= Classifier"),
		},
	}
	g.Sort()
	return g
}

func TestVerdictNoChange(t *testing.T) {
	g := baseGraph()
	d := delta.Compute(g, g) // no change
	res := Build(g, g, d, Options{})
	if res.Verdict != NoChange {
		t.Fatalf("want NO_CHANGE, got %s", res.Verdict)
	}
	if !strings.Contains(res.Summary, "fast-track") {
		t.Errorf("summary %q missing fast-track", res.Summary)
	}
}

func TestVerdictArchitecturalChange(t *testing.T) {
	a := baseGraph()
	// head removes the implements edge (a fully decoupled from b at that seam).
	b := &graph.Graph{
		Module:   mod,
		Packages: []graph.Package{pkg("a", true), pkg("b", true)},
		Edges: []graph.Edge{
			edge("a", "b", "import", "a/x.go"),
			edge("a", "b", "call", "NewThing"), // dropped DefaultThing -> WEAKENED
		},
	}
	b.Sort()
	d := delta.Compute(a, b)
	res := Build(a, b, d, Options{})
	if res.Verdict != ArchitecturalChange {
		t.Fatalf("want ARCHITECTURAL_CHANGE, got %s (empty=%v)", res.Verdict, res.EmptyAtPackageAltitude)
	}

	// Witness: implements edge REMOVED (full), call edge WEAKENED (partial).
	var sawRemoved, sawWeak bool
	for _, w := range res.Witnesses {
		if w.Kind == "implements" && w.Status == wsRemoved {
			sawRemoved = true
		}
		if w.Kind == "call" && w.Status == wsWeakened {
			sawWeak = true
			if len(w.Remaining) != 1 || w.Remaining[0] != "NewThing" {
				t.Errorf("weakened remaining = %v, want [NewThing]", w.Remaining)
			}
		}
	}
	if !sawRemoved || !sawWeak {
		t.Errorf("witness rows missing: removed=%v weak=%v rows=%+v", sawRemoved, sawWeak, res.Witnesses)
	}
	if res.Counts.WitnessesFull != 1 || res.Counts.WitnessesWeak != 1 {
		t.Errorf("counts full=%d weak=%d, want 1/1", res.Counts.WitnessesFull, res.Counts.WitnessesWeak)
	}
}

// An invariant-only change (the package boundary did not move) is NO_CHANGE
// under the binary verdict — the guarded-promise detail still rides along in
// review.json, but it does not trip an architecture review.
func TestVerdictInvariantOnlyIsNoChange(t *testing.T) {
	a := baseGraph()
	// Same boundary, but head adds a guarding test (invariant) to package a.
	b := baseGraph()
	for i := range b.Packages {
		if b.Packages[i].Path == mod+"/a" {
			b.Packages[i].Invariants = []graph.Invariant{
				{Name: "TestPromise", File: "a_test.go", Hash: "deadbeef"},
			}
		}
	}
	b.Sort()
	d := delta.Compute(a, b)
	res := Build(a, b, d, Options{})
	if res.Verdict != NoChange {
		t.Fatalf("want NO_CHANGE (invariant-only, empty boundary), got %s", res.Verdict)
	}
	// The note in review.md should still point at the touched promise.
	md := renderMarkdown(res)
	if !strings.Contains(md, "guarded promise") {
		t.Errorf("review.md should note the touched guarded promise:\n%s", md)
	}
}

// An added dependency moves the boundary -> ARCHITECTURAL_CHANGE. When an
// allow-list is supplied, the off-baseline edge is still recorded as a violation
// (surfaced in the report), but there is no separate BLOCK verdict.
func TestVerdictViolationRecorded(t *testing.T) {
	a := baseGraph()
	// head adds a new dependency a -> c that the allow-list forbids.
	b := &graph.Graph{
		Module:   mod,
		Packages: []graph.Package{pkg("a", true), pkg("b", true), pkg("c", true)},
		Edges: []graph.Edge{
			edge("a", "b", "import", "a/x.go"),
			edge("a", "b", "call", "NewThing", "DefaultThing"),
			edge("a", "b", "implements", "Bank |= Classifier"),
			edge("a", "c", "import", "a/y.go"),
		},
	}
	b.Sort()
	d := delta.Compute(a, b)
	allow := map[string][]string{
		mod + "/a": {mod + "/b"}, // a may import b, but NOT c
	}
	d.CheckContract(b, allow)
	res := Build(a, b, d, Options{})
	if res.Verdict != ArchitecturalChange {
		t.Fatalf("want ARCHITECTURAL_CHANGE, got %s", res.Verdict)
	}
	if res.Counts.Violations == 0 {
		t.Errorf("expected a violation recorded")
	}
}

// TestDeterminism: Build produces identical review.md and review.json bytes on
// repeated runs of the same input.
func TestDeterminism(t *testing.T) {
	a := baseGraph()
	b := &graph.Graph{
		Module:   mod,
		Packages: []graph.Package{pkg("a", true), pkg("b", true)},
		Edges: []graph.Edge{
			edge("a", "b", "import", "a/x.go"),
			edge("a", "b", "call", "NewThing"),
		},
	}
	b.Sort()

	render := func() (string, string) {
		d := delta.Compute(a, b)
		res := Build(a, b, d, Options{LabelA: "base", LabelB: "head"})
		return renderMarkdown(res), res.Components.Mermaid
	}
	md1, mm1 := render()
	md2, mm2 := render()
	if md1 != md2 {
		t.Error("review.md not deterministic")
	}
	if mm1 != mm2 {
		t.Error("component Mermaid not deterministic")
	}
	// Spot-check the Mermaid mentions both components and the four-color classes.
	if !strings.Contains(mm1, "classDef boundary") {
		t.Errorf("mermaid missing classDef boundary:\n%s", mm1)
	}
}

func TestComponentCycleDetection(t *testing.T) {
	// a -> b and b -> a at the package altitude -> a cycle between components.
	g := &graph.Graph{
		Module:   mod,
		Packages: []graph.Package{pkg("a", true), pkg("b", true)},
		Edges: []graph.Edge{
			edge("a", "b", "import", "a/x.go"),
			edge("b", "a", "import", "b/y.go"),
		},
	}
	g.Sort()
	d := delta.Compute(g, g)
	cv := buildComponents(g, g, d, 2)
	for _, c := range cv.Components {
		if !c.InCycle {
			t.Errorf("component %s should be flagged in-cycle", c.Name)
		}
	}
}

func TestSurfaceWideningReported(t *testing.T) {
	a := baseGraph()
	// head adds a new exported symbol to package b.
	b := &graph.Graph{
		Module: mod,
		Packages: []graph.Package{
			pkg("a", true),
			{Path: mod + "/b", Name: "b", Internal: true, Surface: []graph.Symbol{
				{Kind: "func", Name: "NewThing"},
				{Kind: "func", Name: "Extra"},
			}},
		},
		Edges: []graph.Edge{
			edge("a", "b", "import", "a/x.go"),
			edge("a", "b", "call", "NewThing", "DefaultThing"),
			edge("a", "b", "implements", "Bank |= Classifier"),
		},
	}
	b.Sort()
	d := delta.Compute(a, b)
	fixed := map[string]bool{mod + "/b": true}
	res := Build(a, b, d, Options{Fixed: fixed})
	if res.Counts.SurfaceWidenings != 1 {
		t.Fatalf("want 1 widening, got %d", res.Counts.SurfaceWidenings)
	}
	if res.Widenings[0].Package != mod+"/b" {
		t.Errorf("widening package = %q", res.Widenings[0].Package)
	}
	md := renderMarkdown(res)
	if !strings.Contains(md, "G3") {
		t.Errorf("review.md should contain G3 surface growth section:\n%s", md)
	}
}

func TestNoWideningWithoutFixed(t *testing.T) {
	a := baseGraph()
	b := &graph.Graph{
		Module: mod,
		Packages: []graph.Package{
			pkg("a", true),
			{Path: mod + "/b", Name: "b", Internal: true, Surface: []graph.Symbol{
				{Kind: "func", Name: "NewThing"},
				{Kind: "func", Name: "Extra"},
			}},
		},
		Edges: []graph.Edge{
			edge("a", "b", "import", "a/x.go"),
			edge("a", "b", "call", "NewThing", "DefaultThing"),
			edge("a", "b", "implements", "Bank |= Classifier"),
		},
	}
	b.Sort()
	d := delta.Compute(a, b)
	res := Build(a, b, d, Options{})
	if res.Counts.SurfaceWidenings != 0 {
		t.Fatalf("without Fixed, want 0 widenings, got %d", res.Counts.SurfaceWidenings)
	}
	if len(res.Widenings) != 0 {
		t.Fatalf("without Fixed, want nil widenings, got %v", res.Widenings)
	}
}

func TestCompKey(t *testing.T) {
	cases := []struct {
		rel   string
		depth int
		want  string
	}{
		{"", 2, "(root)"},
		{"sim", 2, "sim"},
		{"sim/saturation", 2, "sim/saturation"},
		{"sim/saturation/inner", 2, "sim/saturation"},
		{"sim/saturation/inner", 1, "sim"},
	}
	for _, c := range cases {
		if got := compKey(c.rel, c.depth); got != c.want {
			t.Errorf("compKey(%q,%d) = %q, want %q", c.rel, c.depth, got, c.want)
		}
	}
}
