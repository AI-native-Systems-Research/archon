# ARCHON user guide — running it on a codebase

ARCHON reads a Go codebase and shows you its architecture: which packages depend
on which, which interfaces are implemented, what a pull request changed at the
boundary level, and whether the contract tests actually cover those boundaries.
It is fully deterministic (same input, same output) and uses no LLM.

Every command below shows the exact command to run and what you will see. The
examples run against a real repo, `inference-sim`; swap in your own path.

## 1. What you need

- **Go 1.26 or newer** (`go version` to check). Needed to build the tool and to
  type-check the repo you point it at.
- **Graphviz** (`dot -V` to check), only if you want the pictures. On a Mac:
  `brew install graphviz`. Every command works without it; you just cannot turn
  the `.dot` output into a PNG.
- **The target must be a Go module** (has a `go.mod`). For the two-commit
  commands it must also be a git repo.

## 2. Build it once

```sh
git clone https://github.com/AI-native-Systems-Research/archon.git
cd archon
go build -o archon-go .
```

That produces a single binary, `./archon-go`. Nothing to install, no config.

Throughout, I use `$R` for the repo you are pointing at:

```sh
R=/path/to/your/repo        # e.g. R=~/code/inference-sim
```

## 3. The 30-second version

```sh
./archon-go health $R                                   # is it healthy?
./archon-go render $R --full --format=dot | dot -Tpng -o arch.png   # draw it
./archon-go delta  $R HEAD~1 HEAD --summary             # what did the last commit change?
```

---

## 4. The commands, each with an example and what you'll see

### extract — the whole architecture as JSON

```sh
./archon-go extract $R > graph.json
```

The raw package graph: every node and every typed edge (import / call /
implements / config / service / capability / protocol), plus schema and
invariants. Every other command is built on this. Save it once and you can feed
the `.json` back in to skip re-extraction.

**What you'll see** (a big JSON document; here is the top):

```json
{
  "module": "github.com/inference-sim/inference-sim",
  "packages": [
    { "path": "cap:net",     "name": "cap:net",  "internal": false },
    { "path": "cmd",         "name": "cmd",       "internal": true  },
    ...
  ],
  "edges": [ ... ]
}
```

On inference-sim that is ~1.3 MB covering 56 packages (internal + external).

### health — understand the current design

```sh
./archon-go health $R
```

The first thing to run on an unfamiliar codebase. Add `--json` for machine output.

**What you'll see:**

```
ARCHITECTURE HEALTH
  cycles: none — internal dependency graph is an acyclic DAG (healthy)
  god-modules (high fan-in + large surface): sim, latency, workload
  coupling (top by blast radius):
    package        fanIn  fanOut   surf   instab  blast
    sim                7       3    305     0.30      8  <god>
    workload           3       1    140     0.25      4  <god>
    cluster            1       5    314     0.83      2
    cmd                1       7     25     0.88      1
    ...
```

Read it as: **cycles** should be none; **god-modules** are packages everything
leans on; **blast** is how many packages break if you touch this one; **instab**
(instability) near 1.0 means "depends on many, depended on by few" (a leaf/entry),
near 0.0 means "depended on by many" (a core others rely on).

### render — draw it

```sh
# the whole architecture as a picture
./archon-go render $R --full --format=dot | dot -Tpng -o arch.png

# include external services / env vars / capabilities as nodes too
./archon-go render $R --full --external --format=dot | dot -Tpng -o arch_full.png

# Mermaid instead of Graphviz (paste straight into Markdown / a PR comment)
./archon-go render $R --full --format=mermaid > arch.mmd
```

Without `--full` it draws only the *changed* neighborhood — that is what you want
for the delta view below. Blue box = internal package; orange note = a world node
(env var, flag, service, capability).

**What you'll see** (Mermaid form, first lines):

```
graph LR
  n2["inference-sim"]
  n3["cmd"]
  n4["sim"]
  n5["cluster"]
  n0["env:HF_TOKEN"]
  ...
```

### impact — blast radius of one package

```sh
./archon-go impact $R cluster
```

Everything that depends on `cluster`, directly and transitively. Answers "if I
touch this, what can break." Use the short package name.

**What you'll see:**

```
BLAST RADIUS of github.com/inference-sim/inference-sim/sim/cluster
  1 direct dependent(s), 2 total (transitive)
  direct:   cmd
  indirect: inference-sim
```

### delta — what a PR changed, architecturally

This is the point of the tool. Give it two commits (before, after). The examples
below use two real inference-sim commits, so with `R=~/code/inference-sim` you can
run them exactly as written:

