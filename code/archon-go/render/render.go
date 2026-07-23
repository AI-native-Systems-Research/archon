// Package render turns an ARCHON package-altitude graph into a picture — either
// Graphviz DOT or Mermaid — so the extracted architecture can be looked at the
// way an architecture diagram is: boxes for packages, typed arrows between them.
//
// Edge styling encodes the arrow kind:
//   - implements → dashed "realization" arrow (the contract seams)
//   - call       → solid arrow (a real use dependency)
//   - import     → faint arrow, drawn only when no call already connects the pair
//     (a call implies its import, so showing both would just add clutter)
package render

import (
	"fmt"
	"sort"
	"strings"

	"archon-go/graph"
)

// isConfigNode reports whether a package path is a synthetic config-key node
// (an env var or CLI flag). These are shown by default even though they are not
// module-internal, because a config change is exactly what we want visible.
func isConfigNode(path string) bool {
	return strings.HasPrefix(path, "env:") || strings.HasPrefix(path, "flag:")
}

// DOT renders the graph as Graphviz DOT. When includeExternal is false only
// module-internal packages (and edges between them) are drawn.
func DOT(g *graph.Graph, includeExternal bool) string {
	nodes, keep := selectNodes(g, includeExternal)
	label := shortLabeler(nodes)
	internal := g.InternalPaths()

	var b strings.Builder
	b.WriteString("digraph archon {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Helvetica\", fillcolor=\"#eef3fb\", color=\"#4a6fa5\"];\n")
	b.WriteString("  edge [fontname=\"Helvetica\", fontsize=10];\n")

	for _, n := range nodes {
		style := ""
		switch {
		case isConfigNode(n):
			style = " [shape=note, fillcolor=\"#fdf0d5\", color=\"#c9820a\"]"
		case !internal[n]:
			style = " [fillcolor=\"#f3f3f3\", color=\"#999999\", style=\"rounded,filled,dashed\"]"
		}
		fmt.Fprintf(&b, "  %q%s;\n", label(n), style)
	}

	for _, e := range renderEdges(g, keep) {
		switch e.Kind {
		case "implements":
			fmt.Fprintf(&b, "  %q -> %q [style=dashed, arrowhead=onormal, color=\"#8a5cf6\", label=\"implements\"];\n", label(e.From), label(e.To))
		case "call":
			fmt.Fprintf(&b, "  %q -> %q [color=\"#333333\"];\n", label(e.From), label(e.To))
		case "import":
			fmt.Fprintf(&b, "  %q -> %q [color=\"#bbbbbb\"];\n", label(e.From), label(e.To))
		case "config":
			fmt.Fprintf(&b, "  %q -> %q [style=dotted, arrowhead=vee, color=\"#c9820a\", label=\"config\"];\n", label(e.From), label(e.To))
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// Mermaid renders the graph as a Mermaid flowchart (renders inline on GitHub).
func Mermaid(g *graph.Graph, includeExternal bool) string {
	nodes, keep := selectNodes(g, includeExternal)
	label := shortLabeler(nodes)
	id := idLabeler(nodes)

	var b strings.Builder
	b.WriteString("graph LR\n")
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s[%q]\n", id(n), label(n))
	}
	for _, e := range renderEdges(g, keep) {
		switch e.Kind {
		case "implements":
			fmt.Fprintf(&b, "  %s -. implements .-> %s\n", id(e.From), id(e.To))
		case "call":
			fmt.Fprintf(&b, "  %s --> %s\n", id(e.From), id(e.To))
		case "import":
			fmt.Fprintf(&b, "  %s -.-> %s\n", id(e.From), id(e.To))
		case "config":
			fmt.Fprintf(&b, "  %s -. config .-> %s\n", id(e.From), id(e.To))
		}
	}
	return b.String()
}

// --- Diff rendering: one picture of the change from A to B ---------------
//
// Nodes and arrows are colored by whether they were added (present only in B),
// removed (present only in A), or unchanged (present in both), so a reviewer
// can see at a glance what the commit did to the architecture.

const (
	statusAdded     = "added"
	statusRemoved   = "removed"
	statusUnchanged = "unchanged"
)

type diffNode struct {
	Path     string
	Internal bool
	Status   string
}

type diffEdge struct {
	From, To, Kind, Status string
}

// buildDiff produces the union node/edge model of A and B, each tagged with its
// add/remove/unchanged status, applying the same import/call collapse as the
// single-graph renderer.
func buildDiff(a, b *graph.Graph, includeExternal bool) ([]diffNode, []diffEdge) {
	internal := map[string]bool{}
	for _, p := range a.Packages {
		if p.Internal {
			internal[p.Path] = true
		}
	}
	for _, p := range b.Packages {
		if p.Internal {
			internal[p.Path] = true
		}
	}
	keep := func(path string) bool { return internal[path] || isConfigNode(path) || includeExternal }
	status := func(inA, inB bool) string {
		switch {
		case inA && inB:
			return statusUnchanged
		case inB:
			return statusAdded
		default:
			return statusRemoved
		}
	}

	aN, bN := map[string]bool{}, map[string]bool{}
	for _, p := range a.Packages {
		aN[p.Path] = true
	}
	for _, p := range b.Packages {
		bN[p.Path] = true
	}
	paths := map[string]bool{}
	for p := range aN {
		paths[p] = true
	}
	for p := range bN {
		paths[p] = true
	}
	var nodes []diffNode
	for p := range paths {
		if !keep(p) {
			continue
		}
		nodes = append(nodes, diffNode{Path: p, Internal: internal[p], Status: status(aN[p], bN[p])})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Path < nodes[j].Path })

	aE, bE := map[string]graph.Edge{}, map[string]graph.Edge{}
	for _, e := range a.Edges {
		aE[e.Key()] = e
	}
	for _, e := range b.Edges {
		bE[e.Key()] = e
	}
	hasCall := map[string]bool{}
	for _, e := range append(append([]graph.Edge{}, a.Edges...), b.Edges...) {
		if e.Kind == "call" && keep(e.From) && keep(e.To) {
			hasCall[e.From+"\x00"+e.To] = true
		}
	}
	keys := map[string]bool{}
	for k := range aE {
		keys[k] = true
	}
	for k := range bE {
		keys[k] = true
	}
	var edges []diffEdge
	for k := range keys {
		e, ok := bE[k]
		if !ok {
			e = aE[k]
		}
		if !keep(e.From) || !keep(e.To) || e.From == e.To {
			continue
		}
		if e.Kind == "import" && hasCall[e.From+"\x00"+e.To] {
			continue // a call already connects this pair; drop the faint import
		}
		_, inA := aE[k]
		_, inB := bE[k]
		edges = append(edges, diffEdge{e.From, e.To, e.Kind, status(inA, inB)})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
	return nodes, edges
}

// focusChanged trims the diagram to the change itself: the nodes that were
// added/removed or sit at the end of a changed arrow, plus the arrows among
// them. Unrelated unchanged nodes (disconnected utilities, other subsystems)
// are dropped so the picture reads as "what this commit did," not the whole map.
func focusChanged(nodes []diffNode, edges []diffEdge) ([]diffNode, []diffEdge) {
	seed := map[string]bool{}
	for _, n := range nodes {
		if n.Status != statusUnchanged {
			seed[n.Path] = true
		}
	}
	for _, e := range edges {
		if e.Status != statusUnchanged {
			seed[e.From] = true
			seed[e.To] = true
		}
	}
	if len(seed) == 0 {
		return nodes, edges // nothing changed — show everything
	}
	var kn []diffNode
	for _, n := range nodes {
		if seed[n.Path] {
			kn = append(kn, n)
		}
	}
	var ke []diffEdge
	for _, e := range edges {
		if seed[e.From] && seed[e.To] {
			ke = append(ke, e)
		}
	}
	return kn, ke
}

// DOTDiff renders the A→B change as a single colored Graphviz diagram. When
// focus is true, only the changed neighborhood is drawn (see focusChanged).
func DOTDiff(a, b *graph.Graph, includeExternal, focus bool) string {
	nodes, edges := buildDiff(a, b, includeExternal)
	if focus {
		nodes, edges = focusChanged(nodes, edges)
	}
	paths := make([]string, len(nodes))
	for i, n := range nodes {
		paths[i] = n.Path
	}
	label := shortLabeler(paths)

	var sb strings.Builder
	sb.WriteString("digraph archon_diff {\n")
	sb.WriteString("  rankdir=LR;\n")
	sb.WriteString("  labelloc=\"t\";\n")
	sb.WriteString("  label=\"architectural delta — green: added   red: removed   grey: unchanged\";\n")
	sb.WriteString("  fontname=\"Helvetica\"; fontsize=20;\n")
	sb.WriteString("  nodesep=0.5; ranksep=1.0;\n")
	sb.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Helvetica\", fontsize=20, margin=\"0.18,0.11\"];\n")
	sb.WriteString("  edge [fontname=\"Helvetica\", fontsize=15];\n")

	for _, n := range nodes {
		fill, border, extra := "#eef3fb", "#4a6fa5", ""
		switch n.Status {
		case statusAdded:
			fill, border = "#e6f4ea", "#1a7f37"
		case statusRemoved:
			fill, border, extra = "#fce8e6", "#cf222e", ", style=\"rounded,filled,dashed\""
		default:
			if isConfigNode(n.Path) {
				fill, border = "#fdf0d5", "#c9820a"
			} else if !n.Internal {
				fill, border, extra = "#f3f3f3", "#999999", ", style=\"rounded,filled,dashed\""
			}
		}
		if isConfigNode(n.Path) {
			extra += ", shape=note"
		}
		fmt.Fprintf(&sb, "  %q [fillcolor=%q, color=%q%s];\n", label(n.Path), fill, border, extra)
	}

	for _, e := range edges {
		color, width := statusEdgeColor(e.Status)
		head := "normal"
		style := "solid"
		switch e.Kind {
		case "implements":
			head, style = "onormal", "dashed"
		case "import":
			style = "dashed"
		case "config":
			head, style = "vee", "dotted"
		}
		if e.Status == statusRemoved && style == "solid" {
			style = "dashed"
		}
		lbl := ""
		if e.Kind == "implements" && e.Status != statusUnchanged {
			lbl = ", label=\"implements\""
		}
		if e.Kind == "config" && e.Status != statusUnchanged {
			lbl = ", label=\"config\""
		}
		fmt.Fprintf(&sb, "  %q -> %q [color=%q, penwidth=%s, arrowhead=%s, style=%s%s];\n",
			label(e.From), label(e.To), color, width, head, style, lbl)
	}
	sb.WriteString("}\n")
	return sb.String()
}

// MermaidDiff renders the A→B change as a colored Mermaid flowchart. When focus
// is true, only the changed neighborhood is drawn (see focusChanged).
func MermaidDiff(a, b *graph.Graph, includeExternal, focus bool) string {
	nodes, edges := buildDiff(a, b, includeExternal)
	if focus {
		nodes, edges = focusChanged(nodes, edges)
	}
	paths := make([]string, len(nodes))
	for i, n := range nodes {
		paths[i] = n.Path
	}
	label := shortLabeler(paths)
	id := idLabeler(paths)

	var sb strings.Builder
	sb.WriteString("graph LR\n")
	byStatus := map[string][]string{}
	for _, n := range nodes {
		fmt.Fprintf(&sb, "  %s[%q]\n", id(n.Path), label(n.Path))
		byStatus[n.Status] = append(byStatus[n.Status], id(n.Path))
	}
	for i, e := range edges {
		switch e.Kind {
		case "implements":
			fmt.Fprintf(&sb, "  %s -. implements .-> %s\n", id(e.From), id(e.To))
		case "import":
			fmt.Fprintf(&sb, "  %s -.-> %s\n", id(e.From), id(e.To))
		case "config":
			fmt.Fprintf(&sb, "  %s -. config .-> %s\n", id(e.From), id(e.To))
		default:
			fmt.Fprintf(&sb, "  %s --> %s\n", id(e.From), id(e.To))
		}
		color, _ := statusEdgeColor(e.Status)
		fmt.Fprintf(&sb, "  linkStyle %d stroke:%s,stroke-width:2px\n", i, color)
	}
	sb.WriteString("  classDef added fill:#e6f4ea,stroke:#1a7f37;\n")
	sb.WriteString("  classDef removed fill:#fce8e6,stroke:#cf222e;\n")
	sb.WriteString("  classDef unchanged fill:#eef3fb,stroke:#4a6fa5;\n")
	for _, s := range []string{statusAdded, statusRemoved, statusUnchanged} {
		if ids := byStatus[s]; len(ids) > 0 {
			fmt.Fprintf(&sb, "  class %s %s\n", strings.Join(ids, ","), s)
		}
	}
	return sb.String()
}

func statusEdgeColor(status string) (color, width string) {
	switch status {
	case statusAdded:
		return "#1a7f37", "2.0"
	case statusRemoved:
		return "#cf222e", "2.0"
	default:
		return "#c8c8c8", "1.0"
	}
}

// renderEdge is a resolved arrow to draw after collapsing import/call overlap.
type renderEdge struct{ From, To, Kind string }

// renderEdges picks, for each ordered pair of kept packages, the arrows worth
// drawing: implements always; call when present; import only when there is no
// call for that pair.
func renderEdges(g *graph.Graph, keep map[string]bool) []renderEdge {
	hasCall := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind == "call" && keep[e.From] && keep[e.To] {
			hasCall[e.From+"\x00"+e.To] = true
		}
	}
	var out []renderEdge
	for _, e := range g.Edges {
		if !keep[e.From] || !keep[e.To] || e.From == e.To {
			continue
		}
		if e.Kind == "import" && hasCall[e.From+"\x00"+e.To] {
			continue // a call already connects this pair; skip the faint import
		}
		out = append(out, renderEdge{e.From, e.To, e.Kind})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// selectNodes returns the package paths to draw (sorted) and a membership set.
func selectNodes(g *graph.Graph, includeExternal bool) ([]string, map[string]bool) {
	keep := map[string]bool{}
	var nodes []string
	for _, p := range g.Packages {
		if p.Internal || isConfigNode(p.Path) || includeExternal {
			keep[p.Path] = true
			nodes = append(nodes, p.Path)
		}
	}
	sort.Strings(nodes)
	return nodes, keep
}

// shortLabeler labels each package by its trailing path segment, falling back to
// the full path when two packages would collide.
func shortLabeler(nodes []string) func(string) string {
	count := map[string]int{}
	for _, n := range nodes {
		count[lastSeg(n)]++
	}
	return func(p string) string {
		s := lastSeg(p)
		if count[s] > 1 {
			return p
		}
		return s
	}
}

// idLabeler assigns each package a Mermaid-safe stable node id.
func idLabeler(nodes []string) func(string) string {
	ids := map[string]string{}
	for i, n := range nodes {
		ids[n] = fmt.Sprintf("n%d", i)
	}
	return func(p string) string { return ids[p] }
}

func lastSeg(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
