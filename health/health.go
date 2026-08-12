// Package health computes architecture-health metrics over an extracted graph:
// coupling (fan-in/fan-out, instability), dependency cycles, blast-radius
// hotspots, and god-module candidates. These are the "understand the current
// design" signals a maintainer needs before a refactor, all derived from the
// internal import/call edges.
package health

import (
	"sort"

	"github.com/AI-native-Systems-Research/archon/graph"
	"github.com/AI-native-Systems-Research/archon/impact"
)

// PkgMetrics holds per-package coupling metrics.
type PkgMetrics struct {
	Path        string  `json:"path"`
	FanIn       int     `json:"fanIn"`       // internal packages that depend on this one
	FanOut      int     `json:"fanOut"`      // internal packages this one depends on
	Instability float64 `json:"instability"` // fanOut/(fanIn+fanOut); 0=stable, 1=unstable (Martin)
	Surface     int     `json:"surface"`     // exported symbols
	BlastRadius int     `json:"blastRadius"` // transitive dependents
	God         bool    `json:"god,omitempty"`
}

// Report is the full health analysis of a graph.
type Report struct {
	Packages   []PkgMetrics `json:"packages"`
	Cycles     [][]string   `json:"cycles"`     // multi-package SCCs over import/call
	GodModules []string     `json:"godModules"` // high fan-in AND large surface
}

// Analyze computes the health report for g over internal import/call edges.
func Analyze(g *graph.Graph) Report {
	internal := g.InternalPaths()
	surface := map[string]int{}
	for _, p := range g.Packages {
		if p.Internal {
			surface[p.Path] = len(p.Surface)
		}
	}

	fanIn := map[string]int{}
	fanOut := map[string]int{}
	// dedupe edges per (from,to) so import+call between the same pair counts once
	seen := map[[2]string]bool{}
	for _, e := range g.Edges {
		if e.Kind != "import" && e.Kind != "call" {
			continue
		}
		if !internal[e.From] || !internal[e.To] || e.From == e.To {
			continue
		}
		k := [2]string{e.From, e.To}
		if seen[k] {
			continue
		}
		seen[k] = true
		fanOut[e.From]++
		fanIn[e.To]++
	}

	var pkgs []PkgMetrics
	for p := range internal {
		fi, fo := fanIn[p], fanOut[p]
		inst := 0.0
		if fi+fo > 0 {
			inst = float64(fo) / float64(fi+fo)
		}
		_, transitive := impact.Dependents(g, p)
		pkgs = append(pkgs, PkgMetrics{
			Path: p, FanIn: fi, FanOut: fo, Instability: inst,
			Surface: surface[p], BlastRadius: len(transitive),
		})
	}

	// God-module heuristic: fan-in AND surface both in the top ~25%.
	fiCut := percentile(values(pkgs, func(m PkgMetrics) int { return m.FanIn }), 0.75)
	surfCut := percentile(values(pkgs, func(m PkgMetrics) int { return m.Surface }), 0.75)
	var gods []string
	for i := range pkgs {
		if pkgs[i].FanIn >= fiCut && pkgs[i].Surface >= surfCut && fiCut > 0 {
			pkgs[i].God = true
			gods = append(gods, pkgs[i].Path)
		}
	}

	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].BlastRadius != pkgs[j].BlastRadius {
			return pkgs[i].BlastRadius > pkgs[j].BlastRadius
		}
		return pkgs[i].FanIn > pkgs[j].FanIn
	})
	sort.Strings(gods)
	return Report{Packages: pkgs, Cycles: cycles(g, internal), GodModules: gods}
}

func values(ms []PkgMetrics, f func(PkgMetrics) int) []int {
	out := make([]int, len(ms))
	for i, m := range ms {
		out[i] = f(m)
	}
	return out
}

func percentile(xs []int, p float64) int {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	idx := int(p * float64(len(s)-1))
	return s[idx]
}

// cycles returns the multi-package strongly-connected components over internal
// import/call edges (Tarjan). An acyclic graph returns an empty slice.
func cycles(g *graph.Graph, internal map[string]bool) [][]string {
	adj := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind != "import" && e.Kind != "call" {
			continue
		}
		if !internal[e.From] || !internal[e.To] || e.From == e.To {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
	}
	idx := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	counter := 0
	var out [][]string
	var dfs func(v string)
	dfs = func(v string) {
		idx[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range adj[v] {
			if _, ok := idx[w]; !ok {
				dfs(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && idx[w] < low[v] {
				low[v] = idx[w]
			}
		}
		if low[v] == idx[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Strings(comp)
				out = append(out, comp)
			}
		}
	}
	for v := range adj {
		if _, ok := idx[v]; !ok {
			dfs(v)
		}
	}
	return out
}
