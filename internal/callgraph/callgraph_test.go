package callgraph_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/callgraph"
)

func build(t *testing.T, fixture string, mode callgraph.Mode) *callgraph.Graph {
	t.Helper()
	g, err := callgraph.Build("testdata/"+fixture, "./...", mode)
	if err != nil {
		t.Fatalf("Build(%s, %s): %v", fixture, mode, err)
	}
	return g
}

// edgeLines renders the graph as "caller -> callee" lines, annotating the ones
// that came from dispatch with the interface method they went through. The order
// is the graph's own, so the determinism test sees the order and not just the
// set.
func edgeLines(g *callgraph.Graph) []string {
	var out []string
	for _, e := range g.SortedEdges() {
		line := fmt.Sprintf("%s -> %s", g.Label[e.From], g.Label[e.To])
		if w := g.Edges[e]; w != "" {
			line += " [via " + w + "]"
		}
		out = append(out, line)
	}
	return out
}

func sorted(lines []string) []string {
	out := append([]string(nil), lines...)
	sort.Strings(out)
	return out
}

func dispatchLines(g *callgraph.Graph) []string {
	var out []string
	for _, l := range edgeLines(g) {
		if strings.Contains(l, " [via ") {
			out = append(out, l)
		}
	}
	return out
}

// TestInterfaceCallsBecomeEdges is the issue: s.Get() resolves to the interface
// method, which has no body, so the static walk drops the call. It also pins
// down where the caller has to be attributed from — a closure, a go statement, a
// defer, and the method of a generic type all have to land on the enclosing
// declaration — and that each edge names the method it dispatched through.
func TestInterfaceCallsBecomeEdges(t *testing.T) {
	static := build(t, "iface", callgraph.Static)
	cha := build(t, "iface", callgraph.CHA)

	t.Log("static mode:\n  " + strings.Join(edgeLines(static), "\n  "))
	t.Log("cha mode:\n  " + strings.Join(edgeLines(cha), "\n  "))

	if got := dispatchLines(static); len(got) != 0 {
		t.Errorf("static mode resolved dispatch, it must not: %v", got)
	}
	// Static mode is the behaviour this refactor has to preserve, so assert what
	// it produces and not merely what it lacks. app.UseBox -> app.Box.Fill is
	// absent because go/types resolves the call to the method of the
	// instantiated Box[int], which is not the declared function — that is how
	// the walk has always behaved and this change does not touch it.
	wantStatic := []string{
		"app.CallHelper -> app.Helper",
		"app.CallStoreHelper -> store.Helper",
		"app.Direct -> store.Mem.Get",
		"app.Mixed -> store.Mem.Get",
	}
	if got, want := sorted(edgeLines(static)), wantStatic; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("static edges:\n got %v\nwant %v", got, want)
	}

	want := []string{
		"app.Box.Fill -> store.Mem.Get [via store.Store.Get]",
		"app.InClosure -> store.Mem.Get [via store.Store.Get]",
		"app.InDefer -> store.Mem.Get [via store.Store.Get]",
		"app.InGo -> store.Mem.Get [via store.Store.Get]",
		"app.Serve -> store.Mem.Get [via store.Store.Get]",
		// Two call sites, through two interfaces, produce this one edge. The
		// witness is the lower of the two names so it cannot depend on which
		// site was visited first.
		"app.Two -> store.Mem.Get [via store.Getter.Get]",
	}
	got := sorted(dispatchLines(cha))
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("cha dispatch edges:\n got %v\nwant %v", got, want)
	}

	// app.Mixed is deliberately absent from that list: dispatch reaches the same
	// pair as its direct call, and relabelling the edge would hide the direct
	// call behind a dashed arrow.
	for _, l := range dispatchLines(cha) {
		if strings.HasPrefix(l, "app.Mixed ->") {
			t.Errorf("a direct call was relabelled as an interface call: %s", l)
		}
	}
}

