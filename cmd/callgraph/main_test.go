package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// This command had no test, which is how the ID-collision defect reached review:
// its symptom lived here, in the --since position match, not in internal/callgraph.
// The BFS, the git hunk parsing and sameFile are all only exercised from here.

// A hunk in one file must not mark a same-named file in another directory as
// changed. Suffix matching cannot express this — "other/p/x.go" really does end
// with "/p/x.go" — so hunks are keyed by absolute path against the repo root.
func TestChangedFileMatchingIsNotBySuffix(t *testing.T) {
	dir := gitRepo(t,
		map[string]string{
			"go.mod":       "module example.com/p\n\ngo 1.26\n",
			"p/x.go":       "package p\n\nfunc PX() {}\n",
			"other/p/x.go": "package p\n\nfunc OtherPX() {}\n",
		},
		map[string]string{
			// Only the SHALLOW file changes. Direction matters: the old code asked
			// whether a declaration's absolute path ends with the hunk's key, so a
			// hunk keyed "p/x.go" falsely matched ".../other/p/x.go". Changing the
			// nested file instead would pass even against the buggy version.
			"p/x.go": "package p\n\nfunc PX() { _ = 1 }\n",
		})

	stdout, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")

	if !strings.Contains(stderr, "1 changed fn") {
		t.Errorf("expected exactly 1 changed function, got: %s", strings.TrimSpace(stderr))
	}
	if !strings.Contains(stdout, `label="p.PX"`) {
		t.Errorf("the function that actually changed is missing:\n%s", stdout)
	}
	if strings.Contains(stdout, `label="p.OtherPX"`) {
		t.Errorf("other/p/x.go was marked changed because its path ends with the "+
			"hunk key \"p/x.go\":\n%s", stdout)
	}
}

// gitRepo builds a temp module, commits it, then applies after{} on top so that
// --since HEAD sees exactly those edits as uncommitted.
func gitRepo(t *testing.T, files, after map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	write := func(m map[string]string) {
		for name, src := range m {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(files)
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		// Ignore the developer's global config: commit.gpgsign, core.hooksPath and
		// init.templateDir would each fail the commit, and diff.relative would
		// change the paths the tool has to parse.
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(after)
	return dir
}

// run invokes the built command and returns stdout, stderr.
func run(t *testing.T, bin, dir string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{dir, "./..."}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v\nstderr: %s", bin, args, err, stderr.String())
	}
	return stdout.String(), stderr.String()
}

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// buildCmd builds the command once for the whole suite; five separate builds
// dominated the runtime.
func buildCmd(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "callgraph-bin")
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(dir, "callgraph")
		if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build: %v\n%s", err, out)
			return
		}
		builtBin = bin
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}

// The regression that prompted this test file. Two func init() in one package
// collapsed to a single node, so the surviving entry carried the other file's
// position, no diff hunk matched, and --since drew nothing at all.
func TestSinceFindsChangedInitAmongSeveral(t *testing.T) {
	dir := gitRepo(t,
		map[string]string{
			"go.mod": "module example.com/p\n\ngo 1.26\n",
			"p/a.go": "package p\n\nfunc init() { A() }\n\nfunc A() {}\n",
			"p/b.go": "package p\n\nfunc init() { B() }\n\nfunc B() {}\n",
		},
		map[string]string{
			"p/a.go": "package p\n\nfunc init() { A(); A() }\n\nfunc A() {}\n",
		})

	stdout, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")

	if !strings.Contains(stderr, "1 changed fn") {
		t.Errorf("expected exactly 1 changed function, got: %s", strings.TrimSpace(stderr))
	}
	if strings.Contains(stderr, "0 visible") {
		t.Errorf("delta view is empty; the init positions were collapsed: %s", strings.TrimSpace(stderr))
	}
	// a.go's init is what changed, and A is its callee at depth 1. B must not appear.
	if !strings.Contains(stdout, "p.init") || !strings.Contains(stdout, `label="p.A"`) {
		t.Errorf("changed init or its callee missing from the DOT:\n%s", stdout)
	}
	if strings.Contains(stdout, `label="p.B"`) {
		t.Errorf("unrelated function p.B was drawn:\n%s", stdout)
	}
}

func TestFullOutputIsWellFormedAndDeterministic(t *testing.T) {
	files := map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.26\n",
		"p/p.go": "package p\n\nfunc A() { B() }\n\nfunc B() {}\n",
	}
	dir := gitRepo(t, files, nil)
	bin := buildCmd(t)

	first, _ := run(t, bin, dir)
	second, _ := run(t, bin, dir)

	if first != second {
		t.Error("two runs of identical input differ; the output is not deterministic")
	}
	if !strings.HasPrefix(first, "digraph callgraph {") || !strings.HasSuffix(strings.TrimSpace(first), "}") {
		t.Errorf("output is not a well-formed DOT graph:\n%s", first)
	}
	if strings.Count(first, "subgraph cluster_") < 2 { // one package + the legend
		t.Errorf("expected a package cluster and a legend:\n%s", first)
	}
}

