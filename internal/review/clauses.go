package review

import (
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// ClauseRow is one plan-declared contract clause on a package the PR touched,
// with whether any test appears to back it.
//
// This is the G4 evidence-gap report at its most modest: it says which promises
// the change implicates and which have nothing behind them. It does NOT verify a
// clause — no clause is checked, proved, or re-run here.
type ClauseRow struct {
	Package   string `json:"package"`
	ID        string `json:"id"`                  // e.g. BC-C2
	Statement string `json:"statement,omitempty"` // the prose promise, when the plan states one
	Class     string `json:"class,omitempty"`     // e.g. "evidenced: property_test"
	BoundTest string `json:"boundTest,omitempty"` // test found for this clause, if any
}

// Bound reports whether a test was found for the clause.
func (c ClauseRow) Bound() bool { return c.BoundTest != "" }

// touchedPackages returns every package the delta implicates at the package
// altitude: added or removed boxes, changed surface or schema, both endpoints of
// every added or removed arrow, packages whose guarding tests changed, and both
// sides of an interface whose implementer set changed.
//
// An edge endpoint counts because a clause on the far side of a new dependency is
// exactly the kind of promise a reviewer needs reminding of. Contract membership
// counts because it is an independent axis: an unexported type gaining a method
// that satisfies an interface changes no surface and adds no arrow when the
// coarse implements edge already existed, yet a contract just gained a member.
//
// d.ContractViolations is deliberately excluded. It requires --allow and reports
// STANDING violations rather than new ones, so keying "touched" off it would
// surface clauses on packages this change never went near.
func touchedPackages(d *delta.Delta) map[string]bool {
	touched := map[string]bool{}
	for _, p := range d.PackagesAdded {
		touched[p.Path] = true
	}
	for _, p := range d.PackagesRemoved {
		touched[p.Path] = true
	}
	for _, sc := range d.Surface {
		touched[sc.Package] = true
	}
	for _, sc := range d.SchemaChanges {
		touched[sc.Package] = true
	}
	for _, e := range d.EdgesAdded {
		touched[e.From] = true
		touched[e.To] = true
	}
	for _, e := range d.EdgesRemoved {
		touched[e.From] = true
		touched[e.To] = true
	}
	for _, ic := range d.Invariants {
		touched[ic.Package] = true
	}
	for _, cc := range d.Contracts {
		// Interface and implementers are "pkgpath.Name"; the declaring package
		// and every changed implementer's package are both implicated.
		if pkg := pkgOfQualified(cc.Interface); pkg != "" {
			touched[pkg] = true
		}
		for _, impl := range append(cc.ImplementersAdded, cc.ImplementersRemoved...) {
			if pkg := pkgOfQualified(impl); pkg != "" {
				touched[pkg] = true
			}
		}
	}
	return touched
}

// pkgOfQualified splits "example.com/m/pkg.TypeName" into its package path. The
// final dot is the separator: a Go type name cannot contain one, while the
// package path usually does ("github.com/..."), so LastIndex is the correct end
// to search from.
func pkgOfQualified(fq string) string {
	i := strings.LastIndex(fq, ".")
	if i < 0 {
		return ""
	}
	return fq[:i]
}

// planClauses extracts the plan-declared clauses for one package.
//
// The discriminator matters: graph.Invariant carries BOTH code-extracted tests
// and plan-declared clauses, and their fields mean different things — for a
// clause, Hash holds the class annotation rather than a body digest. Plan-sourced
// entries are the ones with File == "plan" (set by plan.parseContractEntry), so
// filtering on it keeps the two kinds from being rendered through each other.
func planClauses(pkg graph.Package) []graph.Invariant {
	var out []graph.Invariant
	for _, inv := range pkg.Invariants {
		if inv.File == "plan" {
			out = append(out, inv)
		}
	}
	return out
}

// bindClause looks for a test backing a clause, by the convention that clause
// BC-C2 is covered by a test named TestBC_C2 (dashes to underscores). Returns
// the test name, or "" when nothing matches.
//
// Deliberately the trivial binding. An explicit annotation scheme is a separate
// decision; reporting "no bound test" for an unconventionally-named test is a
// visible, correctable miss rather than a false claim of coverage.
func bindClause(id string, codeInvariants []graph.Invariant) string {
	want := "Test" + strings.ReplaceAll(id, "-", "_")
	for _, inv := range codeInvariants {
		if inv.File == "plan" {
			continue // a clause cannot evidence itself
		}
		if inv.Name == want {
			return inv.Name
		}
	}
	return ""
}

// ClauseReport lists the plan clauses on every package the delta touched.
// Returns nil when there is no plan, or when no touched package declares one —
// so the review renders no section rather than an empty one.
func ClauseReport(planGraph, head *graph.Graph, d *delta.Delta) []ClauseRow {
	if planGraph == nil || d == nil {
		return nil
	}
	touched := touchedPackages(d)
	if len(touched) == 0 {
		return nil
	}

	// Code-extracted invariants of the head graph, per package, for binding.
	codeByPkg := map[string][]graph.Invariant{}
	if head != nil {
		for _, p := range head.Packages {
			codeByPkg[p.Path] = p.Invariants
		}
	}

	var rows []ClauseRow
	for _, pkg := range planGraph.Packages {
		if !touched[pkg.Path] {
			continue
		}
		for _, cl := range planClauses(pkg) {
			rows = append(rows, ClauseRow{
				Package:   pkg.Path,
				ID:        cl.Name,
				Statement: cl.Statement,
				Class:     cl.Hash,
				BoundTest: bindClause(cl.Name, codeByPkg[pkg.Path]),
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Package != rows[j].Package {
			return rows[i].Package < rows[j].Package
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}
