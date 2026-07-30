# Graphs, LLMs, and How to Design the Architecture Graph

Two questions:
1. What are LLMs actually good at, and is a **graph** the right representation
   for this problem? (with papers)
2. If I'm building architecture graphs, **how should they look** — nodes, edges,
   features, what to keep in mind, and what scope?

---

# Part 1 — What LLMs are good at, and whether a graph is right

## 1.1 The short version

- LLMs are **strong** at *local, textual, self-contained* things: read this
  function, summarize this file, write this snippet, explain this diff.
- LLMs are **weak** at *global, structural, cross-file* things: "what calls this
  and what breaks if I change it," "does this cross a layer boundary," "what
  depends on this service." That's because a transformer reads a flat sequence of
  tokens; following a dependency across ten files is not something text position
  gives it.
- A codebase is **naturally a graph** (imports, calls, interfaces, data flow),
  and the review question is a **graph question**. So the mismatch is exactly:
  *the problem is relational, the model is sequential.*

**Conclusion:** a graph is the right representation for the *problem*. But — and
this is the subtle part — that does **not** mean you hand the LLM a giant graph
and ask it to reason over it. It means you **compute the structure with a tool**
and hand the LLM a small, distilled result. That is precisely what ARCHON does.

## 1.2 Evidence that structure/graphs help code models

Two eras. First, ML models that *learn from* code graphs; then, LLMs that are
*given* code graphs.

**Learning from code structure (pre-LLM, still the foundational evidence):**
- **code2vec / code2seq** (Alon et al., POPL 2019 / ICLR 2019) — represent a
  method as paths through its **syntax tree**; these structural paths predict
  method names and summaries far better than treating code as plain text. Early,
  clean evidence: *structure carries the signal.*
- **GraphCodeBERT** (Guo et al., ICLR 2021) — pre-trains a code model using
  **data-flow** (a semantic graph), not just tokens. Beats token-only models on
  code search, clone detection, translation, and refinement. The canonical "add
  the graph, get better understanding" result.
- **Universal Representation for Code** (Liu et al., PAKDD 2021 — in your refs) —
  pre-trains **graph neural networks** on a control/data-flow code graph to get
  general code embeddings. Same lesson, GNN flavor.

**Giving LLMs the structure (the current, directly-relevant evidence):**
- **Code Graph Model / CGM** (Tao et al., NeurIPS 2025 — in your refs) —
  integrates the repository's **structural dependency graph** into an LLM (graph-
  RAG) and improves repo-level software-engineering tasks. Direct evidence that
  explicit code graphs help *LLMs*, not just GNNs.
