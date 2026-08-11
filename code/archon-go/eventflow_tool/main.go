// Command eventflow extracts the discrete-event flow graph of a Go simulator:
// for each event type (a type whose pointer method set includes
// Execute(*Engine)), which other events its handler schedules. "Schedules" is
// detected structurally as "the handler constructs an &EventType{...}", which in
// this codebase is always immediately handed to Schedule/AddEvent. Scheduling
// done inside a helper the handler calls is followed transitively (intra-module
// call graph) and reported as an indirect edge.
//
// Usage: eventflow <module-dir> <pkg-pattern>   e.g.  eventflow ../inference-sim ./sim/...
// Emits Graphviz DOT on stdout.
package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type edge struct{ from, to *types.Named }

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: eventflow <module-dir> <pkg-pattern>")
		os.Exit(2)
	}
	dir, pattern := os.Args[1], os.Args[2]
	directOnly := false
	for _, a := range os.Args[3:] {
		if a == "direct" || a == "--direct-only" {
			directOnly = true
		}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}

	// Pass 1: find event types. An event type is a named type whose pointer
	// method set has Execute(*X) with exactly one pointer param X ("the engine").
	engineOf := map[*types.Named]string{} // event named type -> engine type name
	nameOf := map[*types.Named]string{}   // event named type -> short name
	for _, p := range pkgs {
		for _, name := range p.Types.Scope().Names() {
			tn, ok := p.Types.Scope().Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			ms := types.NewMethodSet(types.NewPointer(named))
			for i := 0; i < ms.Len(); i++ {
				fn, ok := ms.At(i).Obj().(*types.Func)
				if !ok || fn.Name() != "Execute" {
					continue
				}
				sig := fn.Type().(*types.Signature)
				if sig.Params().Len() != 1 {
					continue
				}
				if ptr, ok := sig.Params().At(0).Type().(*types.Pointer); ok {
					if en, ok := ptr.Elem().(*types.Named); ok {
						engineOf[named] = en.Obj().Name()
						nameOf[named] = named.Obj().Name()
					}
				}
			}
		}
	}
	isEvent := func(t types.Type) (*types.Named, bool) {
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
		if n, ok := t.(*types.Named); ok {
			if _, yes := engineOf[n]; yes {
				return n, true
			}
		}
		return nil, false
	}

	// Pass 2: per function, record (a) which event types it constructs and
	// (b) which in-module functions it calls.
	constructs := map[*types.Func]map[*types.Named]bool{}
	calls := map[*types.Func]map[*types.Func]bool{}
	executeOf := map[*types.Named]*types.Func{} // event -> its Execute method

	for _, p := range pkgs {
		info := p.TypesInfo
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				obj, _ := info.Defs[fd.Name].(*types.Func)
				if obj == nil {
					continue
				}
				// record Execute methods of event types
				if fd.Recv != nil && fd.Name.Name == "Execute" {
					if rt := info.Defs[fd.Name].(*types.Func).Type().(*types.Signature).Recv(); rt != nil {
						if n, ok := isEvent(rt.Type()); ok {
							executeOf[n] = obj
						}
					}
				}
				constructs[obj] = map[*types.Named]bool{}
				calls[obj] = map[*types.Func]bool{}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch e := n.(type) {
					case *ast.CompositeLit:
						if t := info.Types[e].Type; t != nil {
							if ev, ok := isEvent(t); ok {
								constructs[obj][ev] = true
							}
						}
					case *ast.CallExpr:
						// also catch construction passed as call arg via a var
						// whose type is an event pointer (belt and suspenders)
						for _, a := range e.Args {
							if at := info.Types[a].Type; at != nil {
								if ev, ok := isEvent(at); ok {
									constructs[obj][ev] = true
								}
							}
						}
						if callee := calleeFunc(info, e.Fun); callee != nil {
							calls[obj][callee] = true
						}
					}
					return true
				})
			}
		}
	}

	// Pass 3: from each event's Execute, collect scheduled events. Direct = built
	// in Execute itself; indirect = built in a transitively-called helper.
	direct := map[edge]bool{}
	indirect := map[edge]bool{}

	for ev, exec := range executeOf {
		// direct
		for t := range constructs[exec] {
			direct[edge{ev, t}] = true
		}
		// transitive via helpers (skip re-entering other Execute methods to keep
		// each handler's own reach)
		seen := map[*types.Func]bool{exec: true}
		stack := []*types.Func{}
		for c := range calls[exec] {
			stack = append(stack, c)
		}
		for len(stack) > 0 {
			fn := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[fn] {
				continue
			}
			seen[fn] = true
			for t := range constructs[fn] {
				e := edge{ev, t}
				if !direct[e] {
					indirect[e] = true
				}
			}
			for c := range calls[fn] {
				if !seen[c] {
					stack = append(stack, c)
				}
			}
		}
	}

	if directOnly {
		indirect = map[edge]bool{}
	}
	emitDOT(engineOf, nameOf, executeOf, direct, indirect, directOnly)
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

