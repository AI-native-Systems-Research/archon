// Package review builds a CI-friendly PR review bundle from two package-altitude
// graphs and their delta. It is the engine behind `archon-go pr-review`: one
// command that does the light thing by default (a one-line fast-track verdict)
// and escalates to the full architectural views only when a PR actually moves a
// boundary.
//
// Everything here is deterministic and uses no LLM: the same repo and commits
// always produce byte-identical output. The bundle is written to a directory
// (default .archon/) and is designed for `cat .archon/review.md >>
// $GITHUB_STEP_SUMMARY` — review.md leads with a GitHub-renderable Mermaid
// diagram and Markdown tables, review.json carries the machine-readable result,
// and the .dot/.png files are downloadable artifacts.
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
	"github.com/AI-native-Systems-Research/archon/internal/graph"
)

// Verdict is the tiered triage outcome. It is reported, never enforced:
// pr-review always exits 0 and the caller's CI decides whether BLOCK fails the
// check.
type Verdict string

const (
	// Block: the PR introduced a dependency its box's allow-list forbids.
	Block Verdict = "BLOCK"
	// ReviewArchitecture: a package boundary moved — needs an architecture pass.
	ReviewArchitecture Verdict = "REVIEW_ARCHITECTURE"
	// ReviewInvariants: boundary is empty, but a guarded promise (invariant or
	// wire/DB schema) was touched — needs an invariant pass.
	ReviewInvariants Verdict = "REVIEW_INVARIANTS"
	// FastTrack: internal-only at the package altitude — fast-track eligible.
	FastTrack Verdict = "FAST_TRACK"
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
	NoPNG  bool   // skip shelling out to `dot` for PNGs
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
	WitnessesFull   int `json:"witnessesFullyDecoupled"`
	WitnessesWeak   int `json:"witnessesPartiallyDecoupled"`
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

	res.Components = buildComponents(gB, d, depth)
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

	res.Verdict, res.Summary = verdict(d)
	return res
}

// verdict applies the tiered triage. Report-only: the caller still exits 0.
func verdict(d *delta.Delta) (Verdict, string) {
	for _, v := range d.ContractViolations {
		if v.Introduced {
			return Block, "BLOCK — this PR introduces a dependency the box's allow-list forbids."
		}
	}
	if !d.EmptyAtPackageAltitude {
		return ReviewArchitecture, "REVIEW_ARCHITECTURE — a package boundary moved; an architecture pass is required."
	}
	if len(d.Invariants) > 0 || len(d.SchemaChanges) > 0 {
		return ReviewInvariants, "REVIEW_INVARIANTS — empty boundary, but a guarded promise (invariant or schema) changed; review that promise."
	}
	return FastTrack, "✓ No architectural change. Internal-only — fast-track eligible."
}

// WriteBundle writes review.md, review.json, and the .mmd/.dot/(.png) artifacts
// into outdir (created if needed). It records the written files in res.Artifacts.
func WriteBundle(res *Result, outdir string, opts Options) error {
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outdir, err)
	}

	// Artifact files are only emitted when the change is architectural — a
	// fast-track PR gets just review.md + review.json.
	architectural := res.Verdict == ReviewArchitecture || res.Verdict == Block

	var artifacts []string
	write := func(name, content string) error {
		p := filepath.Join(outdir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		artifacts = append(artifacts, name)
		return nil
	}

	if architectural {
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
		if !opts.NoPNG && dotAvailable() {
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

	// review.md and review.json list the artifacts written so far.
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
