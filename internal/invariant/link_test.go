package invariant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureRepo is a tiny repository under testdata: INV-1, INV-6 and INV-13
// cited in production code, INV-2 cited only from a test, INV-42 with nothing
// but a test named for it, INV-99 nowhere at all — plus the paths that must not
// count: vendor/, a dot-directory, a .md file, a nested testdata/ and an
// underscore-prefixed source file.
const fixtureRepo = "testdata/repo"

func fixtureInvariants() []Invariant {
	ids := []string{"INV-1", "INV-2", "INV-6", "INV-13", "INV-42", "INV-99"}
	invs := make([]Invariant, len(ids))
	for i, id := range ids {
		invs[i] = Invariant{ID: id, Scope: "docs/invariants.md"}
	}
	return invs
}

func linkFixture(t *testing.T) map[string]Link {
	t.Helper()
	links, err := LinkRepo(fixtureRepo, fixtureInvariants())
	if err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	byID := map[string]Link{}
	for _, l := range links {
		byID[l.Invariant.ID] = l
	}
	return byID
}

// TestLinkRepo_BareNumberNeverMatches is the first mandatory regression.
// Matching INV-2 by its numeric part matched every test whose name contained
// "E2E" — output that looked entirely plausible. INV-2 is cited in
// lifecycle_test.go and nowhere else, and TestFooE2E is named for nothing.
func TestLinkRepo_BareNumberNeverMatches(t *testing.T) {
	inv2 := linkFixture(t)["INV-2"]

	if got := inv2.Status(); got != StatusTestOnly {
		t.Errorf("INV-2 status = %s, want %s", got, StatusTestOnly)
	}
	if want := []string{"lifecycle/lifecycle_test.go"}; !reflect.DeepEqual(inv2.TestFiles, want) {
		t.Errorf("INV-2 TestFiles = %v, want %v", inv2.TestFiles, want)
	}
	if len(inv2.NamedTests) != 0 {
		t.Errorf("INV-2 NamedTests = %v, want none — TestFooE2E is not named for INV-2", inv2.NamedTests)
	}

	// Directly, too: the pattern itself must reject a bare number.
	if CitationRegexp("INV-2").MatchString("func TestFooE2E(t *testing.T) {}") {
		t.Error("CitationRegexp(INV-2) matched TestFooE2E")
	}
	if namedFor("TestFooE2E", map[string]string{"INV2": "INV-2"}) != nil {
		t.Error("namedFor attributed TestFooE2E to INV-2")
	}
}

// TestLinkRepo_INV1DoesNotMatchINV13 is the second mandatory regression. A
// prefix ID must not absorb a longer one: INV-1 owns exactly the sites that say
// INV-1, and TestINV13_RunReplayParity belongs to INV-13.
func TestLinkRepo_INV1DoesNotMatchINV13(t *testing.T) {
	byID := linkFixture(t)
	inv1, inv13 := byID["INV-1"], byID["INV-13"]

	if want := []string{"core/engine.go"}; !reflect.DeepEqual(inv1.CodeFiles, want) {
		t.Errorf("INV-1 CodeFiles = %v, want %v — parity.go cites only INV-13", inv1.CodeFiles, want)
	}
	if want := []string{"parity/parity.go"}; !reflect.DeepEqual(inv13.CodeFiles, want) {
		t.Errorf("INV-13 CodeFiles = %v, want %v", inv13.CodeFiles, want)
	}
	if want := []string{"core/engine_test.go:TestINV1_Conservation"}; !reflect.DeepEqual(inv1.NamedTests, want) {
		t.Errorf("INV-1 NamedTests = %v, want %v", inv1.NamedTests, want)
	}
	if want := []string{"core/engine_test.go:TestINV13_RunReplayParity"}; !reflect.DeepEqual(inv13.NamedTests, want) {
		t.Errorf("INV-13 NamedTests = %v, want %v", inv13.NamedTests, want)
	}

	// And at the pattern level, in both directions.
	if CitationRegexp("INV-1").MatchString("// INV-13 holds across run and replay.") {
		t.Error("CitationRegexp(INV-1) matched an INV-13 citation")
	}
	if !CitationRegexp("INV-13").MatchString("// INV-13 holds.") {
		t.Error("CitationRegexp(INV-13) missed its own citation")
	}
	if !CitationRegexp("INV-1").MatchString("// INV-1 at end of line") {
		t.Error("CitationRegexp(INV-1) missed a plain citation")
	}
	if !CitationRegexp("INV-1").MatchString("upholds INV-1") {
		t.Error("CitationRegexp(INV-1) requires a trailing character; it must match at end of input")
	}
}

