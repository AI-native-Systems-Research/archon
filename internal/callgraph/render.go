package callgraph

// This file is the callgraph CLI's rendering half, moved here verbatim from
// cmd/callgraph so that the command can be a subcommand of archon-go while the
// graph and the way it is drawn stay in one package.

import (
	"bufio"
	"bytes"
	"fmt"
	"go/types"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Options are the knobs the callgraph command exposes.
type Options struct {
	Mode Mode
	// SinceRef, when set, scopes the graph to the functions a diff against that
	// ref touched, plus their callers and callees out to Depth hops.
	SinceRef string
	Depth    int
}

// Render builds the call graph of pattern in dir and writes Graphviz DOT to out,
// with a one-line summary and any warnings to errw.
//
// Two views. By default every in-module function is drawn, clustered by package.
// With Options.SinceRef the tool diffs that ref against the working tree, marks
// the functions whose bodies overlap changed lines, and draws only those plus
// their callers and callees out to Options.Depth hops — one by default. The
// changed functions and the edges touching them are highlighted.
func Render(dir, pattern string, o Options, out, errw io.Writer) error {
	cg, err := Build(dir, pattern, o.Mode)
	if err != nil {
		return err
	}
	reportIllTyped(cg, errw)
	defined, pos, edges := cg.Defined, cg.Pos, cg.Edges

	// delta scope: which functions did <ref>..worktree touch?
	changed := map[*types.Func]bool{}
	if o.SinceRef != "" {
		ranges := gitChangedRanges(dir, o.SinceRef, errw)
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
	if o.SinceRef != "" {
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
		for d := 0; d < o.Depth; d++ {
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
	if o.SinceRef != "" {
		mode = fmt.Sprintf("delta since %s (%d changed fn, depth %d)", o.SinceRef, len(changed), o.Depth)
	}
	if o.Mode != Static {
		mode += ", " + o.Mode.String()
	}
	fmt.Fprintf(errw, "callgraph: %d functions in module, %d visible, %d edges [%s]\n",
		len(defined), len(visible), nEdges, mode)

	if o.Mode == Static {
		fmt.Fprintln(errw, "callgraph: static mode: calls through an interface are dropped; --mode=cha resolves them")
	}
	reportUnresolved(cg, errw)

	emitDOT(out, cg, visible, changed, o)
	return nil
}

// iv is a line interval on the new side of a diff.
type iv struct{ lo, hi int }

// reportIllTyped prints what callgraph.IllTypedPkg documents: a package that did
// not type-check contributes nodes but not all of its edges, so the graph looks
// complete when it is not. Causes are listed before importers, so truncating the
// list cannot hide the package that has to be fixed.
func reportIllTyped(cg *Graph, errw io.Writer) {
	if len(cg.IllTyped) == 0 {
		return
	}
	fmt.Fprintf(errw, "callgraph: %d packages did not type-check; calls inside them are missing:\n", len(cg.IllTyped))
	const maxShown = 20
	for i, p := range cg.IllTyped {
		if i == maxShown {
			fmt.Fprintf(errw, "  ... and %d more\n", len(cg.IllTyped)-maxShown)
			break
		}
		if p.Cause {
			fmt.Fprintf(errw, "  cause    %s: %s\n", p.Path, p.Err)
			continue
		}
		fmt.Fprintf(errw, "  importer %s\n", p.Path)
	}
}

// reportUnresolved names the interface call sites that produced no edge, with
// their positions, so they can be looked at rather than merely counted.
func reportUnresolved(cg *Graph, errw io.Writer) {
	if len(cg.Unresolved) == 0 && cg.UnresolvedInWrappers == 0 {
		return
	}
	fmt.Fprintf(errw, "callgraph: %d unresolved interface dispatches, %d of them positioned (no function with a body to draw the edge from):\n",
		len(cg.Unresolved)+cg.UnresolvedInWrappers, len(cg.Unresolved))
	const maxShown = 10
	for i, d := range cg.Unresolved {
		if i == maxShown {
			fmt.Fprintf(errw, "  ... and %d more\n", len(cg.Unresolved)-maxShown)
			break
		}
		fmt.Fprintf(errw, "  %s\n", d)
	}
	if cg.UnresolvedInWrappers > 0 {
		fmt.Fprintf(errw, "  %d inside wrappers go/ssa synthesised, which have no position: method values and method expressions\n",
			cg.UnresolvedInWrappers)
	}
}

// gitChangedRanges diffs ref against the working tree and returns, per file
// (repo-relative path), the new-side line intervals that changed.
func gitChangedRanges(dir, ref string, errw io.Writer) map[string][]iv {
	out := map[string][]iv{}
	cmd := exec.Command("git", "-C", dir, "diff", "--unified=0", ref, "--", "*.go")
	b, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(errw, "git diff failed:", err)
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
	out io.Writer,
	cg *Graph,
	visible, changed map[*types.Func]bool,
	o Options,
) {
	delta, sinceRef, depth, mode := o.SinceRef != "", o.SinceRef, o.Depth, o.Mode
	label, pkgOf := cg.Label, cg.Pkg
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

	fmt.Fprintln(out, "digraph callgraph {")
	fmt.Fprintln(out, "  rankdir=LR;")
	fmt.Fprintln(out, `  labelloc="t"; fontname="Helvetica-Bold"; fontsize=18;`)
	resolved := ""
	if mode != Static {
		resolved = fmt.Sprintf(", %s: interface calls resolved", mode)
	}
	if delta {
		fmt.Fprintf(out, "  label=\"Function call graph (delta-scoped: since %s, depth %d%s)   arrow: A calls B\";\n", sinceRef, depth, resolved)
	} else {
		fmt.Fprintf(out, "  label=\"Function call graph (full%s)   arrow: A calls B; boxes = packages\";\n", resolved)
	}
	fmt.Fprintln(out, `  node [shape=box, style="rounded,filled", fontname="Helvetica", fontsize=10, fillcolor="#eef3fb", color="#4a6fa5"];`)
	fmt.Fprintln(out, `  edge [color="#666666", arrowsize=0.7];`)

	// Not FullName, for the reason given at callgraph.assignIDs: two nodes with
	// the same DOT id are one node.
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
		fmt.Fprintf(out, "  subgraph cluster_p%d {\n", ci)
		fmt.Fprintf(out, "    label=%q; labelloc=\"t\"; fontname=\"Helvetica-Bold\"; fontsize=12;\n", short)
		fmt.Fprintln(out, `    style="rounded,filled"; color="#b0b0c0"; fillcolor="#fafafd"; margin=10;`)
		for _, f := range nodes {
			fill, pen := "#eef3fb", "#4a6fa5"
			if changed[f] {
				fill, pen = "#e6f4ea", "#1a7f37" // touched by the change
			}
			fmt.Fprintf(out, "    %q [label=%q, fillcolor=%q, color=%q];\n", id(f), label[f], fill, pen)
		}
		fmt.Fprintln(out, "  }")
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
		if w := cg.Edges[e]; w != "" {
			attrs = append(attrs, "style=dashed", fmt.Sprintf("tooltip=%q", "dispatched through "+w))
		}
		style := ""
		if len(attrs) > 0 {
			style = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Fprintf(out, "  %q -> %q%s;\n", id(e.From), id(e.To), style)
	}

	// legend
	fmt.Fprintln(out, `  subgraph cluster_legend {`)
	fmt.Fprintln(out, `    label="Legend"; labelloc="t"; fontname="Helvetica-Bold"; fontsize=12;`)
	fmt.Fprintln(out, `    style="rounded,filled"; color="#cccccc"; fillcolor="#fbfbfb"; margin=10;`)
	fmt.Fprintln(out, `    Lp [label="pkg.Recv.Method", fillcolor="#eef3fb", color="#4a6fa5"];`)
	if delta {
		fmt.Fprintln(out, `    Lc [label="changed by this delta", fillcolor="#e6f4ea", color="#1a7f37"];`)
		fmt.Fprintln(out, `    Lp -> Lc [label="  calls", color="#666666"];`)
	} else {
		fmt.Fprintln(out, `    Lq [label="another function", fillcolor="#eef3fb", color="#4a6fa5"];`)
		fmt.Fprintln(out, `    Lp -> Lq [label="  calls", color="#666666"];`)
	}
	if mode != Static {
		fmt.Fprintln(out, `    Li [label="an implementation", fillcolor="#eef3fb", color="#4a6fa5"];`)
		fmt.Fprintln(out, `    Lp -> Li [label="  calls through an interface", color="#666666", style=dashed];`)
	}
	fmt.Fprintln(out, `  }`)
	fmt.Fprintln(out, "}")
}
