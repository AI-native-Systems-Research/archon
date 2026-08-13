// Command archon-go extracts package-altitude architecture graphs from Go
// repositories and diffs them into architectural deltas.
//
// Usage:
//
//	archon-go extract <dir> [commit]        # graph of a working tree or a commit
//	archon-go delta <graphA.json> <graphB.json>
//	archon-go delta <repo> <commitA> <commitB>
//
// The --json flag on `delta` prints the machine-readable delta instead of the
// human report.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/evidence"
	"github.com/AI-native-Systems-Research/archon/internal/extract"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
	"github.com/AI-native-Systems-Research/archon/internal/health"
	"github.com/AI-native-Systems-Research/archon/internal/impact"
	"github.com/AI-native-Systems-Research/archon/internal/reflexion"
	"github.com/AI-native-Systems-Research/archon/internal/render"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "extract":
		cmdExtract(os.Args[2:])
	case "delta":
		cmdDelta(os.Args[2:])
	case "render":
		cmdRender(os.Args[2:])
	case "contract":
		cmdContract(os.Args[2:])
	case "evidence":
		cmdEvidence(os.Args[2:])
	case "impact":
		cmdImpact(os.Args[2:])
	case "health":
		cmdHealth(os.Args[2:])
	case "reflexion":
		cmdReflexion(os.Args[2:])
	default:
		usage()
	}
}

// cmdReflexion compares a declared intended layering against the recovered graph
// and reports divergences (upward, layer-violating dependencies). The violation
// count is the reflexion distance; comparing it across commits answers "did this
// PR move toward the target architecture?".
func cmdReflexion(args []string) {
	jsonOut := false
	var pos []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "usage: archon-go reflexion <repo|graph.json> <layers.json> [commit] [--json]")
		os.Exit(2)
	}
	var g *graph.Graph
	if isJSONFile(pos[0]) {
		g = loadGraph(pos[0])
	} else {
		commit := ""
		if len(pos) >= 3 {
			commit = pos[2]
		}
		g = extractAt(pos[0], commit)
	}
	var spec reflexion.Spec
	data, err := os.ReadFile(pos[1])
	if err != nil {
		fatal("read layers %s: %v", pos[1], err)
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		fatal("parse layers %s: %v", pos[1], err)
	}
	rep := reflexion.Analyze(g, spec)
	if jsonOut {
		printJSON(rep)
		return
	}
	total := rep.DownEdges + rep.UpEdges
	fmt.Printf("REFLEXION MODEL — declared layering vs actual code\n")
	fmt.Printf("  layers (top→bottom): %s\n", strings.Join(spec.Layers, " → "))
	fmt.Printf("  convergent (downward) deps: %d\n", rep.DownEdges)
	fmt.Printf("  DIVERGENT (upward, layering violations): %d", rep.UpEdges)
	if total > 0 {
		fmt.Printf("  (%d%% of cross-layer deps)", rep.UpEdges*100/total)
	}
	fmt.Println()
	for _, v := range rep.Violations {
		fmt.Printf("    ! %s depends on higher layer %s  ×%d edge(s)\n", v.From, v.To, v.Count)
	}
	if len(rep.Unmapped) > 0 {
		fmt.Printf("  unmapped components (no layer assigned): %s\n", strings.Join(rep.Unmapped, ", "))
	}
	if rep.UpEdges == 0 {
		fmt.Println("  → code conforms to the declared layering.")
	}
}

