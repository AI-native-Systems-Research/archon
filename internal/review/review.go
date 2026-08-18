// Package review builds a CI-friendly PR review bundle from two package-altitude
// graphs and their delta. It is the engine behind `archon-go pr-review`: one
// command that does the light thing by default (a one-line fast-track verdict)
// and escalates to the full architectural views only when a PR actually moves a
// boundary.
//
// Everything here is deterministic and uses no LLM: the same repo and commits
// always produce byte-identical output. The bundle is written to a directory
// (default .archon/) and is designed for `cat .archon/review.md >>
// $GITHUB_STEP_SUMMARY`. By default it is exactly two self-contained files:
// review.md — which embeds every view inline as GitHub-renderable Mermaid plus
// Markdown tables — and review.json, the machine-readable result. The separate
// .mmd/.dot/.md sources and PNGs are only written with --emit-artifacts.
//
// This package deliberately re-implements the three reviewer views (component,
// witness, contract) in Go — the Python reviewer/ scripts remain the interactive
// human path — so that CI needs nothing but the single archon-go binary and no
// working-tree checkout (each commit is read through an ephemeral git worktree,
// and the contract view is derived from the delta, not from `consumes`).
package review

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AI-native-Systems-Research/archon/internal/delta"
	"github.com/AI-native-Systems-Research/archon/internal/gate"
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Verdict is the triage outcome. There are exactly two: either the PR moved a
// package boundary (an architectural change that needs a review) or it did not
// (fast-track eligible). It is reported, never enforced — pr-review always exits
// 0 and the caller's CI decides what to do with the verdict.
type Verdict string

const (
	// NoChange: nothing moved at the package altitude — fast-track eligible.
	// A touched guarded promise (invariant / schema) alone does NOT flip this;
	// the detail is still carried in review.json.
	NoChange Verdict = "NO_CHANGE"
	// ArchitecturalChange: a package boundary moved (a dependency, interface, or
	// package was added/removed) — an architecture review is required.
	ArchitecturalChange Verdict = "ARCHITECTURAL_CHANGE"
)

// Four-color scheme shared by every view (matches the reviewer Python scripts
// and internal/render): green added, red removed, blue modified, grey unchanged.
const (
	colAdded     = "#1a7f37"
	colRemoved   = "#cf222e"
	colModified  = "#0969da"
	colUnchanged = "#57606a"
	colFill      = "#eef3fb"
)

// Options configure a pr-review run.
type Options struct {
	Repo   string // repo path or a label for the source
	Base   string // base commit (label only; extraction happens in main)
	Head   string // head commit
	Out    string // output bundle directory (default .archon)
	LabelA string // human label for base (defaults to Base)
	LabelB string // human label for head (defaults to Head)
	Depth  int    // component grouping depth (default 2)

	// Fixed is the set of package paths whose public surface must not grow.
	// When non-nil, the surface gate (G3) checks for unauthorized widenings.
	Fixed map[string]bool

	// EmitArtifacts also writes the separate .mmd/.dot/.md source files and, if
	// `dot` is on PATH, PNGs. Off by default: review.md embeds every diagram
	// inline (as Mermaid), so the bundle is self-contained without them.
	EmitArtifacts bool
}

// Counts is the at-a-glance tally embedded in review.json.
type Counts struct {
	PackagesAdded   int `json:"packagesAdded"`
	PackagesRemoved int `json:"packagesRemoved"`
	EdgesAdded      int `json:"edgesAdded"`
	EdgesRemoved    int `json:"edgesRemoved"`
	SurfaceChanged  int `json:"surfaceChanged"`
	SchemaChanged   int `json:"schemaChanged"`
	Invariants      int `json:"invariants"`
	Contracts       int `json:"contracts"`
	Violations      int `json:"violations"`
	WitnessesFull      int `json:"witnessesFullyDecoupled"`
	WitnessesWeak      int `json:"witnessesPartiallyDecoupled"`
	SurfaceWidenings   int `json:"surfaceWidenings,omitempty"`
}

// Result is the review.json schema (archon.pr-review/v1). It is fully
// serializable and self-contained: a CI step can read it without re-running
// ARCHON.
type Result struct {
	Schema string `json:"schema"`
	Repo   string `json:"repo,omitempty"`
	Base   string `json:"base"`
	Head   string `json:"head"`
	LabelA string `json:"labelA"`
	LabelB string `json:"labelB"`

	Verdict Verdict `json:"verdict"`
	Summary string  `json:"summary"`

	EmptyAtPackageAltitude bool   `json:"emptyAtPackageAltitude"`
	Counts                 Counts `json:"counts"`

	// Raw delta axes (machine-readable; the Markdown tables are derived from
	// these).
	Invariants []delta.InvariantChange `json:"invariants,omitempty"`
	Schema_    []delta.SurfaceChange   `json:"schemaChanges,omitempty"`
	Surface    []delta.SurfaceChange   `json:"surface,omitempty"`
	Contracts  []delta.ContractChange  `json:"contracts,omitempty"`
	Violations []delta.Violation       `json:"violations,omitempty"`
	Widenings  []gate.Widening        `json:"widenings,omitempty"`

	// Higher-altitude views (computed here).
	Components ComponentView `json:"components"`
	Witnesses  []WitnessRow  `json:"witnesses,omitempty"`

	// Artifacts lists the files written into the bundle directory (relative
	// paths), for a CI step that uploads them.
	Artifacts []string `json:"artifacts,omitempty"`
}

