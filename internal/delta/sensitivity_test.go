// Sensitivity tests (issue #43). The demo/ golden tests prove STABILITY — the
// same input yields the same bytes. These prove SENSITIVITY: a real boundary
// violation is actually detected. A bug that made delta silently miss a removed
// edge would keep every golden test green, so stability alone is not enough.
//
// External test package (delta_test) is required: gate imports delta, so an
// in-package test importing gate would be an import cycle.
package delta_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/extract"
	"github.com/AI-native-Systems-Research/archon/internal/gate"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
	"github.com/AI-native-Systems-Research/archon/internal/plan"
)

const (
	modPath  = "example.com/s"
	pkgApp   = modPath + "/app"
	pkgMem   = modPath + "/mem"
	pkgStore = modPath + "/store"
)

// baseFiles is the unmutated fixture module, returned fresh each call so a
// mutation in one test cannot leak into another.
//
// Shape: app --import,call--> mem --import--> store, and mem.Store satisfies
// store.Cache (an implements edge). mem carries a test so the gofmt-normalized
// invariant hash is exercised too.
func baseFiles() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/s\n\ngo 1.26\n",

		"store/store.go": `package store

type Item struct{ Key string }

type Cache interface {
	Get(k string) (Item, bool)
}
`,

		"mem/mem.go": `package mem

import "example.com/s/store"

type Entry struct{ Key string }

type Store struct{}

func New() *Store { return &Store{} }

func (s *Store) Get(k string) (store.Item, bool) { return store.Item{Key: k}, true }

func (s *Store) Put(e Entry) {}

func Flush() {}
`,

		"mem/mem_test.go": `package mem

import "testing"

func TestConserve(t *testing.T) {
	s := New()
	if _, ok := s.Get("k"); !ok {
		t.Fatal("want hit")
	}
}
`,

		"app/app.go": `package app

import "example.com/s/mem"

func Run() {
	c := mem.New()
	c.Put(mem.Entry{})
	mem.Flush()
}
`,
	}
}

// mutate returns baseFiles with the named files replaced. It rejects an unknown
// filename: a typo would otherwise leave the fixture unmutated and the test
// would pass while proving nothing.
func mutate(t *testing.T, edits map[string]string) map[string]string {
	t.Helper()
	files := baseFiles()
	for name, src := range edits {
		if _, ok := files[name]; !ok {
			t.Fatalf("mutate targets unknown file %q — check the name", name)
		}
		files[name] = src
	}
	return files
}

// extractModule writes files to a temp module and extracts its graph.
func extractModule(t *testing.T, files map[string]string) *graph.Graph {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	res, err := extract.Extract(dir)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// Guard, not decoration: Extract returns a partial graph when the fixture
	// fails to typecheck, so without this a broken fixture yields an empty
	// delta and the test passes for entirely the wrong reason.
	if res.NumErrors != 0 {
		t.Fatalf("fixture does not typecheck (%d package errors); fix the fixture — "+
			"a non-compiling fixture makes this test meaningless", res.NumErrors)
	}
	return res.Graph
}

// baseAnd extracts the base graph and a mutated graph, and returns the delta.
func baseAnd(t *testing.T, edits map[string]string) (before, after *graph.Graph, d *delta.Delta) {
	t.Helper()
	before = extractModule(t, baseFiles())
	after = extractModule(t, mutate(t, edits))
	return before, after, delta.Compute(before, after)
}

func findEdge(edges []graph.Edge, from, kind, to string) *graph.Edge {
	for i := range edges {
		if edges[i].From == from && edges[i].Kind == kind && edges[i].To == to {
			return &edges[i]
		}
	}
	return nil
}

func surfaceOf(d *delta.Delta, pkg string) *delta.SurfaceChange {
	for i := range d.Surface {
		if d.Surface[i].Package == pkg {
			return &d.Surface[i]
		}
	}
	return nil
}

