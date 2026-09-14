package callgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildModule writes a module to a temp dir and builds its call graph.
func buildModule(t *testing.T, mode Mode, files map[string]string) *Graph {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	g, err := Build(dir, "./...", mode)
	if err != nil {
		t.Fatalf("Build(%v): %v", mode, err)
	}
	return g
}

// ifaceModule is the shape the whole issue is about: api calls a method on an
// interface, and two packages implement it.
//
//	api.Serve --Store.Get--> mem.(*Store).Get
//	api.Serve --Store.Get--> disk.(*Store).Get
//
// api imports only store, never mem or disk — which is the point of the
// interface, and why the static walk has nothing to record.
func ifaceModule() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"store/store.go": `package store

type Store interface {
	Get(k string) string
}
`,
		"mem/mem.go": `package mem

type Store struct{}

func (s *Store) Get(k string) string { return memHelper(k) }

func memHelper(k string) string { return k }
`,
		"disk/disk.go": `package disk

type Store struct{}

func (s *Store) Get(k string) string { return "disk:" + k }
`,
		"api/api.go": `package api

import "example.com/cg/store"

func Serve(s store.Store) string { return s.Get("x") }
`,
		// A main so RTA has an entry point, wiring up only mem.
		"main.go": `package main

import (
	"example.com/cg/api"
	"example.com/cg/mem"
)

func main() { println(api.Serve(&mem.Store{})) }
`,
	}
}

func edgesFrom(g *Graph, fromSuffix string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if strings.HasSuffix(e.From, fromSuffix) {
			out = append(out, e)
		}
	}
	return out
}

func hasEdge(g *Graph, fromSuffix, toSuffix, via string) bool {
	for _, e := range g.Edges {
		if strings.HasSuffix(e.From, fromSuffix) && strings.HasSuffix(e.To, toSuffix) && e.Via == via {
			return true
		}
	}
	return false
}

// --- The bug ---

// Static must keep dropping interface calls. This is not a wish, it is the
// documented behaviour the CHA mode exists to supplement, and pinning it is what
// makes the CHA test below meaningful.
func TestStaticDropsInterfaceCalls(t *testing.T) {
	g := buildModule(t, Static, ifaceModule())

	if got := edgesFrom(g, "api.Serve"); len(got) != 0 {
		t.Errorf("static mode resolved an interface call: %+v", got)
	}
	// But it must still find ordinary calls, or the fixture proves nothing.
	if !hasEdge(g, "mem.Store).Get", "mem.memHelper", "") {
		t.Errorf("static mode lost a direct call; edges = %+v", g.Edges)
	}
}

// --- The fix ---

func TestCHAResolvesInterfaceCallToEveryImplementer(t *testing.T) {
	g := buildModule(t, CHA, ifaceModule())

	if !hasEdge(g, "api.Serve", "mem.Store).Get", "Get") {
		t.Errorf("no edge api.Serve -> mem.Get; edges = %+v", g.Edges)
	}
	if !hasEdge(g, "api.Serve", "disk.Store).Get", "Get") {
		t.Errorf("no edge api.Serve -> disk.Get; edges = %+v", g.Edges)
	}
	// The documented cost of soundness: CHA cannot know which implementer is
	// wired up, so it reports both. Pinned so the trade-off is a decision.
	if got := edgesFrom(g, "api.Serve"); len(got) != 2 {
		t.Errorf("got %d edges from api.Serve, want 2 (one per implementer): %+v", len(got), got)
	}
}

// The witness is what distinguishes a resolved interface call from a direct one.
// Without it a reader cannot tell why the edge exists.
func TestResolvedEdgeCarriesDispatchingMethod(t *testing.T) {
	g := buildModule(t, CHA, ifaceModule())

	for _, e := range edgesFrom(g, "api.Serve") {
		if e.Via != "Get" {
			t.Errorf("edge %+v has Via=%q, want the dispatching method \"Get\"", e, e.Via)
		}
	}
	// A direct call must NOT be labelled as dispatched.
	for _, e := range g.Edges {
		if strings.HasSuffix(e.To, "mem.memHelper") && e.Via != "" {
			t.Errorf("direct call marked as dispatched: %+v", e)
		}
	}
}