```sh
# one-line triage verdict
./archon-go delta $R 428982c 3340de7 --summary

# full human report
./archon-go delta $R 428982c 3340de7

# machine-readable, for scripts or the coverage view
./archon-go delta $R 428982c 3340de7 --json > delta.json
```

(On your own repo, any two commit hashes work; or use `HEAD~1 HEAD` for the last
commit, or `<mergeCommit>~1 <mergeCommit>` to inspect one merged PR.)

**What you'll see — `--summary`** (the triage line):

```
ARCHON verdict: FAST-TRACK — empty boundary delta; no architecture review required.
```

Most PRs are internal changes that move no package boundary, so they fast-track.
When something structural does move, the verdict flips to "needs an architecture
pass" instead.

**What you'll see — the full report** (this PR moved no boundary, but it did
change guarded invariants, so it still asks for a look):

```
ARCHITECTURAL DELTA: empty at package altitude
  -> internal change; no package boundary moved.
INVARIANTS TOUCHED — review required (a system promise changed)
  ~ invariant cluster.TestDisaggregation_MetricProjection_E2ECorrectness (guard changed)
  + invariant cluster.TestPDParentE2E_GeqDecodeOwnE2E (new guard)
  ...
```

Draw the same delta as a picture (added = green, removed = red, grey = context):

```sh
./archon-go render $R 428982c 3340de7 --format=dot | dot -Tpng -o delta.png
```

**What you'll see — `--json`** (top-level shape, for scripting):

```
top-level keys: commitA, commitB, emptyAtPackageAltitude, invariants
```

### evidence — do the contract tests actually cover the interfaces

```sh
./archon-go evidence $R
```

For each interface, which implementers are actually exercised by a bound test.
This is the "green tests, but is the seam really covered" check. `--json` for
machine output.

**What you'll see:**

```
CONTRACT EVIDENCE — inference-sim

Contract: sim.AdapterCost
  implementer lora.CostModel — unconfirmed (a contract test exists but drives
    implementers via a factory, so this one cannot be attributed)
  evidence: TestStepTime_AdapterBatch_SlowerThanBase — CI: PASS
  ...
```

Each implementer is tagged **proven** (a test guards its interface and exercises
it), **unconfirmed** (a test exists but cannot be attributed to this implementer),
or **no-test**.

### reflexion — declared layering vs actual code

Write a tiny `layers.json` describing your intended top-to-bottom layers, then:

```sh
./archon-go reflexion $R layers.json
```

`layers.json` looks like:

```json
{ "layers": ["entry", "core", "leaf"],
  "map": { "cmd": "entry", "sim": "core", "util": "leaf" } }
```

**What you'll see:**

```
REFLEXION MODEL — declared layering vs actual code
  layers (top→bottom): entry → core → leaf
  convergent (downward) deps: 7
  DIVERGENT (upward, layering violations): 0  (0% of cross-layer deps)
  → code conforms to the declared layering.
```

An upward dependency (a leaf importing an entry package) counts as a violation.

### contract — snapshot an allow-list baseline

```sh
./archon-go contract $R > allow.json
```

Records the currently-permitted internal dependencies. Feed it back to `delta` to
fail on any *new* dependency that is not on the list:

```sh
./archon-go delta $R 428982c 3340de7 --allow allow.json
```

**What you'll see** (the baseline JSON: each package → what it may import):

```json
{
  "github.com/inference-sim/inference-sim/cmd": [
    "github.com/inference-sim/inference-sim/sim",
    "github.com/inference-sim/inference-sim/sim/cluster",
    ...
  ]
}
```

### pr-review — the CI one-shot (a review bundle)

This is the command CI calls. Give it the repo and the two commits; it extracts
both (through an ephemeral git worktree, so your working tree is never touched),
computes the delta, and writes a **review bundle** into `--out` (default
`.archon/`):

```sh
./archon-go pr-review $R <base> <head> --out .archon
```

It is **report-only**: it always exits 0. The verdict — including `BLOCK` — is
carried in `review.json` so your CI decides whether to fail the check.

**What you'll get** in the bundle:

```
.archon/
├── review.md      # primary: paste into a PR comment / >> $GITHUB_STEP_SUMMARY
├── review.json    # machine-readable result (schema: archon.pr-review/v1)
├── component.mmd  # component diagram (Mermaid, also embedded in review.md)
├── component.dot  # + component.png if Graphviz is installed
├── witness.dot    # + witness.png if Graphviz is installed
└── contract.md    # interface-contract delta table
```

`review.md` leads with the **verdict**, then — only when the change is
architectural — embeds a GitHub-renderable **Mermaid** component diagram and the
witness / contract / (violation) tables. A CI job does just:

