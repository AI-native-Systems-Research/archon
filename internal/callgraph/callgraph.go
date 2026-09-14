// Package callgraph extracts a function-level call graph from a Go module: for
// every function or method with a body, which in-module functions it calls.
//
// Three modes trade precision against what they require of the input:
//
//	Static  resolve calls through go/types only. A call is an edge when the
//	        callee is a concrete function defined in one of the loaded
//	        packages, so calls through an interface — which resolve to the
//	        interface method, and it has no body — are dropped.
//	CHA     the static edges plus, for every interface call site, an edge to
//	        each in-module method that could satisfy it. Class Hierarchy
//	        Analysis assumes conservatively that every concrete type is put
//	        into every interface it satisfies, which is what makes it sound on
//	        a partial program: it needs no main, so it is the mode that works
//	        for a library package.
//	RTA     the static edges plus interface call sites resolved from the types
//	        actually reachable from the entry points. More precise than CHA
//	        but it needs a whole program, so Build reports an error when the
//	        pattern matches no main package.
//
// Only invoke-mode call sites are taken from the SSA call graph. A call through
// a function value is also resolved, and that is not dispatch: CHA returns every
// function in the program whose signature matches, and RTA every address-taken
// function of that signature it found reachable. Either way the result is edges
// between packages that never reference each other.
//
// Two kinds of interface call stay out of reach, and Graph.Unresolved lists them
// rather than letting them vanish. A method value (f := s.Get) is dispatched
// inside a wrapper go/ssa synthesises, whose object is the interface method
// itself — and an interface method has no body, so there is no implementation to
// draw the edge from. A closure in a package-level variable initializer sits
// under a synthetic package initializer, which has no object at all. The static
// walk misses both as well, because in each case the callee it resolves is not a
// function declared in the module, so the two halves agree.
package callgraph

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"

	xcallgraph "golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Mode selects how a call site is resolved to a callee.
type Mode int

const (
	Static Mode = iota
	CHA
	RTA
)

func (m Mode) String() string {
	switch m {
	case Static:
		return "static"
	case CHA:
		return "cha"
	case RTA:
		return "rta"
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// ParseMode maps a --mode flag value to a Mode.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "static":
		return Static, nil
	case "cha":
		return CHA, nil
	case "rta":
		return RTA, nil
	}
	return Static, fmt.Errorf("unknown mode %q: want static, cha or rta", s)
}

// Span is the line range a function declaration occupies in a file.
type Span struct {
	File   string
	Lo, Hi int
}

// Edge is a call from one in-module function to another.
type Edge struct{ From, To *types.Func }

// IllTypedPkg is a package go/types could not fully check. It matters beyond
// the usual "results may be incomplete": go/ssa builds no code at all for such
// a package (ssautil skips it), so CHA and RTA see none of its call sites and
// none of the implementations it declares. If it is one of the packages asked
// for, its functions still appear as nodes and the graph looks complete.
type IllTypedPkg struct {
	Path string
	// Cause is true when this package has errors of its own. When it is
	// false the package is ill-typed only because something it imports is,
	// and fixing the causes fixes it. packages.IllTyped is transitive, so
	// importers normally outnumber causes.
	Cause bool
	Err   string
}

// Graph is a call graph over the functions of one module.
//
// Functions are identified by *types.Func. Use ID for anything that has to name
// a function in output, for the reason given at assignIDs.
type Graph struct {
	Defined map[*types.Func]bool // in-module functions we have a body for
	Label   map[*types.Func]string
	Pkg     map[*types.Func]string
	Pos     map[*types.Func]Span
	ID      map[*types.Func]string
	// Edges maps a call to the interface method dispatched at the site that
	// produced it, or to "" for a direct call. One map rather than a set plus a
	// witness table, so an edge cannot carry a witness for an edge that is not
	// there.
	Edges    map[Edge]string
	IllTyped []IllTypedPkg
	// Unresolved describes the interface call sites, in the packages that were
	// asked for, which produced no edge because the caller could not be traced
	// to a function with a body. Each entry names the method that was
	// dispatched, with a position where there is one — a wrapper go/ssa
	// synthesises has no syntax, so for a method value the wrapper's own name
	// is the only handle. Empty in Static mode, which resolves no dispatch and
	// so measures nothing.
	Unresolved []string
}

