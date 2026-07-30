# ARCHON — Implementation & Evaluation: Next Steps

A design/planning doc. Tags: **[DECISION]** must choose before building,
**[OPEN]** unresolved question to think about, **[BUILD]** concrete work item,
**[DONE]** already exists. Grounded in what we've built + `LITERATURE_LESSONS.md`.

---

## 0. The gating decision — resolve this before building anything `[DECISION]`

**ARCHON is Go-only** (built on `go/packages` + `go/types`). **Every labeled
dataset we want is Python/C++**: OpenStack Nova & Neutron are Python; Qt is
C++/Python. As the evaluation is written, it cannot run — ARCHON can't parse the
subjects it's being evaluated on. This decision gates all implementation:

| Path | What it means | Cost / risk |
|---|---|---|
| **A. Pilot with hand-made deltas first** *(recommended start)* | Hand-produce ARCHON-style deltas for ~3 OpenStack sweet-spot cases; test whether feeding them to an AI reviewer beats diff-only. | ~1 day. Tests the core hypothesis + contamination BEFORE investing. Decide B vs C after. |
| **B. Build a Python extractor** | Extend ARCHON to run directly on Nova/Neutron. | Biggest lift. Python has no static interface satisfaction, dynamic imports, duck typing — `implements`/`service`/`schema` edges are much harder than in Go. High risk, high reward (unlocks the datasets). |
| **C. Evaluate on Go; use OpenStack qualitatively** | Run the quantitative AI-reviewer experiment on Go subjects (todo-api, BLIS, other Go repos we label ourselves); OpenStack cases become qualitative illustrations. | Matches the tool as built; loses ready-made labeled scale (we'd label Go PRs ourselves — the design already allows ≥2 annotators). |

**Recommendation:** do the **pilot (A) first** — it tells us whether the delta
actually helps and whether contamination is fatal, cheaply, and *that result*
decides between B and C. Don't build the Python extractor on faith.

---

## 1. Evaluation design (refined from the current draft)

### Review conditions (keep) `[BUILD]`
Per PR, ask multiple AI reviewers to review the same change under:
1. textual diff only,
2. diff + dependency-graph baseline,
3. diff + other architecture-analysis baseline,
4. diff + ARCHON architectural delta.

### Ground truth (keep)
Original expert reviewer comments (layer violations, improper dependencies,
interface problems, service-boundary violations).

### Scoring (keep) `[BUILD]`
AI judge matches each generated review to a ground-truth concern (returns the
matching concern + the reviewer's statement + supporting evidence); a **human**
verifies and assigns binary 1/0. New/extra concerns → judge extracts, human
validates. *(Human stays the final arbiter — good.)*

### Metrics (keep)
Ground-truth concern-detection rate; additional valid concerns; false-positive
rate; review token cost; review time; architectural-delta size vs textual-diff
size.

### Refinements to ADD

**(a) Stratify results by concern type — and lead with ARCHON's sweet spot.**
ARCHON detects dependency/interface/service concerns, not semantic ones. Report
per-type; headline the ✅ rows:

| Example case | Concern | ARCHON detects? |
|---|---|---|
| Nova 275073 | cross-service interface | ✅ strongest (service/api edges) |
| Nova 312488 | layer violation | ✅ (needs layer labels) |
| Neutron 87841 | wrong dependency | ✅ (import edge) |
| Nova 250907 | cyclic dependency | ✅ (graph traversal) |
| Nova 219153 | problematic interface | ⚠️ partial (implements; signature gap) |
| Neutron 195439 | inconsistent implementations | ⚠️ partial |
| Nova 229964 | duplicated capability | ❌ needs semantic similarity |
| Nova 282580 | responsibility overload | ❌ needs fan-out heuristic |

→ Averaging over all types understates the tool and looks weak; per-type is
honest and stronger on the cases that are genuinely ARCHON's.

**(b) Validity threats to design in now `[OPEN]`:**
- **LLM contamination.** OpenStack reviews are public and old → AI reviewers were
  likely trained on them and may *recall* the concern rather than derive it from
  ARCHON. Mitigate: feed only the pre-review patch; probe whether the model
  recognizes the PR; prefer the newest PRs; report it as a threat. Check whether
  "Are Developers Aware…" has more recent PRs.
- **Fair comparison.** Only claim improvement on concern types ARCHON can detect;
  report the ❌ types separately (expected ~no gain).
- **Sample size / power.** Decide how many PRs per concern type for a meaningful
  comparison; the 472/606 comments are not all per-PR or all in-scope — filter.
- **Baseline choice.** Pin the concrete dep-graph baseline per language
  (import-linter/pydeps for Python; a Go dep tool for Go) and the "other
  architecture baseline" (Arch-Engine? a static conformance checker?).

### Headline result (hypothesis, state honestly as a hypothesis)
"Without ARCHON, AI reviewers identified X% of expert-reported architectural
issues; with ARCHON, ~2X+%, with the largest gains on cross-layer, cross-service,
and interface changes." — a *target*, not a finding yet.

---

## 2. Implementation — priority order

1. **Pilot (de-risk) `[BUILD]` — do first.** 3 sweet-spot cases (Nova 275073,
   312488, Neutron 87841). Produce the ARCHON-style delta (by hand, since Python
   isn't supported), run diff-only vs diff+delta on 1–2 AI reviewers, and check:
   does it help, and is contamination a problem? Gate the rest on this.
2. **Dataset prep `[BUILD]`.** Parse the Zenodo datasets into structured
   `PR → [expert concerns]` records (concern text, category, the PR/patch, the
   pre-review revision). Filter to in-scope, per-PR architectural concerns.
3. **Eval harness `[BUILD]`.** Orchestration: per PR, assemble the 4 conditions,
   run N AI reviewers, collect outputs. Language-agnostic (ARCHON's delta is just
   one input string).
4. **Judge + human-scoring pipeline `[BUILD]`.** LLM judge → structured
   match/evidence; human verification UI/sheet → binary scores; aggregation into
   the metrics above.
5. **Language extractor decision `[DECISION]` → B or C** (from §0), informed by
   the pilot.

### Graph-model gaps worth prioritizing (from `LITERATURE_LESSONS.md`)
Ordered by leverage for the evaluation:
- **Layer labels on packages `[BUILD]`** — needed to call a cross-layer edge a
  *layer violation* (Nova 312488). Cheap; high value for the headline cases.
- **Delta encoding for the AI reviewer `[BUILD]`** — serialize the delta as
  relationships ("A now calls B", "X implements contract I, untested") and
  **precompute structural facts** (cycles, new cross-layer/cross-service edges);
  do NOT hand the model a raw graph. (NLGraph / Talk-like-a-Graph: LLMs are weak
  graph reasoners; incident encoding ≫ adjacency.)
- **Interface method signatures `[OPEN]`** — a signature change on a contract is
  invisible to the current `implements` edge (Nova 219153).
- **Duplicate / obsolete functionality `[OPEN]`** — ARCHON's #1 honest gap vs the
  erosion data (duplicate = 15.5% of concerns). Needs semantic-similarity /
  dead-code analysis, not edges. **Decide: add, or scope out explicitly** in the
  paper.
- **Intent vs. actual `[OPEN]`** — ~1/5 of erosion comments are "violates what we
  decided." Our snapshot-and-ratchet baseline is the mechanism; make sure it's in
  the story.

---

## 3. Open questions to think about `[OPEN]`
- **Language path** (§0): pilot → then Python extractor (B) or Go + qualitative
  OpenStack (C)?
- **How many PRs / which concern types** are in-scope for the headline claim?
- **Contamination:** newer PRs, recognition probe, or accept-and-report?
- **Baselines:** which concrete dep-graph tool and which "other architecture
  baseline" per language?
- **AI reviewers:** which models, how many, what prompt/framing for each of the 4
  conditions?
- **Duplicate/obsolete/overload:** add detection, or explicitly out of scope?

---

## 4. Suggested near-term sequence
1. Run the **3-case pilot** (hand-made deltas) → measure help + contamination.
2. Based on the pilot, **pick B or C** (language path).
3. Build **dataset parsing → harness → judge/scoring** (language-agnostic; can
   start in parallel with the pilot).
4. Add **layer labels** + the **delta-encoding-for-LLM** step (both cheap, both
   directly raise the headline-case numbers).
5. Decide the scope of the **semantic gaps** (duplicate/obsolete/overload) for the
   paper.
