// Command callgraph extracts a function-level call graph from a Go module: for
// every function/method with a body, which in-module functions it calls. Calls
// are resolved through go/types, so a call is an edge only when the callee is a
// concrete function defined in one of the loaded packages. Calls through an
// interface resolve to the interface method (which has no body) and are dropped,
// so this is a static direct-call graph, not a devirtualized one.
//
// Two views:
//   full          every in-module function, clustered by package.
//   delta-scoped  pass --since <ref>; the tool diffs <ref> against the working
//                 tree, marks the functions whose bodies overlap changed lines,
//                 and draws only those plus their callers/callees out to --depth
//                 hops (default 1). Changed functions are highlighted.
//
// Usage:
//   callgraph <module-dir> <pkg-pattern> [--since <ref>] [--depth N]
//   e.g.  callgraph ../inference-sim ./...                 # full graph
//         callgraph ../inference-sim ./... --since HEAD~1  # what the last commit touched
// Emits Graphviz DOT on stdout; a one-line summary on stderr.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

type edge struct{ from, to *types.Func }
type span struct {
	file   string
	lo, hi int
}
type iv struct{ lo, hi int }

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: callgraph <module-dir> <pkg-pattern> [--since <ref>] [--depth N]")
		os.Exit(2)
	}
	dir, pattern := os.Args[1], os.Args[2]
	sinceRef := ""
	depth := 1
	for i := 3; i < len(os.Args); i++ {
		a := os.Args[i]
		switch {
		case a == "--since" && i+1 < len(os.Args):
			sinceRef = os.Args[i+1]
			i++
		case strings.HasPrefix(a, "--since="):
			sinceRef = strings.TrimPrefix(a, "--since=")
		case a == "--depth" && i+1 < len(os.Args):
			depth, _ = strconv.Atoi(os.Args[i+1])
			i++
		case strings.HasPrefix(a, "--depth="):
			depth, _ = strconv.Atoi(strings.TrimPrefix(a, "--depth="))
		}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports |
			packages.NeedFiles,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
	if len(pkgs) == 0 {
		fmt.Fprintln(os.Stderr, "no packages matched", pattern)
		os.Exit(1)
	}

	defined := map[*types.Func]bool{} // functions we have a body for (in scope)
	label := map[*types.Func]string{}
	pkgOf := map[*types.Func]string{}
	pos := map[*types.Func]span{}
	rawEdges := map[edge]bool{}

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
				defined[obj] = true
				label[obj] = shortLabel(obj)
				if obj.Pkg() != nil {
					pkgOf[obj] = obj.Pkg().Path()
				}
				s := p.Fset.Position(fd.Pos())
				e := p.Fset.Position(fd.End())
				pos[obj] = span{s.Filename, s.Line, e.Line}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					ce, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if callee := calleeFunc(info, ce.Fun); callee != nil {
						rawEdges[edge{obj, callee}] = true
					}
					return true
				})
			}
		}
	}

	// keep only edges whose callee is also in-module and not a self-loop noise
	edges := map[edge]bool{}
	for e := range rawEdges {
		if defined[e.to] {
			edges[e] = true
		}
	}

	// delta scope: which functions did <ref>..worktree touch?
	changed := map[*types.Func]bool{}
	if sinceRef != "" {
		ranges := gitChangedRanges(dir, sinceRef)
		for f, sp := range pos {
			for file, ivs := range ranges {
				if !sameFile(sp.file, file) {
					continue
				}
				for _, r := range ivs {
					if sp.lo <= r.hi && r.lo <= sp.hi {
						changed[f] = true
					}
				}
			}
		}
	}

	// visible set
	visible := map[*types.Func]bool{}
	if sinceRef != "" {
		adjOut := map[*types.Func][]*types.Func{}
		adjIn := map[*types.Func][]*types.Func{}
		for e := range edges {
			adjOut[e.from] = append(adjOut[e.from], e.to)
			adjIn[e.to] = append(adjIn[e.to], e.from)
		}
		frontier := map[*types.Func]bool{}
		for f := range changed {
			visible[f] = true
			frontier[f] = true
		}
		for d := 0; d < depth; d++ {
			next := map[*types.Func]bool{}
			for f := range frontier {
				for _, g := range adjOut[f] {
					if !visible[g] {
						visible[g] = true
						next[g] = true
					}
				}
				for _, g := range adjIn[f] {
					if !visible[g] {
						visible[g] = true
						next[g] = true
					}
				}
			}
			frontier = next
		}
	} else {
		for f := range defined {
			visible[f] = true
		}
	}

	nEdges := 0
	for e := range edges {
		if visible[e.from] && visible[e.to] {
			nEdges++
		}
	}
	mode := "full"
	if sinceRef != "" {
		mode = fmt.Sprintf("delta since %s (%d changed fn, depth %d)", sinceRef, len(changed), depth)
	}
	fmt.Fprintf(os.Stderr, "callgraph: %d functions in module, %d visible, %d edges [%s]\n",
		len(defined), len(visible), nEdges, mode)

	emitDOT(visible, changed, edges, label, pkgOf, sinceRef != "", sinceRef, depth)
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

