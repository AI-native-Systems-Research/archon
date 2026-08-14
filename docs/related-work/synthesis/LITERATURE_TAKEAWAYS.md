# ARCHON — What We Learned from the Related Work

A reflective summary: from reading the related work, what became clearer, what
gave us confidence, and what we'd been neglecting. Plain English, papers cited in
brackets. (Deeper detail in `LITERATURE_LESSONS.md`; the paper list in
`LITERATURE_REVIEW.md`.)

---

## What gave us more confidence

- **A graph is the right representation for reasoning about code.** Multiple
  independent results show that using code *structure* beats treating code as
  flat text: data-flow structure improved code understanding [GraphCodeBERT, Guo
  et al., ICLR 2021], and an ablation showed accuracy jumping from ~55% (syntax
  only) to ~85% once semantic edges were added [Learning to Represent Programs
  with Graphs, Allamanis et al., ICLR 2018]. So "represent the repo as a graph"
  isn't a hunch — it's well-founded.

- **The edges we chose are the high-signal ones.** Those same ablations show that
  the *semantic* edges — calls, data flow, type/interface relationships — carry
  the signal, while plain imports are the weak baseline [Allamanis 2018;
  GraphCodeBERT 2021]. ARCHON's `call` and `implements` edges are exactly these;
  `import` alone was never going to be enough, and now we know why.

- **The problem is real — reviewers genuinely argue about architecture.** The
  OpenStack studies mined thousands of review comments and found hundreds about
  layer violations, cyclic dependencies, interface problems, and cross-service
  boundaries [Symptoms of Architecture Erosion in Code Reviews, Li et al., ICSA
  2022; Warnings…, Li et al., IST 2023]. We're not inventing a need; we're
  targeting one that shows up in real review threads.

- **Our operational edges are genuinely new.** Services, APIs, config, and
  capabilities do not appear in the academic code-graph literature — those papers
  are intra-program (AST/CFG/data-flow) [Code Property Graph, Yamaguchi et al.,
  S&P 2014; Allamanis 2018]. That's real novelty territory, not a reinvention.

- **Feeding a graph to an LLM demonstrably helps at repo scale** — so the
  AI-reviewer angle is plausible: injecting a repo graph improved repo-level tasks
  [Code Graph Model, Tao et al., NeurIPS 2025], and giving agents a repo graph cut
  token cost ~26% while keeping/raising accuracy [LLM Agents Can See Code
  Repositories, ASE 2026].

- **No competitor combines what we do.** Erode needs a hand-drawn model,
  Arch-Engine is dependency-only, CHID is a risk score — each has a piece, none
  has {auto-recovery + many typed edges + witness-based empty-delta +
  evidence-tied}. The combination is ours.

---

## What became clear that we hadn't fully thought through

