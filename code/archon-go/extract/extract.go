// Package extract turns Go source at one commit into an ARCHON package-altitude
// graph. It composes go/packages (which already resolves imports, symbols, and
// visibility) rather than reimplementing name resolution — the extractor reports
// facts and decides no policy.
package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"archon-go/graph"

	"golang.org/x/tools/go/packages"
)

// Result carries the extracted graph plus any load diagnostics, so callers can
// distinguish "clean extraction" from "extracted with N package errors".
type Result struct {
	Graph      *graph.Graph
	NumErrors  int
	NumPkgs    int
}

// Extract loads all packages under dir (a module root) and builds the graph.
func Extract(dir string) (*Result, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedModule,
		Dir:   dir,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	modulePath := findModulePath(pkgs)
	if modulePath == "" {
		return nil, fmt.Errorf("could not determine module path under %s", dir)
	}

	g := &graph.Graph{Module: modulePath}
	res := &Result{Graph: g, NumPkgs: len(pkgs)}
	seenExternal := map[string]bool{}
	addExternalBox := func(path string) {
		if !strings.HasPrefix(path, modulePath) && !seenExternal[path] {
			seenExternal[path] = true
			g.Packages = append(g.Packages, graph.Package{
				Path: path, Name: pathBase(path), Internal: false,
			})
		}
	}

	// Gathered across the first pass so implements edges (a structural relation
	// that needs no import) can be computed between packages afterwards.
	var ifaces []ifaceDef
	var concretes []concreteDef

	for _, pkg := range pkgs {
		res.NumErrors += len(pkg.Errors)
		if !isInternal(pkg, modulePath) {
			continue
		}

		box := graph.Package{
			Path:       pkg.PkgPath,
			Name:       pkg.Name,
			Internal:   true,
			Files:      baseNames(pkg.GoFiles),
			Surface:    surfaceOf(pkg),
			Invariants: testInvariants(pkg),
		}
		g.Packages = append(g.Packages, box)

		// Import edges (the coarse "can reach" layer) + file-level witnesses.
		for _, e := range importEdges(pkg, modulePath) {
			g.Edges = append(g.Edges, e)
			// Record external import targets as lightweight boxes so that a
			// new third-party dependency is visible as a new node/edge.
			addExternalBox(e.To)
		}

		// Call edges (the "does use" layer): internal package -> internal
		// package it actually references, witnessed by the callee symbols.
		g.Edges = append(g.Edges, callEdges(pkg, modulePath)...)

		// Config edges (an "operational" edge kind, per the paper): the package
		// reads an environment variable or defines a command-line flag. The key
		// becomes a node so a new/removed key is visible in the delta.
		for _, e := range configEdges(pkg) {
			g.Edges = append(g.Edges, e)
			addExternalBox(e.To)
		}

		i, c := typeDefs(pkg)
		ifaces = append(ifaces, i...)
		concretes = append(concretes, c...)
	}

	// Implements edges: a concrete type in one package satisfying an exported
	// interface in another. This is invisible to imports (Go interfaces are
	// structural) and is exactly the "architecture as contract" relation.
	g.Edges = append(g.Edges, implementsEdges(concretes, ifaces)...)

	g.Sort()
	return res, nil
}

func findModulePath(pkgs []*packages.Package) string {
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Main {
			return p.Module.Path
		}
	}
	// Fallback: any module.
	for _, p := range pkgs {
		if p.Module != nil {
			return p.Module.Path
		}
	}
	return ""
}

func isInternal(pkg *packages.Package, modulePath string) bool {
	return pkg.PkgPath == modulePath || strings.HasPrefix(pkg.PkgPath, modulePath+"/")
}

// importEdges returns one edge per (this package -> imported package), with the
// witness set being the base filenames whose import block names that target.
func importEdges(pkg *packages.Package, modulePath string) []graph.Edge {
	witnesses := map[string]map[string]bool{} // target path -> set of files
	for _, file := range pkg.Syntax {
		fname := pathBase(pkg.Fset.File(file.Pos()).Name())
		for _, imp := range file.Imports {
			target := strings.Trim(imp.Path.Value, `"`)
			if _, ok := witnesses[target]; !ok {
				witnesses[target] = map[string]bool{}
			}
			witnesses[target][fname] = true
		}
	}

	var edges []graph.Edge
	for target, files := range witnesses {
		var w []string
		for f := range files {
			w = append(w, f)
		}
		edges = append(edges, graph.Edge{
			From:      pkg.PkgPath,
			To:        target,
			Kind:      "import",
			Witnesses: w,
		})
	}
	return edges
}

