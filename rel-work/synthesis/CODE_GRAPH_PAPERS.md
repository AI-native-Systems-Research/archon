# Six Papers, Explained Simply — Code Graphs, LLMs, and Architecture Recovery

Plain-language notes on six papers, each with: the **problem**, their **solution**,
their **contributions**, **what question it answers**, its **venue/year**, and
**what ARCHON can take from it**.

> Venue note: I'm confident on Ducasse & Pollet (TSE 2009), CGM (NeurIPS 2025),
> GraphCoder (ASE 2024), and Code Property Graph (IEEE S&P 2014). RepoGraph
> (ICLR 2025) and RepoHyper (2024 preprint) I'm fairly sure of but worth a quick
> double-check before you cite them.

They fall into **three groups**:
- **Positioning** — where ARCHON sits in the field: *SAR Taxonomy*.
- **Foundational representation** — code as a typed graph: *Code Property Graph*.
- **Graphs feeding models** (the modern cluster, all saying the same thing) —
  *CGM, RepoGraph, RepoHyper, GraphCoder*.

---

## GROUP 1 — Positioning

### 1. Software Architecture Reconstruction: A Process-Oriented Taxonomy
**Ducasse & Pollet — IEEE Transactions on Software Engineering (TSE), 2009.**

- **Problem.** Lots of people have built ways to *recover* a system's
  architecture from its code and other artifacts, but the methods are scattered
  and hard to compare — there's no shared map of the field.
- **Solution.** A **taxonomy** (a classification) that sorts every Software
  Architecture Reconstruction (SAR) approach along five axes:
  1. **Goals** — *why* recover it (re-documentation, conformance checking,
     evolution/co-evolution, analysis, reuse).
  2. **Process** — *direction*: bottom-up (from code), top-down (from a
     hypothesis), or hybrid.
  3. **Inputs** — *what you feed it*: source code, runtime traces, version
     history, human knowledge, physical layout.
  4. **Techniques** — *how*: manual → semi-automatic → automatic; clustering,
     queries, pattern recognizers, etc.
  5. **Outputs** — *what you get*: views, diagrams, formal models.
- **Contributions.** A common vocabulary and map for the whole SAR field, so any
  tool can be placed and compared, and gaps become visible.
- **Question it answers.** *"What are all the ways people reconstruct architecture
  from code, and what design choices does such a tool have?"*
- **What ARCHON takes from it (high value — positioning).** ARCHON **is** an SAR
  tool, so this is the map you locate yourself on. In this paper's language,
  ARCHON is: **bottom-up** (from source), **source-code-driven** (plus config /
  history), **quasi-automatic** (mechanical extraction — no hand-drawn model),
  aimed at **conformance + co-evolution**, producing a **typed graph + a per-PR
  delta**. The gap you fill: classic SAR is mostly a *one-time re-documentation
  snapshot*; the taxonomy names "co-evolution" as a goal but few tools deliver it.
  ARCHON delivers exactly that — architecture recomputed and **diffed per change**,
  with **evidence** attached. Great sentence for your related-work section:
  *"In Ducasse & Pollet's terms, ARCHON is bottom-up, source-driven,
  quasi-automatic SAR for conformance and co-evolution — but computed per pull
  request rather than as a one-off snapshot."*

---

## GROUP 2 — Foundational representation

### 2. Modeling and Discovering Vulnerabilities with Code Property Graphs
**Yamaguchi, Golde, Arp, Rieck — IEEE Symposium on Security & Privacy (S&P / "Oakland"), 2014.**

- **Problem.** To find security bugs automatically you often need to reason about
  three things at once — the code's **structure**, its **execution order**, and
  its **data flow** — but those lived in three separate representations, so no
  single query could span them.
