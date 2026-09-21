package main

import (
	"encoding/json"
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
		for _, want := range []string{"DECLARED INVARIANTS", "invariants.md", "INV-1", "LINKED", "UNLINKED", "anchored"} {
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
				Linked, TestOnly, Unlinked int
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
		for _, l := range res.Links {
			if l.Status == "" {
				t.Errorf("%s has no status field", l.Invariant.ID)
			}
			// The scope is the registry as asked for, never a temp worktree.
			if strings.HasPrefix(l.Invariant.Scope, "/") {
				t.Errorf("%s scope is absolute: %q", l.Invariant.ID, l.Invariant.Scope)
			}
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

	t.Run("a bad --at fails loudly", func(t *testing.T) {
		_, stderr, code := runInv(t, bin, ".", "docs/invariants.md", "--at", "definitely-not-a-commit")
		if code == 0 {
			t.Error("exit 0 for a commit that does not exist")
		}
		if stderr == "" {
			t.Error("a bad commit should say something on stderr")
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
