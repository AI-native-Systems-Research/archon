# ARCHON — Positioning, Related Work & Evaluation Notes

Tags: **[DONE]** built & verified, **[PARTIAL]** started, **[PLANNED]** not built.
Numbers/commits are cited only where we actually ran them; older figures are
flagged "re-confirm".

---

## 1. One-liner & positioning (AI era)

**The pitch:** AI now writes code faster than humans can review it. PRs land in
volume, and a textual diff is a poor proxy for the question that actually matters
— *did this change move an architectural boundary?* ARCHON makes the boxes-and-
arrows view a **mechanically-recovered, per-PR, typed artifact**, so both humans
and AI reviewers can review the **architectural delta** accurately and
efficiently — and so an agent's change has a checkable gate before it merges.

**Candidate one-liners (pick/trim for the talk):**
- *"In the agent era, code is generated faster than it can be reviewed. ARCHON
  reviews the architectural delta of a PR — not its text — so humans and AI focus
  on what actually changed the design."*
- *"The diff is the wrong unit of review. ARCHON computes what a PR changed
  architecturally, and whether the guarantees behind that change still hold."*

**Motivation beats:**
1. Code-delivery volume is exploding (coding agents); review is now the
   bottleneck.
2. Reviewers get a textual diff — a 2,000-line PR might change *no* boundary, or
   a 20-line PR might quietly add a service dependency or widen an API. The diff
   doesn't say which.
3. Existing architecture tools either need a hand-drawn model, only check import
   rules, or just show the current state — none give a per-PR architectural delta
   across many edge kinds tied to evidence.
4. ARCHON: auto-recover the architecture → diff it per PR → route review + flag
   the evidence a change puts at risk. Same artifact serves humans and agents.

---

## 2. Related work — the gap, and why it matters

**The gap in one sentence:** existing work either checks hand-written rules,
shows/recovers architecture *state*, improves the PR *code* view, analyzes code at
a low level, studies evolution over history, produces evidence, or feeds graphs to
models to *write* code — **none compute a typed architectural delta of a single
PR, across many edge kinds, tied to evidence.** That combination is ARCHON.

### Why do we need it? (the "so what")

A delta of a PR is only interesting if it changes what a reviewer does. Here is
why it does:

1. **Review attention is the scarce resource, and ARCHON spends it well.** Nobody
   can carefully review every PR anymore. ARCHON tells you *which PRs to look at*:
   an **empty** delta means "internal change, no boundary moved — no architecture
   review needed," and a non-empty delta means "a boundary moved, look here."
   That triage is the whole point — it lets reviewers spend their limited
   attention on the changes that can actually reshape the system.

2. **The textual diff misleads in both directions; the delta fixes both.** A huge
   diff can be architecturally empty (a refactor, a body rewrite) → today it
   wastes review effort; ARCHON collapses it to "empty." A tiny diff can hide a
   real boundary move (a new service dependency, a widened API, a new
   implementer of a contract) → today it slips through; ARCHON surfaces it. On
   todo-api's real cache-aside PR, the actual risk — a cache decorator inserted on
   the store contract with **no test guaranteeing the cache and store agree** — is
   invisible in the diff but explicit in ARCHON.

3. **It ties a change to the guarantee it puts at risk, not just to structure.**
   "You added a cache → a cache-consistency guarantee is now owed, and no test
   covers it." That connects the change to *what could actually break*, which is
   the reviewer's real question — and no dependency graph or rule checker does it.

4. **It is the missing gate for agentic merges.** If agents are going to land
   changes at scale, we need a *machine-checkable* condition for "this change
   didn't move a boundary" (fast-path) vs "it did" (send to a human). ARCHON
   computes exactly that condition, automatically. Nothing else provides it.

**Honest scope of the need:** the payoff scales with repo size, PR volume, and
how often the review-relevant boundary is *not* an obvious import (operational
edges — services, APIs, config, schemas). For a small, simple, low-traffic repo,
the diff may be enough. ARCHON earns its keep on large, fast-moving, multi-concern
systems — exactly where review breaks down today.

