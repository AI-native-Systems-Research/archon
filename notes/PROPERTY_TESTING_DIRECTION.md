# ARCHON — Property Testing & Invariant Evolution: Where It Fits, the Gap, Open Questions

Situates the mentor-endorsed "invariants evolve with the architecture" direction
against (a) what ARCHON already does, (b) what we learned from related work, and
(c) what's genuinely still open. Tags: **[DONE]**, **[GAP]** not built,
**[OPEN]** decide before building. Papers cited in brackets.

---

## Where this fits (the big picture)

- The mentor chose the **deeper interpretation**: as the architecture evolves,
  the system's **invariants evolve with it**, and **property tests provide the
  evidence** those invariants still hold. Plus: always run the property tests in
  CI, and **flag when a property test is modified/deleted** (you touched a
  promise).
- Strategically this is not a side feature — it's **ARCHON's defensible
  novelty**. The prior-art survey showed competitors (Erode, Arch-Engine, CHID)
  tie changes to *nothing*; none link a change to the guarantee it affects. The
  evidence/invariant layer is exactly the "tied-to-evidence" piece that no other
  tool has. So this direction is the differentiator, and it plugs into three
  things we already have:
  1. **The evidence gate** — "which guarantee now needs validating."
  2. **The two-doors / merge gate** — touching an invariant, or adding an
     implementer no test covers, is a boundary event → escalate to a human;
     enough invariant coverage is what could make Door-1 auto-merge defensible.
  3. **The AI reviewer** — implicated invariants + touched tests + uncovered
     implementers are exactly the kind of *computed facts* we should hand the LLM
     (per the lesson that LLMs should act on facts, not reason over the graph
     [NLGraph, Wang et al., NeurIPS 2023]).

- It rests on two classic ideas: **property-based testing** as the evidence type
  [QuickCheck, Claessen & Hughes, ICFP 2000] and **consumer-driven contracts** as
  the placement — a consumer relies on a subset of a provider's behavior [Fowler,
  2006]. Our "an interface is a contract; every implementer must pass its test"
  is essentially Liskov-substitution / consumer-driven contract testing.

---

## What we've ALREADY built (v1 of this direction) `[DONE]`

We implemented a first cut earlier this session (in `code/archon-go`):

- **The invariant↔boundary mapping** — the thing Naima flagged as the open
  representation question — is done via **type-inferred binding**: each guarding
  test carries `Guards` (the interfaces it references) and `Exercises` (the
  concrete types it drives). So a test is bound to the contract it protects,
  automatically, no annotations.
- **Contract coverage** — for each interface, ARCHON checks which implementers a
  bound test actually exercises → **covered ✓** vs **uncovered = evidence gap**.
  This is the mentor's "interface-level property tests, so every implementation of
  the interface goes through the same test."
- **Touched-invariant flagging** — a modified/deleted guarding test is reported as
  *"a promise on contract X changed"* — exactly the mentor's "flag if property
  tests were modified/deleted."
- **A runnable demo** (`fixtures/contract`, c1–c4): a `Store` interface with
  mem/file/redis implementers and a table-driven `TestStoreContract`; adding an
  uncovered implementer → evidence gap; covering it → ✓; weakening the test →
  "promise changed."

So the **static half of the mentor's direction already exists**: the mapping, the
coverage check, and the touched-test flag.

**Reality check from the subjects:** neither BLIS nor todo-api uses a
property-testing *library* (rapid/Hypothesis) — BLIS invariants are named tests
(Conservation, Determinism, …). So our binding treats "invariant = a test bound
to a contract," not "imports a PBT library." That matches reality.

**A confidence boost from the erosion data:** one real reviewer concern is
"inconsistent implementations" — different implementations of the same role
diverge [Neutron 195439; Warnings…, Li et al., IST 2023]. Interface-level contract
tests are precisely the mechanism that catches this (every implementer runs the
same test), so the mentor's idea maps onto a documented review need.

---

## The gap — what's NOT built yet `[GAP]`

