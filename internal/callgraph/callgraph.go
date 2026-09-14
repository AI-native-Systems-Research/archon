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
	// an implementer that is never actually wired up still gets an edge. Measured
	// cost is about 1.5x the static edge count on both archon and BLIS.
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
	case "", "static":
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
	// ID is types.Func.FullName(): stable across runs and unique, so it works as
	// both a map key and a DOT node id.
	ID    string `json:"id"`
	Label string `json:"label"` // pkg.Recv.Method, for display
	Pkg   string `json:"pkg"`
	File  string `json:"file"`
	Lo    int    `json:"lo"` // first line of the declaration
	Hi    int    `json:"hi"` // last line
}

// Edge is a call from one in-module function to another.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Via names the interface method that dispatched the call, empty for a direct
	// call. It is the witness: without it an interface-resolved edge looks
	// identical to a direct one and a reader cannot tell why it exists.
	Via string `json:"via,omitempty"`
}

// Graph is the call graph of one module at one mode. Funcs and Edges are sorted,
// so two builds of the same tree are byte-identical.
type Graph struct {
	Mode  Mode   `json:"mode"`
	Funcs []Func `json:"funcs"`
	Edges []Edge `json:"edges"`
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

	defined, byID := declaredFuncs(pkgs)
	if len(defined) == 0 {
		return nil, fmt.Errorf("no functions with bodies found in %q", pattern)
	}

	edges := map[Edge]bool{}
	for e := range staticEdges(pkgs, defined) {
		edges[e] = true
	}

	if mode != Static {
		iface, err := interfaceEdges(pkgs, defined, mode)
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

	g := &Graph{Mode: mode}
	for _, f := range byID {
		g.Funcs = append(g.Funcs, f)
	}
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

// declaredFuncs finds every function with a body in the root packages. Those are
// the graph's nodes, and membership also serves as the in-module filter: an edge
// is kept only when both ends are here, which excludes stdlib and dependencies
// without needing a module-path prefix test.
func declaredFuncs(pkgs []*packages.Package) (map[*types.Func]bool, map[string]Func) {
	defined := map[*types.Func]bool{}
	byID := map[string]Func{}
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				obj, _ := p.TypesInfo.Defs[fd.Name].(*types.Func)
				if obj == nil {
					continue
				}
				defined[obj] = true
				start := p.Fset.Position(fd.Pos())
				end := p.Fset.Position(fd.End())
				pkgPath := ""
				if obj.Pkg() != nil {
					pkgPath = obj.Pkg().Path()
				}
				byID[obj.FullName()] = Func{
					ID:    obj.FullName(),
					Label: ShortLabel(obj),
					Pkg:   pkgPath,
					File:  start.Filename,
					Lo:    start.Line,
					Hi:    end.Line,
				}
			}
		}
	}
	return defined, byID
}

// staticEdges walks call expressions and keeps those whose callee is a concrete
// in-module function. This is the pre-existing behaviour, unchanged.
func staticEdges(pkgs []*packages.Package, defined map[*types.Func]bool) map[Edge]bool {
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
					if callee := calleeFunc(info, ce.Fun); callee != nil && defined[callee] {
						out[Edge{From: caller.FullName(), To: callee.FullName()}] = true
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
func interfaceEdges(pkgs []*packages.Package, defined map[*types.Func]bool, mode Mode) (map[Edge]bool, error) {
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
			if fn := m.Func("main"); fn != nil {
				roots = append(roots, fn)
			}
		}
		if len(roots) == 0 {
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
		from := declaredFunc(e.Caller.Func, defined)
		to := declaredFunc(e.Callee.Func, defined)
		if from == nil || to == nil {
			return nil
		}
		via := ""
		if m := e.Site.Common().Method; m != nil {
			via = m.Name()
		}
		out[Edge{From: from.FullName(), To: to.FullName(), Via: via}] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk call graph: %w", err)
	}
	return out, nil
}

// declaredFunc maps an SSA function back to the source function it came from, or
// nil when it is not one of ours.
//
// Object() already yields the declaration for a generic instantiation, and nil for
// the synthetic thunks SSA builds for method values, so both are handled by the
// nil check — no Origin() walk is needed. Verified on BLIS: adding one changed the
// edge set by zero.
func declaredFunc(fn *ssa.Function, defined map[*types.Func]bool) *types.Func {
	if fn == nil {
		return nil
	}
	tf, _ := fn.Object().(*types.Func)
	if tf == nil || !defined[tf] {
		return nil
	}
	return tf
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

// ShortLabel renders a function for display: pkg.Func, or pkg.Recv.Method.
func ShortLabel(f *types.Func) string {
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