### Nearest competitors — the difference, and is ARCHON actually better?

- **Erode** (compares a PR against an existing architecture model, LikeC4/
  Structurizr). *Difference:* it needs a **hand-maintained model**; ARCHON
  **auto-recovers** the architecture from source. *Is ARCHON better?* On adoption
  and faithfulness, yes — most repos have no model, and a hand-drawn model drifts
  from the code, so it can silently disagree with reality; ARCHON's baseline is
  recomputed from source, so it can't. *Honest caveat:* if a team *does* maintain
  a curated intended model, Erode enforces **intent** (what you *meant* the
  architecture to be), while ARCHON's snapshot captures **what is** — so ARCHON
  needs its snapshot-and-ratchet story to encode intent over time. Net: ARCHON
  wins on zero-setup and faithfulness; Erode has an edge only where a good intent
  model already exists.

- **Arch-Engine** (PR-vs-base dependency edges, policy violations, topology).
  *Difference:* dependency/rule-based; ARCHON adds the typed **non-import** edges
  (interfaces, services, APIs, schemas, capabilities) and the contract/evidence
  layer. *Is ARCHON better?* Yes when the review-relevant boundary isn't a plain
  dependency — which we've shown it often isn't (a cache is an `implements` +
  `service` change, not a new import). *Honest caveat:* if a team only cares about
  layering/dependency rules, Arch-Engine already covers that; ARCHON's advantage
  is precisely the extra edge kinds and contracts.

- **CHID** (call graph → impacted methods + a risk score on the PR). *Difference:*
  it answers "how risky is this / what might break" (impact analysis); ARCHON
  answers "what boundary moved" (architecture) and "what evidence is missing."
  *Is ARCHON better?* It's a **different axis**, not strictly better — they're
  complementary. CHID gives blast radius; ARCHON gives architectural meaning + the
  evidence gap. A strong system could combine both.

- **import-linter / dependency-cruiser** (hand-written import rules, checked in
  CI). *Difference:* manual rules, imports only, no per-PR delta. *Is ARCHON
  better?* Broader on every axis (auto-derived, many edge kinds, delta). *Honest
  caveat:* for a team that just wants "package A must not import B," import-linter
  is simpler and sufficient.

- **Reflexion Models** (intended-vs-actual architecture; the intellectual
  ancestor). *Difference:* needs a hand-drawn high-level model and works on a
  snapshot, not per-PR. *Is ARCHON better?* Yes for this use — it removes the main
  adoption barrier (the hand-drawn model) and makes conformance a per-change
  review instrument rather than a periodic audit.

**Bottom line to state plainly:** no single competitor has all four of
{auto-recovery, per-PR delta, many typed edge kinds, tied-to-evidence}. Each has a
subset. ARCHON's contribution is the *combination* — and each competitor may still
win in its own narrow niche, which we should say out loud rather than overclaim.

---

## 3. Concrete case studies (what we actually have)

### [DONE] todo-api (MarioCarrion/todo-api-microservice-example) — ground truth
Chosen because each architectural feature is documented per-commit → free labels.
Real deltas we ran and verified:
- **Cache-aside** `d64dc8b → b578358`: new `memcached` box + `service:Memcached`;
  `implements` edges = the cache-aside decorator (memcached.Task ⊨
  service.TaskSearchRepository; elasticsearch.Task ⊨ memcached.Datastore); contract
  coverage flags **no test guards those interfaces (evidence gap)**.
- **Kafka** `8f7d667`: `+ service:Kafka`, `cap:syscall`; RabbitMQ indexer removed.
- **Secure Config / Vault** `b0867ac`: `+ service:Vault`, `+ cap:net`.
- **OpenAPI adoption** `db5a098`: 5 endpoint boxes + caught an **endpoint rename**
  (`- api:POST /search/tasks` → `+ api:POST /tasks/search`); schema delta showed
  new `SearchTaskJSONBody.*` payload fields.