// Build loads pattern from dir and returns its call graph.
func Build(dir, pattern string, mode Mode) (*Graph, error) {
	pkgs, err := load(dir, pattern)
	if err != nil {
		return nil, err
	}
	g := staticEdges(pkgs)
	var noSSA []string
	if mode != Static {
		if noSSA, err = g.addDispatchEdges(pkgs, mode); err != nil {
			return nil, err
		}
	}
	g.IllTyped = illTyped(pkgs, noSSA)
	g.assignIDs() // last: it needs the final contents of Defined
	return g, nil
}

// loadMode is the same for every builder: NeedDeps already brings the syntax and
// type information of the dependencies, which is what ssautil.AllPackages needs
// on top of what the AST walk needs.
const loadMode = packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
	packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports |
	packages.NeedFiles

func load(dir, pattern string) ([]*packages.Package, error) {
	pkgs, err := packages.Load(&packages.Config{Mode: loadMode, Dir: dir}, pattern)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	// A pattern that matches nothing does not come back as an empty slice: it
	// comes back as one synthetic package carrying the go/list error. Without
	// this the caller gets a valid, empty graph and a zero exit status.
	syntax := 0
	for _, p := range pkgs {
		syntax += len(p.Syntax)
	}
	if syntax == 0 {
		for _, p := range pkgs {
			if len(p.Errors) > 0 {
				return nil, fmt.Errorf("no Go packages matched: %s", p.Errors[0].Msg)
			}
		}
		return nil, fmt.Errorf("no Go packages matched %s", pattern)
	}
	return pkgs, nil
}

// staticEdges walks declaration bodies and records calls whose callee go/types
// resolves to a function declared in one of the loaded packages.
func staticEdges(pkgs []*packages.Package) *Graph {
	g := &Graph{
		Defined: map[*types.Func]bool{},
		Label:   map[*types.Func]string{},
		Pkg:     map[*types.Func]string{},
		Pos:     map[*types.Func]Span{},
		ID:      map[*types.Func]string{},
		Edges:   map[Edge]string{},
	}
	raw := map[Edge]bool{}

	for _, p := range pkgs {
		info := p.TypesInfo
		if info == nil {
			continue
		}
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				obj, _ := info.Defs[fd.Name].(*types.Func)
				if obj == nil {
					continue
				}
				g.Defined[obj] = true
				g.Label[obj] = shortLabel(obj)
				// Unconditional: a function declared in a package that was
				// type-checked always has one, and emitDOT's clustering relies
				// on every function in Defined having an entry here.
				g.Pkg[obj] = obj.Pkg().Path()
				s := p.Fset.Position(fd.Pos())
				e := p.Fset.Position(fd.End())
				g.Pos[obj] = Span{s.Filename, s.Line, e.Line}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					ce, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if callee := calleeFunc(info, ce.Fun); callee != nil {
						raw[Edge{obj, callee}] = true
					}
					return true
				})
			}
		}
	}

	// keep only edges whose callee is also in-module
	for e := range raw {
		if g.Defined[e.To] {
			g.Edges[e] = "" // a direct call has no dispatching method
		}
	}
	return g
}

