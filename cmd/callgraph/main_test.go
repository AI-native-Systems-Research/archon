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
	return gitRepoAt(t, "", files, after)
}

// gitRepoAt puts the module under sub/ inside the repository. git reports diff
// paths relative to the repo ROOT, so only a nested module exercises diff.relative
// and the root-joining logic.
func gitRepoAt(t *testing.T, sub string, files, after map[string]string) string {
	t.Helper()
	nest := func(m map[string]string) map[string]string {
		if sub == "" || m == nil {
			return m
		}
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[filepath.Join(sub, k)] = v
		}
		return out
	}
	root := gitRepoRoot(t, nest(files), nest(after))
	return filepath.Join(root, sub)
}

func gitRepoRoot(t *testing.T, files, after map[string]string) string {
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

// gitConfig sets a config key on the repo, isolated from ambient config.
func gitConfig(t *testing.T, dir, key, val string) {
	t.Helper()
	c := exec.Command("git", "-C", dir, "config", key, val)
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git config %s=%s: %v\n%s", key, val, err, out)
	}
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
	buildDir  string // recorded separately: on a build failure builtBin stays empty
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
		buildDir = dir
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

	if !strings.Contains(stderr, "failed to type-check") {
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
		// Replaces the output format wholesale, which no path-format pin can cover.
		{"diff.external", "/usr/bin/true"},
	} {
		name := "default"
		if cfg != nil {
			name = cfg[0]
		}
		t.Run(name, func(t *testing.T) {
			// Nested module: diff.relative has nothing to make relative when the
			// module IS the repo root, which is why the previous version of this
			// subtest passed against the unfixed binary.
			dir := gitRepoAt(t, "sub", files, after)
			if cfg != nil {
				gitConfig(t, dir, cfg[0], cfg[1])
			}
			_, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")
			if !strings.Contains(stderr, "1 changed fn") {
				t.Errorf("%s: expected 1 changed function, got: %s", name, strings.TrimSpace(stderr))
			}
		})
	}
}

