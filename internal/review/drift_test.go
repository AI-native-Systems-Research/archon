package review

import (
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/plan"
)

func TestSurfaceDriftTableRendersRow(t *testing.T) {
	var b strings.Builder
	writeSurfaceDriftTable(&b, []plan.SurfaceDrift{{
		Package:  "example.com/m/pkg",
		Entity:   "Do",
		Declared: "(s string) string",
		Actual:   "func(parts ...string) string",
	}})
	out := b.String()

	if !strings.Contains(out, "**Surface drift (1)**") {
		t.Errorf("header missing:\n%s", out)
	}
	// Whole row, so column order is pinned too.
	want := "| `pkg` | `Do` | `(s string) string` | `func(parts ...string) string` |"
	if !strings.Contains(out, want) {
		t.Errorf("missing row:\n  %s\n--- got ---\n%s", want, out)
	}
	if !strings.Contains(out, "Does not affect the distance above") {
		t.Errorf("the report-only caveat is missing:\n%s", out)
	}
}

// Signatures are full of "*" and "_". Two pointer types in one row would render
// the span between them as italics and silently mangle the text a reviewer is
// there to compare, so signature cells must be code spans.
func TestSurfaceDriftTableWrapsSignaturesInCodeSpans(t *testing.T) {
	var b strings.Builder
	writeSurfaceDriftTable(&b, []plan.SurfaceDrift{{
		Package:  "example.com/m/pkg",
		Entity:   "Auth",
		Declared: "(t string) (*User, *Session)",
		Actual:   "func(t string) (*User, error)",
	}})
	out := b.String()

	if !strings.Contains(out, "`(t string) (*User, *Session)`") {
		t.Errorf("declared signature not in a code span:\n%s", out)
	}
	if !strings.Contains(out, "`func(t string) (*User, error)`") {
		t.Errorf("actual signature not in a code span:\n%s", out)
	}
}

func TestSurfaceDriftTableOmittedWhenEmpty(t *testing.T) {
	var b strings.Builder
	writeSurfaceDriftTable(&b, nil)
	if b.String() != "" {
		t.Errorf("rendered a section for zero drift: %q", b.String())
	}
}

// Standing drift on a large plan would otherwise crowd out the rest of the bundle.
func TestSurfaceDriftTableCapsRows(t *testing.T) {
	var rows []plan.SurfaceDrift
	for i := 0; i < maxDriftRows+5; i++ {
		rows = append(rows, plan.SurfaceDrift{
			Package: "example.com/m/pkg", Entity: string(rune('A' + i)),
			Declared: "() int", Actual: "func() (int, error)",
		})
	}
	var b strings.Builder
	writeSurfaceDriftTable(&b, rows)
	out := b.String()

	if strings.Count(out, "| `pkg` |") != maxDriftRows {
		t.Errorf("rendered %d rows, want the cap of %d", strings.Count(out, "| `pkg` |"), maxDriftRows)
	}
	if !strings.Contains(out, "and 5 more") {
		t.Errorf("truncation not disclosed:\n%s", out)
	}
	// The header must still state the true total, not the shown count.
	if !strings.Contains(out, "Surface drift (15)") {
		t.Errorf("header shows the capped count instead of the total:\n%s", out)
	}
}

// A raw pipe ends a table cell even inside a code span, and the extractor emits
// them for real: a generic constraint renders as "interface{~int | ~string}".
// Without escaping, GFM splits the row into extra columns and mangles the exact
// text a reviewer is there to compare.
func TestSurfaceDriftTableEscapesPipeInSignature(t *testing.T) {
	var b strings.Builder
	writeSurfaceDriftTable(&b, []plan.SurfaceDrift{{
		Package:  "example.com/m/pkg",
		Entity:   "Constrained",
		Declared: "(x T) T",
		Actual:   "func[T interface{~int | ~string}](x T) (T, error)",
	}})
	out := b.String()

	if strings.Contains(out, "~int | ~string") {
		t.Errorf("unescaped pipe left in a table cell:\n%s", out)
	}
	if !strings.Contains(out, `~int \| ~string`) {
		t.Errorf("pipe not escaped as tableCell does it:\n%s", out)
	}
	// Every rendered row must still have exactly 4 columns.
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "| `pkg`") {
			continue
		}
		if n := strings.Count(line, "|") - strings.Count(line, `\|`); n != 5 {
			t.Errorf("row has %d unescaped pipes, want 5 (4 columns):\n  %s", n, line)
		}
	}
}
