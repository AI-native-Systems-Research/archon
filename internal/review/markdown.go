package review

import (
	"fmt"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// renderMarkdown builds review.md — the primary human interface, designed for
// `cat review.md >> $GITHUB_STEP_SUMMARY` or a PR comment. It leads with the
// verdict and, for architectural changes, embeds a GitHub-renderable Mermaid
// diagram plus the witness/contract/violation tables. Fast-track PRs stop at the
// one-line note.
func renderMarkdown(res *Result) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## ARCHON PR review — `%s` → `%s`\n\n", res.LabelA, res.LabelB)
	b.WriteString("_Deterministic · no LLM · package-altitude architecture._\n\n")

	// Verdict badge line.
	fmt.Fprintf(&b, "**Verdict: `%s`**\n\n%s\n\n", res.Verdict, res.Summary)

	if res.Verdict == FastTrack {
		writeFooter(&b, res)
		return b.String()
	}

	// Counts line — a quick tally for any non-fast-track PR.
	writeCounts(&b, res)

	if res.Verdict == Block && len(res.Violations) > 0 {
		b.WriteString("\n### ⛔ Allow-list violations\n\n")
		b.WriteString("| From | → | To | Kind | Introduced by this PR |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, v := range res.Violations {
			intro := "no"
			if v.Introduced {
				intro = "**yes**"
			}
			fmt.Fprintf(&b, "| `%s` | → | `%s` | %s | %s |\n",
				shortID(v.From), shortID(v.To), v.Kind, intro)
		}
		b.WriteString("\n")
	}

	// Architectural views (component + witness + contract) for boundary moves.
	if res.Verdict == ReviewArchitecture || res.Verdict == Block {
		b.WriteString("### Component view\n\n")
		b.WriteString("_Green = boundary moved · blue-dashed = surface/schema/invariant only · grey = unchanged. ⟲ marks a dependency cycle._\n\n")
		b.WriteString("```mermaid\n")
		b.WriteString(res.Components.Mermaid)
		if !strings.HasSuffix(res.Components.Mermaid, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")

		b.WriteString("### Witness delta — full vs partial decoupling\n\n")
		if v := witnessVerdict(res.Witnesses); v != "" {
			fmt.Fprintf(&b, "**%s**\n\n", v)
		}
		writeWitnessTable(&b, res.Witnesses)

		b.WriteString("\n### Interface-contract delta\n\n")
		b.WriteString(contractMarkdown(res.Contracts))
		b.WriteString("\n")
	}

	// Invariant / schema / surface detail (all tiers above fast-track show these).
	writeInvariantTable(&b, res.Invariants)
	writeSchemaTable(&b, res.Schema_)
	writeSurfaceTable(&b, res.Surface)

	writeFooter(&b, res)
	return b.String()
}

func writeCounts(b *strings.Builder, res *Result) {
	c := res.Counts
	parts := []string{}
	add := func(n int, label string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(c.PackagesAdded, "pkg+")
	add(c.PackagesRemoved, "pkg−")
	add(c.EdgesAdded, "edge+")
	add(c.EdgesRemoved, "edge−")
	add(c.SurfaceChanged, "surface")
	add(c.SchemaChanged, "schema")
	add(c.Invariants, "invariant")
	add(c.Contracts, "contract")
	add(c.Violations, "violation")
	if len(parts) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\n", strings.Join(parts, " · "))
}

func writeWitnessTable(b *strings.Builder, rows []WitnessRow) {
	if len(rows) == 0 {
		b.WriteString("_No package edge changed._\n")
		return
	}
	b.WriteString("| Edge | Kind | Status | Removed | Still coupled via |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, r := range rows {
		noun := witnessNoun(r.Kind)
		removed := cellCode(capList(r.Removed, 4), noun, len(r.Removed))
		remaining := "—"
		if r.Status == wsWeakened || r.Status == wsChurned {
			remaining = cellCode(capList(r.Remaining, 4), noun, len(r.Remaining))
		}
		fmt.Fprintf(b, "| `%s → %s` | %s | %s | %s | %s |\n",
			r.From, r.To, r.Kind, statusBadge(r.Status), removed, remaining)
	}
}

func statusBadge(status string) string {
	switch status {
	case wsRemoved:
		return "**REMOVED** (full decoupling)"
	case wsWeakened:
		return "**WEAKENED** (partial)"
	default:
		return status
	}
}

func cellCode(items []string, noun string, total int) string {
	if len(items) == 0 {
		return "—"
	}
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = "`" + it + "`"
	}
	s := strings.Join(quoted, ", ")
	if total > len(items) {
		s += fmt.Sprintf(" _(+%d more %s)_", total-len(items), noun)
	}
	return s
}

func capList(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}

func writeInvariantTable(b *strings.Builder, invs []delta.InvariantChange) {
	if len(invs) == 0 {
		return
	}
	b.WriteString("### Invariants touched (guarded promises)\n\n")
	b.WriteString("| Package | + added | ~ modified | − removed | promise on |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, ic := range invs {
		fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n",
			shortID(ic.Package),
			cellList(ic.Added),
			cellList(ic.Modified),
			cellList(ic.Removed),
			cellList(shortAll(ic.GuardedContracts)),
		)
	}
	b.WriteString("\n")
}

func writeSchemaTable(b *strings.Builder, changes []delta.SurfaceChange) {
	if len(changes) == 0 {
		return
	}
	b.WriteString("### Schema (wire/DB data contract) changes\n\n")
	b.WriteString("| Package | + fields | − fields |\n")
	b.WriteString("|---|---|---|\n")
	for _, sc := range changes {
		fmt.Fprintf(b, "| `%s` | %s | %s |\n",
			shortID(sc.Package), symbolList(sc.Added), symbolList(sc.Removed))
	}
	b.WriteString("\n")
}

func writeSurfaceTable(b *strings.Builder, changes []delta.SurfaceChange) {
	if len(changes) == 0 {
		return
	}
	b.WriteString("### Public surface changes\n\n")
	b.WriteString("| Package | + added | − removed |\n")
	b.WriteString("|---|---|---|\n")
	for _, sc := range changes {
		fmt.Fprintf(b, "| `%s` | %s | %s |\n",
			shortID(sc.Package), symbolList(sc.Added), symbolList(sc.Removed))
	}
	b.WriteString("\n")
}

func symbolList(syms []graph.Symbol) string {
	if len(syms) == 0 {
		return "—"
	}
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return "`" + strings.Join(names, "`, `") + "`"
}

func writeFooter(b *strings.Builder, res *Result) {
	if len(res.Artifacts) > 0 {
		b.WriteString("---\n\n")
		b.WriteString("Artifacts: ")
		links := make([]string, 0, len(res.Artifacts))
		for _, a := range res.Artifacts {
			if a == "review.md" {
				continue
			}
			links = append(links, fmt.Sprintf("[`%s`](%s)", a, a))
		}
		b.WriteString(strings.Join(links, " · "))
		b.WriteString("\n")
	}
}
