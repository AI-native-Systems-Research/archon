package review

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/invariant"
)

// funcSite builds a func-scoped citation: a citation inside a function spanning
// [start,end]. Line is put at start, which is inside the span.
func funcSite(file, name string, start, end int) invariant.CitationSite {
	return invariant.CitationSite{File: file, Line: start, Func: name, Start: start, End: end, Scope: "func"}
}

// fixtureRegistry is a linked registry as internal/invariant produces one, now
// carrying citation sites: INV-6 cited in four functions across three files plus
// a named test, INV-2 in a test only, INV-PD-2 declared and cited nowhere.
func fixtureRegistry() *invariant.Result {
	inv := func(id string) invariant.Invariant {
		return invariant.Invariant{ID: id, Scope: "docs/invariants.md", Title: id, Tier: invariant.TierH3, Source: "docs/invariants.md:1"}
	}
	return &invariant.Result{
		Registry:     "docs/invariants.md",
		Commit:       "d77764f5",
		FilesScanned: 318,
		Links: []invariant.Link{
			{Invariant: inv("INV-6"),
				CodeFiles:  []string{"sim/a.go", "sim/b.go", "sim/c.go"},
				TestFiles:  []string{"sim/a_test.go"},
				NamedTests: []string{"sim/a_test.go:TestINV6_Determinism", "sim/z_test.go:TestINV6_Other"},
				Citations:  5,
				CitationSites: []invariant.CitationSite{
					funcSite("sim/a.go", "Foo", 10, 20),
					funcSite("sim/a.go", "Bar", 30, 40),
					funcSite("sim/b.go", "Baz", 5, 15),
					funcSite("sim/c.go", "Qux", 1, 10),
				}},
			{Invariant: inv("INV-2"),
				TestFiles:     []string{"sim/lifecycle_test.go"},
				Citations:     1,
				CitationSites: []invariant.CitationSite{funcSite("sim/lifecycle_test.go", "TestThing", 3, 9)}},
			{Invariant: inv("INV-PD-2")},
		},
	}
}

// changed / changedRng model one PR: it edits a line inside INV-6's Foo (sim/a.go)
// and inside Baz (sim/b.go) — two of INV-6's four citing functions — and touches
// the test file holding one of its two named tests. It does NOT touch Bar (the
// sim/a.go hunk is far from Bar's span), Qux, or anything of INV-2's. File-level
// matching would have counted all of sim/a.go, and so Bar; function-level does not.
var changed = []string{"sim/a.go", "sim/b.go", "sim/a_test.go", "unrelated/x.go"}
var changedRng = map[string][]LineRange{
	"sim/a.go":       {{Start: 12, End: 12}}, // inside Foo [10,20], nowhere near Bar [30,40]
	"sim/b.go":       {{Start: 8, End: 9}},   // inside Baz [5,15]
	"sim/a_test.go":  {{Start: 1, End: 1}},   // named-test column only; no citing func here
	"unrelated/x.go": {{Start: 1, End: 1}},
}

// TestRegistrySection_MatchesByFunction is the heart of #77: an invariant is
// counted for the citing functions a changed line actually landed in, not for
// every function in a touched file. INV-6 is cited in four functions; the change
// reached two (Foo, Baz) and left Bar untouched even though it edited Bar's file.
func TestRegistrySection_MatchesByFunction(t *testing.T) {
	sec := buildRegistrySection(fixtureRegistry(), changed, changedRng, nil)
	if sec == nil {
		t.Fatal("no section")
	}
	if sec.Declared() != 3 || sec.Anchored() != 2 {
		t.Errorf("declared/anchored = %d/%d, want 3/2", sec.Declared(), sec.Anchored())
	}
	byID := map[string]RegistryRow{}
	for _, r := range sec.Rows {
		byID[r.ID] = r
	}
	got := byID["INV-6"]
	if got.CitingFunctionsTouched != 2 || got.CitingFunctionsTotal != 4 {
		t.Errorf("INV-6 = %d of %d citing functions, want 2 of 4 (Foo+Baz hit, Bar+Qux not)",
			got.CitingFunctionsTouched, got.CitingFunctionsTotal)
	}
	if got.NamedTestsInTouchedFiles != 1 || got.NamedTestsTotal != 2 {
		t.Errorf("INV-6 named tests = %d of %d, want 1 of 2", got.NamedTestsInTouchedFiles, got.NamedTestsTotal)
	}
	if _, ok := byID["INV-PD-2"]; ok {
		t.Error("standing UNLINKED INV-PD-2 should not be a pr-review row")
	}
	if _, ok := byID["INV-2"]; ok {
		t.Error("INV-2's functions were not touched; it should not be a row")
	}
}

