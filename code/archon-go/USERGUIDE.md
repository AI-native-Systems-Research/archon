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
cd code/archon-go
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

---

## 5. Reviewer helper scripts (optional)

Two standalone Python scripts in `reviewer/` turn an extracted graph into a
higher-altitude "component" picture for a PR comment. They need no extra setup
beyond Python 3.

```sh
# stable component map (subsystem boxes), as a picture
./archon-go extract $R > graph.json
python3 reviewer/component_view.py graph.json --format dot | dot -Tpng -o components.png

# one PR's change painted on those boxes
./archon-go delta $R 428982c 3340de7 --json > delta.json
python3 reviewer/component_view.py graph.json --emit-components > components.json
python3 reviewer/component_delta.py delta.json components.json "inference-sim 428982c..3340de7" --graph graph.json | dot -Tpng -o pr.png
```

## 6. A complete first session, start to finish

```sh
cd code/archon-go
go build -o archon-go .
R=/path/to/your/repo

./archon-go health $R                                   # healthy? cycles? god-modules?
./archon-go render $R --full --format=dot | dot -Tpng -o arch.png   # see it
./archon-go delta  $R HEAD~1 HEAD --summary             # what did the last commit change?
```
