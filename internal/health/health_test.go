package health_test

import (
	"encoding/json"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
	"github.com/AI-native-Systems-Research/archon/internal/health"
)

// tiedGraph is built so that the two things Analyze orders by are both
// ambiguous. Every leaf has the same blast radius and the same fan-in, so the
// coupling table can only be ordered by something else; and there are two
// separate cycles, so the list of cycles can only be ordered by something else
// too. Both slices Analyze fills come from iterating maps.
func tiedGraph() *graph.Graph {
	g := &graph.Graph{Module: "example.com/m"}
	add := func(path string) {
		g.Packages = append(g.Packages, graph.Package{Path: path, Name: path, Internal: true})
	}
	// Four leaves nothing depends on: identical fan-in (0) and blast radius (0).
	for _, p := range []string{"z/leaf", "a/leaf", "m/leaf", "d/leaf"} {
		add(p)
	}
	// Two independent two-package cycles.
	for _, p := range []string{"cyc1/a", "cyc1/b", "cyc2/a", "cyc2/b"} {
		add(p)
	}
	g.Edges = []graph.Edge{
		{From: "cyc1/a", To: "cyc1/b", Kind: "import"},
		{From: "cyc1/b", To: "cyc1/a", Kind: "import"},
		{From: "cyc2/a", To: "cyc2/b", Kind: "import"},
		{From: "cyc2/b", To: "cyc2/a", Kind: "import"},
	}
	return g
}

// TestAnalyzeIsDeterministic is the property the tool claims: same input, same
// output. Analyze builds both of its slices by iterating maps, so a run-to-run
// difference means the report cannot be diffed across commits and `--json`
// cannot be consumed by a script.
//
// Repeated in one process on purpose: Go randomises map iteration on every
// range, so the same graph analysed twice is enough to catch it.
func TestAnalyzeIsDeterministic(t *testing.T) {
	g := tiedGraph()

	first, err := json.Marshal(health.Analyze(g))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		next, err := json.Marshal(health.Analyze(g))
		if err != nil {
			t.Fatal(err)
		}
		if string(next) != string(first) {
			t.Fatalf("run %d differs from the first:\n first %s\n  then %s", i+2, first, next)
		}
	}
}

// TestCouplingRowsAreOrderedByPathWhenTied pins the order rather than only its
// stability, so the tiebreak cannot be swapped for another arbitrary one.
func TestCouplingRowsAreOrderedByPathWhenTied(t *testing.T) {
	r := health.Analyze(tiedGraph())

	var leaves []string
	for _, p := range r.Packages {
		if p.BlastRadius == 0 && p.FanIn == 0 {
			leaves = append(leaves, p.Path)
		}
	}
	want := []string{"a/leaf", "d/leaf", "m/leaf", "z/leaf"}
	if len(leaves) != len(want) {
		t.Fatalf("expected %d tied leaves, got %v", len(want), leaves)
	}
	for i := range want {
		if leaves[i] != want[i] {
			t.Errorf("tied rows out of order: got %v, want %v", leaves, want)
			break
		}
	}
}

// TestCyclesAreOrderedDeterministically does the same for the cycle list, which
// is ordered by the search's starting point.
func TestCyclesAreOrderedDeterministically(t *testing.T) {
	r := health.Analyze(tiedGraph())

	if len(r.Cycles) != 2 {
		t.Fatalf("expected the two cycles in the fixture, got %v", r.Cycles)
	}
	if r.Cycles[0][0] != "cyc1/a" || r.Cycles[1][0] != "cyc2/a" {
		t.Errorf("cycles out of order: got %v", r.Cycles)
	}
}