// calleeFunc resolves a call target to an in-package *types.Func when possible.
func calleeFunc(info *types.Info, fun ast.Expr) *types.Func {
	switch f := fun.(type) {
	case *ast.Ident:
		if fn, ok := info.Uses[f].(*types.Func); ok {
			return fn
		}
	case *ast.SelectorExpr:
		// This branch is not known to be load-bearing: removing it changes no
		// edge on the fixtures or on a 1400-edge module, because the branch
		// below resolves the same calls. It is kept because this resolver is
		// the pre-existing static path and this change is not the place to
		// alter what static resolves.
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

// addDispatchEdges adds an edge for every interface call site whose caller and
// possible callee are both in-module.
// It returns the paths of the packages SSA could not build, which is a stronger
// signal than packages.IllTyped: AllPackages also skips a package that has no
// type information at all.
func (g *Graph) addDispatchEdges(pkgs []*packages.Package, mode Mode) ([]string, error) {
	asked := map[string]bool{}
	for _, p := range pkgs {
		asked[p.PkgPath] = true
	}

	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()

	var noSSA []string
	for i, sp := range ssaPkgs {
		if sp == nil {
			noSSA = append(noSSA, pkgs[i].PkgPath)
		}
	}

	var cg *xcallgraph.Graph
	switch mode {
	case CHA:
		cg = cha.CallGraph(prog)
	case RTA:
		roots := rtaRoots(prog)
		if len(roots) == 0 {
			return nil, fmt.Errorf("mode=rta needs an entry point: no main package among the matched packages; use mode=cha for a library")
		}
		cg = rta.Analyze(roots, true).CallGraph
	default:
		return nil, fmt.Errorf("addDispatchEdges: mode %s resolves no dispatch", mode)
	}

	unresolved := map[string]bool{}
	err := xcallgraph.GraphVisitEdges(cg, func(e *xcallgraph.Edge) error {
		if e.Site == nil {
			return nil
		}
		call := e.Site.Common()
		if !call.IsInvoke() {
			return nil
		}
		from, to := declaredFunc(e.Caller.Func), declaredFunc(e.Callee.Func)
		if from == nil || !g.Defined[from] {
			// The call site is real but there is no function with a body to
			// draw it from. Worth recording when it is in a package we were
			// asked about; in a dependency it is noise.
			if asked[sitePkg(e.Caller.Func, from)] {
				unresolved[describeSite(prog, e)] = true
			}
			return nil
		}
		if to == nil || !g.Defined[to] {
			return nil // callee is outside the module
		}
		ed := Edge{from, to}
		w := shortLabel(call.Method)
		// Several call sites can produce one edge; take the lowest name so the
		// witness does not depend on visit order.
		if cur, ok := g.Edges[ed]; !ok || cur == "" || w < cur {
			g.Edges[ed] = w
		}
		return nil
	})
	for d := range unresolved {
		g.Unresolved = append(g.Unresolved, d)
	}
	sort.Strings(g.Unresolved)
	return noSSA, err
}

// rtaRoots returns the entry points of every main package: both main and the
// package initializer, because the registry pattern behind database/sql,
// prometheus and cobra does its work in init and nothing reaches it from main.
func rtaRoots(prog *ssa.Program) []*ssa.Function {
	mains := ssautil.MainPackages(prog.AllPackages())
	sort.Slice(mains, func(i, j int) bool { return mains[i].Pkg.Path() < mains[j].Pkg.Path() })
	var roots []*ssa.Function
	for _, m := range mains {
		for _, name := range []string{"init", "main"} {
			if f := m.Func(name); f != nil {
				roots = append(roots, f)
			}
		}
	}
	return roots
}

// describeSite says what was dispatched and, where SSA has one, where. A
// synthesised wrapper carries no position, so it is named instead: the $bound
// suffix is what a method value looks like.
func describeSite(prog *ssa.Program, e *xcallgraph.Edge) string {
	what := shortLabel(e.Site.Common().Method)
	if pos := prog.Fset.Position(e.Site.Pos()); pos.IsValid() {
		return fmt.Sprintf("%s at %s:%d", what, pos.Filename, pos.Line)
	}
	return fmt.Sprintf("%s in %s", what, e.Caller.Func.String())
}

// sitePkg names the package a call site belongs to, for a caller that could not
// be traced to a declaration. A synthesised wrapper has no ssa.Package but its
// object is the interface method, which does have one.
func sitePkg(fn *ssa.Function, from *types.Func) string {
	if from != nil && from.Pkg() != nil {
		return from.Pkg().Path()
	}
	if fn.Pkg != nil {
		return fn.Pkg.Pkg.Path()
	}
	return ""
}

// declaredFunc maps an SSA function to the declaration it belongs to. A
// closure, and the body of a go or defer statement, has no object of its own,
// so its calls are attributed to the enclosing declaration — which is where
// the static AST walk already puts the direct calls made in the same place.
func declaredFunc(fn *ssa.Function) *types.Func {
	for f := fn; f != nil; f = f.Parent() {
		if obj, ok := f.Object().(*types.Func); ok && obj != nil {
			// An instantiated generic maps back to the generic declaration.
			return obj.Origin()
		}
	}
	return nil
}

// illTyped reports the packages go/types could not fully check, causes first. A
// cause is reported wherever it is, including in a dependency, because that is
// the package someone can fix. An importer is only reported when it is one of
// the packages asked for: naming every dependency that merely inherited the
// problem buries the one that caused it.
func illTyped(pkgs []*packages.Package, noSSA []string) []IllTypedPkg {
	asked := map[string]bool{}
	for _, p := range pkgs {
		asked[p.PkgPath] = true
	}
	reported := map[string]bool{}
	var causes, importers []IllTypedPkg
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if !p.IllTyped {
			return
		}
		reported[p.PkgPath] = true
		it := IllTypedPkg{Path: p.PkgPath}
		if len(p.Errors) > 0 {
			it.Cause = true
			it.Err = p.Errors[0].Msg
			if n := len(p.Errors) - 1; n > 0 {
				it.Err += fmt.Sprintf(" (and %d more)", n)
			}
			causes = append(causes, it)
			return
		}
		if asked[p.PkgPath] {
			importers = append(importers, it)
		}
	})
	// Belt and braces: a package SSA refused to build that go/types did not call
	// ill-typed would have no type information at all. No input in this repo
	// reaches this, and it exists so that the assumption fails loudly rather
	// than by under-reporting.
	for _, path := range noSSA {
		if !reported[path] {
			causes = append(causes, IllTypedPkg{Path: path, Cause: true, Err: "no type information, so no SSA was built"})
		}
	}
	// packages.Visit already walks imports in sorted order, so these sorts are
	// not observable on any input I could construct — they are here so the
	// report does not depend on that being true of a postorder walk over
	// several roots.
	sort.Slice(causes, func(i, j int) bool { return causes[i].Path < causes[j].Path })
	sort.Slice(importers, func(i, j int) bool { return importers[i].Path < importers[j].Path })
	return append(causes, importers...)
}

