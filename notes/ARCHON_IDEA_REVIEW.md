# ARCHON: A Critical Review of the Research Idea

*A reviewer-style assessment of the ARCHON draft ("Architecture as Contract: Typed
Graphs for Reviewable and Agentic Software Evolution"), grounded in a literature
review. Prepared as an internal working document, not for submission.*

---

## 1. TL;DR verdict

**Yes, this is a good research-paper idea** — with an important caveat about
*framing and evidence*, not about the concept.

The concept is timely, well-motivated, and sits on a genuine and widening gap:
human review still operates on textual diffs, while both cross-cutting human
refactors and LLM agents increasingly produce changes whose *architectural*
effect is invisible in that diff. ARCHON's central object — an **architectural
delta** (a typed, hierarchical graph diff projected to a review altitude) that
serves simultaneously as documentation, review lens, and admission gate for
automated proposers — is a clean, unifying idea with real explanatory power.

The risk is not novelty of the *vision* but **defensibility of the delta over
prior art plus the absence of results**. In its current form the paper is a
*design + evaluation plan*, not an evaluated system. That is publishable in some
venues (vision/new-ideas tracks) but is a hard sell as a full FSE research paper,
where reviewers will expect at least the extraction and retrospective-review
studies (RQ1–RQ2) to be *done*. The single strongest move available is to build
the extractor and run the retrospective PR study on the three named repos before
submission.

Below: what the idea is, why it is good, where it is weak, what it does and does
not address, how impactful it can be, how to make it more impactful, and a fairly
thorough map of related work — both work that *does* something similar and work
that *establishes the problem*.

---

## 2. What the idea actually is (plain-language restatement)

Strip away the notation and ARCHON makes five claims:

1. **Represent the repo as a hierarchical, typed, attributed graph.** Boxes nest
   (repo ⊇ package ⊇ module ⊇ function). Arrows are *typed*, and crucially not
   only "import/call" but *operational* edges: protocol fields, config keys,
   capabilities/permissions, metrics, traces, platform targets, toolchain policy,
   external runtime dependencies. Coarse arrows are *derived by aggregation* from
   fine leaf edges, so the package-level picture cannot silently disagree with the
   code.

2. **Review the graph delta, not the text diff.** The same physical change has
   different deltas at different altitudes. A 1,000-line internal rewrite can be
   *empty* at the package altitude. This turns "blast radius" from a gut feeling
   into a measured quantity, and it is recomputed from source every push.

3. **Make node identity stable under rename/move** (via stable IDs → signature
   match → GumTree-style tree diff). Without this, a refactor reads as
   delete+create and every delta is noise. The paper is careful: bad identity hurts
   *legibility*, not *safety* (the gates still run on the post-change graph).

4. **Two doors gated by one predicate — "does the boundary delta change?"**
   Empty boundary delta + preserved surface contract + discharged evidence →
   Door 1 (auto-admit internal evolution, the agent fast path). Any boundary change
   → Door 2 (human reviews the *delta*, which advances the intended baseline).

5. **A conditional boundary-locality theorem.** Under two explicit assumptions
   (surface-mediated interaction / no hidden channels; adequate contract), an
   empty-boundary, contract-preserving change is observationally unobservable
   outside the box. The theorem is deliberately narrow — its job is to *name the
   preconditions the review pipeline must establish*, not to assert safety.

Plus a deployment philosophy that is the paper's most practically credible part:
**greenfield contract-first** vs **brownfield snapshot–classify–ratchet**, where
the first shipped product is a *review-triage report* (boundaries touched, owners,
implicated contracts, missing evidence) rather than a proof, and instrumentation
edits are treated as first-class architectural repairs.

The unifying insight worth stating crisply: *documentation = the current graph;
review = the graph delta; conformance = policy over the delta; agent safety =
boundary delta + behavioral contract; brownfield repair = a sequence of intended
graph edits.* One artifact, many consumers.

---

## 3. Is this a good research-paper idea?

### The case for "yes"

- **It names a real, growing problem precisely.** "A large diff can hide a small
  architecture change, and a small import can hide a large design change" is a
  crisp, memorable framing of something every large-repo maintainer feels. The
  agent angle makes it urgent rather than evergreen.
- **The core object is genuinely unifying.** Most prior work does *one* of
  {documentation, conformance, review, agent-gating}. Collapsing them into
  projections of one versioned artifact is a real conceptual contribution, not
  just an engineering mash-up.
- **The two-altitude projection is the technical heart and it is elegant.** The
  "arrows aggregate, contracts are restated" asymmetry is a thoughtful,
  non-obvious design decision, and the authors flag it as such.
- **Intellectual honesty is high.** Conditional theorem, named assumptions,
  negative results named in advance, identity-vs-safety separation. This is the
  posture reviewers reward.

### The case for "not yet, as a full paper"

- **No results.** Everything empirical is a *plan*. FSE/ICSE/ASE research tracks
  increasingly bounce "here is a design and we will evaluate it" papers.
- **The delta over conformance checking must be earned, not asserted.** Reflexion
  models, DSMs, dependency-constraint languages, and fitness-function tools
  (ArchUnit, dependency-cruiser, import-linter) already do structural
  conformance in CI. The paper lists four differentiators; reviewers will test
  each one empirically.
- **The theorem is arguably the weakest "contribution."** It is essentially the
  classical *representation-independence / contextual-equivalence* result (Parnas
  options, Reynolds/Mitchell representation independence) restated for this graph.
  Its assumptions (no hidden channels; adequate contract) are exactly what fails
  in the messy repos the paper targets. That is fine *if* it is sold as
  "framework for stating preconditions," which the draft mostly does — but a
  skeptical reviewer will call it near-tautological.

**Recommendation on framing:** target either (a) a full paper *after* RQ1–RQ2 are
executed, or (b) an ISSTA/FSE-IVR / ICSE-NIER / *Onward!* style
new-ideas/vision venue that explicitly rewards well-argued designs with a plan.
Do not submit as a full research-track paper with only a plan.

---

## 4. Strengths (pros)

1. **Right problem, right moment.** Agentic SE is exploding; the safety/review
   substrate for it is underdeveloped. This is a substrate paper for a wave that
   is already here.
2. **Operational edges are the sleeper contribution.** Modeling config, protocol,
   capability, platform, and observability edges is where ARCHON escapes "toy
   module diagrams." The vLLM/Tauri/BLIS exemplars make this concrete and
   credible, and this is genuinely underexplored in the conformance literature.
3. **Brownfield story is unusually mature.** Snapshot-then-freeze the intended
   graph, classify debt, ratchet, treat instrumentation as repair — this directly
   answers the #1 reason architecture-conformance tools die in practice: the
   upfront cost of authoring the intended model.
4. **Separation of fixed substrate vs open proposers (the "single funnel").**
   This is a strong systems idea: it future-proofs the work against whatever agent
   framework wins, and makes the acceptance boundary proposer-independent.
5. **Identity is treated as a measured research question**, not hand-waved. Tying
   it to GumTree/RefactoringMiner-style detection is the right instinct.
6. **Calibrated writing.** The STYLE.md discipline (proved / implemented /
   measured / planned / conjectured) is visible in the prose and will land well.
7. **Evaluation design is genuinely good** even though unexecuted: the
   asymmetric RQ structure, the "realizes/exceeds/conflicts/unrelated" plan
   relation, and naming escaped regressions as the primary negative result are all
   the marks of someone who has reviewed papers.

---

## 5. Weaknesses, risks, and likely reviewer objections (cons)

1. **It's a plan, not a result** (already covered). Biggest single risk.
2. **Extraction precision is load-bearing and hardest exactly where it matters.**
   The whole edifice rests on faithfully extracting the *actual* graph. Dynamic
   imports, reflection, monkey-patching, DI containers, code generation, config-
   driven wiring, and cross-language boundaries (vLLM is Python/C++/CUDA/Rust) are
   precisely where extraction is weakest — and precisely where hidden channels
   (Assumption 1) live. If extraction is only 80% precise on operational edges, the
   triage report becomes noise, which the paper itself admits is fatal.
3. **The boundary-locality theorem may be seen as thin.** See §3. Under-specified
   contracts + hidden channels are common, so Door 1 frequently degrades to
   "ordinary tests," which is... the status quo agent safety net. Reviewers will
   ask: *in real repos, what fraction of boxes can actually carry an adequate
   contract?* This is the true open question and the paper knows it (RQ4), but it
   currently answers it only with a conjecture ("favorable economics").
4. **Contract authoring cost.** Behavioral contracts at every surface you want to
   automate is a large human cost. The "author once, amortize over unbounded
   changes" argument is plausible but unproven, and history (Eiffel/DbC, JML,
   formal specs) suggests contract adoption is *hard*.