// gitChangedRanges diffs ref against the working tree and returns, per file
// (repo-relative path), the new-side line intervals that changed.
func gitChangedRanges(dir, ref string) map[string][]iv {
	out := map[string][]iv{}
	cmd := exec.Command("git", "-C", dir, "diff", "--unified=0", ref, "--", "*.go")
	b, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "git diff failed:", err)
		return out
	}
	hunk := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	cur := ""
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "+++ ") {
			p := strings.TrimPrefix(line, "+++ ")
			p = strings.TrimPrefix(p, "b/")
			cur = p
			continue
		}
		if m := hunk.FindStringSubmatch(line); m != nil {
			start, _ := strconv.Atoi(m[1])
			cnt := 1
			if m[2] != "" {
				cnt, _ = strconv.Atoi(m[2])
			}
			if cnt == 0 {
				cnt = 1
			}
			out[cur] = append(out[cur], iv{start, start + cnt - 1})
		}
	}
	return out
}

func sameFile(abs, rel string) bool {
	return strings.HasSuffix(filepath.ToSlash(abs), filepath.ToSlash(rel))
}

func emitDOT(
	visible, changed map[*types.Func]bool,
	edges map[edge]bool,
	label, pkgOf map[*types.Func]string,
	delta bool, sinceRef string, depth int,
) {
	// group visible functions by package
	byPkg := map[string][]*types.Func{}
	for f := range visible {
		byPkg[pkgOf[f]] = append(byPkg[pkgOf[f]], f)
	}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	fmt.Println("digraph callgraph {")
	fmt.Println("  rankdir=LR;")
	fmt.Println(`  labelloc="t"; fontname="Helvetica-Bold"; fontsize=18;`)
	if delta {
		fmt.Printf("  label=\"Function call graph (delta-scoped: since %s, depth %d)   arrow: A calls B\";\n", sinceRef, depth)
	} else {
		fmt.Println(`  label="Function call graph (full)   arrow: A calls B; boxes = packages";`)
	}
	fmt.Println(`  node [shape=box, style="rounded,filled", fontname="Helvetica", fontsize=10, fillcolor="#eef3fb", color="#4a6fa5"];`)
	fmt.Println(`  edge [color="#666666", arrowsize=0.7];`)

	id := func(f *types.Func) string { return f.FullName() }

	for ci, p := range pkgs {
		nodes := byPkg[p]
		sort.Slice(nodes, func(i, j int) bool { return label[nodes[i]] < label[nodes[j]] })
		short := p
		if idx := strings.LastIndex(p, "/"); idx >= 0 {
			short = p[idx+1:]
		}
		fmt.Printf("  subgraph cluster_p%d {\n", ci)
		fmt.Printf("    label=%q; labelloc=\"t\"; fontname=\"Helvetica-Bold\"; fontsize=12;\n", short)
		fmt.Println(`    style="rounded,filled"; color="#b0b0c0"; fillcolor="#fafafd"; margin=10;`)
		for _, f := range nodes {
			fill, pen := "#eef3fb", "#4a6fa5"
			if changed[f] {
				fill, pen = "#e6f4ea", "#1a7f37" // touched by the change
			}
			fmt.Printf("    %q [label=%q, fillcolor=%q, color=%q];\n", id(f), label[f], fill, pen)
		}
		fmt.Println("  }")
	}

	for e := range edges {
		if !visible[e.from] || !visible[e.to] {
			continue
		}
		style := ""
		if changed[e.from] || changed[e.to] {
			style = ` [color="#1a7f37", penwidth=1.4]`
		}
		fmt.Printf("  %q -> %q%s;\n", id(e.from), id(e.to), style)
	}

	// legend
	fmt.Println(`  subgraph cluster_legend {`)
	fmt.Println(`    label="Legend"; labelloc="t"; fontname="Helvetica-Bold"; fontsize=12;`)
	fmt.Println(`    style="rounded,filled"; color="#cccccc"; fillcolor="#fbfbfb"; margin=10;`)
	fmt.Println(`    Lp [label="pkg.Recv.Method", fillcolor="#eef3fb", color="#4a6fa5"];`)
	if delta {
		fmt.Println(`    Lc [label="changed by this delta", fillcolor="#e6f4ea", color="#1a7f37"];`)
		fmt.Println(`    Lp -> Lc [label="  calls", color="#666666"];`)
	} else {
		fmt.Println(`    Lq [label="another function", fillcolor="#eef3fb", color="#4a6fa5"];`)
		fmt.Println(`    Lp -> Lq [label="  calls", color="#666666"];`)
	}
	fmt.Println(`  }`)
	fmt.Println("}")
}
