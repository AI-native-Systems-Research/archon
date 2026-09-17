package invariant

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// ErrNoEntries is returned when a document parses without yielding a single
// entry. It is an error rather than an empty result because the two are not the
// same thing and look identical downstream: a truncated fetch, a path pointing
// at the wrong markdown file, or a registry whose heading convention drifted
// would otherwise report "this repository declares no invariants" — a clean,
// reassuring, false answer.
var ErrNoEntries = errors.New("no invariant entries found")

// idPattern is the ID grammar: an uppercase prefix, then hyphenated segments of
// capitals and digits with at most one trailing lowercase letter — INV-6,
// INV-A2, INV-BC-DP1, INV-PD-6b, NS-6.
//
// The prefix is not assumed to be "INV": BLIS declares NS-6 in the same
// registry, numbered by a different design note, and its resolution rule is
// about resolvability rather than the prefix. But segments cannot be capitalised
// words, or ordinary prose headings become declared invariants — "### KV-Cache:
// Terminology" and a group-label row "| **Run-Level** |" would each inflate the
// UNLINKED count that is this feature's headline number.
const idPattern = `[A-Z][A-Z0-9]*(?:-[A-Z0-9]+[a-z]?)+`

var (
	// A heading entry: "### INV-1: Request Conservation". Any heading level is
	// accepted and reported as the Tier verbatim; BLIS uses ### and #### to
	// express tiering, but pinning the parser to those two levels would drop an
	// entry the day a registry promotes a section or adds a tier — the silent
	// kind of loss this package exists to avoid.
	headingEntryRe = regexp.MustCompile(`^(#{1,6}) (` + idPattern + `): +(\S.*)$`)

	// A table entry: "| **INV-L1** | statement | ... |". The bold first cell is
	// what separates an entry from an index row like
	// "| [INV-1](#inv-1-request-conservation) Request conservation | ... |" —
	// accepting a bare ID here would declare every indexed invariant twice.
	tableEntryRe = regexp.MustCompile(`^\| *\*\*(` + idPattern + `)\*\* *\|(.*)$`)

	statementRe = regexp.MustCompile(`^\*\*Statement:\*\* *(.*)$`)
	fenceRe     = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")

	// A heading whose text opens with a complete ID. If it is not also a
	// well-formed entry — a missing colon, a backticked ID, an em dash, no title
	// — that is an error: silently skipping it is how a registry loses an entry
	// to a typo and still reports cleanly. The trailing boundary makes the ID
	// maximal, so "KV-Cache" is not read as the ID "KV-Ca".
	headingIDRe = regexp.MustCompile("^#{1,6} +`?(" + idPattern + ")`?([^0-9A-Za-z-].*|)$")

	// A statement paragraph belongs to the entry it follows. These end the
	// search so a "**Statement:**" further down the section — after a horizontal
	// rule, or under a table that declares its own entries — is not claimed by
	// the heading above it.
	statementStopRe = regexp.MustCompile(`^(#{1,6} |\||-{3,}$|\*{3,}$|_{3,}$)`)
)

// Tier reports the shape an entry was declared in. It is the registry's own
// grouping, recorded and not interpreted: archon does not decide what "core" or
// "subsystem" mean for a repo.
const (
	TierH3    = "h3"    // declared as a "### ID: Title" heading
	TierH4    = "h4"    // declared as a "#### ID: Title" heading
	TierTable = "table" // declared as a table row whose first cell is a bold ID
)