5. **The hierarchy assumption.** Projection assumes a containment *tree*. Real
   architectures are often a *web* (feature-based cross-cutting, plugins,
   cross-package cycles). The acyclicity gate (G1) is strict; the "treat SCC as a
   box" escape hatch is reasonable but under-developed, and cycle-heavy repos are
   common.
6. **Scope sprawl.** The paper tries to be documentation + review + conformance +
   agent gating + brownfield repair + governance + executable invariants. That is
   a lot of surface for reviewers to attack, and dilutes the "one crisp result."
   Ironically, a *narrower* paper (e.g., "architectural deltas make PR review
   measurably better") might be stronger.
7. **Human-factors claims are unvalidated.** "Reviewers find graph deltas more
   legible" is a UX/empirical claim requiring a user study, not just metrics on
   delta size. Delta size ≠ cognitive load.
8. **Baseline strength.** ArchUnit + import-linter + CODEOWNERS + Danger/reviewdog
   + a dependency graph already give teams a lot. The marginal value must be shown
   against a *strong* modern baseline, not against "nothing."

---

## 6. What limitations does ARCHON actually address?

Framed against the status quo, ARCHON meaningfully addresses:

- **Diff-vs-design mismatch.** The central win: separating "what changed in files"
  from "what changed in design," and making the latter first-class and measurable.
- **Documentation drift.** Because coarse arrows are *derived*, the architecture
  picture cannot silently rot away from the code — a direct answer to the
  architecture-erosion / drift literature (§9.1).
- **Upfront-model cost of conformance tools.** Snapshot-and-ratchet lets the
  intended model be *derived-then-frozen*, sidestepping the classic reflexion-model
  adoption barrier.
- **Refactor noise.** Stable node identity makes a rename/move a no-op delta rather
  than a maximal one — a real pain point in code review.
- **Review routing.** Owner/reviewer/test routing from implicated boundaries is a
  concrete, shippable value even with no automation.
- **Agent over-reach.** Door separation directly attacks the "I only tuned a
  function" failure mode where an agent quietly adds a dependency or widens a
  surface.
- **Unobservable invariants.** Treating instrumentation as architectural repair is
  a genuinely nice reframing that few tools capture.

## 7. What it does *not* address (honest boundaries)

- **Semantic correctness beyond contracts.** If the contract is silent, Door 1 is
  only as safe as the test suite. It does not solve verification.
- **Hidden channels in practice.** It *names* them (Assumption 1) and proposes
  sound over-approximation as future work, but does not solve escape/effect
  analysis for dynamic languages.
- **Non-architectural friction.** Driver bugs, model-specific issues, environment
  misconfig — explicitly out of scope (the vLLM discussion is honest about this).
- **Cross-repo / distributed architecture.** External boxes are opaque; true
  multi-repo governance (Tauri↔TAO/WRY) is acknowledged but not solved.
- **Non-tree architectures.** As above.
- **Cost of contract authoring at scale.** Assumed favorable, not demonstrated.

---

## 8. How impactful could this be?

**Ceiling: high. Floor: moderate. Expected: solid, if executed.**

- **If only RQ1–RQ2 land** (extraction + retrospective review value): a useful,
  citable tool paper in the architecture-conformance / code-review lineage. Real
  but incremental impact — it competes with a crowded fitness-function ecosystem.
- **If RQ4 lands even partially** (a measured account of *how much contract
  coverage buys how much safe autonomy*, with a taxonomy of escaped regressions):
  this becomes a **reference framework** for the agentic-SE-safety community. The
  "boundary delta + behavioral contract = admission condition" formulation could
  become the vocabulary people use to reason about when an agent's change is
  "internal." That is high impact.
- **The unifying framing itself has memetic potential.** "Architectural delta"
  vs "textual diff" is the kind of one-liner that gets adopted. If the paper
  ships a usable open-source extractor for even one ecosystem (Go is the cleanest),
  adoption could carry the idea further than the paper.

The impact is bottlenecked by **execution and extraction precision**, not by the
idea. The idea is strong enough to matter; the question is whether the artifact
can be made real on messy repos.

---

## 9. Related work: a fairly thorough map

I organize this by (a) work that *does something similar* (so ARCHON must position
against it) and (b) work that *establishes the problem*. Items already cited in
`refs.bib` are marked ✅; items **not yet cited but that reviewers will expect**
are marked ⚠️ and are the most important gaps to close.

### 9.1 The problem: architecture erosion, drift, and degradation

- ✅ **Parnas (1972)** — information hiding; the intellectual root of
  boundary-locality (a stable surface *is* the option to change the interior).
- ✅ **Baldwin & Clark, *Design Rules* (2000)** — modularity as options; ARCHON's
  economic argument for contracts leans on this.
- ⚠️ **Perry & Wolf (1992)**, *Foundations for the Study of Software Architecture*
  — origin of "architectural erosion/drift" terminology. Should be cited as the
  problem's canonical source.
- ⚠️ **de Silva & Balasubramaniam (2012)**, *Controlling software architecture
  erosion: A survey* (JSS) — the standard erosion survey. A must-cite.
- ⚠️ **Li et al. (2022), *Understanding Software Architecture Erosion*** (JSEP /
  related SMS) and the **2025 "Architectural Degradation" multivocal review
  (arXiv:2507.14547)** — recent syntheses showing detection is well-studied but
  *continuous, preventive remediation is the gap* ARCHON's ratchet targets. Strong
  positioning cite: ARCHON is a remediation-and-prevention substrate, not another
  detector.

*Positioning:* ARCHON's "documentation is the derived graph, so it cannot drift"
is a direct structural answer to this whole line. Say so explicitly.

### 9.2 Architecture conformance checking (closest prior art — DOES this)

- ✅ **Murphy, Notkin, Sullivan — Software Reflexion Models (2001).** The direct
  ancestor: compare an implementation's dependencies to an architect-supplied
  high-level model. ARCHON's deltas over G1–G3 are the reflexion idea made
  per-change and per-altitude.
- ✅ **Terra & Valente — Dependency Constraint Language (2009); Passos et al. —
  Static Architecture-Conformance Checking overview (2010).** The lineage of the
  structural gates.
- ⚠️ **Knodel & Popescu (2007)** — comparison of reflexion, relation-conformance,
  and component-access rules. Useful for precisely stating the delta.
- ⚠️ **REMEDY / MAPE-K ACC (2024–2025, arXiv:2401.16382)** — recent
  domain-specific conformance with DSL + recovery + conformance engine. Nearest
  *recent* competitor; shows the "reduce modeling effort via domain knowledge"
  theme ARCHON also pursues (via snapshotting). Cite to show currency.

*Positioning:* ARCHON's four claimed differentiators — (i) derived-then-frozen
model, (ii) per-change per-altitude delta, (iii) operational edges, (iv)
evidence-typed contracts + locality result for *conditional automation* — are the
crux. Reviewers will demand empirical support for (i)–(iii); (iv) is the novel
conceptual axis.

### 9.3 Industrial fitness functions / dependency enforcement (DOES this, at scale)

- ⚠️ **Ford, Parsons, Kua — *Building Evolutionary Architectures* (2017)** —
  "architectural fitness functions." ARCHON's gates *are* fitness functions with a
  hierarchy and a delta. **This must be cited**; it is the industrial framing of
  the same idea and its omission is a glaring gap.
- ⚠️ **ArchUnit (Java), ArchUnitNet/.NET, dependency-cruiser (JS/TS),
  import-linter (Python), NetArchTest, jQAssistant/Structurizr.** These are what
  practitioners actually use in CI today. ARCHON's structural gates overlap
  heavily with them. The paper *must* distinguish itself: these tools are
  (a) hand-authored rules, (b) mostly import/call edges only, (c) whole-repo
  pass/fail rather than per-change altitude-projected deltas, and (d) no identity
  model, contracts, or agent-gating. That is a defensible delta — but only if the
  baseline is named.
- Note the AI-pattern-book observation that fitness functions work *especially*
  well in agentic loops (agents fix violations they see in the feedback loop) —
  this is direct external support for ARCHON's Door-1/Door-2 gating thesis.

### 9.4 Graph program representations (DOES the "repo as graph" part)

- ⚠️ **Yamaguchi et al. — Code Property Graphs (S&P 2014); Joern.** The most
  prominent "program as unified queryable graph" work. ARCHON differs: CPGs merge
  AST/CFG/PDG for *intraprocedural security* analysis at statement level; ARCHON
  is *inter-module, hierarchical, typed with operational edges, versioned, and
  diffed per change*. But reviewers in the analysis community will expect this
  cite and a crisp distinction. Joern's "overlays / levels of abstraction" is a
  notable parallel to ARCHON's projection operator.
- ✅ **Design Structure Matrices — Sangal et al. (2005); MacCormack et al.
  (2006).** ARCHON explicitly relates projection to DSM aggregation. Good.
- ⚠️ **Feature/knowledge/architecture recovery** (e.g., Bunch clustering, ACDC,
  ARC/architecture recovery by Garcia, Medvidović et al.) — relevant to the
  *brownfield snapshot* step (how good is the auto-recovered box structure?).
  Worth a sentence: ARCHON's snapshot quality inherits recovery limitations.

### 9.5 Architectural technical debt (DOES the "measure/ratchet debt" part)

- ⚠️ **Kazman, Cai, Mo, Xiao — Design Rule Spaces (TSE 2018), Hotspot Patterns
  (WICSA 2015), Decoupling Level (ICSE 2016), Titan toolset; "Architectural Roots
  of Technical Debt" (ICSE 2015).** This is the canonical ATD-metrics line and
  ARCHON's "debt ratchet / distance-to-target-graph" is squarely in its territory.
  **Important gap.** ARCHON should cite DL as a candidate debt metric for its
  ratchets, and position its contribution as *per-change ratcheting* rather than
  *periodic debt measurement*.
- ⚠️ **Nord et al. (2012), *In Search of a Metric for Managing Architectural
  Technical Debt*** — ATD framing.

### 9.6 Node identity / refactoring detection (DOES the identity part)

- ✅ **Falleri et al. — GumTree (2014).** Cited; correct basis.
- ⚠️ **Tsantalis et al. — RefactoringMiner / RMiner (ICSE 2018, TSE 2020),
  RefDiff (Silva et al.).** State-of-the-art refactoring detection with ~99%
  precision / ~98% recall on move/rename — *exactly* the signal ARCHON's identity
  correspondence needs, and a natural baseline/component for its identity RQ.
  **This is a must-cite**; it both strengthens feasibility ("move/rename detection
  is a largely solved problem") and gives a ready-made evaluation oracle.

### 9.7 Contracts and verification (DOES the "evidence" part)

- ✅ **Meyer — Design by Contract; Claessen & Hughes — QuickCheck; Fowler/Robinson
  — Consumer-Driven Contracts; Leino — Dafny.** Correctly positioned as techniques
  ARCHON *places* at surfaces rather than reinvents.
- ⚠️ Consider **Pact** (industrial CDC tooling) and **metamorphic testing surveys
  (Chen et al.)** since the eval leans on metamorphic/differential oracles.

### 9.8 Agentic software engineering & its safety (the "why now" — DOES/NEEDS this)

- ✅ **Codex (Chen 2021); SWE-agent (Yang 2024); SWE-bench (Jiménez 2024);
  FunSearch (Romera-Paredes 2024); ELM/Evolution-through-LLMs (Lehman 2023).**
  Good coverage of proposers.
- ⚠️ **AutoCodeRover, Agentless, RepairAgent, and repo-level agent surveys
  (2024–2025).** Reviewers will want a couple of *current* agentic-SE cites beyond
  SWE-agent.
- ⚠️ **The 2025–2026 "agent harness safety" line** — HarnessAudit,
  execution-grounded verification, and industrial guardrail systems (SafeAgent,
  Phalanx). These argue safety must be enforced by the *harness/substrate*, not the
  model, and audited over the whole trajectory. **This is powerful external
  validation of ARCHON's entire "fixed substrate + single funnel" thesis** and
  should be cited: ARCHON supplies the *architectural* layer of exactly the
  substrate this community says is needed. Distinction: those systems check
  execution/security/side-effects; ARCHON checks *architectural boundary
  preservation*. They are complementary layers of the same guardrail stack.
- ⚠️ **LLM code-review systems** — RepoReviewer (multi-agent, repo-level),
  Anthropic Claude Code Review (whole-repo context, self-verification), and
  agentic architecture-review-in-CI work. These are the *competing* approach to
  ARCHON's problem: use an LLM to read the PR in repo context and flag
  architectural issues. ARCHON's answer — deterministic, replayable, typed graph
  deltas vs probabilistic LLM judgment — is a strong and honest contrast worth
  making explicitly. (Deterministic gate + LLM proposer is arguably the right
  synthesis, and ARCHON's funnel already allows it.)

### 9.9 Theory ARCHON's theorem descends from (should acknowledge)

- ⚠️ **Reynolds (1983) / Mitchell — representation independence; Morris/Leroy —
  contextual (observational) equivalence; module-as-existential-type (Mitchell &
  Plotkin).** ARCHON's boundary-locality theorem is a software-architecture
  instance of representation independence / contextual equivalence. Citing this
  lineage would (a) strengthen the theorem's pedigree and (b) preempt the
  "near-tautology" objection by placing it correctly as *applied* theory.

---

## 10. What would make it more impactful

Ordered roughly by leverage.

1. **Build the extractor and run RQ1 + RQ2 before submission.** Even one ecosystem
   (Go/BLIS is cleanest; Rust/Tauri richest for operational edges) with real
   numbers on extraction precision and retrospective delta-vs-diff would transform
   the paper from "plausible plan" to "demonstrated substrate." This is the single
   highest-leverage action.
2. **Ship a usable open-source tool + a public dataset of labeled architectural
   deltas.** Datasets and tools carry ideas. A "PR → architectural delta" corpus
   across vLLM/Tauri/BLIS would be independently citable and would let others build
   on the vocabulary.
3. **Report the contract-coverage/autonomy curve (RQ4), even small.** The field's
   real open question is *how much evidence buys how much safe autonomy*. A
   measured curve on 2–4 boxes, with the escaped-regression taxonomy (hidden
   channel / weak contract / weak evidence), would be the most novel empirical
   result in the paper and could define the sub-area.
4. **Do a small human study on legibility.** Even n=8–12 developers rating
   graph-delta vs diff-only review for the same PRs would substantiate the "review
   value" claim that metrics alone cannot.
5. **Tackle hidden channels head-on with sound over-approximation** for at least
   one dynamic-language pattern (e.g., Python globals/monkey-patching surfaced as
   extra arrows), and *measure the precision cost*. This directly shores up
   Assumption 1, the theorem's weakest leg.
6. **Position against the strong modern baseline explicitly** (ArchUnit /
   import-linter / CODEOWNERS / LLM reviewers), ideally head-to-head on the same
   PRs. Winning against "nothing" convinces no one.
7. **Narrow the framing for the flagship claim.** Consider leading with the
   *review* contribution (architectural deltas measurably improve PR review) and
   treating agentic evolution as the forward-looking application, rather than
   splitting the reader's attention across seven sub-stories. Keep the substrate
   general, but let one result be the headline.
8. **Close the citation gaps in §9** (fitness functions/ArchUnit, CPG/Joern,
   Kazman-Cai ATD, RefactoringMiner, erosion surveys, agent-harness-safety,
   representation-independence theory). Several of these are *expected* cites whose
   absence would itself draw reviewer fire.
9. **Add one greenfield exemplar** (the paper promises this) so the contract-first
   path is not purely hypothetical against three brownfield cases.

---

## 11. Problems and limitations (plain English)

This section lists the things that could sink the paper or the system, roughly
from most dangerous to least. For each one I say what the problem is, why it
hurts, and what evidence would make it convincing. Simple version: the *idea*
survives all of these. What decides the paper is whether we honestly measure
*where the abstraction leaks*.

### Tier 1 — problems that hit the core claim

**1. The "adequate contract" assumption rarely holds for popular code.**
The safety theorem needs the behavioral contract to capture everything a caller
depends on. But if a piece of code has many users, people start depending on
*every* little behavior of it — exact error messages, ordering, timing — even
things nobody wrote in the contract (this is "Hyrum's Law"). So for the popular
boxes where auto-evolution would help most, a truly complete contract is almost
impossible, and Door 1 quietly falls back to "the tests passed," which is just
today's situation.
*What's needed:* a **contract-coverage census** — on real code, measure how many
boxes can actually carry a good-enough contract that an independent checker finds
no escaped bugs. If that number is small, we should sell Door 1 as "less risk +
shows missing evidence," not "safe automation."

**2. The false-negative rate is the number that matters, and it's the hardest.**
"The architectural delta is smaller than the text diff" is almost always true
(any summary is smaller) and does not prove the tool is good. The number that
proves *safety* is the opposite one: **how often does an 'empty' delta hide a
change that really did matter architecturally?** Every such case slips through
Door 1 or gets marked "no review needed" when it should have been reviewed.
*What's needed:* a dedicated **false-negative study** with humans labeling "was
this actually an architecture change?" on a mixed sample of PRs, plus a check
that different labelers agree. Reporting only delta size will look like measuring
the easy thing.

**3. "Boundary didn't change" does not mean "behavior didn't change."**
Even with the same surface, same types, and same allowed arrows, a change can
still alter: thrown exceptions, error codes, ordering, timing/latency, memory
use, concurrency, and — big one for vLLM — floating-point/numeric results.
Callers can observe these, but they're almost never in a contract.
*What's needed:* state clearly that the boundary check is *necessary but not
sufficient*, and show in the evaluation that the independent checker
(differential, metamorphic, and numeric-tolerance tests) catches these cases.
An agent "optimizing" a GPU kernel with an empty boundary delta can still change
the output numbers — a great honest test case.

### Tier 2 — the graph might not match reality (extraction)

**4. Cross-language / FFI edges are invisible, and vLLM is multi-language.**
vLLM is Python + C++ + CUDA + Rust glued with FFI (pybind, ctypes). Static tools
work one language at a time, so the edge that crosses the language boundary — often
the most important one — gets dropped. This is a bigger and more certain gap than
the dynamic-dispatch one.
*What's needed:* either build an FFI-boundary extractor plus "external boxes" for
the native side, or make a **single-language repo (Go/BLIS) the flagship** and
treat vLLM as a stress case. Claiming precision on a repo we can't fully parse is
risky.

**5. Macros, decorators, templates, and generated code fool the parser.**
Rust macros, C++ templates, Python decorators/`__getattr__`, and build-time code
generation mean the code we read isn't the code that runs. Generated code adds
fake edges; expanded macros hide real ones.
*What's needed:* extract from the *expanded / generated* code where possible, and
report an explicit "extractor unsure" edge category with numbers in RQ1.

**6. Extraction must give the same answer every time.**
If upgrading a parser or indexer silently changes the extracted graph, every such
upgrade looks like a fake change and the baseline slowly rots.
*What's needed:* pinned, versioned extractors and a test that the same commit
always produces the same graph.

### Tier 3 — gaps in the formal model

**7. Node identity is defined as a one-to-one match, but split/merge are not.**
Agents and refactors often split one function into two, or merge two into one.
A strict one-to-one matching literally cannot represent that, so it shows up as
delete + create — exactly the noise the identity idea was meant to remove.
*What's needed:* allow identity to be a relation (or add explicit split/merge
operations) and cite RefactoringMiner, which already detects split/merge well.
This is a real hole in the current draft, not just a nice-to-have.

**8. Rolling edges up loses "how strong" the dependency is.**
An arrow exists if *any* single leaf edge exists. So one throwaway debug import
looks the same as deep, everywhere coupling. Worse, a coarse arrow only
disappears when the *last* underlying edge is removed: deleting 9 of 10 uses of a
dependency shows an "empty" delta (looks like nothing happened), while adding the
*first* use fires a "new dependency!" alarm for a one-line change.
*What's needed:* keep the **count** of underlying edges, and show "witness count
changed" as its own smaller signal, not just present/absent.

### Tier 4 — will teams actually use it (adoption)

**9. A green "no review needed" badge can make reviewers stop looking.**
If people trust the badge, they pay less attention — so when the leaky checks
(problems 1 and 3) let something bad through, there are now *fewer* human eyes on
it than before the tool existed. A tool that lowers vigilance needs a higher
safety bar than a tool that only adds information.
*What's needed:* present the first version as **extra help** (routing +
missing-evidence report), not as review removal, and ideally check whether
reviewers actually disengage on "empty delta" PRs.

**10. The shared baseline is a bottleneck and a big classification chore.**
Every boundary-changing PR updates the intended graph, so two PRs editing it at
once can conflict on the *architecture* itself. And on a messy existing repo,
the first snapshot dumps thousands of edges that someone has to label as
"intended" vs "tolerated debt" — endless, unglamorous work. This is the classic
reason such tools die.
*What's needed:* a clear plan for baseline merge conflicts, and evidence that
labeling can be done **lazily** (only when a PR touches that boundary) instead of
all up front. Measure "time to first useful report" without full labeling.

**11. Speed on a big repo is claimed, not shown.**
The rule checks are cheap, but extracting the whole graph and matching identities
on every push over a large monorepo may not be. If it doesn't fit the CI time
budget, nobody adopts it.
*What's needed:* incremental extraction (only re-do changed files and what
depends on them) and real timing numbers on a large repo.

