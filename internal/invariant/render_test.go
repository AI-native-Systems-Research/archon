package invariant

import (
	"encoding/json"
	"errors"
	"io"
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
		Registry:     "docs/invariants.md",
		Commit:       "73a17c00",
		FilesScanned: 120,
		Links: []Link{
			{Invariant: inv("INV-1", "Request Conservation"),
				CodeFiles: []string{"core/engine.go", "core/replay.go"},
				TestFiles: []string{"core/engine_test.go"},
				// Two, so the continuation line — which repeats no ID — is covered.
				NamedTests: []string{"core/engine_test.go:TestINV1_Conservation", "core/replay_test.go:TestINV1_Replay"},
				Citations:  7},
			{Invariant: inv("INV-2", "Request Lifecycle"),
				TestFiles: []string{"lifecycle/lifecycle_test.go"},
				Citations: 1},
			{Invariant: inv("INV-99", "Never Anchored")},
		},
	}
}

func TestRender_Table(t *testing.T) {
	var b strings.Builder
	if err := Render(&b, fixtureResult()); err != nil {
		t.Fatal(err)
	}
	got := b.String()

	// Whole rows, not loose substrings: "LINKED" is a substring of "UNLINKED",
	// and per-field assertions cannot catch a status landing on the wrong row or
	// two columns swapping.
	for _, row := range []string{
		"INV-1        LINKED        2     1      7      2",
		"INV-2        TEST ONLY     0     1      1      0",
		"INV-99       UNLINKED      0     0      0      0",
	} {
		if !strings.Contains(got, row) {
			t.Errorf("missing row\n  %q\nin\n%s", row, got)
		}
	}
	// The summary is the product of this command, so it is pinned exactly.
	if want := "  2 of 3 anchored — 1 LINKED, 1 TEST ONLY, 1 UNLINKED"; !strings.Contains(got, want) {
		t.Errorf("missing totals line %q:\n%s", want, got)
	}
	// The continuation line carries no ID.
	if want := "                 core/replay_test.go:TestINV1_Replay"; !strings.Contains(got, want) {
		t.Errorf("a second named test should continue without repeating the ID:\n%s", got)
	}

	for _, want := range []string{
		"docs/invariants.md",
		"73a17c00",
		"INV-1", "LINKED",
		"INV-2", "TEST ONLY",
		"INV-99", "UNLINKED",
		"core/engine_test.go:TestINV1_Conservation",
		"tests named for an invariant:",
		"scanned:  120 Go files",
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
	if err := Render(&b, fixtureResult()); err != nil {
		t.Fatal(err)
	}
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

	var b strings.Builder
	if err := Render(&b, res); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, "0 of 3 anchored") {
		t.Errorf("want a \"0 of 3 anchored\" finding:\n%s", got)
	}
	if !strings.Contains(got, "3 UNLINKED") {
		t.Errorf("want the totals line:\n%s", got)
	}
}

// TestRender_NamedTestsAreCountedInTheTableAndListedBelow: an invariant can have a
// dozen or more named tests, so a NAMED TESTS column turns a scannable row into a
// wrapped paragraph. The count stays in the row; the names go underneath.
func TestRender_NamedTestsAreCountedInTheTableAndListedBelow(t *testing.T) {
	var b strings.Builder
	if err := Render(&b, fixtureResult()); err != nil {
		t.Fatal(err)
	}
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
	if !strings.HasSuffix(strings.TrimSpace(row), "2") {
		t.Errorf("INV-1 row should end in its named-test count of 2: %q", row)
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
	if err := Render(&b, res); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "named for an invariant") {
		t.Errorf("no named tests, so no section:\n%s", b.String())
	}
}

func TestRender_NoCommitOmitsTheCommitLine(t *testing.T) {
	res := fixtureResult()
	res.Commit = ""
	var b strings.Builder
	if err := Render(&b, res); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "commit") {
		t.Errorf("no commit was pinned, so no commit line:\n%s", b.String())
	}
}

// TestResult_ValidateRejectsAbsolutePaths: the rule used to be checked inside
// Render, so --json marshalled straight past it and could publish a
// machine-specific path into a byte-compared golden.
func TestResult_ValidateRejectsAbsolutePaths(t *testing.T) {
	cases := map[string]func(*Result){
		"absolute registry":  func(r *Result) { r.Registry = "/Users/me/repo/docs/invariants.md" },
		"absolute source":    func(r *Result) { r.Links[0].Invariant.Source = "/var/folders/x/archon-wt-1/docs/i.md:3" },
		"absolute scope":     func(r *Result) { r.Links[0].Invariant.Scope = "/var/folders/x/archon-wt-1/docs/i.md" },
		"absolute code file": func(r *Result) { r.Links[0].CodeFiles = []string{"/tmp/repo/core/engine.go"} },
		"absolute test file": func(r *Result) { r.Links[0].TestFiles = []string{"/tmp/repo/core/engine_test.go"} },
		"absolute named":     func(r *Result) { r.Links[0].NamedTests = []string{"/tmp/repo/x_test.go:TestINV1"} },
	}
	for name, mutate := range cases {
		res := fixtureResult()
		mutate(&res)
		if err := res.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
		// Render must refuse it too, and write nothing rather than a bad table.
		var b strings.Builder
		if err := Render(&b, res); err == nil {
			t.Errorf("%s: Render accepted it", name)
		}
	}
}

// TestResult_ValidateRejectsNilLinks: a nil slice marshals as null, which makes
// "the registry declared nothing" and "no registry was read" the same document.
func TestResult_ValidateRejectsNilLinks(t *testing.T) {
	res := fixtureResult()
	res.Links = nil
	if err := res.Validate(); err == nil {
		t.Error("Validate accepted nil Links")
	}
	if err := Render(io.Discard, res); err == nil {
		t.Error("Render accepted nil Links")
	}
	// An empty-but-non-nil registry result is a legitimate document.
	res.Links = []Link{}
	if err := res.Validate(); err != nil {
		t.Errorf("an empty link set is valid: %v", err)
	}
}

// TestResult_JSONCarriesSchemaAndDerivedTotals: the totals in the document are
// computed from the links, so a consumer cannot be handed a summary that
// disagrees with the rows.
func TestResult_JSONCarriesSchemaAndDerivedTotals(t *testing.T) {
	b, err := json.Marshal(fixtureResult())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		SchemaVersion int `json:"schema_version"`
		FilesScanned  int `json:"files_scanned"`
		Totals        struct {
			Linked   int `json:"linked"`
			TestOnly int `json:"test_only"`
			Unlinked int `json:"unlinked"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, SchemaVersion)
	}
	if got.FilesScanned != 120 {
		t.Errorf("files_scanned = %d, want 120", got.FilesScanned)
	}
	if got.Totals.Linked != 1 || got.Totals.TestOnly != 1 || got.Totals.Unlinked != 1 {
		t.Errorf("totals = %+v, want 1/1/1", got.Totals)
	}
}

// TestRender_WriteErrorIsReported: a half-written table that exits 0 is the same
// class of lie as a wrong number.
func TestRender_WriteErrorIsReported(t *testing.T) {
	if err := Render(&failingWriter{after: 3}, fixtureResult()); err == nil {
		t.Error("Render swallowed a write error")
	}
}

type failingWriter struct{ after int }

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.after <= 0 {
		return 0, errors.New("disk full")
	}
	f.after--
	return len(p), nil
}