// CHA adds to the static set, never replaces it. Union rather than substitution
// is what makes this true by construction.
func TestCHAIsStrictSupersetOfStatic(t *testing.T) {
	files := ifaceModule()
	static := buildModule(t, Static, files)
	chaG := buildModule(t, CHA, files)

	present := map[Edge]bool{}
	for _, e := range chaG.Edges {
		present[e] = true
	}
	for _, e := range static.Edges {
		if !present[e] {
			t.Errorf("cha lost a static edge: %+v", e)
		}
	}
	if len(chaG.Edges) <= len(static.Edges) {
		t.Errorf("cha has %d edges, static %d — expected strictly more",
			len(chaG.Edges), len(static.Edges))
	}
}

// CHA must add ONLY interface-dispatched edges. Taking SSA's direct calls as well
// looks harmless — an identical edge would dedupe — but SSA attributes calls made
// through func values to the enclosing function, which invents edges the source
// does not have. Measured on BLIS: dropping the invoke filter added 187 edges,
// including sim.VLLMBatchFormation.FormBatch -> cmd.isBankSelection, a layering
// violation that does not exist.
func TestCHAAddsOnlyDispatchedEdges(t *testing.T) {
	// The fixture needs a call through a FUNC VALUE as well as through an
	// interface. The static walk cannot resolve `f()` — f is a variable, not a
	// function — while SSA + CHA can, and attributes it to the enclosing function.
	// That is the shape that produced the bogus BLIS edges, and without it here
	// this test passes even with the invoke filter removed.
	files := ifaceModule()
	files["fv/fv.go"] = `package fv

import "example.com/cg/store"

func target() string { return "t" }

func pick() func() string { return target }

func Run(s store.Store) string {
	f := pick()
	return f() + s.Get("k")
}
`
	static := buildModule(t, Static, files)
	chaG := buildModule(t, CHA, files)

	// Precondition: the static walk really does miss the func-value call.
	if hasEdge(static, "fv.Run", "fv.target", "") {
		t.Fatal("static resolved a func-value call; the fixture no longer isolates the case")
	}

	inStatic := map[Edge]bool{}
	for _, e := range static.Edges {
		inStatic[e] = true
	}
	added := 0
	for _, e := range chaG.Edges {
		if inStatic[e] {
			continue
		}
		added++
		if e.Via == "" {
			t.Errorf("cha added a NON-dispatched edge, so it is importing SSA's direct "+
				"calls rather than only interface ones: %+v", e)
		}
	}
	if added == 0 {
		t.Fatal("cha added nothing; this test would pass vacuously")
	}
}

// --- Determinism (archon guarantees byte-identical output) ---

func TestBuildIsDeterministic(t *testing.T) {
	for _, mode := range []Mode{Static, CHA} {
		files := ifaceModule()
		a := buildModule(t, mode, files)
		b := buildModule(t, mode, files)

		if len(a.Edges) != len(b.Edges) || len(a.Funcs) != len(b.Funcs) {
			t.Fatalf("mode %v: sizes differ between builds", mode)
		}
		for i := range a.Edges {
			if a.Edges[i] != b.Edges[i] {
				t.Errorf("mode %v: edge %d differs: %+v vs %+v", mode, i, a.Edges[i], b.Edges[i])
			}
		}
		for i := range a.Funcs {
			if a.Funcs[i].ID != b.Funcs[i].ID {
				t.Errorf("mode %v: func %d differs: %s vs %s", mode, i, a.Funcs[i].ID, b.Funcs[i].ID)
			}
		}
	}
}

func TestEdgesAndFuncsAreSorted(t *testing.T) {
	g := buildModule(t, CHA, ifaceModule())

	for i := 1; i < len(g.Funcs); i++ {
		if g.Funcs[i-1].ID > g.Funcs[i].ID {
			t.Errorf("funcs not sorted at %d: %q > %q", i, g.Funcs[i-1].ID, g.Funcs[i].ID)
		}
	}
	for i := 1; i < len(g.Edges); i++ {
		p, c := g.Edges[i-1], g.Edges[i]
		if p.From > c.From || (p.From == c.From && p.To > c.To) ||
			(p.From == c.From && p.To == c.To && p.Via > c.Via) {
			t.Errorf("edges not sorted at %d: %+v > %+v", i, p, c)
		}
	}
}

// --- Filtering ---

// CHA returns edges into the stdlib and every dependency; only in-module
// functions with a body belong in the graph.
func TestOutOfModuleCallsAreExcluded(t *testing.T) {
	g := buildModule(t, CHA, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"a/a.go": `package a

import (
	"sort"
	"strings"
)

func Run(xs []string) string {
	sort.Strings(xs)
	return strings.Join(xs, ",")
}
`,
	})

	for _, e := range g.Edges {
		for _, bad := range []string{"sort.", "strings.", "internal/"} {
			if strings.HasPrefix(e.To, bad) || strings.HasPrefix(e.From, bad) {
				t.Errorf("edge touches a non-module function: %+v", e)
			}
		}
	}
	for _, f := range g.Funcs {
		if !strings.HasPrefix(f.Pkg, "example.com/cg") {
			t.Errorf("func from outside the module: %+v", f)
		}
	}
}

