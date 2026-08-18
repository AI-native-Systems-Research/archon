package plan

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// TestExample_CompileAndDist demonstrates the full input/output of the hole
// compiler and distance function. Run with:
//
//	go test -v -run TestExample_CompileAndDist ./internal/plan/
func TestExample_CompileAndDist(t *testing.T) {
	// --- INPUT: .archon source file ---
	src, err := os.ReadFile("testdata/minimal.archon")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("INPUT (.archon source):\n%s", string(src))

	// --- COMPILE ---
	planGraph, diags := Compile(src)
	if len(diags) > 0 {
		for _, d := range diags {
			t.Errorf("  diagnostic: %s", d)
		}
		t.Fatal("compile failed")
	}

	planJSON, _ := json.MarshalIndent(planGraph, "", "  ")
	t.Logf("OUTPUT (compiled plan.json):\n%s", string(planJSON))

	// --- DIST against empty repo (nothing implemented yet) ---
	emptyRepo := &graph.Graph{}
	res := Dist(planGraph, emptyRepo)
	distJSON, _ := json.MarshalIndent(res, "", "  ")
	t.Logf("DIST (plan vs empty repo):\n%s", distJSON)
	t.Logf("VERDICT: dist = %d (C1=%d holes, C2=%d boxes, C3=%d arrows, C4=%d disallowed)",
		res.Total, res.C1, res.C2, res.C3, res.C4)

	// --- DIST after filling the hole ---
	filledRepo := &graph.Graph{
		Packages: []graph.Package{
			{Path: "example.com/m/existing", Files: []string{"existing.go"}},
			{Path: "example.com/m/newpkg", Files: []string{"newpkg.go"}, Surface: []graph.Symbol{
				{Kind: "func", Name: "Run"},
				{Kind: "func", Name: "Stop"},
			}},
		},
		Edges: []graph.Edge{
			{From: "example.com/m/newpkg", To: "example.com/m/existing", Kind: "import"},
			{From: "example.com/m/cmd", To: "example.com/m/newpkg", Kind: "import"},
		},
	}
	resAfter := Dist(planGraph, filledRepo)
	distAfterJSON, _ := json.MarshalIndent(resAfter, "", "  ")
	t.Logf("DIST (plan vs filled repo):\n%s", distAfterJSON)
	t.Logf("VERDICT: dist = %d (was %d → decreased by %d)", resAfter.Total, res.Total, res.Total-resAfter.Total)

	// --- ASSERTIONS ---
	if res.Total == 0 {
		t.Error("dist against empty repo should be > 0")
	}
	if resAfter.Total >= res.Total {
		t.Error("filling the hole should decrease dist")
	}
}
