// Package reflexion implements a reflexion model: it compares a declared,
// intended layering (a human design decision ARCHON cannot extract) against the
// recovered dependency graph, and reports where the code diverges from it.
// A dependency that flows down the layer order is convergent; one that flows up
// (a lower layer depends on a higher one) is a divergence — a layering
// violation. The violation count is the reflexion distance, and its change
// across commits is the "did this PR move toward the target?" signal.
package reflexion

import (
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Spec is the declared intended architecture: an ordered list of layers
// (top to bottom) and a map from a package's top-level segment to a layer name.
type Spec struct {
	Layers []string          `json:"layers"`
	Map    map[string]string `json:"map"` // top-level dir -> layer name
}

// Violation is an aggregated upward (layer-violating) dependency.
type Violation struct {
	From      string `json:"from"`      // component (top-level dir)
	To        string `json:"to"`        // component it depends on
	FromLayer string `json:"fromLayer"` // higher-ordinal (lower) layer
	ToLayer   string `json:"toLayer"`   // lower-ordinal (higher) layer
	Count     int    `json:"count"`
}

// Report is the reflexion analysis.
type Report struct {
	DownEdges  int         `json:"downEdges"`  // convergent (top->down or same)
	UpEdges    int         `json:"upEdges"`    // divergent (violations)
	Violations []Violation `json:"violations"` // aggregated upward deps, worst first
	Unmapped   []string    `json:"unmapped"`   // top-level dirs with no layer assignment
}

// Analyze compares g against the declared layering spec.
func Analyze(g *graph.Graph, spec Spec) Report {
	order := map[string]int{}
	for i, l := range spec.Layers {
		order[l] = i
	}
	internal := g.InternalPaths()

	// component (top-level dir) -> layer ordinal, and track unmapped
	layerOf := func(pkgPath string) (string, int, bool) {
		rel := strings.TrimPrefix(pkgPath, g.Module+"/")
		rel = strings.TrimPrefix(rel, g.Module) // root
		seg := rel
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			seg = rel[:i]
		}
		if seg == "" {
			seg = "(root)"
		}
		lname, ok := spec.Map[seg]
		if !ok {
			return seg, -1, false
		}
		return lname, order[lname], true
	}

	unmapped := map[string]bool{}
	down := map[[2]string]int{}
	up := map[[2]string]int{}
	seenPkg := map[[2]string]bool{} // dedupe import+call between the same package pair
	for _, e := range g.Edges {
		if e.Kind != "import" && e.Kind != "call" {
			continue
		}
		if !internal[e.From] || !internal[e.To] || e.From == e.To {
			continue
		}
		pk := [2]string{e.From, e.To}
		if seenPkg[pk] {
			continue
		}
		seenPkg[pk] = true
		fname, fo, fok := layerOf(e.From)
		tname, to, tok := layerOf(e.To)
		if !fok {
			unmapped[fname] = true
		}
		if !tok {
			unmapped[tname] = true
		}
		if !fok || !tok || fname == tname {
			continue
		}
		key := [2]string{fname, tname}
		if fo <= to {
			down[key]++
		} else {
			up[key]++
		}
	}

	rep := Report{}
	for _, n := range down {
		rep.DownEdges += n
	}
	for k, n := range up {
		rep.UpEdges += n
		fl := spec.Map[k[0]]
		tl := spec.Map[k[1]]
		rep.Violations = append(rep.Violations, Violation{From: k[0], To: k[1], FromLayer: fl, ToLayer: tl, Count: n})
	}
	sort.Slice(rep.Violations, func(i, j int) bool { return rep.Violations[i].Count > rep.Violations[j].Count })
	for u := range unmapped {
		rep.Unmapped = append(rep.Unmapped, u)
	}
	sort.Strings(rep.Unmapped)
	return rep
}
