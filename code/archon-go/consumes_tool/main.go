// Command consumes reports, for every interface declared in a module's own
// packages, who CONSUMES it as an abstraction: any place that references the
// interface type in a consuming position (function parameter, result, struct
// field, var/const type, type assertion, type switch, alias). This is the
// "usage" altitude that sits below the contract/type view: an interface can be
// implemented without anyone ever depending on the abstraction, because Go
// satisfaction is implicit and structural.
//
// It also computes, deterministically via go/types:
//   - implementers: concrete named types in the module that satisfy the interface
//   - method signatures of each interface
//
// A conformance assertion `var _ Iface = (*Impl)(nil)` is NOT counted as a
// consumer: that is an implementer declaring it satisfies the interface, not a
// dependency on the abstraction. It is tallied separately.
//
// An interface with <= 1 implementer AND 0 real consumers is reported as a
// CANDIDATE unconsumed abstraction. This is a fact to validate, not a verdict:
// a one-implementer interface can be perfectly intentional (a test seam, a
// public boundary, a planned extension point). The tool never says "smell".
//
// Usage:
//   consumes <module-dir> <pkg-pattern> [--json]
//   e.g.  consumes ../inference-sim ./...
//         consumes ../inference-sim ./... --json > consumes.json
//
// Human summary on stdout by default; --json emits machine-readable facts.
// Deterministic: same module state in, byte-identical output out.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type consumerRef struct {
	Pkg  string `json:"pkg"`  // consuming package (module-relative)
	Kind string `json:"kind"` // param | result | field | var | assertion | type-switch | alias | type-ref
	Pos  string `json:"pos"`  // file:line
}

type ifaceEntry struct {
	obj   *types.TypeName
	iface *types.Interface
	fact  *ifaceFact
}