```sh
./archon-go pr-review $R "$BASE" "$HEAD" --out .archon
cat .archon/review.md >> "$GITHUB_STEP_SUMMARY"   # renders the Mermaid inline
```

**The verdict is tiered** (least to most review needed):

| verdict | meaning |
|---|---|
| `FAST_TRACK` | internal-only at the package altitude — `review.md` is a one-line note, no graphs |
| `REVIEW_INVARIANTS` | boundary empty, but a guarded promise (invariant or wire/DB schema) changed |
| `REVIEW_ARCHITECTURE` | a package boundary moved — full component + witness + contract views |
| `BLOCK` | the PR introduced a dependency the box's allow-list forbids (needs `--allow`) |

**Flags:** `--out DIR` (default `.archon`), `--allow FILE` (enables `BLOCK`
detection against an allow-list baseline from `archon-go contract`), `--depth N`
(component grouping granularity, default 2), `--label-a/-b S` (human labels for
base/head), `--no-png` (skip Graphviz).

**What you'll see — a boundary-moving PR** (inference-sim #1546, which decoupled
`sim/saturation`):

```
archon pr-review: REVIEW_ARCHITECTURE — bundle written to .archon
```

and `review.md`'s witness table distinguishes the full decoupling from the
partial one:

```
| Edge                            | Kind       | Status                        | Removed                | Still coupled via                              |
| sim/saturation → sim            | implements | REMOVED (full decoupling)     | Bank |= BatchClassifier | —                                              |
| sim/saturation → sim/workload   | call       | WEAKENED (partial)            | NewBacklogClassifier    | DefaultBacklogDriftConfig, NewBacklogDriftConfig |
```

This is the same information as `reviewer/review.py --level 3`, produced by the
single Go binary with no Python and no checkout — which is what makes it a clean
fit for a CI runner. The Python wrapper remains the interactive human path (see
§5).

---

## 5. Reviewer views — one command, three levels

For a PR, the fastest path is the wrapper `reviewer/review.py`. Give it the repo
and the two commits and pick an altitude with `--level`; it runs `archon-go` /
`consumes` and the right renderers for you and drops the text + PNGs in one
folder. It adds no analysis and calls no model — it only sequences tools that are
already deterministic, so the same repo + commits reproduce the same bytes.

```sh
R=/path/to/inference-sim

# escalating altitude — pick one; each is self-contained
python3 reviewer/review.py $R 70e9ba8 5e28e00b --level 1 --label-a base --label-b "#1546"
python3 reviewer/review.py $R 70e9ba8 5e28e00b --level 2 --label-a base --label-b "#1546"
python3 reviewer/review.py $R 70e9ba8 5e28e00b --level 3 --label-a base --label-b "#1546"
```

(`70e9ba8` / `5e28e00b` are PR #1546's merge-base and head.) Artifacts land in
`./archon_review_<A>_<B>/` — text on stdout and saved as `.txt`, plus a `.png` per
view when Graphviz is present.

| level | question it answers | views it produces |
|---|---|---|
| **1** SUMMARY   | *what changed?* — packages / exported symbols / schemas / edges / invariants, with a triage verdict | `surface_delta` |
| **2** STRUCTURE | *where did it land?* — the system as auto-derived component boxes, PR painted on top | `component_view` + `component_delta` |
| **3** CONTRACTS | *did the decoupling actually happen?* — per-edge witnesses (full vs **partial** decoupling) + interface-contract delta + stranded-smell flips | `witness_delta` + `contract_delta` |

All views share one four-color scheme: **green** = added, **red** = removed,
**blue** = modified, **grey** = unchanged.

**Worked example — PR #1546**, all three levels, with the figures and a walkthrough
of what each shows, is checked in at [`reviewer/examples/pr1546/`](reviewer/examples/pr1546/).
Its short version: the PR meant to decouple `sim/saturation` from both `sim` and
`sim/workload`. Level 3 shows one decoupling landed **fully** (`saturation ⊨ sim`
REMOVED) and the other only **partially** (`saturation → workload` WEAKENED — the
interface call was cut but two config calls still cross the boundary) — a
distinction the level-1/2 present/absent edge cannot show.

Useful flags: `--reuse` re-renders from JSON already in the outdir with **no**
binary calls and no repo access (fully offline); `--skip-contract` gives level 3's
witness view without the `consumes` checkout; `--depth N` sets level-2 granularity;
`--from/--to PKG` (level 3 witness) and `--interface SUBSTR` (level 3 contract)
focus on one dependency or interface; `--outdir DIR` picks the output folder.

> **Note on level 3.** The contract half uses `consumes`, which is working-tree
> based, so the wrapper **checks each commit out and restores HEAD afterward**.
> Don't run level 3 against a repo another job is using; use `--skip-contract` (no
> checkout) or `--reuse` (offline) if that matters.

