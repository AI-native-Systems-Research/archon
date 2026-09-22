package review

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/invariant"
)

// registrySchemaVersion versions *this* shape, in *this* document. It is
// deliberately not invariant.SchemaVersion: that versions the `archon invariants`
// JSON, and borrowing it would mean a bump there silently bumping this, while a
// change here could not bump without lying about that one.
//
// v2: `rows` changed meaning from "the rendered table" to "every declared
// invariant this change touched", of which the table was a filtered subset.
//
// v3: matching moved from file granularity to function granularity (#77). The
// `shown` flag and the proportion threshold it recorded are gone — every touched
// row now renders — and the two count columns changed meaning and name, from
// `citingFilesTouched`/`citingFilesTotal` to `citingFunctionsTouched`/
// `citingFunctionsTotal`. A v2 consumer reading the old keys, or keying off
// `shown`, would silently read nothing; the bump is what makes that a visible
// break rather than a quiet one.
const registrySchemaVersion = 3

// LineRange is a new-side line span a change touches, inclusive on both ends. It
// is how the review side learns *where* in a file a change landed, which is what
// lets a citation-bearing function be judged touched or not rather than the whole
// file counting because it mentions an ID somewhere.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// RegistryRow is one declared invariant this change has a reason to mention:
// either a changed line landed in a function (or fallback scope) citing it, a
// test named for it sits in a touched file, or the change deleted a file that
// cited it.
//
// CitingFunctions* counts the citation-bearing *scopes* of this ID — functions,
// or the declaration-block / whole-file scopes a citation falls back to — not the
// repo-wide file count in the section header, and not files: a function is the
// unit matching now works at.
type RegistryRow struct {
	ID                     string           `json:"id"`
	Status                 invariant.Status `json:"status"`
	CitingFunctionsTouched int              `json:"citingFunctionsTouched"`
	CitingFunctionsTotal   int              `json:"citingFunctionsTotal"`

	// AnchorsRemoved counts files the change deletes that cited this ID at the
	// base commit. Without it, deleting an invariant's only anchor renders
	// identically to a whitespace-only commit: the totals are read at head, where
	// the deleted file no longer appears, so it can never be "touched".
	AnchorsRemoved int `json:"anchorsRemoved,omitempty"`
	// NamedTestsInTouchedFiles counts named tests whose *file* the change
	// touched. It is not "tests that changed": the named-test link data carries
	// file:func, not line ranges, so a hunk elsewhere in the same file counts
	// here. This column stays file-level on purpose — #77 moved the citation
	// column to function scope, not this one — and is labelled to match.
	NamedTestsInTouchedFiles int `json:"namedTestsInTouchedFiles"`
	NamedTestsTotal          int `json:"namedTestsTotal"`
}

// RegistrySection is the advisory declared-invariant report.
//
// It is deliberately not the same thing as the "Invariants touched (guarded
// promises)" section, which renders delta.InvariantChange — extracted test
// functions. Those two share a word and nothing else, so this one names the
// registry in its heading and keeps its numbers out of Counts, where
// Counts.Invariants is already the other concept and changing its meaning would
// break any consumer reading it.
//
// No count is stored. Every total is derived from the linked registry, for the
// reason invariant.Result gives for doing the same: a stored copy can contradict
// what it summarises, and "32 of 3 anchored" is worse output than an error.
// Rows holds only the invariants worth mentioning, so the totals could not be
// derived from Rows alone — hence the registry is held rather than discarded.
//
// Wire names are camelCase to match the rest of review.json rather than the
// snake_case of invariant.Result: one document should not mix both.
type RegistrySection struct {
	reg  *invariant.Result
	Rows []RegistryRow
}

// Registry is the path the report was built from, as the caller asked for it.
func (s *RegistrySection) Registry() string { return s.reg.Registry }

// Commit is the commit the registry and the code were read at, if pinned.
func (s *RegistrySection) Commit() string { return s.reg.Commit }

// Declared is how many invariants the registry declares.
func (s *RegistrySection) Declared() int { return len(s.reg.Links) }

// Anchored is how many have anything at all behind them.
func (s *RegistrySection) Anchored() int { return s.reg.Anchored() }

// Totals counts the declared invariants by status.
func (s *RegistrySection) Totals() invariant.Totals { return s.reg.Totals() }

