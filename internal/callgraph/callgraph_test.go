package callgraph

import (
	"fmt"
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

// Uses a generated fixture with many edges on purpose: the small interface
// fixture has only 4, so an unsorted slice has a real chance of landing in order
// by luck and the assertion passes while proving nothing.
func TestEdgesAndFuncsAreSorted(t *testing.T) {
	src := "package big\n\n"
	for i := 0; i < 24; i++ {
		src += fmt.Sprintf("func F%02d() {", i)
		for j := 0; j < 24; j++ {
			if j != i {
				src += fmt.Sprintf(" F%02d();", j)
			}
		}
		src += " }\n"
	}
	g := buildModule(t, Static, map[string]string{
		"go.mod":     "module example.com/big\n\ngo 1.26\n",
		"big/big.go": src,
	})
	if len(g.Edges) < 100 {
		t.Fatalf("fixture produced only %d edges; too few for this to mean anything", len(g.Edges))
	}

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

// An interface call inside a function literal must be attributed to the enclosing
// declaration. Closures, goroutine and defer bodies have no SSA Object() of their
// own, so without walking to the parent these edges vanish — and the static AST
// walk already attributes direct calls inside literals to the enclosing FuncDecl,
// so the two halves would disagree. This is the most common real shape there is.
func TestInterfaceCallInsideFunctionLiteralIsAttributed(t *testing.T) {
	files := ifaceModule()
	files["lit/lit.go"] = `package lit

import "example.com/cg/store"

func InClosure(s store.Store) func() string {
	return func() string { return s.Get("c") }
}

func InGoroutine(s store.Store) {
	go func() { _ = s.Get("g") }()
}

func InDefer(s store.Store) {
	defer func() { _ = s.Get("d") }()
}
`
	g := buildModule(t, CHA, files)

	for _, fn := range []string{"lit.InClosure", "lit.InGoroutine", "lit.InDefer"} {
		if !hasEdge(g, fn, "mem.Store).Get", "Get") {
			t.Errorf("%s: interface call inside a function literal was dropped; edges = %+v",
				fn, edgesFrom(g, fn))
		}
	}
	// And no synthetic SSA name may reach the graph.
	for _, e := range g.Edges {
		if strings.Contains(e.From, "$") || strings.Contains(e.To, "$") {
			t.Errorf("synthetic SSA name leaked into an edge: %+v", e)
		}
	}
}

// A generic function calling a constrained method dispatches through an interface,
// so it must produce an edge. The previous version of this test only exercised the
// static path and would have passed with interfaceEdges deleted entirely.
func TestGenericConstrainedMethodCallIsResolved(t *testing.T) {
	g := buildModule(t, CHA, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"cons/cons.go": `package cons

type Getter interface{ Get() string }
`,
		"impl/impl.go": `package impl

type T struct{}

func (T) Get() string { return "t" }
`,
		"api/api.go": `package api

import "example.com/cg/cons"

func Serve[G cons.Getter](g G) string { return g.Get() }
`,
	})

	if !hasEdge(g, "api.Serve", "impl.T).Get", "Get") {
		t.Errorf("call to a constrained method was not resolved; edges = %+v", g.Edges)
	}
	// The declaration, not a per-instantiation clone.
	for _, f := range g.Funcs {
		if strings.Contains(f.ID, "[") {
			t.Errorf("a generic instantiation leaked instead of the declaration: %+v", f)
		}
	}
}

// types.Func.FullName() is not unique: every func init() in a package renders the
// same. Collapsing them keeps one declaration's position and discards the rest,
// which silently empties the --since view. Reproduced as a regression against the
// original, which keyed by *types.Func and was immune.
func TestDuplicateFullNamesGetDistinctIDs(t *testing.T) {
	g := buildModule(t, Static, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"p/a.go": `package p

func init() { A() }

func A() {}
`,
		"p/b.go": `package p

func init() { B() }

func B() {}
`,
	})

	var inits []Func
	for _, f := range g.Funcs {
		if strings.Contains(f.ID, ".init") {
			inits = append(inits, f)
		}
	}
	if len(inits) != 2 {
		t.Fatalf("got %d init entries, want 2 (one per declaration): %+v", len(inits), g.Funcs)
	}
	if inits[0].ID == inits[1].ID {
		t.Errorf("both inits share the id %q, so one position was discarded", inits[0].ID)
	}
	// Each must keep its OWN file, or --since cannot match a diff hunk to it.
	if inits[0].File == inits[1].File {
		t.Errorf("both inits report file %q; positions were collapsed", inits[0].File)
	}
	// Both call edges must survive, keyed to the right init.
	if !hasEdge(g, "p.init#1", "p.A", "") || !hasEdge(g, "p.init#2", "p.B", "") {
		t.Errorf("init call edges lost or mis-attributed: %+v", g.Edges)
	}
}

// Pins the numbering contract: which id lands on which declaration. Note this
// cannot distinguish sorted order from traversal order — packages.Load yields files
// alphabetically, so they coincide, and disabling the sort leaves this green. What
// it does protect is the contract itself, since these ids are DOT node names and
// --since keys: if numbering ever changed, every stored graph would renumber.
func TestDuplicateIDNumberingFollowsFileAndLine(t *testing.T) {
	// m.go's init sits on a later line than the others, so the expectation below
	// pins line position as well as file order.
	g := buildModule(t, Static, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"p/a.go": "package p\n\nfunc init() {}\n",
		"p/m.go": "package p\n\n// pad\n// pad\nfunc init() {}\n",
		"p/z.go": "package p\n\nfunc init() {}\n",
	})

	got := map[string]string{} // id -> "basename:line"
	for _, f := range g.Funcs {
		if strings.Contains(f.ID, ".init") {
			got[f.ID] = fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Lo)
		}
	}
	want := map[string]string{
		"example.com/cg/p.init#1": "a.go:3",
		"example.com/cg/p.init#2": "m.go:5",
		"example.com/cg/p.init#3": "z.go:3",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d init ids, want %d: %+v", len(got), len(want), got)
	}
	for id, pos := range want {
		if got[id] != pos {
			t.Errorf("%s is at %q, want %q — numbering does not follow (file, line): %+v",
				id, got[id], pos, got)
		}
	}
}

