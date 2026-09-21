package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
		if !strings.Contains(md, "`INV-99` is declared but cited in no file") {
			t.Errorf("an uncited invariant is a finding:\n%s", md)
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
