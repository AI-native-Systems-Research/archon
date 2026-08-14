package review

import (
	"fmt"
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