- **Why a graph and not a tree.** An AST is a tree, and tree methods work well for
  *local, single-file* things (naming, edit scripts) [code2vec, Alon et al., POPL
  2019; GumTree, Falleri et al., ASE 2014]. But the relationships that matter for
  architecture — calls, imports, interface-implements — **cross the tree** (they
  link nodes that aren't parent/child) [Allamanis 2018]. So we can now state
  crisply: a tree is enough for one file's syntax; a graph is *required* the
  moment you reason about relationships between units — which is all of review.

- **LLMs are weak graph *reasoners*, which changes how we use them.** This was the
  biggest "we hadn't thought of that" moment. LLMs fail at graph algorithms and
  can't even reason about what's *not* connected [Can Language Models Solve Graph
  Problems in Natural Language?, Wang et al., NeurIPS 2023], and their performance
  swings wildly with how the graph is written down — "A calls B" phrasing scored
  ~54% vs ~20% for an adjacency matrix on the same task [Talk like a Graph,
  Fatemi et al., ICLR 2024]. Implication: **don't hand the LLM a raw graph and ask
  it to reason** — compute the structural facts (cycles, new cross-layer/service
  edges) with the tool, and give the model those facts as plain relationships.
  This is a concrete design rule we didn't have before.

- **Bigger isn't better for the graph.** More granularity has a cost — a full AST
  can *hurt* a model versus a focused subset [GraphCodeBERT 2021]. This backs our
  choice to stay at the **package altitude** for review rather than chasing
  function-level detail.

- **There are established answers to "the graph is too big."** We'd worried about
  this abstractly; the literature is concrete: pull a **1-hop neighborhood around
  the changed nodes** (1-hop beat 2-hop; more adds noise) [RepoGraph, Ouyang et
  al., ICLR 2025], retrieve-then-rerank [CGM 2025], or summarize clusters
  [GraphRAG, Edge et al., 2024]. Our instinct — the delta *is* the changed
  neighborhood — is exactly right; now we know how to bound it.

- **"Showing a graph of PR changes" is table-stakes, not our contribution.** This
  reframes the whole pitch: shipping tools already visualize PR changes (CodeSee,
  CodeRabbit). Our novelty is the *typed, witness-based, evidence-tied delta* and
  the *measurement that it helps review* — not the picture. (From the commercial
  prior-art survey.)

---

## New aspects we were neglecting

- **Semantic concerns we can't currently detect — and they're common.** Reviewers
  flag **duplicate functionality (≈15.5% of erosion comments — their #2)**,
  obsolete functionality, and responsibility overload [Li et al., ICSA 2022].
  ARCHON's edges can't see these; they need semantic-similarity / dead-code /
  fan-out analysis. We must either add such detection or **explicitly scope it
  out** in the paper — silence would look like a blind spot.

- **Intent vs. "what is."** About a fifth of erosion comments are "this violates a
  decision we made," not a structural fact [Li et al., IST 2023]. ARCHON captures
  *what is*; encoding *intent* is what the snapshot-and-ratchet baseline is for —
  and it needs to be visible in the story, echoing the original idea of comparing
  intended vs. actual architecture [Reflexion Models, Murphy et al., 1995/2001].

- **Interface *signatures*, not just "who implements."** We track "T implements
  I," but a change to a method's signature on the contract is invisible to that
  edge — yet it's a real contract change [Code Property Graph, Yamaguchi et al.,
  S&P 2014]. Worth adding.

- **Layer labels.** The single most common concern is the *layer violation*
  (≈15.9%) [Li et al., ICSA 2022], but to *call* a cross-layer edge a violation we
  need packages tagged with their layer. Cheap to add, high payoff for the
  headline cases.

- **Delta encoding is a first-class design choice (for the AI reviewer).** Given
  the LLM-graph findings above, *how* we serialize the delta to a model matters as
  much as what's in it [Talk like a Graph, ICLR 2024]. We hadn't treated encoding
  as part of the design.

- **Evaluation contamination.** Thinking through the eval surfaced a threat we
  hadn't: the OpenStack reviews are old and public, so the AI reviewers were
  likely trained on them and may *recall* the concern rather than *derive* it from
  ARCHON. This has to be designed around (pre-review patch only, recognition
  probe, prefer newer PRs).

---

## The single biggest reframe

Two things, together:
1. **Our contribution is not "show architecture" — it's the typed, witness-based,
   evidence-tied architectural delta plus the measurement that it helps review.**
   Visualization is already shipping elsewhere; the semantics and the study are
   ours.
2. **The tool doesn't reason over the graph — it computes the delta, and the human
   or LLM acts on it.** The graph is the substrate; the *artifact* is a small,
   focused, plainly-encoded delta. This is the lesson from both the human side
   (show one altitude, the changed neighborhood — C4/DSM tradition) and the LLM
   side (LLMs can't reason over raw graphs) [NLGraph 2023; Talk like a Graph
   2024].

## Net effect on the plan
- **Confidence up** that graph-as-representation and the per-PR delta are sound,
  and that the problem is real.
- **Sharper** on why-graph-not-tree, why-package-altitude, and how to bound a big
  graph.
- **New must-dos:** layer labels; a delta-encoding step for the AI reviewer;
  precompute structural facts rather than asking the model; and an explicit
  decision on the semantic gaps (duplicate/obsolete/overload) — add or scope out.
- **A reframed pitch:** lead with the delta semantics + evidence + measurement,
  not the diagram.
