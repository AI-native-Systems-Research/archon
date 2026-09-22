package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// buildTool compiles archon-go so the flag table and the exit codes are
// exercised the way a user meets them, through main.
func buildArchon(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "archon-go")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// run invokes the subcommand against this repository.
func runCG(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	return runCGIn(t, bin, ".", "./internal/graph/...", args...)
}

func runCGIn(t *testing.T, bin, dir, pattern string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"callgraph", dir, pattern}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return stdout.String(), stderr.String(), code
}

func TestCallgraphSubcommand(t *testing.T) {
	bin := buildArchon(t)

	t.Run("bare --mode does not fall through to static", func(t *testing.T) {
		// The whole point of the flag is to stop producing the graph with the
		// interface calls missing, so accepting it silently is the worst answer.
		stdout, stderr, code := runCG(t, bin, "--mode")
		if code != 2 {
			t.Errorf("exit code %d, want 2; stderr %q", code, stderr)
		}
		if !strings.Contains(stderr, "takes a value") {
			t.Errorf("stderr should say what is wrong, got %q", stderr)
		}
		if strings.Contains(stdout, "digraph") {
			t.Error("a graph was emitted for a bad flag")
		}
	})

	t.Run("unknown mode value", func(t *testing.T) {
		_, stderr, code := runCG(t, bin, "--mode=CHA")
		if code != 2 || !strings.Contains(stderr, "unknown mode") {
			t.Errorf("exit %d, stderr %q; want exit 2 and an unknown-mode message", code, stderr)
		}
	})

	t.Run("rta on a library names the mode that works", func(t *testing.T) {
		_, stderr, code := runCG(t, bin, "--mode=rta")
		if code != 1 {
			t.Errorf("exit code %d, want 1; stderr %q", code, stderr)
		}
		if !strings.Contains(stderr, "cha") {
			t.Errorf("stderr should point at cha, got %q", stderr)
		}
	})

	t.Run("cha says so in the graph and in the summary", func(t *testing.T) {
		stdout, stderr, code := runCG(t, bin, "--mode=cha")
		if code != 0 {
			t.Fatalf("exit code %d; stderr %q", code, stderr)
		}
		if !strings.Contains(stdout, "digraph callgraph {") {
			t.Error("no DOT on stdout")
		}
		// The DOT is the artifact that gets saved and shared, so it has to name
		// the builder itself rather than leaving it on stderr.
		if !strings.Contains(stdout, "interface calls resolved") {
			t.Error("the graph title does not say the mode")
		}
		if !strings.Contains(stderr, ", cha]") {
			t.Errorf("the summary does not name the mode: %q", stderr)
		}
		// internal/graph makes no interface calls, so the only thing dashed here
		// is the legend key. The edges themselves are checked below against a
		// module that has some.
		if !strings.Contains(stdout, "calls through an interface") {
			t.Error("the legend has no key for the dashed edges")
		}
	})

	t.Run("an interface call is drawn dashed, with its witness", func(t *testing.T) {
		stdout, stderr, code := runCGIn(t, bin, "internal/callgraph/testdata/iface", "./...", "--mode=cha")
		if code != 0 {
			t.Fatalf("exit code %d; stderr %q", code, stderr)
		}
		// A reader of the artifact has to be able to tell an interface call from
		// a direct one, and to see which method was dispatched.
		if !strings.Contains(stdout, "style=dashed") {
			t.Error("no dashed edge")
		}
		if !strings.Contains(stdout, `tooltip="dispatched through store.Store.Get"`) {
			t.Error("a dashed edge does not name the method it dispatched")
		}
	})

	t.Run("static draws no dashed edge and no key for one", func(t *testing.T) {
		stdout, _, code := runCG(t, bin)
		if code != 0 {
			t.Fatal(code)
		}
		if strings.Contains(stdout, "style=dashed") || strings.Contains(stdout, "calls through an interface") {
			t.Error("static mode resolves no dispatch, so it must draw none")
		}
	})

	t.Run("a package that does not type-check is reported", func(t *testing.T) {
		_, stderr, _ := runCGIn(t, bin, "internal/callgraph/testdata/illtyped", "./...", "--mode=cha")
		if !strings.Contains(stderr, "did not type-check") || !strings.Contains(stderr, "cause ") {
			t.Errorf("the warning is not wired up: %q", stderr)
		}
	})

	t.Run("interface calls that produced no edge are reported", func(t *testing.T) {
		_, stderr, _ := runCGIn(t, bin, "internal/callgraph/testdata/iface", "./...", "--mode=cha")
		if !strings.Contains(stderr, "unresolved interface dispatches") || !strings.Contains(stderr, "inside wrappers") {
			t.Errorf("the report is not wired up: %q", stderr)
		}
	})

	t.Run("default is static, and repeats byte for byte", func(t *testing.T) {
		first, stderr, code := runCG(t, bin)
		if code != 0 {
			t.Fatalf("exit code %d; stderr %q", code, stderr)
		}
		if strings.Contains(first, "interface calls resolved") {
			t.Error("the default mode should not claim to resolve interface calls")
		}
		second, _, _ := runCG(t, bin)
		if first != second {
			t.Error("two runs on identical input produced different output")
		}
	})
}

// runInv invokes the invariants subcommand.
func runInv(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"invariants"}, args...)...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return stdout.String(), stderr.String(), code
}

// invFixture is a tiny repo whose registry and citations are committed under
// demo/flow4-invariants, so these checks need no BLIS checkout and run in CI.
const invFixture = "demo/flow4-invariants/fixture"

