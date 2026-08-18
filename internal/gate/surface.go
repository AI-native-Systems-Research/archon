package gate

import (
	"sort"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Widening records unauthorized surface growth on a package declared fixed.
type Widening struct {
	Package string         `json:"package"`
	Added   []graph.Symbol `json:"added"`
}

// CheckSurface reports packages whose public surface grew despite being declared
// fixed. It is vacuous when fixed is nil or empty — no widenings are possible.
func CheckSurface(surface []delta.SurfaceChange, fixed map[string]bool) []Widening {
	if len(fixed) == 0 {
		return nil
	}
	var out []Widening
	for _, sc := range surface {
		if !fixed[sc.Package] {
			continue
		}
		if len(sc.Added) == 0 {
			continue
		}
		out = append(out, Widening{Package: sc.Package, Added: sc.Added})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Package < out[j].Package
	})
	return out
}
