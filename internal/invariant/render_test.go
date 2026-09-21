package invariant

import (
	"strings"
	"testing"
)

// fixtureResult is the shape a caller renders: a registry, an optional pinned
// commit, the links, and the totals.
func fixtureResult() Result {
	inv := func(id, title string) Invariant {
		return Invariant{ID: id, Scope: "docs/invariants.md", Title: title, Tier: TierH3, Source: "docs/invariants.md:1"}
	}
	return Result{
		Registry: "docs/invariants.md",
		Commit:   "73a17c00",
		Links: []Link{
			{Invariant: inv("INV-1", "Request Conservation"),
				CodeFiles:  []string{"core/engine.go", "core/replay.go"},
				TestFiles:  []string{"core/engine_test.go"},
				NamedTests: []string{"core/engine_test.go:TestINV1_Conservation"},
				Citations:  7},
			{Invariant: inv("INV-2", "Request Lifecycle"),
				TestFiles: []string{"lifecycle/lifecycle_test.go"},
				Citations: 1},
			{Invariant: inv("INV-99", "Never Anchored")},
		},
		Totals: Totals{Linked: 1, TestOnly: 1, Unlinked: 1},
	}
}

func TestRender_Table(t *testing.T) {
	var b strings.Builder
	Render(&b, fixtureResult())
	got := b.String()

	for _, want := range []string{
		"docs/invariants.md",
		"73a17c00",
		"INV-1", "LINKED",
		"INV-2", "TEST ONLY",
		"INV-99", "UNLINKED",
		"core/engine_test.go:TestINV1_Conservation",
		"tests named for an invariant:",
		"1 LINKED", "1 TEST ONLY", "1 UNLINKED",
		"2 of 3 anchored",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestRender_NoAbsolutePathsOrTrailingSpace: this output is diffed byte-for-byte
// by demo/run-all.sh. demo/flow1-pr-review's goldens embed one absolute repo path
// and consequently only reproduce from a single checkout — not repeating that.
func TestRender_NoAbsolutePathsOrTrailingSpace(t *testing.T) {
	var b strings.Builder
	Render(&b, fixtureResult())
	for i, line := range strings.Split(b.String(), "\n") {
		if strings.Contains(line, "/var/folders/") || strings.Contains(line, "/Users/") || strings.Contains(line, "/tmp/") {
			t.Errorf("line %d has an absolute path: %q", i+1, line)
		}
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}
}

// TestRender_ZeroAnchoredIsAFinding: "0 of N anchored" is the most useful thing
// archon can tell a repo adopting this, so it must render as a stated result and
// not as an empty table.
func TestRender_ZeroAnchoredIsAFinding(t *testing.T) {
	res := fixtureResult()
	for i := range res.Links {
		res.Links[i].CodeFiles, res.Links[i].TestFiles, res.Links[i].NamedTests, res.Links[i].Citations = nil, nil, nil, 0
	}
	res.Totals = Totals{Unlinked: 3}

	var b strings.Builder
	Render(&b, res)
	got := b.String()
	if !strings.Contains(got, "0 of 3 anchored") {
		t.Errorf("want a \"0 of 3 anchored\" finding:\n%s", got)
	}
	if !strings.Contains(got, "3 UNLINKED") {
		t.Errorf("want the totals line:\n%s", got)
	}
}

// TestRender_NamedTestsAreCountedInTheTableAndListedBelow: one BLIS invariant has
// fourteen named tests, so a NAMED TESTS column turns a scannable row into a
// wrapped paragraph. The count stays in the row; the names go underneath.
func TestRender_NamedTestsAreCountedInTheTableAndListedBelow(t *testing.T) {
	var b strings.Builder
	Render(&b, fixtureResult())
	lines := strings.Split(b.String(), "\n")

	// The first INV-1 line is the table row; a later one is its entry in the
	// names list below.
	var row string
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "INV-1 ") {
			row = l
			break
		}
	}
	if row == "" {
		t.Fatalf("no INV-1 row:\n%s", b.String())
	}
	if strings.Contains(row, "TestINV1_Conservation") {
		t.Errorf("the table row must carry the count, not the names: %q", row)
	}
	if !strings.HasSuffix(strings.TrimSpace(row), "1") {
		t.Errorf("INV-1 row should end in its named-test count of 1: %q", row)
	}

	// An invariant with no named tests contributes no line to the list.
	if strings.Contains(b.String(), "INV-99 ") && strings.Count(b.String(), "INV-99") != 1 {
		t.Errorf("INV-99 has no named tests and should appear once:\n%s", b.String())
	}
}

// TestRender_NoNamedTestsOmitsTheSection keeps the output honest when nothing in
// the repo is named for an invariant: an empty heading reads as a rendering bug.
func TestRender_NoNamedTestsOmitsTheSection(t *testing.T) {
	res := fixtureResult()
	for i := range res.Links {
		res.Links[i].NamedTests = nil
	}
	var b strings.Builder
	Render(&b, res)
	if strings.Contains(b.String(), "named for an invariant") {
		t.Errorf("no named tests, so no section:\n%s", b.String())
	}
}

func TestRender_NoCommitOmitsTheCommitLine(t *testing.T) {
	res := fixtureResult()
	res.Commit = ""
	var b strings.Builder
	Render(&b, res)
	if strings.Contains(b.String(), "commit") {
		t.Errorf("no commit was pinned, so no commit line:\n%s", b.String())
	}
}
