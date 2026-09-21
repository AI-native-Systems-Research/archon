package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/invariant"
)

// RegistryRow is one declared invariant this change has a reason to mention:
// either the change touched a file citing it, or nothing in the repository cites
// it at all.
type RegistryRow struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	FilesTouched int    `json:"filesTouched"`
	FilesTotal   int    `json:"filesTotal"`
	NamedTouched int    `json:"namedTestsTouched"`
	NamedTotal   int    `json:"namedTestsTotal"`
}

// RegistrySection is the advisory declared-invariant report.
//
// It is deliberately not the same thing as the "Invariants touched (guarded
// promises)" section, which renders delta.InvariantChange — extracted test
// functions. Those two share a word and nothing else, so this one names the
// registry in its heading and keeps its numbers out of Counts, where
// Counts.Invariants already means the other concept and CI consumers parse it.
//
// Field names are camelCase to match the rest of review.json, rather than
// embedding invariant.Result or invariant.Totals, whose own JSON is snake_case —
// one document should not mix both. SchemaVersion is carried across so a consumer
// can still tell which shape it is reading.
type RegistrySection struct {
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

	totals := reg.Totals()
	sec := &RegistrySection{
		SchemaVersion: invariant.SchemaVersion,
		Registry:      reg.Registry,
		Commit:        reg.Commit,
		Declared:      len(reg.Links),
		Anchored:      reg.Anchored(),
		Linked:        totals.Linked,
		TestOnly:      totals.TestOnly,
		Unlinked:      totals.Unlinked,
		FilesScanned:  reg.FilesScanned,
		Rows:          []RegistryRow{},
	}

	for _, l := range reg.Links {
		files := append(append([]string{}, l.CodeFiles...), l.TestFiles...)
		touched := 0
		for _, f := range files {
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
			ID:           l.Invariant.ID,
			Status:       string(l.Status()),
			FilesTouched: touched,
			FilesTotal:   len(files),
			NamedTouched: namedTouched,
			NamedTotal:   len(l.NamedTests),
		})
	}

	// Most-exposed first, so the first row is the one worth reading. Ties break
	// on ID so the output is stable.
	sort.SliceStable(sec.Rows, func(i, j int) bool {
		if sec.Rows[i].FilesTouched != sec.Rows[j].FilesTouched {
			return sec.Rows[i].FilesTouched > sec.Rows[j].FilesTouched
		}
		if sec.Rows[i].NamedTouched != sec.Rows[j].NamedTouched {
			return sec.Rows[i].NamedTouched > sec.Rows[j].NamedTouched
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
	if sec.Commit != "" {
		at = fmt.Sprintf(" at `%s`", sec.Commit)
	}
	fmt.Fprintf(b, "Registry: `%s`%s — %d declared, %d of %d anchored (%d LINKED, %d TEST ONLY, %d UNLINKED), %d Go files scanned.\n\n",
		sec.Registry, at, sec.Declared, sec.Anchored, sec.Declared,
		sec.Linked, sec.TestOnly, sec.Unlinked, sec.FilesScanned)

	if sec.Anchored == 0 {
		// The most useful thing archon can say to a repo that has written a
		// registry and cited none of it, so it is stated rather than implied.
		fmt.Fprintf(b, "**0 of %d anchored** — every declared invariant above exists only in the document declaring it.\n\n", sec.Declared)
	}

	if len(sec.Rows) == 0 {
		b.WriteString("This change touched no file citing a declared invariant.\n\n")
		return
	}

	b.WriteString("| ID | Status | files touched | named tests touched |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, r := range sec.Rows {
		if r.FilesTotal == 0 && r.NamedTotal == 0 {
			fmt.Fprintf(b, "| `%s` | %s | — | — |\n", r.ID, r.Status)
			continue
		}
		fmt.Fprintf(b, "| `%s` | %s | %d of %d | %d of %d |\n",
			r.ID, r.Status, r.FilesTouched, r.FilesTotal, r.NamedTouched, r.NamedTotal)
	}
	b.WriteString("\n")

	var unlinked []string
	for _, r := range sec.Rows {
		if r.Status == string(invariant.StatusUnlinked) {
			unlinked = append(unlinked, "`"+r.ID+"`")
		}
	}
	if len(unlinked) > 0 {
		noun := "is declared but cited in no file"
		if len(unlinked) > 1 {
			noun = "are declared but cited in no file"
		}
		fmt.Fprintf(b, "%s %s.\n\n", strings.Join(unlinked, ", "), noun)
	}
}
