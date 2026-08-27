package plan

import (
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

func TestClassify_Realizes(t *testing.T) {
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	base := &graph.Graph{}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	r := Classify(p, base, head)
	if r.Verdict != Realizes {
		t.Fatalf("want REALIZES (filled a hole), got %s: %s", r.Verdict, r.Reason)
	}
}

func TestClassify_Conflicts(t *testing.T) {
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
		Edges: []graph.Edge{{From: "m/a", To: "m/c", Kind: "import"}},
	}
	r := Classify(p, base, head)
	if r.Verdict != Conflicts {
		t.Fatalf("want CONFLICTS (disallowed arrow), got %s: %s", r.Verdict, r.Reason)
	}
}

func TestClassify_Exceeds(t *testing.T) {
	// PR fills the hole (touches plan) AND adds an unplanned edge from a plan package
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
			{Path: "m/b"},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/b", Files: []string{"b.go"}},
		},
	}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
			{Path: "m/b", Files: []string{"b.go"}},
		},
		Edges: []graph.Edge{
			{From: "m/cmd", To: "m/a", Kind: "import"},
			{From: "m/a", To: "m/b", Kind: "call"}, // unplanned edge from plan-declared package
		},
	}
	r := Classify(p, base, head)
	if r.Verdict != Exceeds {
		t.Fatalf("want EXCEEDS (fills hole + adds unplanned edge), got %s: %s", r.Verdict, r.Reason)
	}
}

func TestClassify_Unrelated(t *testing.T) {
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/planned", Hole: true},
		},
	}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/other", Files: []string{"o.go"}},
		},
	}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/other", Files: []string{"o.go", "o2.go"}},
		},
	}
	r := Classify(p, base, head)
	if r.Verdict != Unrelated {
		t.Fatalf("want UNRELATED (touched nothing plan declares), got %s: %s", r.Verdict, r.Reason)
	}
}

func TestClassify_Exceeds_UnplannedEdgeBetweenPlanPackages(t *testing.T) {
	// PR adds a call edge between two plan-declared packages without filling any holes
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a"},
			{Path: "m/b"},
		},
	}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
		},
	}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
		},
		Edges: []graph.Edge{{From: "m/a", To: "m/b", Kind: "call"}},
	}
	r := Classify(p, base, head)
	if r.Verdict != Exceeds {
		t.Fatalf("want EXCEEDS (unplanned edge between plan packages), got %s: %s", r.Verdict, r.Reason)
	}
}

func TestClassify_Realizes_CallEdgeImpliedByDeclaredImport(t *testing.T) {
	// A plan declares `hello -> util : import`. An implementation that calls into
	// util yields both an import and a call edge for that pair; the call must not
	// be counted as unplanned structure.
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/util"},
			{Path: "m/hello", Hole: true},
		},
		Edges: []graph.Edge{{From: "m/hello", To: "m/util", Kind: "import"}},
	}
	base := &graph.Graph{
		Packages: []graph.Package{{Path: "m/util", Files: []string{"util.go"}}},
	}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/util", Files: []string{"util.go"}},
			{Path: "m/hello", Files: []string{"hello.go"}},
		},
		Edges: []graph.Edge{
			{From: "m/hello", To: "m/util", Kind: "import"},
			{From: "m/hello", To: "m/util", Kind: "call"},
		},
	}
	r := Classify(p, base, head)
	if r.Verdict != Realizes {
		t.Fatalf("want REALIZES (call implied by declared import), got %s: %s %v", r.Verdict, r.Reason, r.Offending)
	}
}

func TestClassify_Exceeds_NamesUnplannedEdge(t *testing.T) {
	p := &graph.Graph{
		Packages: []graph.Package{{Path: "m/a"}, {Path: "m/b"}},
	}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
		},
	}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
		},
		Edges: []graph.Edge{{From: "m/a", To: "m/b", Kind: "call"}},
	}
	r := Classify(p, base, head)
	want := []string{"undeclared: m/a -> m/b : call"}
	if r.Verdict != Exceeds || !equalStrs(r.Offending, want) {
		t.Fatalf("want EXCEEDS naming %v, got %s %v", want, r.Verdict, r.Offending)
	}
}

