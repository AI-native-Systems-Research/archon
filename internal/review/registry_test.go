package review

import (
	"encoding/json"
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
	sec := buildRegistrySection(fixtureRegistry(), changed)
	if sec == nil {
		t.Fatal("no section")
	}
	if sec.Declared != 3 || sec.Anchored != 2 {
		t.Errorf("declared/anchored = %d/%d, want 3/2", sec.Declared, sec.Anchored)
	}

	byID := map[string]RegistryRow{}
	for _, r := range sec.Rows {
		byID[r.ID] = r
	}
	if got := byID["INV-6"]; got.FilesTouched != 3 || got.FilesTotal != 4 || got.NamedTouched != 1 || got.NamedTotal != 2 {
		t.Errorf("INV-6 = %+v, want 3/4 files and 1/2 named", got)
	}
	// Declared, cited nowhere: a finding, so it appears even though the change
	// cannot have touched it.
	if got, ok := byID["INV-PD-2"]; !ok || got.Status != string(invariant.StatusUnlinked) {
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
	sec := buildRegistrySection(reg, changed)
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
	sec := buildRegistrySection(reg, changed)
	if sec.Anchored != 0 || sec.Declared != 3 {
		t.Fatalf("anchored/declared = %d/%d, want 0/3", sec.Anchored, sec.Declared)
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
	writeRegistrySection(&b, buildRegistrySection(fixtureRegistry(), changed))
	out := b.String()
	if !strings.Contains(out, "`docs/invariants.md`") {
		t.Errorf("the section must name the registry path it used:\n%s", out)
	}
	if !strings.Contains(out, "d77764f5") {
		t.Errorf("the section should name the commit it read:\n%s", out)
	}
}

func TestRegistrySection_NilIsNoSection(t *testing.T) {
	if sec := buildRegistrySection(nil, changed); sec != nil {
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

	plain := Build(a, b, d, Options{LabelA: "base", LabelB: "head"})
	withReg := Build(a, b, d, Options{LabelA: "base", LabelB: "head",
		Registry: fixtureRegistry(), ChangedFiles: changed})

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

	// review.json likewise: the only new key is "registry".
	plainJSON, withJSON := mustJSONKeys(t, plain), mustJSONKeys(t, withReg)
	delete(withJSON, "registry")
	if len(plainJSON) != len(withJSON) {
		t.Errorf("review.json gained or lost a key besides \"registry\": %v vs %v", plainJSON, withJSON)
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
	sec := buildRegistrySection(fixtureRegistry(), changed)
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
		FilesScanned  int    `json:"filesScanned"`
		Rows          []struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			FilesTouched int    `json:"filesTouched"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != invariant.SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", got.SchemaVersion, invariant.SchemaVersion)
	}
	if got.Registry != "docs/invariants.md" || got.Declared != 3 || got.Anchored != 2 || got.FilesScanned != 318 {
		t.Errorf("header fields wrong: %+v", got)
	}
	if len(got.Rows) == 0 || got.Rows[0].ID != "INV-6" || got.Rows[0].FilesTouched != 3 {
		t.Errorf("rows wrong: %+v", got.Rows)
	}
}
