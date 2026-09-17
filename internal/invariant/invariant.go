// Package invariant reads a repository's declared invariant registry and links
// each entry to the code and tests that cite its ID. It follows IDs that a repo
// already writes into its comments; it never infers an invariant from code
// structure, and it runs no model.
//
// This is deliberately not graph.Invariant. That type is an extracted test
// function; an entry here is prose with an ID. The two share a word and nothing
// else.
package invariant

import "sort"

// Invariant is one entry in a declared registry.
type Invariant struct {
	ID        string `json:"id"`        // "INV-6", "INV-PD-2", "NS-6"
	Scope     string `json:"scope"`     // namespace the ID is unique within
	Title     string `json:"title"`     //
	Statement string `json:"statement"` //
	Tier      string `json:"tier"`      // the registry's own grouping shape, uninterpreted
	Source    string `json:"source"`    // file:line where declared
}

// Tier labels. They record the shape the entry was declared in and nothing
// more: archon does not decide what "core" or "subsystem" mean for a repo.
const (
	TierH3    = "h3"    // declared as a "### ID: Title" heading
	TierH4    = "h4"    // declared as a "#### ID: Title" heading
	TierTable = "table" // declared as a table row whose first cell is a bold ID
)

// Key identifies an invariant globally. An ID is only unique within its scope:
// BLIS cites BC-1 307 times across four packages meaning something different in
// each, so any comparison or lookup has to carry the scope too.
func (i Invariant) Key() string { return i.Scope + "#" + i.ID }

// Status is what the repository has behind a declared invariant.
type Status string

const (
	// StatusLinked means at least one non-test file cites the ID.
	StatusLinked Status = "LINKED"
	// StatusTestOnly means only tests stand behind the ID: the promise is
	// checked but nothing in production names it.
	StatusTestOnly Status = "TEST ONLY"
	// StatusUnlinked means nothing in the repository names the ID at all. The
	// invariant exists only in the document declaring it.
	StatusUnlinked Status = "UNLINKED"
)

// Link is what the code and tests say about one invariant.
type Link struct {
	Invariant  Invariant `json:"invariant"`
	CodeFiles  []string  `json:"code_files,omitempty"`  // non-test files citing the ID
	TestFiles  []string  `json:"test_files,omitempty"`  // _test.go files citing the ID
	NamedTests []string  `json:"named_tests,omitempty"` // test funcs whose name embeds the ID
	Citations  int       `json:"citations"`             // total occurrences of the ID across both file sets
}

// Status derives from where the ID appears. A test merely named for an
// invariant counts as test evidence: reporting UNLINKED — "nowhere" — for an
// invariant with a test named after it would be exactly the plausible-looking
// wrong output this package exists to avoid.
func (l Link) Status() Status {
	switch {
	case len(l.CodeFiles) > 0:
		return StatusLinked
	case len(l.TestFiles) > 0 || len(l.NamedTests) > 0:
		return StatusTestOnly
	default:
		return StatusUnlinked
	}
}

// Totals counts links by status. Returned by value so a caller reporting
// summary lines never iterates a map to produce output.
type Totals struct {
	Linked   int `json:"linked"`
	TestOnly int `json:"test_only"`
	Unlinked int `json:"unlinked"`
}

// Count tallies links by status.
func Count(links []Link) Totals {
	var t Totals
	for _, l := range links {
		switch l.Status() {
		case StatusLinked:
			t.Linked++
		case StatusTestOnly:
			t.TestOnly++
		default:
			t.Unlinked++
		}
	}
	return t
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