func TestInvariantsSubcommand(t *testing.T) {
	bin := buildArchon(t)

	t.Run("prints the table and the totals", func(t *testing.T) {
		stdout, stderr, code := runInv(t, bin, invFixture, "invariants.md")
		if code != 0 {
			t.Fatalf("exit %d; stderr %q", code, stderr)
		}
		// Whole rows and the exact totals line. "LINKED" is a substring of
		// "UNLINKED", so loose substring checks pass even when every status is
		// wrong — and the number is this command's entire product.
		for _, want := range []string{
			"INV-1        LINKED        1     0      1      1",
			"INV-2        TEST ONLY     0     1      1      0",
			"INV-99       UNLINKED      0     0      0      0",
			"  3 of 4 anchored — 2 LINKED, 1 TEST ONLY, 1 UNLINKED",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout missing %q:\n%s", want, stdout)
			}
		}
	})

	t.Run("--json carries the derived status", func(t *testing.T) {
		stdout, stderr, code := runInv(t, bin, invFixture, "invariants.md", "--json")
		if code != 0 {
			t.Fatalf("exit %d; stderr %q", code, stderr)
		}
		var res struct {
			Registry string `json:"registry"`
			Links    []struct {
				Status    string `json:"status"`
				Citations int    `json:"citations"`
				Invariant struct {
					ID    string `json:"id"`
					Scope string `json:"scope"`
				} `json:"invariant"`
			} `json:"links"`
			Totals struct {
				Linked   int `json:"linked"`
				TestOnly int `json:"test_only"`
				Unlinked int `json:"unlinked"`
			} `json:"totals"`
		}
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("--json is not valid JSON: %v\n%s", err, stdout)
		}
		if res.Registry != "invariants.md" {
			t.Errorf("registry = %q", res.Registry)
		}
		if len(res.Links) == 0 {
			t.Fatal("no links in JSON")
		}
		want := map[string]struct {
			status string
			cites  int
		}{
			"INV-1":  {"LINKED", 1},
			"INV-2":  {"TEST ONLY", 1},
			"INV-6":  {"LINKED", 2},
			"INV-99": {"UNLINKED", 0},
		}
		for _, l := range res.Links {
			w, ok := want[l.Invariant.ID]
			if !ok {
				t.Errorf("unexpected invariant %s", l.Invariant.ID)
				continue
			}
			if l.Status != w.status || l.Citations != w.cites {
				t.Errorf("%s = %s/%d cites, want %s/%d", l.Invariant.ID, l.Status, l.Citations, w.status, w.cites)
			}
			// The scope is the registry as asked for, never a temp worktree.
			if strings.HasPrefix(l.Invariant.Scope, "/") {
				t.Errorf("%s scope is absolute: %q", l.Invariant.ID, l.Invariant.Scope)
			}
		}
		if res.Totals.Linked != 2 || res.Totals.TestOnly != 1 || res.Totals.Unlinked != 1 {
			t.Errorf("totals = %+v, want 2/1/1", res.Totals)
		}
	})

	t.Run("omitting the registry names the conventional paths", func(t *testing.T) {
		stdout, stderr, code := runInv(t, bin, invFixture)
		if code == 0 {
			t.Errorf("exit 0 with no registry given; stdout %q", stdout)
		}
		for _, want := range []string{"docs/contributing/standards/invariants.md", "docs/invariants.md", "INVARIANTS.md"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr should name %q, got %q", want, stderr)
			}
		}
	})

	t.Run("an absolute registry path with --at is refused", func(t *testing.T) {
		// Reading code at a commit and the registry from the working tree is a
		// silent mismatch, so it is refused rather than guessed at.
		_, stderr, code := runInv(t, bin, ".", "/etc/hosts", "--at", "HEAD")
		if code == 0 {
			t.Error("an absolute registry path with --at should be refused")
		}
		if !strings.Contains(stderr, "repo-relative") {
			t.Errorf("stderr should say why, got %q", stderr)
		}
	})

	t.Run("a bad --at fails for the right reason", func(t *testing.T) {
		// Against a registry that exists, so the failure cannot be the registry
		// being missing — which is what made this pass before while --at itself
		// had no working coverage at all.
		_, stderr, code := runInv(t, bin, ".", "demo/flow4-invariants/fixture/invariants.md", "--at", "definitely-not-a-commit")
		if code == 0 {
			t.Error("exit 0 for a commit that does not exist")
		}
		if !strings.Contains(stderr, "definitely-not-a-commit") {
			t.Errorf("stderr should name the bad commit, got %q", stderr)
		}
	})

	t.Run("--at with an empty value is refused, both spellings", func(t *testing.T) {
		// `--at "$SHA"` with an unset SHA is how a script produces this, and
		// accepting it reports a live working-tree scan as a pinned run.
		for _, spelling := range [][]string{{"--at", ""}, {"--at="}} {
			args := append([]string{invFixture, "invariants.md"}, spelling...)
			_, stderr, code := runInv(t, bin, args...)
			if code == 0 {
				t.Errorf("%v should be refused", spelling)
			}
			if !strings.Contains(stderr, "takes a value") {
				t.Errorf("%v: stderr should say what is wrong, got %q", spelling, stderr)
			}
		}
	})

	t.Run("--at= with no value is refused", func(t *testing.T) {
		// Accepting it scanned the working tree and dropped the commit line: a
		// pinned invocation silently becoming unpinned.
		_, stderr, code := runInv(t, bin, invFixture, "invariants.md", "--at=")
		if code == 0 {
			t.Error("--at= should be refused")
		}
		if !strings.Contains(stderr, "takes a value") {
			t.Errorf("stderr should say what is wrong, got %q", stderr)
		}
	})

	t.Run("a mistyped flag is refused, not treated as a path", func(t *testing.T) {
		_, stderr, code := runInv(t, bin, invFixture, "invariants.md", "--jsonn")
		if code == 0 {
			t.Error("--jsonn should be refused; the caller asked for JSON")
		}
		if !strings.Contains(stderr, "unknown flag") {
			t.Errorf("stderr should name the flag, got %q", stderr)
		}
	})

	t.Run("a registry path escaping the repo is refused", func(t *testing.T) {
		// Relative but climbing out reads a registry that is in the repo at no
		// commit, while the report still claims to be pinned.
		_, stderr, code := runInv(t, bin, invFixture, "../../../etc/hosts", "--at", "HEAD")
		if code == 0 {
			t.Error("an escaping registry path should be refused")
		}
		if !strings.Contains(stderr, "escapes") {
			t.Errorf("stderr should say why, got %q", stderr)
		}
	})

	t.Run("a third positional is refused, not ignored", func(t *testing.T) {
		_, stderr, code := runInv(t, bin, invFixture, "invariants.md", "other.md")
		if code == 0 {
			t.Error("a third positional should be refused rather than silently dropped")
		}
		if !strings.Contains(stderr, "too many arguments") {
			t.Errorf("stderr should say what is wrong, got %q", stderr)
		}
	})

	t.Run("--at without a value does not fall through", func(t *testing.T) {
		_, stderr, code := runInv(t, bin, invFixture, "invariants.md", "--at")
		if code == 0 {
			t.Error("bare --at should not be accepted")
		}
		if !strings.Contains(stderr, "takes a value") {
			t.Errorf("stderr should say what is wrong, got %q", stderr)
		}
	})
}