// TestLinkRepo_NamedTestIsTestEvidence: INV-42 is spelled out in no file; one
// test is named for it and that is all. Reporting UNLINKED — "nowhere" — would
// be wrong, so a named test counts as test evidence.
func TestLinkRepo_NamedTestIsTestEvidence(t *testing.T) {
	l := linkFixture(t)["INV-42"]
	if l.Citations != 0 || len(l.TestFiles) != 0 {
		t.Fatalf("INV-42 should have no textual citation, got %+v", l)
	}
	if want := []string{"lifecycle/named_only_test.go:TestINV42_NamedOnly"}; !reflect.DeepEqual(l.NamedTests, want) {
		t.Fatalf("INV-42 NamedTests = %v, want %v", l.NamedTests, want)
	}
	if got := l.Status(); got != StatusTestOnly {
		t.Errorf("INV-42 status = %s, want %s", got, StatusTestOnly)
	}
}

func TestLinkRepo_StatusesAndCounts(t *testing.T) {
	byID := linkFixture(t)

	for id, want := range map[string]Status{
		"INV-1":  StatusLinked,
		"INV-6":  StatusLinked,
		"INV-13": StatusLinked,
		"INV-2":  StatusTestOnly,
		"INV-42": StatusTestOnly,
		"INV-99": StatusUnlinked,
	} {
		if got := byID[id].Status(); got != want {
			t.Errorf("%s status = %s, want %s", id, got, want)
		}
	}

	// Citations count occurrences, not files: engine.go names INV-6 twice.
	// This is the number BLIS maintains by hand in its LoRA table.
	if got := byID["INV-6"].Citations; got != 2 {
		t.Errorf("INV-6 Citations = %d, want 2", got)
	}
	if got := byID["INV-99"]; got.Citations != 0 || got.CodeFiles != nil || got.TestFiles != nil {
		t.Errorf("INV-99 should be empty, got %+v", got)
	}

	links, err := LinkRepo(fixtureRepo, fixtureInvariants())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Count(links), (Totals{Linked: 3, TestOnly: 2, Unlinked: 1}); got != want {
		t.Errorf("Count = %+v, want %+v", got, want)
	}
}

// TestLinkRepo_SkipsVendorDotDirsAndNonGo: vendor/dep/dep.go, .hidden/hidden.go
// and notes.md all cite INV-1. None of them is a citation. Excluding non-Go
// files is also what stops a registry from counting as a citation of itself.
func TestLinkRepo_SkipsVendorDotDirsAndNonGo(t *testing.T) {
	inv1 := linkFixture(t)["INV-1"]
	for _, f := range append(append([]string{}, inv1.CodeFiles...), inv1.TestFiles...) {
		switch f {
		case "vendor/dep/dep.go", ".hidden/hidden.go", "notes.md":
			t.Errorf("scanned %s, which must be skipped", f)
		}
	}
	if inv1.Citations != 1 {
		t.Errorf("INV-1 Citations = %d, want 1 (engine.go only)", inv1.Citations)
	}
}

