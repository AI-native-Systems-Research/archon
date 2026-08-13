# Related Work, Explained Simply

*What each related work actually does, how it does it, whether it competes with
ARCHON, where ARCHON is better, and what each one's limits are. Plain English,
no jargon where it can be avoided.*

Legend:
- ✅ = already cited in `refs.bib`
- ⚠️ = **not** cited yet but reviewers will expect it

---

## Group 1 — The ideas ARCHON is *built on* (intellectual roots, not competitors)

### 1.1 Parnas — "On the Criteria to Be Used in Decomposing Systems into Modules" (1972) ✅

- **What it does.** Argues that a module should *hide* the design decisions that
  are most likely to change. If a module hides a decision behind a stable
  interface, the rest of the system doesn't have to care when that decision
  changes.
- **How it does it.** It's a design principle, not a tool. Parnas showed via
  worked examples that decomposing systems by "what might change" gives you
  cleaner, more maintainable systems than decomposing by processing steps.
- **Different from ARCHON?** Not competing — this is the *intellectual root* of
  ARCHON's boundary-locality claim. A stable surface = the option to freely
  change the interior.
- **Is ARCHON "better"?** Wrong question. ARCHON operationalizes Parnas: it
  makes the "hidden vs. exposed" distinction a checkable property in real repos,
  every PR.
- **Limitation.** Parnas is a principle. It gives no way to *check* whether a
  real repo actually hides what it should. That's what ARCHON adds.

### 1.2 Baldwin & Clark — *Design Rules: The Power of Modularity* (2000) ✅

- **What it does.** Frames modularity in economic terms: a modular system's
  value lies in the *options* it creates — the ability to swap or improve one
  module without touching others.
- **How it does it.** A book-length economic and design-theoretic treatment,
  with case studies from computer industry evolution.
- **Different from ARCHON?** Same as Parnas — a foundation, not a competitor.
- **Is ARCHON "better"?** ARCHON gives Baldwin & Clark's "options" a mechanical
  meaning: an empty boundary delta *is* the exercised option to change the
  interior.
- **Limitation.** Purely theoretical. Doesn't tell you how to compute or check
  which modules are actually delivering options today.

---

## Group 2 — Dependency structure as a matrix (early formal descendants)

### 2.1 Sangal et al. — "Using Dependency Models to Manage Complex Software Architecture" (2005) ✅

- **What it does.** Introduces the practical use of **Design Structure Matrices
  (DSMs)** in software: a square matrix where rows/columns are components and a
  cell means "component X depends on component Y."
- **How it does it.** Parse the codebase → extract file/module dependencies →
  fill in the matrix → reorder rows/columns until block patterns emerge. Cycles
  appear as blocks that can't be triangularized. The commercial tool built on
  this is Lattix.
- **Different from ARCHON?** Yes — a DSM is a *snapshot* at one point in time,
  and it's usually one flat matrix without hierarchy.
- **Is ARCHON better?** In three ways: (1) ARCHON is per-PR, not periodic; (2)
  ARCHON has typed edges (not just "depends on"); (3) ARCHON's projection is
  hierarchical — you can zoom to any level, DSMs are one level.
- **Limitation.** No notion of *change* over time, no notion of edge type, no
  contracts, no agent gating. Purely a visualization.

### 2.2 MacCormack et al. — Empirical DSM study (2006) ✅

- **What it does.** Uses DSMs to *measure* how modular real open-source and
  proprietary systems are, and shows that architectural style predicts
  maintenance cost.
- **How it does it.** Applied DSMs to several large systems, computed
  "propagation cost" metrics, and correlated those with defect and change data.
- **Different from ARCHON?** Yes — this is an empirical study, not a tool.
  Useful precedent for how to measure architecture in real repos.
- **Is ARCHON better?** Not a fair comparison; ARCHON is a substrate, this is
  evidence. But ARCHON's per-PR delta gives finer-grained data than
  whole-repo snapshots.
- **Limitation.** Snapshot analysis; doesn't gate changes; doesn't type edges.

---

## Group 3 — Architecture conformance checking (the closest competitors)

This is where the paper's real prior art lives. ARCHON must beat these
carefully.

### 3.1 Murphy, Notkin, Sullivan — Software Reflexion Models (2001) ✅

