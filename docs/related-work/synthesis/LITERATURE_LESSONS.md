# What the Literature Says About ARCHON's Design Decisions

Synthesis of a deep read of the related work, organized around the open
questions. Sources cited inline. Where a claim is from established knowledge
rather than a fresh read, it's marked *(general)*.

---

## 1. What nodes/edges should ARCHON have, for *our* purpose (review)?

**What recurs across code-graph representations** (Code Property Graph;
Allamanis, ICLR 2018; GraphCodeBERT, ICLR 2021):
- Nodes: syntax nodes, variables/identifiers, methods/functions, types.
- Edges: AST/containment, control flow (CFG), **data flow** (last-read/last-write/
  reaching-def), **call**, **inherits/implements**, control dependence.

**Which edges carry the most signal** — measured, not asserted:
- Allamanis's ablation: syntax-only (AST) edges gave **55.3%** on their bug task;
  adding **data-flow/semantic edges → 85.5%**. Data-flow and call edges are the
  high-signal ones.
- GraphCodeBERT deliberately used **only data-flow** (a subset), not the full
  AST — "AST even hurts performance when sequence length is short."

**Takeaway for ARCHON:** the papers validate our high-signal edges — `call` and
`implements` are exactly the semantic/hierarchy edges shown to matter, and
`import` alone is the low-signal baseline. Our **operational edges (service, API,
config, capability) are NOT in the academic papers** — those focus intra-program;
this is genuine differentiator territory.

**Honest gaps the papers surface** (candidate additions, not urgent):
- **Interface method *signatures*.** We track "T implements I" but not "I requires
  method M with signature S." A signature change is a contract change invisible to
  the implements edge. *(CPG models METHOD_PARAMETER_IN; Allamanis has
  FormalArgName/ReturnsTo.)*
- **Backward/inverse edges** (`implemented-by` alongside `implements`). Allamanis
  doubled edges with inverses to propagate information faster; makes "who
  implements this contract?" a direct query.
- Data-flow / control-dependence at the function level — **likely overkill** for
  package-altitude review; note and skip.

**What reviewers actually flag as architectural** (this should drive edge
coverage — from the OpenStack erosion studies, Li et al. ICSA 2022 / IST 2023):
layer violations (15.9%), **duplicate functionality (15.5%)**, cyclic
dependencies (11.9%), obsolete functionality (8.3%), functionality overload,
unwanted dependency, unused/ambiguous interface, API-spec violations,
cross-service problems. Mapping to ARCHON:

| Reviewer concern | ARCHON today | Status |
|---|---|---|
| Cyclic dependency | import/call cycle detection | ✅ covered |
| Unwanted dependency | import/call | ✅ covered |
| Unused interface | implements-edge absence | ✅ covered |
| API-spec / cross-service | api / service / protocol edges | ✅ covered |
| Layer violation | import/call **+ layer labels** | ⚠ needs layer annotation |
| Duplicate functionality | — | ❌ needs semantic similarity |
| Obsolete functionality | — | ❌ needs dead-code/orphan analysis |
| Ambiguous interface (god method) | — | ❌ needs signature analysis |
| Design-decision / intent violation | — | ❌ needs the intent baseline (ratchet) |

→ We cover the *dependency and interface/service* concerns well; the honest
misses are **duplicate/obsolete functionality** and **intent violations**.

---

## 2. Is a graph the right artifact to show a *human* reviewer?

Short answer: **the graph is the right internal model; the right *artifact* is a
focused, single-altitude view of the *delta*, not the whole graph.**

- Real reviewers *do* care about architectural concerns — the erosion studies
  found hundreds of review comments about layer violations, cyclic deps, interface
  problems, etc. So an architecture-level artifact addresses a real need
  (Li et al. ICSA 2022). This is the strongest evidence the artifact is wanted.
