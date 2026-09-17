package invariant

import (
	"reflect"
	"testing"
)

// fixtureRepo is a tiny repository under testdata: INV-1, INV-6 and INV-13
// cited in production code, INV-2 cited only from a test, INV-42 with nothing
// but a test named for it, INV-99 nowhere at all — plus a vendor/ directory, a
// dot-directory and a .md file that must not be scanned.
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
	if namedFor("TestFooE2E", []string{"INV2"}, map[string]string{"INV2": "INV-2"}) != nil {
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
	ids := []string{"INV-6", "INV-1", "INV-13", "INV-P2-1", "INV-PD-3", "INV-PD-6", "INV-PD-6b", "INV-A", "NS-6"}
	norm := map[string]string{}
	var normIDs []string
	for _, id := range ids {
		n := normalizeID(id)
		norm[n] = id
		normIDs = append(normIDs, n)
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
		if got := namedFor(c.test, normIDs, norm); !reflect.DeepEqual(got, c.want) {
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