### Tier 5 — someone gaming the system

**12. The gate becomes a target for a clever or prompt-injected agent.**
Once "empty boundary delta" means "safe for agents," a bad proposer can craft a
change that looks empty but does its real work through a channel the extractor
can't see (network, global state, reflection) — the exact "hidden channels" the
theorem assumes away. Testing only with honest agents won't reveal this.
*What's needed:* state the threat model, and use the dynamic / audit-hook pass to
check that a Door-1 change added no unexpected file/network/process edge. Here the
dynamic analysis is not a bonus — it's a security requirement.

### Short list of "must-haves" to be convincing

1. False-negative study (empty deltas that were actually architectural).
2. Contract-coverage census (how often the safety assumption can even hold).
3. Cross-language/FFI handling, or lead with a clean single-language repo.
4. Dynamic augmentation pass (for completeness *and* hidden-channel/security check).
5. Fix the identity model for split/merge.
6. Reproducible extraction + speed/incrementality numbers.
7. Head-to-head against a strong baseline (ArchUnit/import-linter + an LLM reviewer).
8. Reframe Door 1 as risk-reduction + evidence surfacing until 1 and 2 justify more.

---

## 12. How extraction actually works, and does prior tooling kill the paper?

### 12.1 How to build the graph (the practical pipeline)