type ifaceFact struct {
	FQ                    string        `json:"interface"`    // module-relative pkg.Name
	Pkg                   string        `json:"pkg"`          // declaring package (module-relative)
	Name                  string        `json:"name"`
	Methods               []string      `json:"methods"`      // signatures
	Implementers          []string      `json:"implementers"` // module-relative fq
	Consumers             []consumerRef `json:"consumers"`
	ConsumerPkgs          []string      `json:"consumerPkgs"`
	ConformanceAssertions int           `json:"conformanceAssertions"`
	ConsumedCrossPackage  bool          `json:"consumedCrossPackage"`
	CandidateUnconsumed   bool          `json:"candidateUnconsumed"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: consumes <module-dir> <pkg-pattern> [--json]")
		os.Exit(2)
	}
	dir, pattern := os.Args[1], os.Args[2]
	asJSON := false
	for _, a := range os.Args[3:] {
		if a == "--json" {
			asJSON = true
		}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports |
			packages.NeedFiles | packages.NeedModule,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
	if len(pkgs) == 0 {
		fmt.Fprintln(os.Stderr, "no packages matched", pattern)
		os.Exit(1)
	}

	module := ""
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Main {
			module = p.Module.Path
			break
		}
	}
	if module == "" && len(pkgs) > 0 && pkgs[0].Module != nil {
		module = pkgs[0].Module.Path
	}

	absDir, _ := filepath.Abs(dir)

	// module-relative package path
	rel := func(path string) string {
		if module != "" && path == module {
			return "(root)"
		}
		if module != "" && strings.HasPrefix(path, module+"/") {
			return path[len(module)+1:]
		}
		return path
	}
	// type-string qualifier: module packages shown relative, others by base name
	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		if module != "" && strings.HasPrefix(p.Path(), module+"/") {
			return p.Path()[len(module)+1:]
		}
		if p.Path() == module {
			return ""
		}
		return p.Name()
	}
	posOf := func(pkg *packages.Package, n ast.Node) string {
		p := pkg.Fset.Position(n.Pos())
		f := p.Filename
		if absDir != "" && strings.HasPrefix(f, absDir+"/") {
			f = f[len(absDir)+1:]
		}
		return fmt.Sprintf("%s:%d", f, p.Line)
	}

	internal := func(p *packages.Package) bool {
		if p.Module != nil {
			return p.Module.Main
		}
		return module != "" && (p.PkgPath == module || strings.HasPrefix(p.PkgPath, module+"/"))
	}

	// ---- 1. collect declared interfaces + concrete named types in the module --
	byObj := map[*types.TypeName]*ifaceEntry{} // identity lookup for the consumer pass
	var ifaces []*ifaceEntry
	var concreteTypes []*types.TypeName // implementer candidates (module-only)

	for _, p := range pkgs {
		if !internal(p) || p.Types == nil {
			continue
		}
		scope := p.Types.Scope()
		for _, nm := range scope.Names() {
			obj := scope.Lookup(nm)
			tn, ok := obj.(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			if it, ok := named.Underlying().(*types.Interface); ok {
				if it.NumMethods() == 0 { // empty interface (any-like): skip noise
					continue
				}
				methods := make([]string, 0, it.NumMethods())
				for i := 0; i < it.NumMethods(); i++ {
					m := it.Method(i)
					sig := types.TypeString(m.Type(), qual)
					methods = append(methods, m.Name()+strings.TrimPrefix(sig, "func"))
				}
				sort.Strings(methods)
				fact := &ifaceFact{
					FQ:      rel(p.PkgPath) + "." + nm,
					Pkg:     rel(p.PkgPath),
					Name:    nm,
					Methods: methods,
				}
				e := &ifaceEntry{obj: tn, iface: it, fact: fact}
				ifaces = append(ifaces, e)
				byObj[tn] = e
			} else {
				concreteTypes = append(concreteTypes, tn)
			}
		}
	}

	// ---- 2. implementers (deterministic assignability check) -----------------
	for _, e := range ifaces {
		var impls []string
		for _, tn := range concreteTypes {
			if tn.Type() == e.obj.Type() {
				continue
			}
			t := tn.Type()
			if types.Implements(t, e.iface) || types.Implements(types.NewPointer(t), e.iface) {
				impls = append(impls, rel(tn.Pkg().Path())+"."+tn.Name())
			}
		}
		sort.Strings(impls)
		e.fact.Implementers = impls
	}

	// ---- 3. consumers (AST references to the interface type as an abstraction) -
	for _, p := range pkgs {
		if !internal(p) || p.TypesInfo == nil {
			continue
		}
		// parent map for context classification
		parent := map[ast.Node]ast.Node{}
		for _, f := range p.Syntax {
			ast.Walk(&parentTracker{parent: parent}, f)
		}
		for id, obj := range p.TypesInfo.Uses {
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			e, ok := byObj[tn]
			if !ok {
				continue
			}
			kind, skip := classify(id, parent)
			if kind == "conformance" {
				e.fact.ConformanceAssertions++
				continue
			}
			if skip {
				continue
			}
			e.fact.Consumers = append(e.fact.Consumers, consumerRef{
				Pkg:  rel(p.PkgPath),
				Kind: kind,
				Pos:  posOf(p, id),
			})
		}
	}

	// ---- 4. finalize derived fields ------------------------------------------
	for _, e := range ifaces {
		f := e.fact
		sort.Slice(f.Consumers, func(i, j int) bool {
			if f.Consumers[i].Pos != f.Consumers[j].Pos {
				return f.Consumers[i].Pos < f.Consumers[j].Pos
			}
			return f.Consumers[i].Kind < f.Consumers[j].Kind
		})
		pkgset := map[string]bool{}
		for _, c := range f.Consumers {
			pkgset[c.Pkg] = true
			if c.Pkg != f.Pkg {
				f.ConsumedCrossPackage = true
			}
		}
		f.ConsumerPkgs = sortedKeys(pkgset)
		if f.Implementers == nil {
			f.Implementers = []string{}
		}
		if f.Consumers == nil {
			f.Consumers = []consumerRef{}
		}
		f.CandidateUnconsumed = len(f.Implementers) <= 1 && len(f.Consumers) == 0
	}

	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].fact.FQ < ifaces[j].fact.FQ })

	if asJSON {
		facts := make([]*ifaceFact, len(ifaces))
		for i, e := range ifaces {
			facts[i] = e.fact
		}
		out := map[string]any{"module": module, "interfaces": facts}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}

	printSummary(module, ifaces)
}

// parentTracker records each node's parent as the AST is walked.
type parentTracker struct {
	parent map[ast.Node]ast.Node
	cur    ast.Node
}

func (v *parentTracker) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	v.parent[n] = v.cur
	return &parentTracker{parent: v.parent, cur: n}
}

// classify walks up from a reference ident to name the consuming position and
// decide whether it should be counted. Returns ("conformance", _) for a
// `var _ Iface = ...` assertion so the caller can tally it separately.
func classify(id ast.Node, parent map[ast.Node]ast.Node) (kind string, skip bool) {
	var child ast.Node = id
	for n := parent[id]; n != nil; n = parent[n] {
		switch p := n.(type) {
		case *ast.TypeAssertExpr:
			return "assertion", false
		case *ast.TypeSwitchStmt:
			return "type-switch", false
		case *ast.ValueSpec:
			if allBlank(p.Names) {
				return "conformance", false
			}
			return "var", false
		case *ast.Field:
			// climb once more to learn whether this field list is params,
			// results, a struct, or an interface embed.
			fl, _ := parent[p].(*ast.FieldList)
			switch owner := parent[fl].(type) {
			case *ast.FuncType:
				if owner.Params == fl {
					return "param", false
				}
				if owner.Results == fl {
					return "result", false
				}
				return "param", false
			case *ast.StructType:
				return "field", false
			case *ast.InterfaceType:
				// embedded in another interface's declaration: not consumption
				return "iface-embed", true
			}
			return "field", false
		case *ast.TypeSpec:
			// `type Foo = Iface` (alias) or `type Foo Iface`: a real dependency.
			// The interface's OWN declaration is a Def, not in Uses, so it never
			// reaches here.
			if p.Name != nil && p.Name == child {
				return "type-def", true
			}
			return "alias", false
		case *ast.InterfaceType:
			// reached an interface literal without a Field frame: embed/decl side
			return "iface-embed", true
		}
		child = n
	}
	return "type-ref", false
}

func allBlank(names []*ast.Ident) bool {
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		if n.Name != "_" {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func printSummary(module string, ifaces []*ifaceEntry) {
	fmt.Printf("# Interface consumption (usage altitude)\n")
	fmt.Printf("module: %s\n", module)
	nCand := 0
	for _, e := range ifaces {
		if e.fact.CandidateUnconsumed {
			nCand++
		}
	}
	fmt.Printf("%d interface(s), %d candidate unconsumed (<=1 implementer, 0 consumers)\n\n", len(ifaces), nCand)

	for _, e := range ifaces {
		f := e.fact
		flag := ""
		if f.CandidateUnconsumed {
			flag = "   [CANDIDATE: unconsumed abstraction]"
		}
		fmt.Printf("%s%s\n", f.FQ, flag)
		fmt.Printf("  implementers: %d %v\n", len(f.Implementers), f.Implementers)
		fmt.Printf("  consumers:    %d", len(f.Consumers))
		if len(f.ConsumerPkgs) > 0 {
			fmt.Printf("  in %v", f.ConsumerPkgs)
		}
		if f.ConsumedCrossPackage {
			fmt.Printf("  (cross-package)")
		}
		fmt.Printf("\n")
		for _, c := range f.Consumers {
			fmt.Printf("    - %-11s %s  (%s)\n", c.Kind, c.Pkg, c.Pos)
		}
		if f.ConformanceAssertions > 0 {
			fmt.Printf("    (%d conformance assertion(s), not counted as consumers)\n", f.ConformanceAssertions)
		}
		fmt.Printf("\n")
	}

	fmt.Printf("Note: candidate unconsumed is a FACT to validate, not a verdict. A\n")
	fmt.Printf("one-implementer interface with no consumers can be intentional (a test\n")
	fmt.Printf("seam, a public boundary, a planned extension point).\n")
}
