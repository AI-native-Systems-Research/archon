package invariant

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// idPattern is the ID grammar: an uppercase prefix followed by at least one
// hyphenated segment. The prefix is required rather than assumed to be "INV" —
// BLIS declares NS-6 in the same registry, numbered by a different design note.
const idPattern = `[A-Z][A-Z0-9]*(?:-[A-Za-z0-9]+)+`

var (
	// A heading entry: "### INV-1: Request Conservation" at ### or ####. BLIS
	// uses both levels to express tiering, so a parser that only accepts one
	// silently drops two thirds of the registry.
	headingEntryRe = regexp.MustCompile(`^(#{3,4}) (` + idPattern + `): +(\S.*)$`)

	// A table entry: "| **INV-L1** | statement | ... |". The bold first cell is
	// what separates an entry from an index row like
	// "| [INV-1](#inv-1-request-conservation) Request conservation | ... |" —
	// accepting a bare ID here would declare every indexed invariant twice.
	tableEntryRe = regexp.MustCompile(`^\| *\*\*(` + idPattern + `)\*\* *\|(.*)$`)

	statementRe = regexp.MustCompile(`^\*\*Statement:\*\* *(.*)$`)
	headingRe   = regexp.MustCompile(`^#{1,6} `)
	fenceRe     = regexp.MustCompile("^\\s*```")
)

// ParseFile reads a registry file. The scope is the path as given: IDs are only
// unique within it.
func ParseFile(path string) ([]Invariant, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	return ParseMarkdown(b, path, path)
}

// ParseMarkdown parses registry markdown. scope namespaces the IDs; source
// names the file for the Source field.
func ParseMarkdown(src []byte, scope, source string) ([]Invariant, error) {
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")

	var out []Invariant
	declaredAt := map[string]string{}
	add := func(inv Invariant) error {
		if prev, dup := declaredAt[inv.ID]; dup {
			return fmt.Errorf("duplicate invariant %s in scope %s: declared at %s and %s", inv.ID, scope, prev, inv.Source)
		}
		declaredAt[inv.ID] = inv.Source
		out = append(out, inv)
		return nil
	}

	inFence := false
	for i, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		if m := headingEntryRe.FindStringSubmatch(line); m != nil {
			tier := TierH3
			if len(m[1]) == 4 {
				tier = TierH4
			}
			if err := add(Invariant{
				ID:        m[2],
				Scope:     scope,
				Title:     strings.TrimSpace(m[3]),
				Statement: statementAfter(lines, i+1),
				Tier:      tier,
				Source:    fmt.Sprintf("%s:%d", source, i+1),
			}); err != nil {
				return nil, err
			}
			continue
		}

		if m := tableEntryRe.FindStringSubmatch(line); m != nil {
			// Title is left empty: a table row carries a statement, and
			// splitting a title out of it would be archon inventing structure
			// the registry did not declare.
			cells := splitCells(m[2])
			var stmt string
			if len(cells) > 0 {
				stmt = cells[0]
			}
			if err := add(Invariant{
				ID:        m[1],
				Scope:     scope,
				Statement: stmt,
				Tier:      TierTable,
				Source:    fmt.Sprintf("%s:%d", source, i+1),
			}); err != nil {
				return nil, err
			}
		}
	}

	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

// statementAfter returns the "**Statement:**" paragraph following a heading, or
// "" if the entry declares none before the next heading.
func statementAfter(lines []string, from int) string {
	inFence := false
	for i := from; i < len(lines); i++ {
		line := lines[i]
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if headingRe.MatchString(line) {
			return ""
		}
		m := statementRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		para := []string{strings.TrimSpace(m[1])}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" || headingRe.MatchString(lines[j]) {
				break
			}
			para = append(para, strings.TrimSpace(lines[j]))
		}
		return strings.TrimSpace(strings.Join(para, " "))
	}
	return ""
}

// splitCells splits a markdown table row on unescaped pipes and unescapes the
// rest. BLIS's LoRA rows contain `\|resident adapters\| <= capacity`, so a
// naive strings.Split shreds the statement mid-sentence.
func splitCells(row string) []string {
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(row); i++ {
		switch {
		case row[i] == '\\' && i+1 < len(row) && row[i+1] == '|':
			cur.WriteByte('|')
			i++
		case row[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(row[i])
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		cells = append(cells, s)
	}
	return cells
}
