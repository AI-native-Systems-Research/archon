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
	// Standing UNLINKED — declared and cited nowhere, true of the repo rather than
	// this change — no longer renders in pr-review. It is the audit surface's job
	// (archon invariants), and repeating it on every PR trains readers to skip the
	// section. Only a citation this change *removed* is an event worth reporting.
	if _, ok := byID["INV-PD-2"]; ok {
		t.Error("standing UNLINKED INV-PD-2 should not be a pr-review row")
	}
	// Untouched and anchored: nothing for a reviewer to act on.
	if _, ok := byID["INV-2"]; ok {
		t.Error("INV-2 is untouched and anchored; it should not be a row")
	}
}

// TestRegistrySection_RowsOrderedByProportion puts the highest touched/citing ratio
// at the top, not the highest raw count. INV-9 cites one file and the change touched
// it (1 of 1 = 100%); INV-6 is 3 of 4 = 75%. The old raw-count sort would rank INV-6
// first on 3 > 1 — the exact inversion #74 fixes.
func TestRegistrySection_RowsOrderedByProportion(t *testing.T) {
	reg := fixtureRegistry()
	reg.Links = append(reg.Links, invariant.Link{
		Invariant: invariant.Invariant{ID: "INV-9", Scope: "docs/invariants.md"},
		CodeFiles: []string{"unrelated/x.go"},
	})
	sec := buildRegistrySection(reg, changed, nil)
	if len(sec.Rows) < 2 || sec.Rows[0].ID != "INV-9" || sec.Rows[1].ID != "INV-6" {
		var ids []string
		for _, r := range sec.Rows {
			ids = append(ids, r.ID)
		}
		t.Errorf("rows = %v, want [INV-9 INV-6] (100%% before 75%%)", ids)
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
	// The header finding stands on its own; standing UNLINKED rows no longer
	// render, so an all-unlinked registry produces no table rows — the "0 of N
	// anchored" line is the whole report.
	if len(sec.Rows) != 0 {
		t.Errorf("standing UNLINKED rows should not render; got %d rows", len(sec.Rows))
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
// rendering covers the row kinds that render: touched, and
// untouched-but-anchor-deleted. The declared-but-uncited INV-PD-2 is in the
// registry but no longer renders as a row — standing UNLINKED is the audit
// surface's job — so it exercises the drop.
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

**This change deletes files that cited ` + "`INV-13`" + ` (2).** Those files are gone at head, so they count in no column above.

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

// blisPR1725Fixture reproduces the exact touched/citing counts archon reports on
// BLIS PR #1725 — the acceptance test case in issue #74. Each invariant cites
// `citing` synthetic files, of which the first `touched` are in the change, so
// buildRegistrySection sees precisely the numbers the issue tabulates.
func blisPR1725Fixture() (*invariant.Result, []string) {
	specs := []struct {
		id              string
		touched, citing int
	}{
		{"INV-6", 7, 147}, {"INV-4", 3, 14}, {"INV-8", 3, 10},
		{"INV-1", 2, 41}, {"INV-11", 1, 10}, {"INV-12", 1, 3},
		{"INV-13", 1, 47}, {"INV-2", 1, 9}, {"INV-3", 1, 28},
		{"INV-7", 1, 8}, {"INV-9", 1, 10},
	}
	var links []invariant.Link
	var changedFiles []string
	for _, s := range specs {
		var files []string
		for i := 0; i < s.citing; i++ {
			f := fmt.Sprintf("sim/%s_f%d.go", s.id, i)
			files = append(files, f)
			if i < s.touched {
				changedFiles = append(changedFiles, f)
			}
		}
		links = append(links, invariant.Link{
			Invariant: invariant.Invariant{ID: s.id, Scope: "docs/invariants.md"},
			CodeFiles: files,
		})
	}
	return &invariant.Result{Registry: "docs/invariants.md", FilesScanned: 431, Links: links}, changedFiles
}

// TestRankByProportion_BLIS1725 is the acceptance check named in #74: on that PR
// INV-4 and INV-8 must be the top rows. Under the old raw-count sort INV-6 led at
// 7 of 147; here the two invariants a reviewer actually wants rise to the top and
// the diffuse tail sinks below the floor.
func TestRankByProportion_BLIS1725(t *testing.T) {
	reg, changedFiles := blisPR1725Fixture()
	sec := buildRegistrySection(reg, changedFiles, nil)

	// The rendered table is exactly the Shown rows, in row order.
	var shown []string
	for _, r := range sec.Rows {
		if r.Shown {
			shown = append(shown, r.ID)
		}
	}
	if len(shown) != 2 || shown[0] != "INV-8" || shown[1] != "INV-4" {
		t.Errorf("shown rows = %v, want [INV-8 INV-4] (proportion desc: 30%%, 21%%)", shown)
	}

	byID := map[string]RegistryRow{}
	for _, r := range sec.Rows {
		byID[r.ID] = r
	}
	// The high-proportion single-file touch the guard exists to stop: 1 of 3 = 33%
	// would top a pure proportion sort, but touched < 2 and touched != citing.
	if byID["INV-12"].Shown {
		t.Error("INV-12 (1 of 3) is a single-file touch; it must not outrank a multi-file touch")
	}
	// Significant (touched >= 2) but too diffuse: 5% and 4.9% are below the 10% floor.
	if byID["INV-6"].Shown || byID["INV-1"].Shown {
		t.Errorf("INV-6 (7 of 147) and INV-1 (2 of 41) are below the floor; got shown=%v/%v",
			byID["INV-6"].Shown, byID["INV-1"].Shown)
	}

	// review.json keeps every touched invariant so the remainder is reachable; the
	// table is a subset, not the whole set.
	if len(sec.Rows) != 11 {
		t.Errorf("all 11 touched invariants must stay in Rows for review.json; got %d", len(sec.Rows))
	}

	var b strings.Builder
	writeRegistrySection(&b, sec)
	out := b.String()
	if !strings.Contains(out, "_9 more touched invariants did not clear the reporting threshold — see review.json._") {
		t.Errorf("want a footer counting the 9 hidden rows:\n%s", out)
	}
	// The table's first data row is INV-8, before INV-4, before anything else.
	i8, i4, i6 := strings.Index(out, "| `INV-8` |"), strings.Index(out, "| `INV-4` |"), strings.Index(out, "| `INV-6` |")
	if i8 < 0 || i4 < 0 || !(i8 < i4) {
		t.Errorf("INV-8 must render above INV-4:\n%s", out)
	}
	if i6 >= 0 {
		t.Errorf("INV-6 is below the floor and must not appear in the table:\n%s", out)
	}
}

// TestSignificance_Guard pins the two halves of the rule that the acceptance check
// alone does not exercise: full coverage (touched == citing) is real signal at any
// count and bypasses the floor, while a single-file touch of a multi-file invariant
// — the noise defect 2 exists to bound — is hidden even at a high proportion.
func TestSignificance_Guard(t *testing.T) {
	reg := &invariant.Result{
		Registry:     "docs/invariants.md",
		FilesScanned: 10,
		Links: []invariant.Link{
			// Full coverage at count 1: the change touched the only file citing it.
			// Real signal at any count, so shown even though touched < 2.
			{Invariant: invariant.Invariant{ID: "INV-SOLO", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/solo.go"}},
			// Full coverage at count 2: shown.
			{Invariant: invariant.Invariant{ID: "INV-PAIR", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/pair_a.go", "sim/pair_b.go"}},
			// One of two citing files: 50%, above the floor, but a single-file touch
			// of a multi-file invariant. This is the promotion the guard blocks.
			{Invariant: invariant.Invariant{ID: "INV-HALF", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/half_a.go", "sim/half_b.go"}},
		},
	}
	sec := buildRegistrySection(reg, []string{"sim/solo.go", "sim/pair_a.go", "sim/pair_b.go", "sim/half_a.go"}, nil)
	byID := map[string]RegistryRow{}
	for _, r := range sec.Rows {
		byID[r.ID] = r
	}
	if !byID["INV-SOLO"].Shown {
		t.Error("the change touched the only file citing INV-SOLO — full coverage is signal at any count")
	}
	if !byID["INV-PAIR"].Shown {
		t.Error("2 of 2 citing files changed is full-coverage signal; it must be shown")
	}
	if byID["INV-HALF"].Shown {
		t.Error("1 of 2 (50%) is a single-file touch of a multi-file invariant; the guard must hide it")
	}
}

// TestRemovedLastAnchorRenders is defect 3's exception: standing UNLINKED does not
// render, but an invariant whose *last* citation this change removed — UNLINKED at
// head as a result — is an event about the change and must be reported loudly.
func TestRemovedLastAnchorRenders(t *testing.T) {
	reg := &invariant.Result{
		Registry:     "docs/invariants.md",
		FilesScanned: 10,
		Links: []invariant.Link{
			// UNLINKED at head: the change deleted its only citing file.
			{Invariant: invariant.Invariant{ID: "INV-ERODED", Scope: "docs/invariants.md"}},
		},
	}
	sec := buildRegistrySection(reg, nil, map[string][]string{"INV-ERODED": {"sim/gone.go"}})
	if len(sec.Rows) != 1 || sec.Rows[0].ID != "INV-ERODED" || !sec.Rows[0].Shown {
		t.Fatalf("a removed-last-anchor invariant must render; got %+v", sec.Rows)
	}
	var b strings.Builder
	writeRegistrySection(&b, sec)
	if out := b.String(); !strings.Contains(out, "deletes files that cited `INV-ERODED`") {
		t.Errorf("want the deleted-anchor line reported loudly:\n%s", out)
	}
}

// TestJSONHiddenRowsCarryShownFlag proves the JSON is a superset of the table and
// that a consumer can tell which rows appeared without re-deriving the gate.
func TestJSONHiddenRowsCarryShownFlag(t *testing.T) {
	reg, changedFiles := blisPR1725Fixture()
	sec := buildRegistrySection(reg, changedFiles, nil)
	raw, err := json.Marshal(sec)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		SchemaVersion int `json:"schemaVersion"`
		Rows          []struct {
			ID    string `json:"id"`
			Shown bool   `json:"shown"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 2 {
		t.Errorf("schemaVersion = %d, want 2 (rows now mean all-touched, not the table)", got.SchemaVersion)
	}
	shownByID := map[string]bool{}
	for _, r := range got.Rows {
		shownByID[r.ID] = r.Shown
	}
	if len(got.Rows) != 11 {
		t.Errorf("review.json must carry all 11 touched rows; got %d", len(got.Rows))
	}
	if !shownByID["INV-8"] || shownByID["INV-6"] {
		t.Errorf("shown flags wrong: INV-8=%v (want true), INV-6=%v (want false)", shownByID["INV-8"], shownByID["INV-6"])
	}
}

// TestTouchedNamedTestIsShown: a test named for an invariant, in a file the change
// edited, is the exact event the "named tests in touched files" column exists for.
// It carries no citing-file proportion, so the proportion floor must not bury it —
// the change touched a test guarding the promise, which a reviewer needs to see.
func TestTouchedNamedTestIsShown(t *testing.T) {
	reg := &invariant.Result{
		Registry:     "docs/invariants.md",
		FilesScanned: 10,
		Links: []invariant.Link{
			// Cited across many files; the change touched none of them, but it did
			// touch a file holding a test named for the invariant. touched == 0.
			{Invariant: invariant.Invariant{ID: "INV-GUARD", Scope: "docs/invariants.md"},
				CodeFiles:  []string{"sim/a.go", "sim/b.go", "sim/c.go"},
				NamedTests: []string{"sim/guard_test.go:TestINVGUARD_Holds"}},
		},
	}
	sec := buildRegistrySection(reg, []string{"sim/guard_test.go"}, nil)
	if len(sec.Rows) != 1 {
		t.Fatalf("the touched named test must produce a row; got %d", len(sec.Rows))
	}
	if r := sec.Rows[0]; !r.Shown || r.NamedTestsInTouchedFiles != 1 || r.CitingFilesTouched != 0 {
		t.Errorf("a touched named test must be shown regardless of citing proportion; got %+v", r)
	}
}

// TestFloorBoundary pins the 10% comparison as inclusive: a multi-file touch at
// exactly 10% shows, just under does not. In the acceptance fixture the only
// exact-10% rows are single-file (killed by touched>=2), so the floor's own
// boundary is otherwise never exercised — a flip of >= to > or *10 to *11 would
// ship green without this.
func TestFloorBoundary(t *testing.T) {
	mk := func(id string, citing int) invariant.Link {
		var files []string
		for i := 0; i < citing; i++ {
			files = append(files, fmt.Sprintf("sim/%s_%d.go", id, i))
		}
		return invariant.Link{Invariant: invariant.Invariant{ID: id, Scope: "docs/invariants.md"}, CodeFiles: files}
	}
	reg := &invariant.Result{
		Registry:     "docs/invariants.md",
		FilesScanned: 50,
		Links:        []invariant.Link{mk("INV-AT", 20), mk("INV-UNDER", 21)},
	}
	// Touch exactly 2 files of each: INV-AT = 2/20 = 10% (shown), INV-UNDER = 2/21
	// < 10% (hidden). Both have touched >= 2, so only the floor decides.
	changedFiles := []string{"sim/INV-AT_0.go", "sim/INV-AT_1.go", "sim/INV-UNDER_0.go", "sim/INV-UNDER_1.go"}
	sec := buildRegistrySection(reg, changedFiles, nil)
	byID := map[string]RegistryRow{}
	for _, r := range sec.Rows {
		byID[r.ID] = r
	}
	if !byID["INV-AT"].Shown {
		t.Error("2 of 20 = exactly 10% must show; the floor is inclusive")
	}
	if byID["INV-UNDER"].Shown {
		t.Error("2 of 21 < 10% must be hidden")
	}
}

// TestTieBreakOnTouchedCount: two rows at equal proportion break the tie on the raw
// touched count, then ID. Nothing in the acceptance fixture ties, so this pins the
// comparator's second and third keys.
func TestTieBreakOnTouchedCount(t *testing.T) {
	mk := func(id string, citing int) invariant.Link {
		var files []string
		for i := 0; i < citing; i++ {
			files = append(files, fmt.Sprintf("sim/%s_%d.go", id, i))
		}
		return invariant.Link{Invariant: invariant.Invariant{ID: id, Scope: "docs/invariants.md"}, CodeFiles: files}
	}
	// INV-A 4/20 and INV-Z 2/10 are both 20%; the larger touch (INV-A) must lead.
	// The IDs are chosen so alphabetical order (INV-A < INV-Z) *contradicts* the
	// touched-count order — otherwise dropping the touched-count key entirely would
	// still pass by falling through to the ID tiebreak.
	reg := &invariant.Result{
		Registry:     "docs/invariants.md",
		FilesScanned: 50,
		Links:        []invariant.Link{mk("INV-Z", 10), mk("INV-A", 20)},
	}
	var changedFiles []string
	for i := 0; i < 2; i++ {
		changedFiles = append(changedFiles, fmt.Sprintf("sim/INV-Z_%d.go", i))
	}
	for i := 0; i < 4; i++ {
		changedFiles = append(changedFiles, fmt.Sprintf("sim/INV-A_%d.go", i))
	}
	sec := buildRegistrySection(reg, changedFiles, nil)
	if len(sec.Rows) != 2 || sec.Rows[0].ID != "INV-A" || sec.Rows[1].ID != "INV-Z" {
		var ids []string
		for _, r := range sec.Rows {
			ids = append(ids, r.ID)
		}
		t.Errorf("equal proportion (20%%) must break on touched count (INV-A=4 before INV-Z=2), not ID: got %v", ids)
	}
}

// TestNamedOnlyRowSortsBelowProportionRows: a row shown only because a named test
// was touched has no citing proportion (touched==0), so it must sort below every
// proportion-ranked row but above the hidden tail, and the footer must count the
// hidden rows even when a named-only row is on the table. The flow1 golden exercises
// this (INV-9), but CI does not run that golden without BLIS_REPO, so it is pinned
// here too.
func TestNamedOnlyRowSortsBelowProportionRows(t *testing.T) {
	reg := &invariant.Result{
		Registry:     "docs/invariants.md",
		FilesScanned: 50,
		Links: []invariant.Link{
			// Proportion-shown: 2 of 4 = 50%, touched >= 2.
			{Invariant: invariant.Invariant{ID: "INV-PROP", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/p0.go", "sim/p1.go", "sim/p2.go", "sim/p3.go"}},
			// Named-only shown: no citing file touched, but a named test's file is.
			{Invariant: invariant.Invariant{ID: "INV-NAMED", Scope: "docs/invariants.md"},
				CodeFiles:  []string{"sim/n0.go", "sim/n1.go", "sim/n2.go"},
				NamedTests: []string{"sim/named_test.go:TestINVNAMED_Holds"}},
			// Hidden: single-file touch of a multi-file invariant.
			{Invariant: invariant.Invariant{ID: "INV-HID", Scope: "docs/invariants.md"},
				CodeFiles: []string{"sim/h0.go", "sim/h1.go", "sim/h2.go", "sim/h3.go", "sim/h4.go"}},
		},
	}
	changedFiles := []string{"sim/p0.go", "sim/p1.go", "sim/named_test.go", "sim/h0.go"}
	sec := buildRegistrySection(reg, changedFiles, nil)

	var shown []string
	for _, r := range sec.Rows {
		if r.Shown {
			shown = append(shown, r.ID)
		}
	}
	if len(shown) != 2 || shown[0] != "INV-PROP" || shown[1] != "INV-NAMED" {
		t.Errorf("named-only row must sort below the proportion row: want [INV-PROP INV-NAMED], got %v", shown)
	}
	var b strings.Builder
	writeRegistrySection(&b, sec)
	out := b.String()
	if !strings.Contains(out, "_1 more touched invariant did not clear the reporting threshold — see review.json._") {
		t.Errorf("footer must count the 1 hidden row while a named-only row is shown:\n%s", out)
	}
}

// TestAllRowsHidden covers the render branch where every touched row is below the
// guard: no table header (which would otherwise be an empty, misleading table), and
// a footer that drops "more" — there is no table for the tail to be more than — while
// the count still distinguishes this from a registry the change never touched.
func TestAllRowsHidden(t *testing.T) {
	mk := func(id string, citing int) invariant.Link {
		var files []string
		for i := 0; i < citing; i++ {
			files = append(files, fmt.Sprintf("sim/%s_%d.go", id, i))
		}
		return invariant.Link{Invariant: invariant.Invariant{ID: id, Scope: "docs/invariants.md"}, CodeFiles: files}
	}
	// Three single-file touches of multi-file invariants: each touched == 1, none is
	// full coverage or a named-test edit, so all three are hidden.
	reg := &invariant.Result{
		Registry:     "docs/invariants.md",
		FilesScanned: 50,
		Links:        []invariant.Link{mk("INV-A", 8), mk("INV-B", 9), mk("INV-C", 10)},
	}
	sec := buildRegistrySection(reg, []string{"sim/INV-A_0.go", "sim/INV-B_0.go", "sim/INV-C_0.go"}, nil)
	if len(sec.Rows) != 3 {
		t.Fatalf("all three are touched, so all three are rows; got %d", len(sec.Rows))
	}
	for _, r := range sec.Rows {
		if r.Shown {
			t.Fatalf("every row should be hidden; %s is shown", r.ID)
		}
	}
	var b strings.Builder
	writeRegistrySection(&b, sec)
	out := b.String()
	if strings.Contains(out, "| ID | Status") {
		t.Errorf("no shown rows, so no table header:\n%s", out)
	}
	if !strings.Contains(out, "_3 touched invariants did not clear the reporting threshold — see review.json._") {
		t.Errorf("want an all-hidden footer without \"more\":\n%s", out)
	}
	if strings.Contains(out, "more touched") {
		t.Errorf("with no table, the footer must not say \"more\":\n%s", out)
	}
}