The most important decision: **don't write your own call-graph / name resolver —
reuse an existing code indexer.** Working out "who calls or uses whom" across
generics, overloads, and imports is a multi-year job per language, and it's
already been solved. So extraction is a *composition* of existing pieces:

- **Stage 0 — Get a semantic index, not just raw syntax.** Run the language's
  indexer: **SCIP / LSIF** (`scip-python`, `scip-typescript`, `scip-java`,
  `scip-clang`, plus `rust-analyzer` and `gopls`), or **CodeQL** (a queryable
  database of the code) or **Joern** (a code property graph). This gives you three
  hard things for free: (1) every symbol with a **stable global ID** — which is
  exactly ARCHON's top-tier node identity, so moves/renames match automatically;
  (2) already-resolved "who references whom" links (typed edges, not text); and
  (3) signatures and visibility, i.e. the public surface.
- **Stage 1 — Nodes + containment tree.** Group indexed symbols by scope:
  package/dir → file → class → function. Use the symbol ID as the node's identity.
- **Stage 2 — Compiler-visible edges.** Turn each reference into a leaf edge
  `caller → callee`, typed as `import`, `call`, `type-use`, `inherit`,
  `implement`. This is a simple transform of what the indexer already gives you.
- **Stage 3 — Operational-edge extractors (the part you actually build).** The
  indexers do *not* give config/protocol/capability edges. You write small
  analyzers: env-var and flag reads (config), `.proto`/OpenAPI/pydantic models
  (protocol fields), manifests (capabilities/permissions), metric/trace SDK call
  sites (observability — these are just calls to known SDK symbols, so you can
  reuse the indexer's data), and `Cargo.toml`/`go.mod`/`cfg`/CI files (platform,
  toolchain). This stage is the only genuinely new work — and, not by accident,
  it's where the paper's novelty lives.