func TestUnknownModeIsRejected(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.26\n",
		"p/p.go": "package p\n\nfunc A() {}\n",
	}, nil)

	cmd := exec.Command(buildCmd(t), dir, "./...", "--mode=vta")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("an unknown mode was accepted; it must not silently analyse less")
	}
	if !strings.Contains(string(out), "unknown mode") {
		t.Errorf("error does not name the problem: %s", out)
	}
}

// An incomplete graph that looks complete is the failure this whole command is
// about, so the warning has to reach stderr.
func TestIllTypedPackageWarns(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"go.mod":     "module example.com/p\n\ngo 1.26\n",
		"p/p.go":     "package p\n\nfunc A() {}\n",
		"bad/bad.go": "package bad\n\nfunc B() { nope() }\n",
	}, nil)

	_, stderr := run(t, buildCmd(t), dir, "--mode=cha")

	if !strings.Contains(stderr, "did not type-check") {
		t.Errorf("no warning about the ill-typed package: %s", stderr)
	}
	if !strings.Contains(stderr, "INCOMPLETE") {
		t.Errorf("cha mode did not warn that the graph is incomplete: %s", stderr)
	}
}

// git's path format is configurable, and three settings would each silently yield a
// zero-change delta: diff.relative makes paths relative to dir rather than the repo
// root, and mnemonicprefix/noprefix change or drop the "b/" prefix. The invocation
// pins all of them, so a repo carrying these settings must behave identically.
func TestSinceIsImmuneToGitPathConfig(t *testing.T) {
	files := map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.26\n",
		"p/a.go": "package p\n\nfunc A() { B() }\n",
		"p/b.go": "package p\n\nfunc B() {}\n",
	}
	after := map[string]string{"p/a.go": "package p\n\nfunc A() { B(); B() }\n"}

	for _, cfg := range [][]string{
		nil,
		{"diff.relative", "true"},
		{"diff.mnemonicprefix", "true"},
		{"diff.noprefix", "true"},
	} {
		name := "default"
		if cfg != nil {
			name = cfg[0]
		}
		t.Run(name, func(t *testing.T) {
			dir := gitRepo(t, files, after)
			if cfg != nil {
				c := exec.Command("git", "-C", dir, "config", cfg[0], cfg[1])
				c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("git config: %v\n%s", err, out)
				}
			}
			_, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")
			if !strings.Contains(stderr, "1 changed fn") {
				t.Errorf("%s: expected 1 changed function, got: %s", name, strings.TrimSpace(stderr))
			}
		})
	}
}

// A path containing a space gets a literal TAB appended by git, and non-ASCII is
// escaped and quoted unless core.quotePath is off.
func TestSinceHandlesAwkwardFilenames(t *testing.T) {
	dir := gitRepo(t,
		map[string]string{
			"go.mod":              "module example.com/p\n\ngo 1.26\n",
			"p/my file.go":        "package p\n\nfunc Spaced() {}\n",
			"p/\u00e9 unicode.go": "package p\n\nfunc Accented() {}\n",
		},
		map[string]string{
			"p/my file.go": "package p\n\nfunc Spaced() { _ = 1 }\n",
		})

	stdout, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")

	if !strings.Contains(stderr, "1 changed fn") {
		t.Errorf("a changed file whose name contains a space was not matched: %s",
			strings.TrimSpace(stderr))
	}
	if !strings.Contains(stdout, `label="p.Spaced"`) {
		t.Errorf("the changed function is missing:\n%s", stdout)
	}
}

// A delta the user asked for that cannot be computed is not a graph. Emitting an
// empty one with exit 0 is the failure mode this command exists to avoid.
func TestSinceFailureExitsNonZero(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.26\n",
		"p/p.go": "package p\n\nfunc A() {}\n",
	}, nil)

	cmd := exec.Command(buildCmd(t), dir, "./...", "--since", "nosuchref")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("an unresolvable --since ref exited 0:\n%s", out)
	}
	if !strings.Contains(string(out), "refusing to emit") {
		t.Errorf("no explanation of why nothing was emitted: %s", out)
	}
	// git's own diagnosis is the useful part; it must not be swallowed by
	// cmd.Output() capturing stderr into ExitError.
	if !strings.Contains(string(out), "revision") && !strings.Contains(string(out), "ambiguous") {
		t.Errorf("git's diagnosis was swallowed: %s", out)
	}
}

// ParseMode is strict, but the argument loop has to be too: a single-dash typo or a
// value-less flag must not quietly analyse in static mode and present the result as
// the graph that was asked for.
func TestMalformedFlagsAreRejected(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.26\n",
		"p/p.go": "package p\n\nfunc A() {}\n",
	}, nil)
	bin := buildCmd(t)

	for _, args := range [][]string{
		{"-mode=cha"},      // single dash
		{"--mode"},         // no value
		{"--depth"},        // no value
		{"--depth", "abc"}, // not a number
		{"--dept=2"},       // typo
	} {
		out, err := exec.Command(bin, append([]string{dir, "./..."}, args...)...).CombinedOutput()
		if err == nil {
			t.Errorf("%v was accepted; it must not silently fall back: %s", args, out)
		}
	}
}
