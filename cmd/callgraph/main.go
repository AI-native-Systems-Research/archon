// Command callgraph draws a function-altitude call graph of a Go module.
//
// Two views:
//
//	full          every in-module function, clustered by package.
//	delta-scoped  pass --since <ref>; the tool diffs <ref> against the working
//	              tree (tracked files only — a brand-new untracked file is not seen)
//	              tree, marks the functions whose bodies overlap changed lines,
//	              and draws only those plus their callers/callees out to --depth
//	              hops (default 1). Changed functions are highlighted.
//
// Three resolution modes:
//
//	--mode=static  (default) a call is an edge only when the callee is a concrete
//	               in-module function. Calls through an interface are dropped.
//	--mode=cha     also resolves an interface call to every in-module implementer.
//	               Sound without a main, so it works on libraries. Cost depends on
//	               how much the module dispatches dynamically: 1.28x on BLIS, no
//	               extra edges on archon itself.
//	--mode=rta     prunes implementers never instantiated on a reachable path.
//	               Needs a main, and finds almost nothing when dispatch goes
//	               through a framework (a Cobra CLI yields one edge).
//
// Usage:
//
//	callgraph <module-dir> <pkg-pattern> [--mode=static|cha|rta] [--since <ref>] [--depth N]
//	e.g.  callgraph ../inference-sim ./...                 # full graph
//	      callgraph ../inference-sim ./... --mode=cha      # include interface calls
//	      callgraph ../inference-sim ./... --since HEAD~1  # what the last commit touched
//
// Emits Graphviz DOT on stdout; a one-line summary on stderr. Analysis lives in
// internal/callgraph; this command is presentation only.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/callgraph"
)