func emitDOT(
	engineOf map[*types.Named]string,
	nameOf map[*types.Named]string,
	executeOf map[*types.Named]*types.Func,
	direct, indirect map[edge]bool,
	directOnly bool,
) {
	// group events by engine
	byEngine := map[string][]*types.Named{}
	outdeg := map[*types.Named]int{}
	indeg := map[*types.Named]int{}
	for e := range direct {
		outdeg[e.from]++
		indeg[e.to]++
	}
	for e := range indirect {
		outdeg[e.from]++
		indeg[e.to]++
	}
	for n, eng := range engineOf {
		byEngine[eng] = append(byEngine[eng], n)
	}

	fmt.Println("digraph eventflow {")
	fmt.Println("  rankdir=LR;")
	fmt.Println(`  labelloc="t"; fontname="Helvetica-Bold"; fontsize=18;`)
	if directOnly {
		fmt.Println(`  label="Event-flow graph (exact backbone)   arrow: this handler directly schedules that event";`)
	} else {
		fmt.Println(`  label="Event-flow graph   arrow: this handler schedules that event";`)
	}
	fmt.Println(`  node [shape=box, style="rounded,filled", fontname="Helvetica", fontsize=11, fillcolor="#eef3fb", color="#4a6fa5"];`)
	fmt.Println(`  edge [fontname="Helvetica", fontsize=9];`)

	engines := make([]string, 0, len(byEngine))
	for eng := range byEngine {
		engines = append(engines, eng)
	}
	sort.Strings(engines)
	for ci, eng := range engines {
		nodes := byEngine[eng]
		sort.Slice(nodes, func(i, j int) bool { return nameOf[nodes[i]] < nameOf[nodes[j]] })
		fmt.Printf("  subgraph cluster_e%d {\n", ci)
		fmt.Printf("    label=\"engine: %s\"; labelloc=\"t\"; fontname=\"Helvetica-Bold\"; fontsize=13;\n", eng)
		fmt.Println(`    style="rounded,filled"; color="#b0b0c0"; fillcolor="#fafafd"; margin=14;`)
		for _, n := range nodes {
			nm := nameOf[n]
			fill, pen := "#eef3fb", "#4a6fa5"
			role := ""
			switch {
			case indeg[n] == 0 && outdeg[n] == 0:
				// no edge in the shown graph; in direct-only this means it is only
				// ever scheduled through a helper, so do not call it an entry.
				if directOnly {
					fill, pen, role = "#f6f6f6", "#aaaaaa", "\\n(indirect only)"
				} else {
					fill, pen, role = "#e6f4ea", "#1a7f37", "\\n(entry)"
				}
			case indeg[n] == 0:
				fill, pen, role = "#e6f4ea", "#1a7f37", "\\n(entry)" // external input
			case outdeg[n] == 0:
				fill, pen, role = "#f1f1f1", "#888888", "\\n(terminal)" // schedules nothing
			}
			fmt.Printf("    %q [fillcolor=%q, color=%q, label=\"%s%s\"];\n", nm, fill, pen, nm, role)
		}
		fmt.Println("  }")
	}

	for e := range direct {
		fmt.Printf("  %q -> %q [color=\"#333333\", penwidth=1.6];\n", nameOf[e.from], nameOf[e.to])
	}
	for e := range indirect {
		if direct[e] {
			continue
		}
		fmt.Printf("  %q -> %q [color=\"#8a5cf6\", style=dashed, label=\"via helper\"];\n", nameOf[e.from], nameOf[e.to])
	}

	// legend
	fmt.Println(`  subgraph cluster_legend {`)
	fmt.Println(`    label="Legend"; labelloc="t"; fontname="Helvetica-Bold"; fontsize=12;`)
	fmt.Println(`    style="rounded,filled"; color="#cccccc"; fillcolor="#fbfbfb"; margin=10;`)
	fmt.Println(`    La [label="handler A"]; Lb [label="event B"]; La -> Lb [color="#333333", penwidth=1.6, label=" schedules directly"];`)
	if !directOnly {
		fmt.Println(`    Lc [label="handler C"]; Ld [label="event D"]; Lc -> Ld [color="#8a5cf6", style=dashed, label=" schedules via helper (reachable)"];`)
	}
	fmt.Println(`    Lentry [label="entry", fillcolor="#e6f4ea", color="#1a7f37"]; Lterm [label="terminal", fillcolor="#f1f1f1", color="#888888"]; Lentry -> Lterm [style=invis];`)
	fmt.Println(`  }`)
	fmt.Println("}")
	_ = strings.TrimSpace
}