func hasSymbol(syms []graph.Symbol, name string) bool {
	for _, s := range syms {
		if s.Name == name {
			return true
		}
	}
	return false
}

func compilePlan(t *testing.T, src string) *graph.Graph {
	t.Helper()
	g, diags := plan.Compile([]byte(src))
	if len(diags) != 0 {
		t.Fatalf("plan does not compile: %v", diags)
	}
	return g
}

// --- Sanity: the base fixture has the shape every case below depends on. ---

func TestSensitivityBaseFixtureShape(t *testing.T) {
	g := extractModule(t, baseFiles())

	for _, want := range []struct{ from, kind, to string }{
		{pkgApp, "import", pkgMem},
		{pkgApp, "call", pkgMem},
		{pkgMem, "import", pkgStore},
		{pkgMem, "implements", pkgStore},
	} {
		if findEdge(g.Edges, want.from, want.kind, want.to) == nil {
			t.Errorf("base fixture missing edge %s --%s--> %s; the cases below rely on it",
				want.from, want.kind, want.to)
		}
	}

	// A base compared with itself must be empty — otherwise every case below
	// could be reporting noise rather than its mutation.
	if d := delta.Compute(g, extractModule(t, baseFiles())); !d.EmptyAtPackageAltitude {
		t.Errorf("base vs base is not empty at package altitude: %+v", d)
	}
}

// --- 1. Import outside the plan's Allow list -> C4 disallowed arrow. ---

func TestSensitivityDisallowedImportIsC4(t *testing.T) {
	planSrc := `hole example.com/s/app {
  surface:
    Run()
  allow:
    import example.com/s/mem
}
box example.com/s/mem
box example.com/s/store
`
	p := compilePlan(t, planSrc)

	before := extractModule(t, baseFiles())
	if got := plan.Dist(p, before).C4; got != 0 {
		t.Fatalf("base already violates Allow: C4 = %d, want 0", got)
	}

	after := extractModule(t, mutate(t, map[string]string{
		"app/app.go": `package app

import (
	"example.com/s/mem"
	"example.com/s/store"
)

func Run() {
	c := mem.New()
	c.Put(mem.Entry{})
	mem.Flush()
	_ = store.Item{}
}
`,
	}))

	res := plan.Dist(p, after)
	if res.C4 != 1 {
		t.Errorf("C4 = %d, want 1 (app imports store, which its Allow list omits); unmet = %+v",
			res.C4, res.Unmet)
	}
}

// --- 2. Declared arrow missing from the code -> C3 absent arrow. ---

func TestSensitivityMissingDeclaredArrowIsC3(t *testing.T) {
	p := compilePlan(t, `box example.com/s/app
box example.com/s/mem
arrow example.com/s/app -> example.com/s/mem : import
`)

	before := extractModule(t, baseFiles())
	if got := plan.Dist(p, before).C3; got != 0 {
		t.Fatalf("base is missing the declared arrow already: C3 = %d, want 0", got)
	}

	after := extractModule(t, mutate(t, map[string]string{
		"app/app.go": "package app\n\nfunc Run() {}\n",
	}))

	res := plan.Dist(p, after)
	if res.C3 != 1 {
		t.Errorf("C3 = %d, want 1 (app no longer imports mem); unmet = %+v", res.C3, res.Unmet)
	}
}

// --- 3. New exported entity -> surface growth, and G3 flags it. ---

