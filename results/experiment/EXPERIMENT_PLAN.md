# ARCHON evaluation plan — does an architectural diff help review, and does it beat related work?

---

## 1. The claim we are testing

A reviewer (human or agent) who sees a PR as **text diff + ARCHON's architectural
diff** catches architectural change and architectural defects better than one who
sees the **text diff alone**, and better than one given the output of existing
architecture tools. ARCHON is deterministic and works at package altitude; the
question is whether that signal is *legible* and *useful* at review time.

### Research questions
- **RQ1 (ablation):** text diff + ARCHON vs text diff only — does the architectural
  diff improve detection of the real architectural change / defect?
- **RQ2 (baselines):** text diff + ARCHON vs text diff + each related-work tool —
  does ARCHON's signal beat the alternatives, holding the reviewer fixed?
- **RQ3 (legibility, precondition):** can a **human** reviewer actually *see* the
  architecture and the issue from each tool's artifact? This gates everything: if
  we can't read it, an agent judge is meaningless.
- **RQ4 (triage):** does ARCHON's fast-track-vs-review verdict track real
  architectural change better than the commit-message proxy?

---

## 2. Conditions (the independent variable = what the reviewer is given)

The reviewer prompt/packet is always `text diff + <context blob>`. Only the context
blob changes:

| ID | Condition | Context blob |
|----|-----------|--------------|
| **C0** | Control | none (text diff only) |
| **C1** | ARCHON | ARCHON delta `--summary` + `--json`, the delta render, and `evidence` gaps |
| **B1** | Dep-graph baseline | a Go dependency-graph diff (before vs after) |
| **B2** | Call/arch baseline | a call-graph or layer-conformance tool's output |
| **B3** | Recovery baseline | an architecture-recovery view (reuse the `gnn-*` artifacts) |

Reviewer held fixed within a comparison. Two reviewer *types*, run in phases:
- **Phase 1: human** (us) — the legibility gate.
- **Phase 2: agent** — Opus 4.8 as the reviewer, LLM judge for scoring.

**Fairness control:** each tool's raw output is normalized to a context blob of
comparable size and format, so RQ2 isn't just "who dumped more text." Record blob
token counts and report them.

---

## 3. Corpus + ground truth

Two repos, three sources of truth:

1. **todo-api** (`code/todo-api`, MarioCarrion's microservice). The author
   documented the architectural change per commit → **existing answer key**. The
   `cache-aside`, `kafka`, `vault` variants already have ARCHON output
   (`*.archon.txt`) and a written review (`*.reviewB_archon.md`). Reuse these as
   real, documented cases.
2. **Seeded mutations** (controlled, objective precision — see §5). Injected into a
   clean base commit of todo-api (and optionally BLIS) so the expected finding is
   known exactly.
3. **BLIS** (`inference-sim`). The 100-PR labeled sample (`blis_pr_labels.csv` /
   `BLIS_PR_LABELS.md`). **Needs the human pass** the caveat calls for, then it is
   the real-PR realism set for RQ1/RQ4.

**Ground-truth record (one per case):**
```json
{ "case_id": "todo-cache-aside",
  "repo": "code/todo-api", "before": "<sha>", "after": "<sha>",
  "is_architectural": true,
  "change_kind": ["new-external-service","new-contract"],
  "expected_findings": ["memcached decorator fronts elasticsearch",
                        "service:Memcached new external dependency",
                        "memcached.Datastore interface introduced"] }
```

---

## 4. Metrics

**Phase 1 — human legibility gate (per case × condition):**
- Can the reviewer, from the artifact alone: (a) describe the architecture, (b)
  name the change, (c) spot the issue? Each scored **found / partial / missed**.
- **Legibility rating** 1–5 and **time-to-find**.
- Gate rule: if humans can't see the architecture/issue from a condition's artifact,
  fix the presentation before any agent run. This is the deliverable of Phase 1.

**Phase 2 — agentic, at scale (per case × condition):**
- **Detection:** precision / recall / F1 against `expected_findings`.
- **Triage (RQ4):** fast-track-vs-review vs `is_architectural` → P/R/F1, confusion.
- **Noise:** false-positive findings per review.
- **Judge score:** rubric via LLM judge, **only after** the judge is validated
  against human labels on a shared subset (report agreement, e.g. Cohen's κ).

---

## 5. Variants — the mutation catalog ("apply variants")

Each variant is a script that edits a clean base commit and records its
`expected_findings`, producing a before/after pair with known truth. Covers the
edge kinds ARCHON reasons about, plus a negative control:

| Variant | Injected change | Expected ARCHON signal |
|---------|-----------------|------------------------|
| V0 | rename / reformat only (negative control) | empty delta → fast-track |
| V1 | new upward/cross-layer import | new dependency edge; reflexion violation |
| V2 | new external service / capability / config | new world-node boundary |
| V3 | add or sever a contract (interface implementer) | contracts changed |
| V4 | add implementer with **no bound test** | evidence gap (uncovered) |
| V5 | introduce a dependency cycle / grow a god-module | health regression |

V0 is the "does it stay quiet when nothing architectural happened" check — as
important as the positives.

---

## 6. The experiment framework (what we build)

A standalone driver (Python, in `results/experiment/eval/`) that, per case:

1. `git diff before..after` → the text diff.
2. **Trigger ARCHON:** `archon-go delta … --summary --json`, `render … delta.png`,
   `archon-go evidence` → artifacts.
3. **Trigger each baseline** (§7) → raw artifacts → normalized context blob.
4. **Assemble** each condition's reviewer packet (`text diff + blob`) from the
   neutral prompt (`AGENTIC_NEUTRAL_PROMPT.md`).
5. **Run reviewer** (Phase 1: emit human packets; Phase 2: Opus 4.8).
6. **Score** vs `ground_truth.json`; **persist everything**.

**Persistence layout (content-addressed, reproducible):**
```
results/experiment/eval/
  cases/<case_id>/ground_truth.json
  tools/<tool>/<case_id>/…            # raw ARCHON / baseline outputs
  conditions/<case_id>/<condition>/prompt.md  review.md  score.json  blob_tokens.txt
  runs/<run_id>/manifest.json         # tool versions, model, seeds, case list
  report/summary.csv                  # one row per (case, condition, run)
```
ARCHON is deterministic; fix reviewer temperature/seed and store every prompt so a
run reproduces byte-for-byte where possible.

---

## 7. Baselines (mixed representative set)

Pick one per row; final choice pending an installability check (§10):

- **B1 dependency graph:** `goda` (or `go mod graph` / `godepgraph`) — diff the
  package graph before vs after. Closest structural apples-to-apples.
- **B2 call / conformance:** `go-callvis` (call graph) or `arch-go` /
  `go-arch-lint` (layer-rule conformance) — closest to ARCHON's call + reflexion
  angle.
- **B3 architecture recovery:** reuse the existing `gnn-*` artifacts in
  `../artifacts/` as the learned-recovery baseline (architecture/components/layers).

Each baseline gets its *best reasonable* output so the comparison is fair, then is
normalized to a comparable blob.

---

## 8. Execution phases

- **Phase 0 — skeleton (smoke):** framework + `ground_truth.json` schema; wire
  ARCHON + one baseline on `todo-api` cache-aside end to end; confirm artifacts
  persist and re-run is stable.
- **Phase 1 — human legibility gate:** run all conditions on the todo-api variants
  and a handful of BLIS PRs; **we read the artifacts and score see/find**; iterate
  on presentation until the architecture and the issue are legible in each
  condition. (Explicit precondition before any agent judge.)
- **Phase 2 — scale + agents:** add remaining baselines; run the full todo-api
  variant set + seeded mutations + the human-passed BLIS sample; Opus 4.8 reviewer;
  validate the LLM judge against the Phase-1 human labels; compute all metrics.
- **Phase 3 — analysis:** RQ1–RQ4 tables + figures for the paper.

---

