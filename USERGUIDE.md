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

### plan — declare the architecture before the code exists

A `.archon` file states the packages you intend to build (`hole`), the ones you
depend on (`box`), the dependencies that must exist (`arrow`), and the promises
each package makes (`contract`). Full grammar: [docs/plan-syntax.md](docs/plan-syntax.md).

```sh
./archon-go plan compile --stats kv-offload.archon > kv-offload.plan.json
```

```
5 clauses: 0 checked, 5 evidenced, 0 attested:external, 0 attested:design
```

The JSON is the same graph format `extract` emits, so every other command accepts
it. The tally on stderr is the epistemic ladder: how much of the plan is *checked*
versus merely *asserted*.

```sh
./archon-go plan dist kv-offload.plan.json $R
```

```
dist(P,G) = 2
  unfilled holes (C1): 1
  absent boxes   (C2): 0
  absent arrows  (C3): 1
  disallowed     (C4): 0

  [C1] hole declared, package absent in actual
  [C3] declared arrow .../sim/kv/transfer -> .../sim (import) absent
```

One number for "how far is the code from the plan". Ratchet it in CI: if `dist`
goes up, the change moved away from the declared design. `dist = 0` means the
*structure* matches — signature divergence is reported separately as **surface
drift** (see `plan dist`'s drift section and `pr-review --plan`).

```sh
./archon-go plan slice kv-offload.plan.json github.com/inference-sim/sim/kv/transfer
```

```markdown
# github.com/inference-sim/sim/kv/transfer

## Surface

- `ActiveJobs(TierIndex, Direction) int`
- `Poll(now int64) []JobId`
- `Submit(TransferJob) JobId`

## Allow

- `import github.com/inference-sim/sim`
```

One hole as a work order — hand it to a teammate or an agent as the spec to
implement, instead of prose in an issue.

```sh
./archon-go plan render kv-offload.plan.json
```

```
graph LR
  n0["sim"]
  n1["cluster"]
  n2["hash"]
  n3(["tierchain"])
  n4(["transfer"])
```

Mermaid for a GitHub issue or PR. Holes are stadium-shaped and dashed; existing
boxes are solid.

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

### invariants — declared invariants vs the code and tests citing them

Many repositories write down the properties the system must uphold, give each an ID,
and then cite those IDs in comments. This follows the IDs: it reports which declared
invariants have production code behind them, which have only tests, and which exist
nowhere but the document declaring them. Nothing is inferred from code structure and
no model is involved.

```sh
./archon-go invariants $BLIS_REPO docs/contributing/standards/invariants.md \
    --at 73a17c00f84f28623e254a625f1f5298bb8c8a38
```

`--at` reads the registry **and** the code at one commit, so the numbers cannot drift
as the repo moves. It is a flag rather than a trailing positional commit — unlike
`health $R <commit>` — because the second positional here is already the registry
path, and guessing whether an argument is a path or a commit is the kind of silent
wrong answer this command exists to surface.

**What you'll see** (abridged; at that commit the registry declares 34):

```
DECLARED INVARIANTS
  registry: docs/contributing/standards/invariants.md (34 declared)
  commit:   73a17c00f84f28623e254a625f1f5298bb8c8a38
  scanned:  446 Go files

  ID           STATUS     CODE  TEST  CITES  NAMED
  INV-1        LINKED       14    31    143     14
  INV-7        LINKED        8     0     16      0
  INV-L4       LINKED        3     2     11      0
  INV-L6       UNLINKED      0     0      0      0
  NS-6         LINKED        3     4     21      6

  32 of 34 anchored — 32 LINKED, 0 TEST ONLY, 2 UNLINKED

  tests named for an invariant:
    INV-1        sim/cluster/cluster_tenant_test.go:TestTenantAdmission_INV1_BudgetShedConservation
                 sim/cluster/cluster_tier_test.go:TestGAIELegacy_INV1_Conservation
```

`scanned` is the denominator: "nothing is anchored" means something very different over
446 Go files than over three. `CODE` and `TEST` count files; `CITES` counts occurrences.
`NAMED` counts test functions whose *name* embeds the ID (`INV-6` → `TestINV6_Determinism`),
listed underneath rather than in a column because an invariant can have a dozen of them.

The two useful readings are the extremes. `INV-L6: UNLINKED` means **no `.go` file names
that ID** — not that the invariant is untested. BLIS's registry says exactly this about
INV-L6 and INV-L7: both have tests, but "neither test nor production site names the ID",
so there is no way to find them from the ID. That is the gap the status reports. A
citation in markdown, YAML or a shell script counts for nothing either, since only `.go`
files are scanned.

At the other extreme, `0 of N anchored` is the most useful thing this can tell a repo that
has just written a registry and not yet cited any of it.

Under `--at`, `<repo>` must be the repository root: `git worktree` checks out the root, so a
subdirectory would read the root's registry and scan the whole tree. The command refuses
that rather than guessing. It also warns if the repo has submodules, whose code a worktree
does not include.

Add `--json` for the machine-readable form; each link carries its derived `status`, so
a consumer never recomputes it.

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

It is **report-only**: it always exits 0. The verdict is carried in `review.json`
so your CI decides whether to fail the check.

**What you'll get** — a self-contained bundle, just two files by default:

```
.archon/
├── review.md      # primary: paste into a PR comment / >> $GITHUB_STEP_SUMMARY
└── review.json    # machine-readable result (schema: archon.pr-review/v1)
```

`review.md` leads with the **verdict**, then — only when the change is
architectural — embeds all three views (component, witness, contract) as
GitHub-renderable **Mermaid**, each followed by its detail table. No separate
image files are needed: the diagrams live inline. A CI job does just:

```sh
./archon-go pr-review $R "$BASE" "$HEAD" --out .archon
cat .archon/review.md >> "$GITHUB_STEP_SUMMARY"   # renders the Mermaid inline
```

**The verdict is binary:**

| verdict | meaning |
|---|---|
| `NO_CHANGE` | no package boundary moved — `review.md` is a one-line fast-track note (plus a pointer if a guarded promise / schema changed within the existing boundary) |
| `ARCHITECTURAL_CHANGE` | a package boundary moved — full component + witness + contract views, all embedded as Mermaid |

**Flags:** `--out DIR` (default `.archon`), `--allow FILE` (records off-baseline
dependencies as violations, against a baseline from `archon-go contract`),
`--depth N` (component grouping granularity, default 2), `--label-a/-b S` (human
labels for base/head), `--emit-artifacts` (also write the `.mmd`/`.dot`/`.md`
sources and PNGs — off by default, since everything is already embedded in
`review.md`), `--invariants FILE` (see below).

#### Declared invariants in the review

If the repo has a declared invariant registry (see `invariants`), `pr-review` adds
a section reporting which declared invariants the change exposes:

```
### Declared invariants — registry

Registry: `docs/contributing/standards/invariants.md` at `d77764f520568b6c67616ca178b214fe288be7fd` — 23 declared, 22 of 23 anchored (18 LINKED, 4 TEST ONLY, 1 UNLINKED), 318 Go files scanned.

| ID | Status | citing functions touched | named tests in touched files |
|---|---|---|---|
| `INV-11` | LINKED | 1 of 4 | 0 of 0 |
| `INV-13` | LINKED | 5 of 30 | 0 of 5 |
| `INV-6` | LINKED | 18 of 170 | 1 of 3 |
| `INV-8` | LINKED | 1 of 14 | 1 of 1 |
| `INV-3` | LINKED | 1 of 26 | 0 of 0 |
| `INV-1` | LINKED | 1 of 70 | 0 of 6 |
| `INV-5` | LINKED | 0 of 21 | 1 of 2 |
| `INV-9` | LINKED | 0 of 11 | 1 of 3 |
```

(Every number above is from that pinned commit, and `demo/flow1-pr-review`'s golden
holds the whole table in its `review.json`.)

**Matching is function-scoped.** Each citation of an ID is attached to the smallest
thing that encloses it — the **function** whose body or doc comment holds it, else
the enclosing **declaration block** (a `const`/`var`/`type` group), else the **whole
file** when it sits in a file-header comment (or the file does not parse). An
invariant is reported only when a line the change actually edited lands inside one
of those scopes. `citing functions touched` is `n of m`: `m` is how many distinct
citation-bearing scopes the ID has, `n` how many a changed line reached. This is the
point of the section over file-level counting — a hunk hundreds of lines from a
citation no longer counts it, and a one-line edit inside a function whose doc comment
cites the ID counts at full strength. Changed lines are the diff's **new-side**
ranges, since citations live in the head tree; a pure deletion is recorded at the
surviving head line it abuts, so gutting a function's body still counts as touching
it.

Rows are **ranked by proportion — `touched / citing`, highest first** — not by raw
count, so `1 of 4` (25%) leads `18 of 170` (11%) even though 1 < 18. The raw counts
stay as the evidence. Rows at an equal proportion break the tie on the raw touched
count, then on ID. A row shown only because a **named test** was touched has no
function overlap (`0 of …`) and sorts below every row a changed line reached.

**Every touched row renders** — there is no proportion threshold and no footer.
Function-scoping is itself the filter: once a row means "a function carrying this
ID's comment changed", every row is real, so there is nothing diffuse left to hide.
(This replaced the earlier `touched/citing` threshold, which — now that the numerator
is functions, not files — would have discarded true signal.)

Two things worth knowing. "Named tests in touched files" stays file-level: the link
data carries `file:function`, not line ranges, so a change elsewhere in the same file
counts there (only the citation column moved to function scope). And because the
footprint is read at the head commit, a file the change *deletes* appears in no
column — so deleted citation sites get their own line, since removing an invariant's
last anchor is the change most likely to leave a declared promise unguarded, and that
line always shows.

Standing `UNLINKED` invariants — declared but cited nowhere in the repo — are *not*
listed here: that is a fact about the repository rather than this change, and it is
the `invariants` command's job to audit. The header totals still count them, and a
citation *this* change removed is reported on its own line as above.

Files touched are computed from the merge base of `<base>` and `<head>`, so a base
that has moved on does not get its commits attributed to this change.

**You do not have to pass the flag.** With none, archon probes
`docs/contributing/standards/invariants.md`, then `docs/invariants.md`, then
`INVARIANTS.md`, **at the head commit** rather than in your working tree:

| situation | behaviour |
|---|---|
| no flag, no registry at a conventional path | **no section**, bundle byte-identical to before this existed; the paths probed are named on stderr |
| no flag, registry found | section rendered, **naming the path it used** |
| no flag, registry found but unparseable | **warning on stderr, no section.** `pr-review` is report-only, so a mistyped heading in a docs PR must not delete the architectural review |
| `--invariants <path>`, file present | section rendered from that path |
| `--invariants <path>`, missing or unparseable | **hard error** — you asked for something that is not there |
| registry parses, nothing cites any ID | section rendered reporting `0 of N anchored` — a finding, not an empty result |

The path is printed in the section for a reason: if the doc is renamed, the section
would otherwise vanish and every later review would look normal.

The section is **advisory**: it cannot change the verdict, `dist`, or the exit code
— same tier as contract clauses. `TestRegistryIsAdvisory` builds the same change
twice, with and without a registry, and compares the verdict, the counts, the plan
ratchet that carries `dist`, the review.md prose and the review.json key set; the
exit code is covered in `TestPRReviewRegistryDiscovery`.

**What you'll see — a boundary-moving PR** (inference-sim #1546, which decoupled
`sim/saturation`):

