package plan

import (
	"fmt"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Render produces a Mermaid diagram from a compiled plan. Holes are drawn with
// dashed borders; filled boxes (non-hole packages) are solid. Generated output
// only — never parsed back.
func Render(g *graph.Graph) string {
	var b strings.Builder
	b.WriteString("graph LR\n")

	for i, pkg := range g.Packages {
		id := nodeID(i)
		label := lastSeg(pkg.Path)
		if pkg.Hole {
			fmt.Fprintf(&b, "  %s([\"%s\"])\n", id, label)
		} else {
			fmt.Fprintf(&b, "  %s[\"%s\"]\n", id, label)
		}
	}
	b.WriteString("\n")

	pathToID := make(map[string]string, len(g.Packages))
	for i, pkg := range g.Packages {
		pathToID[pkg.Path] = nodeID(i)
	}

	for _, e := range g.Edges {
		from, ok1 := pathToID[e.From]
		to, ok2 := pathToID[e.To]
		if !ok1 || !ok2 {
			continue
		}
		fmt.Fprintf(&b, "  %s --> %s\n", from, to)
	}
	b.WriteString("\n")

	// Style: holes get dashed stroke
	var holes []string
	var boxes []string
	for i, pkg := range g.Packages {
		if pkg.Hole {
			holes = append(holes, nodeID(i))
		} else {
			boxes = append(boxes, nodeID(i))
		}
	}
	if len(holes) > 0 {
		b.WriteString("  classDef hole fill:#ffeccc,stroke:#b45309,stroke-width:3px,stroke-dasharray:6 3,color:#3b2200\n")
		fmt.Fprintf(&b, "  class %s hole\n", strings.Join(holes, ","))
	}
	if len(boxes) > 0 {
		b.WriteString("  classDef box fill:#dbe7f8,stroke:#3b5f8f,color:#0d1b2e\n")
		fmt.Fprintf(&b, "  class %s box\n", strings.Join(boxes, ","))
	}

	return b.String()
}

func nodeID(i int) string {
	return fmt.Sprintf("n%d", i)
}
