package invariant

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// blisFixture is BLIS's live registry, docs/contributing/standards/invariants.md
// at inference-sim commit 73a17c00f84f28623e254a625f1f5298bb8c8a38. It is
// checked in rather than fetched so the expectations below stay pinned.
const blisFixture = "testdata/blis-invariants.md"

func parseBLIS(t *testing.T) []Invariant {
	t.Helper()
	invs, err := ParseFile(blisFixture)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return invs
}

func find(t *testing.T, invs []Invariant, id string) Invariant {
	t.Helper()
	for _, inv := range invs {
		if inv.ID == id {
			return inv
		}
	}
	t.Fatalf("%s not parsed from the registry", id)
	return Invariant{}
}

// TestParseFile_BLISRegistry_BothEntryShapes pins the entry counts per shape.
// The registry uses ### for 9 INV-* entries and #### for 17 to express tiering,
// so a parser that assumes flat headings drops two thirds of it.
func TestParseFile_BLISRegistry_BothEntryShapes(t *testing.T) {
	invs := parseBLIS(t)

	byTier := map[string]int{}
	for _, inv := range invs {
		byTier[inv.Tier]++
	}
	// 9 INV-* at ### plus NS-6, which the registry declares at the same level.
	want := map[string]int{TierH3: 10, TierH4: 17, TierTable: 7}
	for tier, n := range want {
		if byTier[tier] != n {
			t.Errorf("tier %s: got %d entries, want %d", tier, byTier[tier], n)
		}
	}
	if len(invs) != 34 {
		t.Errorf("got %d entries, want 34", len(invs))
	}
}

// TestParseFile_IndexRowsAreNotEntries is the reason a table entry must have a
// bold ID in its first cell. The registry's index has a row per invariant
// ("| [INV-1](#inv-1-request-conservation) Request conservation | ... |"); if
// those parsed as entries, every indexed invariant would be declared twice and
// the duplicate check below would reject BLIS's own registry.
func TestParseFile_IndexRowsAreNotEntries(t *testing.T) {
	invs := parseBLIS(t)
	seen := map[string]string{}
	for _, inv := range invs {
		if prev, dup := seen[inv.ID]; dup {
			t.Errorf("%s declared twice: %s and %s", inv.ID, prev, inv.Source)
		}
		seen[inv.ID] = inv.Source
	}
	// The index lists INV-1 with a link, and the run-level section declares it
	// with a heading. Only the heading is the declaration.
	if got := find(t, invs, "INV-1").Tier; got != TierH3 {
		t.Errorf("INV-1 tier = %q, want %q (the heading, not the index row)", got, TierH3)
	}
}

func TestParseFile_HeadingEntry(t *testing.T) {
	inv := find(t, parseBLIS(t), "INV-7")
	if inv.Title != "Signal Freshness Hierarchy" {
		t.Errorf("Title = %q", inv.Title)
	}
	if inv.Tier != TierH4 {
		t.Errorf("Tier = %q, want %q", inv.Tier, TierH4)
	}
	if !strings.HasPrefix(inv.Statement, "Routing snapshot signals have tiered freshness") {
		t.Errorf("Statement = %q", inv.Statement)
	}
	if inv.Source != blisFixture+":258" {
		t.Errorf("Source = %q, want %s:258", inv.Source, blisFixture)
	}
	if inv.Scope != blisFixture {
		t.Errorf("Scope = %q, want %q", inv.Scope, blisFixture)
	}
}

// TestParseFile_TableEntry_EscapedPipes covers the second shape. INV-L2's
// statement contains "\|resident adapters\|", so splitting the row on every
// pipe truncates the statement mid-sentence.
func TestParseFile_TableEntry_EscapedPipes(t *testing.T) {
	inv := find(t, parseBLIS(t), "INV-L2")
	want := "Capacity bound: for every instance at every point in the run, `|resident adapters| <= configured capacity`."
	if inv.Statement != want {
		t.Errorf("Statement =\n  %q\nwant\n  %q", inv.Statement, want)
	}
	if inv.Tier != TierTable {
		t.Errorf("Tier = %q, want %q", inv.Tier, TierTable)
	}
}

// TestParseFile_NonINVPrefix: the registry declares NS-6, numbered by a
// different design note. Hard-coding an "INV" prefix silently drops it, and the
// registry's own resolution rule is about resolvability, not about the prefix.
func TestParseFile_NonINVPrefix(t *testing.T) {
	inv := find(t, parseBLIS(t), "NS-6")
	if inv.Title != "Catalog is Authoritative and Read-Only" {
		t.Errorf("Title = %q", inv.Title)
	}
}

