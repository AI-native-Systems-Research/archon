// Command callgraph extracts a function-level call graph from a Go module: for
// every function/method with a body, which in-module functions it calls. The
// graph itself is built by internal/callgraph; see that package for how each
// mode resolves a call.
//
// Two views:
//
//	full          every in-module function, clustered by package.
//	delta-scoped  pass --since <ref>; the tool diffs <ref> against the working
//	              tree, marks the functions whose bodies overlap changed lines,
//	              and draws only those plus their callers/callees out to --depth
//	              hops (default 1). Changed functions are highlighted.
//
// Three modes:
//
//	--mode=static  (default) go/types only: a call is an edge when the callee is
//	               a concrete function defined in one of the loaded packages, so
//	               calls through an interface are dropped.
//	--mode=cha     also resolves interface calls, to every in-module method that
//	               could satisfy them. Sound on a library, with no main.
//	--mode=rta     resolves interface calls from the types reachable from the
//	               entry points; needs a main package.
//
// Usage:
//
//	callgraph <module-dir> <pkg-pattern> [--mode static|cha|rta] [--since <ref>] [--depth N]
//	e.g.  callgraph ../inference-sim ./...                 # full graph
//	      callgraph ../inference-sim ./... --mode cha      # with interface calls
//	      callgraph ../inference-sim ./... --since HEAD~1  # what the last commit touched
//
// Emits Graphviz DOT on stdout; a one-line summary on stderr. An interface call
// is drawn dashed, with the dispatching method as the edge tooltip.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/callgraph"
)

type iv struct{ lo, hi int }

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: callgraph <module-dir> <pkg-pattern> [--mode static|cha|rta] [--since <ref>] [--depth N]")
		os.Exit(2)
	}
	dir, pattern := os.Args[1], os.Args[2]
	sinceRef := ""
	depth := 1
	modeArg := "static"
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
		case a == "--mode" && i+1 < len(os.Args):
			modeArg = os.Args[i+1]
			i++
		case strings.HasPrefix(a, "--mode="):
			modeArg = strings.TrimPrefix(a, "--mode=")
		case strings.HasPrefix(a, "--mode"):
			// Falling through here would silently give static, which is the
			// graph with the interface calls missing.
			fmt.Fprintf(os.Stderr, "bad flag %q: --mode takes a value, one of static, cha or rta\n", a)
			os.Exit(2)
		}
	}
	cgMode, err := callgraph.ParseMode(modeArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	cg, err := callgraph.Build(dir, pattern, cgMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	reportIllTyped(cg)
	defined, label, pkgOf, pos, edges := cg.Defined, cg.Label, cg.Pkg, cg.Pos, cg.Edges

	// delta scope: which functions did <ref>..worktree touch?
	changed := map[*types.Func]bool{}
	if sinceRef != "" {
		ranges := gitChangedRanges(dir, sinceRef)
		for f, sp := range pos {
			for file, ivs := range ranges {
				if !sameFile(sp.File, file) {
					continue
				}
				for _, r := range ivs {
					if sp.Lo <= r.hi && r.lo <= sp.Hi {
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
			adjOut[e.From] = append(adjOut[e.From], e.To)
			adjIn[e.To] = append(adjIn[e.To], e.From)
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
		if visible[e.From] && visible[e.To] {
			nEdges++
		}
	}
	mode := "full"
	if sinceRef != "" {
		mode = fmt.Sprintf("delta since %s (%d changed fn, depth %d)", sinceRef, len(changed), depth)
	}
	if cgMode != callgraph.Static {
		mode += ", " + cgMode.String()
	}
	fmt.Fprintf(os.Stderr, "callgraph: %d functions in module, %d visible, %d edges [%s]\n",
		len(defined), len(visible), nEdges, mode)

	if cg.Unattributed > 0 {
		fmt.Fprintf(os.Stderr, "callgraph: %d interface call sites had no declaration to attribute them to (method values, closures in variable initializers)\n",
			cg.Unattributed)
	}

	emitDOT(cg, visible, changed, label, pkgOf, sinceRef != "", sinceRef, depth)
}

// reportIllTyped prints what callgraph.IllTypedPkg documents: a package that did
// not type-check contributes nodes but not all of its edges, so the graph looks
// complete when it is not. Causes are listed before importers, so truncating the
// list cannot hide the package that has to be fixed.
func reportIllTyped(cg *callgraph.Graph) {
	if len(cg.IllTyped) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "callgraph: %d packages did not type-check; calls inside them are missing:\n", len(cg.IllTyped))
	const maxShown = 20
	for i, p := range cg.IllTyped {
		if i == maxShown {
			fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(cg.IllTyped)-maxShown)
			break
		}
		if p.Cause {
			fmt.Fprintf(os.Stderr, "  cause    %s: %s\n", p.Path, p.Err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  importer %s\n", p.Path)
	}
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
	cg *callgraph.Graph,
	visible, changed map[*types.Func]bool,
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

	// Not FullName: a package's init functions all share one, and two nodes
	// with the same DOT id are one node.
	id := func(f *types.Func) string { return cg.ID[f] }

	for ci, p := range pkgs {
		nodes := byPkg[p]
		sort.Slice(nodes, func(i, j int) bool {
			if label[nodes[i]] != label[nodes[j]] {
				return label[nodes[i]] < label[nodes[j]]
			}
			return id(nodes[i]) < id(nodes[j])
		})
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

	// SortedEdges, not the edge map: map order is not stable, so without this two
	// runs on identical input differ everywhere and a real change cannot be
	// spotted in the diff.
	for _, e := range cg.SortedEdges() {
		if !visible[e.From] || !visible[e.To] {
			continue
		}
		var attrs []string
		if changed[e.From] || changed[e.To] {
			attrs = append(attrs, `color="#1a7f37"`, "penwidth=1.4")
		}
		if w := cg.Witness[e]; w != "" {
			attrs = append(attrs, "style=dashed", fmt.Sprintf("tooltip=%q", "dispatched through "+w))
		}
		style := ""
		if len(attrs) > 0 {
			style = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Printf("  %q -> %q%s;\n", id(e.From), id(e.To), style)
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
