package plan

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCompile_Minimal(t *testing.T) {
	src, err := os.ReadFile("testdata/minimal.archon")
	if err != nil {
		t.Fatal(err)
	}
	g, diags := Compile(src)
	if len(diags) > 0 {
		for _, d := range diags {
			t.Errorf("diagnostic: %s", d)
		}
		t.Fatal("compile failed")
	}

	// Should have 2 packages: existing (box) and newpkg (hole)
	if len(g.Packages) != 2 {
		t.Fatalf("want 2 packages, got %d", len(g.Packages))
	}

	var hole, box bool
	for _, p := range g.Packages {
		if p.Path == "example.com/m/newpkg" {
			hole = true
			if !p.Hole {
				t.Error("newpkg should be marked as Hole")
			}
			if len(p.Surface) != 2 {
				t.Errorf("newpkg surface: want 2, got %d", len(p.Surface))
			}
			if len(p.Allow) != 1 || p.Allow[0] != "example.com/m/existing" {
				t.Errorf("newpkg allow: %v", p.Allow)
			}
		}
		if p.Path == "example.com/m/existing" {
			box = true
			if p.Hole {
				t.Error("existing should NOT be a hole")
			}
		}
	}
	if !hole || !box {
		t.Errorf("missing packages: hole=%v box=%v", hole, box)
	}

	// Should have edges: newpkg->existing (import from allow) + cmd->newpkg (arrow)
	if len(g.Edges) < 2 {
		t.Errorf("want at least 2 edges, got %d", len(g.Edges))
	}
}

func TestCompile_Determinism(t *testing.T) {
	src, err := os.ReadFile("testdata/minimal.archon")
	if err != nil {
		t.Fatal(err)
	}
	g1, d1 := Compile(src)
	g2, d2 := Compile(src)
	if len(d1) > 0 || len(d2) > 0 {
		t.Fatal("compile failed")
	}
	j1, _ := json.Marshal(g1)
	j2, _ := json.Marshal(g2)
	if string(j1) != string(j2) {
		t.Error("compile is not deterministic")
	}
}

func TestCompile_UndeclaredInvariant(t *testing.T) {
	src := []byte(`
hole example.com/m/pkg {
  surface:
    Foo() error
  cites:
    invariant nonexistent
}
`)
	_, diags := Compile(src)
	if len(diags) == 0 {
		t.Fatal("expected compile error for undeclared invariant")
	}
	found := false
	for _, d := range diags {
		if contains(d.Message, "undeclared invariant") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'undeclared invariant' diagnostic, got: %v", diags)
	}
}

func TestCompile_MalformedArrow(t *testing.T) {
	src := []byte(`arrow broken line`)
	_, diags := Compile(src)
	if len(diags) == 0 {
		t.Fatal("expected compile error for malformed arrow")
	}
}

func TestCompile_EmptyInput(t *testing.T) {
	g, diags := Compile([]byte(""))
	if len(diags) > 0 {
		t.Fatal("empty input should not error")
	}
	if len(g.Packages) != 0 {
		t.Errorf("empty input should produce empty graph, got %d packages", len(g.Packages))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
