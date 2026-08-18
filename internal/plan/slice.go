package plan

import (
	"fmt"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Slice extracts a single hole from a compiled plan as a human-readable work
// order: surface, allow, contract, and evidence. Returns an error if the hole
// is not found or is not marked as a hole.
func Slice(g *graph.Graph, holePath string) (string, error) {
	for _, pkg := range g.Packages {
		if pkg.Path == holePath {
			if !pkg.Hole {
				return "", fmt.Errorf("%s is not a hole (it is a declared box)", holePath)
			}
			return renderSlice(pkg), nil
		}
	}
	return "", fmt.Errorf("hole %q not found in plan", holePath)
}

func renderSlice(pkg graph.Package) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", pkg.Path)

	if len(pkg.Surface) > 0 {
		b.WriteString("## Surface\n\n")
		for _, s := range pkg.Surface {
			if s.Sig != "" {
				fmt.Fprintf(&b, "- `%s%s`\n", s.Name, s.Sig)
			} else {
				fmt.Fprintf(&b, "- `%s`\n", s.Name)
			}
		}
		b.WriteString("\n")
	}

	if len(pkg.Allow) > 0 {
		b.WriteString("## Allow\n\n")
		for _, a := range pkg.Allow {
			fmt.Fprintf(&b, "- `import %s`\n", a)
		}
		b.WriteString("\n")
	}

	if len(pkg.Invariants) > 0 {
		b.WriteString("## Contract\n\n")
		for _, inv := range pkg.Invariants {
			fmt.Fprintf(&b, "- **%s**\n", inv.Name)
		}
		b.WriteString("\n")
	}

	return b.String()
}
