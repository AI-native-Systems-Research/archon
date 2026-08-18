// Package graph defines ARCHON's architecture graph at the package altitude.
//
// A Graph is the "actual" architecture extracted mechanically from source at a
// single commit: boxes (packages), their public surface, and typed arrows
// (currently import edges) that aggregate the file-level dependencies beneath
// them. Every coarse arrow carries its witness set — the files that realize it —
// so the package-level picture can never silently disagree with the code.
package graph

import (
	"sort"
)

// Graph is the package-altitude architecture graph at one commit.
type Graph struct {
	Module   string    `json:"module"`
	Commit   string    `json:"commit,omitempty"`
	Packages []Package `json:"packages"`
	Edges    []Edge    `json:"edges"`

	// LocalImplements records interface satisfaction within a single package
	// (the concrete type and the interface it satisfies live in the same
	// package). These are deliberately NOT in Edges: no package boundary is
	// crossed, so they are not architectural arrows and must not affect the
	// boundary/empty/DAG/blast-radius logic. Contract coverage reads them so
	// that a package's own interface and its in-package implementers (a very
	// common Go idiom: an interface plus its default implementation) are still
	// tracked and can be checked for a contract test.
	LocalImplements []Edge `json:"localImplements,omitempty"`
}

// Package is a box: one Go package.
type Package struct {
	Path       string      `json:"path"`                 // import path, the stable identity at this altitude
	Name       string      `json:"name"`                 // clause name
	Internal   bool        `json:"internal"`             // true if part of the module under study
	Hole       bool        `json:"hole,omitempty"`       // true if declared but not yet implemented (Def. 5.1)
	Files      []string    `json:"files,omitempty"`      // base filenames, sorted
	Surface    []Symbol    `json:"surface,omitempty"`    // exported entities other packages may depend on
	Schema     []Symbol    `json:"schema,omitempty"`     // serialized (wire/DB) fields — the data contract that crosses a boundary
	Invariants []Invariant `json:"invariants,omitempty"` // tests guarding this box (candidate invariants)
	Allow      []string    `json:"allow,omitempty"`      // structural contract: internal packages this box is permitted to depend on
}

// Invariant is one test function that guards a box — a candidate system
// invariant. Its Hash is a gofmt-normalized digest of the function body, so a
// pure reformat does not read as a change, but touching what the test asserts
// does. Modifying or deleting an invariant is a boundary event: it means a PR
// changed a promise the system was making.
type Invariant struct {
	Name string `json:"name"` // test function name, e.g. TestScheduler_Determinism
	File string `json:"file"` // base filename it lives in
	Hash string `json:"hash"` // short digest of the normalized function, to detect modification

	// Guards and Exercises bind the test to the contract it protects, inferred
	// from the types the test references (via type info). Guards are the
	// interface (contract) nodes it exercises ("pkgpath.Interface"); Exercises
	// are the concrete types it touches ("pkgpath.Type"). A test that guards an
	// interface and exercises that interface's implementers is an interface-level
	// contract test: every implementer is expected to be covered by it. Both are
	// empty when binding could not be resolved (a candidate invariant only).
	Guards    []string `json:"guards,omitempty"`
	Exercises []string `json:"exercises,omitempty"`
}

// Key identifies an invariant within its package (by test name).
func (i Invariant) Key() string { return i.Name }

// Symbol is one element of a package's public surface.
type Symbol struct {
	Kind string `json:"kind"`          // func | method | type | var | const
	Name string `json:"name"`          // e.g. "NewScheduler" or "Scheduler.Step"
	Sig  string `json:"sig,omitempty"` // rendered signature/type, package-relative
}

// Edge is a typed arrow between two boxes. Kind is one of:
//   - "import"     — From imports To (the coarse "can reach" layer)
//   - "call"       — From references an exported func/method of To
//   - "implements" — a concrete type in From satisfies an interface in To
//   - "config"     — From reads an env var / defines a CLI flag; To is the
//     config-key node ("env:KEY" / "flag:NAME")
//   - "service"    — From imports a client library for a runtime service; To is
//     the service node ("service:Postgres", "service:Kafka", …)
//   - "capability" — From imports an escape-hatch package (unsafe, reflect,
//     os/exec, syscall, net, plugin); To is the capability node ("cap:net", …).
//     A candidate hidden channel for the surface-mediation assumption.
//   - "protocol"   — From registers an HTTP endpoint; To is the endpoint node
//     ("api:POST /tasks"). A new/removed route is a boundary change.
//
// The last four are "operational" edge kinds (per the paper): the review-
// relevant boundary is often not a source import. Witnesses records what
// realizes the arrow (files for imports; callee symbols for calls; "T |= I"
// pairs for implements; "role@file" for config; "import@file"/"register@file"
// for service/capability/protocol), so a change that does not touch a witness
// cannot change the package-level arrow. (The wire/DB *schema* is tracked
// separately, as Package.Schema, because a field change is witness-invisible.)
type Edge struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Kind      string   `json:"kind"`
	Witnesses []string `json:"witnesses,omitempty"`
}

// Key uniquely identifies an edge at this altitude.
func (e Edge) Key() string { return e.From + "\x00" + e.Kind + "\x00" + e.To }

// Key uniquely identifies a surface symbol within its package.
func (s Symbol) Key() string { return s.Kind + "\x00" + s.Name }

// Sort orders every collection in the graph so that JSON output is
// deterministic and two extractions of the same commit diff to nothing
// (a reproducibility requirement for the baseline).
func (g *Graph) Sort() {
	sort.Slice(g.Packages, func(i, j int) bool { return g.Packages[i].Path < g.Packages[j].Path })
	for pi := range g.Packages {
		p := &g.Packages[pi]
		sort.Strings(p.Files)
		sort.Slice(p.Surface, func(i, j int) bool { return p.Surface[i].Key() < p.Surface[j].Key() })
		sort.Slice(p.Schema, func(i, j int) bool { return p.Schema[i].Key() < p.Schema[j].Key() })
		sort.Slice(p.Invariants, func(i, j int) bool { return p.Invariants[i].Key() < p.Invariants[j].Key() })
		for ii := range p.Invariants {
			sort.Strings(p.Invariants[ii].Guards)
			sort.Strings(p.Invariants[ii].Exercises)
		}
		sort.Strings(p.Allow)
	}
	sort.Slice(g.Edges, func(i, j int) bool { return g.Edges[i].Key() < g.Edges[j].Key() })
	for ei := range g.Edges {
		sort.Strings(g.Edges[ei].Witnesses)
	}
	sort.Slice(g.LocalImplements, func(i, j int) bool { return g.LocalImplements[i].Key() < g.LocalImplements[j].Key() })
	for ei := range g.LocalImplements {
		sort.Strings(g.LocalImplements[ei].Witnesses)
	}
}

// InternalPaths returns the set of import paths that belong to the module.
func (g *Graph) InternalPaths() map[string]bool {
	m := make(map[string]bool, len(g.Packages))
	for _, p := range g.Packages {
		if p.Internal {
			m[p.Path] = true
		}
	}
	return m
}