// ParseFile reads a registry file. The scope is the path as given: IDs are only
// unique within it.
//
// An error means the registry could not be read as declared. It never means
// "this repository declares no invariants" — a caller that collapses the two
// turns a broken registry into a clean report.
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
	fenced, err := fenceMask(lines, source)
	if err != nil {
		return nil, err
	}

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

	for i, line := range lines {
		if fenced[i] {
			continue
		}

		if m := headingEntryRe.FindStringSubmatch(line); m != nil {
			if err := add(Invariant{
				ID:        m[2],
				Scope:     scope,
				Title:     strings.TrimSpace(m[3]),
				Statement: statementAfter(lines, fenced, i+1),
				Tier:      fmt.Sprintf("h%d", len(m[1])),
				Source:    fmt.Sprintf("%s:%d", source, i+1),
			}); err != nil {
				return nil, err
			}
			continue
		}

		if m := headingIDRe.FindStringSubmatch(line); m != nil {
			return nil, fmt.Errorf("%s:%d: heading names %s but is not an entry — expected %q",
				source, i+1, m[1], "#... "+m[1]+": Title")
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

	if len(out) == 0 {
		return nil, fmt.Errorf("%s: %w", source, ErrNoEntries)
	}
	sortByID(out)
	return out, nil
}

// fenceMask marks every line that is part of a fenced code block, so an example
// registry inside a code block is not parsed as a declaration.
//
// A fence closes only on the same character it opened with, at the same length
// or longer, which is what keeps a ``` sample nested in a ```` block — or a ~~~
// block — from inverting the state.
//
// Two shapes are errors rather than a shifted mask, because a fence off by one
// line silently reassigns which half of the document is code. Deleting a single
// "```bash" line from BLIS's registry took 25 of its 34 entries with it, error
// free — the mask simply re-balanced one fence later.
func fenceMask(lines []string, source string) ([]bool, error) {
	fenced := make([]bool, len(lines))
	open, openAt := "", 0
	for i, line := range lines {
		m := fenceRe.FindStringSubmatch(line)
		switch {
		case open == "":
			if m == nil {
				continue
			}
			// An opening fence may carry an info string; a closing one may not.
			open, openAt = m[1], i
			fenced[i] = true
		case m != nil && m[1][0] == open[0] && len(m[1]) >= len(open) && strings.TrimSpace(m[2]) == "":
			open = ""
			fenced[i] = true
		case m != nil && len(m[1]) == len(open) && strings.TrimSpace(m[2]) != "":
			// An info string on a fence of the same width as the one already
			// open: this is an opener, and openers do not nest at equal width.
			// It means the mask is one fence out of step.
			return nil, fmt.Errorf("%s:%d: code fence %q opens an info string while the fence at line %d is still open — the mask is out of step and half the document would be read as code",
				source, i+1, m[1]+strings.TrimSpace(m[2]), openAt+1)
		default:
			fenced[i] = true
		}
	}
	if open != "" {
		return nil, fmt.Errorf("%s:%d: unterminated code fence %q — the rest of the document would be ignored", source, openAt+1, open)
	}
	return fenced, nil
}

// statementAfter returns the "**Statement:**" paragraph following a heading, or
// "" if the entry declares none before the next heading. The paragraph ends at a
// blank line, as it does in markdown.
func statementAfter(lines []string, fenced []bool, from int) string {
	for i := from; i < len(lines); i++ {
		if fenced[i] {
			continue
		}
		if statementStopRe.MatchString(lines[i]) {
			return ""
		}
		m := statementRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		para := []string{strings.TrimSpace(m[1])}
		for j := i + 1; j < len(lines); j++ {
			if fenced[j] || strings.TrimSpace(lines[j]) == "" || statementStopRe.MatchString(lines[j]) {
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

// sortByID orders entries so a reader finds them where they expect: numeric
// segments compare as numbers, so INV-2 precedes INV-10 instead of trailing
// INV-13. A lexicographic list reads as truncated when the reader is scanning
// for an ID.
func sortByID(invs []Invariant) {
	sort.SliceStable(invs, func(a, b int) bool { return lessID(invs[a].ID, invs[b].ID) })
}

func lessID(a, b string) bool {
	as, bs := strings.Split(a, "-"), strings.Split(b, "-")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, aok := segNum(as[i])
		bn, bok := segNum(bs[i])
		if aok && bok && an != bn {
			return an < bn
		}
		if aok != bok {
			// A numeric segment sorts before an alphanumeric one, so INV-1
			// precedes INV-A.
			return aok
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}

// segNum reads a leading run of digits, so "6b" compares as 6 and sorts next to
// "6" rather than beside "60".
func segNum(s string) (int, bool) {
	n, digits := 0, 0
	for ; digits < len(s) && s[digits] >= '0' && s[digits] <= '9'; digits++ {
		n = n*10 + int(s[digits]-'0')
	}
	return n, digits > 0
}