// FilesScanned is how many Go files the link scan read. It is the denominator
// behind every status, and unrelated to a row's CitingFunctionsTotal.
func (s *RegistrySection) FilesScanned() int { return s.reg.FilesScanned }

// MarshalJSON emits the derived counts, so a consumer cannot be handed a summary
// that disagrees with the registry it came from.
func (s *RegistrySection) MarshalJSON() ([]byte, error) {
	t := s.Totals()
	return json.Marshal(struct {
		SchemaVersion int           `json:"schemaVersion"`
		Registry      string        `json:"registry"`
		Commit        string        `json:"commit,omitempty"`
		Declared      int           `json:"declared"`
		Anchored      int           `json:"anchored"`
		Linked        int           `json:"linked"`
		TestOnly      int           `json:"testOnly"`
		Unlinked      int           `json:"unlinked"`
		FilesScanned  int           `json:"filesScanned"`
		Rows          []RegistryRow `json:"rows"`
	}{
		SchemaVersion: registrySchemaVersion,
		Registry:      s.Registry(),
		Commit:        s.Commit(),
		Declared:      s.Declared(),
		Anchored:      s.Anchored(),
		Linked:        t.Linked,
		TestOnly:      t.TestOnly,
		Unlinked:      t.Unlinked,
		FilesScanned:  s.FilesScanned(),
		Rows:          s.Rows,
	})
}

// scopeKey identifies one citation-bearing scope: a file plus a line span. Two
// citations of the same ID in the same function share a key and count once, so
// the numerator and denominator are functions (and fallback scopes), not raw
// citations.
type scopeKey struct {
	file       string
	start, end int
}

// scopeTouched reports whether a change reached a citation's scope. A whole-file
// scope — the fallback for a header comment or an unparseable file — counts when
// the file was changed at all, which is the pre-#77 behaviour that keeps such
// citations from vanishing. A function or declaration scope counts only when a
// changed line range overlaps its span.
func scopeTouched(s invariant.CitationSite, changed map[string]bool, ranges map[string][]LineRange) bool {
	if s.Scope == invariant.ScopeFile {
		return changed[s.File]
	}
	for _, r := range ranges[s.File] {
		if r.Start <= s.End && s.Start <= r.End {
			return true
		}
	}
	return false
}

// buildRegistrySection reports which declared invariants this change exposes.
//
// A nil registry yields a nil section, which is how "no registry found" stays
// byte-identical to the output before this existed.
func buildRegistrySection(reg *invariant.Result, changedFiles []string, changedRanges map[string][]LineRange, removedAnchors map[string][]string) *RegistrySection {
	if reg == nil {
		return nil
	}
	changed := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changed[f] = true
	}

	sec := &RegistrySection{reg: reg, Rows: []RegistryRow{}}
	for _, l := range reg.Links {
		// Count citation-bearing scopes, de-duplicated to distinct spans, and how
		// many of them a changed line reached. A function cited three times is one
		// scope; a citation that fell back to whole-file scope is one scope too.
		total := map[scopeKey]bool{}
		touchedScopes := map[scopeKey]bool{}
		for _, s := range l.CitationSites {
			k := scopeKey{s.File, s.Start, s.End}
			total[k] = true
			if scopeTouched(s, changed, changedRanges) {
				touchedScopes[k] = true
			}
		}
		touched := len(touchedScopes)
		totalScopes := len(total)

		namedTouched := 0
		for _, n := range l.NamedTests {
			// NamedTests entries are "<file>:<FuncName>"; the separator is the
			// final colon, since a Go function name cannot contain one.
			if i := strings.LastIndex(n, ":"); i > 0 && changed[n[:i]] {
				namedTouched++
			}
		}
		removed := len(removedAnchors[l.Invariant.ID])
		if touched == 0 && namedTouched == 0 && removed == 0 {
			// No changed line reached a citing function, no named test's file was
			// touched, and no citing file was deleted — nothing to act on. Standing
			// UNLINKED belongs to the audit surface (archon invariants), not here.
			continue
		}
		sec.Rows = append(sec.Rows, RegistryRow{
			ID:                       l.Invariant.ID,
			Status:                   l.Status(),
			CitingFunctionsTouched:   touched,
			CitingFunctionsTotal:     totalScopes,
			AnchorsRemoved:           removed,
			NamedTestsInTouchedFiles: namedTouched,
			NamedTestsTotal:          len(l.NamedTests),
		})
	}

	// Highest proportion first: touched/citing functions is the meaningful
	// quantity, not the raw count. With matching now function-scoped every row is
	// real (there is no threshold to clear), so the ranking's only job is to put
	// the most-affected invariant first. Raw counts stay in the output as the
	// evidence, but the ordering is by proportion.
	sort.SliceStable(sec.Rows, func(i, j int) bool {
		a, b := sec.Rows[i], sec.Rows[j]
		if a.AnchorsRemoved != b.AnchorsRemoved {
			// A removed anchor outranks any amount of touching: it is the change
			// most likely to leave a declared promise unguarded.
			return a.AnchorsRemoved > b.AnchorsRemoved
		}
		// Proportion touched/total, descending, by cross-multiplication so the
		// ranking never divides and never rounds two near-equal ratios together.
		// A row shown only by a touched named test has touched==0 and so sorts
		// below any row a change actually reached.
		lhs := a.CitingFunctionsTouched * b.CitingFunctionsTotal
		rhs := b.CitingFunctionsTouched * a.CitingFunctionsTotal
		if lhs != rhs {
			return lhs > rhs
		}
		if a.CitingFunctionsTouched != b.CitingFunctionsTouched {
			return a.CitingFunctionsTouched > b.CitingFunctionsTouched
		}
		return a.ID < b.ID
	})
	return sec
}