// TestLinkRepo_ScopeIsRespected: the same ID declared in two registries is two
// invariants. BC-1 is cited 307 times across four BLIS packages meaning
// something different in each, which is why Scope exists from day one.
func TestLinkRepo_ScopeIsRespected(t *testing.T) {
	a := Invariant{ID: "INV-1", Scope: "docs/a.md", Title: "Request conservation"}
	b := Invariant{ID: "INV-1", Scope: "docs/b.md", Title: "Something else entirely"}
	if a.Key() == b.Key() {
		t.Fatal("Key() must distinguish scopes")
	}

	links, err := LinkRepo(fixtureRepo, []Invariant{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("got %d links, want one per scope", len(links))
	}
	for _, l := range links {
		if !reflect.DeepEqual(l.CodeFiles, []string{"core/engine.go"}) {
			t.Errorf("scope %s: CodeFiles = %v", l.Invariant.Scope, l.CodeFiles)
		}
	}
	if links[0].Invariant.Scope == links[1].Invariant.Scope {
		t.Error("links must keep their invariant's scope")
	}
}

func TestNamedFor_SeparatorSpellings(t *testing.T) {
	norm := map[string]string{}
	for _, id := range []string{"INV-6", "INV-1", "INV-13", "INV-P2-1", "INV-PD-3", "INV-PD-6", "INV-PD-6b", "INV-A", "NS-6"} {
		norm[strings.ToUpper(normalizeID(id))] = id
	}

	cases := []struct {
		test string
		want []string
	}{
		{"TestINV6_Determinism", []string{"INV-6"}},
		{"TestINV_P2_1_PoolConfigConsistency", []string{"INV-P2-1"}},
		{"TestTransferContention_BCP27_INVPD3_Holds", []string{"INV-PD-3"}},
		{"TestPrefixAffinityScorer_INV1_INV2_Conformance", []string{"INV-1"}}, // INV-2 is not in ids
		{"TestINV13_RunReplayParity", []string{"INV-13"}},
		// A longer declared ID wins at the same position, so a test named for
		// INV-PD-6b is not also INV-PD-6's.
		{"TestDisaggregation_INVPD6b_CompletionTime", []string{"INV-PD-6b"}},
		{"TestDisaggregation_INVPD6_MetricMap", []string{"INV-PD-6"}},
		{"TestNS6_NoRuntimeFetch_StaticGuard", []string{"NS-6"}},
		{"TestFooE2E", nil},
		{"TestSomethingUnrelated", nil},
		// INV-A must not swallow INV-A2, declared or not.
		{"TestINVA2_PlacementFailureVisibility", nil},
	}
	for _, c := range cases {
		if got := namedFor(c.test, norm); !reflect.DeepEqual(got, c.want) {
			t.Errorf("namedFor(%q) = %v, want %v", c.test, got, c.want)
		}
	}
}

func TestLinkRepo_IndistinguishableIDsAreAnError(t *testing.T) {
	_, err := LinkRepo(fixtureRepo, []Invariant{
		{ID: "INV-P2-1", Scope: "s"},
		{ID: "INVP2-1", Scope: "s"},
	})
	if err == nil {
		t.Fatal("want an error: both IDs normalize to INVP21, so a named test cannot be attributed")
	}
}

func TestLinkRepo_Deterministic(t *testing.T) {
	first, err := LinkRepo(fixtureRepo, fixtureInvariants())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		next, err := LinkRepo(fixtureRepo, fixtureInvariants())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("run %d differs from the first", i)
		}
	}
}

// TestNamedFor_WordsAreNotIDSegments is the bare-number bug wearing a letter.
// With INV-A declared, substring matching on a separator-stripped name read
// "Allocator" as the "A" segment and reported these as tests for INV-A. Because
// a named test is test evidence, that promoted a genuinely unlinked invariant to
// TEST ONLY — "the promise is checked" — on the strength of an English word.
func TestNamedFor_WordsAreNotIDSegments(t *testing.T) {
	norm := map[string]string{}
	for _, id := range []string{"INV-A", "INV-1", "NS-6", "INV-PD-6"} {
		norm[strings.ToUpper(normalizeID(id))] = id
	}

	for _, name := range []string{
		"TestINVAllocatorBoundary",
		"TestOptionsINVArgs",
		"TestINVAB_Something",
		"TestBINV1_X",
		// INV-PD-6b is not declared here, which is exactly where the old
		// longest-declared-ID rule failed: a trailing lowercase letter continues
		// an ID whether or not the longer one is in the registry.
		"TestDisaggregation_INVPD6b_CompletionTime",
	} {
		if got := namedFor(name, norm); got != nil {
			t.Errorf("namedFor(%q) = %v, want none", name, got)
		}
	}

	// The real spellings still match.
	for name, want := range map[string]string{
		"TestINVA_GPUConservation": "INV-A",
		"TestINV1_Conservation":    "INV-1",
		"TestNS6_StaticGuard":      "NS-6",
		"TestX_INVPD6_MetricMap":   "INV-PD-6",
	} {
		if got := namedFor(name, norm); !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("namedFor(%q) = %v, want [%s]", name, got, want)
		}
	}
}