// cmdHealth reports architecture-health metrics: coupling (fan-in/out,
// instability), dependency cycles, blast-radius hotspots, and god-module
// candidates — the "understand the current design" view for a refactor.
func cmdHealth(args []string) {
	jsonOut := false
	var pos []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) < 1 {
		usage()
	}
	var g *graph.Graph
	if isJSONFile(pos[0]) {
		g = loadGraph(pos[0])
	} else {
		commit := ""
		if len(pos) >= 2 {
			commit = pos[1]
		}
		g = extractAt(pos[0], commit)
	}
	rep := health.Analyze(g)
	if jsonOut {
		printJSON(rep)
		return
	}
	fmt.Printf("ARCHITECTURE HEALTH\n")
	if len(rep.Cycles) == 0 {
		fmt.Println("  cycles: none — internal dependency graph is an acyclic DAG (healthy)")
	} else {
		fmt.Printf("  cycles: %d dependency cycle(s) found:\n", len(rep.Cycles))
		for _, c := range rep.Cycles {
			refs := make([]string, len(c))
			for i, x := range c {
				refs[i] = short(x)
			}
			fmt.Printf("    ! %s\n", strings.Join(refs, " <-> "))
		}
	}
	if len(rep.GodModules) > 0 {
		refs := make([]string, len(rep.GodModules))
		for i, x := range rep.GodModules {
			refs[i] = short(x)
		}
		fmt.Printf("  god-modules (high fan-in + large surface): %s\n", strings.Join(refs, ", "))
	}
	fmt.Printf("  coupling (top by blast radius):\n")
	fmt.Printf("    %-28s %6s %7s %6s %8s %6s\n", "package", "fanIn", "fanOut", "surf", "instab", "blast")
	n := len(rep.Packages)
	if n > 12 {
		n = 12
	}
	for _, m := range rep.Packages[:n] {
		flag := ""
		if m.God {
			flag = "  <god>"
		}
		fmt.Printf("    %-28s %6d %7d %6d %8.2f %6d%s\n", short(m.Path), m.FanIn, m.FanOut, m.Surface, m.Instability, m.BlastRadius, flag)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `archon-go — package-altitude architecture graphs and deltas

  archon-go extract <dir> [commit]              extract a graph (JSON to stdout)
  archon-go delta <graphA.json> <graphB.json>   diff two saved graphs
  archon-go delta <repo> <commitA> <commitB>    extract two commits and diff
      --json                                    print machine-readable delta
      --summary                                 concise triage verdict (fast-track
                                                vs review) + only the key items
      --allow <file>                            check deps against an allow-list
  archon-go impact <repo|graph.json> <pkg> [commit]  blast radius: what depends
                                                on <pkg> (direct + transitive)
  archon-go contract <repo|graph.json> [commit] snapshot the allow-list baseline
                                                (per-box permitted internal deps)
  archon-go evidence <repo> [commit]            run the contract tests bound to
                                                each interface; report coverage +
                                                PASS/FAIL per contract  (--json)
  archon-go health <repo|graph.json> [commit]   coupling, cycles, god-modules,
                                                blast-radius hotspots  (--json)
  archon-go reflexion <repo|graph.json> <layers.json> [commit]  declared layering
                                                vs actual code; report violations (--json)
  archon-go render <repo|graph.json> [commit]   draw the architecture
  archon-go render <repo> <commitA> <commitB>   draw the delta (added=green,
  archon-go render <graphA.json> <graphB.json>    removed=red, unchanged=grey)
      --format=dot|mermaid                      output format (default dot)
      --external                                include external packages
      --full                                    draw the whole graph, not just
                                                the changed neighborhood
`)
	os.Exit(2)
}

func cmdRender(args []string) {
	format := "dot"
	external := false
	full := false
	var pos []string
	for _, a := range args {
		switch {
		case a == "--external":
			external = true
		case a == "--full":
			full = true
		case strings.HasPrefix(a, "--format="):
			format = strings.TrimPrefix(a, "--format=")
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) < 1 {
		usage()
	}

	// Diff render: two saved graphs, or a repo with two commits. Colors added
	// (green), removed (red), and unchanged (grey) so the change is visible.
	var a, b *graph.Graph
	switch {
	case len(pos) == 2 && isJSONFile(pos[0]) && isJSONFile(pos[1]):
		a, b = loadGraph(pos[0]), loadGraph(pos[1])
	case len(pos) == 3:
		a, b = extractAt(pos[0], pos[1]), extractAt(pos[0], pos[2])
	}
	if a != nil {
		switch format {
		case "dot":
			fmt.Print(render.DOTDiff(a, b, external, !full))
		case "mermaid":
			fmt.Print(render.MermaidDiff(a, b, external, !full))
		default:
			fatal("unknown --format %q (want dot or mermaid)", format)
		}
		return
	}

	// Single-graph render.
	var g *graph.Graph
	if isJSONFile(pos[0]) {
		g = loadGraph(pos[0])
	} else {
		commit := ""
		if len(pos) >= 2 {
			commit = pos[1]
		}
		g = extractAt(pos[0], commit)
	}

	switch format {
	case "dot":
		fmt.Print(render.DOT(g, external))
	case "mermaid":
		fmt.Print(render.Mermaid(g, external))
	default:
		fatal("unknown --format %q (want dot or mermaid)", format)
	}
}

func cmdExtract(args []string) {
	if len(args) < 1 {
		usage()
	}
	dir := args[0]
	commit := ""
	if len(args) >= 2 {
		commit = args[1]
	}
	g := extractAt(dir, commit)
	printJSON(g)
}

func cmdDelta(args []string) {
	jsonOut := false
	summaryOut := false
	allowPath := ""
	var pos []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			jsonOut = true
		case a == "--summary":
			summaryOut = true
		case a == "--allow":
			if i+1 >= len(args) {
				usage()
			}
			i++
			allowPath = args[i]
		case strings.HasPrefix(a, "--allow="):
			allowPath = strings.TrimPrefix(a, "--allow=")
		default:
			pos = append(pos, a)
		}
	}

	var a, b *graph.Graph
	switch {
	case len(pos) == 2 && isJSONFile(pos[0]) && isJSONFile(pos[1]):
		a = loadGraph(pos[0])
		b = loadGraph(pos[1])
	case len(pos) == 3:
		a = extractAt(pos[0], pos[1])
		b = extractAt(pos[0], pos[2])
	default:
		usage()
	}

	d := delta.Compute(a, b)
	if allowPath != "" {
		d.CheckContract(b, loadAllow(allowPath))
	}
	if jsonOut {
		printJSON(d)
		return
	}
	if summaryOut {
		fmt.Print(d.Summary())
		return
	}
	fmt.Print(d.Render())
}