// callEdges returns one edge per (this package -> internal package it uses),
// where "uses" means a resolved reference to an exported func or method defined
// in that other package. Witnesses are the callee symbol names, so a new call
// into an already-used package is a witness-only change and stays empty; the
// first call into a package appears as a new call edge.
func callEdges(pkg *packages.Package, modulePath string) []graph.Edge {
	if pkg.TypesInfo == nil {
		return nil
	}
	witnesses := map[string]map[string]bool{} // target path -> set of callee names
	for _, obj := range pkg.TypesInfo.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || !fn.Exported() {
			continue
		}
		target := fn.Pkg().Path()
		if target == pkg.PkgPath || !isInternalPath(target, modulePath) {
			continue // same package, or external (already covered by imports)
		}
		if witnesses[target] == nil {
			witnesses[target] = map[string]bool{}
		}
		witnesses[target][calleeName(fn)] = true
	}

	var edges []graph.Edge
	for target, names := range witnesses {
		var w []string
		for n := range names {
			w = append(w, n)
		}
		edges = append(edges, graph.Edge{
			From: pkg.PkgPath, To: target, Kind: "call", Witnesses: w,
		})
	}
	return edges
}

// calleeName renders a callee as "Type.Method" for methods or "Func" for
// package-level functions, so witnesses read like the public surface.
func calleeName(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if ok && sig.Recv() != nil {
		recv := sig.Recv().Type()
		if p, isPtr := recv.(*types.Pointer); isPtr {
			recv = p.Elem()
		}
		if named, isNamed := recv.(*types.Named); isNamed {
			return named.Obj().Name() + "." + fn.Name()
		}
	}
	return fn.Name()
}

// flagKeyArg maps each stdlib `flag` definer to the argument position of the
// flag name. The plain forms take the name first (String(name, …)); the *Var
// forms take a destination pointer first, so the name is the second argument
// (StringVar(&p, name, …)). flag.Var(value, name, …) is likewise name-second.
var flagKeyArg = map[string]int{
	"String": 0, "Bool": 0, "Int": 0, "Int64": 0, "Uint": 0, "Uint64": 0,
	"Float64": 0, "Duration": 0, "Func": 0, "BoolFunc": 0, "TextVar": 1,
	"Var": 1, "StringVar": 1, "BoolVar": 1, "IntVar": 1, "Int64Var": 1,
	"UintVar": 1, "Uint64Var": 1, "Float64Var": 1, "DurationVar": 1,
}

// configEdges extracts "config" edges: references to a configuration key via a
// recognized, type-resolved config API. Two universal Go sources are covered:
// environment reads (os.Getenv / os.LookupEnv) and command-line flags (the
// stdlib flag package, including *flag.FlagSet methods, since those resolve to
// the same flag.* functions by type). The key is the string-literal argument at
// the API's key position; non-literal (computed) keys are skipped, as they
// cannot be resolved statically — a documented limitation shared with prior
// static configuration-extraction work (Rabkin & Katz, ICSE 2011). The callee
// is resolved through go/types, so a locally-defined function that merely shares
// a name (e.g. a package-local `Getenv`) is never mistaken for the real API.
func configEdges(pkg *packages.Package) []graph.Edge {
	if pkg.TypesInfo == nil {
		return nil
	}
	witnesses := map[string]map[string]bool{} // config-key node path -> {role@file}
	record := func(nodePath, wit string) {
		if witnesses[nodePath] == nil {
			witnesses[nodePath] = map[string]bool{}
		}
		witnesses[nodePath][wit] = true
	}

	for _, file := range pkg.Syntax {
		fname := pathBase(pkg.Fset.File(file.Pos()).Name())
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			fn, ok := pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
			if !ok || fn.Pkg() == nil {
				return true
			}
			switch p, name := fn.Pkg().Path(), fn.Name(); {
			case p == "os" && (name == "Getenv" || name == "LookupEnv"):
				if k, ok := literalArg(call, 0); ok {
					record("env:"+k, "read@"+fname)
				}
			case p == "flag":
				if idx, ok := flagKeyArg[name]; ok {
					if k, ok := literalArg(call, idx); ok {
						record("flag:"+k, "define@"+fname)
					}
				}
			}
			return true
		})
	}

	var edges []graph.Edge
	for target, wits := range witnesses {
		var w []string
		for x := range wits {
			w = append(w, x)
		}
		edges = append(edges, graph.Edge{From: pkg.PkgPath, To: target, Kind: "config", Witnesses: w})
	}
	return edges
}

// literalArg returns the unquoted string literal at argument position i, or
// false if that argument is absent or not a string literal.
func literalArg(call *ast.CallExpr, i int) (string, bool) {
	if i >= len(call.Args) {
		return "", false
	}
	lit, ok := call.Args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// testInvariants enumerates the test functions in a package's directory as
// candidate invariants. Detection is parse-only (fast, no type-checking): each
// *_test.go file in the directory is parsed, and every top-level Test*/Fuzz*
// function is recorded with a gofmt-normalized hash of the function, so a pure
// reformat is not mistaken for a change to what it asserts. A test is bound to
// the package that declares it (both the in-package and external `_test`
// variants live in that directory and guard the same box).
//
// v0 treats every test as a candidate invariant (an over-approximation). The
// planned refinement is to bind a test to the specific contract it exercises
// (interface-level via the surface it references), which both reduces noise and
// realizes the "interface-level property test across all implementers" model.
func testInvariants(pkg *packages.Package) []graph.Invariant {
	if len(pkg.GoFiles) == 0 {
		return nil
	}
	dir := filepath.Dir(pkg.GoFiles[0])
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	var out []graph.Invariant
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil || !isTestFuncName(fn.Name.Name) {
				continue
			}
			out = append(out, graph.Invariant{Name: fn.Name.Name, File: name, Hash: hashNode(fset, fn)})
		}
	}
	return out
}

