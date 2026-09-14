package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
			// Only the nested copy changes.
			"other/p/x.go": "package p\n\nfunc OtherPX() { _ = 1 }\n",
		})

	stdout, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")

	if !strings.Contains(stderr, "1 changed fn") {
		t.Errorf("expected exactly 1 changed function, got: %s", strings.TrimSpace(stderr))
	}
	if !strings.Contains(stdout, `label="p.OtherPX"`) {
		t.Errorf("the function that actually changed is missing:\n%s", stdout)
	}
	if strings.Contains(stdout, `label="p.PX"`) {
		t.Errorf("p/x.go was marked changed because other/p/x.go ends with the same "+
			"path suffix:\n%s", stdout)
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
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
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

func buildCmd(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "callgraph")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
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
