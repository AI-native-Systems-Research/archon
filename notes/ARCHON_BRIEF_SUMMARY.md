# ARCHON — Brief Summary (as of Jul 28, 2026)

*Intended for a git issue / discussion to bring the team up to speed. Deeper
detail lives in `LITERATURE_REVIEW.md`, `TODOS_AND_DESIGN.md`,
`GRAPH_DESIGN_AND_LLMS.md`, and the `code/archon-go` tool.*

## The problem

AI now writes code faster than humans can review it. Pull requests arrive faster
than reviewers can keep up, and a **textual diff is a poor way to answer the
question that decides how a PR should be handled: did this change move an
architectural boundary?** A 2,000-line refactor may move no boundary, while a
10-line edit can add a new service dependency, widen an API, or implement a new
interface — and the diff makes both look like "just some lines changed."

## The idea

**ARCHON automatically recovers a repo's architecture from source, computes the
architectural change each PR introduces, and presents it as a structured
"architectural delta."** The shift is not "showing architecture" — it is making
**architectural change the unit of control for every PR**: changes that *preserve*
boundaries can be fast-tracked, and only changes that *move* a boundary are
escalated to a human. The same artifact serves a human reviewer and an AI
reviewer, and gives a machine-checkable gate for agent-generated changes.

Key property: every relationship records the code that realizes it (a "witness
set"), so a change that only moves files or rewrites function bodies produces an
**empty** delta — no architecture review needed. That is what turns a huge diff
into "nothing changed architecturally," and it is what makes the merge gate
meaningful.

## What's built today `[done]`

A working Go tool (`code/archon-go`) that extracts a **package-altitude graph**,
diffs two commits into an architectural delta, and renders it (Graphviz/Mermaid,
delta colored). It models:

- **Nodes:** internal packages (with their public surface + the tests that guard
  them), external packages, and synthetic operational nodes for services
  (`service:Kafka`), API endpoints (`api:POST /tasks`), config keys (`env:…`),
  and capabilities (`cap:net`). Data **schemas** (serialized struct fields) are
  tracked as a separate axis.
- **Edges (typed, witnessed):** *compiler* edges — import, call,
  interface-implements; *operational* edges — service, config, protocol (HTTP
  endpoints), capability.
- **Contract coverage:** for each interface, which implementers a bound test
  actually exercises → covered vs. an **evidence gap**; a modified/deleted test is
  flagged as "a promise on this contract changed."

## Evidence so far `[done]`

- **todo-api** (a microservice with per-commit documented changes = ground
  truth). ARCHON correctly recovered real architectural changes on actual commits:
  a Memcached cache-aside decorator (with a flagged *missing consistency test*),
  a Kafka broker swap, a Vault/secure-config change, and an OpenAPI adoption where
  it even caught an **endpoint rename** — all invisible or buried in the textual
  diff.
- **BLIS** (clean Go simulator, invariant-rich). Extraction ~1.4s; 2,226 guarding
  tests found, 257 bound to a contract; same-commit comparison yields an empty
  delta (no false churn). An earlier run found ~57% of PRs had an empty
  architectural delta *(to be re-confirmed with the current edge set)*.

## Where it sits vs. prior work (honest novelty)

We surveyed the literature and shipping tools (see `LITERATURE_REVIEW.md`, buckets
A–J). **Table-stakes — already done, not our contribution:** showing a graph/map
of PR changes (CodeSee, now defunct; CodeRabbit's per-PR diagrams), static
dependency graphs (Sourcegraph, NDepend), AI PR reviewers using a codebase graph
(Greptile, Qodo). The nearest architectural PR-diff tools are **Erode** (needs a
hand-maintained model) and **Arch-Engine** (dependency/rule-based).

**What no shipping tool does — the defensible core:** (1) typed extraction across
*many* relationship kinds (services, APIs, config, schemas, interfaces) with no
hand-drawn model; (2) witness-based **empty-delta** semantics (refactor = empty);
(3) tying changes to the **contracts/evidence** they put at risk; (4) the per-PR
architectural delta as a first-class object and merge gate — plus a **formal
model** and a **measurement study** of whether the delta actually improves review.
*Framing note:* we lead with the delta semantics + evidence + measurement, **not**
"we visualize PRs," since visualization is contested commercial ground.

## Evaluation plan `[planned]`

- **Subjects:** BLIS and todo-api now; vLLM / Tauri later for cross-language and
  operational-edge stress.
- **Unit:** one merged PR, before vs. after, over a deliberate mix of PR types
  (bug fix, refactor, dependency bump, config/service/API/schema change,
  performance, test-only, architecture reorg).
- **Ground truth:** maintainer docs where available (todo-api), plus the OpenStack
  architecture-erosion review datasets — real expert-flagged concerns like layer
  violations, cross-service interface problems, cyclic dependencies (Li et al.,
  ICSA 2022 / IST 2023).
- **Headline metrics:** empty-delta fraction; delta compression (vs. textual diff
  size); false-negative rate (empty deltas that were actually architectural — the
  safety number); routing accuracy. For the AI-reviewer story: whether an AI
  reviewer given ARCHON's delta reproduces the expert's architectural concern more
  often, and at lower token cost, than one given only the diff.
- **Who judges what:** humans + docs for "was this architectural"; automated
  property/metamorphic/differential tests for "did behavior change"; an LLM only
  as a *scale helper* or as the *reviewer under test* — never the final arbiter.
- **Baselines:** textual diff, dependency-graph diff (import-linter), Erode /
  Arch-Engine, CHID (impact/risk), and an LLM reviewer with vs. without ARCHON.

## Next steps

1. **Killer case studies** that make the motivation concrete: a small,
   innocent-looking PR that hides a real boundary move; a locally-correct change
   that violates a system-level invariant; and an AI-generated PR that is clean +
   tested but drifts architecturally (to construct).
2. **De-risk novelty:** run CodeRabbit/Greptile on our sample PRs to pin down the
   closest commercial overlap concretely.
3. **Re-confirm the BLIS empty-delta fraction** and scale the todo-api
   ground-truth confusion matrix.
4. **Scope the AI-reviewer experiment** (model, task framing, metrics) and stand
   up the evaluation harness on the OpenStack datasets.

## Open questions

- Altitude: stay at package level or add function-level nodes + rename/move
  identity (so refactors stay empty at the finer grain)?
- Operational edges are heuristic — how to keep precision high without noise?
  (Payoff is per-repo: config lit up todo-api but not config-light BLIS.)
- How much evidence must a boundary carry before an agent-generated change can be
  auto-merged (the ambitious frontier)?

*Team on ARCHON: Naima Abrar, Mert Toslali.*