// cmdImpact reports the blast radius of a package: which internal packages
// depend on it (directly and transitively). Answers "what breaks if we change
// X?" — useful for review and for planning a refactor.
func cmdImpact(args []string) {
	jsonOut := false
	var pos []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) < 2 {
		usage()
	}
	src, target := pos[0], pos[1]
	commit := ""
	if len(pos) >= 3 {
		commit = pos[2]
	}
	var g *graph.Graph
	if isJSONFile(src) {
		g = loadGraph(src)
	} else {
		g = extractAt(src, commit)
	}

	matches := impact.Resolve(g, target)
	switch {
	case len(matches) == 0:
		fatal("no internal package matches %q", target)
	case len(matches) > 1:
		fmt.Fprintf(os.Stderr, "ambiguous target %q — matches:\n", target)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		os.Exit(1)
	}
	resolved := matches[0]
	direct, transitive := impact.Dependents(g, resolved)

	if jsonOut {
		printJSON(map[string]any{
			"target":           resolved,
			"directDependents": direct,
			"totalTransitive":  len(transitive),
			"transitive":       transitive,
			"directCount":      len(direct),
		})
		return
	}

	fmt.Printf("BLAST RADIUS of %s\n", resolved)
	fmt.Printf("  %d direct dependent(s), %d total (transitive)\n", len(direct), len(transitive))
	if len(transitive) == 0 {
		fmt.Println("  → nothing depends on it; safe to change in isolation.")
		return
	}
	directSet := map[string]bool{}
	for _, d := range direct {
		directSet[d] = true
		fmt.Printf("  direct:   %s\n", short(d))
	}
	for _, t := range transitive {
		if !directSet[t] {
			fmt.Printf("  indirect: %s\n", short(t))
		}
	}
}

// cmdContract snapshots the structural-contract baseline: for each internal
// box, the internal packages it currently depends on (import/call). This is the
// brownfield "snapshot" step — a maintainer commits it, then tightens it, and
// `delta --allow` flags dependencies that step outside it.
func cmdContract(args []string) {
	if len(args) < 1 {
		usage()
	}
	var g *graph.Graph
	if isJSONFile(args[0]) {
		g = loadGraph(args[0])
	} else {
		commit := ""
		if len(args) >= 2 {
			commit = args[1]
		}
		g = extractAt(args[0], commit)
	}

	internal := g.InternalPaths()
	allow := map[string][]string{}
	for _, e := range g.Edges {
		if e.Kind != "import" && e.Kind != "call" {
			continue
		}
		if !internal[e.From] || !internal[e.To] || e.From == e.To {
			continue
		}
		if !contains(allow[e.From], e.To) {
			allow[e.From] = append(allow[e.From], e.To)
		}
	}
	for k := range allow {
		sort.Strings(allow[k])
	}
	// Ensure every internal box appears (even with an empty allow-list).
	for p := range internal {
		if _, ok := allow[p]; !ok {
			allow[p] = []string{}
		}
	}
	printJSON(allow)
}

