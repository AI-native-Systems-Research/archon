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
	Graph     *graph.Graph
	NumErrors int
	NumPkgs   int
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
			Schema:     schemaOf(pkg),
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

		// Service edges (operational): importing a known client library is a
		// dependency on the runtime service behind it (Postgres, Redis, Kafka…).
		// The service becomes a "service:NAME" node, so a PR that wires in a new
		// backing service shows up as a new arrow, not buried in the diff.
		for _, e := range serviceEdges(pkg) {
			g.Edges = append(g.Edges, e)
			addExternalBox(e.To)
		}

		// Protocol edges (operational): HTTP routes the package registers. Each
		// endpoint becomes an "api:METHOD /path" node, so adding or removing a
		// route is a visible boundary change — the API surface, not an import.
		for _, e := range protocolEdges(pkg) {
			g.Edges = append(g.Edges, e)
			addExternalBox(e.To)
		}

		// Capability edges (operational): importing an escape-hatch package
		// (unsafe, reflect, os/exec, syscall, net, plugin) grants a capability
		// that can observe or affect the world outside the box's surface — a
		// candidate hidden channel for the boundary-locality assumption
		// (Assumption: surface-mediated interaction).
		for _, e := range capabilityEdges(pkg) {
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

	// Bind guarding tests to the contracts they exercise (interfaces they touch
	// as Guards, concrete types as Exercises), so the delta can verify that every
	// implementer of an interface is covered by that interface's contract test.
	// Best-effort and separate from the structural extraction above, so a binding
	// failure never perturbs the graph's structure.
	ifaceSet := make(map[string]bool, len(ifaces))
	for _, i := range ifaces {
		ifaceSet[i.pkgPath+"."+i.name] = true
	}
	concreteSet := make(map[string]bool, len(concretes))
	for _, c := range concretes {
		concreteSet[c.pkgPath+"."+c.name] = true
	}
	bindInvariants(g, dir, ifaceSet, concreteSet)

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

// httpVerbs is the set of HTTP methods recognized in route patterns and as
// verb-named router methods.
var httpVerbs = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// protocolEdges extracts the HTTP endpoints a package registers, as
// "api:METHOD /path" nodes. It recognizes two common, syntactic conventions
// (no framework dependency): Go 1.22 method-prefixed patterns passed to
// Handle/HandleFunc ("POST /tasks"), and verb-named router methods
// (r.Get("/tasks", …)). String patterns built by concatenation with a base-URL
// variable are folded from their literal parts. This is a heuristic extractor:
// dynamically-computed routes are not resolved (a documented limitation, as with
// non-literal config keys).
func protocolEdges(pkg *packages.Package) []graph.Edge {
	witnesses := map[string]map[string]bool{} // "api:METHOD /path" -> {register@file}
	record := func(node, file string) {
		if witnesses[node] == nil {
			witnesses[node] = map[string]bool{}
		}
		witnesses[node]["register@"+file] = true
	}
	for _, file := range pkg.Syntax {
		fname := pathBase(pkg.Fset.File(file.Pos()).Name())
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			name := sel.Sel.Name
			arg, ok := concatStringLiterals(call.Args[0])
			if !ok {
				return true
			}
			switch {
			// Verb-named router method: r.Get("/tasks", handler).
			case httpVerbs[strings.ToUpper(name)] && strings.HasPrefix(arg, "/"):
				record("api:"+strings.ToUpper(name)+" "+arg, fname)
			// Method-prefixed pattern: m.HandleFunc("POST /tasks", handler).
			case name == "HandleFunc" || name == "Handle":
				if m, p, ok := splitMethodPattern(arg); ok {
					record("api:"+m+" "+p, fname)
				}
			}
			return true
		})
	}
	var edges []graph.Edge
	for node, wits := range witnesses {
		var w []string
		for x := range wits {
			w = append(w, x)
		}
		edges = append(edges, graph.Edge{From: pkg.PkgPath, To: node, Kind: "protocol", Witnesses: w})
	}
	return edges
}

// concatStringLiterals folds a string expression built from literals joined by
// `+`, treating non-literal operands (e.g. a base-URL variable) as empty. It
// returns the concatenation and whether at least one string literal was present.
func concatStringLiterals(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if s, err := strconv.Unquote(x.Value); err == nil {
				return s, true
			}
		}
		return "", false
	case *ast.ParenExpr:
		return concatStringLiterals(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			l, lok := concatStringLiterals(x.X)
			r, rok := concatStringLiterals(x.Y)
			if lok || rok {
				return l + r, true
			}
		}
	}
	return "", false
}

// splitMethodPattern parses a Go 1.22 "METHOD /path" ServeMux pattern into its
// method and path, returning ok only when the leading token is an HTTP verb and
// the remainder is a path.
func splitMethodPattern(pat string) (method, path string, ok bool) {
	i := strings.IndexByte(pat, ' ')
	if i <= 0 {
		return "", "", false
	}
	method = strings.ToUpper(pat[:i])
	path = strings.TrimSpace(pat[i+1:])
	if !httpVerbs[method] || !strings.HasPrefix(path, "/") {
		return "", "", false
	}
	return method, path, true
}

