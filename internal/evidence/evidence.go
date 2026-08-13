// Package evidence runs the contract/property tests that guard a graph's
// interfaces and reports their CI outcome (pass/fail). It is the "discharge the
// evidence" step: static contract coverage (which implementers a bound test
// references) says a test *exists* and *touches* an implementer; running it here
// says whether that guarantee currently *holds*.
//
// A guarding test is any candidate invariant bound to at least one contract
// (Invariant.Guards non-empty). Tests are executed per package with
// `go test -json -run '^(Name1|Name2|...)$' <import-path>`, so only the bound
// tests run, and the top-level pass/fail is parsed from the JSON event stream.
package evidence

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// TestResult is the CI outcome of one guarding (contract) test.
type TestResult struct {
	Name    string // test function name
	Package string // import path it was run under
	Ran     bool   // whether we observed a pass/fail event for it
	Passed  bool   // valid only when Ran
}

// perPackageTimeout bounds each `go test` invocation.
const perPackageTimeout = 180 * time.Second

// boundTestsByPkg groups the names of contract-bound guarding tests by the
// package that declares them.
func boundTestsByPkg(g *graph.Graph) map[string][]string {
	out := map[string][]string{}
	for _, p := range g.Packages {
		seen := map[string]bool{}
		for _, inv := range p.Invariants {
			if len(inv.Guards) == 0 || seen[inv.Name] {
				continue
			}
			seen[inv.Name] = true
			out[p.Path] = append(out[p.Path], inv.Name)
		}
	}
	return out
}

// Run executes the contract-bound guarding tests found in g, within the module
// rooted at dir, and returns their outcomes keyed by test name. Tests that could
// not be observed to pass or fail (build error, no such test, timeout) come back
// with Ran=false. Errors from `go test` (a failing test makes it exit non-zero)
// are expected and handled by parsing the JSON stream regardless of exit code.
func Run(g *graph.Graph, dir string) map[string]TestResult {
	byPkg := boundTestsByPkg(g)
	results := map[string]TestResult{}
	for _, names := range byPkg {
		for _, n := range names {
			if _, ok := results[n]; !ok {
				results[n] = TestResult{Name: n}
			}
		}
	}
	for pkg, names := range byPkg {
		regex := "^(" + strings.Join(names, "|") + ")$"
		ctx, cancel := context.WithTimeout(context.Background(), perPackageTimeout)
		cmd := exec.CommandContext(ctx, "go", "test", "-json", "-run", regex, pkg)
		cmd.Dir = dir
		out, _ := cmd.Output() // non-zero exit on test failure is normal; parse anyway
		cancel()

		sc := bufio.NewScanner(strings.NewReader(string(out)))
		sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
		for sc.Scan() {
			var e struct{ Action, Test string }
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.Test == "" {
				continue
			}
			if e.Action != "pass" && e.Action != "fail" {
				continue
			}
			// Only record top-level bound tests (subtests like "T/case" and
			// unrelated tests are ignored because they aren't pre-seeded keys).
			if _, want := results[e.Test]; want {
				results[e.Test] = TestResult{Name: e.Test, Package: pkg, Ran: true, Passed: e.Action == "pass"}
			}
		}
	}
	return results
}
