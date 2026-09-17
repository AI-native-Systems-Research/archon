package invariant

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CitationRegexp matches a citation of id in source text.
//
// Two rules, both of them bugs we shipped in the prototype:
//
//   - The full ID is required, prefix included. Matching "INV-2" by its numeric
//     part matched every test whose name contained "E2E".
//   - The trailing boundary is spelled out as a consumed character class rather
//     than a lookahead, so "INV-1" cannot match "INV-13" (and "INV-PD-6" cannot
//     match "INV-PD-6b"). RE2 has no lookahead by design; the prototype's
//     "(?!...)" fix made BSD grep -E return zero matches and the report looked
//     clean while being broken.
//
// The boundary excludes "-" and "_" as well as letters and digits, so an ID is
// counted only where it stands on its own. That drops "INV-6-safe", which BLIS's
// registry names as a known false positive of its own git-grep recipe: it is the
// adjective "INV-6-safe", not a citation. Both boundaries agree, so "foo_INV-6"
// and "INV-6_foo" are treated alike.
func CitationRegexp(id string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(id) + `([^0-9A-Za-z_-]|$)`)
}

var testFuncRe = regexp.MustCompile(`(?m)^[ \t]*func (Test[A-Za-z0-9_]*)\(`)

// skipDir is the set of directory names never scanned. Beyond the obvious
// non-source trees, "testdata" is excluded because the Go tool itself ignores
// it: a fixture that mentions an ID is not evidence that anything upholds it.
var skipDir = map[string]bool{".git": true, "vendor": true, "node_modules": true, "testdata": true}