type iv struct{ lo, hi int }

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: callgraph <module-dir> <pkg-pattern> [--mode=static|cha|rta] [--since <ref>] [--depth N]")
		os.Exit(2)
	}
	dir, pattern := os.Args[1], os.Args[2]
	sinceRef := ""
	modeStr := "static"
	depth := 1
	// Anything unrecognised is an error. Ignoring it silently means "-mode=cha" or a
	// value-less "--mode" analyses in static mode and reports a graph as if it were
	// the one asked for, which is the failure this whole command is about.
	fail := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
		os.Exit(2)
	}
	needValue := func(i int, flag string) string {
		if i+1 >= len(os.Args) {
			fail("%s needs a value", flag)
		}
		return os.Args[i+1]
	}
	atoi := func(s, flag string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			fail("%s: %q is not a number", flag, s)
		}
		return n
	}
	for i := 3; i < len(os.Args); i++ {
		a := os.Args[i]
		switch {
		case a == "--since":
			sinceRef = needValue(i, a)
			i++
		case strings.HasPrefix(a, "--since="):
			sinceRef = strings.TrimPrefix(a, "--since=")
		case a == "--depth":
			depth = atoi(needValue(i, a), a)
			i++
		case strings.HasPrefix(a, "--depth="):
			depth = atoi(strings.TrimPrefix(a, "--depth="), "--depth")
		case a == "--mode":
			modeStr = needValue(i, a)
			i++
		case strings.HasPrefix(a, "--mode="):
			modeStr = strings.TrimPrefix(a, "--mode=")
		default:
			fail("unknown argument %q (want --mode, --since or --depth)", a)
		}
	}

	mode, err := callgraph.ParseMode(modeStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	g, err := callgraph.Build(dir, pattern, mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(g.IllTyped) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d matched package(s) did not type-check.\n", len(g.IllTyped))
		if mode != callgraph.Static {
			fmt.Fprintf(os.Stderr, "  go/ssa builds nothing for those, so mode=%s sees no call sites "+
				"and no implementers in them: the graph below is INCOMPLETE.\n", mode)
		} else {
			fmt.Fprintln(os.Stderr, "  calls to their undefined symbols are missing from the graph below.")
		}
		// Own errors first: packages.IllTyped is transitive, so most entries are
		// merely importers of a broken package. Truncating without ordering can hide
		// every package that actually has an error.
		for i, e := range g.IllTyped {
			if i == 8 {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(g.IllTyped)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
	}

	byID := make(map[string]callgraph.Func, len(g.Funcs))
	for _, f := range g.Funcs {
		byID[f.ID] = f
	}

	// delta scope: which functions did <ref>..worktree touch?
	changed := map[string]bool{}
	if sinceRef != "" {
		ranges, ok := gitChangedRanges(dir, sinceRef)
		if !ok {
			fmt.Fprintf(os.Stderr, "cannot compute the delta for --since %s; refusing to emit "+
				"an empty graph as if nothing changed\n", sinceRef)
			os.Exit(1)
		}
		resolved := map[string]string{}
		for _, f := range g.Funcs {
			r, seen := resolved[f.File]
			if !seen {
				r = resolve(f.File)
				resolved[f.File] = r
			}
			ivs, ok := ranges[r]
			if !ok {
				continue
			}
			for _, r := range ivs {
				if f.Lo <= r.hi && r.lo <= f.Hi {
					changed[f.ID] = true
				}
			}
		}
	}

	visible := map[string]bool{}
	if sinceRef != "" {
		adjOut := map[string][]string{}
		adjIn := map[string][]string{}
		for _, e := range g.Edges {
			adjOut[e.From] = append(adjOut[e.From], e.To)
			adjIn[e.To] = append(adjIn[e.To], e.From)
		}
		frontier := map[string]bool{}
		for f := range changed {
			visible[f] = true
			frontier[f] = true
		}
		for d := 0; d < depth; d++ {
			next := map[string]bool{}
			for f := range frontier {
				// Two loops rather than append(adjOut[f], adjIn[f]...): that writes
				// into adjOut[f]'s spare capacity, and shadowing g here would hide
				// the graph.
				for _, nb := range adjOut[f] {
					if !visible[nb] {
						visible[nb] = true
						next[nb] = true
					}
				}
				for _, nb := range adjIn[f] {
					if !visible[nb] {
						visible[nb] = true
						next[nb] = true
					}
				}
			}
			frontier = next
		}
	} else {
		for _, f := range g.Funcs {
			visible[f.ID] = true
		}
	}

	nEdges := 0
	for _, e := range g.Edges {
		if visible[e.From] && visible[e.To] {
			nEdges++
		}
	}
	scope := "full"
	if sinceRef != "" {
		scope = fmt.Sprintf("delta since %s (%d changed fn, depth %d)", sinceRef, len(changed), depth)
	}
	fmt.Fprintf(os.Stderr, "callgraph: %d functions in module, %d visible, %d edges [%s, mode=%s]\n",
		len(g.Funcs), len(visible), nEdges, scope, mode)

	emitDOT(g, byID, visible, changed, sinceRef != "", sinceRef, depth)
}

// gitChangedRanges diffs ref against the working tree and returns, per ABSOLUTE
// file path, the new-side line intervals that changed.
//
// Keys are absolute on purpose. git reports paths relative to the repository root,
// and matching those against a declaration's absolute path by suffix is wrong:
// "other/p/x.go" ends with "p/x.go", so a hunk in one would mark functions in the
// other as changed. Joining to the repo root makes the comparison exact.
func gitChangedRanges(dir, ref string) (map[string][]iv, bool) {
	out := map[string][]iv{}
	root := gitTopLevel(dir)
	if root == "" {
		fmt.Fprintln(os.Stderr, "not a git repository:", dir)
		return out, false
	}
	// Every flag here neutralises a config setting that would otherwise change the
	// path format and silently yield a zero-change delta:
	//   diff.relative       -> paths relative to dir, not the repo root (--no-relative)
	//   diff.mnemonicprefix  -> "w/" instead of "b/"      (--dst-prefix)
	//   diff.noprefix        -> no prefix at all          (--dst-prefix)
	//   core.quotePath       -> non-ASCII escaped as \303\251 and the path quoted
	cmd := exec.Command("git", "-C", dir, "-c", "core.quotePath=false", "diff",
		"--unified=0", "--no-relative", "--src-prefix=a/", "--dst-prefix=b/", ref, "--", "*.go")
	b, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			// git's own diagnosis ("unknown revision") is the useful part.
			fmt.Fprintf(os.Stderr, "git diff failed: %v: %s\n", err, strings.TrimSpace(string(ee.Stderr)))
		} else {
			fmt.Fprintln(os.Stderr, "git diff failed:", err)
		}
		return out, false
	}
	hunk := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	cur := ""
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "+++ ") {
			p := strings.TrimPrefix(line, "+++ ")
			// git appends a literal TAB when the path contains a space.
			p = strings.TrimSuffix(p, "\t")
			p = strings.TrimPrefix(p, "b/")
			cur = p
			continue
		}
		if m := hunk.FindStringSubmatch(line); m != nil {
			start, _ := strconv.Atoi(m[1])
			cnt := 1
			if m[2] != "" {
				cnt, _ = strconv.Atoi(m[2])
			}
			if cnt == 0 {
				cnt = 1
			}
			if cur == "" {
				continue
			}
			key := resolve(filepath.Join(root, cur))
			out[key] = append(out[key], iv{start, start + cnt - 1})
		}
	}
	return out, true
}