1. **Actually running the tests as evidence.** The mentor wants all property tests
   run in CI. Today we verify coverage **statically** — a bound test *exists* and
   *references* each implementer — but we don't **execute** it and report
   pass/stale. Closing this = the real "evidence discharged" (the paper's G4).
2. **Invariants that *evolve* (the deep, novel part).** We do "new implementer of
   an existing interface ⇒ must be covered." We do **not** yet do "a new *kind* of
   box implies a new *kind* of invariant is now owed" — e.g., a new cache ⇒ a
   cache-consistency property; a new queue ⇒ at-least-once/ordering. That needs a
   **box/edge-kind → invariant-template catalog**, and it's the most novel but
   most speculative piece.
3. **How an interface-level test enumerates its implementers.** The mentor's open
   question. We infer coverage from type references (works for clean table-driven
   tests, loose otherwise). We haven't settled a convention by which a repo
   declares "this test covers all implementers of I" that ARCHON cross-checks
   against the implements-edges.
4. **The "ARCHON-compatible repo" policy** — the mentor's idea that, whenever a PR
   touches architecture, property-test updates should be bundled in. We *flag* a
   touched test; we don't yet *require*/gate that a boundary change comes with the
   corresponding invariant update.

---

## What we still need to think about before implementing more `[OPEN]`

1. **Static coverage vs. running tests.** Do we stay at "a bound test exists and
   references each impl" (cheap, language-agnostic), or build a per-repo
   test-execution step that runs the bound tests and reports satisfied/failed/
   stale? The latter is what the mentor literally asked for but needs a runner per
   ecosystem. *Recommendation: this is the highest-value next step — it makes the
   evidence real.*

2. **The mapping representation: inference vs. convention vs. hybrid.** We built
   pure **inference** (test→interface via types). The mentor's "ARCHON-compatible
   repo" hints at an explicit **convention** (a repo declares which test guards
   which contract, and interface tests enumerate their implementers). Options:
   (a) keep inference, (b) add an explicit registry/marker, (c) hybrid (infer by
   default, marker to override). This is a real fork — inference is
   zero-adoption-cost but loose; a convention is precise but asks something of the
   repo.

3. **How interface-level tests enumerate implementers.** If we go the convention
   route: a table/registry the test ranges over, which ARCHON cross-checks against
   the implements-edges (registered set == implementer set, else evidence gap).
   Decide the convention.

4. **The new-invariant catalog — in scope or just a suggestion?** "New cache ⇒
   cache-consistency" is exciting and novel, but domain-specific and
   hard-to-generalize. Decide: (a) build a small catalog (cache/queue/retry →
   invariant templates) and *suggest* the owed invariant, or (b) defer and only do
   "new implementer ⇒ cover it." *Recommendation: start as a suggestion/flag layer
   ("new box of kind cache — consider a consistency invariant"), not enforcement.*

5. **Subjects lack interface-level property tests.** For a real end-to-end demo /
   eval we must author them (we did in `fixtures/contract`). Decide which repo(s)
   get authored contract tests, and whether the eval needs repos that already have
   them.

6. **Contract adequacy — how much is enough?** The paper's assumption is that
   enough evidence makes an internal change substitutable (Door-1). *How much*
   coverage before we trust auto-merge is an open empirical question (RQ4). The
   property-testing layer is what makes that claim testable at all.

7. **Paper scoping.** Is invariant-evolution the **flagship** (the evidence-tied
   novelty), or a secondary contribution behind the review-value result? Given the
   competitor survey, it's the strongest novelty claim — but it's also the least
   built. Decide how much weight it carries.

---

## Suggested sequencing

1. **Close the "run it" gap** `[GAP #1]` — execute the bound contract tests and
   report satisfied/failed/stale. Turns static coverage into real evidence and
   directly satisfies the mentor's "run in CI." (Language-scoped: start with Go.)
2. **Decide the mapping convention** `[OPEN #2/#3]` — inference vs. registry — and,
   if convention, the enumeration cross-check. This unblocks precise coverage.
3. **Add the new-invariant *suggestion* layer** `[GAP #2]` — a small
   box/edge-kind → invariant-template catalog that *flags* an owed invariant
   (cache→consistency, queue→delivery). Suggestion, not enforcement, first.
4. **Author interface-level contract tests** in a subject repo for the end-to-end
   demo/eval `[OPEN #5]`.
5. Keep the **touched-test flag** and **coverage gap** wired into the two-doors /
   escalation story `[DONE]`.

**Net:** the static mapping + coverage + touched-flag is done and is already the
novel "tied-to-evidence" differentiator. The two things that would make it land:
actually **running** the tests (real evidence), and deciding the **mapping
convention**. The "invariants literally evolve" catalog is the exciting frontier —
worth doing as a *suggestion* layer, but scope it carefully; it's the least
grounded part.