- **Solution.** Merge the three classic code graphs into **one** graph — the
  **Code Property Graph** — combining the **AST** (syntax/structure), the
  **Control-Flow Graph** (execution order), and the **Program Dependence Graph**
  (data + control dependencies). Store it in a graph database and write
  **traversal queries** to find vulnerable patterns (e.g., "untrusted input
  reaches a dangerous call without sanitization").
- **Contributions.** A single unified, typed graph of code; the idea that bug
  patterns can be written as **graph queries**; a way to do it at scale.
- **Question it answers.** *"How do you represent code as one graph that captures
  structure + flow, and can you find bugs by querying it?"*
- **What ARCHON takes from it (foundational).** This is the canonical "code as a
  **typed** graph" paper — proof that typed, queryable code graphs are powerful.
  Two lessons: (1) **types on nodes/edges carry meaning** — an AST edge ≠ a
  data-flow edge, exactly like your `import` ≠ `implements` ≠ `service` edges;
  (2) **altitude matters** — CPG works at the *statement/expression* level for
  security. That's the **microscope**; ARCHON is the **map** (packages,
  contracts, services). Cite it as the representation ancestor and to justify
  "typed graph + queries," while making the altitude contrast that keeps ARCHON
  distinct.

---

## GROUP 3 — Graphs feeding models (the modern cluster)

> All four below share one message: **a codebase is naturally a graph, and giving
> a model that graph structure beats feeding it flat text** — for generating
> fixes, locating edits, and completing code. This is your evidence base that "a
> graph is the right representation." None of them, though, produce a
> human-readable *architectural delta of a PR* — that's ARCHON's niche.

### 3. CGM — Code Graph Model
**Tao et al. — NeurIPS, 2025.** *(already in your refs)*

- **Problem.** LLMs handle local code well but struggle with **whole-repository**
  tasks (like fixing a real GitHub issue spanning many files), because the repo's
  dependency structure isn't in the flat text they read. Agent frameworks bolt on
  tools to compensate, adding complexity.
- **Solution.** Put the repository's **structure graph directly into the LLM**.
  Build a graph (files/classes/functions as nodes, dependencies as edges), then
  use an **adapter** that maps each node's content into the model's input space
  and injects the graph structure into its attention — so the model *sees* the
  graph, not just tokens. No agent scaffolding.
- **Contributions.** A graph-integrated LLM for repo-level tasks; strong
  issue-fixing results **without** an agent framework; evidence that injecting
  structure beats text-only.
- **Question it answers.** *"Does giving an LLM the repo's dependency graph make
  it better at repo-scale work?"* → yes; and *"how do you get graph structure
  into a text model?"*
- **What ARCHON takes from it.** Direct evidence that explicit code graphs help
  LLMs at repo scale — supports feeding ARCHON's graph/delta to an **AI
  reviewer**, and shows one concrete mechanism for it. Key difference: CGM's graph
  is a **means** (help the model write a patch); ARCHON's graph is the **end
  product** (a reviewable artifact). Complementary, not competing.

### 4. RepoGraph — Repository-level Code Graph for AI Software Engineering
**Ouyang et al. — ICLR, 2025 (verify).**

- **Problem.** Coding agents that fix issues must find the **right lines** to edit
  across a large repo. Whole-file context is too coarse; flat text retrieval
  misses how the code is connected.
- **Solution.** Build a **fine-grained repo code graph** where nodes are code
  **definitions** (functions/classes) and edges are their **references/relations**
  (who defines, who calls whom). Ship it as a **plug-in module** you drop into
  existing agent frameworks to give them structure-aware context.
- **Contributions.** A definition-level repo graph; a plug-and-play module that
  **boosts several existing SWE agents** on SWE-bench; shows structure helps
  **localization** (finding *where* to change).
- **Question it answers.** *"How do you make a coding agent structurally aware so
  it edits the right place?"*
- **What ARCHON takes from it.** Same core belief as ARCHON — the repo is a graph
  and that structure is the missing ingredient. Difference: RepoGraph helps an
  agent **find/edit** code; ARCHON **reviews the architectural change**.
  RepoGraph's node/edge design (definitions + references) is a good reference if
  you ever drop **below packages** to the function altitude, and it strengthens
  your "graphs help agents" story for the agentic/Door-1 future.

### 5. RepoHyper — Search-Expand-Refine on Semantic Graphs
**Phan et al. — 2024 preprint (verify final venue).**

- **Problem.** Repository-level **code completion** needs the right context pulled
  from all over the repo, and choosing that context is hard.
- **Solution.** Build a **Repo-level Semantic Graph** (code elements as nodes,
  semantic relations as edges), then run **Search → Expand → Refine**: search the
  graph for seeds relevant to the cursor, **expand along edges** to gather related
  context, then refine/rank what to feed the model.
