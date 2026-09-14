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
//	        Analysis computes the whole implements relation up front, which
//	        makes it sound on partial programs: it needs no main, so it is
//	        the mode that works for a library package.
//	RTA     the static edges plus interface call sites resolved from the types
//	        actually reachable from the entry points. More precise than CHA
//	        but it needs a whole program, so Build reports an error when the
//	        pattern matches no main package.
//
// Only invoke-mode call sites are taken from SSA. SSA also resolves calls made
// through a function value, and those are not dispatch: it attributes them to
// whatever function the value points at, which manufactures edges between
// packages that never reference each other.
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
// a package, so CHA and RTA see none of its call sites and none of the
// implementations it declares, while its functions still appear as nodes.
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
// Functions are identified by *types.Func. Use ID for anything that has to
// name a function in output: types.Func.FullName is not unique — every
// func init() in a package renders identically — so ID appends a discriminator
// where it must to keep distinct functions distinct.
type Graph struct {
	Defined map[*types.Func]bool // in-module functions we have a body for
	Label   map[*types.Func]string
	Pkg     map[*types.Func]string
	Pos     map[*types.Func]Span
	ID      map[*types.Func]string
	Edges   map[Edge]bool
	// Witness names the interface method dispatched at the call site that
	// produced an edge. Only invoke-derived edges have one, so its presence
	// also reports that an edge came from dispatch rather than a direct call.
	Witness  map[Edge]string
	IllTyped []IllTypedPkg
}

// Build loads pattern from dir and returns its call graph.
func Build(dir, pattern string, mode Mode) (*Graph, error) {
	pkgs, err := load(dir, pattern, mode)
	if err != nil {
		return nil, err
	}
	g := staticEdges(pkgs)
	g.IllTyped = illTyped(pkgs)
	if mode != Static {
		if err := g.addDispatchEdges(pkgs, mode); err != nil {
			return nil, err
		}
	}
	g.assignIDs()
	return g, nil
}

// loadMode is the packages.Load mode of the static AST walk.
const loadMode = packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
	packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports |
	packages.NeedFiles

func load(dir, pattern string, mode Mode) ([]*packages.Package, error) {
	m := loadMode
	if mode != Static {
		// ssautil.AllPackages needs the syntax and type info of every
		// dependency, not just of the matched packages.
		m |= packages.NeedCompiledGoFiles
	}
	pkgs, err := packages.Load(&packages.Config{Mode: m, Dir: dir}, pattern)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %s", pattern)
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
		Edges:   map[Edge]bool{},
		Witness: map[Edge]string{},
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
				if obj.Pkg() != nil {
					g.Pkg[obj] = obj.Pkg().Path()
				}
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
			g.Edges[e] = true
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
func (g *Graph) addDispatchEdges(pkgs []*packages.Package, mode Mode) error {
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()

	var cg *xcallgraph.Graph
	switch mode {
	case CHA:
		cg = cha.CallGraph(prog)
	case RTA:
		roots := rtaRoots(prog)
		if len(roots) == 0 {
			return fmt.Errorf("mode=rta needs an entry point: no main package among the matched packages; use mode=cha for a library")
		}
		cg = rta.Analyze(roots, true).CallGraph
	default:
		return fmt.Errorf("addDispatchEdges: mode %s resolves no dispatch", mode)
	}

	return xcallgraph.GraphVisitEdges(cg, func(e *xcallgraph.Edge) error {
		if e.Site == nil {
			return nil
		}
		call := e.Site.Common()
		if !call.IsInvoke() {
			return nil
		}
		from, to := declaredFunc(e.Caller.Func), declaredFunc(e.Callee.Func)
		if from == nil || to == nil || !g.Defined[from] || !g.Defined[to] {
			return nil
		}
		ed := Edge{from, to}
		g.Edges[ed] = true
		w := shortLabel(call.Method)
		// Several call sites can produce one edge; take the lowest name so
		// the witness does not depend on visit order.
		if cur, ok := g.Witness[ed]; !ok || w < cur {
			g.Witness[ed] = w
		}
		return nil
	})
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

// illTyped reports the packages go/types could not fully check, causes first.
func illTyped(pkgs []*packages.Package) []IllTypedPkg {
	var causes, importers []IllTypedPkg
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if !p.IllTyped {
			return
		}
		it := IllTypedPkg{Path: p.PkgPath}
		if len(p.Errors) > 0 {
			it.Cause = true
			it.Err = p.Errors[0].Error()
			causes = append(causes, it)
			return
		}
		importers = append(importers, it)
	})
	sort.Slice(causes, func(i, j int) bool { return causes[i].Path < causes[j].Path })
	sort.Slice(importers, func(i, j int) bool { return importers[i].Path < importers[j].Path })
	return append(causes, importers...)
}

// assignIDs gives every function a name that is unique within the graph.
// types.Func.FullName is not: a package's init functions all render as
// <pkg>.init, and collapsing them would merge unrelated functions into one
// node. Only colliding names get the #N suffix, so every other ID is the
// FullName the graph has always used.
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
// the edge set are both map-backed, so anything emitting them must sort first.
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