### 5.1 The pieces individually

If you want to run one renderer by hand, each is a standalone script in
`reviewer/` that reads a different `archon-go` output and calls no model:

| script | reads | altitude it answers |
|---|---|---|
| `component_view.py`  | one `extract` JSON            | subsystem boxes: "what is the system" |
| `component_delta.py` | `delta` JSON + components + `--graph` | one PR painted on those boxes |
| `surface_delta.py`   | one `delta` JSON              | PR summary: symbols/schema/invariants added/removed |
| `witness_delta.py`   | two `extract` JSONs (A, B)    | per-edge *reasons*: full vs **partial** decoupling |
| `contract_view.py` / `contract_delta.py` | two `consumes` JSONs | interface implementers/consumers and their delta |

```sh
# --- component map (subsystem boxes), as a picture ---
./archon-go extract $R > graph.json
python3 reviewer/component_view.py graph.json --format dot | dot -Tpng -o components.png

# --- one PR's change painted on those boxes ---
./archon-go delta $R 428982c 3340de7 --json > delta.json
python3 reviewer/component_view.py graph.json --emit-components > components.json
python3 reviewer/component_delta.py delta.json components.json "inference-sim 428982c..3340de7" --graph graph.json | dot -Tpng -o pr.png

# --- PR summary (symbols / schema / invariants), good for a PR comment ---
python3 reviewer/surface_delta.py delta.json --label-a base --label-b HEAD

# --- witness delta: WHY each package edge survived, weakened, or died ---
# Needs a snapshot at each commit. This is the view that distinguishes a full
# decoupling (edge and all its reasons removed) from a partial one (edge kept,
# some reasons removed, others persist). The commits below are PR #1546's
# base and head, so this reproduces the output shown next.
./archon-go extract $R 70e9ba8 > A.json
./archon-go extract $R 5e28e00b > B.json
# focus on one package's outgoing dependencies (both of #1546's decouplings):
python3 reviewer/witness_delta.py A.json B.json --label-a base --label-b "#1546" --from saturation
# the figure (drop --from to see the whole changed graph):
python3 reviewer/witness_delta.py A.json B.json --label-a base --label-b "#1546" \
    --format dot | dot -Tpng -o witness.png

# --- contract delta: interfaces, implementers, and stranded-smell flips ---
# `consumes` is working-tree based, so snapshot each commit (checkout, run,
# restore). This is the check the wrapper's --level 3 automates.
go build -o consumes ./cmd/consumes          # once
git -C $R checkout 70e9ba8 && ./consumes $R ./... --json > conA.json
git -C $R checkout 5e28e00b && ./consumes $R ./... --json > conB.json
git -C $R checkout -            # restore your branch
python3 reviewer/contract_delta.py conA.json conB.json --label-a base --label-b "#1546" --format text
python3 reviewer/contract_delta.py conA.json conB.json --interface BatchClassifier \
    --format dot | dot -Tpng -o contract.png
```

**What `witness_delta.py` prints** on PR #1546 (each changed edge, with the exact
symbols/files that witness it):

```
# Witness delta (why each package edge survived / weakened / died)
#   A: base   -> B: #1546

VERDICT: 1 edge(s) fully decoupled; 1 edge(s) PARTIALLY decoupled (weakened)

sim/saturation --implements--> sim   REMOVED
    - type: Bank |= BatchClassifier
    ok FULL decoupling: edge and all its reasons removed.

sim/saturation --call--> sim/workload   WEAKENED   (3 -> 2 symbols)
    - symbol: NewBacklogClassifier
    = still coupled via: DefaultBacklogDriftConfig, NewBacklogDriftConfig
    !! PARTIAL decoupling: edge remains; some reasons removed, others persist.
```

Read it as: the first edge is **fully** decoupled (gone); the second is only
**partially** decoupled — the edge remains because two config calls
(`DefaultBacklogDriftConfig`, `NewBacklogDriftConfig`) still cross the boundary,
even though the interface call (`NewBacklogClassifier`) was removed. Useful
filters: `--from PKG` / `--to PKG` to focus one dependency,
`--kind call|import|implements`, `--all` to include unchanged edges.

## 6. A complete first session, start to finish

```sh
git clone https://github.com/AI-native-Systems-Research/archon.git && cd archon
go build -o archon-go .
R=/path/to/your/repo

./archon-go health $R                                   # healthy? cycles? god-modules?
./archon-go render $R --full --format=dot | dot -Tpng -o arch.png   # see it
./archon-go delta  $R HEAD~1 HEAD --summary             # what did the last commit change?
```