- **Stage 4 — Dynamic pass (optional evidence layer).** Run the tests with
  tracing / audit hooks; label edges as `confirmed`, `dynamic-only` (possible
  hidden channel), or `static-only-unexercised`.
- **Stage 5 — Aggregate + save.** Roll leaf edges up the tree into coarse arrows,
  keeping the set (and count) of underlying edges. Save one graph per commit.
- **Stage 6 — Diff + identity.** Between two commits, match nodes by stable ID
  first, then by signature, then by GumTree/RefactoringMiner for the rest.
  Compute the delta and project it to the chosen review level.

Tools you would compose or compare against: tree-sitter (fast incremental
parsing, good for CI), SCIP/LSIF, CodeQL, Joern/CPG, `grimp`/`import-linter`/
`pydeps` (Python), `go/packages` (Go), `cargo-modules` (Rust),
`dependency-cruiser` (JS/TS), Sourcegraph, Meta's Glean. A very believable build
is literally "extraction = some CodeQL/SCIP queries + a few operational-edge
analyzers."

### 12.2 If extraction tools already exist, is the paper still interesting?

**Yes — but only if we say clearly that the extractor is *not* the contribution.**
If the pitch is "we built an architecture graph extractor," the paper is dead;
that space is mature and crowded. Here's what already exists:

| Category | Existing tools | What they do |
|---|---|---|
| Dependency views | pydeps, dependency-cruiser, Sourcegraph, Structure101, Lattix, NDepend, CodeScene | Show the graph (a snapshot) |
| Conformance | ArchUnit, import-linter, Sonargraph | Enforce **hand-written** rules, whole-repo pass/fail |
| Program graphs | CodeQL, Joern/CPG, Glean | Queryable code databases |
| Architecture debt | Titan / Design Rule Spaces | **Periodic** debt measurement |

But **none** of them do the things that make ARCHON new:

1. **The per-change architectural *delta* as the review unit** — projected to a
   chosen level, with node identity so a refactor is a *no-op*. Existing tools
   give whole-repo snapshots or rule pass/fail; none give "here is the design
   change *this PR* makes, with rename/move noise removed." This is the new object.
2. **One derived artifact doing documentation + review + agent-gating** at once.
   Today those are three separate tools that drift apart.
3. **Typed operational edges beyond imports** (config, protocol, capability,
   platform, observability). Almost all existing tools are import/call-only — and
   this is also where extraction itself still has an *unsolved* research question
   (operational-edge accuracy and cross-language edges), so there's real novelty
   even inside extraction, just not in the plain import graph.
4. **Evidence-typed contracts + the two-door protocol for admitting agent
   changes.** No architecture tool connects the graph to *conditional autonomy*.