// An ill-typed package means go/ssa builds nothing for it, so CHA and RTA see no
// call sites and no implementers in it while the AST walk still lists its
// functions. Build must report that rather than return a quietly deflated graph.
func TestIllTypedPackagesAreReported(t *testing.T) {
	files := ifaceModule()
	files["broken/broken.go"] = "package broken\n\nfunc B() { thisDoesNotExist() }\n"

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
	g, err := Build(dir, "./...", CHA)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(g.IllTyped) == 0 {
		t.Fatalf("an ill-typed package was not reported; the graph would look complete")
	}
	found := false
	for _, e := range g.IllTyped {
		if strings.Contains(e.Path, "broken") {
			found = true
			if !e.Own {
				t.Errorf("the package with the actual error is not marked Own: %+v", e)
			}
		}
	}
	if !found {
		t.Errorf("IllTyped does not name the broken package: %+v", g.IllTyped)
	}
}

// packages.IllTyped is transitive: importers of a broken package are flagged with
// no errors of their own. They usually outnumber the causes, so an alphabetical
// list truncates away the only lines worth reading. Causes must come first.
func TestIllTypedOrdersCausesBeforeImporters(t *testing.T) {
	files := map[string]string{
		"go.mod":               "module example.com/cg\n\ngo 1.26\n",
		"zzbroken/zzbroken.go": "package zzbroken\n\nfunc B() { thisDoesNotExist() }\n",
	}
	// Importers sort alphabetically BEFORE the cause, which is the trap.
	for _, n := range []string{"a01", "a02", "a03", "a04", "a05"} {
		files[n+"/"+n+".go"] = "package " + n + "\n\nimport \"example.com/cg/zzbroken\"\n\nvar _ = zzbroken.B\n"
	}
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
	g, err := Build(dir, "./...", CHA)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(g.IllTyped) < 2 {
		t.Fatalf("expected the cause and its importers, got %+v", g.IllTyped)
	}
	if !g.IllTyped[0].Own || !strings.Contains(g.IllTyped[0].Path, "zzbroken") {
		t.Errorf("the actual cause is not first, so truncation would hide it: %+v", g.IllTyped)
	}
	if got := g.OwnErrors(); got != 1 {
		t.Errorf("OwnErrors() = %d, want 1 — the count must not be inflated by importers", got)
	}
}

// A pair reached both directly and through an interface must yield ONE edge, or
// Graphviz draws two arrows between the same nodes and the edge count inflates.
func TestDirectAndDispatchedCallDoNotDuplicate(t *testing.T) {
	g := buildModule(t, CHA, map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"p/p.go": `package p

type Getter interface{ Get() string }

type T struct{}

func (t *T) Get() string { return "t" }

func Caller(t *T, g Getter) string { return t.Get() + g.Get() }
`,
	})

	n := 0
	for _, e := range g.Edges {
		if strings.HasSuffix(e.From, "p.Caller") && strings.HasSuffix(e.To, "p.T).Get") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("got %d edges Caller -> T.Get, want 1: %+v", n, g.Edges)
	}
}

// --- RTA ---
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

// RTA must root at init as well as main. A type instantiated in a package-level
// var or in func init — the registry pattern behind database/sql, prometheus and
// cobra — is otherwise never reachable and RTA prunes every edge to it.
func TestRTARootsIncludePackageInit(t *testing.T) {
	files := map[string]string{
		"go.mod": "module example.com/cg\n\ngo 1.26\n",
		"store/store.go": `package store

type Getter interface{ Get() string }

var Registry []Getter

func Register(g Getter) { Registry = append(Registry, g) }
`,
		"impl/impl.go": `package impl

import "example.com/cg/store"

type T struct{}

func (T) Get() string { return "t" }

func init() { store.Register(T{}) }
`,
		"api/api.go": `package api

import "example.com/cg/store"

func ServeAll() string {
	out := ""
	for _, g := range store.Registry {
		out += g.Get()
	}
	return out
}
`,
		"main.go": `package main

import (
	"example.com/cg/api"
	_ "example.com/cg/impl"
)

func main() { println(api.ServeAll()) }
`,
	}
	g := buildModule(t, RTA, files)

	if !hasEdge(g, "api.ServeAll", "impl.T).Get", "Get") {
		t.Errorf("rta pruned an implementer registered in init; edges = %+v", g.Edges)
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

// A package that exists but declares no function bodies yields an empty graph
// rather than an error: the original command emitted a valid empty DOT and exited
// 0. (No package matching the pattern at all is still an error, as before.)
func TestBuildOnPackageWithNoFunctionsIsNotAnError(t *testing.T) {
	g := buildModule(t, Static, map[string]string{
		"go.mod": "module example.com/empty\n\ngo 1.26\n",
		"p/p.go": "package p\n\ntype T struct{ A int }\n",
	})
	if len(g.Funcs) != 0 || len(g.Edges) != 0 {
		t.Errorf("expected an empty graph, got %d funcs / %d edges", len(g.Funcs), len(g.Edges))
	}
}

func TestBuildErrorsWhenNoPackageMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/empty\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(dir, "./...", Static); err == nil {
		t.Error("Build with no matching package succeeded; want an error, as before")
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