```
archon pr-review: ARCHITECTURAL_CHANGE — bundle written to .archon
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

### callgraph — which function calls which

Everything above works at package altitude; this one works at function altitude,
which is what you want for "if I change this method, who is affected?"

```sh
./archon-go callgraph $R ./...                  # direct calls
./archon-go callgraph $R ./... --mode=cha       # also calls made through an interface
```

Graphviz DOT on stdout, a summary on stderr. Add `--since <ref>` to draw only the
functions a change touched plus their neighbours, and `--depth N` to widen that.

**Why the mode matters.** In `inference-sim`, `sim/event.go:46` reads:

```go
queued_delay := sim.latencyModel.QueueingTime(e.Request)
```

`latencyModel` is an interface, and two types implement it. The default mode
resolves calls through `go/types`, so the callee here is the *interface* method —
which has no body, so the call is dropped:

```sh
./archon-go callgraph $R ./... | grep ' -> .*QueueingTime'
# nothing: the graph has the method as a node, but no arrow into it
```

`--mode=cha` resolves it to every implementation that could satisfy the call:

```sh
./archon-go callgraph $R ./... --mode=cha | grep ' -> .*QueueingTime'
```

**What you'll see:**

```
"(*sim.ArrivalEvent).Execute" -> "(*sim/latency.RooflineLatencyModel).QueueingTime" [style=dashed, tooltip="dispatched through sim.LatencyModel.QueueingTime"];
"(*sim.ArrivalEvent).Execute" -> "(*sim/latency.TrainedPhysicsModel).QueueingTime" [style=dashed, tooltip="dispatched through sim.LatencyModel.QueueingTime"];
```

An interface call is drawn **dashed**, and its tooltip names the method that was
dispatched, so you can tell it from a direct call and see why it is there. On that
module the difference is 1413 edges against 1817 — about a fifth of the call graph
was missing.

**Which mode to use.**

| mode | resolves dispatch | needs | use it for |
|---|---|---|---|
| `static` (default) | not at all | anything | the behaviour this tool has always had |
| `cha` | every implementation that satisfies the interface | nothing — works on a library | the general answer |
| `rta` | only implementations reachable from `main` | a `main` package | a whole program, when `cha` is too broad |

`cha` is the one to reach for. It over-approximates, and it is sound on a partial
program — a package with no `main` — which `rta` is not:

```sh
./archon-go callgraph . ./internal/graph/... --mode=rta
# mode=rta needs an entry point: no main package among the loaded packages; use mode=cha for a library
```

**It tells you what it cannot see.** A call through a function value is not
dispatch and is excluded on purpose. So are calls whose caller is a wrapper the
compiler synthesised — a method value such as `f := s.Get` — because a wrapper has
no body to draw the edge from. Those are counted rather than dropped in silence:

```
callgraph: 6 unresolved interface dispatches, 1 of them positioned (no function with a body to draw the edge from):
  cmd.Err at $R/cmd/root.go:2744
  5 inside wrappers go/ssa synthesised, which have no position
```

It also warns when a package does not type-check, because `go/ssa` builds nothing
for such a package: its functions still appear as nodes while its calls go
missing, so the graph would look complete when it is not.

> These are **leaf** edges, and they are deliberately not aggregated into the
> package-level graph that `render`, `impact` and `health` draw. A caller that
> reaches an implementation through an interface does not depend on it — avoiding
> that dependency is what the interface bought — so a package arrow would erase
> the decoupling and make well-factored code look tangled.

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
