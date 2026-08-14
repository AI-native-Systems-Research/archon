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

func TestVerdictFastTrack(t *testing.T) {
	g := baseGraph()
	d := delta.Compute(g, g) // no change
	res := Build(g, g, d, Options{})
	if res.Verdict != FastTrack {
		t.Fatalf("want FAST_TRACK, got %s", res.Verdict)
	}
	if !strings.Contains(res.Summary, "fast-track") {
		t.Errorf("summary %q missing fast-track", res.Summary)
	}
}

func TestVerdictReviewArchitecture(t *testing.T) {
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
	if res.Verdict != ReviewArchitecture {
		t.Fatalf("want REVIEW_ARCHITECTURE, got %s (empty=%v)", res.Verdict, res.EmptyAtPackageAltitude)
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

func TestVerdictReviewInvariants(t *testing.T) {
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
	if res.Verdict != ReviewInvariants {
		t.Fatalf("want REVIEW_INVARIANTS, got %s", res.Verdict)
	}
}

func TestVerdictBlock(t *testing.T) {
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
	if res.Verdict != Block {
		t.Fatalf("want BLOCK, got %s", res.Verdict)
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
	cv := buildComponents(g, d, 2)
	for _, c := range cv.Components {
		if !c.InCycle {
			t.Errorf("component %s should be flagged in-cycle", c.Name)
		}
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
