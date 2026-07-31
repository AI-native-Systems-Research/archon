// Package delta computes the ARCHON architectural delta between two package-
// altitude graphs. The unit of review is this delta, not the textual diff: a
// change that only moves files or rewrites function bodies leaves the same set
// of package edges and surfaces, so its package-altitude delta is empty.
package delta

import (
	"fmt"
	"sort"
	"strings"

	"archon-go/graph"
)

// PackageRef names a package that appeared or disappeared.
type PackageRef struct {
	Path     string `json:"path"`
	Internal bool   `json:"internal"`
}

// SurfaceChange records how one package's public surface changed.
type SurfaceChange struct {
	Package string         `json:"package"`
	Added   []graph.Symbol `json:"added,omitempty"`
	Removed []graph.Symbol `json:"removed,omitempty"`
}

// InvariantChange records how one package's guarding tests (candidate
// invariants) changed. Modified/Removed are the review-worthy cases: they mean
// a PR touched a promise the system was making.
type InvariantChange struct {
	Package  string   `json:"package"`
	Added    []string `json:"added,omitempty"`    // new tests (a new promise — additive, safe)
	Removed  []string `json:"removed,omitempty"`  // deleted tests (a promise dropped — flag)
	Modified []string `json:"modified,omitempty"` // changed test bodies (a promise altered — flag)

	// GuardedContracts names the interface (contract) nodes that the touched
	// (modified or removed) tests in this package were bound to, so review can
	// report which promise was altered — "a promise on scheduler.RoutingPolicy
	// changed" — not merely which test file moved.
	GuardedContracts []string `json:"guardedContracts,omitempty"`
}

// Delta is the package-altitude architectural delta from A to B.
type Delta struct {
	CommitA string `json:"commitA,omitempty"`
	CommitB string `json:"commitB,omitempty"`

	PackagesAdded   []PackageRef    `json:"packagesAdded,omitempty"`
	PackagesRemoved []PackageRef    `json:"packagesRemoved,omitempty"`
	EdgesAdded      []graph.Edge    `json:"edgesAdded,omitempty"`
	EdgesRemoved    []graph.Edge    `json:"edgesRemoved,omitempty"`
	Surface         []SurfaceChange `json:"surface,omitempty"`

	// SchemaChanges records added/removed serialized (wire/DB) fields per package
	// — a data-contract change. It is a separate axis from Surface and, like
	// Invariants, does not by itself flip the structural boundary verdict.
	SchemaChanges []SurfaceChange `json:"schema,omitempty"`

	// EmptyAtPackageAltitude is true when no internal box, no arrow, and no
	// public surface changed — i.e. the change is internal at this altitude.
	// This is the STRUCTURAL verdict; invariant changes are a separate axis
	// (Invariants) so that touching tests does not conflate with a boundary move.
	EmptyAtPackageAltitude bool `json:"emptyAtPackageAltitude"`

	// Invariants records tests added/modified/removed per package. A modified or
	// removed invariant is its own review event (a system promise was touched),
	// reported independently of the structural verdict above.
	Invariants []InvariantChange `json:"invariants,omitempty"`

	// ContractViolations lists internal dependencies the after-graph has that a
	// box's declared Allow-list does not permit (set by CheckContract).
	ContractViolations []Violation `json:"contractViolations,omitempty"`

	// Contracts records changes to interface membership (who implements each
	// interface). A new implementer must be covered by that interface's contract
	// test — the invariants/property-testing hook.
	Contracts []ContractChange `json:"contracts,omitempty"`
}

