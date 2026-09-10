package review

import (
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// clause builds a plan-sourced clause. File == "plan" is the discriminator that
// separates these from code-extracted invariants, and Hash carries the class
// annotation rather than a body digest.
func clause(id, statement, class string) graph.Invariant {
	return graph.Invariant{Name: id, File: "plan", Hash: class, Statement: statement}
}

// codeInv builds a code-extracted invariant, where Hash IS a body digest.
func codeInv(name string) graph.Invariant {
	return graph.Invariant{Name: name, File: "thing_test.go", Hash: "abc123def456"}
}

// planWithClauses declares clauses on pkg a and pkg b.
func planWithClauses() *graph.Graph {
	return &graph.Graph{
		Module: mod,
		Packages: []graph.Package{
			{Path: mod + "/a", Name: "a", Internal: true, Invariants: []graph.Invariant{
				clause("BC-A2", "Validate never panics", "evidenced: fuzz"),
				clause("BC-A1", "Login rejects empty credentials", "evidenced: property_test"),
			}},
			{Path: mod + "/b", Name: "b", Internal: true, Invariants: []graph.Invariant{
				clause("BC-B1", "capacity is conserved", "attested:design"),
			}},
		},
	}
}

func TestClauseReportListsClausesForTouchedPackage(t *testing.T) {
	d := &delta.Delta{Surface: []delta.SurfaceChange{{Package: mod + "/a"}}}

	rows := ClauseReport(planWithClauses(), baseGraph(), d)

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (both clauses on pkg a): %+v", len(rows), rows)
	}
	// Sorted by package then clause ID.
	if rows[0].ID != "BC-A1" || rows[1].ID != "BC-A2" {
		t.Errorf("rows not sorted by ID: %s, %s", rows[0].ID, rows[1].ID)
	}
	if rows[0].Statement != "Login rejects empty credentials" {
		t.Errorf("statement = %q, want the plan's prose", rows[0].Statement)
	}
	if rows[0].Class != "evidenced: property_test" {
		t.Errorf("class = %q, want the declared evidence type", rows[0].Class)
	}
}

func TestClauseReportExcludesUntouchedPackages(t *testing.T) {
	// Only pkg a is touched; pkg b declares a clause but must not be reported.
	d := &delta.Delta{Surface: []delta.SurfaceChange{{Package: mod + "/a"}}}

	for _, r := range ClauseReport(planWithClauses(), baseGraph(), d) {
		if r.Package == mod+"/b" {
			t.Errorf("clause %s on untouched package b was reported", r.ID)
		}
	}
}

func TestClauseReportCountsEdgeEndpointsAsTouched(t *testing.T) {
	// A new arrow a -> b implicates promises on BOTH ends: the reviewer needs to
	// know what b guarantees now that a depends on it.
	d := &delta.Delta{EdgesAdded: []graph.Edge{edge("a", "b", "import", "a/x.go")}}

	rows := ClauseReport(planWithClauses(), baseGraph(), d)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (both endpoints): %+v", len(rows), rows)
	}
}

func TestClauseReportBindsTestByNamingConvention(t *testing.T) {
	head := baseGraph()
	// BC-A1 has a conventionally-named test; BC-A2 does not.
	for i := range head.Packages {
		if head.Packages[i].Path == mod+"/a" {
			head.Packages[i].Invariants = []graph.Invariant{codeInv("TestBC_A1")}
		}
	}
	d := &delta.Delta{Surface: []delta.SurfaceChange{{Package: mod + "/a"}}}

	rows := ClauseReport(planWithClauses(), head, d)

	byID := map[string]ClauseRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if got := byID["BC-A1"]; got.BoundTest != "TestBC_A1" || !got.Bound() {
		t.Errorf("BC-A1 bound test = %q, want TestBC_A1", got.BoundTest)
	}
	if got := byID["BC-A2"]; got.BoundTest != "" || got.Bound() {
		t.Errorf("BC-A2 bound test = %q, want empty (no such test)", got.BoundTest)
	}
}

