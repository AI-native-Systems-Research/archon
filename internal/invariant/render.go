package invariant

import (
	"fmt"
	"io"
	"strings"
)

// Result is one invariants run: which registry was read, at which commit if any,
// and what the repository has behind each entry.
//
// Every path in here is repo-relative, and the registry is recorded as it was
// asked for. A caller that puts an absolute path in either field makes its own
// output reproducible on exactly one machine.
type Result struct {
	Registry string `json:"registry"`
	Commit   string `json:"commit,omitempty"`
	Links    []Link `json:"links"`
	Totals   Totals `json:"totals"`
}

// Anchored counts invariants with anything at all behind them, in code or in
// tests.
func (r Result) Anchored() int { return r.Totals.Linked + r.Totals.TestOnly }

// Render writes the link table and the totals.
//
// The output is diffed byte-for-byte by demo/run-all.sh, so no line carries an
// absolute path or trailing whitespace: demo/flow1-pr-review's goldens embed one
// absolute repo path and reproduce from a single checkout only.
func Render(w io.Writer, res Result) {
	p := func(format string, args ...any) {
		fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf(format, args...), " \t"))
	}

	p("DECLARED INVARIANTS")
	p("  registry: %s (%d declared)", res.Registry, len(res.Links))
	if res.Commit != "" {
		p("  commit:   %s", res.Commit)
	}
	p("")
	p("  %-12s %-9s %5s %5s %6s %6s", "ID", "STATUS", "CODE", "TEST", "CITES", "NAMED")
	for _, l := range res.Links {
		p("  %-12s %-9s %5d %5d %6d %6d", l.Invariant.ID, l.Status(),
			len(l.CodeFiles), len(l.TestFiles), l.Citations, len(l.NamedTests))
	}
	p("")
	p("  %d of %d anchored — %d LINKED, %d TEST ONLY, %d UNLINKED",
		res.Anchored(), len(res.Links), res.Totals.Linked, res.Totals.TestOnly, res.Totals.Unlinked)

	// The names go below the table rather than in a column: one BLIS invariant
	// has fourteen of them, which turns a scannable row into a wrapped paragraph.
	// The count stays in the table so the table alone answers "is anything named
	// for this?".
	named := false
	for _, l := range res.Links {
		if len(l.NamedTests) > 0 {
			named = true
			break
		}
	}
	if !named {
		return
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
}