// ContractChange records how the set of implementers of one interface changed.
// An interface is a *contract*; the types that implement it are bound to that
// contract. A new implementer must be covered by the interface's contract test
// (and, per the adoption convention, that test update should be bundled in the
// same PR). This is derived at the witness level ("T |= I"), so it catches a new
// implementation even when the coarse implements edge between the two packages
// already existed (e.g. the package already implemented a different interface
// there) — something the edge-level diff alone would miss.
type ContractChange struct {
	Interface           string   `json:"interface"`
	ImplementersAdded   []string `json:"implementersAdded,omitempty"`
	ImplementersRemoved []string `json:"implementersRemoved,omitempty"`

	// Coverage of the interface in the after-graph, verified against the
	// contract test(s) bound to it (Invariant.Guards): Covered are implementers
	// a bound contract test exercises; Uncovered are implementers no bound test
	// exercises — an evidence gap. ContractTests names the bound tests. When
	// ContractTests is empty, the interface has no contract test at all: every
	// implementer is an obligation, not a verified gap.
	Covered       []string `json:"covered,omitempty"`
	Uncovered     []string `json:"uncovered,omitempty"`
	ContractTests []string `json:"contractTests,omitempty"`
}

// interfaceImplementers maps each interface ("pkgpath.Interface") to the set of
// concrete types ("pkgpath.Type") that implement it, read from the implements
// edges' witnesses ("T |= I"). The interface lives in the edge's To package;
// the implementing type lives in the From package.
func interfaceImplementers(g *graph.Graph) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind != "implements" {
			continue
		}
		for _, w := range e.Witnesses {
			parts := strings.SplitN(w, " |= ", 2)
			if len(parts) != 2 {
				continue
			}
			iface := e.To + "." + parts[1]
			impl := e.From + "." + parts[0]
			if out[iface] == nil {
				out[iface] = map[string]bool{}
			}
			out[iface][impl] = true
		}
	}
	return out
}

// contractCoverage computes, per interface in g, which of its implementers are
// covered by a bound contract test and which are not. An implementer is covered
// when some test whose Guards names the interface also Exercises that concrete
// type — i.e. an interface-level contract test actually drives it. This verifies
// the coverage obligation instead of merely asserting it: the implementer set is
// read from the same implements witnesses, so "covered" means the promise is
// tested for that implementation.
func contractCoverage(g *graph.Graph) map[string]*ContractChange {
	impls := interfaceImplementers(g)

	exercisedBy := map[string]map[string]bool{} // iface -> concrete ids a bound test exercises
	testsFor := map[string]map[string]bool{}    // iface -> bound test names
	for _, p := range g.Packages {
		for _, inv := range p.Invariants {
			for _, iface := range inv.Guards {
				if exercisedBy[iface] == nil {
					exercisedBy[iface] = map[string]bool{}
					testsFor[iface] = map[string]bool{}
				}
				for _, ex := range inv.Exercises {
					exercisedBy[iface][ex] = true
				}
				testsFor[iface][inv.Name] = true
			}
		}
	}

	out := map[string]*ContractChange{}
	for iface, implSet := range impls {
		cc := &ContractChange{Interface: iface}
		for impl := range implSet {
			if exercisedBy[iface][impl] {
				cc.Covered = append(cc.Covered, impl)
			} else {
				cc.Uncovered = append(cc.Uncovered, impl)
			}
		}
		for t := range testsFor[iface] {
			cc.ContractTests = append(cc.ContractTests, t)
		}
		sort.Strings(cc.Covered)
		sort.Strings(cc.Uncovered)
		sort.Strings(cc.ContractTests)
		out[iface] = cc
	}
	return out
}

// Coverage returns per-interface contract coverage for a SINGLE graph
// (implementers, covered, uncovered, and the bound contract tests), independent
// of any delta. The `evidence` command uses it to show, per contract, which
// implementers a bound test exercises. Only interfaces with at least one
// implementer are returned.
func Coverage(g *graph.Graph) []ContractChange {
	m := contractCoverage(g)
	out := make([]ContractChange, 0, len(m))
	for _, c := range m {
		if len(c.Covered)+len(c.Uncovered) == 0 {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Interface < out[j].Interface })
	return out
}

