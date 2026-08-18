package review

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/gate"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
	"github.com/AI-native-Systems-Research/archon/internal/plan"
)

// renderMarkdown builds review.md — the primary human interface, designed for
// `cat review.md >> $GITHUB_STEP_SUMMARY` or a PR comment. It leads with the
// verdict. A NO_CHANGE PR stops at the one-line note (plus a short pointer if a
// guarded promise was touched). An ARCHITECTURAL_CHANGE PR embeds all three
// views inline as GitHub-renderable Mermaid, each with its detail table.
func renderMarkdown(res *Result) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## ARCHON PR review — `%s` → `%s`\n\n", res.LabelA, res.LabelB)
	b.WriteString("_Deterministic · no LLM · package-altitude architecture._\n\n")

	// Verdict line.
	fmt.Fprintf(&b, "**Verdict: `%s`**\n\n%s\n\n", res.Verdict, res.Summary)

	// Plan ratchet is shown regardless of verdict — it's always useful info.
	writePlanRatchetSection(&b, res.PlanRatchet)
	writeWideningSection(&b, res.Widenings)

	if res.Verdict == NoChange {
		if n := res.Counts.Invariants + res.Counts.SchemaChanged; n > 0 {
			fmt.Fprintf(&b, "_Note: %d guarded promise(s) (invariant / schema) also changed within the existing boundary — see `review.json`._\n\n", n)
		}
		writeCollapsibleJSON(&b, res)
		writeFooter(&b, res)
		return b.String()
	}

	// Counts line — a quick tally.
	writeCounts(&b, res)

	// An allow-list violation is still surfaced when present (it implies an added
	// edge, so we are already in the architectural-change branch).
	if len(res.Violations) > 0 {
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

	// The three views, each embedded inline as Mermaid.
	b.WriteString("### Component view\n\n")
	b.WriteString("_Nodes: green = boundary moved · blue-dashed = surface/schema/invariant only · grey = unchanged. Edges: green = added · red-dashed = removed · grey = unchanged. ⟲ = dependency cycle._\n\n")
	writeMermaid(&b, res.Components.Mermaid)

	b.WriteString("### Witness delta — full vs partial decoupling\n\n")
	if v := witnessVerdict(res.Witnesses); v != "" {
		fmt.Fprintf(&b, "**%s**\n\n", v)
	}
	if m := witnessMermaid(res.Witnesses); m != "" {
		b.WriteString("_Red dashed = connection fully removed · red solid = weakened (still coupled) · green = added/strengthened · blue = churned._\n\n")
		writeMermaid(&b, m)
	}
	writeWitnessTable(&b, res.Witnesses)

	b.WriteString("\n### Interface-contract delta\n\n")
	if m := contractMermaid(res.Contracts); m != "" {
		b.WriteString("_Green = implementer added · red dashed = implementer removed._\n\n")
		writeMermaid(&b, m)
	}
	b.WriteString(contractMarkdown(res.Contracts))
	b.WriteString("\n")

	// Invariant / schema / surface detail.
	writeInvariantTable(&b, res.Invariants)
	writeSchemaTable(&b, res.Schema_)
	writeSurfaceTable(&b, res.Surface)

	writeCollapsibleJSON(&b, res)
	writeFooter(&b, res)
	return b.String()
}

// writeMermaid emits a fenced ```mermaid block, ensuring a trailing newline.
func writeMermaid(b *strings.Builder, src string) {
	if strings.TrimSpace(src) == "" {
		return
	}
	b.WriteString("```mermaid\n")
	b.WriteString(src)
	if !strings.HasSuffix(src, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```\n\n")
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
		quoted[i] = "`" + tableCell(it) + "`"
	}
	s := strings.Join(quoted, ", ")
	if total > len(items) {
		s += fmt.Sprintf(" _(+%d more %s)_", total-len(items), noun)
	}
	return s
}

// tableCell escapes a literal "|" so it does not break a GitHub Markdown table
// (a raw pipe ends the cell even inside a code span). Witness strings like
// "Bank |= BatchClassifier" would otherwise mangle the row.
func tableCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
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

func writePlanRatchetSection(b *strings.Builder, r *plan.RatchetResult) {
	if r == nil {
		return
	}
	b.WriteString("### G5 — Plan distance ratchet\n\n")
	status := "OK"
	if !r.OK {
		status = "REGRESSION"
	}
	fmt.Fprintf(b, "**dist(P,G): %d → %d** — %s\n\n", r.Before, r.After, status)
}

func writeWideningSection(b *strings.Builder, widenings []gate.Widening) {
	if len(widenings) == 0 {
		return
	}
	b.WriteString("### G3 — Surface growth on fixed packages\n\n")
	for _, w := range widenings {
		fmt.Fprintf(b, "**`%s`** +%d entities: %s\n\n", shortID(w.Package), len(w.Added), symbolList(w.Added))
	}
}

func writeCollapsibleJSON(b *strings.Builder, res *Result) {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return
	}
	b.WriteString("<details>\n<summary><code>review.json</code></summary>\n\n")
	b.WriteString("```json\n")
	b.WriteString(string(data))
	b.WriteString("\n```\n\n</details>\n\n")
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
