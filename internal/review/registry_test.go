package review

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/invariant"
)

// fixtureRegistry is a linked registry as internal/invariant produces one:
// INV-6 cited widely, INV-2 in tests only, INV-PD-2 declared and cited nowhere.
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
				Citations:  40},
			{Invariant: inv("INV-2"),
				TestFiles: []string{"sim/lifecycle_test.go"},
				Citations: 1},
			{Invariant: inv("INV-PD-2")},
		},
	}
}

// changed is the PR's changed-file list: it touches three of INV-6's four files
// (two production, one test) and so one of its two named tests, and nothing of
// INV-2's.
var changed = []string{"sim/a.go", "sim/b.go", "sim/a_test.go", "unrelated/x.go"}

func TestRegistrySection_CountsTouchedFilesAndNamedTests(t *testing.T) {
	sec := buildRegistrySection(fixtureRegistry(), changed, nil)
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
	if got := byID["INV-6"]; got.CitingFilesTouched != 3 || got.CitingFilesTotal != 4 || got.NamedTestsInTouchedFiles != 1 || got.NamedTestsTotal != 2 {
		t.Errorf("INV-6 = %+v, want 3/4 citing files and 1/2 named", got)
	}
	// Declared, cited nowhere: a finding, so it appears even though the change
	// cannot have touched it.
	if got, ok := byID["INV-PD-2"]; !ok || got.Status != invariant.StatusUnlinked {
		t.Errorf("INV-PD-2 should be reported as UNLINKED, got %+v", got)
	}
	// Untouched and anchored: nothing for a reviewer to act on.
	if _, ok := byID["INV-2"]; ok {
		t.Error("INV-2 is untouched and anchored; it should not be a row")
	}
}

// TestRegistrySection_RowsOrderedByExposure puts what the change touched most at
// the top, so the first row is the one worth reading.
func TestRegistrySection_RowsOrderedByExposure(t *testing.T) {
	reg := fixtureRegistry()
	reg.Links = append(reg.Links, invariant.Link{
		Invariant: invariant.Invariant{ID: "INV-9", Scope: "docs/invariants.md"},
		CodeFiles: []string{"unrelated/x.go"},
	})
	sec := buildRegistrySection(reg, changed, nil)
	if len(sec.Rows) < 2 || sec.Rows[0].ID != "INV-6" {
		var ids []string
		for _, r := range sec.Rows {
			ids = append(ids, r.ID)
		}
		t.Errorf("rows = %v, want INV-6 first", ids)
	}
}

// TestRegistrySection_ZeroAnchoredIsAFinding: a repo that has written a registry
// and cited none of it is the case this feature exists for, so it must render as
// a stated result rather than an empty section.
func TestRegistrySection_ZeroAnchoredIsAFinding(t *testing.T) {
	reg := fixtureRegistry()
	for i := range reg.Links {
		reg.Links[i].CodeFiles, reg.Links[i].TestFiles, reg.Links[i].NamedTests, reg.Links[i].Citations = nil, nil, nil, 0
	}
	sec := buildRegistrySection(reg, changed, nil)
	if sec.Anchored() != 0 || sec.Declared() != 3 {
		t.Fatalf("anchored/declared = %d/%d, want 0/3", sec.Anchored(), sec.Declared())
	}

	var b strings.Builder
	writeRegistrySection(&b, sec)
	out := b.String()
	if !strings.Contains(out, "0 of 3 anchored") {
		t.Errorf("want a \"0 of 3 anchored\" finding:\n%s", out)
	}
	if len(sec.Rows) != 3 {
		t.Errorf("all three are unlinked, so all three are rows; got %d", len(sec.Rows))
	}
}

// TestRegistrySection_NamesThePath: auto-discovery's one bad failure mode is the
// repo renaming the doc, the section vanishing, and every later review looking
// normal. Printing the path turns a silent disable into something a reader sees.
func TestRegistrySection_NamesThePath(t *testing.T) {
	var b strings.Builder
	writeRegistrySection(&b, buildRegistrySection(fixtureRegistry(), changed, nil))
	out := b.String()
	if !strings.Contains(out, "`docs/invariants.md`") {
		t.Errorf("the section must name the registry path it used:\n%s", out)
	}
	if !strings.Contains(out, "d77764f5") {
		t.Errorf("the section should name the commit it read:\n%s", out)
	}
}

