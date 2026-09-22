// Package invariant reads a repository's declared invariant registry and links
// each entry to the code and tests that cite its ID. It follows IDs that a repo
// already writes into its comments; it never infers an invariant from code
// structure, and it runs no model.
//
// This is deliberately not graph.Invariant. That type is an extracted test
// function; an entry here is prose with an ID. The two share a word and nothing
// else.
package invariant

import (
	"encoding/json"
	"sort"
)

// Invariant is one entry in a declared registry.
type Invariant struct {
	ID        string `json:"id"`        // "INV-6", "INV-PD-2", "NS-6"
	Scope     string `json:"scope"`     // namespace the ID is unique within
	Title     string `json:"title"`     //
	Statement string `json:"statement"` //
	Tier      string `json:"tier"`      // the registry's own grouping shape, uninterpreted
	Source    string `json:"source"`    // file:line where declared
}

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

// CitationSite is one occurrence of an invariant ID in source, attached to the
// smallest enclosing scope archon can name. Start and End are 1-based line
// numbers bounding that scope, inclusive; the review side treats a change that
// overlaps [Start,End] as touching the citation, which is what moves matching
// from "the file mentions the ID somewhere" to "a changed line is in the thing
// the ID documents".
//
// Scope records which granularity the attachment reached, so a report can say
// which fallback applied:
//   - "func": the citation sits in a FuncDecl or its doc comment; Func names it.
//   - "decl": no enclosing function, but a top-level declaration (const/var/type)
//     block encloses it; Func is "".
//   - "file": neither applied — a file-header or package-doc comment — or the file
//     did not parse. The whole file is the scope, so any change to it counts, which
//     is the pre-function-granularity behaviour and keeps such citations from
//     silently vanishing from the report.
//
// Consumers switch on Scope, never on Func being empty: Func is "" for both decl
// and file scopes, so it is not a discriminator.
type CitationSite struct {
	File  string
	Line  int    // 1-based line of the citation itself
	Func  string // enclosing function name, "" unless Scope == "func"
	Start int    // 1-based first line of the enclosing scope, inclusive
	End   int    // 1-based last line of the enclosing scope, inclusive
	Scope string // "func" | "decl" | "file"
}

// Link is what the code and tests say about one invariant.
type Link struct {
	Invariant Invariant `json:"invariant"`
	CodeFiles []string  `json:"code_files,omitempty"` // non-test files citing the ID
	TestFiles []string  `json:"test_files,omitempty"` // _test.go files citing the ID
	// NamedTests holds "<repo-relative file>:<FuncName>" entries. The separator
	// is the FINAL colon: a path may contain one, a Go function name may not.
	NamedTests []string `json:"named_tests,omitempty"`
	Citations  int      `json:"citations"` // total occurrences of the ID across both file sets

	// CitationSites records where each citation sits and what scope encloses it.
	// It is json:"-" on purpose: Link.MarshalJSON embeds every other field via a
	// type alias, so serialising this would move the pinned `archon invariants`
	// output. The review side reads it in-process; review.json carries only the
	// derived per-invariant counts, not the sites.
	CitationSites []CitationSite `json:"-"`
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

// MarshalJSON emits the derived status alongside the fields, so a --json
// consumer sees the same verdict as the table without recomputing it.
func (l Link) MarshalJSON() ([]byte, error) {
	type link Link // shed the method, so this does not recurse
	return json.Marshal(struct {
		link
		Status Status `json:"status"`
	}{link(l), l.Status()})
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