- **Contributions.** A semantic graph + **graph-traversal retrieval** for
  completion that beats flat-retrieval baselines.
- **Question it answers.** *"How do you use a graph to retrieve the right
  cross-file context for completion?"*
- **What ARCHON takes from it.** The transferable idea is **graph expansion**:
  given a changed node, walk the edges to find the **blast radius** / who's
  affected. That's precisely the "impacted neighborhood" you'd compute around a
  delta, and the Search-Expand-Refine shape maps onto *"given this PR's changed
  boxes, expand to the implicated contracts and owners."* The least central of the
  six for you, but useful for the "who does this change affect?" step.

### 6. GraphCoder — Coarse-to-fine Retrieval on a Code Context Graph
**Liu et al. — IEEE/ACM Automated Software Engineering (ASE), 2024.**

- **Problem.** Repo-level completion again — but existing retrieval uses **text
  similarity / sliding windows** and ignores code structure, so it grabs
  irrelevant context.
- **Solution.** Build a **Code Context Graph** capturing **control flow + data
  dependence + control dependence** around the current spot, then do
  **coarse-to-fine** retrieval: first match structurally-similar subgraphs
  (coarse), then narrow to the most relevant snippets (fine).
- **Contributions.** A structure-aware retrieval method that beats text-based
  retrieval; evidence that **dependence structure is a better relevance signal
  than text proximity**.
- **Question it answers.** *"Is code's control/data-dependence structure a better
  way to find relevant context than raw text similarity?"* → yes.
- **What ARCHON takes from it.** Reinforces the recurring theme — **structure
  beats text**. The concrete parallel: dependence edges define "what's actually
  related," which is the same principle behind your **witness sets and typed
  edges** — a real call/data relationship, not textual adjacency. Good support for
  "the diff text is the wrong unit; the graph is the right one."

---

## The through-line (what they collectively tell you)

| Paper | Venue / Year | One-line | ARCHON takeaway |
|---|---|---|---|
| SAR Taxonomy (Ducasse & Pollet) | TSE 2009 | Map of all architecture-recovery methods | Positions ARCHON: bottom-up, source-driven, quasi-automatic SAR for **conformance + co-evolution**, done **per PR** |
| Code Property Graph (Yamaguchi et al.) | S&P 2014 | Merge AST+CFG+PDG into one typed, queryable graph | The typed-graph ancestor; **types matter**, but it's a statement-level *microscope* vs ARCHON's *map* |
| CGM (Tao et al.) | NeurIPS 2025 | Inject the repo graph into an LLM | Graphs help LLMs at repo scale → feed ARCHON's delta to an AI reviewer |
| RepoGraph (Ouyang et al.) | ICLR 2025 | Definition-level repo graph boosts coding agents | Same belief; helps agents *edit*, ARCHON *reviews*; node/edge design for a future leaf altitude |
| RepoHyper (Phan et al.) | 2024 | Search-Expand-Refine over a semantic graph | **Graph expansion** = computing a change's blast radius / affected owners |
| GraphCoder (Liu et al.) | ASE 2024 | Coarse-to-fine retrieval over a dependence graph | **Structure beats text** for relevance — same logic as typed edges + witnesses |

**What questions do these papers answer, together?**
1. *Is a graph the right representation for reasoning about code?* → **Yes.**
   Four separate SOTA results (generation, localization, completion) all come from
   adding graph structure over flat text.
2. *At what altitude?* → CPG shows you *can* go to statements (for security);
   the LLM cluster works at file/definition level; the taxonomy reminds you
   *architecture recovery* is its own goal. ARCHON's choice — **package/contract
   altitude, per PR** — is deliberate and defensible against all of them.
3. *What's still missing (ARCHON's gap)?* → Every one of these uses the graph to
   help a model **do a task** (write, complete, locate). **None produce a
   human-readable architectural delta of a PR tied to evidence.** That is ARCHON.

**One line for the talk:** *Everyone agrees code is a graph and structure beats
text — they use it to help models write code. ARCHON uses it to show a reviewer
what a PR changed architecturally, and whether the guarantee behind that change
still holds.*