// The two kinds of Invariant share a struct, and mixing them is the main hazard
// here: a plan clause's Hash holds a class annotation, not a digest.
func TestClauseReportIgnoresCodeInvariantsAsClauses(t *testing.T) {
	// A plan package whose Invariants somehow include a code-extracted entry.
	p := &graph.Graph{Module: mod, Packages: []graph.Package{
		{Path: mod + "/a", Name: "a", Internal: true, Invariants: []graph.Invariant{
			clause("BC-A1", "a real clause", "evidenced: fuzz"),
			codeInv("TestSomethingElse"),
		}},
	}}
	d := &delta.Delta{Surface: []delta.SurfaceChange{{Package: mod + "/a"}}}

	rows := ClauseReport(p, baseGraph(), d)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 — a code invariant must not be rendered as a clause: %+v", len(rows), rows)
	}
	if rows[0].ID != "BC-A1" {
		t.Errorf("reported %q, want BC-A1", rows[0].ID)
	}
}

// A clause must not be able to satisfy itself: plan entries live in the same
// slice as code invariants, so binding has to skip them.
func TestClauseCannotEvidenceItself(t *testing.T) {
	head := baseGraph()
	for i := range head.Packages {
		if head.Packages[i].Path == mod+"/a" {
			// A plan-sourced entry literally named TestBC_A1.
			head.Packages[i].Invariants = []graph.Invariant{
				{Name: "TestBC_A1", File: "plan", Hash: "evidenced: fuzz"},
			}
		}
	}
	d := &delta.Delta{Surface: []delta.SurfaceChange{{Package: mod + "/a"}}}

	for _, r := range ClauseReport(planWithClauses(), head, d) {
		if r.ID == "BC-A1" && r.BoundTest != "" {
			t.Errorf("BC-A1 bound to a plan-sourced entry %q; only real tests may count", r.BoundTest)
		}
	}
}

func TestClauseReportNilWithoutPlan(t *testing.T) {
	d := &delta.Delta{Surface: []delta.SurfaceChange{{Package: mod + "/a"}}}
	if rows := ClauseReport(nil, baseGraph(), d); rows != nil {
		t.Errorf("got %+v, want nil without a plan", rows)
	}
}

func TestClauseReportNilWhenNothingTouched(t *testing.T) {
	if rows := ClauseReport(planWithClauses(), baseGraph(), &delta.Delta{}); rows != nil {
		t.Errorf("got %+v, want nil when the delta touches nothing", rows)
	}
}

// --- Markdown rendering ---

func TestClauseTableRendersPromiseAndGap(t *testing.T) {
	var b strings.Builder
	writeClauseTable(&b, []ClauseRow{
		{Package: mod + "/a", ID: "BC-A1", Statement: "Login rejects empty credentials",
			Class: "evidenced: property_test", BoundTest: "TestBC_A1"},
		{Package: mod + "/a", ID: "BC-A2", Statement: "Validate never panics",
			Class: "evidenced: fuzz"},
	})
	out := b.String()

	for _, want := range []string{
		"Contract clauses implicated (2, 1 without evidence)",
		"BC-A1", "Login rejects empty credentials", "evidenced: property_test", "TestBC_A1",
		"BC-A2", "Validate never panics", "none found",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("clause table missing %q\n---\n%s", want, out)
		}
	}
}

func TestClauseTableOmittedWhenEmpty(t *testing.T) {
	var b strings.Builder
	writeClauseTable(&b, nil)
	if b.String() != "" {
		t.Errorf("rendered a section for zero clauses: %q", b.String())
	}
}

// A pipe in the prose would split the Markdown row and corrupt every column
// after it.
func TestClauseTableEscapesPipeInStatement(t *testing.T) {
	var b strings.Builder
	writeClauseTable(&b, []ClauseRow{
		{Package: mod + "/a", ID: "BC-P1", Statement: "Bank |= Classifier holds", Class: "attested:design"},
	})
	out := b.String()
	if strings.Contains(out, "Bank |= Classifier") {
		t.Errorf("unescaped pipe left in table cell:\n%s", out)
	}
	if !strings.Contains(out, `Bank \|= Classifier`) {
		t.Errorf("pipe not escaped as expected:\n%s", out)
	}
}

func TestClauseSectionAbsentFromMarkdownWithoutPlan(t *testing.T) {
	gA, gB := baseGraph(), baseGraph()
	d := delta.Compute(gA, gB)
	res := Build(gA, gB, d, Options{})

	if res.Clauses != nil {
		t.Errorf("Clauses populated without a plan: %+v", res.Clauses)
	}
	if strings.Contains(renderMarkdown(res), "Contract clauses implicated") {
		t.Error("clause section rendered without a plan")
	}
}