// cmdEvidence extracts the graph, then for each interface (contract) reports its
// implementers (covered ✓ / uncovered = evidence gap) AND runs the contract
// tests bound to it, reporting PASS/FAIL. Unlike other commands it needs the
// source on disk to run `go test`, so it manages its own worktree lifetime.
func cmdEvidence(args []string) {
	jsonOut := false
	var pos []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) < 1 {
		usage()
	}
	dir := pos[0]
	commit := ""
	if len(pos) >= 2 {
		commit = pos[1]
	}
	work := dir
	if commit != "" {
		tmp, cleanup := checkoutWorktree(dir, commit)
		defer cleanup()
		work = tmp
	}
	res, err := extract.Extract(work)
	if err != nil {
		fatal("extract %s@%s: %v", dir, commit, err)
	}
	g := res.Graph

	cov := delta.Coverage(g)
	results := evidence.Run(g, work)
	if jsonOut {
		type contractJSON struct {
			Interface     string   `json:"interface"`
			Covered       []string `json:"covered,omitempty"`
			Uncovered     []string `json:"uncovered,omitempty"`
			ContractTests []string `json:"contractTests,omitempty"`
			GapKind       string   `json:"gapKind,omitempty"` // none | no-test | proven | unconfirmed
		}
		out := []contractJSON{}
		for _, c := range cov {
			gap := "none"
			if len(c.Uncovered) > 0 {
				switch {
				case len(c.ContractTests) == 0:
					gap = "no-test"
				case c.TestsNameConcretes:
					gap = "proven"
				default:
					gap = "unconfirmed"
				}
			}
			out = append(out, contractJSON{c.Interface, c.Covered, c.Uncovered, c.ContractTests, gap})
		}
		printJSON(out)
		return
	}
	renderEvidence(dir, commit, cov, results)
}

func renderEvidence(dir, commit string, cov []delta.ContractChange, results map[string]evidence.TestResult) {
	where := dir
	if commit != "" {
		where += "@" + commit
	}
	fmt.Printf("CONTRACT EVIDENCE — %s\n", where)
	if len(cov) == 0 {
		fmt.Println("  (no interface contracts with implementers found)")
		return
	}
	for _, c := range cov {
		fmt.Printf("\nContract: %s\n", short(c.Interface))
		for _, im := range c.Covered {
			fmt.Printf("  implementer %s — covered by a contract test\n", short(im))
		}
		for _, im := range c.Uncovered {
			switch {
			case len(c.ContractTests) == 0:
				fmt.Printf("  implementer %s — NOT covered (evidence gap: no contract test binds this interface)\n", short(im))
			case c.TestsNameConcretes:
				fmt.Printf("  implementer %s — NOT covered (evidence gap: the contract test names other implementers, not this one)\n", short(im))
			default:
				fmt.Printf("  implementer %s — unconfirmed (a contract test exists but drives implementers via a factory, so this one cannot be attributed)\n", short(im))
			}
		}
		if len(c.ContractTests) == 0 {
			fmt.Printf("  evidence: no contract test binds this interface\n")
			continue
		}
		for _, t := range c.ContractTests {
			status := "not run"
			if r, ok := results[t]; ok && r.Ran {
				if r.Passed {
					status = "PASS"
				} else {
					status = "FAIL"
				}
			}
			fmt.Printf("  evidence: %s — CI: %s\n", t, status)
		}
	}
}

// short trims a "pkgpath.Name" identifier to its final segment for readable
// reports.
func short(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func loadAllow(path string) map[string][]string {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read allow-list %s: %v", path, err)
	}
	var m map[string][]string
	if err := json.Unmarshal(data, &m); err != nil {
		fatal("parse allow-list %s: %v", path, err)
	}
	return m
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// extractAt extracts the graph of dir, optionally at a specific commit using an
// ephemeral git worktree so the working tree is never disturbed.
func extractAt(dir, commit string) *graph.Graph {
	work := dir
	if commit != "" {
		tmp, cleanup := checkoutWorktree(dir, commit)
		defer cleanup()
		work = tmp
	}
	res, err := extract.Extract(work)
	if err != nil {
		fatal("extract %s@%s: %v", dir, commit, err)
	}
	res.Graph.Commit = commit
	if res.NumErrors > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d package error(s) during extraction of %s@%s (graph may be partial)\n", res.NumErrors, dir, commit)
	}
	return res.Graph
}

func checkoutWorktree(repo, commit string) (string, func()) {
	tmp, err := os.MkdirTemp("", "archon-wt-")
	if err != nil {
		fatal("mktemp: %v", err)
	}
	run(repo, "git", "worktree", "add", "--detach", "--quiet", tmp, commit)
	return tmp, func() {
		run(repo, "git", "worktree", "remove", "--force", tmp)
		os.RemoveAll(tmp)
	}
}

func run(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fatal("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func isJSONFile(p string) bool {
	if strings.ToLower(filepath.Ext(p)) != ".json" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func loadGraph(path string) *graph.Graph {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read %s: %v", path, err)
	}
	var g graph.Graph
	if err := json.Unmarshal(data, &g); err != nil {
		fatal("parse %s: %v", path, err)
	}
	return &g
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatal("encode json: %v", err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "archon-go: "+format+"\n", args...)
	os.Exit(1)
}
