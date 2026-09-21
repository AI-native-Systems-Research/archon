package invariant

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestExample_ParseAndLink shows the whole package end to end: a registry goes
// in, a link table comes out. Run with:
//
//	go test -v -run TestExample_ParseAndLink ./internal/invariant/
func TestExample_ParseAndLink(t *testing.T) {
	const registry = "testdata/repo-registry.md"

	// --- INPUT: the declared registry ---
	src, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("INPUT (registry %s):\n%s", registry, src)

	// --- INPUT: the repository being linked ---
	t.Logf("INPUT (repository %s):\n%s", fixtureRepo, tree(t, fixtureRepo))

	// --- PARSE ---
	invs, err := ParseFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, inv := range invs {
		b.WriteString("  " + inv.ID + "  tier=" + inv.Tier + "  source=" + inv.Source + "\n")
		b.WriteString("      " + inv.Statement + "\n")
	}
	t.Logf("PARSED (%d entries, scope %q):\n%s", len(invs), invs[0].Scope, b.String())

	// --- LINK ---
	links, _, err := LinkRepo(fixtureRepo, invs)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	b.WriteString("  ID        STATUS     CODE  TEST  CITES  NAMED TESTS\n")
	for _, l := range links {
		fmt.Fprintf(&b, "  %-10s%-11s%-6d%-6d%-7d%s\n", l.Invariant.ID, l.Status(),
			len(l.CodeFiles), len(l.TestFiles), l.Citations, strings.Join(l.NamedTests, ", "))
	}
	t.Logf("LINK TABLE:\n%s", b.String())

	totals := Count(links)
	t.Logf("TOTALS: %d LINKED, %d TEST ONLY, %d UNLINKED (of %d declared)",
		totals.Linked, totals.TestOnly, totals.Unlinked, len(links))

	one, _ := json.MarshalIndent(links[0], "", "  ")
	t.Logf("ONE LINK AS JSON:\n%s", one)

	if got := (Totals{Linked: 3, TestOnly: 2, Unlinked: 1}); totals != got {
		t.Errorf("totals = %+v, want %+v", totals, got)
	}
}

func tree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			b.WriteString("  " + e.Name() + "\n")
			continue
		}
		sub, err := os.ReadDir(root + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sub {
			b.WriteString("  " + e.Name() + "/" + s.Name() + "\n")
		}
	}
	return b.String()
}