// LinkRepo scans the Go sources under root and reports what the repository has
// behind each declared invariant.
//
// Only files the Go tool would build are scanned — .go outside testdata and
// underscore-prefixed paths — so a fixture or a markdown registry mentioning an
// ID is not mistaken for something upholding it. A citation is still only
// evidence that a file names the ID: a file that discusses an invariant, such as
// a linter listing IDs as data, counts as a citation, which is inherent to
// following IDs rather than inferring them. Reports name the files so a reader
// can see the site.
//
// An empty result is only ever "nothing cites these IDs". Being handed a root
// that is not a directory, or one containing no Go files at all, is an error
// rather than a repository-wide UNLINKED verdict.
//
// Returned links are in the order of invs; every slice inside them is sorted and
// de-duplicated, and no map iteration reaches the result.
func LinkRepo(root string, invs []Invariant) ([]Link, error) {
	byNorm := map[string]string{} // normalized ID -> ID
	for _, inv := range invs {
		n := normalizeID(inv.ID)
		if prev, ok := byNorm[n]; ok && prev != inv.ID {
			return nil, fmt.Errorf("invariants %s and %s are indistinguishable in test names (both normalize to %s)", prev, inv.ID, n)
		}
		byNorm[n] = inv.ID
	}
	links := make([]Link, len(invs))
	for i, inv := range invs {
		links[i] = Link{Invariant: inv}
	}
	// Several invariants may share an ID across scopes; a citation of that ID
	// text is a citation for each of them.
	byID := map[string][]*Link{}
	for i := range links {
		byID[links[i].Invariant.ID] = append(byID[links[i].Invariant.ID], &links[i])
	}
	patterns := map[string]*regexp.Regexp{}
	for id := range byID {
		patterns[id] = CitationRegexp(id)
	}

	// WalkDir does not follow symlinks, and a symlinked root would arrive as a
	// non-directory entry, be skipped for want of a .go suffix, and yield a
	// confident repository-wide UNLINKED report. CI checkouts and macOS /tmp are
	// routinely reached through a symlink.
	walkRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", root, err)
	}
	if fi, err := os.Stat(walkRoot); err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	scanned := 0
	err = filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != walkRoot && (skipDir[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return fs.SkipDir
			}
			return nil
		}
		// An underscore-prefixed file is invisible to the Go tool, so it is not
		// production evidence either.
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, "_") {
			return nil
		}
		scanned++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(walkRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		isTest := strings.HasSuffix(name, "_test.go")
		src := string(b)

		for id, ls := range byID {
			n := len(patterns[id].FindAllStringIndex(src, -1))
			if n == 0 {
				continue
			}
			for _, l := range ls {
				if isTest {
					l.TestFiles = append(l.TestFiles, rel)
				} else {
					l.CodeFiles = append(l.CodeFiles, rel)
				}
				l.Citations += n
			}
		}

		if isTest {
			for _, m := range testFuncRe.FindAllStringSubmatch(src, -1) {
				for _, id := range namedFor(m[1], byNorm) {
					for _, l := range byID[id] {
						l.NamedTests = append(l.NamedTests, rel+":"+m[1])
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	// Reporting every invariant UNLINKED is the loudest thing this package can
	// say about a repository. It must not be what a caller gets for pointing at
	// a docs directory, an empty checkout, or a tree whose Go files all live
	// somewhere the walk skips.
	if scanned == 0 {
		return nil, fmt.Errorf("scan %s: no Go files found", root)
	}

	for i := range links {
		links[i].CodeFiles = sortedUnique(links[i].CodeFiles)
		links[i].TestFiles = sortedUnique(links[i].TestFiles)
		links[i].NamedTests = sortedUnique(links[i].NamedTests)
	}
	return links, nil
}

// namedFor returns the IDs a test function is named for. Separators may be
// stripped or substituted, so INV-6 matches TestINV6_Determinism, INV-P2-1
// matches TestINV_P2_1_PoolConfigConsistency, and INV-PD-3 matches
// TestTransferContention_BCP27_INVPD3_Holds.
//
// The match is against whole runs of the name's own tokens, not a substring of
// it. Stripping separators and looking for a substring is the bare-number bug
// wearing a letter: with INV-A declared, TestINVAllocatorBoundary and
// TestOptionsINVArgs both come back as tests for INV-A, and TestBINV1_X as a
// test for INV-1. Tokenising both sides means "Allocator" cannot serve as the
// "A" segment.
//
// A match must also not be followed by a digit or a lowercase letter, which is
// what keeps INV-1 off TestINV13_RunReplayParity and INV-PD-6 off a test named
// for INV-PD-6b — including when the longer ID is not itself declared, which
// is where the previous longest-wins rule quietly failed.
func namedFor(testName string, byNorm map[string]string) []string {
	tokens := tokenize(testName)
	var out []string
	for i := range tokens {
		run := ""
		for j := i; j < len(tokens); j++ {
			run += tokens[j]
			id, ok := byNorm[run]
			if !ok {
				continue
			}
			if j+1 < len(tokens) {
				c := tokens[j+1][0]
				if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' {
					continue
				}
			}
			out = append(out, id)
		}
	}
	return sortedUnique(out)
}

// tokenize splits an identifier into the runs a reader sees as separate words:
// at every non-alphanumeric character, and at each transition between an
// uppercase run, a lowercase run and a digit run. "TestINV_P2_1_PoolConfig"
// becomes Test INV P 2 1 Pool Config; "INVPD6b" becomes INVPD 6 b.
func tokenize(s string) []string {
	type run struct {
		text     string
		class    byte // 'U', 'L' or 'D'
		adjacent bool // no separator between this run and the previous one
	}
	var runs []run
	adjacent := false
	for i := 0; i < len(s); {
		c := s[i]
		var class byte
		switch {
		case isUpper(c):
			class = 'U'
		case isLower(c):
			class = 'L'
		case isDigit(c):
			class = 'D'
		default:
			i++
			adjacent = false
			continue
		}
		j := i
		for j < len(s) && classOf(s[j]) == class {
			j++
		}
		runs = append(runs, run{text: s[i:j], class: class, adjacent: adjacent})
		i, adjacent = j, true
	}

	var out []string
	for i := 0; i < len(runs); i++ {
		text := runs[i].text
		// A capitalised word arrives as a one-character uppercase run followed
		// by a lowercase one ("T"+"est"); an uppercase run ending a longer
		// acronym donates only its last character ("INVA"+"llocator" is INV and
		// Allocator, never INVA).
		if runs[i].class == 'U' && i+1 < len(runs) && runs[i+1].adjacent && runs[i+1].class == 'L' {
			if len(text) > 1 {
				out = append(out, text[:len(text)-1])
				text = text[len(text)-1:]
			}
			text += runs[i+1].text
			i++
		}
		out = append(out, text)
	}
	return out
}

func classOf(c byte) byte {
	switch {
	case isUpper(c):
		return 'U'
	case isLower(c):
		return 'L'
	case isDigit(c):
		return 'D'
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

// normalizeID drops every non-alphanumeric character and preserves case, so
// "INV-PD-6b" and "INVPD6b" compare equal while "INV-PD-6" stays distinct
// from both.
func normalizeID(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isDigit(c) || isUpper(c) || isLower(c) {
			b.WriteByte(c)
		}
	}
	return b.String()
}