func TestSensitivityNewExportedFuncIsSurfaceGrowth(t *testing.T) {
	_, _, d := baseAnd(t, map[string]string{
		"mem/mem.go": `package mem

import "example.com/s/store"

type Entry struct{ Key string }

type Store struct{}

func New() *Store { return &Store{} }

func (s *Store) Get(k string) (store.Item, bool) { return store.Item{Key: k}, true }

func (s *Store) Put(e Entry) {}

func Flush() {}

func Compact() {}
`,
	})

	sc := surfaceOf(d, pkgMem)
	if sc == nil {
		t.Fatalf("no surface change recorded for %s; delta = %+v", pkgMem, d)
	}
	if !hasSymbol(sc.Added, "Compact") {
		t.Errorf("surface added = %+v, want it to include Compact", sc.Added)
	}

	// G3: with mem declared fixed, the growth must be reported as unauthorized.
	widenings := gate.CheckSurface(d.Surface, &gate.SurfacePolicy{
		Fixed: map[string]bool{pkgMem: true},
	})
	if len(widenings) != 1 {
		t.Fatalf("CheckSurface returned %d widenings, want 1: %+v", len(widenings), widenings)
	}
	if !hasSymbol(widenings[0].Added, "Compact") {
		t.Errorf("widening added = %+v, want it to include Compact", widenings[0].Added)
	}

	// The same growth must be silent once explicitly authorized, or the gate
	// would be reporting any change rather than an unauthorized one.
	authorized := gate.CheckSurface(d.Surface, &gate.SurfacePolicy{
		Fixed: map[string]bool{pkgMem: true},
		Widen: map[string][]string{pkgMem: {"Compact"}},
	})
	if len(authorized) != 0 {
		t.Errorf("authorized widening still reported: %+v", authorized)
	}
}

// --- 4. Unexporting an entity -> surface removal. ---

func TestSensitivityUnexportIsSurfaceRemoval(t *testing.T) {
	_, _, d := baseAnd(t, map[string]string{
		"mem/mem.go": `package mem

import "example.com/s/store"

type Entry struct{ Key string }

type Store struct{}

func New() *Store { return &Store{} }

func (s *Store) Get(k string) (store.Item, bool) { return store.Item{Key: k}, true }

func (s *Store) Put(e Entry) {}

func flush() {}

var _ = flush
`,
		// Flush is no longer reachable from app.
		"app/app.go": `package app

import "example.com/s/mem"

func Run() {
	c := mem.New()
	c.Put(mem.Entry{})
}
`,
	})

	sc := surfaceOf(d, pkgMem)
	if sc == nil {
		t.Fatalf("no surface change recorded for %s; delta = %+v", pkgMem, d)
	}
	if !hasSymbol(sc.Removed, "Flush") {
		t.Errorf("surface removed = %+v, want it to include Flush", sc.Removed)
	}
}

// --- 5. Breaking interface satisfaction -> implements edge removed. ---

func TestSensitivityLostInterfaceSatisfactionRemovesImplementsEdge(t *testing.T) {
	// Get loses its (Item, bool) shape, so Store no longer satisfies Cache. mem
	// still imports store, which isolates the implements edge from the import edge.
	before, after, d := baseAnd(t, map[string]string{
		"mem/mem.go": `package mem

import "example.com/s/store"

type Entry struct{ Key string }

type Store struct{}

func New() *Store { return &Store{} }

func (s *Store) Get(k string) store.Item { return store.Item{Key: k} }

func (s *Store) Put(e Entry) {}

func Flush() {}
`,
		"mem/mem_test.go": `package mem

import "testing"

func TestConserve(t *testing.T) {
	s := New()
	if s.Get("k").Key != "k" {
		t.Fatal("want key")
	}
}
`,
	})

	if findEdge(before.Edges, pkgMem, "implements", pkgStore) == nil {
		t.Fatal("base fixture has no implements edge to lose")
	}
	if e := findEdge(after.Edges, pkgMem, "implements", pkgStore); e != nil {
		t.Errorf("implements edge survived the break: %+v", e)
	}
	if findEdge(d.EdgesRemoved, pkgMem, "implements", pkgStore) == nil {
		t.Errorf("EdgesRemoved does not report the implements edge: %+v", d.EdgesRemoved)
	}
	// The import edge must NOT be collateral damage.
	if findEdge(d.EdgesRemoved, pkgMem, "import", pkgStore) != nil {
		t.Errorf("import edge wrongly reported as removed: %+v", d.EdgesRemoved)
	}
}