// TestCHAKeepsEveryStaticEdge is the safety net at fixture scale: resolving
// dispatch may only add.
func TestCHAKeepsEveryStaticEdge(t *testing.T) {
	static := build(t, "iface", callgraph.Static)
	cha := build(t, "iface", callgraph.CHA)

	have := map[string]bool{}
	for _, l := range edgeLines(cha) {
		have[strings.SplitN(l, " [via ", 2)[0]] = true
	}
	for _, l := range edgeLines(static) {
		if !have[l] {
			t.Errorf("cha lost a static edge: %s", l)
		}
	}
	if len(edgeLines(static)) == 0 {
		t.Fatal("no static edges, so this test would pass on an empty graph")
	}
	if len(edgeLines(cha)) <= len(edgeLines(static)) {
		t.Errorf("cha added nothing: static %d edges, cha %d", len(edgeLines(static)), len(edgeLines(cha)))
	}
}

// TestFuncValueCallIsNotDispatch guards the filter to invoke-mode call sites.
// SSA also resolves a call made through a function value, by pointing it at
// every function of that signature — apply.Apply calls its parameter, and
// hidden.Squirrel merely has the same signature and is imported by nothing. An
// edge between them would be an invention, and at module scale these are what
// manufacture dependencies between unrelated packages.
func TestFuncValueCallIsNotDispatch(t *testing.T) {
	cha := build(t, "iface", callgraph.CHA)
	for _, l := range edgeLines(cha) {
		if strings.HasPrefix(l, "apply.Apply ->") || strings.Contains(l, "hidden.Squirrel") {
			t.Errorf("edge through a function value: %s", l)
		}
	}
}

// TestNothingLeavesTheModule: CHA returns edges into the standard library and
// every dependency, and a call graph of the module has no business showing them.
// app.OutOfModule calls fmt.Sprint.
func TestNothingLeavesTheModule(t *testing.T) {
	for _, mode := range []callgraph.Mode{callgraph.Static, callgraph.CHA} {
		g := build(t, "iface", mode)
		for f := range g.Defined {
			if !strings.HasPrefix(g.Pkg[f], "example.com/iface") {
				t.Errorf("mode %s: %s is not in the module", mode, g.Pkg[f])
			}
		}
		// Check the endpoints, not the rendered line: a function outside the
		// module has no label, so an edge to it renders as "caller -> " and a
		// check on the text cannot see it.
		for _, e := range g.SortedEdges() {
			if !g.Defined[e.From] || !g.Defined[e.To] {
				t.Errorf("mode %s: edge with an endpoint outside the module: %q -> %q",
					mode, g.ID[e.From], g.ID[e.To])
			}
		}
	}
}

// TestUnresolvedCallSitesArePinpointed: a method value dispatches inside a
// wrapper go/ssa synthesises whose object is the interface method, and an
// interface method has no body, so there is no implementation to draw the edge
// from and app.MethodValue gets no edge. The graph has to name that site rather
// than look complete — by position, so that a count fed by unrelated dependency
// initializers cannot stand in for it.
func TestUnresolvedCallSitesArePinpointed(t *testing.T) {
	g := build(t, "iface", callgraph.CHA)

	for _, l := range edgeLines(g) {
		if strings.HasPrefix(l, "app.MethodValue ->") {
			t.Fatalf("unexpected edge %q: if this resolves now, the docs need updating", l)
		}
	}

	t.Logf("unresolved:\n  %s", strings.Join(g.Unresolved, "\n  "))
	var methodValue, inInitializer bool
	for _, d := range g.Unresolved {
		// The method value in app.MethodValue: dispatched through Store.Get
		// inside the bound wrapper go/ssa made for it, which has no syntax and
		// so no position — the wrapper's name is the handle.
		// Named by the function that takes the method value, not by the package
		// the interface happens to be declared in.
		if d == "store.Store.Get as a method value in app.MethodValue" {
			methodValue = true
		}
		// The closure in app.Registered does have syntax, so this one is
		// reported with a file and a line.
		if strings.Contains(d, "store.Store.Get at ") && strings.Contains(d, "app.go:") {
			inInitializer = true
		}
	}
	if !methodValue {
		t.Errorf("the method value in app.MethodValue is missing from %v", g.Unresolved)
	}
	if !inInitializer {
		t.Errorf("the closure in app.Registered is missing from %v", g.Unresolved)
	}
}

