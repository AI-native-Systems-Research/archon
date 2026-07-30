# Writing Style

This paper should read like it was written by a human expert for FSE reviewers:
clear, technical, concrete, and careful about what has been established.

The closest local model is `../papers/memorytime`, especially its emphasis on
earned confidence and reader-first structure. ARCHON should borrow that
discipline without importing every rule. This paper is a software-engineering
systems paper with architecture, review, agents, contracts, and evaluation
design. It needs more navigation than a compact theory paper.

## Voice

Write with calibrated confidence. State what the model proves, what the system
would check, what the evaluation will measure, and what remains open. Do not
blur those categories.

Avoid overclaiming. Words such as "clearly", "obviously", "trivially",
"always", "guarantees", and "the first" invite reviewers to search for
counterexamples. Use them only when the paper has earned them under stated
assumptions.

Avoid defensiveness. State limitations as engineering facts. Do not apologize
for scope. Do not pre-answer objections that the text has not yet raised.

Avoid dismissiveness toward prior work. Credit existing architecture
conformance, modularity, contracts, and agentic software-engineering work as
foundations. State ARCHON's delta crisply.

## Central Object

Keep one operational object at the center of the paper:

> A textual diff shows what changed in files. An architectural delta shows what
> changed in design.

Use this object repeatedly. Documentation is the current graph. Review is the
graph delta. Conformance is policy over the graph delta. Agent safety is the
boundary delta plus a behavioral contract. Brownfield repair is a sequence of
intended graph edits.

The broader thesis is architecture as contract. The operational mechanism is the
architectural delta.

## Argument Shape

Prefer this arc in the abstract and introduction:

1. Start from a concrete engineering problem in large repositories.
2. Give the status quo its best case. Textual diffs, architecture diagrams,
   language visibility, and tests all help.
3. Puncture the status quo with a specific failure. A large diff can hide a small
   architecture change, and a small import can hide a large design change.
4. Introduce ARCHON's object. The architectural delta is the reviewable and
   checkable form of the change.
5. State the conditional safety claim. Boundary locality holds under explicit
   assumptions about encapsulation and contract adequacy.
6. State what must be evaluated. Extraction accuracy, delta legibility, policy
   behavior, brownfield repair, agentic rewrites, and workflow cost.

Do not list definitions and theorems before the reader knows why they are
needed. Give the intuition first, then the formal object, then the consequence.

## Prose Rules

Use short declarative sentences when the argument is complex.

Use active voice when it does not obscure the technical subject.

Prefer concrete nouns and verbs over abstract framing. Write "the PR adds an
arrow from `executor` to `core/sched`" before "the proposal changes the
architecture."

Keep related ideas close. If a theorem depends on assumptions, name the
assumptions in the same paragraph as the theorem's practical meaning.

Use `\paragraph{}` headings when they help technical navigation. ARCHON has many
moving parts, and FSE reviewers will skim. Headings should be short and
substantive, not decorative.

Colons, semicolons, and em-dashes are allowed when they improve clarity. Do not
use them as a substitute for clear sentence structure.

Use contrast forms sparingly. "Not X but Y" is useful when the reader likely
holds X as the default. Otherwise, state Y directly.

## Claims

Every important claim should be one of:

- **proved**, with the assumptions named;
- **implemented**, with the artifact named;
- **measured**, with the method and metric named;
- **planned**, with the research question named;
- **conjectured**, with the uncertainty visible.

For the current draft, avoid language that implies completed implementation or
completed experiments. The paper may define an evaluation plan, but it should
not sound as if those results already exist.

## Evaluation Writing

Frame each experiment as a skeptic's question.

Weak:

> We evaluate ARCHON on three monorepos.

Better:

> RQ3 asks whether architectural deltas are smaller and more reviewable than
> textual diffs in historical pull requests.

For each study, state:

- the question;
- the subject repositories or boxes;
- the artifact being measured;
- the metric;
- the result that would support the claim;
- the result that would falsify or limit the claim.

Name negative results in advance. Escaped regressions, hidden channels, noisy
identity matches, false-positive gates, and unhelpful graph repairs are not
embarrassments. They are the empirical boundaries of the approach.

## Brownfield Framing

ARCHON should be especially clear about brownfield use.

Do not imply that snapshotting a messy graph endorses the mess. The snapshot is
the starting contract. It makes erosion visible and gives teams a ratchet.

Describe cleanup as reviewed graph edits:

- collapse a strongly connected component into an explicit box;
- split an oversized box;
- introduce an adapter boundary;
- remove an allowed arrow;
- narrow a public surface after migrating callers;
- replace a direct dependency with an interface.

The practical promise is not an instant clean architecture. It is incremental
repair without a flag day.

## Checklist

Before submitting a section, check:

- Does the section keep architectural delta or boundary delta in view?
- Does each formal definition answer a question the reader already has?
- Are assumptions attached to the claims that need them?
- Are prior systems credited before the delta is stated?
- Are greenfield and brownfield adoption paths distinguished where relevant?
- Are implementation and evaluation claims phrased as future work unless done?
- Would a reviewer know what result would falsify the claim?
