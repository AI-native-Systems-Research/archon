# Related Work, in Plain English

The point of this file: understand what everyone else built, sort them into a
few **buckets**, and see clearly where ARCHON sits.

## A running example (use this to feel the difference)

Take a real PR: **someone adds a Memcached cache in front of the search store**
(this is todo-api's actual "cache-aside" commit). Concretely that PR:

- adds a new `memcached` package whose type **implements the same store
  interface** the database already implemented (so callers don't change),
- pulls in a **Memcached client library**,
- wires the cache into the app's startup, and
- rewrites a bunch of function bodies.

Keep this PR in mind. Below, each tool sees a *different slice* of it — and most
see almost none of the architectural point.

## The one-sentence summary

Almost every existing tool does one of two things:

1. **Checks rules** you wrote by hand ("layer A may not import layer B"), or
2. **Shows the current state** of the architecture (a picture of the whole
   system as it is *right now*).

Let me make those two concrete, because they sound vague:

- **"Checks rules you wrote by hand"** means: *before you get any value*, a human
  has to write down the architecture — either as rules ("payments must not import
  billing") or as a drawing in a modeling language (LikeC4, Structurizr, a
  Reflexion mapping). If nobody wrote it, the tool has nothing to check. On our
  example PR, such a tool only reacts if the new cache happens to break a rule
  someone already wrote.
- **"Shows the current state"** means: you open the tool and it draws the
  *entire* dependency graph of the repo, or prints a list like *"cycles found:
  payments → billing → payments."* It shows the architecture **as it is**, not
  **what this PR changed**. To find the cache, you'd have to eyeball the diagram
  before and after and spot the difference yourself.

Now here is what **almost nobody** does — and this is exactly ARCHON, applied to
the example PR:

- **Recovers the architecture automatically from the code.** No one has to draw
  anything first. ARCHON reads the source (using Go's own compiler tooling) and
  builds the boxes-and-arrows itself, then freezes that as the baseline. *On the
  example:* it already knows the store interface and its implementers without
  being told.
- **Computes the difference this one PR makes.** It builds the picture before and
  after and subtracts them, so you get *only what changed*, not the whole map.
  *On the example:* `+ memcached box`, `+ implements edge onto the store
  contract`, `+ Memcached service` — and everything else (the body rewrites)
  comes out **empty**.
- **Across many kinds of edge.** Not just imports. Interfaces (the cache
  *implements* the store contract — invisible to imports), services (a Memcached
  dependency), config keys, API endpoints, data schemas, capabilities. *On the
  example:* a plain import checker sees "a new import"; ARCHON sees "a cache-aside
  decorator was slotted into the store contract **and** a Memcached service is now
  required."
- **Ties it to evidence.** It doesn't just say the shape changed — it asks *"is
  there a test guaranteeing the cache behaves like the real store?"* *On the
  example:* it flags that no contract test covers the new implementer — an
  **evidence gap**.

That *combination* is ARCHON's gap. Everyone else has **some** of it; nobody has
all of it in one per-PR report.

### What "no per-PR delta / manual model / no contracts" means, concretely
These three phrases appear a lot below, so here is what each actually means:

- **No per-PR delta** = the tool shows the architecture as it is (or checks it
  against rules), but does **not** hand you a "*this is what changed between the
  base branch and this PR*" report. You compare snapshots yourself.
- **Needs a manual model** = you must **hand-author** the intended architecture
  (rules or a diagram) before the tool does anything useful. ARCHON needs no such
  input — it derives the model from the code.
- **No contracts / evidence** = the tool can tell you a dependency or shape
  changed, but never "…and here is the guarantee that change puts at risk, and
  whether a test still backs it." That behavioral/evidence layer is missing.

---

## The buckets at a glance

On our example PR (adding the Memcached cache), here is what each family would
actually give you, and why ARCHON's answer is different and more useful.

| Bucket | Examples | What you actually get from them (on the example PR) | Why ARCHON is different & more impactful |
|---|---|---|---|
| **A. Architecture conformance & recovery** | Reflexion Models, Structure101, Sotograph, Lattix, Erode, RepoWise, Arch-Engine | The *current* dependency diagram of the whole repo, or "you now have N cycles." You spot the cache by comparing pictures yourself. Several (Reflexion, Erode) need you to **draw the intended architecture first**. | ARCHON needs **no hand-drawn model** and hands you **only the change**: `+ memcached`, `+ implements onto the store contract`, `+ Memcached service`. It's the diff, not the whole map. |
| **B. Dependency / layering rule checkers** | ArchUnit, import-linter, deptrac | Only reacts if you *pre-wrote a rule* the cache violates (e.g., "web must not import memcached"). Sees the **import**, nothing else. | ARCHON auto-derives the baseline (no rules to write) and sees the parts imports **can't**: the interface implementation, the service dependency, config/API/schema. |
| **C. PR-level change & review tooling** | CHID, DERT, ChangeViz, ChangePrism, Cybewave, RefactoringMiner | A **risk score** + "these 14 methods may be affected" (CHID), or a **UML picture of changed classes/methods** (DERT), or method definitions pulled into the PR view (ChangeViz). All at the *code* level. | ARCHON answers a *different question*: not "how risky / what code moved" but "**what boundary moved**" — a new service dependency, a widened API, a new contract implementer — plus whether evidence still covers it. |
| **D. Low-level program graphs** | Code Property Graph, ProgQuery (go/packages, go/types = infra) | A microscope: statement/data-flow graph to answer "can user input reach this SQL call?" It would show cache internals as nodes/flows. | ARCHON is a **map, not a microscope**: it works at the package/contract altitude a reviewer thinks in, and reports *architectural consequence*, not data flow. |
| **E. Architecture evolution research** | Hassan & Holt, Lehman's laws, Bhattacharya et al. (ICSE'12) | Studies/predictions of how architecture drifts across **releases and years** (often graph-metric analytics over history). | ARCHON operates on a **single PR, in CI, now** — a precise diff, not a trend prediction. |
| **F. Evidence / property testing** | QuickCheck, Hedgehog | Tools to *write* tests that hammer behavior with generated inputs. They don't know the cache exists as a contract. | ARCHON **binds** a test to the contract it guards, so it can say "the cache implements the store contract but no test proves they agree." |
| **G. Single-artifact evolution** | ConfDroid, OpenAPI compat, protobuf/Avro evolution | Each compares **one artifact type** in isolation: just config, or just the API schema, or just the DB schema. | ARCHON folds config + services + APIs + schemas into **one** graph, so a PR touching several shows up in **one** report. |
| **H. LLM / ML + code graphs** | RAIM, "LLM Agents Can See Code Repos", RepoReviewer, Code Graph Model (NeurIPS'25), Universal Representation for Code | Feed repo structure to an AI so it **writes**/navigates code better, or **learn embeddings** from code graphs for ML tasks. | ARCHON produces the one thing they don't: an explicit **architecture diff of a PR** an AI (or human) reviewer can read. |

---

## Bucket A — Architecture conformance & recovery
*"What is the architecture, and does it follow the rules?" — state, not change.*

- **Reflexion Models** (Murphy, Notkin, Sullivan, ICSE 1995) — The classic. A
  human draws the *intended* architecture; the tool maps the real code onto it
  and flags where reality violates the drawing. **Difference:** needs a
  hand-made model, works on a snapshot, no per-commit delta, no contracts.
- **Structure101 / Sotograph / Lattix** — Industrial tools that recover
  dependency graphs, find cycles, show layering violations, and track "erosion"
  over time. **Difference:** they show the *state* of the architecture, not the
  *change* one PR makes; no CI-per-PR review; no contracts.
- **Erode** — A GitHub Action/CLI that compares a PR against an existing
  architecture model (LikeC4 or Structurizr) and flags undeclared dependencies.
  Very close competitor. **Difference:** you must *already have* an architecture
  model; ARCHON recovers and diffs the architecture straight from the code.
- **Arch-Engine** — Compares the PR branch to the base branch and reports
  added/removed dependency edges and policy violations. Close on the "compare
  branches" idea. **Difference:** it's mostly rule- and dependency-based, not a
  reviewer-facing *explanation* of the architectural change.
- **RepoWise** — Keeps a system dependency graph and checks PRs for forbidden
  dependencies and cycles. **Difference:** validates predefined rules; doesn't
  explain the full architectural delta.

## Bucket B — Dependency / layering rule checkers
*"I wrote a rule; enforce it." — closest to ARCHON's structural gate, but manual.*

- **ArchUnit** (Java) — You write rules in code ("controllers must not touch
  repositories") and it checks them. **Difference:** rules are manual; ARCHON
  extracts the architecture automatically.
- **import-linter** (Python) — Lets you declare "package A cannot import package
  B." Same spirit as ARCHON's allow-list. **Difference:** only imports — no
  interfaces, services, APIs, schemas, and no before/after delta.
- **deptrac** (PHP) — Same idea again: layering rules. **Difference:** same as
  above.

> ARCHON's structural contract (the `Allow`-list) lives in this family — but it
> is *seeded automatically by snapshotting the real code*, and it sits on top of
> many more edge kinds than imports.

## Bucket C — PR-level change & review tooling
*Closest to ARCHON's framing ("help the reviewer of this PR"), but each stops short.*

> **Why this is different (the part that's easy to miss):** these tools live *on
> the PR*, just like ARCHON — so it's tempting to think they're the same thing.
> The difference is the **question they answer**. On our cache PR:
> - **CHID** posts: *"this PR touches 14 methods; risk: medium; files that
>   usually change together: search.go, store.go."* → a **risk/impact** answer.
> - **DERT** draws: *"class `Task` (memcached) added; methods `Get`/`Put`
>   added, marked new."* → a **picture of which code changed**.
> - **ChangeViz** shows you the definitions and call-sites of those methods
>   inline so you don't have to click into the repo. → a **navigation** aid.
>
> None of them say the architectural sentence: *"a **Memcached service**
> dependency appeared, and a **cache-aside decorator** was slotted onto the
> **store contract** — with no test proving the cache and store agree."* That
> sentence — the *consequence*, not the *code* — is ARCHON's job.

- **CHID** (2025) — Builds a repo-wide call graph, finds methods a PR might
  impact, notices files that historically change together, and posts a **risk
  report** on the PR. The closest academic work. **Difference:** it's about
  *impacted methods and risk scores*, not a clear before/after **architecture**
  diff.
- **DERT** — Draws a UML-like picture of what a PR changed (classes, methods,
  interfaces) marked added/removed/modified. Conceptually very close to a
  structural diff. **Difference:** it *visualizes structure*; it doesn't reason
  about architectural consequences or contracts.
- **ChangeViz** — Adds method definitions and call-site references into the
  GitHub PR view so reviewers don't have to navigate the repo. **Difference:**
  helps you read code around the diff; not an architectural delta.
- **ChangePrism** — Visualizes commits beyond red/green lines, separating
  modifications, refactorings, and small "micro-changes." **Difference:** great
  motivation for "text diffs are not enough," but it's still code-level.
- **Cybewave** — Puts Mermaid/PlantUML diagrams inside PRs so architecture can be
  reviewed with the code. Same motivation as ARCHON. **Difference:** the diagrams
  are hand-maintained or AI-updated — **not automatically inferred** as the
  consequence of the code change.
- **RefactoringMiner** — Produces AST-level diffs and explicitly labels
  refactorings, including code moved between files. **Difference:** not a
  competitor — it's a possible **building block** for ARCHON's "is this a pure
  refactor or a real change?" question (node identity / empty-delta).

## Bucket D — Low-level program graphs & static-analysis infrastructure

> **Why this is different (altitude):** these also build a "graph of the code,"
> so the name collides with ARCHON — but they work at a totally different zoom
> level. A Code Property Graph is a **microscope**: nodes are statements and
> expressions, edges are control- and data-flow, and the questions are things
> like *"can a value from this HTTP handler reach a Memcached call unsanitized?"*
> ARCHON is a **map**: nodes are packages/services/contracts, and the question is
> *"did a dependency from the web layer to a cache service appear in this PR?"*
> Same PR, opposite ends of the zoom range — one is for finding a security bug in
> a line, the other for reviewing a boundary in a design.

- **Code Property Graph** (Yamaguchi et al.) — Very influential: puts the syntax
  tree, control-flow, and data-flow of code into one graph, mainly for security
  bug-finding. **Difference:** far lower-level (statements, data flow); ARCHON's
  graph is at the *architecture* altitude (packages, contracts, services).
- **ProgQuery** (Rodriguez-Prieto, Mycroft, Ortin, IEEE Access 2020) — A platform
  that stores a Java program as several *overlaid* graphs (syntax tree, control
  flow, data flow, call graph, type graph) in a graph database, so you can write
  queries to find bugs, vulnerabilities, or compute metrics. **Difference:** it's
  a powerful low-level *code-query* engine — same microscope altitude as CPG.
  ARCHON isn't a query platform over statements; it's a fixed, higher-level
  package/contract map, diffed per PR. (It *is* good evidence that "represent code
  as a queryable graph" is a proven idea — ARCHON just does it one altitude up.)
- **go/packages, go/types** — Not research; Go's own tooling. They give you
  packages, imports, types, and interface-satisfaction. **ARCHON is built on
  these** — that's why it reads real types instead of guessing from text.

## Bucket E — Architecture evolution research
- **Hassan & Holt** — Study how a system's architecture changes across releases
  and history. **Difference:** they look at long-range history, not a single PR
  under review.
- **Lehman's laws of software evolution** — Classic observation that software
  changes continuously and architecture drifts. **Difference:** a motivation, not
  a tool; ARCHON is a concrete per-PR delta.
- **Bhattacharya et al., "Graph-based analysis and prediction for software
  evolution"** (ICSE 2012) — Models a system as graphs (e.g. call graphs,
  developer-collaboration graphs) and uses graph-theoretic metrics *across the
  project's history* to analyze and even **predict** things like bug severity,
  maintenance effort, and defect-prone modules. **Difference:** it's graph-based
  *predictive analytics over history* — "which parts will get harder to
  maintain." ARCHON also uses a graph and cares about evolution, but it computes
  the **exact architectural change of one PR right now**, not a statistical
  forecast over releases. (Nice framing point: both say "graphs + evolution," but
  one predicts trends, the other pins down a single change.)

## Bucket F — Evidence / property testing (ARCHON's future work)
- **QuickCheck** (Claessen & Hughes, ICFP 2000) — Invented property-based
  testing: describe a property, it generates many inputs to try to break it.
- **Hedgehog** — A modern property-testing library.
- **Difference (both):** they *produce evidence* that behavior holds, but they
  don't know which **architectural contract** a property belongs to. ARCHON's
  contract-coverage work is exactly the bridge: bind each property test to the
  interface it guards.

## Bucket G — Single-artifact evolution (ARCHON unifies these)
- **ConfDroid** and other config-dependency papers — Track configuration options
  as they flow through code. **Difference:** they stop at *config*; ARCHON turns
  a config key into a first-class node in the *architecture* graph.
- **API compatibility / OpenAPI evolution** — Compare two versions of an API
  schema. **Difference:** ARCHON puts API endpoints inside the *same* graph as
  everything else, so an API change is one part of the architectural delta.
- **Schema evolution (protobuf, Avro, DB migrations)** — Compare two versions of
  a data schema. **Difference:** same story — ARCHON tracks the wire/DB schema as
  one axis of a single per-PR delta.

> The pattern: config, API, and schema each have their own research communities.
> ARCHON's move is to say they are all just **edges in one architecture graph**,
> so a PR that touches any of them shows up in one report.

## Bucket H — LLM / ML + code graphs
*Why an automatic architecture diff could help AI reviewers, not just humans — and how "graphs of code" are used in machine learning.*

- **RAIM** — Uses repo structure and call graphs to locate cross-file changes and
  assess upstream/downstream/regression risk. **Difference:** it targets *writing
  a feature*, not *reviewing a PR*.
- **"LLM Agents Can See Code Repositories"** — Gives agents a visual repository
  graph as context; cut token use by up to ~26% while keeping or improving
  accuracy. **Difference / support:** strong evidence that an architecture view
  helps an AI — motivation for feeding ARCHON's delta to an AI reviewer.
- **RepoReviewer** — A multi-agent PR-review system (context synthesis, file
  review, prioritization, reporting). **Difference:** a natural **baseline** —
  compare a normal repo-aware agent against one that also gets ARCHON's explicit
  architecture diff.
- **Code Graph Model (CGM)** (Tao et al., NeurIPS 2025) — Puts repo structural
  dependencies directly into an LLM and uses graph-RAG for repo-level tasks.
  **Difference / support:** backs the broad claim that explicit code graphs
  improve repository understanding.
- **Universal Representation for Code** (Liu et al., PAKDD 2021) — Builds a
  graph-based code representation (capturing control and data flow) and
  *pre-trains graph neural networks* on it to get general-purpose code embeddings,
  then fine-tunes for tasks like method-name prediction and code-graph link
  prediction. **Difference:** this is **representation learning** — it turns code
  graphs into vectors for ML models. ARCHON's graph is the opposite: a
  human-readable, typed, exact structure meant to be *diffed and reviewed*, not
  embedded. (Shared idea: code is naturally a graph; different use: learning vs.
  reviewing.)

---

## What nobody really does (ARCHON's contribution)

Current tools answer:

- *"Does the architecture satisfy my rules?"* (Bucket A, B), or
- *"Has the architecture drifted over time?"* (Bucket E), or
- *"What code did this PR touch?"* (Bucket C).

ARCHON asks a different question:

> **"What changed *architecturally* in this PR?"**
> and eventually **"Which guarantees changed, and what evidence supports them?"**

- vs **Reflexion / conformance:** no hand-drawn model needed — ARCHON recovers
  the architecture from code and *freezes it* as the baseline.
- vs **ArchUnit / import-linter:** not just imports and not just hand-written
  rules — interfaces, config, services, APIs, schemas, capabilities, computed
  per PR.
- vs **CHID / DERT / ChangeViz:** not risk scores or code-structure pictures —
  an architectural **before/after** with an empty-delta verdict and evidence
  gaps.
- vs **config / API / schema tools:** all of these live in **one** graph, so one
  PR report covers them together.
- vs **property testing:** ARCHON connects the evidence to the *contract* it
  protects.

---

## The related-work slide (four columns)

| Area | Examples | What they do | Limitation vs ARCHON |
|---|---|---|---|
| Architecture conformance | Reflexion Models, Lattix, Structure101, Sotograph | Recover/check architecture state | Analyze architecture, not the architectural **delta** of a PR |
| Dependency checking | ArchUnit, import-linter, deptrac | Enforce hand-written import/layer rules | Rule-based, imports only, no delta or contracts |
| PR change/review tooling | CHID, DERT, ChangeViz | Impact/risk or code-structure views on PRs | Not an architecture-level before/after across edge types |
| Program analysis | Code Property Graph | Detailed code graph for (security) analysis | Too low-level; not architecture-altitude review |
| Property testing | QuickCheck, Hedgehog | Generate behavioral evidence | Evidence not connected to an architectural contract |

**One-liner for the talk:** *Everyone else checks rules or shows state. ARCHON
computes the architectural delta of a single PR — across imports, interfaces,
config, services, APIs, and schemas — and is starting to say which guarantees
that change puts at risk.*