// TestInvariantsAtCommit is the coverage that was missing entirely: nothing
// exercised a *successful* --at, so making the flag inert, or reading the
// registry from the working tree while scanning the commit, left the whole suite
// green while the report claimed a pin it did not have.
//
// The fixture is a throwaway git repo with two commits whose registries differ,
// so a run at the first commit must disagree with a run at HEAD.
func TestInvariantsAtCommit(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")

	// First commit: one invariant, cited.
	write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n")
	write("core/engine.go", "package core\n\n// Conserve upholds INV-1.\nfunc Conserve() {}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "first")
	first := git("rev-parse", "HEAD")

	// Second commit: a second invariant, declared but cited nowhere.
	write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n\n### INV-2: Two\n\n**Statement:** Second.\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "second")

	at := func(commit string) string {
		t.Helper()
		args := []string{"invariants", repo, "docs/invariants.md"}
		if commit != "" {
			args = append(args, "--at", commit)
		}
		cmd := exec.Command(bin, args...)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("invariants --at %s: %v\n%s", commit, err, stderr.String())
		}
		return stdout.String()
	}

	atFirst, atHead, unpinned := at(first), at("HEAD"), at("")

	// The registry at the first commit declares one invariant; at HEAD, two. If
	// --at were inert, or the registry came from the working tree, these would
	// agree.
	if !strings.Contains(atFirst, "(1 declared)") {
		t.Errorf("at the first commit the registry declares 1 invariant:\n%s", atFirst)
	}
	if !strings.Contains(atHead, "(2 declared)") {
		t.Errorf("at HEAD the registry declares 2:\n%s", atHead)
	}
	if !strings.Contains(atFirst, "1 of 1 anchored") {
		t.Errorf("at the first commit, INV-1 is anchored:\n%s", atFirst)
	}
	if !strings.Contains(atHead, "1 of 2 anchored") {
		t.Errorf("at HEAD, INV-2 is declared and cited nowhere:\n%s", atHead)
	}

	// The commit line carries the resolved SHA, not the ref: "commit: HEAD"
	// claims a pin that moves.
	head := git("rev-parse", "HEAD")
	if !strings.Contains(atHead, "commit:   "+head) {
		t.Errorf("--at HEAD should record the resolved SHA %s:\n%s", head, atHead)
	}
	if strings.Contains(atHead, "commit:   HEAD") {
		t.Error("--at recorded the ref verbatim instead of resolving it")
	}
	if strings.Contains(unpinned, "commit:") {
		t.Errorf("without --at there is no commit line:\n%s", unpinned)
	}

	// No temp worktree path anywhere in the output.
	for _, out := range []string{atFirst, atHead} {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "archon-wt-") {
				t.Errorf("temp worktree path leaked into output: %q", line)
			}
		}
	}

	// A subdirectory under --at would read the root registry and scan the whole
	// tree while labelling the output identically, so it is refused.
	cmd := exec.Command(bin, "invariants", filepath.Join(repo, "core"), "docs/invariants.md", "--at", "HEAD")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Error("--at on a subdirectory should be refused")
	}
	if !strings.Contains(stderr.String(), "repository root") {
		t.Errorf("stderr should say why, got %q", stderr.String())
	}

	// Every failure under --at must still remove its worktree: fatal is
	// os.Exit, which runs no deferred function, and a leaked worktree stays
	// registered in the target repo's .git metadata.
	before := git("worktree", "list")
	for i := 0; i < 3; i++ {
		c := exec.Command(bin, "invariants", repo, "docs/nope.md", "--at", "HEAD")
		_ = c.Run()
	}
	if after := git("worktree", "list"); after != before {
		t.Errorf("failed --at runs leaked worktrees:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestInvariantsAtRejectsUnpinnable covers the two ways a report could print a
// pinned SHA over content that is not pinned.
func TestInvariantsAtRejectsUnpinnable(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()
	outside := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "reg.md"), []byte("### INV-OUT: Outside\n\n**Statement:** Not in the repo.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"docs/invariants.md": "### INV-1: One\n\n**Statement:** First.\n",
		"core/engine.go":     "package core\n\n// Conserve upholds INV-1.\nfunc Conserve() {}\n",
	} {
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A committed symlink pointing out of the repository: cleaning the path
	// cannot see it, so only resolving it does.
	if err := os.Symlink(filepath.Join(outside, "reg.md"), filepath.Join(repo, "escape.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	git("add", "-A")
	git("commit", "--quiet", "-m", "first")

	run := func(args ...string) (string, string, int) {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"invariants", repo}, args...)...)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return stdout.String(), stderr.String(), code
	}

	t.Run("a symlink out of the repo cannot be pinned", func(t *testing.T) {
		stdout, stderr, code := run("escape.md", "--at", "HEAD")
		if code == 0 {
			t.Errorf("accepted a registry symlinked outside the repo:\n%s", stdout)
		}
		if !strings.Contains(stderr, "outside the tree") {
			t.Errorf("stderr should say why, got %q", stderr)
		}
	})

	t.Run("--at does not swallow a following flag", func(t *testing.T) {
		// git rev-parse "--json^{commit}" exits 0 echoing its argument back, so
		// the SHA resolution below would have accepted this.
		_, stderr, code := run("docs/invariants.md", "--at", "--json")
		if code == 0 {
			t.Error("--at took a flag as its commit")
		}
		if !strings.Contains(stderr, "not a flag") {
			t.Errorf("stderr should say why, got %q", stderr)
		}
	})
}

// TestPRReviewRegistryDiscovery covers issue #65's discovery table end to end.
//
// For the no-registry case it asserts what it can observe here: no section in
// review.md and no registry key in review.json, so nothing is added to the bundle.
// Byte-identity against a pre-feature build is a claim about two binaries and is
// verified in review rather than here.
//
// It runs in Go rather than only in demo/run-all.sh because CI leaves BLIS_REPO
// unset, which skips flow 1 entirely — so the demo alone proves nothing on a PR.
func TestPRReviewRegistryDiscovery(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write("go.mod", "module m\n\ngo 1.26.3\n")
	write("core/engine.go", "package core\n\n// Conserve upholds INV-1.\nfunc Conserve() {}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "base")
	base := git("rev-parse", "HEAD")

	write("leaf/leaf.go", "package leaf\n\n// Leaf also cites INV-1.\nfunc Leaf() {}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "head")
	head := git("rev-parse", "HEAD")

	run := func(t *testing.T, out string, extra ...string) (string, string, int) {
		t.Helper()
		args := append([]string{"pr-review", repo, base, head, "--out", out}, extra...)
		cmd := exec.Command(bin, args...)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("pr-review: %v\n%s", err, stderr.String())
		}
		return stdout.String(), stderr.String(), code
	}
	read := func(t *testing.T, p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// --- no registry anywhere: no section, and this is the baseline bundle ---
	noRegDir := filepath.Join(t.TempDir(), "none")
	if _, stderr, code := run(t, noRegDir); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	baseMD := read(t, filepath.Join(noRegDir, "review.md"))
	baseJSON := read(t, filepath.Join(noRegDir, "review.json"))
	if _, _, code := run(t, filepath.Join(t.TempDir(), "again")); code != 0 {
		t.Errorf("pr-review without a registry exited %d; it is report-only", code)
	}
	if strings.Contains(baseMD, "Declared invariants") {
		t.Errorf("a section rendered with no registry present:\n%s", baseMD)
	}
	if strings.Contains(baseJSON, "\"registry\"") {
		t.Error("review.json gained a registry key with no registry present")
	}

	// --- explicit path that does not exist: hard error ---
	t.Run("explicit missing path is a hard error", func(t *testing.T) {
		_, stderr, code := run(t, filepath.Join(t.TempDir(), "x"), "--invariants", "docs/nope.md")
		if code == 0 {
			t.Error("exit 0 for a registry that does not exist")
		}
		if !strings.Contains(stderr, "docs/nope.md") {
			t.Errorf("stderr should name the path asked for, got %q", stderr)
		}
	})

	// --- explicit path that exists but declares nothing: hard error ---
	t.Run("explicit unparseable path is a hard error", func(t *testing.T) {
		write("docs/empty.md", "# Nothing here\n\nJust prose.\n")
		git("add", "-A")
		git("commit", "--quiet", "-m", "empty registry")
		h := git("rev-parse", "HEAD")
		cmd := exec.Command(bin, "pr-review", repo, base, h, "--out", filepath.Join(t.TempDir(), "y"), "--invariants", "docs/empty.md")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			t.Error("a registry declaring no invariants should be a hard error when asked for by path")
		}
		if !strings.Contains(stderr.String(), "no invariant entries") {
			t.Errorf("stderr should say why, got %q", stderr.String())
		}
	})

	// --- registry at a conventional path: discovered, named, section rendered ---
	t.Run("auto-discovery names the path and renders", func(t *testing.T) {
		write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n\n### INV-99: Never cited\n\n**Statement:** Nowhere.\n")
		git("add", "-A")
		git("commit", "--quiet", "-m", "add registry")
		h := git("rev-parse", "HEAD")

		out := filepath.Join(t.TempDir(), "disc")
		cmd := exec.Command(bin, "pr-review", repo, base, h, "--out", out)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v\n%s", err, stderr.String())
		}
		md := read(t, filepath.Join(out, "review.md"))
		if !strings.Contains(md, "### Declared invariants — registry") {
			t.Errorf("no section:\n%s", md)
		}
		// Naming the path is what turns a silent disable into something a reader
		// notices if the doc is ever renamed.
		if !strings.Contains(md, "`docs/invariants.md`") {
			t.Errorf("the section must name the discovered path:\n%s", md)
		}
		// Standing UNLINKED is no longer a per-row line in pr-review (#74): repeating
		// it on every PR trains readers to skip the section, and it is the audit
		// surface's job (archon invariants). The header totals still surface that one
		// exists, so the finding is not lost — only moved out of the reviewer's way.
		if strings.Contains(md, "is declared but cited in no file") {
			t.Errorf("standing UNLINKED should not render as a per-row finding:\n%s", md)
		}
		if !strings.Contains(md, "1 UNLINKED") {
			t.Errorf("the header totals should still report the uncited invariant:\n%s", md)
		}
		if !strings.Contains(stderr.String(), "found invariant registry at docs/invariants.md") {
			t.Errorf("discovery should say what it found, got %q", stderr.String())
		}
		// Advisory extends to the exit code: pr-review is report-only either way.
		if cmd.ProcessState.ExitCode() != 0 {
			t.Errorf("exit %d with a registry; pr-review is report-only", cmd.ProcessState.ExitCode())
		}

		// Advisory: the verdict is whatever it was without a registry.
		if !strings.Contains(md, "Verdict:") {
			t.Fatal("no verdict line")
		}
		if v := verdictLine(baseMD); v != verdictLine(md) {
			t.Errorf("verdict moved: %q vs %q", v, verdictLine(md))
		}
	})
}

// verdictLine extracts the verdict line so the advisory guarantee can be compared
// without comparing bundles built from different commits.
func verdictLine(md string) string {
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, "**Verdict:") {
			return l
		}
	}
	return ""
}

