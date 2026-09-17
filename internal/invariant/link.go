package invariant

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
// The leading boundary is \b, which treats "_" as a word character: a citation
// written as "foo_INV-6" is not counted. Citations in Go comments are written
// with the ID standing on its own, and the named-test matcher handles the
// underscore spellings separately.
func CitationRegexp(id string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(id) + `([^0-9A-Za-z]|$)`)
}

var testFuncRe = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)

// skipDir is the set of directory names never scanned for citations.
var skipDir = map[string]bool{".git": true, "vendor": true, "node_modules": true}

// LinkRepo scans the Go sources under root and reports what the repository has
// behind each declared invariant.
//
// Only .go files are scanned, which is also what keeps a registry from counting
// as a citation of itself — the failure that made a BLIS invariant appear
// anchored when it existed only in the document declaring it.
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
	normIDs := make([]string, 0, len(byNorm))
	for n := range byNorm {
		normIDs = append(normIDs, n)
	}
	sort.Strings(normIDs)

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

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (skipDir[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
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
				for _, id := range namedFor(m[1], normIDs, byNorm) {
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
// Two IDs can be prefixes of one another once separators are gone, so at each
// position the longest declared ID wins — that is what keeps a test named for
// INV-PD-6b out of INV-PD-6's list. A digit may never follow the match, so
// INV-1 does not claim TestINV13_RunReplayParity even when INV-13 is not
// declared.
func namedFor(testName string, normIDs []string, byNorm map[string]string) []string {
	nt := normalizeID(testName)
	var out []string
	for i := range nt {
		best := ""
		for _, n := range normIDs {
			if len(n) <= len(best) || !strings.HasPrefix(nt[i:], n) {
				continue
			}
			if j := i + len(n); j < len(nt) && nt[j] >= '0' && nt[j] <= '9' {
				continue
			}
			best = n
		}
		if best != "" {
			out = append(out, byNorm[best])
		}
	}
	return sortedUnique(out)
}

// normalizeID drops every non-alphanumeric character and preserves case, so
// "INV-PD-6b" and "INVPD6b" compare equal while "INV-PD-6" stays distinct
// from both.
func normalizeID(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			b.WriteByte(c)
		}
	}
	return b.String()
}