// writeRegistrySection renders the section, or nothing at all when there is no
// registry.
func writeRegistrySection(b *strings.Builder, sec *RegistrySection) {
	if sec == nil {
		return
	}
	b.WriteString("### Declared invariants — registry\n\n")

	// The path is named on purpose. Auto-discovery's one bad failure mode is the
	// repo renaming the doc: the section disappears and every later review looks
	// normal. Naming it turns a silent disable into something a reader notices.
	at := ""
	if sec.Commit() != "" {
		at = fmt.Sprintf(" at `%s`", sec.Commit())
	}
	t := sec.Totals()
	fmt.Fprintf(b, "Registry: `%s`%s — %d declared, %d of %d anchored (%d LINKED, %d TEST ONLY, %d UNLINKED), %d Go files scanned.\n\n",
		sec.Registry(), at, sec.Declared(), sec.Anchored(), sec.Declared(),
		t.Linked, t.TestOnly, t.Unlinked, sec.FilesScanned())

	if sec.Anchored() == 0 {
		// The most useful thing archon can say to a repo that has written a
		// registry and cited none of it, so it is stated rather than implied.
		fmt.Fprintf(b, "**0 of %d anchored** — every declared invariant above exists only in the document declaring it.\n\n", sec.Declared())
	}

	if len(sec.Rows) == 0 {
		b.WriteString("This change touched no function citing a declared invariant.\n\n")
		return
	}

	// Every row renders: with function-scoped matching there is no diffuse tail to
	// filter, so #74's proportion threshold and its footer are gone (#77).
	b.WriteString("| ID | Status | citing functions touched | named tests in touched files |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, r := range sec.Rows {
		if r.CitingFunctionsTotal == 0 && r.NamedTestsTotal == 0 {
			fmt.Fprintf(b, "| `%s` | %s | — | — |\n", r.ID, r.Status)
			continue
		}
		fmt.Fprintf(b, "| `%s` | %s | %d of %d | %d of %d |\n",
			r.ID, r.Status, r.CitingFunctionsTouched, r.CitingFunctionsTotal,
			r.NamedTestsInTouchedFiles, r.NamedTestsTotal)
	}
	b.WriteString("\n")

	var removed []string
	for _, r := range sec.Rows {
		if r.AnchorsRemoved > 0 {
			removed = append(removed, fmt.Sprintf("`%s` (%d)", r.ID, r.AnchorsRemoved))
		}
	}
	if len(removed) > 0 {
		fmt.Fprintf(b, "**This change deletes files that cited %s.** Those files are gone at head, so they count in no column above.\n\n",
			strings.Join(removed, ", "))
	}
}