// isTestFuncName reports whether a function is a Go test/fuzz entry point
// (Test* or Fuzz*), excluding the TestMain harness hook.
func isTestFuncName(name string) bool {
	if name == "TestMain" {
		return false
	}
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz")
}

// hashNode returns a short digest of a node in gofmt-normalized form, so
// whitespace/formatting changes do not alter the hash but semantic edits do.
func hashNode(fset *token.FileSet, n ast.Node) string {
	var b strings.Builder
	if err := format.Node(&b, fset, n); err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// ifaceDef and concreteDef carry the type facts needed to compute implements
// edges after every internal package has been walked.
type ifaceDef struct {
	pkgPath string
	name    string
	iface   *types.Interface
}

type concreteDef struct {
	pkgPath string
	name    string
	typ     types.Type // the named type (value form)
}

// typeDefs splits a package's exported named types into interfaces (with >=1
// method) and concrete types, for the cross-package implements computation.
func typeDefs(pkg *packages.Package) (ifaces []ifaceDef, concretes []concreteDef) {
	if pkg.Types == nil {
		return nil, nil
	}
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		if !ast.IsExported(name) {
			continue
		}
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		if iface, ok := named.Underlying().(*types.Interface); ok {
			if iface.NumMethods() > 0 { // skip empty interface{} — everything satisfies it
				ifaces = append(ifaces, ifaceDef{pkg.PkgPath, name, iface})
			}
			continue
		}
		concretes = append(concretes, concreteDef{pkg.PkgPath, name, named})
	}
	return ifaces, concretes
}

// implementsEdges emits one edge per (package of a concrete type -> package of
// an interface it satisfies), aggregating the "T |= I" pairs as witnesses. Only
// cross-package satisfaction is reported (a type satisfying its own package's
// interface is not an architectural boundary).
func implementsEdges(concretes []concreteDef, ifaces []ifaceDef) []graph.Edge {
	witnesses := map[string]map[string]bool{} // "from\x00to" -> set of "T |= I"
	for _, c := range concretes {
		ptr := types.NewPointer(c.typ)
		for _, i := range ifaces {
			if c.pkgPath == i.pkgPath {
				continue
			}
			if !types.Implements(c.typ, i.iface) && !types.Implements(ptr, i.iface) {
				continue
			}
			key := c.pkgPath + "\x00" + i.pkgPath
			if witnesses[key] == nil {
				witnesses[key] = map[string]bool{}
			}
			witnesses[key][c.name+" |= "+i.name] = true
		}
	}

	var edges []graph.Edge
	for key, pairs := range witnesses {
		fromTo := strings.SplitN(key, "\x00", 2)
		var w []string
		for p := range pairs {
			w = append(w, p)
		}
		edges = append(edges, graph.Edge{
			From: fromTo[0], To: fromTo[1], Kind: "implements", Witnesses: w,
		})
	}
	return edges
}

func isInternalPath(path, modulePath string) bool {
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

// surfaceOf returns the exported public surface of a package: exported
// package-scope objects, plus exported methods of exported named types.
func surfaceOf(pkg *packages.Package) []graph.Symbol {
	if pkg.Types == nil {
		return nil
	}
	rel := types.RelativeTo(pkg.Types)
	var out []graph.Symbol
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		if !ast.IsExported(name) {
			continue
		}
		obj := scope.Lookup(name)
		switch o := obj.(type) {
		case *types.Func:
			out = append(out, graph.Symbol{Kind: "func", Name: name, Sig: types.TypeString(o.Type(), rel)})
		case *types.TypeName:
			out = append(out, graph.Symbol{Kind: "type", Name: name})
			out = append(out, methodsOf(o, name, rel)...)
		case *types.Var:
			out = append(out, graph.Symbol{Kind: "var", Name: name, Sig: types.TypeString(o.Type(), rel)})
		case *types.Const:
			out = append(out, graph.Symbol{Kind: "const", Name: name, Sig: types.TypeString(o.Type(), rel)})
		}
	}
	return out
}

func methodsOf(tn *types.TypeName, typeName string, rel types.Qualifier) []graph.Symbol {
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return nil
	}
	var out []graph.Symbol
	for i := 0; i < named.NumMethods(); i++ {
		m := named.Method(i)
		if !ast.IsExported(m.Name()) {
			continue
		}
		out = append(out, graph.Symbol{
			Kind: "method",
			Name: typeName + "." + m.Name(),
			Sig:  types.TypeString(m.Type(), rel),
		})
	}
	return out
}

func baseNames(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, pathBase(p))
	}
	return out
}

func pathBase(p string) string {
	if strings.Contains(p, "/") {
		return p[strings.LastIndex(p, "/")+1:]
	}
	return filepath.Base(p)
}