// --- 6. Removing one call of several -> edge survives, witnesses shrink. ---

func TestSensitivityRemovingOneCallWeakensEdge(t *testing.T) {
	before, after, d := baseAnd(t, map[string]string{
		"app/app.go": `package app

import "example.com/s/mem"

func Run() {
	c := mem.New()
	c.Put(mem.Entry{})
}
`,
	})

	b := findEdge(before.Edges, pkgApp, "call", pkgMem)
	a := findEdge(after.Edges, pkgApp, "call", pkgMem)
	if b == nil || a == nil {
		t.Fatalf("call edge should exist on both sides: before=%v after=%v", b, a)
	}
	if len(a.Witnesses) >= len(b.Witnesses) {
		t.Errorf("witnesses did not shrink: before %v, after %v", b.Witnesses, a.Witnesses)
	}
	// A weakened edge is not a removed edge — conflating them would make every
	// refactor look like a decoupling.
	if findEdge(d.EdgesRemoved, pkgApp, "call", pkgMem) != nil {
		t.Errorf("surviving call edge wrongly reported as removed: %+v", d.EdgesRemoved)
	}
}

// --- 7. Removing the last call -> edge removed. ---

func TestSensitivityRemovingLastCallRemovesEdge(t *testing.T) {
	_, after, d := baseAnd(t, map[string]string{
		"app/app.go": "package app\n\nfunc Run() {}\n",
	})

	if e := findEdge(after.Edges, pkgApp, "call", pkgMem); e != nil {
		t.Errorf("call edge survived with no call sites left: %+v", e)
	}
	if findEdge(d.EdgesRemoved, pkgApp, "call", pkgMem) == nil {
		t.Errorf("EdgesRemoved does not report the call edge: %+v", d.EdgesRemoved)
	}
	// Dropping the last call also drops the import, since Go rejects an unused
	// one. Asserted so the coupling between the two is explicit, not a surprise.
	if findEdge(d.EdgesRemoved, pkgApp, "import", pkgMem) == nil {
		t.Errorf("EdgesRemoved does not report the import edge: %+v", d.EdgesRemoved)
	}
}

// --- 8. Pure reformat -> empty delta. ---

// This is the case that protects a real advantage over file-hash-based tooling
// (Bazel and friends re-run on any byte change). Invariant bodies are
// gofmt-normalized before hashing, so reformatting a test must NOT register as
// a touched promise. The sources below are deliberately mis-formatted in ways
// gofmt restores — indentation and spacing only, never added or removed lines,
// which gofmt preserves and which would legitimately change the hash.
func TestSensitivityReformatIsEmptyDelta(t *testing.T) {
	_, _, d := baseAnd(t, map[string]string{
		"mem/mem.go": `package mem

import "example.com/s/store"

type Entry struct{ Key string }

type Store struct{}

func New() *Store    { return   &Store{} }

func (s *Store) Get(k string) (store.Item,bool) { return store.Item{Key:k},true }

func (s *Store) Put(e Entry)   {}

func Flush()   {}
`,
		"mem/mem_test.go": `package mem

import "testing"

func TestConserve(t *testing.T) {
    s   :=   New()
    if _,ok := s.Get( "k" ); !ok {
        t.Fatal( "want hit" )
    }
}
`,
	})

	if !d.EmptyAtPackageAltitude {
		t.Errorf("reformat is not empty at package altitude: %+v", d)
	}
	if len(d.Surface) != 0 {
		t.Errorf("reformat changed the surface: %+v", d.Surface)
	}
	if len(d.EdgesAdded) != 0 || len(d.EdgesRemoved) != 0 {
		t.Errorf("reformat changed edges: added %+v removed %+v", d.EdgesAdded, d.EdgesRemoved)
	}
	if len(d.Invariants) != 0 {
		t.Errorf("reformat registered an invariant change (gofmt normalization failed): %+v",
			d.Invariants)
	}
}
