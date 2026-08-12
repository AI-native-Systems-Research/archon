// Package impact computes the "blast radius" of a box: which internal packages
// depend on it, directly and transitively, over import/call edges. If the target
// changes, these are the packages that could be affected — the answer to
// "what breaks if we touch X?" that a reviewer (or a refactor plan) needs.
package impact

import (
	"sort"

	"github.com/AI-native-Systems-Research/archon/graph"
)

// Dependents returns the internal packages that depend on target: `direct` are
// immediate dependents (one import/call edge into target); `transitive` is the
// full set reachable by following dependents-of-dependents (includes the direct
// ones). Only internal import/call edges are followed.
func Dependents(g *graph.Graph, target string) (direct, transitive []string) {
	internal := g.InternalPaths()

	// Reverse adjacency over internal import/call edges: to -> {from...}.
	rev := map[string]map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind != "import" && e.Kind != "call" {
			continue
		}
		if !internal[e.From] || !internal[e.To] || e.From == e.To {
			continue
		}
		if rev[e.To] == nil {
			rev[e.To] = map[string]bool{}
		}
		rev[e.To][e.From] = true
	}

	directSet := rev[target]
	seen := map[string]bool{} // transitive dependents
	queue := []string{target}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for f := range rev[n] {
			if f == target || seen[f] {
				continue
			}
			seen[f] = true
			queue = append(queue, f)
		}
	}

	for d := range directSet {
		direct = append(direct, d)
	}
	for t := range seen {
		transitive = append(transitive, t)
	}
	sort.Strings(direct)
	sort.Strings(transitive)
	return direct, transitive
}

// Resolve maps a user-supplied target (an exact internal import path, or a
// unique suffix like "internal/memcached") to the internal package path. It
// returns the matches; the caller decides what to do with 0 or >1.
func Resolve(g *graph.Graph, target string) []string {
	internal := g.InternalPaths()
	if internal[target] {
		return []string{target}
	}
	var matches []string
	for p := range internal {
		if p == target || hasSuffixSeg(p, target) {
			matches = append(matches, p)
		}
	}
	sort.Strings(matches)
	return matches
}

// hasSuffixSeg reports whether path ends with suffix on a path-segment boundary
// (so "internal/memcached" matches ".../internal/memcached" but not "...cached").
func hasSuffixSeg(path, suffix string) bool {
	if len(suffix) >= len(path) {
		return path == suffix
	}
	if path[len(path)-len(suffix):] != suffix {
		return false
	}
	return path[len(path)-len(suffix)-1] == '/'
}
