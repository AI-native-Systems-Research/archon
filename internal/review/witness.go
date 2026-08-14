package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Witness status values, ordered by review interest (most decoupling first).
const (
	wsRemoved      = "REMOVED"      // edge and all its reasons gone — full decoupling
	wsWeakened     = "WEAKENED"     // edge remains, some reasons removed — partial decoupling
	wsChurned      = "CHURNED"      // reasons both added and removed
	wsStrengthened = "STRENGTHENED" // reasons added only
	wsAdded        = "ADDED"        // new edge
	wsUnchanged    = "unchanged"
)

var statusOrder = map[string]int{
	wsRemoved: 0, wsWeakened: 1, wsChurned: 2, wsStrengthened: 3, wsAdded: 4, wsUnchanged: 5,
}

// witnessNoun names what a witness of each edge kind is, for readable prose.
func witnessNoun(kind string) string {
	switch kind {
	case "call":
		return "symbol"
	case "implements":
		return "type"
	case "import":
		return "file"
	default:
		return "witness"
	}
}

// WitnessRow is one changed package edge with the exact witnesses that were
// removed, added, or still remain. It is the "why did this connection die,
// weaken, or survive" record — the full-vs-partial-decoupling distinction.
type WitnessRow struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Kind      string   `json:"kind"`
	Status    string   `json:"status"`
	Removed   []string `json:"removed,omitempty"`
	Added     []string `json:"added,omitempty"`
	Remaining []string `json:"remaining,omitempty"` // witnesses still coupling the edge in B
}

// buildWitnesses diffs the per-edge witness sets between the two graphs.
// Internal-only edges are considered (both endpoints internal), matching
// witness_delta.py. Unchanged edges are dropped.
func buildWitnesses(gA, gB *graph.Graph) []WitnessRow {
	module := gB.Module
	if module == "" {
		module = gA.Module
	}
	mapA := edgeWitnessMap(gA)
	mapB := edgeWitnessMap(gB)

	keys := map[edgeKey]bool{}
	for k := range mapA {
		keys[k] = true
	}
	for k := range mapB {
		keys[k] = true
	}

	var rows []WitnessRow
	for k := range keys {
		wa, inA := mapA[k]
		wb, inB := mapB[k]
		row := WitnessRow{
			From: rel(module, k.from),
			To:   rel(module, k.to),
			Kind: k.kind,
		}
		switch {
		case inA && !inB:
			row.Status = wsRemoved
			row.Removed = sortedSet(wa)
		case inB && !inA:
			row.Status = wsAdded
			row.Added = sortedSet(wb)
		default:
			removed := diffSet(wa, wb)
			added := diffSet(wb, wa)
			remaining := intersectSet(wa, wb)
			switch {
			case len(removed) == 0 && len(added) == 0:
				row.Status = wsUnchanged
			case len(removed) > 0 && len(added) == 0:
				row.Status = wsWeakened
			case len(added) > 0 && len(removed) == 0:
				row.Status = wsStrengthened
			default:
				row.Status = wsChurned
			}
			row.Removed = removed
			row.Added = added
			row.Remaining = remaining
		}
		if row.Status == wsUnchanged {
			continue
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if statusOrder[rows[i].Status] != statusOrder[rows[j].Status] {
			return statusOrder[rows[i].Status] < statusOrder[rows[j].Status]
		}
		if rows[i].From != rows[j].From {
			return rows[i].From < rows[j].From
		}
		if rows[i].To != rows[j].To {
			return rows[i].To < rows[j].To
		}
		return rows[i].Kind < rows[j].Kind
	})
	return rows
}

type edgeKey struct{ from, to, kind string }

// edgeWitnessMap maps each internal-only edge to its witness set.
func edgeWitnessMap(g *graph.Graph) map[edgeKey]map[string]bool {
	internal := g.InternalPaths()
	out := map[edgeKey]map[string]bool{}
	for _, e := range g.Edges {
		if !internal[e.From] || !internal[e.To] {
			continue
		}
		k := edgeKey{e.From, e.To, e.Kind}
		if out[k] == nil {
			out[k] = map[string]bool{}
		}
		for _, w := range e.Witnesses {
			out[k][w] = true
		}
	}
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// diffSet returns sorted (a - b).
func diffSet(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// intersectSet returns sorted (a ∩ b).
func intersectSet(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// witnessVerdict summarizes the decoupling: N fully decoupled (REMOVED), M
// partially decoupled (WEAKENED).
func witnessVerdict(rows []WitnessRow) string {
	var full, weak int
	for _, r := range rows {
		switch r.Status {
		case wsRemoved:
			full++
		case wsWeakened:
			weak++
		}
	}
	if full == 0 && weak == 0 {
		return ""
	}
	return fmt.Sprintf("%d edge(s) fully decoupled; %d edge(s) PARTIALLY decoupled (weakened)", full, weak)
}

// witnessDOT renders the witness delta as Graphviz DOT (for the optional PNG).
// Node ids are p0,p1,... by sorted package name. REMOVED edges are dashed;
// WEAKENED edges are solid but colored to flag the partial decoupling.
func witnessDOT(rows []WitnessRow, labelA, labelB string) string {
	pkgs := map[string]bool{}
	for _, r := range rows {
		pkgs[r.From] = true
		pkgs[r.To] = true
	}
	names := make([]string, 0, len(pkgs))
	for p := range pkgs {
		names = append(names, p)
	}
	sort.Strings(names)
	id := map[string]string{}
	for i, p := range names {
		id[p] = fmt.Sprintf("p%d", i)
	}

	var b strings.Builder
	b.WriteString("digraph witness {\n")
	b.WriteString("  rankdir=LR;\n")
	fmt.Fprintf(&b, "  label=%q; labelloc=t; fontsize=10;\n", fmt.Sprintf("witness delta  %s → %s", labelA, labelB))
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fillcolor=\"" + colFill + "\", fontname=\"Helvetica\"];\n")
	for _, p := range names {
		fmt.Fprintf(&b, "  %s [label=%q];\n", id[p], p)
	}
	for _, r := range rows {
		color, style := colUnchanged, "solid"
		switch r.Status {
		case wsRemoved:
			color, style = colRemoved, "dashed"
		case wsWeakened:
			color = colRemoved
		case wsAdded, wsStrengthened:
			color = colAdded
		case wsChurned:
			color = colModified
		}
		fmt.Fprintf(&b, "  %s -> %s [label=%q, color=%q, fontcolor=%q, penwidth=2, style=%s, fontsize=9];\n",
			id[r.From], id[r.To], r.Kind+" "+r.Status, color, color, style)
	}
	b.WriteString("}\n")
	return b.String()
}
