# Demo: End-to-End Golden Tests

Runnable examples for both Archon flows. Golden files are committed — if any
future PR changes Archon's output, the demo script catches it.

## Run all demos

```sh
go build -o archon-go .
ARCHON=./archon-go BLIS_REPO=/path/to/blis-repo ./demo/run-all.sh
```

Output:
```
=== Flow 1: PR Review (BLIS #1546) ===
  PASS: review.md matches golden
  PASS: review.json matches golden

=== Flow 2: Design Phase (kv-offload plan) ===
  PASS: plan compile deterministic
  PASS: dist vs empty repo
  PASS: dist vs partial repo
  PASS: delta --plan summary

=== Flow 3: BLIS Design-Phase Tracking (real PRs) ===
  PASS: flow3 plan compile
  PASS: flow3 dist at base (6)
  PASS: flow3 PR#1593 (6→6)
  PASS: flow3 PR#1594 (6→6)
  PASS: flow3 PR#1592 (6→6)
  PASS: flow3 PR#1595 (6→4)
  PASS: flow3 final dist (4)

=== Results: 16 passed, 0 failed ===
ALL DEMOS PASS
```

## Flow 1: PR Review

Runs `archon-go pr-review` on BLIS PR #1546 (saturation detector bank) and
diffs against committed golden output. Tests the full extraction → delta →
review pipeline.

**Requires:** `BLIS_REPO` env var pointing to a checkout of the BLIS repo.
Skip this flow by omitting `BLIS_REPO`.

## Flow 2: Design Phase (self-contained)

Tests the plan compilation and distance pipeline using a BLIS-style
kv-offload plan with fixture graphs:

1. Compile `.archon` → `plan.json` (must be deterministic)
2. Compute distance against empty repo (expected: 9)
3. Compute distance against partial implementation (expected: 2)
4. Run `delta --plan` (expected: "9 → 2 (OK)")
5. Slice, render, stats utilities

**Self-contained** — no external repo needed.

## Flow 3: BLIS Design-Phase Tracking (real PRs)

The full design→review workflow on real BLIS kv-offload PRs. Tracks plan
distance across 4 PRs and shows dist decreasing from 6→4 as holes are filled.

See [flow3-blis-design/README.md](flow3-blis-design/README.md) for the full
step-by-step walkthrough with observations.

**Requires:** `BLIS_REPO` env var. Tests: compile, initial dist, 4 PRs, final dist.

## Adding a new demo

1. Add input fixtures to the appropriate flow directory
2. Run archon to generate expected output
3. Add a `check` call to `run-all.sh`
4. Commit both input and expected output