// Build computes the verdict and all view data from the two graphs and their
// delta. gA/gB are the base/head graphs; d is delta.Compute(gA, gB) with
// CheckContract already applied if an allow-list was given.
func Build(gA, gB *graph.Graph, d *delta.Delta, opts Options) *Result {
	labelA := opts.LabelA
	if labelA == "" {
		labelA = firstNonEmpty(opts.Base, "base")
	}
	labelB := opts.LabelB
	if labelB == "" {
		labelB = firstNonEmpty(opts.Head, "head")
	}
	depth := opts.Depth
	if depth < 1 {
		depth = 2
	}

	res := &Result{
		Schema:                 "archon.pr-review/v1",
		Repo:                   opts.Repo,
		Base:                   opts.Base,
		Head:                   opts.Head,
		LabelA:                 labelA,
		LabelB:                 labelB,
		EmptyAtPackageAltitude: d.EmptyAtPackageAltitude,
		Invariants:             d.Invariants,
		Schema_:                d.SchemaChanges,
		Surface:                d.Surface,
		Contracts:              d.Contracts,
		Violations:             d.ContractViolations,
	}

	res.Components = buildComponents(gA, gB, d, depth)
	res.Witnesses = buildWitnesses(gA, gB)

	res.Counts = Counts{
		PackagesAdded:   len(d.PackagesAdded),
		PackagesRemoved: len(d.PackagesRemoved),
		EdgesAdded:      len(d.EdgesAdded),
		EdgesRemoved:    len(d.EdgesRemoved),
		SurfaceChanged:  len(d.Surface),
		SchemaChanged:   len(d.SchemaChanges),
		Invariants:      len(d.Invariants),
		Contracts:       len(d.Contracts),
		Violations:      len(d.ContractViolations),
	}
	for _, w := range res.Witnesses {
		switch w.Status {
		case wsRemoved:
			res.Counts.WitnessesFull++
		case wsWeakened:
			res.Counts.WitnessesWeak++
		}
	}

	if len(opts.Fixed) > 0 {
		res.Widenings = gate.CheckSurface(d.Surface, opts.Fixed)
		res.Counts.SurfaceWidenings = len(res.Widenings)
	}

	res.Verdict, res.Summary = verdict(d)
	return res
}

// verdict is the binary triage: a package boundary either moved or it didn't.
// Report-only — the caller still exits 0. A touched invariant/schema alone does
// not flip the verdict (the boundary is what matters); that detail rides along
// in review.json regardless.
func verdict(d *delta.Delta) (Verdict, string) {
	if !d.EmptyAtPackageAltitude {
		return ArchitecturalChange, "Architectural change — a package boundary moved; an architecture review is required."
	}
	return NoChange, "✓ No architectural change. Internal-only — fast-track eligible."
}

// WriteBundle writes the review bundle into outdir (created if needed). By
// default the bundle is exactly two self-contained files — review.md (every
// diagram embedded inline as Mermaid) and review.json. The separate
// .mmd/.dot/.md sources and PNGs are only written when opts.EmitArtifacts is set
// (a convenience for anyone who wants high-res pictures or Graphviz sources).
func WriteBundle(res *Result, outdir string, opts Options) error {
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outdir, err)
	}

	var artifacts []string
	write := func(name, content string) error {
		p := filepath.Join(outdir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		artifacts = append(artifacts, name)
		return nil
	}

	// Optional extra artifacts. review.md already embeds all three views inline,
	// so these are off by default; they only make sense for an architectural PR.
	if opts.EmitArtifacts && res.Verdict == ArchitecturalChange {
		if err := write("component.mmd", res.Components.Mermaid); err != nil {
			return err
		}
		if err := write("component.dot", res.Components.DOT); err != nil {
			return err
		}
		if err := write("witness.dot", witnessDOT(res.Witnesses, res.LabelA, res.LabelB)); err != nil {
			return err
		}
		if err := write("contract.md", contractMarkdown(res.Contracts)); err != nil {
			return err
		}
		// Only emit a contract graph when there is a membership change to draw.
		haveContractGraph := len(res.Contracts) > 0
		if haveContractGraph {
			if err := write("contract.dot", contractDOT(res.Contracts)); err != nil {
				return err
			}
		}
		if dotAvailable() {
			pngJobs := []struct{ dot, png string }{
				{"component.dot", "component.png"},
				{"witness.dot", "witness.png"},
			}
			if haveContractGraph {
				pngJobs = append(pngJobs, struct{ dot, png string }{"contract.dot", "contract.png"})
			}
			for _, c := range pngJobs {
				if err := renderPNG(filepath.Join(outdir, c.dot), filepath.Join(outdir, c.png)); err == nil {
					artifacts = append(artifacts, c.png)
				}
			}
		}
	}

	// review.md and review.json list any artifacts written so far.
	sort.Strings(artifacts)
	res.Artifacts = artifacts

	md := renderMarkdown(res)
	if err := write("review.md", md); err != nil {
		return err
	}

	jsonBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal review.json: %w", err)
	}
	if err := write("review.json", string(jsonBytes)+"\n"); err != nil {
		return err
	}

	sort.Strings(artifacts)
	res.Artifacts = artifacts
	return nil
}

func dotAvailable() bool {
	_, err := exec.LookPath("dot")
	return err == nil
}

func renderPNG(dotPath, pngPath string) error {
	data, err := os.ReadFile(dotPath)
	if err != nil {
		return err
	}
	cmd := exec.Command("dot", "-Tpng", "-o", pngPath)
	cmd.Stdin = strings.NewReader(string(data))
	return cmd.Run()
}

// rel strips the module prefix from an import path, matching the Python
// reviewer scripts' rel(). The module path itself becomes "".
func rel(module, path string) string {
	if path == module {
		return ""
	}
	if module != "" && strings.HasPrefix(path, module+"/") {
		return strings.TrimPrefix(path, module+"/")
	}
	return path
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