// TestPRReviewTouchedCountsFromRealGitDiff asserts the numbers the section
// prints, computed from an actual `git diff` rather than a hand-fed list.
//
// Nothing did that before, which is how two wrong-number bugs survived: git
// quotes non-ASCII and space-bearing paths by default, so they never matched the
// linker's raw UTF-8 paths, and a two-dot diff against a base that has moved on
// attributed mainline commits to the change.
func TestPRReviewTouchedCountsFromRealGitDiff(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cite := func(id string) string {
		return "package p\n\n// upholds " + id + ".\nfunc F() {}\n"
	}

	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write("go.mod", "module m\n\ngo 1.26.3\n")
	write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n\n### INV-2: Two\n\n**Statement:** Second.\n")
	write("a/a.go", cite("INV-1"))
	write("b/b.go", cite("INV-1"))
	write("c/c.go", cite("INV-2"))
	git("add", "-A")
	git("commit", "--quiet", "-m", "base")
	base := git("rev-parse", "HEAD")

	// The change: touch one of INV-1's two files, and add a third whose path
	// needs quoting under git's default core.quotePath.
	write("a/a.go", cite("INV-1")+"\n// touched\n")
	write("café/naïve.go", cite("INV-1"))
	git("add", "-A")
	git("commit", "--quiet", "-m", "head")
	head := git("rev-parse", "HEAD")

	// Mainline moves on, touching INV-2's only file. A diff from base rather than
	// from the merge base would credit this change with it.
	git("checkout", "--quiet", "-b", "mainline", base)
	write("c/c.go", cite("INV-2")+"\n// unrelated\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "mainline moves")
	movedBase := git("rev-parse", "HEAD")
	git("checkout", "--quiet", "-")

	out := filepath.Join(t.TempDir(), "bundle")
	cmd := exec.Command(bin, "pr-review", repo, movedBase, head, "--out", out)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(out, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Registry struct {
			Rows []struct {
				ID                 string `json:"id"`
				CitingFilesTouched int    `json:"citingFilesTouched"`
				CitingFilesTotal   int    `json:"citingFilesTotal"`
			} `json:"rows"`
		} `json:"registry"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	total := map[string]int{}
	for _, r := range res.Registry.Rows {
		byID[r.ID] = r.CitingFilesTouched
		total[r.ID] = r.CitingFilesTotal
	}

	// INV-1: three citing files at head (a, b, café/naïve), two of them touched —
	// and the quoted path must be one of them.
	if total["INV-1"] != 3 {
		t.Errorf("INV-1 citing files = %d, want 3", total["INV-1"])
	}
	if byID["INV-1"] != 2 {
		t.Errorf("INV-1 touched = %d, want 2 — a path needing git quoting was likely dropped", byID["INV-1"])
	}
	// INV-2 was touched only by mainline, not by this change.
	if _, ok := byID["INV-2"]; ok {
		t.Errorf("INV-2 appears as touched (%d); mainline touched it, not this change", byID["INV-2"])
	}
}

// TestPRReviewReportsDeletedAnchors: removing an invariant's citation site is the
// change most likely to leave a declared promise unguarded, and counts read at
// head cannot see it — the file is gone.
func TestPRReviewReportsDeletedAnchors(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write("go.mod", "module m\n\ngo 1.26.3\n")
	write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n")
	write("a/a.go", "package a\n\n// upholds INV-1.\nfunc A() {}\n")
	write("b/b.go", "package b\n\n// also upholds INV-1.\nfunc B() {}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "base")
	base := git("rev-parse", "HEAD")

	if err := os.RemoveAll(filepath.Join(repo, "b")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "delete an anchor")
	head := git("rev-parse", "HEAD")

	out := filepath.Join(t.TempDir(), "bundle")
	cmd := exec.Command(bin, "pr-review", repo, base, head, "--out", out)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	md, err := os.ReadFile(filepath.Join(out, "review.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(md), "touched no file citing a declared invariant") {
		t.Errorf("deleting an anchor read as inaction:\n%s", md)
	}
	if !strings.Contains(string(md), "deletes files that cited `INV-1` (1)") {
		t.Errorf("the deleted anchor is not reported:\n%s", md)
	}
}

// TestPRReviewRegistryPinnedToHead covers the guarantees that were structurally
// invisible to the suite: every fixture repo had working tree == head, so reading
// the dirty checkout instead of the commit passed every test.
func TestPRReviewRegistryPinnedToHead(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	review := func(t *testing.T, extra ...string) (string, string) {
		t.Helper()
		out := filepath.Join(t.TempDir(), "bundle")
		args := append([]string{"pr-review", repo, git("rev-parse", "HEAD~1"), git("rev-parse", "HEAD"), "--out", out}, extra...)
		cmd := exec.Command(bin, args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v\n%s", err, stderr.String())
		}
		b, err := os.ReadFile(filepath.Join(out, "review.md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b), stderr.String()
	}

	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write("go.mod", "module m\n\ngo 1.26.3\n")
	write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n")
	write("a/a.go", "package a\n\n// upholds INV-1.\nfunc A() {}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "base")
	write("a/a.go", "package a\n\n// upholds INV-1.\nfunc A() {}\n\n// touched\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "head")
	head := git("rev-parse", "HEAD")

	t.Run("the section names the head commit", func(t *testing.T) {
		md, _ := review(t)
		if !strings.Contains(md, "at `"+head+"`") {
			t.Errorf("the section must name the commit it read:\n%s", md)
		}
	})

	t.Run("working-tree edits do not reach the report", func(t *testing.T) {
		// An uncommitted second citing file would raise the denominator from 1 to
		// 2 if the report read the checkout rather than the commit.
		write("b/uncommitted.go", "package b\n\n// also upholds INV-1.\nfunc B() {}\n")
		defer os.RemoveAll(filepath.Join(repo, "b"))
		md, _ := review(t)
		if !strings.Contains(md, "| `INV-1` | LINKED | 1 of 1 |") {
			t.Errorf("an uncommitted citing file reached the counts:\n%s", md)
		}
	})

	t.Run("an untracked registry is not discovered", func(t *testing.T) {
		// docs/invariants.md is tracked here, so probe the case where the only
		// candidate exists solely in the working tree.
		bare := t.TempDir()
		for _, a := range [][]string{{"init", "--quiet"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}} {
			cmd := exec.Command("git", a...)
			cmd.Dir = bare
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", a, err, out)
			}
		}
		for name, body := range map[string]string{
			"go.mod": "module n\n\ngo 1.26.3\n",
			"x/x.go": "package x\n\n// upholds INV-1.\nfunc X() {}\n",
		} {
			if err := os.MkdirAll(filepath.Join(bare, filepath.Dir(name)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bare, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, a := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "one"}} {
			cmd := exec.Command("git", a...)
			cmd.Dir = bare
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", a, err, out)
			}
		}
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = bare
		shaOut, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		sha := strings.TrimSpace(string(shaOut))
		// Untracked, so it must not be discovered.
		if err := os.WriteFile(filepath.Join(bare, "INVARIANTS.md"), []byte("### INV-1: One\n\n**Statement:** First.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "b2")
		c := exec.Command(bin, "pr-review", bare, sha, sha, "--out", out)
		var stderr strings.Builder
		c.Stderr = &stderr
		if err := c.Run(); err != nil {
			t.Fatalf("%v\n%s", err, stderr.String())
		}
		b, err := os.ReadFile(filepath.Join(out, "review.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "Declared invariants") {
			t.Errorf("an untracked registry was discovered:\n%s", b)
		}
		if !strings.Contains(stderr.String(), "no invariant registry at") {
			t.Errorf("the absence should be stated on stderr, got %q", stderr.String())
		}
	})

	t.Run("an explicit path wins over a conventional one", func(t *testing.T) {
		// docs/invariants.md exists and would be discovered; the flag names a
		// different file, and silently preferring the conventional one is a clean
		// looking wrong answer.
		write("docs/other.md", "### INV-77: Explicit\n\n**Statement:** Only in the explicit file.\n")
		git("add", "-A")
		git("commit", "--quiet", "-m", "add a second registry")
		md, stderr := review(t, "--invariants", "docs/other.md")
		if !strings.Contains(md, "`docs/other.md`") || strings.Contains(md, "`docs/invariants.md`") {
			t.Errorf("the explicit path must be the one used:\n%s", md)
		}
		// INV-77 is uncited, so it no longer renders as a row (#74). The distinguishing
		// content proof is the anchored count: other.md declares one uncited invariant
		// (0 of 1 anchored), where the conventional docs/invariants.md would report its
		// INV-1 as linked (1 of 1). "0 of 1 anchored" can only come from other.md.
		if !strings.Contains(md, "0 of 1 anchored") {
			t.Errorf("the section should reflect the explicit registry (INV-77, uncited):\n%s", md)
		}
		if strings.Contains(stderr, "found invariant registry") {
			t.Errorf("discovery should not run when a path is given, got %q", stderr)
		}
	})

	t.Run("probe order prefers the first conventional path", func(t *testing.T) {
		write("INVARIANTS.md", "### INV-88: Last resort\n\n**Statement:** Should lose.\n")
		git("add", "-A")
		git("commit", "--quiet", "-m", "add a lower-priority registry")
		md, _ := review(t)
		if !strings.Contains(md, "`docs/invariants.md`") {
			t.Errorf("docs/invariants.md ranks above INVARIANTS.md:\n%s", md)
		}
	})

	t.Run("zero anchored renders through the binary", func(t *testing.T) {
		write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n\n### INV-2: Two\n\n**Statement:** Second.\n")
		if err := os.RemoveAll(filepath.Join(repo, "a")); err != nil {
			t.Fatal(err)
		}
		write("a/a.go", "package a\n\n// no invariant is named here.\nfunc A() {}\n")
		git("add", "-A")
		git("commit", "--quiet", "-m", "nothing cites anything")
		md, _ := review(t)
		if !strings.Contains(md, "**0 of 2 anchored**") {
			t.Errorf("0 of N anchored must render as a finding:\n%s", md)
		}
	})
}

// TestPRReviewDeletedAnchorsRespectTheScanSet: a deleted file that the link scan
// would never have read was never an anchor, and announcing it as a removed one
// puts a wrong number in the section's most prominent line — the deleted-anchor
// row sorts above everything.
func TestPRReviewDeletedAnchorsRespectTheScanSet(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Distinct bodies: byte-identical files make git's rename pairing ambiguous,
	// which would make this test's rename case depend on which delete git happens
	// to pair the new file with.
	cite := func(tag string) string {
		return "package p\n\n// upholds INV-1 (" + tag + ").\nfunc F" + tag + "() {}\n"
	}

	git("init", "--quiet")
	// Rename detection must come from the flag, not from the reviewed repo.
	git("config", "diff.renames", "false")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write("go.mod", "module m\n\ngo 1.26.3\n")
	write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n")
	write("keep/keep.go", cite("Keep"))
	// None of these is ever scanned, so none is an anchor.
	write("vendor/dep/dep.go", cite("Vendored"))
	write("keep/testdata/fixture.go", cite("Fixture"))
	write("keep/_scratch.go", cite("Scratch"))
	write("moved/old.go", cite("Moved"))
	git("add", "-A")
	git("commit", "--quiet", "-m", "base")
	base := git("rev-parse", "HEAD")

	for _, p := range []string{"vendor", "keep/testdata", "keep/_scratch.go"} {
		if err := os.RemoveAll(filepath.Join(repo, p)); err != nil {
			t.Fatal(err)
		}
	}
	// A rename, which git would call a delete+add with renames off.
	write("moved/new.go", cite("Moved"))
	if err := os.Remove(filepath.Join(repo, "moved/old.go")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "delete unscanned files and rename one")
	head := git("rev-parse", "HEAD")

	out := filepath.Join(t.TempDir(), "bundle")
	cmd := exec.Command(bin, "pr-review", repo, base, head, "--out", out)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	md, err := os.ReadFile(filepath.Join(out, "review.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(md), "deletes files that cited") {
		t.Errorf("unscanned deletions and a rename were reported as removed anchors:\n%s", md)
	}
}

// TestPRReviewFromSubdirectoryWithGitConfig: the report must not depend on the
// reviewed repository's git config or on where inside the checkout it was invoked.
// diff.relative makes git emit cwd-relative paths, which match nothing on the
// registry side, and the deleted-anchor line then disappears in silence.
func TestPRReviewFromSubdirectoryWithGitConfig(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	// Both defaults flipped, so a report that leans on either is caught.
	git("config", "diff.relative", "true")
	git("config", "diff.renames", "false")
	write("go.mod", "module m\n\ngo 1.26.3\n")
	write("docs/invariants.md", "### INV-1: One\n\n**Statement:** First.\n")
	write("sub/keep.go", "package sub\n\n// upholds INV-1 here.\nfunc Keep() {}\n")
	write("sub/gone.go", "package sub\n\n// upholds INV-1 there too.\nfunc Gone() {}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "base")
	base := git("rev-parse", "HEAD")

	if err := os.Remove(filepath.Join(repo, "sub/gone.go")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "delete an anchor")
	head := git("rev-parse", "HEAD")

	// Invoked from a subdirectory, which the command documents as supported.
	out := filepath.Join(t.TempDir(), "bundle")
	cmd := exec.Command(bin, "pr-review", ".", base, head, "--out", out)
	cmd.Dir = filepath.Join(repo, "sub")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	md, err := os.ReadFile(filepath.Join(out, "review.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "deletes files that cited `INV-1` (1)") {
		t.Errorf("the deleted anchor went unreported from a subdirectory:\n%s", md)
	}
	if !strings.Contains(string(md), "| `INV-1` | LINKED | 0 of 1 |") {
		t.Errorf("counts are wrong from a subdirectory:\n%s", md)
	}
}

// TestWorktreeCleanupOnFatal is issue #71: commands that analyse an old commit
// create a throwaway checkout with `git worktree add`, and every error path calls
// fatal() — which is os.Exit, so the deferred cleanup never runs. Each failure left
// a temp directory on disk and an entry registered in the *target* repo's
// .git/worktrees, which `git worktree prune` will not clear while the directory
// exists.
//
// TMPDIR is redirected into the test's own directory, so the assertion is "this
// directory is empty" rather than a before/after comparison against the shared
// system temp dir. That removes three problems at once: no dependence on however
// many stale archon-wt-* directories a machine already holds, no flake when another
// archon runs concurrently, and nothing left behind when the test itself fails.
func TestWorktreeCleanupOnFatal(t *testing.T) {
	bin := buildArchon(t)
	repo := t.TempDir()
	tmpdir := t.TempDir()

	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// mustFail runs archon with TMPDIR redirected and requires a non-zero exit.
	mustFail := func(t *testing.T, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "TMPDIR="+tmpdir)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			t.Fatalf("%v unexpectedly succeeded; this test needs it to fail", args)
		}
		// The whole test turns vacuous if a command starts failing *before* the
		// checkout is made — nothing under test would run and everything would
		// still pass. The temp path in the message is the proof that the directory
		// existed by the time the process gave up, whatever the error says.
		if got := stderr.String(); !strings.Contains(got, "archon-wt-") {
			t.Fatalf("%v failed before the worktree existed, so it proves nothing:\n%s", args, got)
		}
		return stderr.String()
	}
	// Only archon's own worktree directories. Asserting the whole redirected TMPDIR
	// is empty would report an unrelated temp file as a leaked worktree, which is
	// the sort of misleading message this change exists to remove.
	leftovers := func() []string {
		t.Helper()
		entries, err := os.ReadDir(tmpdir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "archon-wt-") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		return names
	}
	worktreeState := func() (string, []string) {
		t.Helper()
		entries, _ := os.ReadDir(filepath.Join(repo, ".git", "worktrees"))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		return git("worktree", "list"), names
	}

	git("init", "--quiet")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	write("go.mod", "module m\n\ngo 1.26.3\n")
	write("a.go", "package m\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "extractable")
	good := git("rev-parse", "HEAD")

	// Removing go.mod makes extraction fail — strictly after the checkout exists,
	// which is what the archon-wt- assertion above pins.
	if err := os.Remove(filepath.Join(repo, "go.mod")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "no go.mod")
	bad := git("rev-parse", "HEAD")

	wantList, wantEntries := worktreeState()

	for _, args := range [][]string{
		{"extract", repo, bad},
		{"evidence", repo, bad},
		{"health", repo, bad},
	} {
		for i := 0; i < 2; i++ {
			mustFail(t, args...)
		}
	}

	// checkoutWorktree's own failure: `git worktree add` rejects the commit after
	// os.MkdirTemp has already created the directory. This is the case a cleanup
	// registered at the call site, or after the add, still leaks.
	mustFail(t, "extract", repo, "0000000000000000000000000000000000000000")

	// Two worktrees in one command: delta extracts base then head, so the first
	// checkout succeeds and is cleaned up normally while the second fails. A registry
	// that honoured only one entry would leak the second.
	// What this pins is the leak, not idempotency: a second run of an already-fired
	// cleanup takes the "is not a working tree" exempt branch and prints nothing, so
	// the sync.Once guarding it has no observable failure mode to assert on. It stays
	// as insurance against a future cleanup that is not harmless to repeat.
	mustFail(t, "delta", repo, good, bad)

	if got := leftovers(); len(got) > 0 {
		t.Errorf("temp worktree directories leaked: %v", got)
	}
	gotList, gotEntries := worktreeState()
	if gotList != wantList {
		t.Errorf("git worktree list changed:\nbefore:\n%s\nafter:\n%s", wantList, gotList)
	}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Errorf(".git/worktrees entries leaked: before %v, after %v", wantEntries, gotEntries)
	}
}

// TestRunPendingCleanups covers the drain itself in-process: ordering, and that one
// cleanup panicking does not strand the others. Both are invisible to the
// subprocess test above, where the worktrees are siblings in a temp directory and
// order is cosmetic.
func TestRunPendingCleanups(t *testing.T) {
	cleanupMu.Lock()
	savedPending, savedDraining := pendingCleanups, draining
	pendingCleanups, draining = nil, false
	cleanupMu.Unlock()
	t.Cleanup(func() {
		cleanupMu.Lock()
		pendingCleanups, draining = savedPending, savedDraining
		cleanupMu.Unlock()
	})

	var order []string
	onFatal(func() { order = append(order, "a") })
	onFatal(func() { panic("a cleanup that dies must not take the others with it") })
	onFatal(func() { order = append(order, "c") })

	stderr := captureStderr(t, runPendingCleanups)

	// A cleanup that dies must leave a trace: otherwise a reader sees the original
	// error plus a leaked worktree and concludes the fix does not work.
	if !strings.Contains(stderr, "a cleanup panicked") {
		t.Errorf("the panic was swallowed silently:\n%s", stderr)
	}
	if !strings.Contains(stderr, "must not take the others with it") {
		t.Errorf("stderr should carry the panic value:\n%s", stderr)
	}

	// Newest first, and the panicking middle one skipped rather than fatal.
	if want := []string{"c", "a"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
	// Drained, so a second call is a no-op rather than a repeat.
	runPendingCleanups()
	if len(order) != 2 {
		t.Errorf("cleanups ran again on a second drain: %v", order)
	}
}

// TestRemoveWorktreeAlreadyGone: "is not a working tree" is the one git failure
// treated as success, because it means the entry is already unregistered. Getting
// this wrong would turn every ordinary cleanup into a false alarm.
func TestRemoveWorktreeAlreadyGone(t *testing.T) {
	repo := newGitRepo(t)
	orphan := filepath.Join(t.TempDir(), "archon-wt-notregistered")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() { removeWorktree(repo, orphan) })

	if strings.Contains(stderr, "could not unregister") {
		t.Errorf("a path git never registered should count as already removed:\n%s", stderr)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the directory should have been deleted, stat err = %v", err)
	}
}

// TestRemoveWorktreeUnregisterFailureStillDeletesTheDirectory pins the ordering this
// PR had backwards once: when `git worktree remove` fails, the directory must be
// deleted anyway.
//
// Why that direction: `git worktree prune` clears an admin entry only when its
// directory is *missing*. Measured on git 2.50.1 — entry plus directory survives
// prune, `prune --expire` and `git gc` indefinitely; entry alone is pruned at once.
// So deleting the directory leaves a stranded entry recoverable and `git gc` clears
// it unattended, while keeping it pins the leak forever.
func TestRemoveWorktreeUnregisterFailureStillDeletesTheDirectory(t *testing.T) {
	// Passing the repository's own main worktree makes git fail in validation,
	// before it touches anything: "is a main working tree", which is not the
	// exempt "is not a working tree". So git fails *and* leaves the directory —
	// the one combination that distinguishes deleting it from bailing out.
	t.Run("git fails and leaves the directory", func(t *testing.T) {
		repo := newGitRepo(t)
		stderr := captureStderr(t, func() { removeWorktree(repo, repo) })

		if !strings.Contains(stderr, "could not unregister") {
			t.Errorf("a non-exempt git failure must be reported:\n%s", stderr)
		}
		if !strings.Contains(stderr, "worktree prune") {
			t.Errorf("the message must name the remedy:\n%s", stderr)
		}
		if _, err := os.Stat(repo); !os.IsNotExist(err) {
			t.Errorf("the directory survived a non-exempt git failure, which pins the entry against prune; stat err = %v", err)
		}
	})

	// A read-only admin directory is the other reachable failure. It does not
	// distinguish the branches — git deletes the checkout before failing on the
	// entry — but it is the shape that happens in practice, so the end state is
	// worth pinning.
	t.Run("read-only admin directory", func(t *testing.T) {
		repo := newGitRepo(t)
		wt := filepath.Join(t.TempDir(), "archon-wt-forced")
		cmd := exec.Command("git", "worktree", "add", "--detach", "--quiet", wt, "HEAD")
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}
		admin := filepath.Join(repo, ".git", "worktrees")
		info, err := os.Stat(admin)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(admin, 0o500); err != nil {
			t.Skipf("cannot make %s read-only: %v", admin, err)
		}
		stderr := captureStderr(t, func() { removeWorktree(repo, wt) })
		_ = os.Chmod(admin, info.Mode())

		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Errorf("the directory must be gone afterwards, stat err = %v\nstderr:\n%s", err, stderr)
		}
	})
}

// newGitRepo makes a throwaway repository with one commit.
func newGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "--quiet", "-m", "one"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// captureStderr swaps os.Stderr for a pipe around f.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	f()
	os.Stderr = saved
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
