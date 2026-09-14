// Package callgraph builds a function-altitude call graph of a Go module.
//
// The default Static mode records a call only when the callee is a concrete
// function with a body in the loaded set, which drops every call made through an
// interface: the callee resolves to the interface method, which has no body. CHA
// mode adds those back.
//
// Scope note: these are LEAF edges. They are deliberately not aggregated into the
// package-altitude graph that internal/extract produces. A caller reaching an
// implementation through an interface does not depend on that implementation —
// avoiding exactly that dependency is what the interface is for — so promoting
// these edges to package arrows would erase the decoupling it buys. What they are
// for is the finer question: "if I change this method, who is affected?"
package callgraph

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Mode selects how a call through an interface is resolved.
type Mode int

const (
	// Static resolves nothing: a call is an edge only when the callee is a
	// concrete in-module function. Interface calls are dropped.
	Static Mode = iota

	// CHA (class hierarchy analysis) computes the implements relation up front and
	// resolves an interface call to EVERY in-module implementer of that method.
	//
	// CHA is the right default because it is sound on partial programs — archon
	// must analyse library packages that have no main at all. It over-approximates:
	// an implementer that is never actually wired up still gets an edge. Cost
	// depends entirely on how much the module dispatches dynamically — measured at
	// 1.28x the static edge count on BLIS, and no extra edges at all on archon
	// itself, whose non-test code barely uses interfaces.
	CHA

	// RTA (rapid type analysis) prunes implementers whose type is never
	// instantiated along a path reachable from an entry point, so it is more
	// precise than CHA — where it applies at all.
	//
	// It requires a whole program with a main, and it is only as good as what it
	// can reach statically. Measured on BLIS, a Cobra CLI whose main is just
	// cmd.Execute(), RTA finds ONE edge: every command is registered as a func
	// value it cannot follow. Never make it the sole builder.
	RTA
)

func (m Mode) String() string {
	switch m {
	case CHA:
		return "cha"
	case RTA:
		return "rta"
	default:
		return "static"
	}
}

// ParseMode maps a flag value to a Mode. Unknown values are an error rather than
// a silent fallback to Static, which would misreport a graph as complete.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "static":
		return Static, nil
	case "cha":
		return CHA, nil
	case "rta":
		return RTA, nil
	}
	return Static, fmt.Errorf("unknown mode %q (want static, cha, or rta)", s)
}

// Func is one in-module function that has a body.
type Func struct {
	// ID identifies the function: types.Func.FullName(), plus a "#N" suffix when
	// that name repeats in a package (every func init() renders the same). Stable
	// across runs, so it serves as both a map key and a DOT node id. "#" cannot
	// occur in a Go identifier, so a suffixed id can never collide with a real name.
	ID    string
	Label string // pkg.Recv.Method, for display
	Pkg   string
	File  string
	Lo    int // first line of the declaration
	Hi    int // last line
}

// Edge is a call from one in-module function to another.
type Edge struct {
	From string
	To   string
	// Via names the interface method that dispatched the call, empty for a direct
	// call. It is the witness: without it an interface-resolved edge looks
	// identical to a direct one and a reader cannot tell why it exists.
	Via string
}

// Graph is the call graph of one module at one mode. Funcs and Edges are sorted,
// so two builds of the same tree are byte-identical.
type Graph struct {
	Mode  Mode
	Funcs []Func
	Edges []Edge

	// IllTyped names matched packages that did not type-check. They are the reason
	// a graph can be quietly incomplete: SSA builds nothing for such a package, so
	// in CHA or RTA mode it contributes no call sites and none of its types count
	// as implementers — while the AST walk still lists its functions as nodes. The
	// result looks complete. Callers must surface this; Build will not pretend the
	// graph is whole.
	//
	// Own-error packages come first, because packages.IllTyped is TRANSITIVE: a
	// package whose import fails is flagged with no errors of its own, so importers
	// usually outnumber causes and truncating an alphabetical list hides every
	// actual error.
	IllTyped []IllTypedPkg
}