func TestRegistrySection_NilIsNoSection(t *testing.T) {
	if sec := buildRegistrySection(nil, changed, nil); sec != nil {
		t.Errorf("no registry means no section, got %+v", sec)
	}
	var b strings.Builder
	writeRegistrySection(&b, nil)
	if b.String() != "" {
		t.Errorf("no section should write nothing, got %q", b.String())
	}
}

// TestRegistryIsAdvisory is the constraint the issue makes explicit: this must
// not move Verdict, dist, or anything else already in the report.
//
// It builds twice from identical inputs and compares everything except the new
// field — including review.md with the new section removed — so it cannot pass by
// the section simply being absent.
func TestRegistryIsAdvisory(t *testing.T) {
	a := baseGraph()
	b := baseGraph()
	b.Packages = append(b.Packages, pkg("m/newbox", true))
	b.Sort()
	d := delta.Compute(a, b)

	// A plan is supplied so PlanRatchet and PlanClassify — which carry dist — are
	// actually computed. Without one they are nil in both builds and comparing
	// them proves nothing, which is how this test used to claim more than it
	// checked.
	planGraph := baseGraph()
	opts := Options{LabelA: "base", LabelB: "head", PlanGraph: planGraph}
	plain := Build(a, b, d, opts)

	withOpts := opts
	withOpts.Registry, withOpts.ChangedFiles = fixtureRegistry(), changed
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
	// dist rides in the plan ratchet, which the issue names explicitly.
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

	// review.md must differ by exactly the section text and nothing else. Deleting
	// the standalone rendering is a stricter check than stripping a heuristic
	// range: it also proves the section appears in review.md verbatim.
	var sectionOnly strings.Builder
	writeRegistrySection(&sectionOnly, withReg.Registry)
	withMD, plainMD := renderMarkdown(withReg), renderMarkdown(plain)
	if !strings.Contains(withMD, sectionOnly.String()) {
		t.Fatalf("review.md does not contain the rendered section verbatim:\n%s", withMD)
	}
	// review.md embeds review.json in a collapsible block, so that half differs by
	// the new key as well. Compare the prose, then the JSON keys separately.
	prose := func(md string) string {
		if i := strings.Index(md, "<details"); i >= 0 {
			return md[:i]
		}
		return md
	}
	if stripped := prose(strings.Replace(withMD, sectionOnly.String(), "", 1)); stripped != prose(plainMD) {
		t.Errorf("review.md prose changed outside the registry section:\n--- want ---\n%s\n--- got ---\n%s", prose(plainMD), stripped)
	}

	// review.json likewise: the only new key is "registry". Compared as sets, not
	// by count — equal counts would survive a rename.
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
	sec := buildRegistrySection(fixtureRegistry(), changed, nil)
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
			ID                 string `json:"id"`
			Status             string `json:"status"`
			CitingFilesTouched int    `json:"citingFilesTouched"`
			CitingFilesTotal   int    `json:"citingFilesTotal"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	// This shape's own version, not invariant.Result's: bumping that one must not
	// silently bump this.
	if got.SchemaVersion != registrySchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", got.SchemaVersion, registrySchemaVersion)
	}
	if got.Registry != "docs/invariants.md" || got.Declared != 3 || got.Anchored != 2 || got.FilesScanned != 318 {
		t.Errorf("header fields wrong: %+v", got)
	}
	// Derived, so they cannot disagree with the registry they came from.
	if got.Anchored != got.Linked+got.TestOnly || got.Declared != got.Linked+got.TestOnly+got.Unlinked {
		t.Errorf("totals are inconsistent: %+v", got)
	}
	if len(got.Rows) == 0 || got.Rows[0].ID != "INV-6" || got.Rows[0].CitingFilesTouched != 3 || got.Rows[0].CitingFilesTotal != 4 {
		t.Errorf("rows wrong: %+v", got.Rows)
	}
}

// sectionFixture adds a row with deleted anchors to the base fixture, so one
// rendering covers all four row kinds: touched, untouched-but-anchor-deleted,
// excluded, and declared-but-uncited.
func sectionFixture() (*invariant.Result, map[string][]string) {
	reg := fixtureRegistry()
	reg.Links = append(reg.Links, invariant.Link{
		Invariant: invariant.Invariant{ID: "INV-13", Scope: "docs/invariants.md"},
		CodeFiles: []string{"sim/keep.go"},
		Citations: 3,
	})
	// Two files, one ID: a count taken from the number of *IDs* with removals
	// would print (1) here.
	return reg, map[string][]string{"INV-13": {"sim/gone_a.go", "sim/gone_b.go"}}
}

// wantSection is the rendered section, byte for byte. Every number in it is
// derivable from sectionFixture by hand.
//
// This exists because the rendered markdown previously rested entirely on
// demo/flow1-pr-review's golden, which CI never runs (BLIS_REPO is unset there).
// Swapping a numerator with its denominator, dropping the deleted-anchor line, or
// losing the row ordering all passed the suite.
const wantSection = "### Declared invariants — registry\n" + `
Registry: ` + "`docs/invariants.md`" + ` at ` + "`d77764f5`" + ` — 4 declared, 3 of 4 anchored (2 LINKED, 1 TEST ONLY, 1 UNLINKED), 318 Go files scanned.

| ID | Status | citing files touched | named tests in touched files |
|---|---|---|---|
| ` + "`INV-13`" + ` | LINKED | 0 of 1 | 0 of 0 |
| ` + "`INV-6`" + ` | LINKED | 3 of 4 | 1 of 2 |
| ` + "`INV-PD-2`" + ` | UNLINKED | — | — |

**This change deletes files that cited ` + "`INV-13`" + ` (2).** Those files are gone at head, so they count in no column above.

` + "`INV-PD-2`" + ` is declared but cited in no file.

`

func TestRender_SectionIsExact(t *testing.T) {
	reg, removed := sectionFixture()
	var b strings.Builder
	writeRegistrySection(&b, buildRegistrySection(reg, changed, removed))
	if got := b.String(); got != wantSection {
		t.Errorf("section changed:\n--- got ---\n%s\n--- want ---\n%s", got, wantSection)
	}
}

// TestRender_DeletedAnchorOutranksTouching: removing a citation site is the change
// most likely to leave a declared promise unguarded, so it sorts above any amount
// of touching even though its touched count is zero.
func TestRender_DeletedAnchorOutranksTouching(t *testing.T) {
	reg, removed := sectionFixture()
	sec := buildRegistrySection(reg, changed, removed)
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
// none touched must say that, not render a bare header a reader could mistake for a
// truncated section.
func TestRender_NothingTouchedSaysSo(t *testing.T) {
	reg := fixtureRegistry()
	reg.Links = reg.Links[:2] // drop the uncited one, so there are no rows at all
	var b strings.Builder
	writeRegistrySection(&b, buildRegistrySection(reg, nil, nil))
	out := b.String()
	if !strings.Contains(out, "This change touched no file citing a declared invariant.") {
		t.Errorf("want an explicit no-rows statement:\n%s", out)
	}
	if strings.Contains(out, "| ID | Status") {
		t.Errorf("no rows, so no table:\n%s", out)
	}
}

// TestRender_SectionRendersOnNoChange is the placement guarantee. The section is
// most useful on a NO_CHANGE review — that is the majority verdict, and "0 of N
// anchored" is exactly the finding an adopting repo needs there — but
// renderMarkdown returns early for NO_CHANGE, so moving the call below that return
// silently drops the section from most reviews.
//
// TestRegistryIsAdvisory cannot catch that: it compares the prose with the section
// text removed, and string removal is position-independent.
func TestRender_SectionRendersOnNoChange(t *testing.T) {
	g := baseGraph()
	d := delta.Compute(g, g)
	reg, removed := sectionFixture()
	res := Build(g, g, d, Options{LabelA: "base", LabelB: "head",
		Registry: reg, ChangedFiles: changed, RemovedAnchors: removed})
	if res.Verdict != NoChange {
		t.Fatalf("fixture should be NO_CHANGE, got %s", res.Verdict)
	}
	md := renderMarkdown(res)
	if !strings.Contains(md, wantSection) {
		t.Errorf("the section is missing from a NO_CHANGE review:\n%s", md)
	}
}
