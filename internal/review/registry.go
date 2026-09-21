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
const registrySchemaVersion = 1

// RegistryRow is one declared invariant this change has a reason to mention:
// either the change touched a file citing it, or nothing in the repository cites
// it at all.
//
// CitingFiles* counts the files that cite this ID, which is not the repo-wide
// file count in the section header — the two would otherwise both read as
// "files".
type RegistryRow struct {
	ID                 string           `json:"id"`
	Status             invariant.Status `json:"status"`
	CitingFilesTouched int              `json:"citingFilesTouched"`
	CitingFilesTotal   int              `json:"citingFilesTotal"`
	NamedTestsTouched  int              `json:"namedTestsTouched"`
	NamedTestsTotal    int              `json:"namedTestsTotal"`
}

// RegistrySection is the advisory declared-invariant report.
//
// It is deliberately not the same thing as the "Invariants touched (guarded
// promises)" section, which renders delta.InvariantChange — extracted test
// functions. Those two share a word and nothing else, so this one names the
// registry in its heading and keeps its numbers out of Counts, where
// Counts.Invariants already means the other concept and CI consumers parse it.
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
// behind every status, and unrelated to a row's CitingFilesTotal.
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

// buildRegistrySection reports which declared invariants this change exposes.
//
// A nil registry yields a nil section, which is how "no registry found" stays
// byte-identical to the output before this existed.
func buildRegistrySection(reg *invariant.Result, changedFiles []string) *RegistrySection {
	if reg == nil {
		return nil
	}
	changed := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changed[f] = true
	}

	sec := &RegistrySection{reg: reg, Rows: []RegistryRow{}}
	for _, l := range reg.Links {
		// De-duplicated: LinkRepo puts a file in exactly one of these, but a
		// double-counted path would inflate both the numerator and the
		// denominator, which is the wrong-number class rather than a crash.
		files := map[string]bool{}
		for _, f := range append(append([]string{}, l.CodeFiles...), l.TestFiles...) {
			files[f] = true
		}
		touched := 0
		for f := range files {
			if changed[f] {
				touched++
			}
		}
		namedTouched := 0
		for _, n := range l.NamedTests {
			// NamedTests entries are "<file>:<FuncName>"; the separator is the
			// final colon, since a Go function name cannot contain one.
			if i := strings.LastIndex(n, ":"); i > 0 && changed[n[:i]] {
				namedTouched++
			}
		}
		unlinked := l.Status() == invariant.StatusUnlinked
		if touched == 0 && namedTouched == 0 && !unlinked {
			// Anchored and untouched: nothing here for a reviewer to act on.
			continue
		}
		sec.Rows = append(sec.Rows, RegistryRow{
			ID:                 l.Invariant.ID,
			Status:             l.Status(),
			CitingFilesTouched: touched,
			CitingFilesTotal:   len(files),
			NamedTestsTouched:  namedTouched,
			NamedTestsTotal:    len(l.NamedTests),
		})
	}

	// Most-exposed first, so the first row is the one worth reading. Ties break
	// on ID so the output is stable.
	sort.SliceStable(sec.Rows, func(i, j int) bool {
		if sec.Rows[i].CitingFilesTouched != sec.Rows[j].CitingFilesTouched {
			return sec.Rows[i].CitingFilesTouched > sec.Rows[j].CitingFilesTouched
		}
		if sec.Rows[i].NamedTestsTouched != sec.Rows[j].NamedTestsTouched {
			return sec.Rows[i].NamedTestsTouched > sec.Rows[j].NamedTestsTouched
		}
		return sec.Rows[i].ID < sec.Rows[j].ID
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
		b.WriteString("This change touched no file citing a declared invariant.\n\n")
		return
	}

	b.WriteString("| ID | Status | files touched | named tests touched |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, r := range sec.Rows {
		if r.CitingFilesTotal == 0 && r.NamedTestsTotal == 0 {
			fmt.Fprintf(b, "| `%s` | %s | — | — |\n", r.ID, r.Status)
			continue
		}
		fmt.Fprintf(b, "| `%s` | %s | %d of %d | %d of %d |\n",
			r.ID, r.Status, r.CitingFilesTouched, r.CitingFilesTotal,
			r.NamedTestsTouched, r.NamedTestsTotal)
	}
	b.WriteString("\n")

	var unlinked []string
	for _, r := range sec.Rows {
		if r.Status == invariant.StatusUnlinked {
			unlinked = append(unlinked, "`"+r.ID+"`")
		}
	}
	if len(unlinked) > 0 {
		verb := "is declared but cited in no file"
		if len(unlinked) > 1 {
			verb = "are declared but cited in no file"
		}
		fmt.Fprintf(b, "%s %s.\n\n", strings.Join(unlinked, ", "), verb)
	}
}