- **What it does.** Compares two things: (a) a high-level architecture model
  the architect draws, and (b) the *actual* dependencies extracted from source.
  Reports where they agree, disagree, and where dependencies exist in one but
  not the other ("convergences, divergences, absences").
- **How it does it.** The architect writes a small mapping: "package `foo` is
  part of layer `X`." The tool extracts real dependencies, aggregates them per
  the mapping, then compares to the declared "should exist" edges.
- **Different from ARCHON?** Reflexion is the direct ancestor. ARCHON is a
  descendant that:
  - Runs **per PR** (reflexion is a periodic audit)
  - Uses a **derived-then-frozen** intended model (reflexion needs the architect
    to hand-write it)
  - Adds **typed operational edges** (reflexion is import/call only)
  - Adds **contracts + evidence** for behavior, not just structure
  - Gates **agent changes** with a boundary-locality condition
- **Is ARCHON better?** Yes, and honestly so — ARCHON is *a* reflexion model,
  extended in five deliberate directions.
- **Limitation.** Reflexion requires up-front architect effort to draw the
  intended model. This is the #1 reason it doesn't get adopted. ARCHON's
  snapshot-and-ratchet directly answers this.

### 3.2 Terra & Valente — Dependency Constraint Language (2009) ✅

- **What it does.** A small language for writing rules like "layer A may only
  depend on layer B, never on layer C" — automatically checked in CI.
- **How it does it.** DSL where the architect writes rules; a checker walks the
  code's import graph and flags violations.
- **Different from ARCHON?** Yes — rules are *hand-written*, checks are
  *whole-repo pass/fail*, edges are *only imports*.
- **Is ARCHON better?**
  - Rules can be **auto-derived** from a snapshot (no hand-writing on day one)
  - Checks are on the **PR delta**, not a periodic audit
  - Edges include **config, protocol, capability, platform** — the things
    that actually cause outages
- **Limitation.** Adoption bottleneck (someone has to write the rules), noisy
  in brownfield (every existing violation floods the report on day one).

### 3.3 Passos et al. — Static Architecture-Conformance Checking overview (2010) ✅

- **What it does.** A survey/tutorial paper that catalogues the main
  conformance approaches: reflexion, relation-conformance, and
  component-access rules.
- **How it does it.** Explanatory paper; not a tool.
- **Different from ARCHON?** It's the taxonomy of what ARCHON's structural
  gates descend from. ARCHON's G1–G3 gates sit in this lineage.
- **Is ARCHON better?** ARCHON adds the *delta*, the *altitude projection*,
  and the *evidence layer* to this classical picture.
- **Limitation.** All classical approaches share the "hand-authored model,
  whole-repo audit, import edges only" trio of weaknesses.

### 3.4 ArchUnit / dependency-cruiser / import-linter ⚠️ **(gap in the paper)**

- **What they do.** Modern industrial tools that let developers write
  architecture rules *as unit tests* — "domain layer must not import the web
  layer" — and run them in CI on every commit.
- **How they do it.**
  - **ArchUnit** (Java, most famous): rules written in Java, run by JUnit.
  - **dependency-cruiser** (JS/TS): rules in a JS config file, run from CLI.
  - **import-linter** (Python): rules in `.ini` config; TOML-configurable.
  - All parse the code, extract the dependency graph, and check hand-written
    rules on it.
- **Different from ARCHON?** These are the closest thing to ARCHON in
  *practice*. Key differences:
  - They are **whole-repo pass/fail**, not per-PR delta
  - They are **import-only** edges (occasionally class inheritance)
  - Rules are **hand-authored** — huge adoption cost in an existing repo
  - No **identity model** (a rename or file move looks like new violations)
  - No **contracts / evidence / agent gating**
- **Is ARCHON better?** Yes on all five dimensions. But this comparison must
  be *made explicitly* in the paper — reviewers will notice these tools are
  what teams actually use today. **This is the #1 missing citation.**
- **Limitation.** Effectively a hand-authored linter for dependencies. Works
  great once configured, but the configuration is exactly the barrier.

### 3.5 REMEDY / MAPE-K Architectural Conformance Checking (2024–2025) ⚠️

- **What it does.** A recent domain-specific conformance checker for
  self-adaptive systems (systems that reconfigure themselves at runtime). Uses
  the MAPE-K reference model as the intended architecture.
- **How it does it.** DSL for describing the intended MAPE-K structure +
  recovery tool that extracts the current structure + a checker that flags
  drifts.
