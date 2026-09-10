package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/extract"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
	"github.com/AI-native-Systems-Research/archon/internal/plan"
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

	rows := ClauseReport(planWithClauses(), baseGraph(), d)
	if len(rows) == 0 {
		t.Fatal("no rows at all — the exclusion below would pass vacuously")
	}
	for _, r := range rows {
		if r.Package == mod+"/b" {
			t.Errorf("clause %s on untouched package b was reported", r.ID)
		}
	}
}

// Every axis of the delta must mark a package touched. Without one case per
// axis, a deleted loop in touchedPackages goes unnoticed.
func TestClauseReportEveryDeltaAxisCountsAsTouched(t *testing.T) {
	a := mod + "/a"
	cases := []struct {
		axis string
		d    *delta.Delta
	}{
		{"PackagesAdded", &delta.Delta{PackagesAdded: []delta.PackageRef{{Path: a}}}},
		{"PackagesRemoved", &delta.Delta{PackagesRemoved: []delta.PackageRef{{Path: a}}}},
		{"Surface", &delta.Delta{Surface: []delta.SurfaceChange{{Package: a}}}},
		{"SchemaChanges", &delta.Delta{SchemaChanges: []delta.SurfaceChange{{Package: a}}}},
		{"EdgesAdded", &delta.Delta{EdgesAdded: []graph.Edge{{From: a, To: mod + "/z", Kind: "import"}}}},
		{"EdgesRemoved", &delta.Delta{EdgesRemoved: []graph.Edge{{From: a, To: mod + "/z", Kind: "import"}}}},
		{"Invariants", &delta.Delta{Invariants: []delta.InvariantChange{{Package: a}}}},
		{"Invariants/GuardedContracts", &delta.Delta{Invariants: []delta.InvariantChange{
			{Package: mod + "/z", Modified: []string{"TestX"}, GuardedContracts: []string{a + ".Cache"}}}}},
		{"Contracts/Interface", &delta.Delta{Contracts: []delta.ContractChange{{Interface: a + ".Store"}}}},
		{"Contracts/Implementer", &delta.Delta{Contracts: []delta.ContractChange{
			{Interface: mod + "/z.Iface", ImplementersAdded: []string{a + ".Impl"}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.axis, func(t *testing.T) {
			rows := ClauseReport(planWithClauses(), baseGraph(), tc.d)
			found := false
			for _, r := range rows {
				if r.Package == a {
					found = true
				}
			}
			if !found {
				t.Errorf("%s did not mark %s as touched; rows = %+v", tc.axis, a, rows)
			}
		})
	}
}

func TestPkgOfQualified(t *testing.T) {
	cases := map[string]string{
		"example.com/m/a.Store":    "example.com/m/a",
		"github.com/x/y/pkg.Thing": "github.com/x/y/pkg",
		"nodothere":                "",
	}
	for in, want := range cases {
		if got := pkgOfQualified(in); got != want {
			t.Errorf("pkgOfQualified(%q) = %q, want %q", in, got, want)
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

	rows := ClauseReport(planWithClauses(), head, d)
	if len(rows) == 0 {
		t.Fatal("no rows at all — the check below would pass vacuously")
	}
	for _, r := range rows {
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

	if !strings.Contains(out, "Contract clauses implicated (2, 1 without evidence)") {
		t.Errorf("header wrong or missing\n---\n%s", out)
	}
	// Whole rows, not scattered substrings: a substring check cannot tell a
	// correct table from one with its columns permuted.
	for _, wantRow := range []string{
		"| `a` | **BC-A1** | Login rejects empty credentials | evidenced: property_test | `TestBC_A1` |",
		"| `a` | **BC-A2** | Validate never panics | evidenced: fuzz | — none found |",
	} {
		if !strings.Contains(out, wantRow) {
			t.Errorf("missing row:\n  %s\n--- got ---\n%s", wantRow, out)
		}
	}
}

// The deliverable is the wiring: --plan must actually produce the section in
// review.md. Without this, deleting the ClauseReport call in Build leaves every
// other test green.
func TestClauseSectionRenderedViaBuildWithPlan(t *testing.T) {
	gA, gB := baseGraph(), baseGraph()
	// Grow pkg a's surface so the delta touches it.
	for i := range gB.Packages {
		if gB.Packages[i].Path == mod+"/a" {
			gB.Packages[i].Surface = []graph.Symbol{{Kind: "func", Name: "Added", Sig: "func()"}}
		}
	}
	d := delta.Compute(gA, gB)

	res := Build(gA, gB, d, Options{PlanGraph: planWithClauses()})

	if len(res.Clauses) == 0 {
		t.Fatalf("Build produced no clauses with a plan; delta surface = %+v", d.Surface)
	}
	out := renderMarkdown(res)
	if !strings.Contains(out, "Contract clauses implicated") {
		t.Errorf("clause section absent from review markdown\n---\n%s", out)
	}
	if !strings.Contains(out, "BC-A1") || !strings.Contains(out, "Login rejects empty credentials") {
		t.Errorf("clause detail missing from review markdown\n---\n%s", out)
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

// --- End-to-end: real extraction, real plan JSON, real Build ---

// writeTempModule writes a small module and returns its directory.
func writeTempModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return dir
}

func extractOrFail(t *testing.T, files map[string]string) *graph.Graph {
	t.Helper()
	res, err := extract.Extract(writeTempModule(t, files))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if res.NumErrors != 0 {
		t.Fatalf("fixture does not typecheck (%d errors) — the result would be meaningless", res.NumErrors)
	}
	return res.Graph
}

// The bound path had no end-to-end coverage: every other test hand-builds
// graphs, so "a real test in a real package was matched to a clause" was never
// actually exercised. This drives the whole chain — extraction, a plan compiled
// and round-tripped through JSON as the CLI does, Build, and the rendered table.
func TestClauseBindingEndToEndWithRealExtraction(t *testing.T) {
	const covMod = "example.com/cov"

	base := map[string]string{
		"go.mod": "module example.com/cov\n\ngo 1.26\n",
		"util/util.go": `package util

func Help() string { return "h" }
`,
		"kv/kv.go": `package kv

import "example.com/cov/util"

func Get() string { return util.Help() }
`,
		// A test named for clause BC-A1, which is what binding looks for.
		"kv/kv_test.go": `package kv

import "testing"

func TestBC_A1(t *testing.T) {
	if Get() == "" {
		t.Fatal("want non-empty")
	}
}
`,
	}
	// Head grows kv's surface, so the delta touches it.
	head := map[string]string{}
	for k, v := range base {
		head[k] = v
	}
	head["kv/kv.go"] = base["kv/kv.go"] + "\nfunc Put() {}\n"

	gA := extractOrFail(t, base)
	gB := extractOrFail(t, head)

	planSrc := `hole example.com/cov/kv {
  surface:
    Get() string
  allow:
    import example.com/cov/util
  contract:
    BC-A1 Get never returns empty [evidenced: property_test]
    BC-A2 Put is idempotent       [evidenced: fuzz]
}
box example.com/cov/util
`
	compiled, diags := plan.Compile([]byte(planSrc))
	if len(diags) != 0 {
		t.Fatalf("plan does not compile: %v", diags)
	}
	// Round-trip through JSON exactly as the CLI does (plan compile > file, then
	// pr-review --plan reads it back), so the prose is proven to survive.
	blob, err := json.Marshal(compiled)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	var planGraph graph.Graph
	if err := json.Unmarshal(blob, &planGraph); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}

	res := Build(gA, gB, delta.Compute(gA, gB), Options{PlanGraph: &planGraph})

	byID := map[string]ClauseRow{}
	for _, r := range res.Clauses {
		byID[r.ID] = r
	}
	if len(byID) != 2 {
		t.Fatalf("got %d clauses, want 2 (both on %s/kv): %+v", len(byID), covMod, res.Clauses)
	}

	// The case with no prior end-to-end coverage: a real test, found by name.
	if got := byID["BC-A1"]; got.BoundTest != "TestBC_A1" {
		t.Errorf("BC-A1 bound test = %q, want TestBC_A1 (the test exists in kv)", got.BoundTest)
	}
	// And the gap case, from the same run.
	if got := byID["BC-A2"]; got.BoundTest != "" {
		t.Errorf("BC-A2 bound test = %q, want empty (no TestBC_A2 exists)", got.BoundTest)
	}
	// Prose survived compile -> JSON -> review.
	if got := byID["BC-A1"].Statement; got != "Get never returns empty" {
		t.Errorf("statement = %q, want the plan's prose after a JSON round trip", got)
	}

	out := renderMarkdown(res)
	if !strings.Contains(out, "Contract clauses implicated (2, 1 without evidence)") {
		t.Errorf("header missing or miscounted\n---\n%s", out)
	}
	if !strings.Contains(out, "`TestBC_A1`") {
		t.Errorf("bound test not rendered\n---\n%s", out)
	}
}

// The Contracts axis earns its place by attributing a change to the INTERFACE's
// package. When package mem gains a new implementer of store.Cache and the
// implements edge mem -> store already existed, the surface axis marks mem (the
// new type is exported) but NOTHING marks store — yet store owns the contract
// that just gained a member, and store is where its clauses live.
//
// Verified on real code rather than argued: an earlier version of this test tried
// the unexported-implementer case, which cannot happen at all — extract's
// typeDefs skips unexported names, so an unexported type is never an implementer.
func TestContractsAxisMarksInterfacePackage(t *testing.T) {
	base := map[string]string{
		"go.mod": "module example.com/ctr\n\ngo 1.26\n",
		"store/store.go": `package store

type Cache interface {
	Get(k string) string
}
`,
		// First already satisfies Cache, so the implements edge mem -> store
		// exists in BOTH graphs and no arrow is added by the change.
		"mem/mem.go": `package mem

type First struct{}

func (f First) Get(k string) string { return k }
`,
	}
	head := map[string]string{}
	for k, v := range base {
		head[k] = v
	}
	head["mem/mem.go"] = base["mem/mem.go"] + `
type Second struct{}

func (s Second) Get(k string) string { return k }
`

	gA := extractOrFail(t, base)
	gB := extractOrFail(t, head)
	d := delta.Compute(gA, gB)

	// The implements edge already existed, so this is the "already coupled" case.
	for _, e := range d.EdgesAdded {
		if e.Kind == "implements" {
			t.Fatalf("an implements edge was added; not the already-coupled case: %+v", d.EdgesAdded)
		}
	}
	if len(d.Contracts) == 0 {
		t.Fatalf("no contract change reported, so the axis would be pointless: %+v", d)
	}

	touched := touchedPackages(d)
	// mem is marked by the surface axis anyway — Second is exported.
	if !touched["example.com/ctr/mem"] {
		t.Errorf("mem not marked touched: %+v", touched)
	}
	// store is the one only this axis can mark.
	if !touched["example.com/ctr/store"] {
		t.Errorf("store (the interface owner) not marked touched; its clauses would be "+
			"silently skipped even though its contract gained a member: contracts = %+v, touched = %+v",
			d.Contracts, touched)
	}
}

// When a contract test is modified, delta records the interfaces it guarded —
// which usually live in a DIFFERENT package from the test. That package owns the
// promise that was just weakened, so it is the strongest possible reason to show
// a clause, and it must not be dropped because only the test's own package was
// marked.
func TestGuardedContractOwnerIsTouchedOnModifiedTest(t *testing.T) {
	base := map[string]string{
		"go.mod": "module example.com/gc\n\ngo 1.26\n",
		"store/store.go": `package store

type Cache interface {
	Get(k string) string
}
`,
		"mem/mem.go": `package mem

type First struct{}

func (f First) Get(k string) string { return k }
`,
		"mem/mem_test.go": `package mem

import (
	"testing"

	"example.com/gc/store"
)

func TestCacheContract(t *testing.T) {
	var c store.Cache = First{}
	if c.Get("k") != "k" {
		t.Fatal("want k")
	}
}
`,
	}
	head := map[string]string{}
	for k, v := range base {
		head[k] = v
	}
	// Semantic edit to the contract test: the promise it guards was altered.
	head["mem/mem_test.go"] = `package mem

import (
	"testing"

	"example.com/gc/store"
)

func TestCacheContract(t *testing.T) {
	var c store.Cache = First{}
	if c.Get("other") == "" {
		t.Fatal("want non-empty")
	}
}
`

	gA := extractOrFail(t, base)
	gB := extractOrFail(t, head)
	d := delta.Compute(gA, gB)

	if len(d.Invariants) == 0 {
		t.Fatalf("no invariant change reported; this test cannot prove anything: %+v", d)
	}
	var guarded []string
	for _, ic := range d.Invariants {
		guarded = append(guarded, ic.GuardedContracts...)
	}
	if len(guarded) == 0 {
		t.Fatalf("delta bound no guarded contract to the modified test, so the "+
			"precondition fails: %+v", d.Invariants)
	}

	touched := touchedPackages(d)
	if !touched["example.com/gc/store"] {
		t.Errorf("store owns the guarded contract %v but was not marked touched; its "+
			"clauses would be skipped for the very change that weakened them: touched = %+v",
			guarded, touched)
	}
}

// cellText's em-dash branch: parseContractEntry demonstrably yields clauses with
// no prose ("BC-C2 [evidenced: fuzz]"), so an empty cell is a reachable state.
func TestClauseTableRendersEmptyFieldsAsDash(t *testing.T) {
	var b strings.Builder
	writeClauseTable(&b, []ClauseRow{{Package: mod + "/a", ID: "BC-A9"}})
	out := b.String()
	if !strings.Contains(out, "| `a` | **BC-A9** | — | — | — none found |") {
		t.Errorf("empty statement/class not rendered as em dashes:\n%s", out)
	}
}
