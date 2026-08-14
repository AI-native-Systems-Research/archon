package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Component is one box in the higher-altitude view: a directory-grouped cluster
// of packages.
type Component struct {
	Name    string   `json:"name"`
	Members []string `json:"members"` // relative package paths, sorted
	InCycle bool     `json:"inCycle"`
	// Change is "" (unchanged), "boundary" (a package boundary moved here —
	// green), or "minor" (only surface/schema/invariants changed here — blue).
	Change string `json:"change,omitempty"`
}

// ComponentEdge is an aggregated arrow between two components.
type ComponentEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`   // "dep" (import/call) or "contract" (implements)
	Change string `json:"change"` // "" (unchanged), "added", "removed"
}

// ComponentView is the component map (from the head graph) with the PR painted
// on top, plus the rendered Mermaid and DOT.
type ComponentView struct {
	Module     string          `json:"module"`
	Depth      int             `json:"depth"`
	Components []Component     `json:"components"`
	Edges      []ComponentEdge `json:"edges"`
	Mermaid    string          `json:"-"`
	DOT        string          `json:"-"`
}

// compKey groups a relative package path into its component at the given depth.
// The module root ("") is "(root)"; otherwise it is the first `depth` path
// segments joined by "/". Mirrors component_view.py's comp_key.
func compKey(relPath string, depth int) string {
	if relPath == "" {
		return "(root)"
	}
	segs := strings.Split(relPath, "/")
	if len(segs) > depth {
		segs = segs[:depth]
	}
	return strings.Join(segs, "/")
}

// buildComponents derives the component boxes from both graphs' internal
// packages, aggregates internal edges to the component altitude, flags cycles,
// and paints the delta — including coloring edges as added/removed.
func buildComponents(gA, gB *graph.Graph, d *delta.Delta, depth int) ComponentView {
	module := gB.Module
	if module == "" {
		module = gA.Module
	}
	cv := ComponentView{Module: module, Depth: depth}

	// pkgComp maps an internal package path -> its component name (union of both).
	pkgComp := map[string]string{}
	members := map[string]map[string]bool{}
	addPkg := func(p graph.Package) {
		if !p.Internal {
			return
		}
		r := rel(module, p.Path)
		c := compKey(r, depth)
		pkgComp[p.Path] = c
		if members[c] == nil {
			members[c] = map[string]bool{}
		}
		members[c][r] = true
	}
	for _, p := range gA.Packages {
		addPkg(p)
	}
	for _, p := range gB.Packages {
		addPkg(p)
	}

	// Collect component-altitude edges from both graphs.
	type compEdgeKey struct{ from, to, kind string }
	edgesInA := map[compEdgeKey]bool{}
	edgesInB := map[compEdgeKey]bool{}

	classifyEdge := func(e graph.Edge) (compEdgeKey, bool) {
		var kind string
		switch e.Kind {
		case "import", "call":
			kind = "dep"
		case "implements":
			kind = "contract"
		default:
			return compEdgeKey{}, false
		}
		cf, okF := pkgComp[e.From]
		ct, okT := pkgComp[e.To]
		if !okF || !okT || cf == ct {
			return compEdgeKey{}, false
		}
		return compEdgeKey{cf, ct, kind}, true
	}

	for _, e := range gA.Edges {
		if k, ok := classifyEdge(e); ok {
			edgesInA[k] = true
		}
	}
	for _, e := range gB.Edges {
		if k, ok := classifyEdge(e); ok {
			edgesInB[k] = true
		}
	}

	// Merge: classify each edge as added/removed/unchanged.
	allEdgeKeys := map[compEdgeKey]bool{}
	for k := range edgesInA {
		allEdgeKeys[k] = true
	}
	for k := range edgesInB {
		allEdgeKeys[k] = true
	}
	edgeSet := map[string]ComponentEdge{}
	for k := range allEdgeKeys {
		change := ""
		switch {
		case edgesInB[k] && !edgesInA[k]:
			change = "added"
		case edgesInA[k] && !edgesInB[k]:
			change = "removed"
		}
		ce := ComponentEdge{From: k.from, To: k.to, Kind: k.kind, Change: change}
		edgeSet[ce.From+"\x00"+ce.Kind+"\x00"+ce.To] = ce
	}

	// Cycles: Tarjan SCC over the component graph (both edge kinds count).
	inCycle := detectCycles(members, edgeSet)

	// Paint the delta.
	boundary := map[string]bool{} // components with a boundary move (green)
	touch := func(pkgPath string) {
		if c, ok := pkgComp[pkgPath]; ok {
			boundary[c] = true
		}
	}
	for _, e := range d.EdgesAdded {
		touch(e.From)
		touch(e.To)
	}
	for _, e := range d.EdgesRemoved {
		touch(e.From)
		touch(e.To)
	}
	for _, cc := range d.Contracts {
		touch(pkgOf(cc.Interface))
		for _, im := range cc.ImplementersAdded {
			touch(pkgOf(im))
		}
		for _, im := range cc.ImplementersRemoved {
			touch(pkgOf(im))
		}
	}
	// Added/removed internal packages are boundary moves too.
	for _, p := range d.PackagesAdded {
		if p.Internal {
			boundary[compKey(rel(module, p.Path), depth)] = true
		}
	}

	minor := map[string]bool{} // only surface/schema/invariants changed (blue)
	markMinor := func(pkgPath string) {
		if c, ok := pkgComp[pkgPath]; ok && !boundary[c] {
			minor[c] = true
		}
	}
	for _, sc := range d.Surface {
		markMinor(sc.Package)
	}
	for _, sc := range d.SchemaChanges {
		markMinor(sc.Package)
	}
	for _, ic := range d.Invariants {
		markMinor(ic.Package)
	}

	// Assemble sorted components.
	names := make([]string, 0, len(members))
	for c := range members {
		names = append(names, c)
	}
	sort.Strings(names)
	for _, c := range names {
		mem := make([]string, 0, len(members[c]))
		for m := range members[c] {
			mem = append(mem, m)
		}
		sort.Strings(mem)
		comp := Component{Name: c, Members: mem, InCycle: inCycle[c]}
		switch {
		case boundary[c]:
			comp.Change = "boundary"
		case minor[c]:
			comp.Change = "minor"
		}
		cv.Components = append(cv.Components, comp)
	}

	// Sorted edges.
	keys := make([]string, 0, len(edgeSet))
	for k := range edgeSet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cv.Edges = append(cv.Edges, edgeSet[k])
	}

	cv.Mermaid = componentMermaid(cv)
	cv.DOT = componentDOT(cv)
	return cv
}

// pkgOf returns the package path of a "pkgpath.Symbol" identifier (split on the
// last "."). Mirrors component_delta.py's split-on-last-dot for contract nodes.
func pkgOf(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[:i]
	}
	return id
}

// detectCycles runs iterative Tarjan SCC over the component graph and returns
// the set of components that sit in a strongly-connected component of size > 1.
func detectCycles(members map[string]map[string]bool, edgeSet map[string]ComponentEdge) map[string]bool {
	// Build adjacency over component names, in sorted order for determinism.
	nodes := make([]string, 0, len(members))
	for c := range members {
		nodes = append(nodes, c)
	}
	sort.Strings(nodes)
	adj := map[string][]string{}
	for _, ce := range edgeSet {
		adj[ce.From] = append(adj[ce.From], ce.To)
	}
	for n := range adj {
		sort.Strings(adj[n])
	}

	index := map[string]int{}
	lowlink := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	idx := 0
	inCycle := map[string]bool{}

	type frame struct {
		node string
		ci   int // child iterator
	}
	for _, root := range nodes {
		if _, seen := index[root]; seen {
			continue
		}
		var call []frame
		call = append(call, frame{root, 0})
		index[root] = idx
		lowlink[root] = idx
		idx++
		stack = append(stack, root)
		onStack[root] = true

		for len(call) > 0 {
			f := &call[len(call)-1]
			children := adj[f.node]
			if f.ci < len(children) {
				w := children[f.ci]
				f.ci++
				if _, seen := index[w]; !seen {
					index[w] = idx
					lowlink[w] = idx
					idx++
					stack = append(stack, w)
					onStack[w] = true
					call = append(call, frame{w, 0})
				} else if onStack[w] {
					if index[w] < lowlink[f.node] {
						lowlink[f.node] = index[w]
					}
				}
				continue
			}
			// Done with f.node: if it is an SCC root, pop the component.
			if lowlink[f.node] == index[f.node] {
				var scc []string
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					scc = append(scc, w)
					if w == f.node {
						break
					}
				}
				if len(scc) > 1 {
					for _, w := range scc {
						inCycle[w] = true
					}
				}
			}
			// Propagate lowlink to parent.
			child := f.node
			call = call[:len(call)-1]
			if len(call) > 0 {
				parent := call[len(call)-1].node
				if lowlink[child] < lowlink[parent] {
					lowlink[parent] = lowlink[child]
				}
			}
		}
	}
	return inCycle
}

// componentMermaid renders the component view as GitHub-renderable Mermaid.
// Node ids are n0,n1,... assigned by sorted component name so the output is
// deterministic. Painting uses the four-color scheme via classDef.
func componentMermaid(cv ComponentView) string {
	id := map[string]string{}
	for i, c := range cv.Components {
		id[c.Name] = fmt.Sprintf("n%d", i)
	}
	var b strings.Builder
	b.WriteString("graph LR\n")
	for _, c := range cv.Components {
		label := c.Name
		if c.InCycle {
			label += " ⟲" // in a dependency cycle
		}
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", id[c.Name], mermaidEscape(label))
	}
	type edgeStyle struct {
		color  string
		dashed bool
	}
	var edgeStyles []edgeStyle
	for _, e := range cv.Edges {
		from, okF := id[e.From]
		to, okT := id[e.To]
		if !okF || !okT {
			continue
		}
		st := edgeStyle{color: colUnchanged}
		switch e.Change {
		case "added":
			st = edgeStyle{color: colAdded}
		case "removed":
			st = edgeStyle{color: colRemoved, dashed: true}
		}
		if e.Kind == "contract" || st.dashed {
			fmt.Fprintf(&b, "  %s -. implements .-> %s\n", from, to)
		} else {
			fmt.Fprintf(&b, "  %s --> %s\n", from, to)
		}
		edgeStyles = append(edgeStyles, st)
	}
	// linkStyle for edge colors.
	for i, s := range edgeStyles {
		if s.color != colUnchanged {
			fmt.Fprintf(&b, "  linkStyle %d stroke:%s,stroke-width:2px;\n", i, s.color)
		}
	}
	// Class definitions (colors) + assignments.
	fmt.Fprintf(&b, "  classDef boundary fill:%s,stroke:%s,stroke-width:2px;\n", colFill, colAdded)
	fmt.Fprintf(&b, "  classDef minor fill:%s,stroke:%s,stroke-width:1px,stroke-dasharray:4 3;\n", colFill, colModified)
	fmt.Fprintf(&b, "  classDef unchanged fill:%s,stroke:%s;\n", colFill, colUnchanged)
	for _, c := range cv.Components {
		class := "unchanged"
		switch c.Change {
		case "boundary":
			class = "boundary"
		case "minor":
			class = "minor"
		}
		fmt.Fprintf(&b, "  class %s %s;\n", id[c.Name], class)
	}
	return b.String()
}

// componentDOT renders the same view as Graphviz DOT (for the optional PNG).
func componentDOT(cv ComponentView) string {
	id := map[string]string{}
	for _, c := range cv.Components {
		id[c.Name] = "c__" + strings.NewReplacer("/", "__", "(", "_", ")", "_").Replace(c.Name)
	}
	var b strings.Builder
	b.WriteString("digraph components {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Helvetica\"];\n")
	for _, c := range cv.Components {
		stroke := colUnchanged
		pw := "1"
		style := "rounded,filled"
		switch c.Change {
		case "boundary":
			stroke, pw = colAdded, "2.4"
		case "minor":
			stroke, pw, style = colModified, "1.8", "rounded,filled,dashed"
		}
		label := c.Name
		if c.InCycle {
			label += " ⟲"
		}
		fmt.Fprintf(&b, "  %s [label=%q, fillcolor=%q, color=%q, penwidth=%s, style=%q];\n",
			id[c.Name], label, colFill, stroke, pw, style)
	}
	for _, e := range cv.Edges {
		from, okF := id[e.From]
		to, okT := id[e.To]
		if !okF || !okT {
			continue
		}
		eColor := colUnchanged
		eStyle := "solid"
		switch e.Change {
		case "added":
			eColor = colAdded
		case "removed":
			eColor, eStyle = colRemoved, "dashed"
		}
		if e.Kind == "contract" {
			fmt.Fprintf(&b, "  %s -> %s [style=dashed, label=\"implements\", color=%q, fontcolor=%q, fontsize=9, penwidth=2];\n", from, to, eColor, eColor)
		} else {
			fmt.Fprintf(&b, "  %s -> %s [color=%q, style=%s, penwidth=2];\n", from, to, eColor, eStyle)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func mermaidEscape(s string) string {
	return strings.ReplaceAll(s, "\"", "'")
}