// TestRegistrySection_SingleLineInsideFunctionReports is the acceptance case the
// issue says file granularity gets backwards: a one-line change inside a function
// whose doc comment cites INV-6 must report INV-6 at full strength.
func TestRegistrySection_SingleLineInsideFunctionReports(t *testing.T) {
	reg := &invariant.Result{
		Registry: "docs/invariants.md", FilesScanned: 10,
		Links: []invariant.Link{{
			Invariant:     invariant.Invariant{ID: "INV-6", Scope: "docs/invariants.md"},
			CodeFiles:     []string{"sim/offload.go"},
			CitationSites: []invariant.CitationSite{funcSite("sim/offload.go", "Reload", 100, 140)},
		}},
	}
	// One line changed, line 118, inside Reload [100,140].
	sec := buildRegistrySection(reg, []string{"sim/offload.go"},
		map[string][]LineRange{"sim/offload.go": {{Start: 118, End: 118}}}, nil)
	if len(sec.Rows) != 1 || sec.Rows[0].ID != "INV-6" {
		t.Fatalf("a single-line change inside a citing function must report it; got %+v", sec.Rows)
	}
	if r := sec.Rows[0]; r.CitingFunctionsTouched != 1 || r.CitingFunctionsTotal != 1 {
		t.Errorf("INV-6 = %d of %d, want 1 of 1", r.CitingFunctionsTouched, r.CitingFunctionsTotal)
	}
}

// TestRegistrySection_UsesNewSideRanges pins that overlap is judged against the
// ranges handed in — which the caller must build from the diff's new (head) side,
// because citations are located in the head tree. A change lands at head line 50,
// inside a function at [40,60]. The new-side range hits it; the base-side range
// (where those lines were something else) does not. Matching head citations
// against base-side numbers is the subtle way to miss a real hit.
func TestRegistrySection_UsesNewSideRanges(t *testing.T) {
	reg := &invariant.Result{
		Registry: "docs/invariants.md", FilesScanned: 10,
		Links: []invariant.Link{{
			Invariant:     invariant.Invariant{ID: "INV-6", Scope: "docs/invariants.md"},
			CodeFiles:     []string{"sim/x.go"},
			CitationSites: []invariant.CitationSite{funcSite("sim/x.go", "F", 40, 60)},
		}},
	}
	newSide := map[string][]LineRange{"sim/x.go": {{Start: 50, End: 50}}}
	if sec := buildRegistrySection(reg, []string{"sim/x.go"}, newSide, nil); len(sec.Rows) != 1 {
		t.Fatalf("new-side range [50,50] is inside F [40,60]; INV-6 must be reported, got %+v", sec.Rows)
	}
	baseSide := map[string][]LineRange{"sim/x.go": {{Start: 10, End: 12}}}
	if sec := buildRegistrySection(reg, []string{"sim/x.go"}, baseSide, nil); len(sec.Rows) != 0 {
		t.Fatalf("base-side range [10,12] is outside F [40,60]; using it would wrongly report INV-6: %+v", sec.Rows)
	}
}