// Violation is an actual internal dependency that a box's structural contract
// (its Allow-list) does not permit. Introduced is true when this PR added the
// offending edge (it is in EdgesAdded), i.e. the review-worthy case.
type Violation struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Introduced bool   `json:"introduced"`
}

// CheckContract compares the after-graph's actual internal dependencies against
// the declared Allow-lists in `allow` (package path -> permitted internal
// targets). A package absent from `allow` is treated as undeclared (not
// checked), so partial, ratcheted policies work. Only internal import/call
// edges are subject to the structural contract. Results are attached to the
// delta and returned.
func (d *Delta) CheckContract(after *graph.Graph, allow map[string][]string) []Violation {
	internal := after.InternalPaths()
	added := map[string]bool{}
	for _, e := range d.EdgesAdded {
		added[e.Key()] = true
	}
	var out []Violation
	for _, e := range after.Edges {
		if e.Kind != "import" && e.Kind != "call" {
			continue
		}
		if !internal[e.From] || !internal[e.To] || e.From == e.To {
			continue
		}
		permitted, declared := allow[e.From]
		if !declared {
			continue // no policy for this box
		}
		if !contains(permitted, e.To) {
			out = append(out, Violation{From: e.From, To: e.To, Kind: e.Kind, Introduced: added[e.Key()]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	d.ContractViolations = out
	return out
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// Compute diffs two graphs. Packages are matched by import path (the stable
// identity at the package altitude); edges by (from, kind, to). Witness-only
// changes — same edge, different files — do NOT register, which is exactly why
// a pure file move produces an empty delta.
func Compute(a, b *graph.Graph) *Delta {
	d := &Delta{CommitA: a.Commit, CommitB: b.Commit}

	pa := indexPackages(a)
	pb := indexPackages(b)

	for path, p := range pb {
		if _, ok := pa[path]; !ok {
			d.PackagesAdded = append(d.PackagesAdded, PackageRef{Path: path, Internal: p.Internal})
		}
	}
	for path, p := range pa {
		if _, ok := pb[path]; !ok {
			d.PackagesRemoved = append(d.PackagesRemoved, PackageRef{Path: path, Internal: p.Internal})
		}
	}

	ea := indexEdges(a)
	eb := indexEdges(b)
	for k, e := range eb {
		if _, ok := ea[k]; !ok {
			d.EdgesAdded = append(d.EdgesAdded, e)
		}
	}
	for k, e := range ea {
		if _, ok := eb[k]; !ok {
			d.EdgesRemoved = append(d.EdgesRemoved, e)
		}
	}

	// Surface diff for packages present in both.
	for path, bp := range pb {
		ap, ok := pa[path]
		if !ok {
			continue
		}
		added, removed := diffSurface(ap.Surface, bp.Surface)
		if len(added) > 0 || len(removed) > 0 {
			d.Surface = append(d.Surface, SurfaceChange{Package: path, Added: added, Removed: removed})
		}
		if sa, sr := diffSurface(ap.Schema, bp.Schema); len(sa) > 0 || len(sr) > 0 {
			d.SchemaChanges = append(d.SchemaChanges, SurfaceChange{Package: path, Added: sa, Removed: sr})
		}
		if ic := diffInvariants(path, ap.Invariants, bp.Invariants); ic != nil {
			d.Invariants = append(d.Invariants, *ic)
		}
	}

	// Contract membership: which types implement each interface, and how that
	// changed. A new implementer is a coverage obligation for the interface's
	// contract test.
	ia := interfaceImplementers(a)
	ib := interfaceImplementers(b)
	seenIface := map[string]bool{}
	for iface := range ia {
		seenIface[iface] = true
	}
	for iface := range ib {
		seenIface[iface] = true
	}
	cov := contractCoverage(b)
	for iface := range seenIface {
		var added, removed []string
		for impl := range ib[iface] {
			if !ia[iface][impl] {
				added = append(added, impl)
			}
		}
		for impl := range ia[iface] {
			if !ib[iface][impl] {
				removed = append(removed, impl)
			}
		}
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		sort.Strings(added)
		sort.Strings(removed)
		cc := ContractChange{Interface: iface, ImplementersAdded: added, ImplementersRemoved: removed}
		// Attach verified coverage of the interface as it stands after the change,
		// so a newly-added implementer is reported as covered ✓ or an evidence gap.
		if c := cov[iface]; c != nil {
			cc.Covered = c.Covered
			cc.Uncovered = c.Uncovered
			cc.ContractTests = c.ContractTests
		}
		d.Contracts = append(d.Contracts, cc)
	}

	d.sortAll()
	d.EmptyAtPackageAltitude = d.isEmpty()
	return d
}

// isEmpty reports whether the boundary moved. External package add/remove is
// not counted directly: it can only occur alongside an edge add/remove, which
// is counted.
func (d *Delta) isEmpty() bool {
	for _, p := range d.PackagesAdded {
		if p.Internal {
			return false
		}
	}
	for _, p := range d.PackagesRemoved {
		if p.Internal {
			return false
		}
	}
	return len(d.EdgesAdded) == 0 && len(d.EdgesRemoved) == 0 && len(d.Surface) == 0
}

// diffInvariants compares a package's guarding tests between two versions,
// classifying each as added (new test), removed (deleted test), or modified
// (same name, different normalized body). Returns nil when nothing changed.
func diffInvariants(pkg string, a, b []graph.Invariant) *InvariantChange {
	am := map[string]graph.Invariant{}
	bm := map[string]graph.Invariant{}
	for _, i := range a {
		am[i.Key()] = i
	}
	for _, i := range b {
		bm[i.Key()] = i
	}
	ic := InvariantChange{Package: pkg}
	for k, bi := range bm {
		ai, ok := am[k]
		if !ok {
			ic.Added = append(ic.Added, bi.Name)
		} else if ai.Hash != bi.Hash {
			ic.Modified = append(ic.Modified, bi.Name)
		}
	}
	for k, ai := range am {
		if _, ok := bm[k]; !ok {
			ic.Removed = append(ic.Removed, ai.Name)
		}
	}
	if len(ic.Added) == 0 && len(ic.Removed) == 0 && len(ic.Modified) == 0 {
		return nil
	}
	// Report which contracts the touched (modified/removed) tests were bound to:
	// the promise that changed, not just the test name. Read guards from both
	// versions so a modified test whose binding shifted is still attributed.
	touched := map[string]bool{}
	for _, n := range ic.Modified {
		touched[n] = true
	}
	for _, n := range ic.Removed {
		touched[n] = true
	}
	gc := map[string]bool{}
	for _, inv := range a {
		if touched[inv.Name] {
			for _, g := range inv.Guards {
				gc[g] = true
			}
		}
	}
	for _, inv := range b {
		if touched[inv.Name] {
			for _, g := range inv.Guards {
				gc[g] = true
			}
		}
	}
	for g := range gc {
		ic.GuardedContracts = append(ic.GuardedContracts, g)
	}
	sort.Strings(ic.Added)
	sort.Strings(ic.Removed)
	sort.Strings(ic.Modified)
	sort.Strings(ic.GuardedContracts)
	return &ic
}

func diffSurface(a, b []graph.Symbol) (added, removed []graph.Symbol) {
	am := map[string]graph.Symbol{}
	bm := map[string]graph.Symbol{}
	for _, s := range a {
		am[s.Key()] = s
	}
	for _, s := range b {
		bm[s.Key()] = s
	}
	for k, s := range bm {
		if _, ok := am[k]; !ok {
			added = append(added, s)
		}
	}
	for k, s := range am {
		if _, ok := bm[k]; !ok {
			removed = append(removed, s)
		}
	}
	return added, removed
}

func indexPackages(g *graph.Graph) map[string]graph.Package {
	m := make(map[string]graph.Package, len(g.Packages))
	for _, p := range g.Packages {
		m[p.Path] = p
	}
	return m
}

func indexEdges(g *graph.Graph) map[string]graph.Edge {
	m := make(map[string]graph.Edge, len(g.Edges))
	for _, e := range g.Edges {
		m[e.Key()] = e
	}
	return m
}

func (d *Delta) sortAll() {
	sort.Slice(d.PackagesAdded, func(i, j int) bool { return d.PackagesAdded[i].Path < d.PackagesAdded[j].Path })
	sort.Slice(d.PackagesRemoved, func(i, j int) bool { return d.PackagesRemoved[i].Path < d.PackagesRemoved[j].Path })
	sort.Slice(d.EdgesAdded, func(i, j int) bool { return d.EdgesAdded[i].Key() < d.EdgesAdded[j].Key() })
	sort.Slice(d.EdgesRemoved, func(i, j int) bool { return d.EdgesRemoved[i].Key() < d.EdgesRemoved[j].Key() })
	sort.Slice(d.Surface, func(i, j int) bool { return d.Surface[i].Package < d.Surface[j].Package })
	sort.Slice(d.SchemaChanges, func(i, j int) bool { return d.SchemaChanges[i].Package < d.SchemaChanges[j].Package })
	sort.Slice(d.Invariants, func(i, j int) bool { return d.Invariants[i].Package < d.Invariants[j].Package })
	sort.Slice(d.Contracts, func(i, j int) bool { return d.Contracts[i].Interface < d.Contracts[j].Interface })
	for i := range d.Surface {
		sort.Slice(d.Surface[i].Added, func(a, b int) bool { return d.Surface[i].Added[a].Key() < d.Surface[i].Added[b].Key() })
		sort.Slice(d.Surface[i].Removed, func(a, b int) bool { return d.Surface[i].Removed[a].Key() < d.Surface[i].Removed[b].Key() })
	}
	for i := range d.SchemaChanges {
		sort.Slice(d.SchemaChanges[i].Added, func(a, b int) bool { return d.SchemaChanges[i].Added[a].Key() < d.SchemaChanges[i].Added[b].Key() })
		sort.Slice(d.SchemaChanges[i].Removed, func(a, b int) bool { return d.SchemaChanges[i].Removed[a].Key() < d.SchemaChanges[i].Removed[b].Key() })
	}
}

// Render produces a compact human-readable review report.
func (d *Delta) Render() string {
	var b strings.Builder
	if d.EmptyAtPackageAltitude {
		b.WriteString("ARCHITECTURAL DELTA: empty at package altitude\n")
		b.WriteString("  -> internal change; no package boundary moved; no architecture review required.\n")
		d.renderSchema(&b)
		d.renderInvariants(&b)
		d.renderViolations(&b)
		d.renderContracts(&b)
		return b.String()
	}
	b.WriteString("ARCHITECTURAL DELTA: boundary changed — review required\n")
	shorten := moduleShortener(d)

	for _, p := range d.PackagesAdded {
		fmt.Fprintf(&b, "  + box   %s%s\n", shorten(p.Path), nodeKindTag(p.Path, p.Internal))
	}
	for _, p := range d.PackagesRemoved {
		fmt.Fprintf(&b, "  - box   %s%s\n", shorten(p.Path), nodeKindTag(p.Path, p.Internal))
	}
	for _, e := range d.EdgesAdded {
		fmt.Fprintf(&b, "  + arrow %s -> %s [%s] (%d witness file(s))\n", shorten(e.From), shorten(e.To), e.Kind, len(e.Witnesses))
	}
	for _, e := range d.EdgesRemoved {
		fmt.Fprintf(&b, "  - arrow %s -> %s [%s]\n", shorten(e.From), shorten(e.To), e.Kind)
	}
	for _, sc := range d.Surface {
		for _, s := range sc.Added {
			fmt.Fprintf(&b, "  + surface %s.%s (%s)\n", shorten(sc.Package), s.Name, s.Kind)
		}
		for _, s := range sc.Removed {
			fmt.Fprintf(&b, "  - surface %s.%s (%s)\n", shorten(sc.Package), s.Name, s.Kind)
		}
	}
	d.renderSchema(&b)
	d.renderInvariants(&b)
	d.renderViolations(&b)
	d.renderContracts(&b)
	return b.String()
}

// renderSchema appends serialized-field (wire/DB schema) changes: a data
// contract that crosses a boundary gained or lost a field. Reported as its own
// axis so it reads distinctly from an API-signature change.
func (d *Delta) renderSchema(b *strings.Builder) {
	if len(d.SchemaChanges) == 0 {
		return
	}
	b.WriteString("SCHEMA CHANGED — a serialized data contract (wire/DB payload) changed shape\n")
	for _, sc := range d.SchemaChanges {
		for _, s := range sc.Added {
			fmt.Fprintf(b, "  + field %s.%s %s\n", shortRef(sc.Package), s.Name, s.Sig)
		}
		for _, s := range sc.Removed {
			fmt.Fprintf(b, "  - field %s.%s %s\n", shortRef(sc.Package), s.Name, s.Sig)
		}
	}
}

// renderContracts appends verified interface coverage: for each contract whose
// membership changed, a new implementer is reported as covered ✓ by a bound
// contract test, or as an evidence gap when no bound test exercises it. This is
// the mechanized form of the coverage obligation — ARCHON does not just ask for
// the test, it checks whether one exists and drives the new implementation.
func (d *Delta) renderContracts(b *strings.Builder) {
	if len(d.Contracts) == 0 {
		return
	}
	b.WriteString("CONTRACT COVERAGE — every implementer of a changed contract must be covered by that contract's test\n")
	for _, c := range d.Contracts {
		for _, impl := range c.ImplementersAdded {
			switch {
			case contains(c.Covered, impl):
				fmt.Fprintf(b, "  + %s now implements %s — covered by %s ✓\n",
					shortRef(impl), shortRef(c.Interface), joinRefs(c.ContractTests))
			case len(c.ContractTests) > 0:
				fmt.Fprintf(b, "  ! %s now implements %s — NOT covered by %s (evidence gap)\n",
					shortRef(impl), shortRef(c.Interface), joinRefs(c.ContractTests))
			default:
				fmt.Fprintf(b, "  ! %s now implements %s — no contract test guards this interface (evidence gap)\n",
					shortRef(impl), shortRef(c.Interface))
			}
		}
		for _, impl := range c.ImplementersRemoved {
			fmt.Fprintf(b, "  - %s no longer implements %s\n", shortRef(impl), shortRef(c.Interface))
		}
		// Pre-existing uncovered implementers (not added by this PR) are noted once
		// so a standing evidence gap on a changed contract stays visible.
		var standing []string
		for _, u := range c.Uncovered {
			if !contains(c.ImplementersAdded, u) {
				standing = append(standing, u)
			}
		}
		if len(standing) > 0 {
			fmt.Fprintf(b, "    (also uncovered on %s: %s)\n", shortRef(c.Interface), joinRefs(standing))
		}
	}
}

// joinRefs shortens each "pkgpath.Name" reference (test names pass through) and
// joins them for a compact one-line listing.
func joinRefs(xs []string) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = shortRef(x)
	}
	return strings.Join(out, ", ")
}

// shortRef trims a "pkgpath.Name" reference to "pkg.Name" for readable output.
func shortRef(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// renderViolations appends structural-contract violations (a box depends on
// something its Allow-list does not permit). Introduced ones are the flag.
func (d *Delta) renderViolations(b *strings.Builder) {
	if len(d.ContractViolations) == 0 {
		return
	}
	shorten := moduleShortener(d)
	b.WriteString("CONTRACT VIOLATIONS — review required (a box depends on something it is not allowed to)\n")
	for _, v := range d.ContractViolations {
		tag := ""
		if v.Introduced {
			tag = " (introduced by this change)"
		}
		fmt.Fprintf(b, "  ! %s -> %s [%s] not in allow-list%s\n", shorten(v.From), shorten(v.To), v.Kind, tag)
	}
}

// renderInvariants appends the invariant (guarding-test) changes. It is its own
// review axis: modified/removed invariants mean a promise was touched, even when
// the structural delta is empty.
func (d *Delta) renderInvariants(b *strings.Builder) {
	if len(d.Invariants) == 0 {
		return
	}
	flag := false
	for _, ic := range d.Invariants {
		if len(ic.Modified) > 0 || len(ic.Removed) > 0 {
			flag = true
		}
	}
	if flag {
		b.WriteString("INVARIANTS TOUCHED — review required (a system promise changed)\n")
	} else {
		b.WriteString("INVARIANTS: new guards added (additive)\n")
	}
	for _, ic := range d.Invariants {
		for _, n := range ic.Removed {
			fmt.Fprintf(b, "  - invariant %s.%s (guard removed)\n", shortRef(ic.Package), n)
		}
		for _, n := range ic.Modified {
			fmt.Fprintf(b, "  ~ invariant %s.%s (guard changed)\n", shortRef(ic.Package), n)
		}
		for _, n := range ic.Added {
			fmt.Fprintf(b, "  + invariant %s.%s (new guard)\n", shortRef(ic.Package), n)
		}
		if len(ic.GuardedContracts) > 0 && (len(ic.Modified) > 0 || len(ic.Removed) > 0) {
			fmt.Fprintf(b, "    → promise on %s changed\n", joinRefs(ic.GuardedContracts))
		}
	}
}

// nodeKindTag labels a box in the report by what it is: a synthetic operational
// node (endpoint / service / capability / config key) reads by its kind, a real
// third-party package as external, and an internal box carries no tag.
func nodeKindTag(path string, internal bool) string {
	if internal {
		return ""
	}
	switch {
	case strings.HasPrefix(path, "api:"):
		return " (endpoint)"
	case strings.HasPrefix(path, "service:"):
		return " (service)"
	case strings.HasPrefix(path, "cap:"):
		return " (capability)"
	case strings.HasPrefix(path, "env:"), strings.HasPrefix(path, "flag:"):
		return " (config)"
	}
	return " (external)"
}

// moduleShortener trims the shared module prefix so the report reads in short
// paths. The prefix is the longest shared slash-delimited prefix of every path
// the delta mentions.
func moduleShortener(d *Delta) func(string) string {
	var prefix string
	first := true
	fold := func(p string) {
		if first {
			prefix, first = p, false
			return
		}
		prefix = commonSlashPrefix(prefix, p)
	}
	for _, p := range d.PackagesAdded {
		fold(p.Path)
	}
	for _, p := range d.PackagesRemoved {
		fold(p.Path)
	}
	for _, e := range d.EdgesAdded {
		fold(e.From)
		fold(e.To)
	}
	for _, e := range d.EdgesRemoved {
		fold(e.From)
		fold(e.To)
	}
	for _, sc := range d.Surface {
		fold(sc.Package)
	}
	return func(p string) string {
		if prefix != "" && strings.HasPrefix(p, prefix+"/") {
			return strings.TrimPrefix(p, prefix+"/")
		}
		return p
	}
}

func commonSlashPrefix(a, b string) string {
	if a == "" {
		return b
	}
	// Keep only up to the shared leading path segments.
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	var shared []string
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			break
		}
		shared = append(shared, as[i])
	}
	return strings.Join(shared, "/")
}