func TestParseMarkdown_SkipsNonEntryHeadingsAndFences(t *testing.T) {
	src := []byte(strings.Join([]string{
		"## Subsystem invariants",
		"### KV cache",
		"#### INV-4: KV Cache Conservation",
		"",
		"**Statement:** Blocks are conserved.",
		"",
		"Re-derive with:",
		"",
		"```markdown",
		"#### INV-999: Not A Real Entry",
		"| **INV-998** | not a real entry | — | 0 |",
		"```",
		"",
	}, "\n"))

	invs, err := ParseMarkdown(src, "docs/invariants.md", "docs/invariants.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 1 || invs[0].ID != "INV-4" {
		t.Fatalf("got %+v, want only INV-4", invs)
	}
	if invs[0].Statement != "Blocks are conserved." {
		t.Errorf("Statement = %q", invs[0].Statement)
	}
}

func TestParseMarkdown_DuplicateIDIsAnError(t *testing.T) {
	src := []byte("### INV-1: First\n\n### INV-1: Second\n")
	_, err := ParseMarkdown(src, "s", "s")
	if err == nil {
		t.Fatal("want an error for an ID declared twice in one scope")
	}
	if !strings.Contains(err.Error(), "INV-1") {
		t.Errorf("error should name the ID: %v", err)
	}
}

// TestParseMarkdown_StatementIsScopedToItsEntry: an entry with no
// "**Statement:**" line must not inherit the next entry's.
func TestParseMarkdown_StatementIsScopedToItsEntry(t *testing.T) {
	src := []byte("### INV-1: No Statement Here\n\nSome prose.\n\n### INV-2: Has One\n\n**Statement:** Mine.\n")
	invs, err := ParseMarkdown(src, "s", "s")
	if err != nil {
		t.Fatal(err)
	}
	if invs[0].Statement != "" {
		t.Errorf("INV-1 Statement = %q, want empty", invs[0].Statement)
	}
	if invs[1].Statement != "Mine." {
		t.Errorf("INV-2 Statement = %q", invs[1].Statement)
	}
}

// TestParseFile_NaturalOrder: numeric segments compare as numbers, so a reader
// scanning a report for INV-2 does not find it after INV-13 and conclude the
// list is truncated.
func TestParseFile_NaturalOrder(t *testing.T) {
	invs := parseBLIS(t)
	var got []string
	for _, inv := range invs[:6] {
		got = append(got, inv.ID)
	}
	want := []string{"INV-1", "INV-2", "INV-3", "INV-4", "INV-5", "INV-6"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("first six = %v, want %v", got, want)
	}
	// INV-PD-6 immediately precedes INV-PD-6b, and numbers precede letters.
	if !lessID("INV-PD-6", "INV-PD-6b") || !lessID("INV-13", "INV-A") || !lessID("INV-9", "INV-10") {
		t.Error("lessID ordering is wrong")
	}
}

// TestParseFile_Deterministic: entries come back in a stable order, so a caller
// rendering them never depends on document order or map iteration.
func TestParseFile_Deterministic(t *testing.T) {
	a, b := parseBLIS(t), parseBLIS(t)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("entry %d differs between parses", i)
		}
		if i > 0 && !lessID(a[i-1].ID, a[i].ID) {
			t.Fatalf("not sorted by ID: %q then %q", a[i-1].ID, a[i].ID)
		}
	}
}

// TestParseFile_FenceOutOfStepIsAnError is the highest-consequence silent
// failure the fence toggle allowed. Deleting one "```bash" line from BLIS's real
// registry used to yield 9 well-formed entries and a nil error: the mask
// re-balanced one fence later, so 25 entries — every PD-disaggregation and pool
// invariant among them — were read as code. A PR touching
// sim/cluster/pd_events.go would have been reported as risking nothing.
func TestParseFile_FenceOutOfStepIsAnError(t *testing.T) {
	b, err := os.ReadFile(blisFixture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	opener := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "```bash") {
			opener = i
			break
		}
	}
	if opener < 0 {
		t.Fatal("fixture no longer contains a ```bash fence")
	}
	maimed := append(append([]string{}, lines[:opener]...), lines[opener+1:]...)

	invs, err := ParseMarkdown([]byte(strings.Join(maimed, "\n")), "s", "s")
	if err == nil {
		t.Fatalf("want an error, got %d entries", len(invs))
	}
	if !strings.Contains(err.Error(), "out of step") {
		t.Errorf("error should name the cause: %v", err)
	}
}

// TestParseMarkdown_NestedAndTildeFences: a fence closes only on the character
// and length it opened with. A naive toggle both invented INV-998 from inside a
// four-backtick example and dropped the real INV-2 after it.
func TestParseMarkdown_NestedAndTildeFences(t *testing.T) {
	for name, src := range map[string]string{
		"nested backticks": "### INV-1: Real\n\n````markdown\n#### INV-998: Example\n```\n#### INV-997: Example\n```\n````\n\n### INV-2: Also Real\n",
		"tilde fence":      "### INV-1: Real\n\n~~~markdown\n#### INV-999: Example\n~~~\n\n### INV-2: Also Real\n",
	} {
		invs, err := ParseMarkdown([]byte(src), "s", "s")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var ids []string
		for _, inv := range invs {
			ids = append(ids, inv.ID)
		}
		if !reflect.DeepEqual(ids, []string{"INV-1", "INV-2"}) {
			t.Errorf("%s: got %v, want [INV-1 INV-2]", name, ids)
		}
	}
}