func TestClassify_Conflicts_NamesDisallowedArrow(t *testing.T) {
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
		Packages: base.Packages,
		Edges:    []graph.Edge{{From: "m/a", To: "m/c", Kind: "import"}},
	}
	r := Classify(p, base, head)
	want := []string{"C4 m/a: arrow m/a -> m/c (import) not in Allow"}
	if r.Verdict != Conflicts || !equalStrs(r.Offending, want) {
		t.Fatalf("want CONFLICTS naming %v, got %s %v", want, r.Verdict, r.Offending)
	}
}

func TestClassify_Conflicts_OmitsAlreadyUnmetObligations(t *testing.T) {
	// m/gone is unmet in base and head alike; only the newly disallowed arrow is
	// this PR's doing, so only it may be named.
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Allow: []string{"m/b"}},
			{Path: "m/b"},
			{Path: "m/c"},
			{Path: "m/gone"},
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
		Packages: base.Packages,
		Edges:    []graph.Edge{{From: "m/a", To: "m/c", Kind: "import"}},
	}
	r := Classify(p, base, head)
	want := []string{"C4 m/a: arrow m/a -> m/c (import) not in Allow"}
	if r.Verdict != Conflicts || !equalStrs(r.Offending, want) {
		t.Fatalf("want CONFLICTS naming only %v, got %s %v", want, r.Verdict, r.Offending)
	}
}

func TestClassify_Conflicts_DistIncrease_NamesRemovedBox(t *testing.T) {
	// Head deletes a declared box, so dist rises without C4 firing. The removed
	// box must be named even though its class is not C4, and m/zzz — unmet in base
	// with a byte-identical detail string, and sorting after m/b — must not be
	// attributed here.
	p := &graph.Graph{
		Packages: []graph.Package{{Path: "m/a"}, {Path: "m/b"}, {Path: "m/zzz"}},
	}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
		},
	}
	head := &graph.Graph{
		Packages: []graph.Package{{Path: "m/a", Files: []string{"a.go"}}},
	}
	r := Classify(p, base, head)
	want := []string{"C2 m/b: declared box absent from actual"}
	if r.Verdict != Conflicts || !equalStrs(r.Offending, want) {
		t.Fatalf("want CONFLICTS naming only %v, got %s %v", want, r.Verdict, r.Offending)
	}
}

func TestClassify_Exceeds_OffendingIsSorted(t *testing.T) {
	p := &graph.Graph{Packages: []graph.Package{{Path: "m/a"}, {Path: "m/b"}, {Path: "m/c"}}}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/b", Files: []string{"b.go"}},
			{Path: "m/c", Files: []string{"c.go"}},
		},
	}
	head := &graph.Graph{
		Packages: base.Packages,
		Edges: []graph.Edge{
			{From: "m/a", To: "m/c", Kind: "call"},
			{From: "m/a", To: "m/b", Kind: "call"},
		},
	}
	r := Classify(p, base, head)
	want := []string{"undeclared: m/a -> m/b : call", "undeclared: m/a -> m/c : call"}
	if !equalStrs(r.Offending, want) {
		t.Fatalf("Offending must be sorted for a byte-stable review; want %v, got %v", want, r.Offending)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestClassify_NilPlan(t *testing.T) {
	r := Classify(nil, &graph.Graph{}, &graph.Graph{})
	if r.Verdict != Unrelated {
		t.Fatalf("nil plan should be UNRELATED, got %s", r.Verdict)
	}
}

func TestClassify_Precedence_ConflictsWins(t *testing.T) {
	// PR fills a hole (would be Realizes) BUT also adds disallowed arrow (Conflicts wins)
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true, Allow: []string{"m/b"}, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
			{Path: "m/b"},
			{Path: "m/c"},
		},
		Edges: []graph.Edge{{From: "m/cmd", To: "m/a", Kind: "import"}},
	}
	base := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/b", Files: []string{"b.go"}},
			{Path: "m/c", Files: []string{"c.go"}},
		},
	}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}, Surface: []graph.Symbol{{Kind: "func", Name: "Run"}}},
			{Path: "m/b", Files: []string{"b.go"}},
			{Path: "m/c", Files: []string{"c.go"}},
		},
		Edges: []graph.Edge{
			{From: "m/cmd", To: "m/a", Kind: "import"},
			{From: "m/a", To: "m/c", Kind: "import"}, // disallowed
		},
	}
	r := Classify(p, base, head)
	if r.Verdict != Conflicts {
		t.Fatalf("want CONFLICTS (precedence: disallowed arrow wins over fill), got %s: %s", r.Verdict, r.Reason)
	}
}