// A .gitattributes marking .go as binary makes git print "Binary files ... differ"
// with no hunks. --text overrides that, so the delta still works. Pinned because
// without --text this silently reported nothing changed.
//
// The .gitattributes file must sit at the repo root, above the module, which is
// also why gitRepoAt exists.
func TestGitattributesBinaryDoesNotBreakSince(t *testing.T) {
	dir := gitRepoAt(t, "sub", map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.26\n",
		"p/a.go": "package p\n\nfunc A() { B() }\n\nfunc B() {}\n",
	}, map[string]string{
		"p/a.go": "package p\n\nfunc A() { B(); B() }\n\nfunc B() {}\n",
	})
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), ".gitattributes"),
		[]byte("*.go -diff\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")
	if !strings.Contains(stderr, "1 changed fn") {
		t.Errorf("a .gitattributes marking .go binary broke the delta: %s", strings.TrimSpace(stderr))
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
			// Must be in the COMMITTED set, not only in the post-commit map: an
			// untracked file never appears in git diff, so the quoted-path branch
			// went unexercised twice before this.
			"p/qu\"ote.go": "package p\n\nfunc Quoted() {}\n",
		},
		map[string]string{
			"p/my file.go":        "package p\n\nfunc Spaced() { _ = 1 }\n",
			"p/\u00e9 unicode.go": "package p\n\nfunc Accented() { _ = 1 }\n",
			"p/qu\"ote.go":        "package p\n\nfunc Quoted() { _ = 1 }\n",
		})

	stdout, stderr := run(t, buildCmd(t), dir, "--since", "HEAD")

	if !strings.Contains(stderr, "3 changed fn") {
		t.Errorf("awkward filenames were not matched: %s", strings.TrimSpace(stderr))
	}
	// A space needs the trailing-TAB trim; a quote needs the C-unquote. Both files
	// must be TRACKED — in the previous version the quoted one lived only in the
	// post-commit map, so it was untracked and never reached a diff at all.
	for _, want := range []string{`label="p.Spaced"`, `label="p.Accented"`, `label="p.Quoted"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %s:\n%s", want, stdout)
		}
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
	cmd.Env = append(os.Environ(), "LC_ALL=C") // git's diagnosis is translated
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
		{"--mode="},        // empty value, e.g. --mode=$UNSET
		{"--since="},       // empty value: asked for a delta, would get a full graph
		{"--depth", "-3"},  // negative: the label would claim an expansion
		// The space-separated empty forms are what "--since $UNSET" actually
		// produces, and they stayed broken after only the "=" forms were guarded.
		// --since "" was the worst of them: a full graph, exit 0, for a user who
		// asked for a delta.
		{"--since", ""},
		{"--mode", ""},
	} {
		out, err := exec.Command(bin, append([]string{dir, "./..."}, args...)...).CombinedOutput()
		if err == nil {
			t.Errorf("%v was accepted; it must not silently fall back: %s", args, out)
		}
	}
}

// TestMain removes the directory buildCmd created; it cannot use t.TempDir()
// because the binary is shared across tests, and without this every go test run
// leaves ~8MB behind.
func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// A diff can legitimately contain file headers and no new-side hunk: a deletion, a
// mode change, a rename with no edit, an added empty file. For every one of them
// "no function changed" is the correct answer, and an earlier version of this tool
// exited 1 on all four.
func TestNoHunkDiffsAreNotFailures(t *testing.T) {
	base := map[string]string{
		"go.mod": "module example.com/p\n\ngo 1.26\n",
		"p/a.go": "package p\n\nfunc A() { B() }\n\nfunc B() {}\n",
		"p/c.go": "package p\n\nfunc C() {}\n",
	}
	cases := map[string]func(t *testing.T, dir string){
		"deletion": func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "p/c.go")); err != nil {
				t.Fatal(err)
			}
		},
		"mode change": func(t *testing.T, dir string) {
			if err := os.Chmod(filepath.Join(dir, "p/c.go"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"rename without edit": func(t *testing.T, dir string) {
			c := exec.Command("git", "-C", dir, "mv", "p/c.go", "p/renamed.go")
			c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git mv: %v\n%s", err, out)
			}
		},
		"added empty file": func(t *testing.T, dir string) {
			p := filepath.Join(dir, "p/new.go")
			if err := os.WriteFile(p, []byte("package p\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			c := exec.Command("git", "-C", dir, "add", "p/new.go")
			c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git add: %v\n%s", err, out)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := gitRepo(t, base, nil)
			mutate(t, dir)
			cmd := exec.Command(buildCmd(t), dir, "./...", "--since", "HEAD")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("exited non-zero on a diff with no new-side hunk:\n%s", out)
			}
			if !strings.Contains(string(out), "0 changed fn") {
				t.Errorf("expected 0 changed functions, got: %s", firstLine(string(out)))
			}
		})
	}
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "callgraph:") {
			return l
		}
	}
	return strings.TrimSpace(s)
}

// With --unified=0 an ADDED source line beginning with "++ " renders as "+++ ...",
// which is legal Go inside a raw string literal. Honouring it as a file header
// re-attributed every following hunk to a path that does not exist.
func TestAddedLineCannotForgeAFileHeader(t *testing.T) {
	dir := gitRepo(t,
		map[string]string{
			"go.mod": "module example.com/p\n\ngo 1.26\n",
			"p/a.go": "package p\n\nvar patch = `\n`\n\nfunc A() { B() }\n\nfunc B() {}\n",
		},
		map[string]string{
			// The added lines inside the raw string look exactly like diff headers.
			"p/a.go": "package p\n\nvar patch = `\n+++ b/nonexistent.go\n@@ -1 +1 @@\n`\n\nfunc A() { B(); B() }\n\nfunc B() {}\n",
		})

	cmd := exec.Command(buildCmd(t), dir, "./...", "--since", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("a forged header in file content broke the run:\n%s", out)
	}
	if strings.Contains(string(out), "nonexistent.go") {
		t.Errorf("content was parsed as a file header:\n%s", out)
	}
	// The real change must still be attributed.
	if !strings.Contains(string(out), `label="p.A"`) {
		t.Errorf("the genuine change was lost to the forged header:\n%s", out)
	}
}

// The cause of a type error can sit outside the matched set — a dependency, or a
// package the pattern misses. Then every entry is an importer, and "0 package(s)
// failed to type-check, and 3 more..." is incoherent.
func TestIllTypedWithNoOwnErrorReadsCoherently(t *testing.T) {
	dir := gitRepo(t, map[string]string{
		"go.mod":      "module example.com/p\n\ngo 1.26\n",
		"broken/b.go": "package broken\n\nfunc B() { nope() }\n",
		"a/a.go":      "package a\n\nimport \"example.com/p/broken\"\n\nvar _ = broken.B\n",
	}, nil)

	// Match only the importer, so the cause is outside the pattern.
	cmd := exec.Command(buildCmd(t), dir, "./a/...", "--mode=cha")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run failed:\n%s", out)
	}
	if strings.Contains(string(out), "0 package(s) failed") {
		t.Errorf("incoherent count when no package has its own error:\n%s", out)
	}
	if !strings.Contains(string(out), "outside the matched set") {
		t.Errorf("did not explain that the cause is not among the matched packages:\n%s", out)
	}
}