// serviceByPrefix maps a client-library import-path prefix to the runtime
// service it implies. Importing the library is the mechanically-visible signal
// that the package depends on that external service — the operational edge a
// microservice review most wants surfaced.
var serviceByPrefix = []struct{ prefix, service string }{
	{"github.com/jackc/pgx", "Postgres"},
	{"github.com/lib/pq", "Postgres"},
	{"github.com/bradfitz/gomemcache", "Memcached"},
	{"github.com/elastic/go-elasticsearch", "Elasticsearch"},
	{"github.com/olivere/elastic", "Elasticsearch"},
	{"github.com/redis/go-redis", "Redis"},
	{"github.com/go-redis/redis", "Redis"},
	{"github.com/gomodule/redigo", "Redis"},
	{"github.com/confluentinc/confluent-kafka-go", "Kafka"},
	{"github.com/segmentio/kafka-go", "Kafka"},
	{"github.com/IBM/sarama", "Kafka"},
	{"github.com/Shopify/sarama", "Kafka"},
	{"github.com/rabbitmq/amqp091-go", "RabbitMQ"},
	{"github.com/streadway/amqp", "RabbitMQ"},
	{"github.com/hashicorp/vault", "Vault"},
	{"go.mongodb.org/mongo-driver", "MongoDB"},
	{"github.com/aws/aws-sdk-go", "AWS"},
}

// serviceEdges emits one edge per (package -> "service:NAME") for each backing
// service the package's imports imply, witnessed by "import@file". The service
// node is synthetic (external), so a newly-wired backend is a new arrow.
func serviceEdges(pkg *packages.Package) []graph.Edge {
	return importClassifiedEdges(pkg, "service", func(path string) (string, bool) {
		for _, s := range serviceByPrefix {
			if path == s.prefix || strings.HasPrefix(path, s.prefix+"/") {
				return "service:" + s.service, true
			}
		}
		return "", false
	})
}

// capabilityEdges emits one edge per (package -> "cap:NAME") for each
// escape-hatch package the box imports. These are deliberately restricted to
// capabilities that let code reach outside its surface — the channels the
// boundary-locality theorem assumes away — rather than every stdlib import.
func capabilityEdges(pkg *packages.Package) []graph.Edge {
	return importClassifiedEdges(pkg, "capability", func(path string) (string, bool) {
		switch {
		case path == "unsafe":
			return "cap:unsafe", true
		case path == "reflect":
			return "cap:reflect", true
		case path == "plugin":
			return "cap:plugin", true
		case path == "os/exec":
			return "cap:exec", true
		case path == "syscall" || strings.HasPrefix(path, "golang.org/x/sys"):
			return "cap:syscall", true
		case path == "net" || strings.HasPrefix(path, "net/"):
			return "cap:net", true
		}
		return "", false
	})
}

// importClassifiedEdges scans a package's imports, and for each import the
// classify function maps to a synthetic node, emits a typed edge to that node
// witnessed by the importing files. Shared by the service and capability
// operational-edge extractors.
func importClassifiedEdges(pkg *packages.Package, kind string, classify func(path string) (string, bool)) []graph.Edge {
	witnesses := map[string]map[string]bool{} // node -> {import@file}
	for _, file := range pkg.Syntax {
		fname := pathBase(pkg.Fset.File(file.Pos()).Name())
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			node, ok := classify(path)
			if !ok {
				continue
			}
			if witnesses[node] == nil {
				witnesses[node] = map[string]bool{}
			}
			witnesses[node]["import@"+fname] = true
		}
	}
	var edges []graph.Edge
	for node, wits := range witnesses {
		var w []string
		for x := range wits {
			w = append(w, x)
		}
		edges = append(edges, graph.Edge{From: pkg.PkgPath, To: node, Kind: kind, Witnesses: w})
	}
	return edges
}

// testInvariants enumerates the test functions in a package's directory as
// candidate invariants. Detection is parse-only (fast, no type-checking): each
// *_test.go file in the directory is parsed, and every top-level Test*/Fuzz*
// function is recorded with a gofmt-normalized hash of the function, so a pure
// reformat is not mistaken for a change to what it asserts. A test is bound to
// the package that declares it (both the in-package and external `_test`
// variants live in that directory and guard the same box).
//
// This pass enumerates the candidate invariants and their Name/File/Hash; the
// binding of a test to the specific contract it exercises (its Guards and
// Exercises, interface-level via the types it references) is added afterward by
// bindInvariants, which realizes the "interface-level contract test across all
// implementers" model.
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

