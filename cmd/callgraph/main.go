// Command callgraph draws a function-altitude call graph of a Go module.
//
// Two views:
//
//	full          every in-module function, clustered by package.
//	delta-scoped  pass --since <ref>; the tool diffs <ref> against the working
//	              tree, marks the functions whose bodies overlap changed lines,
//	              and draws only those plus their callers/callees out to --depth
//	              hops (default 1). Changed functions are highlighted.
//
// Three resolution modes:
//
//	--mode=static  (default) a call is an edge only when the callee is a concrete
//	               in-module function. Calls through an interface are dropped.
//	--mode=cha     also resolves an interface call to every in-module implementer.
//	               Sound without a main, so it works on libraries. ~1.5x the edges.
//	--mode=rta     prunes implementers never instantiated on a reachable path.
//	               Needs a main, and finds almost nothing when dispatch goes
//	               through a framework (a Cobra CLI yields one edge).
//
// Usage:
//
//	callgraph <module-dir> <pkg-pattern> [--mode=static|cha|rta] [--since <ref>] [--depth N]
//	e.g.  callgraph ../inference-sim ./...                 # full graph
//	      callgraph ../inference-sim ./... --mode=cha      # include interface calls
//	      callgraph ../inference-sim ./... --since HEAD~1  # what the last commit touched
//
// Emits Graphviz DOT on stdout; a one-line summary on stderr. Analysis lives in
// internal/callgraph; this command is presentation only.
package main

import (
	"bufio"
	"bytes"
	"fmt"
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
		fmt.Fprintln(os.Stderr, "usage: callgraph <module-dir> <pkg-pattern> [--mode=static|cha|rta] [--since <ref>] [--depth N]")
		os.Exit(2)
	}
	dir, pattern := os.Args[1], os.Args[2]
	sinceRef := ""
	modeStr := "static"
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
		case a == "--mode" && i+1 < len(os.Args):
			modeStr = os.Args[i+1]
			i++
		case strings.HasPrefix(a, "--mode="):
			modeStr = strings.TrimPrefix(a, "--mode=")
		}
	}

	mode, err := callgraph.ParseMode(modeStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	g, err := callgraph.Build(dir, pattern, mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	byID := make(map[string]callgraph.Func, len(g.Funcs))
	for _, f := range g.Funcs {
		byID[f.ID] = f
	}

	// delta scope: which functions did <ref>..worktree touch?
	changed := map[string]bool{}
	if sinceRef != "" {
		ranges := gitChangedRanges(dir, sinceRef)
		for _, f := range g.Funcs {
			for file, ivs := range ranges {
				if !sameFile(f.File, file) {
					continue
				}
				for _, r := range ivs {
					if f.Lo <= r.hi && r.lo <= f.Hi {
						changed[f.ID] = true
					}
				}
			}
		}
	}

	visible := map[string]bool{}
	if sinceRef != "" {
		adjOut := map[string][]string{}
		adjIn := map[string][]string{}
		for _, e := range g.Edges {
			adjOut[e.From] = append(adjOut[e.From], e.To)
			adjIn[e.To] = append(adjIn[e.To], e.From)
		}
		frontier := map[string]bool{}
		for f := range changed {
			visible[f] = true
			frontier[f] = true
		}
		for d := 0; d < depth; d++ {
			next := map[string]bool{}
			for f := range frontier {
				for _, g := range append(adjOut[f], adjIn[f]...) {
					if !visible[g] {
						visible[g] = true
						next[g] = true
					}
				}
			}
			frontier = next
		}
	} else {
		for _, f := range g.Funcs {
			visible[f.ID] = true
		}
	}

	nEdges := 0
	for _, e := range g.Edges {
		if visible[e.From] && visible[e.To] {
			nEdges++
		}
	}
	scope := "full"
	if sinceRef != "" {
		scope = fmt.Sprintf("delta since %s (%d changed fn, depth %d)", sinceRef, len(changed), depth)
	}
	fmt.Fprintf(os.Stderr, "callgraph: %d functions in module, %d visible, %d edges [%s, mode=%s]\n",
		len(g.Funcs), len(visible), nEdges, scope, mode)

	emitDOT(g, byID, visible, changed, sinceRef != "", sinceRef, depth)
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
	g *callgraph.Graph,
	byID map[string]callgraph.Func,
	visible, changed map[string]bool,
	delta bool, sinceRef string, depth int,
) {
	byPkg := map[string][]callgraph.Func{}
	for id := range visible {
		f, ok := byID[id]
		if !ok {
			continue
		}
		byPkg[f.Pkg] = append(byPkg[f.Pkg], f)
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
		fmt.Printf("  label=\"Function call graph (full, mode=%s)   arrow: A calls B; boxes = packages\";\n", g.Mode)
	}
	fmt.Println(`  node [shape=box, style="rounded,filled", fontname="Helvetica", fontsize=10, fillcolor="#eef3fb", color="#4a6fa5"];`)
	fmt.Println(`  edge [color="#666666", arrowsize=0.7];`)

	for ci, p := range pkgs {
		nodes := byPkg[p]
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Label < nodes[j].Label })
		short := p
		if idx := strings.LastIndex(p, "/"); idx >= 0 {
			short = p[idx+1:]
		}
		fmt.Printf("  subgraph cluster_p%d {\n", ci)
		fmt.Printf("    label=%q; labelloc=\"t\"; fontname=\"Helvetica-Bold\"; fontsize=12;\n", short)
		fmt.Println(`    style="rounded,filled"; color="#b0b0c0"; fillcolor="#fafafd"; margin=10;`)
		for _, f := range nodes {
			fill, pen := "#eef3fb", "#4a6fa5"
			if changed[f.ID] {
				fill, pen = "#e6f4ea", "#1a7f37" // touched by the change
			}
			fmt.Printf("    %q [label=%q, fillcolor=%q, color=%q];\n", f.ID, f.Label, fill, pen)
		}
		fmt.Println("  }")
	}

	// g.Edges is sorted, so this loop is deterministic. Iterating the edge map
	// directly (as this did before) made two runs of identical input differ.
	for _, e := range g.Edges {
		if !visible[e.From] || !visible[e.To] {
			continue
		}
		var attrs []string
		if changed[e.From] || changed[e.To] {
			attrs = append(attrs, `color="#1a7f37"`, "penwidth=1.4")
		}
		if e.Via != "" {
			// Dashed marks a resolved interface call, labelled with the method that
			// dispatched it — without that a reader cannot tell it from a direct call.
			attrs = append(attrs, "style=dashed", fmt.Sprintf("label=%q", e.Via))
		}
		suffix := ""
		if len(attrs) > 0 {
			suffix = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Printf("  %q -> %q%s;\n", e.From, e.To, suffix)
	}

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
	if g.Mode != callgraph.Static {
		fmt.Println(`    Li [label="via interface", fillcolor="#eef3fb", color="#4a6fa5"];`)
		fmt.Println(`    Lp -> Li [label="  Method", color="#666666", style=dashed];`)
	}
	fmt.Println(`  }`)
	fmt.Println("}")
}
