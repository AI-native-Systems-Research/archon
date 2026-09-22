package invariant

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLinkRepo_CitationSites_EnclosingScope pins the scope each citation is
// attached to: the enclosing function (including its doc comment), else the
// enclosing top-level declaration block, else the whole file. A file that does
// not parse degrades to whole-file scope rather than dropping its citations.
func TestLinkRepo_CitationSites_EnclosingScope(t *testing.T) {
	dir := t.TempDir()
	// A citation in a function's doc comment documents that function.
	write(t, dir, "fn.go", `package p

// Foo does a thing. INV-6 holds inside it.
func Foo() {
	_ = 1
}
`)
	// A citation above a const block has no enclosing function; the declaration
	// block is the scope.
	write(t, dir, "decl.go", `package p

// INV-7: names are required here.
const answer = 42
`)
	// A citation in a file-header comment encloses in neither a func nor a decl.
	write(t, dir, "hdr.go", `// File note: INV-8 lives up here, above package.
package p

var Z = 1
`)
	// A file that does not parse must not drop its citation: it degrades to the
	// whole file.
	write(t, dir, "broken.go", `package p

// INV-9 cited here, but the file does not parse.
func Broken( {
`)

	invs := []Invariant{{ID: "INV-6"}, {ID: "INV-7"}, {ID: "INV-8"}, {ID: "INV-9"}}
	links, _, err := LinkRepo(dir, invs)
	if err != nil {
		t.Fatalf("LinkRepo: %v", err)
	}
	byID := map[string]Link{}
	for _, l := range links {
		byID[l.Invariant.ID] = l
	}

	site := func(id string) CitationSite {
		t.Helper()
		l := byID[id]
		if len(l.CitationSites) != 1 {
			t.Fatalf("%s: want exactly one citation site, got %+v", id, l.CitationSites)
		}
		return l.CitationSites[0]
	}

	if s := site("INV-6"); s.Scope != "func" || s.Func != "Foo" {
		t.Errorf("INV-6 site = %+v, want scope=func func=Foo", s)
	} else if !(s.Start <= s.Line && s.Line <= s.End) {
		t.Errorf("INV-6 site line %d not within scope span [%d,%d]", s.Line, s.Start, s.End)
	}
	if s := site("INV-7"); s.Scope != "decl" {
		t.Errorf("INV-7 site = %+v, want scope=decl (enclosing const block)", s)
	}
	if s := site("INV-8"); s.Scope != "file" {
		t.Errorf("INV-8 site = %+v, want scope=file (header comment)", s)
	}
	if s := site("INV-9"); s.Scope != "file" {
		t.Errorf("INV-9 site = %+v, want scope=file (unparseable file degrades, not dropped)", s)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
