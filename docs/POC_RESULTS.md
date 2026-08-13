# ARCHON PoC: What I Built, How It Works, How I Evaluated It, What I Got

## 1. What the PoC is

ARCHON's goal is to make **architectural** code review faster and more accurate,
now that AI writes PRs faster than humans can carefully review them. The PoC is the
smallest thing that shows the idea works: **get ARCHON's core loop running on a real
repository, and run a first experiment testing whether its output actually helps a
reviewer.**

**Test repository:** `todo-api` (MarioCarrion's todo-api-microservice-example), a
real Go microservice whose author **documented the architectural change in each
commit**. Those documented changes are my "answer key" (ground truth): I know what
each commit was *supposed* to change, so I can check whether ARCHON finds it.

---

## 2. What ARCHON is and how it works

### 2.1 It's a static-analysis tool
ARCHON is a **static-analysis tool**: it reads the source code and reasons about it
*without running the program*. It's built on Go's own compiler libraries:
- **`go/packages`**: loads and type-checks the repository (the same machinery the
  Go compiler uses).
- **`go/types`**: gives full type information, including **interface satisfaction**
  ("does type `T` implement interface `I`?").

The one place it goes beyond pure static analysis is the `evidence` step, where it
additionally *runs* the tests via `go test` (see §2.4).

### 2.2 The graph it builds, and how each node/edge is derived
It builds a **package-altitude "boxes and arrows" graph**. Everything is derived
from the **type-checked code**, not from text or regex matching. Two techniques
produce it:
- **(a) the Go type-checker itself** gives the exact compiler-level facts, and
- **(b) recognition rules** look for specific, known API calls and imports in that
  type-checked code, for the operational facts.

**Nodes ("compiler" = from the type-checker; "rule" = a recognizer over the
type-checked code):**
- internal package (compiler): each package `go/packages` loads from the module.
- external package (compiler): appears when an internal package imports a
  third-party package.
- `service:` / `api:` / `env:` / `flag:` / `cap:` nodes (rule): created by the same
  recognizers that produce the operational edges (finding a `service` edge creates
  its `service:Kafka` node, an HTTP route creates its `api:` node, and so on).
- **surface** of a package (compiler): its exported symbols, read from the
  type-checker's package scope.
- **schema** of a package (compiler): its exported struct types whose fields carry
  serialization tags (`json:"…"`, `db:"…"`, …), read from the type-checker.
- **invariants** of a package (compiler): the test functions (`Test*` / `Fuzz*`)
  in its `_test.go` files; each is bound to the contract it references (its
  `Guards` / `Exercises`) using the type-checker.

**Edges (typed). "compiler" = from the type-checker; "rule" = a recognizer over the
type-checked code:**
- `import` (compiler): read directly from the `import` statements.
- `call` (compiler): a reference to another package's exported function that the
  type-checker resolves, so a same-named local function is never mistaken for it.
- `implements` (compiler): ARCHON asks the type-checker "does concrete type `T`
  satisfy interface `I`?" for every pair. This is invisible to imports and is the
  "architecture as contract" seam.
- `config` (rule): a recognized call to `os.Getenv("X")` or `flag.String("y", …)`;
  ARCHON confirms via the type-checker that it really is the stdlib function, then
  takes the literal key. The key becomes an `env:X` / `flag:y` node.
- `service` (rule): importing a known client library (e.g. the Postgres or Kafka
  driver), matched against a curated list, becomes a `service:Name` node.
- `protocol` (rule): a recognized HTTP route registration (e.g.
  `HandleFunc("POST /tasks", …)`) becomes an `api:METHOD /path` node.
- `capability` (rule): importing an "escape-hatch" package (`unsafe`, `reflect`,
  `os/exec`, `net`, …) becomes a `cap:name` node.

Every edge stores its **witnesses** (which files/symbols created it), so a change
that only shuffles those (a file move, a body rewrite) leaves the edge intact.

### 2.3 The features (what each does, and how)
| Command | What it does | How it does it |
|---|---|---|
| `extract` | build the architecture graph (JSON) | `go/packages` + `go/types` static analysis |
| `delta` | the architectural change of a PR | extract the graph *before* and *after*, then diff them; witness-only changes stay **empty** |
| `delta --summary` | a short triage verdict + key items | see §2.5 |
| `delta --allow <f>` / `contract` | structural conformance | snapshot each box's allowed internal deps, then flag any that step outside it |
| `render` | draw the architecture / delta | emit Graphviz DOT or Mermaid (delta colored green/red) |
| `evidence` | contract coverage + PASS/FAIL | see §2.4 |
| `impact` | blast radius: what depends on a box | reverse-reachability over `import`/`call` edges |

### 2.4 How the contract / "is it tested" check actually works
This is the part that answers *"how are you checking the contracts-and-tested
thing?"* It's four steps.

1. **Who implements a contract?** For every concrete type `T` and interface `I`,
   ARCHON asks `go/types` `Implements(T, I)`. Every yes becomes an `implements`
   edge. So for interface `Store`, ARCHON knows the exact set of implementers.
2. **Which test guards which contract?** ARCHON also type-checks the *test* code.
   For each test, it notes which types the test mentions. If the test mentions an
   **interface**, that interface is something the test *guards*. If it mentions a
   **concrete type**, that's something the test *exercises* (actually runs).
   *Example:* a test that builds a `MemStore` and a `FileStore` and checks both
   against the `Store` interface *guards* `Store` and *exercises* `MemStore` and
   `FileStore`.
3. **Is a given implementer actually tested?** ARCHON compares the two: for
   `Store`, it knows all the types that implement it (step 1) and all the types the
   test actually exercises (step 2). An implementer the test exercises is
   **covered**; an implementer the test never touches is **uncovered**, an evidence
   gap. *Example:* if `RedisStore` implements `Store` but the contract test only
   exercises `MemStore` and `FileStore`, ARCHON flags `RedisStore` as uncovered:
   a real implementation nobody tests against the contract.
4. **Does the test pass?** The `evidence` command then *runs* the bound tests
   (`go test -json`) and reports **PASS / FAIL**.

**Important honesty (a real limitation):** static analysis **cannot prove** two
implementations behave identically. What it proves is weaker but still useful: that
a new implementation **is (or isn't) tested against the same contract**, and whether
that test **passes**. Steps 1 to 3 are static; only step 4 runs code.

### 2.5 What the `--summary` flag is
The normal `delta` output can be long (every added/removed arrow). Research on AI
review shows that *more comments hurt*: reviewers fixate on flagged spots ("anchor
bias") and review gets slower. So `--summary` prints a **short triage verdict**
instead:
- **FAST-TRACK**: the boundary delta is empty, so no architecture review is needed; or
- **NEEDS ARCHITECTURE REVIEW**: a boundary moved, followed by *only the few things
  worth looking at* (new services, endpoints, contracts, surface changes, and any
  evidence gaps), not the full edge dump.

### 2.6 What ARCHON supports today (capability inventory)
This is how developed the tool is right now.

**Nodes (the "boxes"):**
- internal packages, external packages,
- services (`service:Kafka`, `service:Vault`, …),
- API endpoints (`api:POST /tasks`),
- config keys (`env:VAULT_TOKEN`, `flag:name`),
- capabilities (`cap:net`, `cap:unsafe`, …).
- Each package box also carries its **surface** (exported API), its **schema**
  (serialized struct fields), and its **invariants** (the tests that guard it).

**Edges (the "arrows", 7 typed kinds), each with a witness set:**
- `import`, `call`, `implements` (compiler-level),
- `config`, `service`, `protocol`, `capability` (operational).

**Testing / contracts (what kind of testing it supports):**
- **Interface-level contract coverage:** for an interface, every implementer is
  expected to be exercised by that interface's test; ARCHON flags any that aren't
  (an evidence gap). *Static.*
- **Evidence (running the tests):** the `evidence` command runs the bound tests
  with `go test` and reports **PASS/FAIL**.
- **Invariant tracking:** flags when a guarding test is modified or deleted ("a
  promise changed"), even if the structure is otherwise unchanged.
- It works with **ordinary Go tests bound to a contract**; no property-testing
  library is required. If a team writes a model-based / property test as the
  interface's contract test, ARCHON binds to it and runs it like any other.

**Review features:**
- per-PR **architectural delta** with an **empty-vs-boundary verdict** (a pure
  refactor comes back empty),
- **structural conformance** (`contract` snapshot + `delta --allow`),
- **render** (DOT / Mermaid, delta colored),
- **blast radius** (`impact`),
- **concise triage summary** (`delta --summary`).

### 2.7 Features I added this cycle that came from the literature (low-effort, high-value)
- **Blast radius** (`impact`): given a package, ARCHON lists everything that
  depends on it, so you can see what might break if you change it. The idea comes
  from change-impact tools (CHID, EMSE 2025; DEPTEX's "topological blast radius",
  2026). It was cheap to add because ARCHON already has the dependency edges, so it
  just follows them backwards.
- **Concise triage summary** (`delta --summary`): instead of a long report, ARCHON
  gives a short verdict, "fast-track this PR" or "review it, and here are the few
  things to look at." The idea comes from studies showing that flooding reviewers
  with AI comments actually slows review down and makes them fixate on the flagged
  spots ("anchor bias") (Tufano 2024; Cihan 2024). So the goal is fewer, sharper
  signals, not more comments.
- Earlier, in the same spirit: **contract coverage + evidence** draws on
  property-based testing (QuickCheck) and consumer-driven contracts; the
  **operational edges** (service/API/config/capability) are ARCHON's own extension,
  not found in the academic code-graph papers.

**Still to add (from the literature):** package ARCHON as a **CI GitHub Action** so
it runs automatically on every PR.

---

## 3. Evaluation Part A: Is ARCHON correct?

I ran ARCHON on 4 documented commits + 1 dependency-version bump, and checked: does
the delta match what the commit's docs say changed, and does the deps-bump (no
architecture change) correctly come back **empty**?

| Commit | Documented change | ARCHON flagged | Match? |
|---|---|---|---|
| Cache-aside | Memcached caching decorator | `+memcached` box, `service:Memcached`, `memcached.Task` implements `TaskSearchRepository`, + evidence gap | ✅ |
| Kafka | Kafka message broker | `service:Kafka`, `internal/kafka`, `kafka.Task` implements `TaskMessageBrokerRepository`, `cap:syscall` | ✅ |
| Vault | Vault + secure config | `service:Vault`, `cap:net`, `env:VAULT_ADDRESS/PATH/TOKEN` | ✅ |
| OpenAPI | typed API endpoints | `api:POST /tasks/search`, `api:*/tasks/{id}`, + payload schema | ✅ |
| deps bump | (nothing architectural) | **empty** | ✅ (true negative) |

**Actual ARCHON output, cache-aside `delta` (trimmed):**
```
ARCHITECTURAL DELTA: boundary changed — review required
  + box   internal/memcached
  + box   service:Memcached (service)
  + arrow cmd/rest-server -> internal/memcached [call]
  + arrow internal/elasticsearch -> internal/memcached [implements]
  + arrow internal/memcached -> internal/service [implements]
CONTRACT COVERAGE — every implementer of a changed contract must be covered by that contract's test
  ! memcached.Task now implements service.TaskSearchRepository — no contract test guards this interface (evidence gap)
  ! elasticsearch.Task now implements memcached.Datastore — no contract test guards this interface (evidence gap)
```
**Result: all 4 feature commits flagged correctly; the dependency bump correctly
produced no delta.**

**Actual `--summary` output, cache-aside:**
```
ARCHON verdict: NEEDS ARCHITECTURE REVIEW — a boundary moved.
  new packages:  memcached
  services:      Memcached
  contracts:     memcached.Task ⊨ service.TaskSearchRepository [UNCOVERED], elasticsearch.Task ⊨ memcached.Datastore [UNCOVERED]
  surface +:     internal.NewMemcached
  evidence gaps: 2 new implementer(s) with no contract test
```
**vs. the deps bump:**
```
ARCHON verdict: FAST-TRACK — empty boundary delta; no architecture review required.
```

**Actual `evidence` output**, on a fixture *with* a real contract test (passes):
```
Contract: store.Store
  implementer file.File — covered by a contract test
  implementer mem.Mem — covered by a contract test
  implementer redis.Redis — covered by a contract test
  evidence: TestStoreContract — CI: PASS
```
and on todo-api, which has *no* contract tests (so everything is an honest gap):
```
Contract: envvar.Provider
  implementer vault.Provider — NOT covered (evidence gap)
  implementer envvartesting.FakeProvider — NOT covered (evidence gap)
  evidence: no contract test binds this interface
```

**Blast radius** answers: if you change (or split, or break) a package, which other
packages depend on it and could be affected? ARCHON walks the dependency edges
*backwards* from the target. "Direct" dependents import or call it straight away;
"transitive" includes things that reach it indirectly. It's the "what breaks if I
touch X?" question, useful for review and for planning a refactor.

**Actual `impact` output.** Here, changing `internal/service` would affect 2
packages (`rest-server` and its test helper):
```
BLAST RADIUS of .../internal/service
  2 direct dependent(s), 2 total (transitive)
  direct:   rest-server
  direct:   servicetesting
```

---

## 4. Evaluation Part B: Does ARCHON help an AI reviewer?

### 4.1 The question
> **If I give an AI code reviewer ARCHON's architecture summary, does it catch
> architectural problems it would otherwise miss?**

The test is whether ARCHON's output makes a reviewer **better**, not whether it
draws a nice graph.

### 4.2 The setup
- The **right answer** for each PR is the author's documented architectural change
  (from Part A), what a good reviewer *should* notice.
- For each commit I gave the **same reviewing task** to **two AI reviewers**,
  changing **only the information they got**:
  - **Reviewer A, diff only:** just the code diff.
  - **Reviewer B, diff + ARCHON:** the same diff **plus** ARCHON's delta report.
- Instructions were identical ("list the architectural concerns"). I **hid the
  author's written architecture doc** from both, so neither could read the answer.
- Since A and B differ in **only one thing** (ARCHON's report), any difference in
  what they catch is caused by ARCHON. (Reviewers were AI subagents; 3 commits.)

### 4.3 Update: I re-ran this with all transcripts saved, and the honest result is narrower
I re-ran the experiment and this time saved every input and every review to disk
(under `results/experiment/`, with a full write-up in
`results/experiment/COMPARISON.md`). The saved transcripts show a more honest
picture than an earlier version of this section claimed, so I am correcting it.

The earlier claim was that the diff-only reviewer "missed the evidence gap 3 out
of 3 times." That is too strong. With transcripts in hand:
- The **diff-only reviewer was strong** and, on its own, flagged "there are no
  tests for this new package" on every commit. On the Kafka commit it even noted
  "interface parity is not enforced between the two broker implementations." It
  was not blind.
- **ARCHON's real, repeatable lift is narrower and sharper:** it turned the vague
  "no tests" into a **precise, certain contract statement** (which interface, how
  many implementers, guarded or not, a computed fact rather than a guess), and it
  reliably surfaced the **boundary and config deltas** (for Vault: the removed
  `DATABASE_URL` key, the new `service:Vault`, the new `cap:net`) that are easy to
  miss in a large diff.

### 4.4 Results: 3 commits (from the saved re-run)
| Commit | Diff-only (A) caught on its own | What ARCHON added (B) |
|---|---|---|
| Cache-aside | staleness/no-invalidation, "no tests for the package", TTL bug | named the exact contracts with no test (`memcached.Task ⊨ service.TaskSearchRepository`) |
| Kafka | no tests, silent message loss, no dead-letter, "interface parity not enforced" | named `kafka.Task ⊨ TaskMessageBrokerRepository`, framed as **behavioral drift between the two broker implementations** |
| Vault | token/TLS security, thread-unsafe cache, "untested error paths" | the `Provider` interface has **3 implementers and no contract test**, plus the **config boundary change** (`DATABASE_URL` removed, `VAULT_*` added) |

**In plain terms:** both reviewers catch the missing tests. ARCHON's contribution
is to make the contract gap **precise and certain**, and to add the
**boundary/config delta**. It is an **assist, not a rescue**. That is still real,
repeatable value, and (see §6) it is likely worth more to a human reviewer than to
an AI one.

---

## 5. On a huge diff, ARCHON's summary is what makes it reviewable (OpenAPI)
One commit's diff was **11,407 lines** (mostly auto-generated code). I did **not**
run the two-reviewer test here; I just showed the size contrast. ARCHON's
`--summary` reduced those 11,407 lines to a few: **5 API endpoints, the payload
changes, and 8 new interface implementers with no contract test.** When a diff is
enormous, ARCHON's summary is usable and the raw diff isn't.

---

## 6. Bottom line
A good AI reviewer already sees the **structure** of a small, clear change on its
own, so against an AI reviewer ARCHON is an **assist**: it makes the contract gap
**precise and certain**, surfaces the **boundary/config delta**, and **compresses
giant diffs** into a few things worth looking at.

**Important nuance for a human audience.** The experiment used AI reviewers, which
is actually a hard test for ARCHON: an AI reads the entire diff carefully and
never tires, so ARCHON's biggest levers (compression, a diagram, holding
cross-file facts, and a fast-track/skip verdict) are wasted on it. Those levers
target exactly human weaknesses (attention, anchoring on a large diff, not being
able to hold the whole dependency map in one's head). So the value is plausibly
**larger for a human reviewer than these AI numbers show**. That is a motivated
hypothesis, not a measured result: confirming it needs a small **human study**
(reviewers with vs without ARCHON, measuring time-to-find and issues-caught),
which is the honest next experiment.

---

## 7. Honest caveats
- **Small sample:** 3 commits, one reviewer each.
- **Possible contamination:** todo-api is public, so the AI reviewers may have seen
  it in training and be *recalling* rather than *reasoning*.
- **Baseline missing:** I compared *diff-only* vs *diff+ARCHON*; I have not yet
  added a *dependency-graph* tool as a third comparison.
- **Not measured yet:** review token cost and review time.
- **Static-analysis limit:** coverage proves a new implementation is *tested against
  the same contract*, not that it *behaves identically*; that would need the deeper
  behavioral analysis planned for later.

---

## 8. What's next
- Add the **dependency-graph baseline** condition (diff-only vs dep-graph vs ARCHON).
- Measure **token cost** and **review time** per condition.
- Add a **contamination probe** and **≥2 reviewers** per condition.
- Run the **OpenAPI** commit through the reviewer experiment (big-diff case).
- Begin the **BLIS grounding** (Go-native): current-design graph + cycles +
  coupling + a blast-radius example.

---

## Appendix: how to reproduce
```sh
cd code/archon-go && go build -o archon-go .
# Part A, correctness (delta matches the documented change; deps bump is empty):
./archon-go delta ../todo-api b578358~1 b578358    # cache-aside
./archon-go delta ../todo-api 8f7d667~1 8f7d667    # kafka
./archon-go delta ../todo-api b0867ac~1 b0867ac    # vault
./archon-go delta ../todo-api db5a098~1 db5a098    # openapi
./archon-go delta ../todo-api 3af47cd~1 3af47cd    # deps bump -> empty
# concise triage verdict:
./archon-go delta ../todo-api b578358~1 b578358 --summary
# blast radius:
./archon-go impact ../todo-api internal/service
# contract coverage + run the bound tests (PASS/FAIL):
./archon-go evidence ../todo-api
```
**Part B (with/without ARCHON):** for each commit, its code diff (with the author's
`docs/*.md` architecture write-up held out) was given to two AI reviewers, one with
ARCHON's delta report and one without, and their reviews compared to the documented
change.
</content>