// TestUnresolvedIgnoresDependencies: the same condition fires constantly inside
// the standard library, where it is noise. Only the packages asked for count.
func TestUnresolvedIgnoresDependencies(t *testing.T) {
	g := build(t, "iface", callgraph.CHA)
	fixture := map[string]bool{"app": true, "store": true, "apply": true, "hidden": true}
	for _, d := range g.Unresolved {
		// Every entry opens with the dispatched method, pkg.Iface.Method.
		i := strings.Index(d, ".")
		if i < 0 || !fixture[d[:i]] {
			t.Errorf("site dispatching an interface from outside the module: %s", d)
		}
		if strings.Contains(d, " at ") && !strings.Contains(d, "testdata/iface") {
			t.Errorf("site in a file outside the module: %s", d)
		}
	}
}

// TestNodeIDsSurviveIdenticalFullNames guards node identity. Every func init()
// in a package has the same types.Func.FullName, so a graph keyed by it merges
// them into one node and loses their positions.
func TestNodeIDsSurviveIdenticalFullNames(t *testing.T) {
	g := build(t, "iface", callgraph.CHA)

	var inits []string
	seen := map[string]int{}
	for f, id := range g.ID {
		seen[id]++
		if f.Name() == "init" && g.Pkg[f] == "example.com/iface/store" {
			inits = append(inits, id)
		}
	}
	sort.Strings(inits)

	want := []string{"example.com/iface/store.init#1", "example.com/iface/store.init#2"}
	if strings.Join(inits, ",") != strings.Join(want, ",") {
		t.Errorf("ids of the two initializers: got %v, want %v", inits, want)
	}

	// Which one is #1 has to be stable too: these ids identify nodes across
	// runs, so if they swap, every diff of the output lies.
	byID := map[string]int{}
	for f, id := range g.ID {
		if f.Name() == "init" && g.Pkg[f] == "example.com/iface/store" {
			byID[id] = g.Pos[f].Lo
		}
	}
	if byID["example.com/iface/store.init#1"] >= byID["example.com/iface/store.init#2"] {
		t.Errorf("#1 should be the initializer declared first, got lines %v", byID)
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("%d functions share the id %q", n, id)
		}
	}
}

// TestBuildIsDeterministic covers both the SSA call graph and the edge set,
// which are map-backed and would otherwise come out in a different order.
func TestBuildIsDeterministic(t *testing.T) {
	for _, mode := range []callgraph.Mode{callgraph.Static, callgraph.CHA} {
		first := strings.Join(edgeLines(build(t, "iface", mode)), "\n")
		second := strings.Join(edgeLines(build(t, "iface", mode)), "\n")
		if first != second {
			t.Errorf("mode %s: two builds disagree:\n%s\n---\n%s", mode, first, second)
		}
	}
}

// TestRTARootsAtInitNotOnlyMain is the registry pattern that cobra,
// database/sql and prometheus all use: plug.P is instantiated in a package
// initializer, and nothing reachable from main names it. Rooting RTA at main
// alone loses the edge.
func TestRTARootsAtInitNotOnlyMain(t *testing.T) {
	g := build(t, "registry", callgraph.RTA)
	t.Log("rta mode:\n  " + strings.Join(edgeLines(g), "\n  "))

	want := "reg.Dispatch -> plug.P.Handle [via reg.Handler.Handle]"
	for _, l := range edgeLines(g) {
		if l == want {
			return
		}
	}
	t.Errorf("missing %q; got %v", want, edgeLines(g))
}