// TestParseMarkdown_NoEntriesIsAnError: a truncated fetch, a path pointing at
// the wrong markdown file, or a registry whose heading convention drifted must
// not read as "this repository declares no invariants".
func TestParseMarkdown_NoEntriesIsAnError(t *testing.T) {
	for _, src := range []string{"", "# BLIS System Invariants\n\nnothing here yet\n", "<html><body>404</body></html>"} {
		_, err := ParseMarkdown([]byte(src), "s", "s")
		if !errors.Is(err, ErrNoEntries) {
			t.Errorf("ParseMarkdown(%q) error = %v, want ErrNoEntries", src, err)
		}
	}
}

// TestParseMarkdown_NearMissHeadingIsAnError: every one of these used to parse
// to zero entries with no error, so a registry could lose an entry to a typo
// and still report cleanly.
func TestParseMarkdown_NearMissHeadingIsAnError(t *testing.T) {
	for _, heading := range []string{
		"### INV-1 — Request Conservation",
		"### `INV-1`: Request Conservation",
		"### INV-1:Request Conservation",
		"### INV-1:",
		"### INV-1",
	} {
		src := "### INV-2: Real\n\n**Statement:** Real.\n\n" + heading + "\n"
		_, err := ParseMarkdown([]byte(src), "s", "s")
		if err == nil {
			t.Errorf("%q parsed without error", heading)
			continue
		}
		if !strings.Contains(err.Error(), "INV-1") {
			t.Errorf("%q: error should name the ID: %v", heading, err)
		}
	}
}

// TestParseMarkdown_IDGrammarRejectsProse: an ID's segments are capitals and
// digits, not words. Otherwise an ordinary heading or a group-label row becomes
// a declared invariant and inflates the UNLINKED count the feature exists to
// report.
func TestParseMarkdown_IDGrammarRejectsProse(t *testing.T) {
	src := "### INV-1: Real\n\n**Statement:** Real.\n\n" +
		"#### KV-Cache: Terminology\n\n### PD-Disaggregation: Overview\n\n" +
		"| Group | Meaning |\n|---|---|\n| **Run-Level** | whole-run property |\n"
	invs, err := ParseMarkdown([]byte(src), "s", "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 1 || invs[0].ID != "INV-1" {
		var ids []string
		for _, inv := range invs {
			ids = append(ids, inv.ID)
		}
		t.Errorf("got %v, want only [INV-1]", ids)
	}
}

// TestParseMarkdown_StatementDoesNotBleedPastItsBlock: a "**Statement:**" under
// a table, or after a horizontal rule, belongs to what follows the rule — not to
// the heading above it.
func TestParseMarkdown_StatementDoesNotBleedPastItsBlock(t *testing.T) {
	src := "### INV-1: No Statement\n\nProse.\n\n---\n\n| ID | Statement |\n|---|---|\n" +
		"| **INV-L1** | table statement | \n\n**Statement:** This belongs to the family below, not INV-1.\n"
	invs, err := ParseMarkdown([]byte(src), "s", "s")
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, invs, "INV-1").Statement; got != "" {
		t.Errorf("INV-1 Statement = %q, want empty", got)
	}
	if got := find(t, invs, "INV-L1").Statement; got != "table statement" {
		t.Errorf("INV-L1 Statement = %q", got)
	}
}

// TestParseFile_AnyHeadingLevel: the level is recorded, not filtered. Accepting
// only ### and #### would drop an entry the day a registry promotes a section.
func TestParseFile_AnyHeadingLevel(t *testing.T) {
	src := "# INV-1: Title\n\n## INV-2: Title\n\n##### INV-3: Title\n\n###### INV-4: Title\n"
	invs, err := ParseMarkdown([]byte(src), "s", "s")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"h1", "h2", "h5", "h6"}
	for i, inv := range invs {
		if inv.Tier != want[i] {
			t.Errorf("%s Tier = %q, want %q", inv.ID, inv.Tier, want[i])
		}
	}
}

func TestParseMarkdown_UnterminatedFenceIsAnError(t *testing.T) {
	src := "### INV-1: Real\n\n**Statement:** Real.\n\n```go\ncode\n\n### INV-2: Lost\n"
	_, err := ParseMarkdown([]byte(src), "s", "s")
	if err == nil {
		t.Fatal("want an error for a fence left open at EOF")
	}
	if !strings.Contains(err.Error(), "unterminated code fence") {
		t.Errorf("error should name the cause: %v", err)
	}
}
