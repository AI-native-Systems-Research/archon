package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
)

// contractMarkdown renders the interface-contract delta as a Markdown table,
// derived entirely from delta.Contracts (no `consumes`, no working-tree
// checkout). Each row is one interface whose implementer set changed; the
// coverage columns flag a new implementer that no bound contract test exercises
// (an evidence gap).
func contractMarkdown(contracts []delta.ContractChange) string {
	if len(contracts) == 0 {
		return "_No interface-contract membership changed._\n"
	}
	var b strings.Builder
	b.WriteString("| Interface | + implementers | − implementers | uncovered (evidence gap) | contract test |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, c := range contracts {
		test := "—"
		if len(c.ContractTests) > 0 {
			test = "`" + strings.Join(shortAll(c.ContractTests), "`, `") + "`"
		}
		gap := "—"
		if len(c.Uncovered) > 0 {
			gap = "**" + strings.Join(shortAll(c.Uncovered), ", ") + "**"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
			shortID(c.Interface),
			cellList(shortAll(c.ImplementersAdded)),
			cellList(shortAll(c.ImplementersRemoved)),
			gap,
			test,
		)
	}
	return b.String()
}

// contractDOT renders the interface-contract delta as Graphviz DOT (for the
// optional contract.png). Each interface is a node; each implementer points at
// it with an "implements" edge. Added implementers are green, removed are red
// (dashed), and an uncovered implementer (an evidence gap) is outlined red.
// Node ids are deterministic: i<n> for interfaces, m<n> for implementers, by
// sorted order.
func contractDOT(contracts []delta.ContractChange) string {
	// Collect implementer nodes with a stable status per node.
	type implState struct{ added, removed, uncovered bool }
	impls := map[string]*implState{}
	touch := func(id string) *implState {
		if impls[id] == nil {
			impls[id] = &implState{}
		}
		return impls[id]
	}

	ifaceNames := make([]string, 0, len(contracts))
	for _, c := range contracts {
		ifaceNames = append(ifaceNames, c.Interface)
		for _, im := range c.ImplementersAdded {
			touch(im).added = true
		}
		for _, im := range c.ImplementersRemoved {
			touch(im).removed = true
		}
		for _, im := range c.Uncovered {
			touch(im).uncovered = true
		}
	}
	sort.Strings(ifaceNames)

	ifaceID := map[string]string{}
	for i, name := range ifaceNames {
		ifaceID[name] = fmt.Sprintf("i%d", i)
	}
	implNames := make([]string, 0, len(impls))
	for id := range impls {
		implNames = append(implNames, id)
	}
	sort.Strings(implNames)
	implID := map[string]string{}
	for i, name := range implNames {
		implID[name] = fmt.Sprintf("m%d", i)
	}

	var b strings.Builder
	b.WriteString("digraph contract {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [fontname=\"Helvetica\"];\n")
	// Interface nodes.
	for _, name := range ifaceNames {
		fmt.Fprintf(&b, "  %s [label=%q, shape=box, style=\"rounded,filled\", fillcolor=%q, color=%q];\n",
			ifaceID[name], shortID(name), colFill, colUnchanged)
	}
	// Implementer nodes.
	for _, name := range implNames {
		st := impls[name]
		stroke := colUnchanged
		style := "filled"
		switch {
		case st.added:
			stroke = colAdded
		case st.removed:
			stroke = colRemoved
			style = "filled,dashed"
		}
		if st.uncovered {
			stroke = colRemoved // an evidence gap is flagged red
		}
		fmt.Fprintf(&b, "  %s [label=%q, shape=ellipse, style=%q, fillcolor=\"#ffffff\", color=%q];\n",
			implID[name], shortID(name), style, stroke)
	}
	// implements edges.
	for _, c := range contracts {
		for _, im := range c.ImplementersAdded {
			fmt.Fprintf(&b, "  %s -> %s [label=\"implements\", color=%q, fontsize=9];\n",
				implID[im], ifaceID[c.Interface], colAdded)
		}
		for _, im := range c.ImplementersRemoved {
			fmt.Fprintf(&b, "  %s -> %s [label=\"implements\", color=%q, style=dashed, fontsize=9];\n",
				implID[im], ifaceID[c.Interface], colRemoved)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// shortID trims a "pkgpath.Symbol" identifier to "pkgtail.Symbol" (the segment
// after the last "/"), for readable tables.
func shortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func shortAll(ids []string) []string {
	out := make([]string, len(ids))
	for i, s := range ids {
		out[i] = shortID(s)
	}
	return out
}

func cellList(xs []string) string {
	if len(xs) == 0 {
		return "—"
	}
	return strings.Join(xs, ", ")
}
