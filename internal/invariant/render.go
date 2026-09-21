package invariant

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Result is one invariants run: which registry was read, at which commit if any,
// how many Go files backed the verdict, and what the repository has behind each
// entry.
//
// Totals are not a field. They are derived from Links wherever they are needed,
// including in the JSON, because a stored copy can contradict the rows it
// summarises: "32 of 3 anchored" under a three-row table is worse output than an
// error, and nothing can detect it after the fact.
//
// Every path in here must be repo-relative; Validate enforces it, because an
// absolute path is what makes a report reproducible on exactly one machine.
type Result struct {
	Registry     string `json:"registry"`
	Commit       string `json:"commit,omitempty"`
	FilesScanned int    `json:"files_scanned"`
	Links        []Link `json:"links"`
}

// SchemaVersion is emitted with every JSON document. It exists so a consumer can
// refuse a shape it does not understand; adding it after the format is published
// would itself be the breaking change it guards against.
const SchemaVersion = 1

// Validate reports whether this result can be published.
//
// Both output paths call it, which is the point: the absolute-path rule used to
// be checked inside Render only, so --json marshalled straight past it — and the
// machine-specific path would land in a byte-compared golden and in whatever
// consumes the JSON.
func (r Result) Validate() error {
	if abs(r.Registry) {
		return fmt.Errorf("registry %q is absolute; the report would reproduce on one machine only", r.Registry)
	}
	// A nil slice marshals as null, which makes "the registry declared nothing"
	// and "no registry was read" the same document.
	if r.Links == nil {
		return fmt.Errorf("no links: a result with no invariants is not distinguishable from one that was never populated")
	}
	for _, l := range r.Links {
		for _, p := range append(append(append(
			[]string{l.Invariant.Source, l.Invariant.Scope},
			l.CodeFiles...), l.TestFiles...), l.NamedTests...) {
			if abs(p) {
				return fmt.Errorf("%s carries an absolute path %q", l.Invariant.ID, p)
			}
		}
	}
	return nil
}

func abs(p string) bool { return strings.HasPrefix(p, "/") }

// Totals counts this result's links by status.
func (r Result) Totals() Totals { return Count(r.Links) }

// Anchored counts invariants with anything at all behind them, in code or in
// tests.
func (r Result) Anchored() int {
	t := r.Totals()
	return t.Linked + t.TestOnly
}

// MarshalJSON emits the schema version and the derived totals, so a consumer
// reads the same numbers the table shows without recomputing them and without
// being able to disagree.
func (r Result) MarshalJSON() ([]byte, error) {
	type result Result // shed the method, so this does not recurse
	return json.Marshal(struct {
		SchemaVersion int `json:"schema_version"`
		result
		Totals Totals `json:"totals"`
	}{SchemaVersion, result(r), r.Totals()})
}

// Render writes the link table and the totals.
//
// The output is diffed byte-for-byte by demo/run-all.sh, so no line carries an
// absolute path or trailing whitespace: demo/flow1-pr-review's goldens embed one
// absolute repo path and reproduce from a single checkout only. A write error is
// returned rather than dropped — a half-written table that exits 0 is the same
// class of lie as a wrong number.
func Render(w io.Writer, res Result) error {
	if err := res.Validate(); err != nil {
		return err
	}

	var werr error
	p := func(format string, args ...any) {
		if werr != nil {
			return
		}
		_, werr = fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf(format, args...), " \t"))
	}

	totals := res.Totals()

	p("DECLARED INVARIANTS")
	p("  registry: %s (%d declared)", res.Registry, len(res.Links))
	if res.Commit != "" {
		p("  commit:   %s", res.Commit)
	}
	p("  scanned:  %d Go files", res.FilesScanned)
	p("")
	p("  %-12s %-9s %5s %5s %6s %6s", "ID", "STATUS", "CODE", "TEST", "CITES", "NAMED")
	for _, l := range res.Links {
		p("  %-12s %-9s %5d %5d %6d %6d", l.Invariant.ID, l.Status(),
			len(l.CodeFiles), len(l.TestFiles), l.Citations, len(l.NamedTests))
	}
	p("")
	p("  %d of %d anchored — %d LINKED, %d TEST ONLY, %d UNLINKED",
		res.Anchored(), len(res.Links), totals.Linked, totals.TestOnly, totals.Unlinked)

	// The names go below the table rather than in a column: an invariant can have
	// a dozen or more, which turns a scannable row into a wrapped paragraph. The
	// count stays in the table so the table alone answers "is anything named for
	// this?".
	named := false
	for _, l := range res.Links {
		if len(l.NamedTests) > 0 {
			named = true
			break
		}
	}
	if !named {
		return werr
	}
	p("")
	p("  tests named for an invariant:")
	for _, l := range res.Links {
		for i, n := range l.NamedTests {
			id := l.Invariant.ID
			if i > 0 {
				id = ""
			}
			p("    %-12s %s", id, n)
		}
	}
	return werr
}