// resolve normalizes a path for comparison. Symlinks are evaluated because a
// module can sit under a symlinked root — /var vs /private/var on macOS — and git
// and go/token then report the same file by different names.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// gitTopLevel returns the absolute root of the repository containing dir, which is
// what git reports diff paths relative to. Empty when dir is not in a repository.
func gitTopLevel(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return resolve(strings.TrimSpace(string(out)))
}

func emitDOT(
	g *callgraph.Graph,
	byID map[string]callgraph.Func,
	visible, changed map[string]bool,
	delta bool, sinceRef string, depth int,
) {
	byPkg := map[string][]callgraph.Func{}
	for id := range visible {
		f, ok := byID[id]
		if !ok {
			continue
		}
		byPkg[f.Pkg] = append(byPkg[f.Pkg], f)
	}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	fmt.Println("digraph callgraph {")
	fmt.Println("  rankdir=LR;")
	fmt.Println(`  labelloc="t"; fontname="Helvetica-Bold"; fontsize=18;`)
	if delta {
		fmt.Printf("  label=\"Function call graph (delta-scoped: since %s, depth %d)   arrow: A calls B\";\n", sinceRef, depth)
	} else {
		fmt.Printf("  label=\"Function call graph (full, mode=%s)   arrow: A calls B; boxes = packages\";\n", g.Mode)
	}
	fmt.Println(`  node [shape=box, style="rounded,filled", fontname="Helvetica", fontsize=10, fillcolor="#eef3fb", color="#4a6fa5"];`)
	fmt.Println(`  edge [color="#666666", arrowsize=0.7];`)

	for ci, p := range pkgs {
		nodes := byPkg[p]
		// ID, not Label, breaks the tie: two func init() declarations share the
		// label "p.init", and leaving them tied means map iteration order decides.
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].Label != nodes[j].Label {
				return nodes[i].Label < nodes[j].Label
			}
			return nodes[i].ID < nodes[j].ID
		})
		short := p
		if idx := strings.LastIndex(p, "/"); idx >= 0 {
			short = p[idx+1:]
		}
		fmt.Printf("  subgraph cluster_p%d {\n", ci)
		fmt.Printf("    label=%q; labelloc=\"t\"; fontname=\"Helvetica-Bold\"; fontsize=12;\n", short)
		fmt.Println(`    style="rounded,filled"; color="#b0b0c0"; fillcolor="#fafafd"; margin=10;`)
		for _, f := range nodes {
			fill, pen := "#eef3fb", "#4a6fa5"
			if changed[f.ID] {
				fill, pen = "#e6f4ea", "#1a7f37" // touched by the change
			}
			fmt.Printf("    %q [label=%q, fillcolor=%q, color=%q];\n", f.ID, f.Label, fill, pen)
		}
		fmt.Println("  }")
	}

	// g.Edges is sorted, so this loop is deterministic. Iterating the edge map
	// directly (as this did before) made two runs of identical input differ.
	for _, e := range g.Edges {
		if !visible[e.From] || !visible[e.To] {
			continue
		}
		var attrs []string
		if changed[e.From] || changed[e.To] {
			attrs = append(attrs, `color="#1a7f37"`, "penwidth=1.4")
		}
		if e.Via != "" {
			// Dashed marks a resolved interface call, labelled with the method that
			// dispatched it — without that a reader cannot tell it from a direct call.
			attrs = append(attrs, "style=dashed", fmt.Sprintf("label=%q", e.Via))
		}
		suffix := ""
		if len(attrs) > 0 {
			suffix = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Printf("  %q -> %q%s;\n", e.From, e.To, suffix)
	}

	fmt.Println(`  subgraph cluster_legend {`)
	fmt.Println(`    label="Legend"; labelloc="t"; fontname="Helvetica-Bold"; fontsize=12;`)
	fmt.Println(`    style="rounded,filled"; color="#cccccc"; fillcolor="#fbfbfb"; margin=10;`)
	fmt.Println(`    Lp [label="pkg.Recv.Method", fillcolor="#eef3fb", color="#4a6fa5"];`)
	if delta {
		fmt.Println(`    Lc [label="changed by this delta", fillcolor="#e6f4ea", color="#1a7f37"];`)
		fmt.Println(`    Lp -> Lc [label="  calls", color="#666666"];`)
	} else {
		fmt.Println(`    Lq [label="another function", fillcolor="#eef3fb", color="#4a6fa5"];`)
		fmt.Println(`    Lp -> Lq [label="  calls", color="#666666"];`)
	}
	if g.Mode != callgraph.Static {
		fmt.Println(`    Li [label="via interface", fillcolor="#eef3fb", color="#4a6fa5"];`)
		fmt.Println(`    Lp -> Li [label="  Method", color="#666666", style=dashed];`)
	}
	fmt.Println(`  }`)
	fmt.Println("}")
}