// SSA builds synthetic thunks for method values, named like (*T).M$bound. Mapping
// back to the declaration keeps them out of the graph.
func TestMethodValuesDoNotLeakSyntheticNames(t *testing.T) {
	g := buildModule(t, CHA, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"a/a.go": `package a

type T struct{}

func (t *T) M() int { return 1 }

func Use() func() int {
	t := &T{}
	return t.M // a method value: SSA builds a $bound thunk
}
`,
	})

	for _, f := range g.Funcs {
		if strings.Contains(f.ID, "$") || strings.Contains(f.Label, "$") {
			t.Errorf("synthetic SSA name leaked into the graph: %+v", f)
		}
	}
	for _, e := range g.Edges {
		if strings.Contains(e.From, "$") || strings.Contains(e.To, "$") {
			t.Errorf("synthetic SSA name leaked into an edge: %+v", e)
		}
	}
}

// A generic function is instantiated per type argument in SSA; Origin maps the
// instantiations back to the one declaration.
func TestGenericsMapBackToDeclaration(t *testing.T) {
	g := buildModule(t, CHA, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"a/a.go": `package a

func Map[T any](xs []T, f func(T) T) []T {
	out := make([]T, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func Ints() []int    { return Map([]int{1}, func(i int) int { return i }) }
func Strs() []string { return Map([]string{"a"}, func(s string) string { return s }) }
`,
	})

	n := 0
	for _, f := range g.Funcs {
		if strings.HasSuffix(f.ID, "a.Map") {
			n++
		}
		if strings.Contains(f.ID, "[") {
			t.Errorf("an instantiation leaked instead of the declaration: %+v", f)
		}
	}
	if n != 1 {
		t.Errorf("found %d entries for a.Map, want exactly 1 declaration", n)
	}
}

// --- RTA ---

// RTA needs an entry point. A library has none, and silently falling back to a
// less sound builder would misreport the graph as complete.
func TestRTAWithoutMainIsAnError(t *testing.T) {
	files := ifaceModule()
	delete(files, "main.go")

	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Build(dir, "./...", RTA); err == nil {
		t.Error("Build with mode rta and no main succeeded; want an error")
	} else if !strings.Contains(err.Error(), "main") {
		t.Errorf("error %q does not mention the missing main", err)
	}
}

// With a main that wires up only mem, RTA should prune disk — the precision CHA
// cannot offer. This is the case where "identify the one that is wired up" works.
func TestRTAPrunesUninstantiatedImplementer(t *testing.T) {
	g := buildModule(t, RTA, ifaceModule())

	if !hasEdge(g, "api.Serve", "mem.Store).Get", "Get") {
		t.Errorf("rta lost the wired-up implementer; edges = %+v", g.Edges)
	}
	if hasEdge(g, "api.Serve", "disk.Store).Get", "Get") {
		t.Errorf("rta kept disk.Store, which main never instantiates; edges = %+v", g.Edges)
	}
}

// --- Modes and errors ---

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"": Static, "static": Static, "cha": CHA, "rta": RTA} {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %v, %v; want %v, nil", in, got, err, want)
		}
	}
	// An unknown mode must fail rather than quietly analysing less.
	if _, err := ParseMode("vta"); err == nil {
		t.Error("ParseMode(\"vta\") succeeded; want an error naming the valid modes")
	}
}

func TestModeString(t *testing.T) {
	for m, want := range map[Mode]string{Static: "static", CHA: "cha", RTA: "rta"} {
		if got := m.String(); got != want {
			t.Errorf("Mode(%d).String() = %q, want %q", m, got, want)
		}
	}
}

func TestBuildErrorsOnNoMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/empty\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(dir, "./...", Static); err == nil {
		t.Error("Build on a module with no Go files succeeded; want an error")
	}
}

// Positions drive the --since delta scoping in cmd/callgraph; a zero line number
// would make every function look unchanged.
func TestFuncsCarryPositions(t *testing.T) {
	g := buildModule(t, Static, ifaceModule())

	for _, f := range g.Funcs {
		if f.File == "" || f.Lo == 0 || f.Hi < f.Lo {
			t.Errorf("func has unusable position: %+v", f)
		}
	}
}
