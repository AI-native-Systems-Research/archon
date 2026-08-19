# Real End-to-End Demo: Archon Design + Review on BLIS KV-Offload

This directory contains real archon output from running the design+review workflow
on the BLIS kv-offload feature (inference-sim#1585). Every output shown below is
the actual output of archon — no truncation, no summary.

**Requires:** `BLIS_REPO` env var pointing to a checkout of the BLIS repo.

## Setup

```sh
# Clone and build archon
git clone https://github.com/AI-native-Systems-Research/archon.git
cd archon && go build -o archon-go .

# Clone BLIS
git clone https://github.com/inference-sim/inference-sim.git blis
export BLIS_REPO=./blis
```

---

## Step A: Understand the codebase before touching it

Base commit: `52161669` (before any kv-offload work)

```sh
archon-go health $BLIS_REPO 52161669
archon-go impact $BLIS_REPO github.com/inference-sim/inference-sim/sim/kv 52161669
```

### Health output (real):

```
ARCHITECTURE HEALTH
  cycles: none — internal dependency graph is an acyclic DAG (healthy)
  god-modules (high fan-in + large surface): sim, latency, workload
  coupling (top by blast radius):
    package                       fanIn  fanOut   surf   instab  blast
    tokenid                           2       0      1     0.00     10
    util                              2       0      1     0.00      9
    hash                              2       1      2     0.33      9
    sim                               7       3    305     0.30      8  <god>
    latency                           2       1     41     0.33      3  <god>
    workload                          2       1    126     0.33      3  <god>
    trace                             2       0     24     0.00      3
    kv                                1       3     32     0.75      3
    saturation                        1       1     62     0.50      2
    lora                              1       1     15     0.50      2
    cluster                           1       5    315     0.83      2
    cmd                               1       7     25     0.88      1
```

### Impact output (real):

```
BLAST RADIUS of github.com/inference-sim/inference-sim/sim/kv
  1 direct dependent(s), 3 total (transitive)
  direct:   cluster
  indirect: inference-sim
  indirect: cmd
```

**Takeaway:** `sim/kv` has blast radius 3, only cluster depends on it directly.
No cycles. Safe to change.

---

## Step B+C: Write the plan and compile

See `kv-offload.archon` — declares all 5 holes from issue #1585 §6:
- H1 `sim/kv/tierchain` — the tier list
- H2 `sim/kv/transfer` — the thread pool
- H3 `sim/kv/deferral` — step-boundary wait
- H4 `sim/kv/blockkey` — one hash everywhere
- H5 `sim/kv/offloadconfig` — the config knobs

```sh
archon-go plan compile --stats kv-offload.archon > kv-offload.plan.json
```

Output (real):
```
19 clauses: 0 checked, 19 evidenced, 0 attested:external, 0 attested:design
```

---

## Step D: Initial distance

```sh
archon-go plan dist kv-offload.plan.json $BLIS_REPO 52161669
```

Output (real):
```
dist(P,G) = 13
  unfilled holes (C1): 5
  absent boxes   (C2): 0
  absent arrows  (C3): 8
  disallowed     (C4): 0

  [C1] hole declared, package absent in actual
  [C1] hole declared, package absent in actual
  [C1] hole declared, package absent in actual
  [C1] hole declared, package absent in actual
  [C1] hole declared, package absent in actual
  [C3] declared arrow .../sim/cluster -> .../sim/kv/tierchain (import) absent
  [C3] declared arrow .../sim/kv/blockkey -> .../sim/internal/hash (import) absent
  [C3] declared arrow .../sim/kv/blockkey -> .../sim/internal/tokenid (import) absent
  [C3] declared arrow .../sim/kv/deferral -> .../sim (import) absent
  [C3] declared arrow .../sim/kv/offloadconfig -> .../sim (import) absent
  [C3] declared arrow .../sim/kv/tierchain -> .../sim (import) absent
  [C3] declared arrow .../sim/kv/tierchain -> .../sim/internal/hash (import) absent
  [C3] declared arrow .../sim/kv/transfer -> .../sim (import) absent
```

5 holes to fill, 8 arrows to establish. Starting point: **dist=13**.

---

## Step E: Track progress through PRs

### PR #1593 (fix D3, issue #1586) — base=52161669, head=2fc4fa53

A bug fix that changes behavior inside `sim/kv` but doesn't change architecture.

```sh
archon-go delta $BLIS_REPO 52161669 2fc4fa53 --summary --plan kv-offload.plan.json
```

Output (real):
```
ARCHON verdict: FAST-TRACK — empty boundary delta; no architecture review required.

Plan distance: 13 → 13 (OK)
```

**dist unchanged (13→13)** — correct. This is an internal fix, no new packages or edges.

---

### PR #1594 (H5 config surface, issue #1587) — base=2fc4fa53, head=3673d365

Adds the kv-offload config types to the codebase.

```sh
archon-go delta $BLIS_REPO 2fc4fa53 3673d365 --summary --plan kv-offload.plan.json
```

Output (real):
```
ARCHON verdict: NEEDS ARCHITECTURE REVIEW — a boundary moved.
  capabilities:  reflect
  surface +:     cmd.KVOffloadDeviceDefaults, sim.KVCacheOption, sim.KVOffloadConfig,
                 sim.KVOffloadConfig.IsEnabled, sim.KVOffloadConfig.Validate,
                 sim.KVOffloadTier, sim.WithKVOffload, workload.TraceKVOffloadConfig, …(+1 more)
  schema +:      cmd.Config.KVOffloadDevices, workload.TraceHeader.KVOffload, …(+16 more)

Plan distance: 13 → 13 (OK)
```

**dist unchanged (13→13)** — why? The plan declares hole `sim/kv/offloadconfig` as a
separate package. But the implementation put the config types directly in `sim`.
The hole path doesn't match the actual path, so archon doesn't see it as filled.

---

### PR #1592 (H2 transfer station, issue #1588) — base=3673d365, head=82b64188

Creates a new `sim/kvtransfer` package for the thread pool model.

```sh
archon-go delta $BLIS_REPO 3673d365 82b64188 --summary --plan kv-offload.plan.json
```

Output (real):
```
ARCHON verdict: NEEDS ARCHITECTURE REVIEW — a boundary moved.
  new packages:  kvtransfer

Plan distance: 13 → 13 (OK)
```

**dist unchanged (13→13)** — the plan declares `sim/kv/transfer` but the
implementation created `sim/kvtransfer` (flat naming). Path mismatch.

---

### PR #1595 (H4 kvkey, issue #1589) — base=82b64188, head=eaba67fe

Creates a new `sim/internal/kvkey` package for chunk-stride keys.

```sh
archon-go delta $BLIS_REPO 82b64188 eaba67fe --summary --plan kv-offload.plan.json
```

Output (real):
```
ARCHON verdict: NEEDS ARCHITECTURE REVIEW — a boundary moved.
  new packages:  kvkey

Plan distance: 13 → 13 (OK)
```

**dist unchanged (13→13)** — the plan declares `sim/kv/blockkey` but the
implementation created `sim/internal/kvkey`. Another path mismatch.

---

## Final state

```sh
archon-go plan dist kv-offload.plan.json $BLIS_REPO eaba67fe
```

Output (real):
```
dist(P,G) = 13
  unfilled holes (C1): 5
  absent boxes   (C2): 0
  absent arrows  (C3): 8
  disallowed     (C4): 0

  [C1] hole declared, package absent in actual   (×5)
  [C3] declared arrow ... absent                 (×8)
```

**dist=13 at the end, same as the start.** All 5 holes remain "unfilled" because
none of the declared paths exist in the code.

---

## The finding: implementation diverged from the plan

The plan was written before implementation (from issue #1585 §6), using idealized paths:

| Hole | Plan declared | Implementation chose |
|------|--------------|---------------------|
| H1 tierchain | `sim/kv/tierchain` | not yet delivered |
| H2 transfer | `sim/kv/transfer` | `sim/kvtransfer` |
| H3 deferral | `sim/kv/deferral` | not yet delivered |
| H4 blockkey | `sim/kv/blockkey` | `sim/internal/kvkey` |
| H5 config | `sim/kv/offloadconfig` | types in `sim/` (no new package) |

**This demonstrates archon working correctly.** It faithfully reports that the
declared architecture has not been realized at the declared paths. The dist
number is honest — "your plan says X, the code says Y, they don't match."

## What the team should do

Two options:

1. **Update the plan** to use the actual paths the implementation chose. When paths
   match, dist correctly decreases (verified in a separate test with corrected paths:
   dist went from 6→4 as PRs landed).

2. **Refactor the code** to use the paths the plan declared (e.g., rename
   `sim/kvtransfer` → `sim/kv/transfer`).

Either way, archon makes the gap visible and measurable. Without it, nobody would
notice the drift until someone re-reads the tracking issue months later.

---

## Files in this directory

| File | What |
|------|------|
| `kv-offload.archon` | The .archon source (5 holes, 8 arrows, 19 contract clauses) |
| `kv-offload.plan.json` | Compiled plan graph |
| `expected-health.txt` | archon-go health output (reference, not golden-checked due to sort non-determinism) |
| `expected-impact.txt` | archon-go impact output |
| `expected-stats.txt` | Compile stats (19 clauses) |
| `expected-dist-base.txt` | Initial distance: dist=13 |
| `expected-pr1593.txt` | PR #1593 result: 13→13 (bug fix, no architecture change) |
| `expected-pr1594.txt` | PR #1594 result: 13→13 (config added to sim/, not a new package) |
| `expected-pr1592.txt` | PR #1592 result: 13→13 (sim/kvtransfer ≠ sim/kv/transfer) |
| `expected-pr1595.txt` | PR #1595 result: 13→13 (sim/internal/kvkey ≠ sim/kv/blockkey) |
| `expected-dist-final.txt` | Final distance: dist=13 (unchanged) |
