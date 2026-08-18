package plan

import (
	"fmt"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Stats returns a summary of clause counts by class from a compiled plan.
// Classes are extracted from the [class: detail] annotation in contract lines
// and stored in Invariant.Hash during compilation.
func Stats(g *graph.Graph) string {
	total := 0
	checked := 0
	evidenced := 0
	attestedExt := 0
	attestedDesign := 0
	unclassified := 0

	for _, pkg := range g.Packages {
		for _, inv := range pkg.Invariants {
			total++
			class := inv.Hash
			switch {
			case strings.HasPrefix(class, "checked"):
				checked++
			case strings.HasPrefix(class, "evidenced"):
				evidenced++
			case strings.HasPrefix(class, "attested:external"):
				attestedExt++
			case strings.HasPrefix(class, "attested:design"):
				attestedDesign++
			case strings.HasPrefix(class, "attested"):
				attestedDesign++
			default:
				unclassified++
			}
		}
	}

	s := fmt.Sprintf("%d clauses: %d checked, %d evidenced, %d attested:external, %d attested:design",
		total, checked, evidenced, attestedExt, attestedDesign)
	if unclassified > 0 {
		s += fmt.Sprintf(", %d unclassified", unclassified)
	}
	return s
}