- **Different from ARCHON?** Yes — narrow (only MAPE-K style systems),
  domain-specific, still periodic rather than per-PR.
- **Is ARCHON better?** ARCHON is general-purpose and adds the delta / agent
  / contracts angle. REMEDY is better *within* its narrow domain.
- **Limitation.** Only useful for self-adaptive systems that already
  conform to MAPE-K.

---

## Group 4 — Contracts and verification (techniques ARCHON *places*, not invents)

### 4.1 Meyer — Design by Contract (1992) ✅

- **What it does.** Every function/method has pre-conditions (what the caller
  must ensure) and post-conditions (what the function guarantees). Contracts
  are checked at runtime.
- **How it does it.** First-class language support in Eiffel; libraries in
  most other languages.
- **Different from ARCHON?** Different scope — DbC is per-function.
- **Is ARCHON better?** Not a "better" question. ARCHON *uses* DbC as one
  of the evidence types attached at box surfaces.
- **Limitation.** In practice, adoption of DbC has been low outside Eiffel —
  the culture never made it mainstream.

### 4.2 Claessen & Hughes — QuickCheck (2000) ✅

- **What it does.** Property-based testing. Instead of writing individual
  test cases, you write a *property* ("reversing twice gives back the
  original") and the tool generates thousands of random inputs to try to
  falsify it.
- **How it does it.** Generators for each type produce random values; the
  property is a predicate that must hold on all of them; failing cases are
  automatically shrunk to a minimal counterexample.
- **Different from ARCHON?** Different scope — a testing technique.
- **Is ARCHON better?** Not competing. ARCHON *uses* property-based tests as
  evidence to discharge contracts.
- **Limitation.** Only as good as the properties you write. Doesn't tell you
  *what* to test.

### 4.3 Fowler / Robinson — Consumer-Driven Contracts (2006) ✅

- **What it does.** In a microservices setting, each consumer publishes what
  behaviors it depends on; the provider is only obligated to preserve those.
- **How it does it.** Consumers publish contract tests; the provider runs
  them in its own CI to make sure changes don't break real consumers. Pact is
  a popular implementation.
- **Different from ARCHON?** ARCHON adopts this idea for *arrow contracts*: a
  cross-box arrow carries only the subset of the target's behavior the source
  actually relies on.
- **Is ARCHON better?** Not competing. ARCHON reuses CDC's placement discipline
  in a broader context.
- **Limitation.** Requires cross-team discipline and infrastructure (contract
  broker, etc.) that many teams don't have.

### 4.4 Leino — Dafny (2010) ✅

- **What it does.** A programming language + verifier: you write code
  annotated with contracts, and the tool proves that the code satisfies them.
- **How it does it.** Translates your code + contracts to an SMT solver's
  language (via Boogie); the solver proves or finds a counterexample.
- **Different from ARCHON?** Very different — Dafny is heavy formal
  verification of program logic.
- **Is ARCHON better?** Not competing. ARCHON *can* accept a Dafny proof as
  one kind of evidence for a contract; it does not itself do verification.
- **Limitation.** High authoring cost; rarely used in industrial codebases.

---

## Group 5 — Build systems and source diffing (supporting technology)

### 5.1 Bazel (Google's build system) ✅

- **What it does.** A fast, reproducible, incremental build system that
  operates on *targets* (fine-grained build units) with explicit declared
  dependencies.
- **How it does it.** You declare each target's inputs and outputs
  (`BUILD` files); Bazel builds a dependency graph and only rebuilds what
  actually changed.
- **Different from ARCHON?** Yes — a build tool, not a review tool.
- **Is ARCHON better?** Not competing. ARCHON *is* the review analogue of
  Bazel's per-target model: per-box gating instead of per-target building.
- **Limitation.** Bazel's graph is about the *build*, not architectural
  intent. You can have a clean Bazel graph over a spaghetti architecture.

### 5.2 Falleri et al. — GumTree fine-grained source differencing (2014) ✅

- **What it does.** Given two versions of a source file, produces a
  fine-grained *tree* diff (moves, renames, splits) rather than a line diff.
- **How it does it.** Builds ASTs of both versions and matches nodes with a
  cheap-then-precise heuristic to align the trees.
- **Different from ARCHON?** Different scope — a diffing algorithm.
- **Is ARCHON better?** Not competing. ARCHON *uses* GumTree as one of the
  signals for node identity (matching a leaf across a rename or move).
- **Limitation.** Works file-by-file; doesn't understand cross-file moves as
  first-class refactorings. That's what RefactoringMiner (below) adds.

### 5.3 Tsantalis et al. — RefactoringMiner (2018 / 2020) ⚠️ **(gap)**

- **What it does.** Detects refactorings (rename method, move method, extract
  method, inline, split, merge, etc.) between two versions of a Java project
  with very high accuracy (~99% precision, ~98% recall on public benchmarks).
- **How it does it.** No code-similarity thresholds — uses structured
  matching over statement mappings between commits.
- **Different from ARCHON?** Different scope — refactoring detection, not
  architecture review.
- **Is ARCHON better?** Not competing. ARCHON's node identity module *should
  use* RefactoringMiner (for Java) — it's a ready-made oracle. Not citing it
  is a missed opportunity to strengthen the identity story with proven tech.
- **Limitation.** Java-only (though ports exist for a couple other
  languages); single language at a time.

### 5.4 Yamaguchi et al. — Code Property Graph / Joern (2014+) ⚠️ **(gap)**

- **What it does.** Represents a program as a single unified graph that
  merges its syntax tree, control flow, and data flow. Analysts query it with
  a DSL (like a database) to find complex patterns — usually security
  vulnerabilities.
- **How it does it.** Parse code → build AST → add control-flow edges →
  add data-dependence edges → store all three in a property graph → expose
  through Joern's query language.
- **Different from ARCHON?** Similar idea ("program as graph") but very
  different scope:
  - CPG is **statement-level**, intra-procedural; ARCHON is **module-level**,
    hierarchical
  - CPG is a **snapshot** for querying; ARCHON is **versioned + diffed** per PR
  - CPG has three edge types (AST/CFG/DFG); ARCHON has many typed *operational*
    edges (config, capability, platform, etc.)
  - CPG is for security analysts; ARCHON is for reviewers + agents
- **Is ARCHON better?** Different problems. But reviewers will expect the
  cite because it's the most famous "code as unified graph" work. ARCHON
  should position itself as "at a higher altitude, with change and contracts."
- **Limitation.** CPGs are not designed for architectural review — the
  altitude is too fine, the edges are wrong for the question.

---

## Group 6 — Architecture technical debt (the "measure the mess" line)

### 6.1 Kazman, Cai, Mo, Xiao — Design Rule Spaces / Decoupling Level / Titan ⚠️ **(gap)**

- **What they do.** Detect *architectural hotspots* — clusters of files that
  are architecturally connected and bug-prone. Measure a repo's overall
  modularity with a metric called Decoupling Level (DL). Suggest refactorings
  to reduce architectural debt.
- **How they do it.** Cluster files by design rules (interfaces, abstract
  classes) into overlapping "Design Rule Spaces." Cross-reference with bug
  history and change frequency to find "architecture roots" of debt. The
  Titan tool implements this end-to-end.
- **Different from ARCHON?** Same territory (architecture as a first-class
  measurable thing) but different focus:
  - DRSpaces = **debt detection** and periodic measurement
  - ARCHON = **per-change gating** and evolution
- **Is ARCHON better?** Complementary, not better. ARCHON's brownfield
  ratchet *should use* something like DL as its debt metric. That's the right
  positioning: ARCHON is the *runtime* substrate, DRSpaces is the *diagnostic*.
- **Limitation.** DRSpaces is periodic and doesn't gate any changes — it
  produces reports the architects then act on manually.

---

## Group 7 — Architecture erosion and drift (the problem statement itself)

### 7.1 Perry & Wolf — "Foundations for the Study of Software Architecture" (1992) ⚠️ **(gap)**

- **What it does.** Introduces the terms "architectural drift" and
  "architectural erosion" — the two ways a system's real structure diverges
  from its intended design.
- **How it does it.** A definitional paper; no tool.
- **Different from ARCHON?** Foundational — ARCHON is a *response* to the
  problem Perry & Wolf named.
- **Is ARCHON better?** ARCHON is a specific mechanism to *prevent* what
  they described.
- **Limitation.** Purely definitional.

### 7.2 de Silva & Balasubramaniam — "Controlling Software Architecture Erosion: A Survey" (2012) ⚠️ **(gap)**

- **What it does.** Surveys the field of erosion prevention and detection —
  the standard reference for anyone talking about architecture drift.
- **How it does it.** Literature review with a taxonomy of approaches
  (prevention, detection, recovery).
- **Different from ARCHON?** ARCHON is a *new point* in this taxonomy.
- **Is ARCHON better?** Not comparable; the survey is a map, ARCHON is a
  location on it. Citing it is important for positioning.
- **Limitation.** From 2012 — pre-agent era.

### 7.3 "Architectural Degradation" multivocal review (2025) ⚠️ **(gap)**

- **What it does.** A very recent survey (108 studies) unifying the messy
  terminology (erosion, decay, degradation, aging) and cataloguing metrics,
  tools, and remediation techniques.
- **How it does it.** Systematic multivocal review of academic + industry
  literature.
- **Different from ARCHON?** ARCHON is exactly the "continuous, proactive
  remediation" the survey identifies as the field's biggest gap.
- **Is ARCHON better?** Not comparable — but the paper says
  "detection is well-studied, continuous remediation is lacking" and ARCHON
  can position itself as filling that gap.
- **Limitation.** Survey, not a solution.

---

## Group 8 — Automated program evolution (the "who's writing the code" line)

### 8.1 Chen et al. — Codex (2021) ✅

- **What it does.** Trained a large language model on code that can
  synthesize functions from natural-language specs.
- **How it does it.** GPT-style transformer trained on public code; released
  as an API and used inside GitHub Copilot.
- **Different from ARCHON?** Very different — a code *generator*, not a
  review substrate.
- **Is ARCHON better?** Not competing. ARCHON is the *gate* for what
  Codex-style generators produce.
- **Limitation.** No architectural awareness; no notion of boundaries,
  contracts, or approval; safety net is whatever tests already exist.

### 8.2 Yang et al. — SWE-agent (2024) ✅

- **What it does.** An agent that resolves GitHub issues autonomously by
  interacting with a codebase through a text-based "agent-computer interface."
- **How it does it.** LLM in a loop with file-viewing/editing/testing tools;
  navigates a repo, proposes patches.
- **Different from ARCHON?** Different — it's a *proposer* (concern 5 in
  ARCHON's model). ARCHON wraps around it.
- **Is ARCHON better?** Not competing. ARCHON provides the boundary its
  changes are checked against.
- **Limitation.** Uses only the test suite as safety net. Can — and does —
  quietly widen interfaces or add cross-boundary dependencies.

### 8.3 Jiménez et al. — SWE-bench (2024) ✅

- **What it does.** A benchmark of ~2,000 real GitHub issue-and-PR pairs used
  to measure how well LLM agents can resolve issues autonomously.
- **How it does it.** Collects real issues + their eventual patches from open
  repos; asks the model to produce a patch; grades based on whether the
  original repo's tests pass.
- **Different from ARCHON?** Very different — an evaluation benchmark.
- **Is ARCHON better?** Not competing. ARCHON *could* be evaluated on this
  data by asking "do the model-produced patches have empty vs. nonempty
  architectural deltas?"
- **Limitation.** Benchmark bias — grading by test-passing rewards changes
  that keep tests green, not changes that respect architecture.

### 8.4 Romera-Paredes et al. — FunSearch (2024) ✅

- **What it does.** Uses LLM-driven program search to *discover* new
  mathematical constructions (found improvements to open problems in
  combinatorics).
- **How it does it.** LLM proposes candidate programs; a fitness function
  evaluates them; an evolutionary loop keeps the best.
- **Different from ARCHON?** Very different — a discovery method, not a
  review substrate.
- **Is ARCHON better?** Not competing. FunSearch is one flavor of proposer
  ARCHON's Concern 5 admits.
- **Limitation.** Only works when a machine-checkable fitness function
  exists. Doesn't scale to general software.

### 8.5 Lehman et al. — Evolution Through Large Models (ELM, 2023) ✅

- **What it does.** Uses LLMs as mutation operators in an evolutionary
  algorithm — the LLM generates variants of a program, evolution selects.
- **How it does it.** Standard genetic algorithm architecture with an LLM
  replacing the random-mutation step.
- **Different from ARCHON?** Different — an evolutionary method for program
  synthesis.
- **Is ARCHON better?** Not competing. Another proposer style; ARCHON's gate
  works regardless.
- **Limitation.** Same as FunSearch — needs a good fitness signal.

---

## Group 9 — The AI-era safety substrates (very recent, very relevant)

### 9.1 HarnessAudit / OpenAgentBench / SafeAgent / Phalanx (2025–2026) ⚠️ **(gap)**

- **What they do.** Argue that AI agent safety should be enforced by the
  *harness* (the software that dispatches the agent's tools, controls its
  execution, and audits its actions) rather than by the model itself. Provide
  auditing frameworks and guardrail layers.
- **How they do it.**
  - **HarnessAudit**: benchmarks harnesses on tool-use safety, resource
    boundaries, and information-flow properties.
  - **SafeAgent**: sandbox execution with SHA-verified file changes and
    diff-safety checks.
  - **Phalanx**: deterministic guardrails (input filter, tool gateway,
    egress firewall, SAST validator) around an LLM-based refactoring agent.
- **Different from ARCHON?** Complementary, not competing. These check
  *execution-level* safety (did the agent access unauthorized data? did the
  code do something dangerous?). ARCHON checks *architectural* safety (did
  the agent silently cross a design boundary?).
- **Is ARCHON better?** Different layer of the same stack. Reviewers will
  recognize this as strong external support for ARCHON's "fixed substrate,
  open proposers" thesis.
- **Limitation.** These focus on execution, IO, and security — they don't
  see architecture. They can catch "the agent tried to `rm -rf /`" but not
  "the agent silently added a dependency from A to B."

### 9.2 RepoReviewer / Claude Code Review (2025–2026) ⚠️ **(gap)**

- **What they do.** Multi-agent LLM systems that read whole repos and produce
  automated PR reviews.
- **How they do it.** Break review into sub-agents (context building, code
  analysis, prioritization, summary). Feed the LLM the whole repo (via
  retrieval or long context) plus the diff, ask it to find issues.
- **Different from ARCHON?** This is *the alternative approach* — use an LLM
  to read the PR and reason about architecture from unstructured code. ARCHON's
  approach is the opposite: derive a structured graph and gate deterministically.
- **Is ARCHON better?** Different trade-offs, and ARCHON should say so
  openly:
  - ARCHON: **deterministic, replayable, auditable**, no false-positive
    hallucinations, but limited to what the extractor can see
  - LLM reviewer: **flexible, catches semantic issues**, but non-deterministic,
    hallucinates, hard to audit
  - **The synthesis is best**: ARCHON's typed graph deltas as structured
    context for an LLM reviewer's semantic judgment.
- **Limitation.** LLM reviewers have no formal safety, no audit trail,
  variable quality, and no gate they can guarantee.

---

## Group 10 — Theory underneath the boundary-locality theorem

### 10.1 Reynolds / Mitchell — Representation Independence, Contextual Equivalence ⚠️ **(gap)**

- **What it does.** In programming-language theory: two implementations of a
  data type are *indistinguishable* to any well-typed client if they satisfy
  the same interface. This is the classical result behind
  "abstraction = information hiding."
- **How it does it.** Uses logical relations and denotational semantics to
  prove that clients cannot distinguish observationally-equivalent
  implementations.
- **Different from ARCHON?** ARCHON's boundary-locality theorem is a
  software-architecture-level instance of representation independence.
  Same shape, different setting.
- **Is ARCHON better?** Not competing — ARCHON should *acknowledge the
  lineage* to place its theorem correctly. This preempts the "your theorem
  is trivial" objection: it's not trivial, it's an *application* of a
  well-known theorem to a new setting.
- **Limitation.** Pure theory; assumes no side channels, adequate typing —
  the same assumptions ARCHON's theorem has to make explicit.

---

## Summary Table

| Work / Group | Cited? | Do they compete with ARCHON? | Is ARCHON better? | Key limitation |
|---|---|---|---|---|
| Parnas 1972 | ✅ | No, foundation | Operationalizes it | Just a principle |
| Baldwin & Clark 2000 | ✅ | No, foundation | Operationalizes it | Purely theoretical |
| Sangal 2005 (DSM) | ✅ | Partially — snapshot dep tool | Yes: per-PR, typed, hierarchical | Snapshot only, one edge type |
| MacCormack 2006 | ✅ | No (empirical study) | Different scope | Snapshot only |
| Murphy 2001 (Reflexion) | ✅ | **Yes, closest ancestor** | Yes on 5 axes | Hand-authored model |
| Terra 2009 (DCL) | ✅ | Yes | Yes: derived-then-frozen, per-PR, typed edges | Hand-written rules |
| Passos 2010 (survey) | ✅ | No, catalog | ARCHON adds delta + evidence | Catalogs classical limits |
| **ArchUnit / dep-cruiser / import-linter** | ⚠️ | **YES — the real industrial baseline** | Yes on 5 axes | Hand-authored, whole-repo, imports only |
| REMEDY (2024–25) | ⚠️ | Yes but narrow (MAPE-K) | ARCHON is general | Domain-specific |
| Meyer (DbC) | ✅ | No (technique ARCHON uses) | Not applicable | Adoption is hard |
| QuickCheck | ✅ | No (technique ARCHON uses) | Not applicable | Only as good as your properties |
| CDC / Pact | ✅ | No (technique ARCHON uses) | Not applicable | Needs cross-team infra |
| Dafny | ✅ | No (technique ARCHON uses) | Not applicable | High authoring cost |
| Bazel | ✅ | No (analogy) | Not applicable | Build graph ≠ architecture |
| GumTree | ✅ | No (used inside ARCHON) | Not applicable | File-by-file only |
| **RefactoringMiner** | ⚠️ | No (should be used by ARCHON) | Not applicable | Java primarily |
| **CPG / Joern** | ⚠️ | Adjacent — "program as graph" | Different altitude | Statement-level, security focus |
| **Kazman-Cai / DRSpaces** | ⚠️ | Adjacent — arch debt metric | Complementary | Periodic, not per-PR |
| **Perry & Wolf 1992** | ⚠️ | No (defines problem) | ARCHON is a response | Just definitions |
| **de Silva 2012 survey** | ⚠️ | No (catalog) | ARCHON is a new point in it | Pre-agent era |
| **Degradation review 2025** | ⚠️ | No (catalog) | ARCHON fills its identified gap | Survey, no solution |
| Codex | ✅ | No (proposer ARCHON gates) | Different layer | No arch awareness |
| SWE-agent | ✅ | No (proposer ARCHON gates) | Different layer | Only tests as safety net |
| SWE-bench | ✅ | No (benchmark) | Different scope | Test-pass bias |
| FunSearch / ELM | ✅ | No (proposers ARCHON gates) | Different layer | Needs fitness signal |
| **HarnessAudit / SafeAgent** | ⚠️ | No — complementary layer | Different concern (exec vs arch) | Don't see architecture |
| **RepoReviewer / Claude Review** | ⚠️ | **Yes — the LLM-review alternative** | Different trade-off | Non-deterministic, no audit trail |
| **Reynolds / representation indep.** | ⚠️ | No (theoretical root) | ARCHON is an application | Pure theory |

---

## The bottom line

**Who ARCHON actually competes with:**
- **Reflexion models** (Murphy 2001) — its direct ancestor
- **Industrial fitness functions** (ArchUnit, dep-cruiser, import-linter) —
  what teams *actually* use in CI today; the paper *must* explicitly beat these
- **LLM code reviewers** (RepoReviewer, Claude Code Review) — the alternative
  approach that says "just have an LLM read the diff"

**Who ARCHON does NOT compete with (but the paper should credit clearly):**
- Parnas, Baldwin & Clark — the ideas ARCHON operationalizes
- DbC, QuickCheck, CDC, Dafny — techniques ARCHON places at surfaces
- Codex, SWE-agent — the proposers ARCHON gates
- Bazel, GumTree, RefactoringMiner, CPG — infrastructure ARCHON composes
- HarnessAudit family — a complementary safety layer (execution) below ARCHON's
  (architecture)

**Where ARCHON's honest delta over prior art lives:**
1. **Derived-then-frozen** intended model → fixes reflexion's #1 adoption problem
2. **Per-PR delta at chosen altitude** → makes conformance a review instrument,
   not an audit
3. **Typed operational edges** (config, protocol, capability, platform,
   observability) → not just imports
4. **Evidence-typed contracts at surfaces + boundary-locality result** → enables
   conditional agent automation, not just static hygiene
5. **Snapshot-and-ratchet brownfield adoption** → no flag day, works before
   the repo is clean

Every one of these is defensible; the paper's job is to *earn* each with data,
not just assert it.