5. **Snapshot-and-ratchet brownfield method** built on the delta.

So the right framing — which actually *strengthens* the paper — is: "We do not
reinvent extraction; we compose mature indexers (SCIP/CodeQL/tree-sitter). Our
contribution is the layer on top: the identity-stable, level-projected
architectural **delta** as a review and agent-admission unit, typed **operational**
edges, an evidence/contract regime, and the boundary-locality protocol." Leaning
on existing extractors makes the work more credible and lets us spend our novelty
budget where it counts.

**The catch:** once extraction is "glue over existing tools," a reviewer may say
"then this is glue plus an easy theorem." The answer is *not* more extractor
engineering — it's delivering the real results from §11's must-have list
(delta-legibility / false-negative study, contract-coverage census, and the
agentic Door-1 study). The novelty here is **conceptual and empirical**; the
extractor is just the infrastructure that makes those measurable.

---

## 13. My overall take

This is a **strong idea with a mature design and an honest research posture**, held
back mainly by being pre-results and by a few conspicuous missing baselines. The
intellectual center — *architectural delta as the reviewable, checkable,
proposer-independent unit of change* — is genuinely good and, I think, correct: it
is the right abstraction for the human+agent review world we are entering. The
operational-edge modeling and the brownfield ratchet are the parts most likely to
make it *useful* rather than merely elegant, and they are underexplored elsewhere.

