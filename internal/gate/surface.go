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

// SurfacePolicy declares which packages have a fixed surface and which
// specific entities are authorized exceptions (widen stanzas).
type SurfacePolicy struct {
	Fixed map[string]bool     // package paths whose surface must not grow
	Widen map[string][]string // package path -> entity names authorized to be added
}

// CheckSurface reports packages whose public surface grew despite being declared
// fixed. Additions named in the Widen map are suppressed. It is vacuous when
// policy is nil or has no fixed packages.
func CheckSurface(surface []delta.SurfaceChange, policy *SurfacePolicy) []Widening {
	if policy == nil || len(policy.Fixed) == 0 {
		return nil
	}
	var out []Widening
	for _, sc := range surface {
		if !policy.Fixed[sc.Package] {
			continue
		}
		unauthorized := filterWiden(sc.Added, policy.Widen[sc.Package])
		if len(unauthorized) == 0 {
			continue
		}
		out = append(out, Widening{Package: sc.Package, Added: unauthorized})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Package < out[j].Package
	})
	return out
}

func filterWiden(added []graph.Symbol, allowed []string) []graph.Symbol {
	if len(allowed) == 0 {
		return added
	}
	set := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		set[name] = true
	}
	var unauthorized []graph.Symbol
	for _, sym := range added {
		if !set[sym.Name] {
			unauthorized = append(unauthorized, sym)
		}
	}
	return unauthorized
}