// bindInvariants type-checks the repository's test files (a second load with
// Tests:true, which exposes typed test ASTs the structural pass deliberately
// skips) and binds each guarding test to the contract it exercises: interfaces
// it references become the test's Guards, concrete types it references become
// its Exercises. Both are restricted to types the graph already knows (its
// interfaces and concretes), so the binding uses the SAME identities as the
// implements edges ("pkgpath.Name"). This is best-effort: a load failure or an
// unresolved test simply leaves the invariant a bare candidate, never an error,
// so the structural graph is unaffected.
func bindInvariants(g *graph.Graph, dir string, ifaceSet, concreteSet map[string]bool) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
		Dir:   dir,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return // graceful: leave invariants unbound
	}

	// basePkgPath -> testName -> {guards, exercises}
	type binding struct{ guards, exercises map[string]bool }
	byTest := map[string]map[string]*binding{}

	for _, pkg := range pkgs {
		base, ok := testBasePath(pkg.PkgPath)
		if !ok || pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Name == nil || fn.Body == nil || !isTestFuncName(fn.Name.Name) {
					continue
				}
				guards, exercises := referencedContracts(fn, pkg.TypesInfo, ifaceSet, concreteSet)
				if len(guards) == 0 && len(exercises) == 0 {
					continue
				}
				if byTest[base] == nil {
					byTest[base] = map[string]*binding{}
				}
				b := byTest[base][fn.Name.Name]
				if b == nil {
					b = &binding{guards: map[string]bool{}, exercises: map[string]bool{}}
					byTest[base][fn.Name.Name] = b
				}
				for k := range guards {
					b.guards[k] = true
				}
				for k := range exercises {
					b.exercises[k] = true
				}
			}
		}
	}

	for pi := range g.Packages {
		p := &g.Packages[pi]
		tests := byTest[p.Path]
		if tests == nil {
			continue
		}
		for ii := range p.Invariants {
			if b := tests[p.Invariants[ii].Name]; b != nil {
				p.Invariants[ii].Guards = setKeys(b.guards)
				p.Invariants[ii].Exercises = setKeys(b.exercises)
			}
		}
	}
}

// testBasePath maps a package produced under Tests:true back to the base package
// its tests guard. go/packages returns, for a package with tests, the plain
// package, an in-package augmented variant (same path), an external "pkg_test"
// package, and a synthetic "pkg.test" main binary. External tests strip the
// "_test" suffix; the synthetic main is skipped; everything else is its own base.
func testBasePath(path string) (string, bool) {
	switch {
	case strings.HasSuffix(path, ".test"):
		return "", false // synthetic test-main binary
	case strings.HasSuffix(path, "_test"):
		return strings.TrimSuffix(path, "_test"), true
	default:
		return path, true
	}
}

// referencedContracts walks a test function and collects the graph-known named
// types it references, split into interfaces it touches (guards) and concrete
// types it touches (exercises). References are read from expression types
// (so `[]store.Store{mem.New()}` yields the interface Store and the concrete
// Mem) and from type-name uses.
func referencedContracts(fn *ast.FuncDecl, info *types.Info, ifaceSet, concreteSet map[string]bool) (guards, exercises map[string]bool) {
	guards = map[string]bool{}
	exercises = map[string]bool{}
	record := func(t types.Type) {
		named := namedOf(t)
		if named == nil || named.Obj().Pkg() == nil {
			return
		}
		id := named.Obj().Pkg().Path() + "." + named.Obj().Name()
		if ifaceSet[id] {
			guards[id] = true
		}
		if concreteSet[id] {
			exercises[id] = true
		}
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if tv, ok := info.Types[e]; ok && tv.Type != nil {
				record(tv.Type)
			}
		}
		if id, ok := n.(*ast.Ident); ok {
			if tn, ok := info.Uses[id].(*types.TypeName); ok {
				record(tn.Type())
			}
		}
		return true
	})
	return guards, exercises
}

// namedOf unwraps pointer/slice/array wrappers to the underlying named type
// (nil for anything that does not bottom out in a named type).
func namedOf(t types.Type) *types.Named {
	for {
		switch x := t.(type) {
		case *types.Named:
			return x
		case *types.Pointer:
			t = x.Elem()
		case *types.Slice:
			t = x.Elem()
		case *types.Array:
			t = x.Elem()
		default:
			return nil
		}
	}
}

func setKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
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

// serdeTagKeys are the struct-tag keys that mark a field as part of a
// serialized (wire, DB, or event) representation — i.e. a data contract that
// crosses a box boundary.
var serdeTagKeys = []string{"json:", "gob:", "db:", "bson:", "xml:", "yaml:", "protobuf:", "avro:", "mapstructure:"}

// schemaOf returns the serialized fields of a package's exported struct types:
// the wire/DB schema, as distinct from the public API surface. A field counts
// when it is exported and carries a recognized serialization tag, so adding or
// removing a field on a payload/row struct shows up as a schema change even
// though it is not an API-signature change. This is a separate axis from
// surfaceOf so it never conflates with the structural boundary verdict.
func schemaOf(pkg *packages.Package) []graph.Symbol {
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
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		st, ok := tn.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			if !f.Exported() || !hasSerdeTag(st.Tag(i)) {
				continue
			}
			out = append(out, graph.Symbol{
				Kind: "field",
				Name: name + "." + f.Name(),
				Sig:  types.TypeString(f.Type(), rel),
			})
		}
	}
	return out
}

func hasSerdeTag(tag string) bool {
	for _, k := range serdeTagKeys {
		if strings.Contains(tag, k) {
			return true
		}
	}
	return false
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