// Build loads the module at dir matching pattern and returns its call graph.
func Build(dir, pattern string, mode Mode) (*Graph, error) {
	// LoadAllSyntax (not the narrower set the static walk needs) because
	// ssautil.AllPackages requires dependency syntax. Using one load for both
	// modes keeps the static edge set identical across modes; the cost is a slower
	// load, which is why internal/extract keeps its own narrower config.
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Dir: dir}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %q in %s", pattern, dir)
	}

	illTyped := illTypedPackages(pkgs)

	ids, funcs := declaredFuncs(pkgs)
	// No error when a matched package has no function bodies: the original emitted
	// a valid empty graph and exited 0, and callers rely on that.

	edges := map[Edge]bool{}
	for e := range staticEdges(pkgs, ids) {
		edges[e] = true
	}

	if mode != Static {
		iface, err := interfaceEdges(pkgs, ids, edges, illTyped, mode)
		if err != nil {
			return nil, err
		}
		// Union, never replacement: the resolved edges are ADDED to the static
		// ones, so a richer mode can only ever be a superset. That makes "no edge
		// is lost" true by construction rather than by testing for it.
		for e := range iface {
			edges[e] = true
		}
	}

	g := &Graph{Mode: mode, Funcs: funcs, IllTyped: illTyped}
	for e := range edges {
		g.Edges = append(g.Edges, e)
	}
	g.sort()
	return g, nil
}

// sort normalizes ordering. Both collections come out of maps, and archon's
// output is guaranteed byte-identical for identical input.
func (g *Graph) sort() {
	sort.Slice(g.Funcs, func(i, j int) bool { return g.Funcs[i].ID < g.Funcs[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		if g.Edges[i].To != g.Edges[j].To {
			return g.Edges[i].To < g.Edges[j].To
		}
		return g.Edges[i].Via < g.Edges[j].Via
	})
}

// IllTypedPkg is one package that failed to type-check.
type IllTypedPkg struct {
	Path string
	Msg  string
	// Own is true when the package has errors of its own, false when it was only
	// flagged because something it imports failed. Only Own packages have a cause
	// worth reading.
	Own bool
}

// illTypedPackages lists matched packages that failed to type-check, causes first.
func illTypedPackages(pkgs []*packages.Package) []IllTypedPkg {
	var out []IllTypedPkg
	for _, p := range pkgs {
		if !p.IllTyped && len(p.Errors) == 0 {
			continue
		}
		e := IllTypedPkg{Path: p.PkgPath}
		// Own means "has an error of its own", so report the first real one. A
		// ListError ("no Go files", "build constraints exclude all Go files") is not
		// a type error but is still the package's own problem, so it counts.
		if len(p.Errors) > 0 {
			e.Own = true
			e.Msg = p.Errors[0].Msg
		} else {
			e.Msg = "an import did not type-check"
		}
		out = append(out, e)
	}
	// Causes before importers, then by path. Truncating the other way round drops
	// exactly the lines a reader needs.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Own != out[j].Own {
			return out[i].Own
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// OwnErrors counts packages with errors of their own, as opposed to those flagged
// only because an import failed.
func (g *Graph) OwnErrors() int {
	n := 0
	for _, e := range g.IllTyped {
		if e.Own {
			n++
		}
	}
	return n
}

// declaredFuncs finds every function with a body in the root packages. Those are
// the graph's nodes, and membership also serves as the in-module filter: an edge
// is kept only when both ends are here, which excludes stdlib and dependencies
// without needing a module-path prefix test.
func declaredFuncs(pkgs []*packages.Package) (map[*types.Func]string, []Func) {
	type decl struct {
		obj  *types.Func
		fn   Func
		base string
		col  int // start column: the only thing separating two decls on one line
	}
	var decls []decl
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			for _, d := range file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				obj, _ := p.TypesInfo.Defs[fd.Name].(*types.Func)
				if obj == nil {
					continue
				}
				start := p.Fset.Position(fd.Pos())
				end := p.Fset.Position(fd.End())
				pkgPath := ""
				if obj.Pkg() != nil {
					pkgPath = obj.Pkg().Path()
				}
				decls = append(decls, decl{
					obj:  obj,
					base: obj.FullName(),
					col:  start.Column,
					fn: Func{
						Label: shortLabel(obj),
						Pkg:   pkgPath,
						File:  start.Filename,
						Lo:    start.Line,
						Hi:    end.Line,
					},
				})
			}
		}
	}

	// types.Func.FullName() is NOT unique: every func init() in a package renders
	// as "pkg/path.init", and so does every func _(). Collapsing them would keep
	// one declaration's position and discard the rest, which silently empties the
	// --since view for any package that registers things in two init functions.
	//
	// The sort makes the numbering an explicit function of (name, file, line). It
	// is not load-bearing today — decls are collected into a slice in traversal
	// order, and packages.Load already yields files alphabetically, so the two
	// orders coincide and no fixture can tell them apart. It is here so the ids,
	// which are DOT node names and --since keys, do not silently renumber if that
	// undocumented loader ordering ever changes.
	sort.Slice(decls, func(i, j int) bool {
		if decls[i].base != decls[j].base {
			return decls[i].base < decls[j].base
		}
		if decls[i].fn.File != decls[j].fn.File {
			return decls[i].fn.File < decls[j].fn.File
		}
		if decls[i].fn.Lo != decls[j].fn.Lo {
			return decls[i].fn.Lo < decls[j].fn.Lo
		}
		// `func init(){}; func init(){}` on one line is legal, and ties Lo AND Hi —
		// both declarations start and end on that line. Column is what separates
		// them, so sort.Slice being unstable no longer matters.
		return decls[i].col < decls[j].col
	})
	count := map[string]int{}
	for _, d := range decls {
		count[d.base]++
	}

	ids := make(map[*types.Func]string, len(decls))
	funcs := make([]Func, 0, len(decls))
	seen := map[string]int{}
	for _, d := range decls {
		id := d.base
		if count[d.base] > 1 {
			seen[d.base]++
			id = fmt.Sprintf("%s#%d", d.base, seen[d.base])
		}
		ids[d.obj] = id
		d.fn.ID = id
		funcs = append(funcs, d.fn)
	}
	return ids, funcs
}

