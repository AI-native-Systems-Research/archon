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

=== Results: 6 passed, 0 failed ===
ALL DEMOS PASS
```

## Flow 1: PR Review

Runs `archon-go pr-review` on BLIS PR #1546 (saturation detector bank) and
diffs against committed golden output. Tests the full extraction → delta →
review pipeline.

**Requires:** `BLIS_REPO` env var pointing to a checkout of the BLIS repo.
Skip this flow by omitting `BLIS_REPO`.

## Flow 2: Design Phase

Tests the plan compilation and distance pipeline using a real BLIS-style
kv-offload plan:

1. Compile `.archon` → `plan.json` (must be deterministic)
2. Compute distance against empty repo (expected: 9)
3. Compute distance against partial implementation (expected: 2)
4. Run `delta --plan` (expected: "9 → 2 (OK)")

**Self-contained** — no external repo needed.

## Adding a new demo

1. Add input fixtures to the appropriate flow directory
2. Run archon to generate expected output
3. Add a `check` call to `run-all.sh`
4. Commit both input and expected output