- **"LLM Agents Can See Code Repositories"** (the one you cited) — giving an agent
  a **visual repository graph** as context cut token use by ~26% while keeping or
  improving issue-resolution accuracy. Structure is not just more accurate, it's
  *cheaper* (you don't dump the whole repo into the prompt).
- **GraphRAG** (Edge et al., Microsoft, 2024) — retrieval over a **knowledge
  graph** beats flat text-chunk retrieval for *global / holistic* questions.
  Analogy for you: an architecture graph is a knowledge graph of the codebase, so
  "what changed architecturally" is exactly the kind of global question graph-RAG
  is better at than dumping files.
- **RepoCoder** (Zhang et al., EMNLP 2023) — repo-level completion via iterative
  retrieval. Reinforces that *picking the right context* (which structure helps
  you find) beats brute-force scale.
- **Practitioner evidence — Aider's "repo map"** — a popular AI coding tool builds
  a symbol graph with tree-sitter and **ranks it (PageRank)** to choose what to
  show the model. Real-world confirmation that a *ranked graph* of the repo is how
  you feed an LLM a big codebase.

## 1.3 The catch: LLMs are poor graph *engines*

If graphs help, why not just serialize the whole graph into the prompt and let
the model reason? Because LLMs are unreliable at actually *running graph
algorithms*:
- **NLGraph** ("Can Language Models Solve Graph Problems in Natural Language?",
  Wang et al., NeurIPS 2023) — LLMs have *limited* graph-reasoning ability;
  they're okay on tiny graphs and simple queries, but degrade fast on larger
  graphs and multi-hop reasoning.
- **Talk like a Graph** (Fatemi, Halcrow, Perozzi, ICLR 2024) — how you *encode*
  a graph as text dramatically changes LLM performance. There's no free lunch:
  the model isn't robustly reading graph structure; it's pattern-matching on your
  encoding.

**So the reliable pattern is:** don't ask the LLM to *compute* over the graph.
Compute deterministically (extraction, diff, projection, coverage), then give the
LLM a **short, focused, natural-language artifact** to explain or judge.

## 1.4 What this means for ARCHON (your architecture is already the right shape)

The winning recipe the literature points to — and what you're building:

> **Graph tool does what tools are good at** (exact structure, diffs, reachability)
> → **LLM does what LLMs are good at** (explanation, judgment, prose).

- Represent the codebase as a graph (right representation for the problem). ✅
- **Compute the delta deterministically** — don't make the model diff two repos in
  its head (avoids the NLGraph failure mode). ✅
- Hand the reviewer (human *or* LLM) the **distilled delta + evidence gaps** — a
  small artifact, not the whole graph (matches CGM / GraphRAG / repo-map). ✅
- This is "tool-augmented review": the same reason coding agents call compilers
  and test runners instead of predicting their output.

**One-liner for the talk:** *A repo is a graph and review is a graph question, but
LLMs are weak graph engines — so ARCHON computes the architectural delta with a
tool and hands the model the answer, not the maze.*

### Reading list (Part 1)

| Paper | Year | Why it matters to you |
|---|---|---|
| code2vec / code2seq (Alon et al.) | 2019 | Structure (AST paths) beats tokens for code understanding |
| GraphCodeBERT (Guo et al.) | 2021 | Data-flow graph improves code models — foundational |
| Universal Representation for Code (Liu et al.) | 2021 | GNN embeddings from code graphs (in your refs) |
| RepoCoder (Zhang et al.) | 2023 | Right context > raw scale for repo-level tasks |
| NLGraph (Wang et al.) | 2023 | LLMs are weak at graph reasoning — don't offload the algorithm |
| Talk like a Graph (Fatemi et al.) | 2024 | Graph→text encoding hugely affects LLM performance |
| GraphRAG (Edge et al., Microsoft) | 2024 | Graph retrieval beats flat chunks for global questions |
| CGM (Tao et al.) | 2025 | Graph-in-LLM helps repo-level SE tasks (in your refs) |
| "LLM Agents Can See Code Repositories" | 2024/25 | Repo graph as context: −26% tokens, ≥ accuracy (you cited) |
| Aider repo map (practitioner) | — | Ranked symbol graph is how real tools feed LLMs a big repo |

---

# Part 2 — How the architecture graph should look

## 2.1 The single most important decision: **altitude**

Everything else follows from *how zoomed-in the graph is*.

- **Too low** (statements/expressions, like Code Property Graph / ProgQuery) →
  great for finding a bug in a line, useless for review: too noisy, a refactor
  looks like an earthquake.
- **Too high** (whole service = one box) → nothing shows up; every PR "touches
  the service."
- **Right for review** = the altitude a human draws on a whiteboard:
  **packages / modules / components**. That's where ARCHON sits.

The deeper idea from `main.tex`: build **one hierarchical graph** (repo ⊇ package
⊇ file ⊇ function) and **project** to whatever altitude the consumer needs —
package altitude for human review, function altitude for an agent editing code.
The coarse arrows are just the **aggregation** of the fine ones, so the two views
can't disagree.

> **Rule of thumb:** pick the altitude of your *consumer's question*. Reviewer →
> package. Agent → function. Start at package; add finer altitudes only when a
> use case demands it.

## 2.2 Nodes — what to include

Two kinds:

1. **Code units** (the boxes): packages/modules. These are the obvious ones.
2. **The outside world, as first-class nodes** — this is what makes an
   *architecture* graph instead of a *dependency* graph:
   - external services (`service:Postgres`),
   - config keys (`env:REDIS_ADDR`),
   - API endpoints (`api:POST /tasks`),
   - schema entities (a wire/DB payload),
   - capabilities (`cap:net`, `cap:unsafe`).

> **Principle:** *anything a change can depend on that a reviewer cares about
> should be a node* — even if it isn't code. The review-relevant boundary is
> often not a source import.

**Node identity must be stable under rename/move.** If identity = file path, then
moving a function reads as *delete + create* and every delta is noise. Use:
- package altitude → the **import path** (already stable), and
- leaf altitude (later) → stable symbol IDs or tree-diff matching (GumTree-style).

Each node also has a **surface**: the subset of it that others may depend on
(exported API). Contracts attach to the surface, not the internals.

## 2.3 Edges — typed, directed, and *witnessed*

Three properties make edges useful:

1. **Typed.** The *kind* of edge is the review signal. Changing a `call` edge is
   not the same event as changing a `permission` or `service` edge. ARCHON's
   kinds: `import`, `call`, `implements` (compiler edges) + `config`, `service`,
   `protocol`, `capability` (operational edges). Distinguish:
   - **Compiler edges** — mechanically exact (the type checker guarantees them).
   - **Operational edges** — the real review boundary that *isn't* a source
     import; usually heuristic, so curate them (see 2.5).
2. **Directed.** A depends on B ≠ B depends on A.
3. **Witnessed** — *the feature that makes deltas meaningful.* Each edge remembers
   *what realizes it* (which files/symbols). Then a change that only shuffles
   witnesses (rename a file, rewrite a body) leaves the edge intact → the delta is
   **empty**. This is how a 2,000-line PR correctly reads as "no architectural
   change." Without witnesses you can't tell a real boundary move from noise.

Coarse edges are the **aggregation** of fine edges (projection), so the package
arrow is literally "some function in A calls some function in B."

## 2.4 Features / attributes to carry

On **nodes**:
- `surface` — the public API others depend on.
- `internal/external` and `kind` (package vs service vs endpoint vs config…).
- `structural contract` — the allow-list of what it may depend on.
- `behavioral contract / invariants` — the guarantees + the tests that guard them.
- `owner` — for routing the review to the right people.

On **edges**:
- `kind`, `direction`, `witnesses` (and optionally count/strength).

Crossing both — the differentiator — **evidence obligations**: each boundary can
declare *what evidence is required* before a change to it is trusted (a test, a
replay trace, a benchmark, a human approval). This is what turns a dependency
graph into a *contract* graph, and it's where your contract-coverage work lives.

## 2.5 Ten things to keep in mind (design principles)

1. **Extract, don't ask.** Recover the graph from source automatically. The moment
   it's hand-maintained, it drifts and dies (the weakness of Reflexion/Erode).
2. **Faithful by construction.** Regenerate from source on every change; the
   diagram can't lie because it's never edited by hand.
3. **Typed + witnessed.** Types say *what kind* changed; witnesses say *whether it
   really changed* (the empty-delta property).
4. **Right altitude + projection.** One graph, many zoom levels; don't drown the
   reviewer in statements.
5. **Stable identity.** Refactors must not look like rewrites.
6. **Model non-code boundaries.** Config, service, API, schema, capability — that's
   where real review risk hides.
7. **Separate structure from behavior.** Structural checks are mechanical, total,
   fast (run on every push); behavioral/evidence checks are slower and partial
   (run when a contract is implicated). Keep them different gates.
8. **Precision vs. noise is the whole game for operational edges.** A heuristic
   edge that fires everywhere is worse than nothing — reviewers tune it out.
   Curate; report your false-positive story; note when payoff is per-repo (your
   own finding: config edges lit up todo-api but not config-light BLIS).
9. **Language-neutral core, language-specific extractors.** The graph *model* is
   universal; only extraction is per-ecosystem (Go/Rust/Python).
10. **The unit of review is the delta, not the graph.** Design everything around
    "what changed in this PR," because that's the artifact people consume.

## 2.6 Scope — what to build, defer, and claim

**In scope (you have most of this):** package-altitude typed graph; per-PR delta
with empty-delta detection; the operational edges (config/service/protocol/
schema/capability); structural allow-list; contract coverage (prototype).

**Deliberately defer (say so, don't pretend):**
- Function/leaf altitude + rename/move matching (GumTree) — only when an agent use
  case needs it.
- Full *behavioral* contract verification (running the bound tests as evidence).
- Permissions edges (barely exist in Go — wait for a Rust subject).
- Deployment topology (Dockerfiles/compose) — lower precision.

**Scope the claim, not just the code:**
- **Flagship (safe):** "architectural delta improves review" — demonstrable now.
- **Frontier (conditional):** "empty boundary delta → safe for an agent to
  auto-merge" — state the assumptions; don't over-claim.

## 2.7 Vocabulary to borrow (so reviewers recognize your model)

- **C4 model** (Brown) — levels of architecture (context/container/component/code)
  → your altitude/projection story.
- **Parnas 1972 (information hiding)** — modules hide decisions behind a surface →
  your surface/encapsulation and behavioral-contract-at-the-surface rule.
- **Design Structure Matrix** (Baldwin & Clark 2000; MacCormack 2006; Sangal 2005)
  → your graph is essentially a *typed, versioned, per-PR-diffed DSM*.
- **Reflexion Models** (Murphy 1995) — intended vs. actual → your snapshot baseline
  (but auto-recovered, not hand-drawn).
- **Code Property Graph** (Yamaguchi 2014) — the "don't go too low" cautionary
  altitude.

## 2.8 A concrete starter schema (what a record should hold)

```
Node {
  id            // stable: import path (pkg) / symbol id (leaf) / "service:Redis"
  kind          // package | service | endpoint | config | schema | capability
  altitude      // repo | package | file | function
  internal      // ours vs external
  surface[]     // exported API others may depend on
  contracts {
    allow[]        // structural: what it may depend on
    invariants[]   // behavioral: guarantees + guarding tests
    evidence[]     // (property, required-evidence-type)
  }
  owner
}

Edge {
  from, to
  kind          // import | call | implements | config | service | protocol | capability
  witnesses[]   // what realizes it (files/symbols) -> enables empty-delta
}
```

Everything you've built already fits this shape — which is a good sign the model
is coherent. The next enrichments (leaf altitude, behavioral evidence execution)
slot in without changing the schema.