// staticEdges walks call expressions and keeps those whose callee is a concrete
// in-module function. This is the pre-existing behaviour, unchanged.
func staticEdges(pkgs []*packages.Package, ids map[*types.Func]string) map[Edge]bool {
	out := map[Edge]bool{}
	for _, p := range pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				caller, _ := info.Defs[fd.Name].(*types.Func)
				if caller == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					ce, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					callee := calleeFunc(info, ce.Fun)
					if callee == nil {
						return true
					}
					if from, ok := ids[caller]; ok {
						if to, ok := ids[callee]; ok {
							out[Edge{From: from, To: to}] = true
						}
					}
					return true
				})
			}
		}
	}
	return out
}

// interfaceEdges resolves calls made through an interface, which staticEdges
// necessarily drops.
//
// Only invoke-mode call sites are taken. The builders also produce direct calls,
// but those are already covered by the static walk, and importing SSA's view of
// them wholesale would drag in synthetic wrappers and init functions that no
// reader recognises.
// staticSeen is read-only: it is consulted so a dispatched edge is not added for a
// pair already recorded as a direct call.
func interfaceEdges(pkgs []*packages.Package, ids map[*types.Func]string, staticSeen map[Edge]bool, illTyped []IllTypedPkg, mode Mode) (map[Edge]bool, error) {
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()

	var cg *callgraph.Graph
	switch mode {
	case CHA:
		cg = cha.CallGraph(prog)
	case RTA:
		mains := ssautil.MainPackages(prog.AllPackages())
		var roots []*ssa.Function
		for _, m := range mains {
			// init as well as main, matching x/tools' own callgraph and deadcode
			// commands. A type instantiated in a package-level var or in func init
			// — the registry pattern behind database/sql, prometheus and cobra — is
			// otherwise never marked reachable and RTA prunes every edge to it.
			for _, name := range []string{"init", "main"} {
				if fn := m.Func(name); fn != nil {
					roots = append(roots, fn)
				}
			}
		}
		if len(roots) == 0 {
			// An ill-typed main package has no SSA package either, so it is
			// indistinguishable here from not existing. Saying "none found" and
			// suggesting cha would be doubly wrong, since cha is equally blind to it.
			if len(illTyped) > 0 {
				return nil, fmt.Errorf("mode rta found no main package, and %d matched package(s) "+
					"did not type-check so no SSA was built for them (first: %s: %s)",
					len(illTyped), illTyped[0].Path, illTyped[0].Msg)
			}
			return nil, fmt.Errorf("mode rta needs a main package; none found (use cha for a library)")
		}
		cg = rta.Analyze(roots, true).CallGraph
	default:
		return nil, fmt.Errorf("interfaceEdges called with mode %v", mode)
	}

	out := map[Edge]bool{}
	err := callgraph.GraphVisitEdges(cg, func(e *callgraph.Edge) error {
		if e.Site == nil || !e.Site.Common().IsInvoke() {
			return nil // direct call: the static walk already has it
		}
		from, ok1 := ids[declaredFunc(e.Caller.Func)]
		to, ok2 := ids[declaredFunc(e.Callee.Func)]
		if !ok1 || !ok2 {
			return nil
		}
		// Already recorded as a direct call: adding the dispatched variant would
		// draw a second arrow between the same pair and double-count the edge.
		if staticSeen[Edge{From: from, To: to}] {
			return nil
		}
		via := ""
		if m := e.Site.Common().Method; m != nil {
			via = m.Name()
		}
		out[Edge{From: from, To: to, Via: via}] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk call graph: %w", err)
	}
	return out, nil
}

// declaredFunc maps an SSA function back to the declaration it belongs to, or nil.
//
// An anonymous function — a closure, a goroutine or defer body, an http handler
// literal — has no Object() of its own, so it must be attributed to its enclosing
// declaration. Without that walk an interface call inside `go func(){ g.Get() }()`
// is dropped, which is the most common shape there is, while the static AST walk
// already attributes direct calls inside literals to the enclosing FuncDecl. The
// two halves have to agree.
//
// Wrappers and bound thunks do NOT reach the walk at all: go/ssa sets their
// object to the declared method, so Object() is non-nil. What keeps them out is
// that ids holds only declarations with a body, so an interface-method wrapper
// resolves to a body-less method and is dropped. A wrapper for a promoted concrete
// method does resolve, to the embedded declaration, which is the useful answer.
// Object() already yields the declaration for a generic instantiation; verified on
// BLIS that an extra Origin() walk changes nothing.
func declaredFunc(fn *ssa.Function) *types.Func {
	for fn != nil {
		if tf, ok := fn.Object().(*types.Func); ok && tf != nil {
			return tf
		}
		fn = fn.Parent()
	}
	return nil
}

// calleeFunc resolves a call target to a *types.Func when it is statically known.
func calleeFunc(info *types.Info, fun ast.Expr) *types.Func {
	switch f := fun.(type) {
	case *ast.Ident:
		if fn, ok := info.Uses[f].(*types.Func); ok {
			return fn
		}
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[f]; ok {
			if fn, ok := sel.Obj().(*types.Func); ok {
				return fn
			}
		}
		if fn, ok := info.Uses[f.Sel].(*types.Func); ok {
			return fn
		}
	}
	return nil
}

// shortLabel renders a function for display: pkg.Func, or pkg.Recv.Method.
func shortLabel(f *types.Func) string {
	pkg := ""
	if f.Pkg() != nil {
		pkg = f.Pkg().Name()
	}
	recv := ""
	if sig, ok := f.Type().(*types.Signature); ok && sig.Recv() != nil {
		t := sig.Recv().Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if n, ok := t.(*types.Named); ok {
			recv = n.Obj().Name()
		}
	}
	if recv != "" {
		return fmt.Sprintf("%s.%s.%s", pkg, recv, f.Name())
	}
	return fmt.Sprintf("%s.%s", pkg, f.Name())
}