func TestTokenize(t *testing.T) {
	for in, want := range map[string][]string{
		"TestINV_P2_1_PoolConfig": {"Test", "INV", "P", "2", "1", "Pool", "Config"},
		"TestINVAllocator":        {"Test", "INV", "Allocator"},
		"TestX_INVPD6b_Holds":     {"Test", "X", "INVPD", "6", "b", "Holds"},
		"TestINVBCDP1_Dense":      {"Test", "INVBCDP", "1", "Dense"},
	} {
		if got := tokenize(in); !reflect.DeepEqual(got, want) {
			t.Errorf("tokenize(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestCitationRegexp_RejectsAdjectiveForm: BLIS's registry names "INV-6-safe" in
// sim/workload/fixed_accumulate_test.go as a known false positive of its own
// git-grep recipe — it is an adjective, not a citation. Counting it makes the
// number a human reads wrong by one that the source document already flagged.
func TestCitationRegexp_RejectsAdjectiveForm(t *testing.T) {
	re := CitationRegexp("INV-6")
	for _, s := range []string{"// an INV-6-safe accumulator", "// INV-6_determinism", "// fooINV-6"} {
		if re.MatchString(s) {
			t.Errorf("matched %q", s)
		}
	}
	for _, s := range []string{"// INV-6 holds", "// determinism (INV-6)", "upholds INV-6"} {
		if !re.MatchString(s) {
			t.Errorf("missed %q", s)
		}
	}
}

// TestLinkRepo_SymlinkedRoot: WalkDir does not follow symlinks, so a symlinked
// root arrived as a non-directory, was skipped for want of a .go suffix, and
// produced a confident repository-wide UNLINKED report. CI checkouts and macOS
// /tmp are routinely reached through a symlink.
func TestLinkRepo_SymlinkedRoot(t *testing.T) {
	abs, err := filepath.Abs(fixtureRepo)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(abs, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	links, err := LinkRepo(link, fixtureInvariants())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Count(links), (Totals{Linked: 3, TestOnly: 2, Unlinked: 1}); got != want {
		t.Errorf("through a symlink: Count = %+v, want %+v", got, want)
	}
}

// TestLinkRepo_BadRootIsAnError: reporting every invariant UNLINKED is the
// loudest thing this package can say about a repository, so it must not be what
// a caller gets for pointing at the wrong path.
func TestLinkRepo_BadRootIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("INV-1"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, root := range map[string]string{
		"no Go files":             dir,
		"a file, not a directory": filepath.Join(fixtureRepo, "core", "engine.go"),
		"nonexistent":             filepath.Join(dir, "nope"),
	} {
		if _, err := LinkRepo(root, fixtureInvariants()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// TestLinkRepo_SkipsPathsTheGoToolIgnores: testdata/fixture.go cites INV-1 and
// core/_scratch.go cites INV-6. The Go tool builds neither, so neither is
// evidence that anything upholds them.
func TestLinkRepo_SkipsPathsTheGoToolIgnores(t *testing.T) {
	byID := linkFixture(t)
	for _, f := range append(byID["INV-1"].CodeFiles, byID["INV-6"].CodeFiles...) {
		if strings.HasPrefix(f, "testdata/") || strings.Contains(f, "_scratch") {
			t.Errorf("scanned %s, which the Go tool ignores", f)
		}
	}
}

// TestLink_JSONCarriesStatus: Status() is derived, so a --json consumer would
// otherwise have to recompute it and could disagree with the table.
func TestLink_JSONCarriesStatus(t *testing.T) {
	b, err := json.Marshal(linkFixture(t)["INV-99"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"status":"UNLINKED"`) {
		t.Errorf("JSON = %s, want a status field", b)
	}
}

// TestNamedFor_FoldsCase: BLIS writes TestINV6_*, but a repo writing
// TestInv6_Determinism means the same thing. Tokenising first is what makes
// case-folding safe — "Invalid" is a token of its own and cannot serve as
// INV-A's "A" segment.
func TestNamedFor_FoldsCase(t *testing.T) {
	norm := map[string]string{}
	for _, id := range []string{"INV-6", "INV-A", "NS-6"} {
		norm[strings.ToUpper(normalizeID(id))] = id
	}
	for name, want := range map[string][]string{
		"TestInv6_Determinism": {"INV-6"},
		"TestNs6_Catalog":      {"NS-6"},
		"TestInvalidRequest":   nil,
		"TestInventoryAudit":   nil,
	} {
		if got := namedFor(name, norm); !reflect.DeepEqual(got, want) {
			t.Errorf("namedFor(%q) = %v, want %v", name, got, want)
		}
	}
}