// TestRTANeedsAnEntryPoint: RTA works from the types reachable from main, so it
// cannot analyse a library. CHA can, which is why CHA is the mode that works
// for a library.
func TestRTANeedsAnEntryPoint(t *testing.T) {
	_, err := callgraph.Build("testdata/iface", "./...", callgraph.RTA)
	if err == nil {
		t.Fatal("rta on a module with no main: want an error, got none")
	}
	if !strings.Contains(err.Error(), "cha") {
		t.Errorf("the error should point at the mode that works: %v", err)
	}
	if _, err := callgraph.Build("testdata/iface", "./...", callgraph.CHA); err != nil {
		t.Errorf("cha on the same module: %v", err)
	}
}

// TestIllTypedPackagesAreReportedCausesFirst guards the honesty of the graph:
// go/ssa builds nothing for an ill-typed package, so dispatch inside it is
// invisible while its functions still appear as nodes. Being ill-typed is
// transitive, so the report has to distinguish the one broken package from the
// packages that merely import it.
func TestIllTypedPackagesAreReportedCausesFirst(t *testing.T) {
	g := build(t, "illtyped", callgraph.CHA)
	t.Logf("ill-typed: %+v", g.IllTyped)

	var got []string
	for _, p := range g.IllTyped {
		kind := "importer"
		if p.Cause {
			kind = "cause"
		}
		got = append(got, kind+" "+p.Path)
	}
	// Both causes first and sorted among themselves, then the importers.
	want := []string{
		"cause example.com/illtyped/broken",
		"cause example.com/illtyped/broken2",
		"importer example.com/illtyped/bystander",
		"importer example.com/illtyped/importer",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("report order:\n got %v\nwant %v", got, want)
	}
	if g.IllTyped[0].Err == "" {
		t.Error("a cause should carry the type error that explains it")
	}
}

// TestIllTypedImportersAreOnlyReportedWhenAskedFor: being ill-typed is
// transitive, so everything importing a broken package inherits it. Listing the
// ones nobody asked about buries the package that actually has to be fixed.
func TestIllTypedImportersAreOnlyReportedWhenAskedFor(t *testing.T) {
	g, err := callgraph.Build("testdata/illtyped", "./importer/...", callgraph.CHA)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ill-typed: %+v", g.IllTyped)
	for _, p := range g.IllTyped {
		if p.Path == "example.com/illtyped/bystander" {
			t.Error("bystander only inherited the problem and was not asked for, so it should not be reported")
		}
	}
	// The causes are still reported, wherever they live.
	if len(g.IllTyped) < 3 {
		t.Errorf("want both causes and the importer asked for, got %+v", g.IllTyped)
	}
}

// TestPatternMatchingNothingIsAnError: packages.Load answers a pattern that
// matches nothing with one synthetic package rather than an empty slice, so
// without a check the caller gets an empty graph and a success.
func TestPatternMatchingNothingIsAnError(t *testing.T) {
	g, err := callgraph.Build("testdata/iface", "./nosuchpackage/...", callgraph.CHA)
	if err == nil {
		t.Fatalf("want an error, got a graph with %d functions", len(g.Defined))
	}
}

func TestParseMode(t *testing.T) {
	for _, s := range []string{"static", "cha", "rta"} {
		m, err := callgraph.ParseMode(s)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", s, err)
		}
		if m.String() != s {
			t.Errorf("ParseMode(%q).String() = %q", s, m.String())
		}
	}
	if _, err := callgraph.ParseMode("vta"); err == nil {
		t.Error("ParseMode(\"vta\"): want an error, got none")
	}
}

// BenchmarkBuild measures each mode over this repository, so the cost of
// resolving dispatch is re-measured rather than quoted from a commit message.
// Run with: go test -bench Build -benchtime 3x ./internal/callgraph/
func BenchmarkBuild(b *testing.B) {
	for _, mode := range []callgraph.Mode{callgraph.Static, callgraph.CHA, callgraph.RTA} {
		b.Run(mode.String(), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := callgraph.Build("../..", "./...", mode); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