- But a big boxes-and-arrows graph overwhelms. The field's answer is
  **abstraction**: *(general)* the **C4 model** shows one *level* at a time
  (context → container → component → code); **DSM** work (Sangal, OOPSLA 2005)
  uses a **matrix** instead of boxes-and-arrows precisely because it scales to
  large systems where node-link diagrams turn to spaghetti; **Reflexion Models**
  show a *mapped* high-level view (convergences/divergences/absences), not raw
  code.
- **Implication:** show the **changed neighborhood at the package altitude**, plus
  a plain-language delta ("+ service:Kafka; memcached now implements the store
  contract; no test guards it"). The full graph is a drill-down, not the default.
  This matches what ARCHON's `render` + text delta already do.

→ Graph = right substrate; the human-facing artifact should be the **projected,
changed-only delta** + a short textual summary, with the diagram as optional
zoom.

---

## 3. Is a graph good for LLM understanding? And why not trees?

**Why not trees.** An AST *is* a tree, and tree methods (code2vec/code2seq, POPL
2019 / ICLR 2019; GumTree, ASE 2014) work well for *local, single-unit* things:
syntax structure, method-name prediction, edit scripts. But the relationships
that matter for architecture — **calls, data flow, imports, interface-implements,
control dependence — cross the tree** (they connect nodes that aren't
parent/child). Allamanis: long-range dependencies "using the same variable or
function in distant locations" are what tree models miss. So: **a tree is enough
for one file's syntax; a graph is required the moment you reason about
relationships between units** — which is all of architecture review.

**Is a graph good for LLMs? Yes, but *how* you feed it is decisive** (and this
directly shapes ARCHON's AI-reviewer design):
- Raw serialized graphs **fail** on real reasoning: NLGraph (NeurIPS 2023) —
  Hamilton path 8%, GNN-simulation 0% exact; accuracy drops >40% as the graph
  grows. Talk-like-a-Graph (ICLR 2024) — LLMs **can't reason about absence**
  (disconnected-nodes ~0%) and impose structural priors regardless of the data.
- **Encoding matters enormously:** incident/relationship phrasing ("A calls B")
  scored **53.8%** vs adjacency-matrix **19.8%** on the same task (Talk like a
  Graph). → Feed the delta as *"module A now calls module B"*, never as a matrix.
- **What works:** retrieved *subgraph* as text (RepoGraph — 1-hop ego-graph, ~12
  nodes, beats 2-hop), or structure *injected* via an adapter + graph-attention
  mask (CGM, NeurIPS 2025 — compresses ~512 tokens into 1 node token, 43% on
  SWE-bench Lite), or **pre-computed summaries** (GraphRAG).
- **The load-bearing lesson:** don't ask the LLM to *run graph algorithms*
  (cycles, reachability, "is this boundary preserved") — **compute those with a
  tool and hand the LLM the answer as facts**, then ask it the *semantic* question
  ("is this new dependency appropriate? what invariant might break?"). This is
  exactly ARCHON's division of labor.

---

## 4. What if the graph becomes too big? What do the papers do?

Nobody renders the whole thing. Concrete techniques, by source:
- **Show one level at a time** — C4 hierarchy *(general)*.
- **Automatic clustering** into higher-level modules — Bunch (Mancoridis, IWPC
  1998), ACDC (Tzerpos & Holt, WCRE 2000) *(general)*.
- **Matrix aggregation** for very large dependency sets — DSM (Sangal 2005)
  *(general)*.
- **k-hop ego-graph from the changed nodes** — RepoGraph: 1-hop (~12 nodes) is
  best; 2+ hops "introduce noise." *This is the key one for a per-PR delta.*
- **Retrieve-then-rerank / coarse-to-fine** — CGM's R4 chain; GraphCoder's
  coarse-to-fine with decay-by-distance.
- **Community detection + hierarchical summaries + map-reduce** — GraphRAG scales
  to 1M+ token corpora by summarizing clusters.
- **Pattern-guided expansion** — RepoHyper reaches 73% coverage exploring only 28%
  of nodes.
- Note: **no paper uses PageRank** for this; ranking is by semantic similarity,
  link-prediction, or subgraph edit distance. (Aider's repo-map does use PageRank
  in practice.)

→ **For ARCHON:** the answer is built-in — the delta is already the *changed
neighborhood*, and witnesses keep it minimal. For big deltas: project to package
altitude, show only changed boxes + their 1-hop neighbors, cluster/summarize the
rest. Don't dump the whole graph to a human or an LLM.

---

## 5. Is it novel? Is the novel aspect impactful?

**Novel — yes, as a combination.** Precise competitor computation
(from the tool/paper docs):

| | Model source | Edge kinds | Delta semantics | Evidence/contract |
|---|---|---|---|---|
| **Erode** | hand-drawn (LikeC4) | model-defined | vs-intent | no |
| **Arch-Engine** | auto (package.json) | dependency only, JS/TS | PR-vs-base | no |
| **CHID** | auto (call graph) | calls only | impact/risk score | no |
| **ARCHON** | **auto (source)** | **import/call/implements/service/api/config/capability/schema** | **witness-based (refactor = empty)** | **yes (test→interface)** |

No tool has all of {auto-recovery, many typed edges, witness-based empty-delta,
evidence/contract tracking}. Each has a subset.

**Impactful — yes, and the erosion data proves it.** The concerns real reviewers
raised map onto exactly what ARCHON's *extra* capabilities catch and the others
miss:
- **Cross-service / interface violations** — ARCHON's `service`/`api`/`implements`
  edges model them directly; Arch-Engine has no service concept, CHID no service
  model, Erode needs the model to declare them.
- **Untested boundaries** — only ARCHON surfaces "no test guards this interface"
  (the todo-api cache-aside evidence gap); reviewers *do* flag this, no competitor
  reports it.
- **Refactor vs. real change** — witness-based empty-delta distinguishes churn
  from a boundary move; none of the three do.

**The honest positioning:** the novelty is NOT "show a graph of PR changes"
(table-stakes: CodeSee, CodeRabbit) — it's the *typed, witness-based, evidence-
tied architectural delta* + the *measurement* that it helps review.

---

## 6. Things we learned that we're maybe NOT thinking about

1. **Encoding for LLMs is a first-class design choice.** If we feed the delta to
   an AI reviewer, phrase it as relationships ("A now calls B") and **precompute
   the structural facts** (cycles, new cross-layer deps) — don't hand it a raw
   graph and ask it to reason. (NLGraph, Talk like a Graph.)
2. **Compute vs. ask split.** The LLM should answer *semantic* questions
   (appropriateness, risk, missing invariants); the *tool* must answer structural
   ones (LLMs demonstrably fail at graph algorithms and can't reason about
   "what's NOT connected").
3. **Interface signatures, not just implements.** A method-signature change on a
   contract is invisible to our current implements edge — worth adding.
4. **Duplicate & obsolete functionality are the #2/#4 things reviewers flag** and
   we can't currently detect them (they need semantic-similarity / dead-code
   analysis, not edges). Either add a capability or scope them out explicitly.
5. **Intent vs. actual.** We capture "what is"; reviewers flag "violates what we
   decided." Our snapshot-and-ratchet baseline is the mechanism — make sure it's
   in the story, because ~1/5 of erosion comments are intent/decision violations.
6. **Backward edges are cheap and useful** for querying ("who implements this?").
7. **Keep the package altitude.** GraphCodeBERT shows more granularity isn't free;
   package-level is the right abstraction for review and keeps the graph small.

---

### Access caveats (honesty)
Full text read on arXiv HTML: Allamanis 2018, GraphCodeBERT, code2vec, CGM,
RepoGraph, RepoHyper, GraphCoder, GraphRAG, NLGraph, Talk-like-a-Graph, the two
OpenStack erosion studies. Read via spec/secondary sources: Code Property Graph
(Joern spec), Garcia smells, Erode/Arch-Engine/CHID (docs/abstracts). Could not
fully access: ProgQuery, ChangeDistiller, code2seq full text, and the C4/DSM/
Reflexion/Bacchelli-Bird cluster (paywalled) — those points are marked *(general)*
and rest on established knowledge, not a fresh read.
</content>