// TestRegistrySection_FallbackScopes covers the two non-function scopes. A
// decl-block citation is matched by range overlap just like a function. A
// whole-file citation — the fallback for a header comment or an unparseable file —
// is matched by the file being changed at all, since there is no function to land
// in; and if the file is untouched it does not count.
func TestRegistrySection_FallbackScopes(t *testing.T) {
	declSite := invariant.CitationSite{File: "sim/d.go", Line: 3, Start: 3, End: 8, Scope: "decl"}
	fileSite := invariant.CitationSite{File: "sim/hdr.go", Line: 1, Start: 1, End: 50, Scope: "file"}
	reg := &invariant.Result{
		Registry: "docs/invariants.md", FilesScanned: 10,
		Links: []invariant.Link{
			{Invariant: invariant.Invariant{ID: "INV-DECL", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/d.go"}, CitationSites: []invariant.CitationSite{declSite}},
			{Invariant: invariant.Invariant{ID: "INV-FILE", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/hdr.go"}, CitationSites: []invariant.CitationSite{fileSite}},
			{Invariant: invariant.Invariant{ID: "INV-UNTOUCHED", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/other.go"},
				CitationSites: []invariant.CitationSite{
					{File: "sim/other.go", Line: 1, Start: 1, End: 20, Scope: "file"}}},
		},
	}
	sec := buildRegistrySection(reg,
		[]string{"sim/d.go", "sim/hdr.go"},
		map[string][]LineRange{"sim/d.go": {{Start: 5, End: 5}}}, // overlaps decl [3,8]; hdr has no range
		nil)
	byID := map[string]RegistryRow{}
	for _, r := range sec.Rows {
		byID[r.ID] = r
	}
	if r, ok := byID["INV-DECL"]; !ok || r.CitingFunctionsTouched != 1 {
		t.Errorf("a decl-block citation overlapped by a range must count; got %+v (ok=%v)", r, ok)
	}
	if r, ok := byID["INV-FILE"]; !ok || r.CitingFunctionsTouched != 1 {
		t.Errorf("a whole-file citation in a changed file must count; got %+v (ok=%v)", r, ok)
	}
	if _, ok := byID["INV-UNTOUCHED"]; ok {
		t.Error("a whole-file citation in an unchanged file must not count")
	}
}

// TestRegistrySection_ScopeDedup pins the denominator semantics the golden freezes:
// two citations of the same ID in one function are one scope, not two.
func TestRegistrySection_ScopeDedup(t *testing.T) {
	reg := &invariant.Result{
		Registry: "docs/invariants.md", FilesScanned: 10,
		Links: []invariant.Link{{
			Invariant: invariant.Invariant{ID: "INV-6", Scope: "docs/invariants.md"},
			CodeFiles: []string{"sim/a.go"},
			CitationSites: []invariant.CitationSite{
				funcSite("sim/a.go", "Foo", 10, 20),
				funcSite("sim/a.go", "Foo", 10, 20), // same span: a second citation in Foo
			}},
		},
	}
	sec := buildRegistrySection(reg, []string{"sim/a.go"},
		map[string][]LineRange{"sim/a.go": {{Start: 12, End: 12}}}, nil)
	if len(sec.Rows) != 1 || sec.Rows[0].CitingFunctionsTotal != 1 || sec.Rows[0].CitingFunctionsTouched != 1 {
		t.Errorf("two citations in one function are one scope; got %+v", sec.Rows)
	}
}

// TestRegistrySection_RowsOrderedByProportion puts the highest touched/citing ratio
// at the top, not the highest raw count. INV-9 cites one function and the change
// touched it (1 of 1 = 100%); INV-6 is 2 of 4 = 50%.
func TestRegistrySection_RowsOrderedByProportion(t *testing.T) {
	reg := fixtureRegistry()
	reg.Links = append(reg.Links, invariant.Link{
		Invariant:     invariant.Invariant{ID: "INV-9", Scope: "docs/invariants.md"},
		CodeFiles:     []string{"unrelated/x.go"},
		CitationSites: []invariant.CitationSite{funcSite("unrelated/x.go", "G", 1, 3)},
	})
	rng := map[string][]LineRange{}
	for k, v := range changedRng {
		rng[k] = v
	}
	rng["unrelated/x.go"] = []LineRange{{Start: 2, End: 2}} // inside G [1,3]
	sec := buildRegistrySection(reg, changed, rng, nil)
	if len(sec.Rows) < 2 || sec.Rows[0].ID != "INV-9" || sec.Rows[1].ID != "INV-6" {
		var ids []string
		for _, r := range sec.Rows {
			ids = append(ids, r.ID)
		}
		t.Errorf("rows = %v, want [INV-9 INV-6] (100%% before 50%%)", ids)
	}
}

// TestRegistrySection_ZeroAnchoredIsAFinding: a repo that has written a registry
// and cited none of it must render as a stated result, not an empty section.
func TestRegistrySection_ZeroAnchoredIsAFinding(t *testing.T) {
	reg := fixtureRegistry()
	for i := range reg.Links {
		reg.Links[i].CodeFiles, reg.Links[i].TestFiles = nil, nil
		reg.Links[i].NamedTests, reg.Links[i].Citations, reg.Links[i].CitationSites = nil, 0, nil
	}
	sec := buildRegistrySection(reg, changed, changedRng, nil)
	if sec.Anchored() != 0 || sec.Declared() != 3 {
		t.Fatalf("anchored/declared = %d/%d, want 0/3", sec.Anchored(), sec.Declared())
	}
	var b strings.Builder
	writeRegistrySection(&b, sec)
	out := b.String()
	if !strings.Contains(out, "0 of 3 anchored") {
		t.Errorf("want a \"0 of 3 anchored\" finding:\n%s", out)
	}
	if len(sec.Rows) != 0 {
		t.Errorf("standing UNLINKED rows should not render; got %d rows", len(sec.Rows))
	}
}

// TestRegistrySection_NamesThePath: printing the path turns a silently disabled
// section (repo renamed the doc) into something a reader sees.
func TestRegistrySection_NamesThePath(t *testing.T) {
	var b strings.Builder
	writeRegistrySection(&b, buildRegistrySection(fixtureRegistry(), changed, changedRng, nil))
	out := b.String()
	if !strings.Contains(out, "`docs/invariants.md`") {
		t.Errorf("the section must name the registry path it used:\n%s", out)
	}
	if !strings.Contains(out, "d77764f5") {
		t.Errorf("the section should name the commit it read:\n%s", out)
	}
}

func TestRegistrySection_NilIsNoSection(t *testing.T) {
	if sec := buildRegistrySection(nil, changed, changedRng, nil); sec != nil {
		t.Errorf("no registry means no section, got %+v", sec)
	}
	var b strings.Builder
	writeRegistrySection(&b, nil)
	if b.String() != "" {
		t.Errorf("no section should write nothing, got %q", b.String())
	}
}

// TestRegistryIsAdvisory is the constraint the issue makes explicit: this must
// not move Verdict, dist, or anything else already in the report. It builds twice
// from identical inputs and compares everything except the new registry field.
func TestRegistryIsAdvisory(t *testing.T) {
	a := baseGraph()
	b := baseGraph()
	b.Packages = append(b.Packages, pkg("m/newbox", true))
	b.Sort()
	d := delta.Compute(a, b)

	planGraph := baseGraph()
	opts := Options{LabelA: "base", LabelB: "head", PlanGraph: planGraph}
	plain := Build(a, b, d, opts)

	withOpts := opts
	withOpts.Registry, withOpts.ChangedFiles, withOpts.ChangedRanges = fixtureRegistry(), changed, changedRng
	withReg := Build(a, b, d, withOpts)

	if withReg.Registry == nil {
		t.Fatal("the registry section did not render, so this test would prove nothing")
	}
	if plain.Registry != nil {
		t.Error("no registry was supplied, so the field must stay nil")
	}
	if plain.Verdict != withReg.Verdict || plain.Summary != withReg.Summary {
		t.Errorf("verdict moved: %q/%q vs %q/%q", plain.Verdict, plain.Summary, withReg.Verdict, withReg.Summary)
	}
	if plain.Counts != withReg.Counts {
		t.Errorf("counts moved: %+v vs %+v", plain.Counts, withReg.Counts)
	}
	if plain.PlanRatchet == nil || withReg.PlanRatchet == nil {
		t.Fatal("no plan ratchet computed, so dist is not actually being compared")
	}
	if fmt.Sprint(*plain.PlanRatchet) != fmt.Sprint(*withReg.PlanRatchet) {
		t.Errorf("dist moved: %+v vs %+v", *plain.PlanRatchet, *withReg.PlanRatchet)
	}
	if fmt.Sprint(plain.PlanClassify) != fmt.Sprint(withReg.PlanClassify) {
		t.Errorf("plan classification moved:\n%+v\n%+v", plain.PlanClassify, withReg.PlanClassify)
	}
	if fmt.Sprint(plain.Clauses) != fmt.Sprint(withReg.Clauses) {
		t.Errorf("clauses moved:\n%+v\n%+v", plain.Clauses, withReg.Clauses)
	}

	var sectionOnly strings.Builder
	writeRegistrySection(&sectionOnly, withReg.Registry)
	withMD, plainMD := renderMarkdown(withReg), renderMarkdown(plain)
	if !strings.Contains(withMD, sectionOnly.String()) {
		t.Fatalf("review.md does not contain the rendered section verbatim:\n%s", withMD)
	}
	prose := func(md string) string {
		if i := strings.Index(md, "<details"); i >= 0 {
			return md[:i]
		}
		return md
	}
	if stripped := prose(strings.Replace(withMD, sectionOnly.String(), "", 1)); stripped != prose(plainMD) {
		t.Errorf("review.md prose changed outside the registry section:\n--- want ---\n%s\n--- got ---\n%s", prose(plainMD), stripped)
	}

	plainJSON, withJSON := mustJSONKeys(t, plain), mustJSONKeys(t, withReg)
	if _, ok := withJSON["registry"]; !ok {
		t.Error("review.json has no registry key, so this comparison proves nothing")
	}
	delete(withJSON, "registry")
	for k := range plainJSON {
		if _, ok := withJSON[k]; !ok {
			t.Errorf("review.json lost key %q", k)
		}
	}
	for k := range withJSON {
		if _, ok := plainJSON[k]; !ok {
			t.Errorf("review.json gained key %q besides \"registry\"", k)
		}
	}
}

func mustJSONKeys(t *testing.T, res *Result) map[string]any {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRegistrySection_JSONShape(t *testing.T) {
	sec := buildRegistrySection(fixtureRegistry(), changed, changedRng, nil)
	b, err := json.Marshal(sec)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		SchemaVersion int    `json:"schemaVersion"`
		Registry      string `json:"registry"`
		Commit        string `json:"commit"`
		Declared      int    `json:"declared"`
		Anchored      int    `json:"anchored"`
		Linked        int    `json:"linked"`
		TestOnly      int    `json:"testOnly"`
		Unlinked      int    `json:"unlinked"`
		FilesScanned  int    `json:"filesScanned"`
		Rows          []struct {
			ID                     string `json:"id"`
			Status                 string `json:"status"`
			CitingFunctionsTouched int    `json:"citingFunctionsTouched"`
			CitingFunctionsTotal   int    `json:"citingFunctionsTotal"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != registrySchemaVersion || registrySchemaVersion != 3 {
		t.Errorf("schemaVersion = %d, want %d (=3 for #77)", got.SchemaVersion, registrySchemaVersion)
	}
	if got.Registry != "docs/invariants.md" || got.Declared != 3 || got.Anchored != 2 || got.FilesScanned != 318 {
		t.Errorf("header fields wrong: %+v", got)
	}
	if got.Anchored != got.Linked+got.TestOnly || got.Declared != got.Linked+got.TestOnly+got.Unlinked {
		t.Errorf("totals are inconsistent: %+v", got)
	}
	if len(got.Rows) == 0 || got.Rows[0].ID != "INV-6" || got.Rows[0].CitingFunctionsTouched != 2 || got.Rows[0].CitingFunctionsTotal != 4 {
		t.Errorf("rows wrong: %+v", got.Rows)
	}
}

// sectionFixture adds a row with deleted anchors to the base fixture, so one
// rendering covers the row kinds that render: touched, and
// untouched-but-anchor-deleted.
func sectionFixture() (*invariant.Result, map[string][]LineRange, map[string][]string) {
	reg := fixtureRegistry()
	reg.Links = append(reg.Links, invariant.Link{
		Invariant:     invariant.Invariant{ID: "INV-13", Scope: "docs/invariants.md"},
		CodeFiles:     []string{"sim/keep.go"},
		Citations:     1,
		CitationSites: []invariant.CitationSite{funcSite("sim/keep.go", "Keep", 3, 8)},
	})
	return reg, changedRng, map[string][]string{"INV-13": {"sim/gone_a.go", "sim/gone_b.go"}}
}

// wantSection is the rendered section, byte for byte. Every number in it is
// derivable from sectionFixture by hand. This exists because the rendered markdown
// otherwise rests on demo/flow1-pr-review's golden, which CI never runs.
const wantSection = "### Declared invariants — registry\n" + `
Registry: ` + "`docs/invariants.md`" + ` at ` + "`d77764f5`" + ` — 4 declared, 3 of 4 anchored (2 LINKED, 1 TEST ONLY, 1 UNLINKED), 318 Go files scanned.

| ID | Status | citing functions touched | named tests in touched files |
|---|---|---|---|
| ` + "`INV-13`" + ` | LINKED | 0 of 1 | 0 of 0 |
| ` + "`INV-6`" + ` | LINKED | 2 of 4 | 1 of 2 |

**This change deletes files that cited ` + "`INV-13`" + ` (2).** Those files are gone at head, so they count in no column above.

`

func TestRender_SectionIsExact(t *testing.T) {
	reg, rng, removed := sectionFixture()
	var b strings.Builder
	writeRegistrySection(&b, buildRegistrySection(reg, changed, rng, removed))
	if got := b.String(); got != wantSection {
		t.Errorf("section changed:\n--- got ---\n%s\n--- want ---\n%s", got, wantSection)
	}
}

// TestRender_NoFooter pins that #74's threshold footer is gone: every touched row
// renders in the table, and nothing counts hidden rows below it.
func TestRender_NoFooter(t *testing.T) {
	reg, rng, removed := sectionFixture()
	var b strings.Builder
	writeRegistrySection(&b, buildRegistrySection(reg, changed, rng, removed))
	if out := b.String(); strings.Contains(out, "did not clear the reporting threshold") {
		t.Errorf("the threshold footer must be gone (#77):\n%s", out)
	}
}

// TestRender_DeletedAnchorOutranksTouching: removing a citation site is the change
// most likely to leave a declared promise unguarded, so it sorts above any amount
// of touching even though its touched count is zero.
func TestRender_DeletedAnchorOutranksTouching(t *testing.T) {
	reg, rng, removed := sectionFixture()
	sec := buildRegistrySection(reg, changed, rng, removed)
	if len(sec.Rows) == 0 || sec.Rows[0].ID != "INV-13" {
		var ids []string
		for _, r := range sec.Rows {
			ids = append(ids, r.ID)
		}
		t.Errorf("rows = %v, want the deleted-anchor row first", ids)
	}
	if sec.Rows[0].AnchorsRemoved != 2 {
		t.Errorf("AnchorsRemoved = %d, want 2 (two files, one ID)", sec.Rows[0].AnchorsRemoved)
	}
}

// TestRender_NothingTouchedSaysSo: a registry whose invariants are all anchored and
// none touched must say that, not render a bare header.
func TestRender_NothingTouchedSaysSo(t *testing.T) {
	reg := fixtureRegistry()
	reg.Links = reg.Links[:2] // drop the uncited one, so there are no rows at all
	var b strings.Builder
	writeRegistrySection(&b, buildRegistrySection(reg, nil, nil, nil))
	out := b.String()
	if !strings.Contains(out, "This change touched no function citing a declared invariant.") {
		t.Errorf("want an explicit no-rows statement:\n%s", out)
	}
	if strings.Contains(out, "| ID | Status") {
		t.Errorf("no rows, so no table:\n%s", out)
	}
}

// TestRender_SectionRendersOnNoChange is the placement guarantee. The section is
// most useful on a NO_CHANGE review, but renderMarkdown returns early for
// NO_CHANGE, so moving the call below that return silently drops the section.
func TestRender_SectionRendersOnNoChange(t *testing.T) {
	g := baseGraph()
	d := delta.Compute(g, g)
	reg, rng, removed := sectionFixture()
	res := Build(g, g, d, Options{LabelA: "base", LabelB: "head",
		Registry: reg, ChangedFiles: changed, ChangedRanges: rng, RemovedAnchors: removed})
	if res.Verdict != NoChange {
		t.Fatalf("fixture should be NO_CHANGE, got %s", res.Verdict)
	}
	md := renderMarkdown(res)
	if !strings.Contains(md, wantSection) {
		t.Errorf("the section is missing from a NO_CHANGE review:\n%s", md)
	}
}

// TestRemovedLastAnchorRenders is the erosion exception: standing UNLINKED does not
// render, but an invariant whose *last* citation this change removed is an event
// about the change and must be reported loudly.
func TestRemovedLastAnchorRenders(t *testing.T) {
	reg := &invariant.Result{
		Registry: "docs/invariants.md", FilesScanned: 10,
		Links: []invariant.Link{
			{Invariant: invariant.Invariant{ID: "INV-ERODED", Scope: "docs/invariants.md"}},
		},
	}
	sec := buildRegistrySection(reg, nil, nil, map[string][]string{"INV-ERODED": {"sim/gone.go"}})
	if len(sec.Rows) != 1 || sec.Rows[0].ID != "INV-ERODED" {
		t.Fatalf("a removed-last-anchor invariant must render; got %+v", sec.Rows)
	}
	var b strings.Builder
	writeRegistrySection(&b, sec)
	if out := b.String(); !strings.Contains(out, "deletes files that cited `INV-ERODED`") {
		t.Errorf("want the deleted-anchor line reported loudly:\n%s", out)
	}
}

// TestTouchedNamedTestIsShown: a test named for an invariant, in a file the change
// edited, is the exact event the "named tests in touched files" column exists for.
// It carries no citing-function overlap, so a row shows on the named test alone.
func TestTouchedNamedTestIsShown(t *testing.T) {
	reg := &invariant.Result{
		Registry: "docs/invariants.md", FilesScanned: 10,
		Links: []invariant.Link{
			// Cited across functions the change did not reach, but it touched a file
			// holding a test named for the invariant. Touched functions == 0.
			{Invariant: invariant.Invariant{ID: "INV-GUARD", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/a.go"},
				CitationSites: []invariant.CitationSite{
					funcSite("sim/a.go", "Foo", 10, 20)},
				NamedTests: []string{"sim/guard_test.go:TestINVGUARD_Holds"}},
		},
	}
	sec := buildRegistrySection(reg, []string{"sim/guard_test.go"}, nil, nil)
	if len(sec.Rows) != 1 {
		t.Fatalf("the touched named test must produce a row; got %d", len(sec.Rows))
	}
	if r := sec.Rows[0]; r.NamedTestsInTouchedFiles != 1 || r.CitingFunctionsTouched != 0 {
		t.Errorf("a touched named test must show regardless of function overlap; got %+v", r)
	}
}

// TestNamedOnlyRowSortsBelowProportionRows: a row shown only because a named test
// was touched has no function-overlap proportion (touched==0), so it sorts below
// every row a changed line actually reached.
func TestNamedOnlyRowSortsBelowProportionRows(t *testing.T) {
	reg := &invariant.Result{
		Registry: "docs/invariants.md", FilesScanned: 50,
		Links: []invariant.Link{
			// Proportion-shown: a changed line landed in one of its functions.
			{Invariant: invariant.Invariant{ID: "INV-PROP", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/p.go"},
				CitationSites: []invariant.CitationSite{
					funcSite("sim/p.go", "P0", 10, 20), funcSite("sim/p.go", "P1", 30, 40)}},
			// Named-only shown: no function reached, but a named test's file changed.
			{Invariant: invariant.Invariant{ID: "INV-NAMED", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/n.go"},
				CitationSites: []invariant.CitationSite{
					funcSite("sim/n.go", "N0", 10, 20)},
				NamedTests: []string{"sim/named_test.go:TestINVNAMED_Holds"}},
		},
	}
	changedFiles := []string{"sim/p.go", "sim/named_test.go"}
	rng := map[string][]LineRange{"sim/p.go": {{Start: 15, End: 15}}} // inside P0
	sec := buildRegistrySection(reg, changedFiles, rng, nil)
	var ids []string
	for _, r := range sec.Rows {
		ids = append(ids, r.ID)
	}
	if len(ids) != 2 || ids[0] != "INV-PROP" || ids[1] != "INV-NAMED" {
		t.Errorf("named-only row must sort below the proportion row: want [INV-PROP INV-NAMED], got %v", ids)
	}
}

// TestTieBreakOnTouchedCount: two rows at equal proportion break the tie on the raw
// touched count, then ID.
func TestTieBreakOnTouchedCount(t *testing.T) {
	mk := func(id string, funcs, touched int) (invariant.Link, map[string][]LineRange) {
		var sites []invariant.CitationSite
		rng := map[string][]LineRange{}
		file := "sim/" + id + ".go"
		for i := 0; i < funcs; i++ {
			start := 10 + i*10
			sites = append(sites, funcSite(file, fmt.Sprintf("F%d", i), start, start+5))
			if i < touched {
				rng[file] = append(rng[file], LineRange{Start: start + 1, End: start + 1})
			}
		}
		return invariant.Link{Invariant: invariant.Invariant{ID: id, Scope: "docs/invariants.md"},
			CodeFiles: []string{file}, CitationSites: sites}, rng
	}
	// INV-A 4/20 and INV-Z 2/10 are both 20%; the larger touch (INV-A) must lead.
	// IDs chosen so alphabetical order contradicts the touched-count order.
	la, ra := mk("INV-A", 20, 4)
	lz, rz := mk("INV-Z", 10, 2)
	reg := &invariant.Result{Registry: "docs/invariants.md", FilesScanned: 50,
		Links: []invariant.Link{lz, la}}
	rng := map[string][]LineRange{}
	for k, v := range ra {
		rng[k] = v
	}
	for k, v := range rz {
		rng[k] = v
	}
	var files []string
	for k := range rng {
		files = append(files, k)
	}
	sec := buildRegistrySection(reg, files, rng, nil)
	if len(sec.Rows) != 2 || sec.Rows[0].ID != "INV-A" || sec.Rows[1].ID != "INV-Z" {
		var ids []string
		for _, r := range sec.Rows {
			ids = append(ids, r.ID)
		}
		t.Errorf("equal proportion (20%%) must break on touched count (INV-A=4 before INV-Z=2): got %v", ids)
	}
}