My main worry is not the concept but the **gap between the clean model and the
messy repos it targets**. The theorem is safe but slight; the value lives entirely
in whether extraction is precise enough on operational edges and whether contract
coverage can be grown cheaply. Both are empirical, and both are currently
promissory. The paper is one solid extraction-plus-retrospective study away from
being convincing, and two studies (add RQ4) away from being influential.

If I were advising: **build the Go/BLIS extractor, run RQ1–RQ2, add the missing
citations (especially fitness functions, RefactoringMiner, CPG, Kazman-Cai, and
the agent-harness-safety line), and either hold for a full paper with results or
submit the current draft to a vision/new-ideas track now while the agentic-SE wave
is cresting.** The window for the framing is open; the evidence bar for the full
claim is not yet met.

---

### Appendix: key references to add (BibTeX keys suggested)

| Theme | Work | Why |
|---|---|---|
| Erosion problem | Perry & Wolf 1992; de Silva & Balasubramaniam 2012 (JSS survey) | Canonical problem statement |
| Erosion (recent) | Architectural Degradation multivocal review 2025 (arXiv:2507.14547) | Currency; "remediation gap" |
| Fitness functions | Ford, Parsons & Kua, *Building Evolutionary Architectures* 2017 | Industrial framing of the gates |
| Enforcement tools | ArchUnit; dependency-cruiser; import-linter | Real CI baseline to beat |
| Graph representation | Yamaguchi et al., Code Property Graphs (S&P 2014); Joern | "Repo as graph" prior art |
| ATD metrics | Kazman/Cai/Mo/Xiao — DRSpaces (TSE 2018), Decoupling Level (ICSE 2016), Titan | Debt ratchet metric basis |
| Refactoring detection | Tsantalis et al., RefactoringMiner (ICSE 2018 / TSE 2020); RefDiff | Node-identity feasibility + oracle |
| Agent harness safety | HarnessAudit; execution-grounded verification (2025–26) | Validates "substrate not model" |
| LLM review | RepoReviewer; Claude Code Review | Competing/complementary approach |
| Theory | Reynolds 1983 (representation independence); contextual equivalence | Pedigree for the theorem |
| Conformance (recent) | REMEDY / MAPE-K ACC 2024–25 | Recent conformance competitor |

*(All URLs/DOIs gathered during the review are available in the search notes; add
formal entries to `refs.bib` as the draft matures.)*
