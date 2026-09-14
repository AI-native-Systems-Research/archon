package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildTool compiles the command so the flag table and the exit codes are
// exercised the way a user meets them, through main.
func buildTool(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "callgraph")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// run invokes the tool against this repository, whose root is two levels up.
func run(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	return runIn(t, bin, "../..", "./internal/graph/...", args...)
}

func runIn(t *testing.T, bin, dir, pattern string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{dir, pattern}, args...)...)
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

func TestModeFlag(t *testing.T) {
	bin := buildTool(t)

	t.Run("bare --mode does not fall through to static", func(t *testing.T) {
		// The whole point of the flag is to stop producing the graph with the
		// interface calls missing, so accepting it silently is the worst answer.
		stdout, stderr, code := run(t, bin, "--mode")
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
		_, stderr, code := run(t, bin, "--mode=CHA")
		if code != 2 || !strings.Contains(stderr, "unknown mode") {
			t.Errorf("exit %d, stderr %q; want exit 2 and an unknown-mode message", code, stderr)
		}
	})

	t.Run("rta on a library names the mode that works", func(t *testing.T) {
		_, stderr, code := run(t, bin, "--mode=rta")
		if code != 1 {
			t.Errorf("exit code %d, want 1; stderr %q", code, stderr)
		}
		if !strings.Contains(stderr, "cha") {
			t.Errorf("stderr should point at cha, got %q", stderr)
		}
	})

	t.Run("cha says so in the graph and in the summary", func(t *testing.T) {
		stdout, stderr, code := run(t, bin, "--mode=cha")
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
		stdout, stderr, code := runIn(t, bin, "../../internal/callgraph/testdata/iface", "./...", "--mode=cha")
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
		stdout, _, code := run(t, bin)
		if code != 0 {
			t.Fatal(code)
		}
		if strings.Contains(stdout, "style=dashed") || strings.Contains(stdout, "calls through an interface") {
			t.Error("static mode resolves no dispatch, so it must draw none")
		}
	})

	t.Run("a package that does not type-check is reported", func(t *testing.T) {
		_, stderr, _ := runIn(t, bin, "../../internal/callgraph/testdata/illtyped", "./...", "--mode=cha")
		if !strings.Contains(stderr, "did not type-check") || !strings.Contains(stderr, "cause ") {
			t.Errorf("the warning is not wired up: %q", stderr)
		}
	})

	t.Run("interface calls that produced no edge are reported", func(t *testing.T) {
		_, stderr, _ := runIn(t, bin, "../../internal/callgraph/testdata/iface", "./...", "--mode=cha")
		if !strings.Contains(stderr, "produced no edge") || !strings.Contains(stderr, "method value") {
			t.Errorf("the report is not wired up: %q", stderr)
		}
	})

	t.Run("default is static, and repeats byte for byte", func(t *testing.T) {
		first, stderr, code := run(t, bin)
		if code != 0 {
			t.Fatalf("exit code %d; stderr %q", code, stderr)
		}
		if strings.Contains(first, "interface calls resolved") {
			t.Error("the default mode should not claim to resolve interface calls")
		}
		second, _, _ := run(t, bin)
		if first != second {
			t.Error("two runs on identical input produced different output")
		}
	})
}