// assignIDs gives every function a name that is unique within the graph.
// types.Func.FullName is not: every func init() in a package renders as
// <pkg>.init, as does every func _(), and merging them into one node would
// discard their positions and so empty any position-based selection. Only
// colliding names get the #N suffix, ordered by position, so every other ID is
// the FullName the graph has always used.
func (g *Graph) assignIDs() {
	byName := map[string][]*types.Func{}
	for f := range g.Defined {
		byName[f.FullName()] = append(byName[f.FullName()], f)
	}
	for name, fs := range byName {
		if len(fs) == 1 {
			g.ID[fs[0]] = name
			continue
		}
		sort.Slice(fs, func(i, j int) bool {
			a, b := g.Pos[fs[i]], g.Pos[fs[j]]
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Lo < b.Lo
		})
		for i, f := range fs {
			g.ID[f] = fmt.Sprintf("%s#%d", name, i+1)
		}
	}
}

// SortedEdges returns the edges in a deterministic order. callgraph.Graph and
// the edge set are both map-backed, so without this two runs on identical input
// produce the same graph in a different order and every diff between them is
// noise.
func (g *Graph) SortedEdges() []Edge {
	out := make([]Edge, 0, len(g.Edges))
	for e := range g.Edges {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := g.ID[out[i].From], g.ID[out[j].From]; a != b {
			return a < b
		}
		return g.ID[out[i].To] < g.ID[out[j].To]
	})
	return out
}