### [PARTIAL] BLIS (inference-sim, Go) — clean ports-and-adapters subject
- Clean `sim` core contracts + kv/latency/saturation adapters + cmd root.
- Rich invariant surface (golden/parity/`TestProperty_*`); invariants are named
  tests (no property-testing library).
- Extraction this cycle: 2226 invariants, **257 bound to a contract**; extract
  ~1.4s; same-commit delta empty (no structural regression).
- **[re-confirm]** Empty-delta fraction ≈ **57% (46/80 PRs)** from an earlier run —
  needs a re-run with the current edge set before citing.

---

## 4. Evaluation design

### Repos (subjects)
- **BLIS** (Go, clean, invariant-rich) — primary. **[PARTIAL]**
- **todo-api** (Go, documented per-commit changes) — ground-truth labels.
  **[PARTIAL]**

### What we run it on (in plain English)
We evaluate **one merged pull request at a time**. For each PR we take the code
*just before* it (its starting point) and the code *after* it merged, run ARCHON
on both, and look at the delta between them.

We deliberately test a **mix** of PR types, so we're not only testing easy cases:
- small bug fixes,
- pure refactors (code moved around but behavior unchanged),
- dependency bumps,
- changes that add/modify a config key, a backing service, an API endpoint, or a
  data schema,
- performance tweaks,
- test-only changes,
- big architecture reorganizations.

Mixing types lets us see both where ARCHON helps and where it over-fires (flags a
routine change) or under-fires (misses a real one).

### Metrics (plain English) — and the ones worth leading with
What we measure, why it matters, and where the idea comes from (cited in
brackets):

- **Empty-delta fraction** — of all PRs, how many come out "empty" (no boundary
  moved, no architecture review needed). Shows how much review load ARCHON can
  safely take off the table. *(Our own core metric.)*
- **Delta compression** — how much smaller the architectural delta is than the
  textual diff (e.g., 1,900 changed lines → "empty," or → 2 arrows). Shows we
  shrink what a reviewer has to reason about. *(Compression framing; related to
  impacted-set size in change-impact analysis [CHID, EMSE 2025].)*
- **False-negative rate (the safety number)** — how often an "empty" verdict
  hides a change that humans judged architectural. This is the number that tests
  the safety claim; it must be near zero. *(The critical recall metric.)*
- **Routing accuracy** — does the report flag the same owners / tests / reviewers
  that maintainers actually pulled in? Shows the delta is *actionable*, not just
  descriptive. *(Analogous to change-propagation prediction [Hassan & Holt, ICSM
  2004].)*
- **Extraction precision & recall, per edge kind** — of the boxes/edges/surfaces
  ARCHON reports, how many are correct (precision) and how many real ones did it
  find (recall). Standard for structural tools *(cf. refactoring-detection
  precision/recall [RefactoringMiner, TSE 2022])*.

For the **AI-reviewer story** (arguably the most important for the positioning):
- **Token cost** — input tokens an AI reviewer uses with vs without ARCHON's
  delta. A structured artifact should cost *fewer* tokens than dumping files.
  *(Repo-graph-as-context cut tokens ~26% [LLM Agents Can See Code Repositories,
  ASE 2026].)*
- **Review accuracy** — does the AI reviewer catch more real issues / make better
  accept/reject calls when given the delta? *(Issue-resolution-style success rate
  [SWE-bench, ICLR 2024].)*

**Recommended headline set:** empty-delta fraction + delta compression +
false-negative rate (for RQ2, review value), and token cost + review accuracy with
vs without ARCHON (for the AI-reviewer claim). Those four to six numbers tell the
whole story.

### Scorers — who (or what) decides if ARCHON is right (plain English)

Different questions need different judges:

- **Ground truth from the maintainers' own docs** (todo-api). The maintainers
  wrote, per commit, what changed architecturally — so we use that as the answer
  key. No judgment call needed; it's documented.
