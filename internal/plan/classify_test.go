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
	p := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Hole: true},
		},
	}
	base := &graph.Graph{}
	head := &graph.Graph{
		Packages: []graph.Package{
			{Path: "m/a", Files: []string{"a.go"}},
			{Path: "m/unplanned", Files: []string{"u.go"}},
		},
	}
	r := Classify(p, base, head)
	if r.Verdict != Exceeds {
		t.Fatalf("want EXCEEDS (added unplanned package), got %s: %s", r.Verdict, r.Reason)
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