- **Human labelers** (at least 2 people) for PRs without docs. Each reads the PR
  and tags it: *routine / boundary-changing / architecture-significant*. Two
  labelers so we can measure how much they agree (inter-rater agreement, e.g.
  Cohen's kappa) — if humans can't agree, the label isn't trustworthy. Humans are
  the gold standard for "was this actually architectural."
- **The safety check** = compare ARCHON's "empty" verdict against the human
  "architectural" label. Every case where ARCHON said empty but a human said
  architectural is a **false negative** — the number that matters most.
- **An automated referee for the agent experiment (the "oracle"), explained
  simply.** When an agent rewrites the *inside* of a box and ARCHON says "boundary
  unchanged → safe to merge," we need an *independent* way to confirm the behavior
  really didn't change — one that doesn't just trust ARCHON's own check. That
  independent referee runs the box's public behavior three ways:
  - **property tests** — throw lots of random inputs and check the rules that must
    always hold still hold;
  - **metamorphic tests** — check relationships that must survive any correct
    implementation (e.g. "sorting then filtering = filtering then sorting");
  - **differential tests** — run the *old* and *new* versions on the same inputs
    and check they produce identical outputs.
  If any of these catches a difference, the "safe" verdict was wrong (an escaped
  regression). "Contract-blind" just means this referee doesn't know or use
  ARCHON's contract — it's a fully independent second opinion.

**Do we use an LLM as judge, or a human?**
- **Correctness / the safety number → humans + maintainer docs.** LLM-as-judge is
  too unreliable to be the final arbiter of whether a change was architectural.
- **Behavior-preservation (agent experiment) → automated oracles**, not a judge —
  we want an objective, reproducible pass/fail, not an opinion.
- **LLM's roles are elsewhere:** (a) as a *scale helper* — pre-label a large batch
  of PRs, then have humans verify a sample; and (b) as the *subject under test* —
  the AI reviewer we're trying to help (with vs without ARCHON). An LLM can assist
  labeling and *is* the thing we're improving, but it is **not** the final judge of
  correctness.

### Competitors / baselines — and why we compare against each
- **Textual diff (lines/files changed)** — the status quo every reviewer uses
  today. This is the baseline we *must* beat to justify existing at all: it shows
  the compression (big diff → empty delta) and the hidden-change catch (small diff
  → surfaced boundary move).
- **import-linter / dependency-cruiser** — represents current "structural
  dependency check" practice. Comparing shows ARCHON catches boundary changes that
  are invisible to imports (interfaces, services, APIs, schemas).
- **Arch-Engine / Erode** — the closest tools that also diff PR-vs-base
  architecturally; the fairest apples-to-apples comparison. Shows the concrete
  value of auto-recovery (no model to maintain) + the extra edge kinds + contracts.
- **CHID** — the change-impact / risk-score baseline. Shows the difference between
  "here's a risk number" and "here's the architectural change + the missing
  evidence."
- **LLM reviewer *without* ARCHON vs *with* ARCHON's delta** — the AI-era
  comparison, and probably the most persuasive for the positioning: does handing
  the model the architecture delta make its review more accurate and cheaper
  (tokens)? **RepoReviewer** (a multi-agent PR reviewer) is a good off-the-shelf
  "LLM reviewer" to use as that baseline.

---

## 5. Implementation — the graph today (package altitude)

**Nodes**
- Internal package (a box), with attributes: `surface` (exported symbols),
  `allow`-list (structural contract), `invariants` (guarding tests, each carrying
  `Guards` = interfaces it exercises + `Exercises` = concrete types it drives).
- External package (third-party import).
- Synthetic operational nodes: `service:Postgres/Kafka/…`, `api:METHOD /path`,
  `cap:net/unsafe/…`, `env:KEY` / `flag:NAME`.
- **Schema** is tracked as a separate *axis* on a box (serialized struct fields),
  not a node — because field-level changes are witness-invisible to edges.

**Edges (typed, directed, witnessed)**
- Compiler edges: `import`, `call`, `implements`.
- Operational edges: `config`, `service`, `protocol`, `capability`.
- Each edge carries a **witness set** (the files/symbols that realize it), so a
  witness-only change (file move, body rewrite) leaves the edge intact → the delta
  stays **empty**.
