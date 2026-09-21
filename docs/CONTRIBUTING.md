# Contributing — Archon PR Workflow

How we deliver PRs that actually close issues, with minimal ceremony.

## The Workflow

### 1. Worktree

Always work in an isolated git worktree. Never commit directly on main.

```bash
git worktree add -b fix/issue-1234 ../fix-issue-1234 main
cd ../fix-issue-1234
```

This keeps main clean and lets you juggle multiple issues in parallel.

### 2. Audit the Issue

Before writing any code, read the linked issue carefully:

- What is the actual problem or request?
- What's ambiguous or underspecified? Ask questions now, not after you've coded.
- What's the acceptance criteria — how will we know this is done?
- **If this is a sub-issue of a tracking issue:** Read the parent tracking issue first. Understand where your piece fits in the larger plan, what other sub-issues depend on yours, and what boundaries you must respect.

If the issue is vague, comment on it to clarify scope before proceeding.

#### Issue Types — What to Focus On

**Bug fix:**
- Reproduce the bug first. Understand the root cause, not just the symptom.
- Your test should reproduce the failure *before* your fix, and pass *after*.
- Scope: fix the bug. Don't refactor surrounding code unless it caused the bug.

**Feature (standalone):**
- Clarify acceptance criteria — what does "done" look like from a user's perspective?
- Consider edge cases and error paths upfront, not after review catches them.
- Scope: deliver the feature as described. Flag scope creep back to the issue.

**Feature sub-issue (part of a tracking issue):**
- Your PR must work independently (merge and pass CI on its own).
- Respect the interfaces/contracts defined by the parent plan or sibling sub-issues.
- Don't solve problems that belong to other sub-issues — note them and move on.
- If your sub-issue reveals a gap in the tracking issue, comment there rather than expanding your PR.

### 3. Micro-Plan — get approval before coding

Write 3-5 bullets covering:

- **What** changes (files, behavior)
- **How** it works (approach, not line-by-line)
- **What tests** prove it works
- **What docs** change — or one line saying why none do
- **What you're NOT changing** (scope boundary)

**Present this to the reviewer (or issue owner) and get explicit approval before writing code.** This avoids wasted work when the approach is wrong. A quick "does this make sense?" saves hours of rework.

This is your contract with the reviewer. If you can't write this clearly, you don't understand the issue yet — go back to step 2.

**Review the plan first when the PR will pin something.** If the plan says it adds or changes an
exported identifier, a flag, a subcommand, an on-disk format, or a file under `demo/*/`, send the
micro-plan to a **fresh** pr-review-toolkit agent before bringing it to a human — one agent, one
round, asked what the plan makes expensive to change later. Same grammar as step 6's table: the
condition is what the plan says it will touch, not whether you expect a sibling PR to care.

Fresh counts twice: not an agent that has already reviewed this work, and not one told what you
think the answer is. Say in the PR that the plan was reviewed, or that it did not need to be.

**These findings are input to the plan, not step 6 findings.** They do not count as rounds, and
none of them can be an open must-fix issue blocking the merge — step 6's accounting starts when
there is code. Human approval remains the gate that ends step 3.

Otherwise skip this. Reviewing a plan document is lower yield than reviewing code — we have tried
it broadly and it mostly surfaced what code review caught anyway. The exception is narrow because
step 6 is too late for exactly one thing: by then the interface is written, and `type-design-analyzer`
reviewing an implemented type cannot cheaply undo a sibling PR already built on it.

### 4. Implement + Test

Write the test first and watch it fail, then write the code. The floor is unchanged: if someone reverts your fix, a test should fail.

**Test-first is the default, not a preference.** A test written first is a claim about what the code must do. A test written afterwards can only describe code that already exists — it cannot fail in the cases you did not think of. Those are precisely the cases review has to find instead, which is the expensive way to find them.

Where the input shape is genuinely unknown — a parser for a format you have to go read first — probe until you know it, then write the test before the implementation. Probe code is throwaway and does not appear in your diff; code you keep is implementation, and implementation comes after a failing test. If you used this carve-out, say so in the PR and name the format you had to read.

**For feature PRs:** Include at least one test that shows concrete input → output when run with `go test -v -run TestXxx`. A reviewer should be able to run that one command and see exactly what goes in and what comes out — no guessing.

Before pushing, confirm everything passes:

```bash
go build ./...    # compiles
go test ./...     # all tests pass
go vet ./...      # no warnings
gofmt -l .        # must print NOTHING (it exits 0 either way, so read the output)

# Run golden-file demos to catch output regressions
ARCHON=./archon-go ./demo/run-all.sh
```

CI runs these same checks on every pull request (`.github/workflows/ci.yml`) and
`main` requires them to pass, so a failure here is a blocked merge rather than a
suggestion. CI runs the demos without `BLIS_REPO`, which exercises Flow 2 plus the two Flow 3
checks that need only the checked-in plan file — if your change touches review
rendering or extraction, run them locally with `BLIS_REPO` set so Flow 1 and the
rest of Flow 3 are covered too.

Flow 1 is the exception, and it is worth knowing before you read its diff as your
own bug: `flow1-pr-review/review.json` and the copy inside `review.md` both record
the repo path as one specific absolute path. Those two checks therefore only
reproduce from that checkout — anywhere else they fail whatever you pass in
`BLIS_REPO`, and that failure is not your change.

If the demo fails, your change altered archon's output — fix the bug. Do NOT update golden files yourself; flag the diff to the human reviewer and justify why the output changed. Only a human may approve golden-file updates.

Commit atomically — each commit should be a coherent unit.

### 5. PR Description Checklist (Medium+ feature PRs)

Your PR description must include:

1. **Context** — what is this about in the big picture? Few sentences linking to the issue/tracking issue.
2. **What the PR delivers** — what changed, concretely (files, behavior, flags).
3. **Input/output proof** — which test to run (e.g., `go test -v -run TestXxx ./internal/pkg/`) and what the output demonstrates.
4. **No-regression proof** — which existing tests still pass, and what guarantees previous behavior is unchanged (e.g., "without `--flag`, output is byte-identical").
5. **Docs** — which doc you updated, or one line saying why none needed.

**When docs are required.** If the change is visible to someone using the tool — a
new flag, subcommand or binary, a change to output, a new mode — then `USERGUIDE.md`
gets the example and `README.md` gets the pointer, **in the same PR**. A doc that
lands later usually does not land. If the change is internal, say "internal only,
no user-visible behavior" and move on; this is not a licence to add a paragraph to
every refactor.

Show real output in the example, copied from a run, not written from memory.

### 6. Review with pr-review-toolkit (/pr-review-toolkit:review-pr) — every PR

**Open the PR as a draft, run the review, then mark it ready.** In that order, so that "ready"
means something other than the author has looked at this. Nothing merges without this step having
run, and a draft merged directly has skipped it just as surely.

Unlike step 5, this step is not scoped by size: what scales is depth, not whether the gate exists.

**The review must come from pr-review-toolkit.** Either invoke `/pr-review-toolkit:review-pr`, or
dispatch its agents directly with the Agent tool. `pr-review-toolkit:code-reviewer` is the default.
If the change matches a row below, **that agent runs too** — the table is an obligation, not a
menu, and the PR must say which rows you judged not to apply. The conditions are observable on the
diff rather than matters of author confidence:

| Agent | Runs when the diff |
|---|---|
| `silent-failure-hunter` | adds or changes error handling, a fallback, a catch block, or any path that can return an empty result |
| `type-design-analyzer` | adds or changes an exported type |
| `pr-test-analyzer` | adds or changes tests |
| `comment-analyzer` | adds or changes prose stating how the tool behaves — doc comments included |

#### Briefing the reviewer

Every PR gets at least one round. "Small" means one agent *plus any row of the table above that
applies*.

The canned prompt below is the floor, not the target. An agent starts with none of your context,
so a thin brief buys a thin review — and a reviewer that only *reads* the guards will report that
they look right:

```
Review this PR against the linked issue. Check:
1. Does it actually resolve the issue's intent?
2. Is the implementation correct — no bugs or missed edge cases?
3. Is there any overengineering or unnecessary scope creep?
```

For anything beyond a small PR, add:

- **What the code is for**, in enough detail that the agent can judge correctness rather than
  style, and **what the worst failure mode would be**.
- **Ground truth it can check numbers against**: a fixture whose expected counts are known, a
  document that states facts about itself, a real checkout it can run against.
- **An instruction to break the guards, not read them** — construct input that produces a
  plausible-but-wrong answer. This is where the findings that matter come from.
- **That probes go in `$(mktemp -d)` and the tree is left clean** — agents have left probe files
  in the worktree that a stray `git add -A` would have committed.
- **Not your conclusions.** Brief it on the problem, never on what you think the answer is,
  otherwise a green verdict only tells you the agent agreed with you.

Reason about each finding first, then fix the valid ones. **List any finding you dismiss in the
PR, with the reason** — otherwise "fix if they are valid" lets an author dismiss everything and
still claim the step ran, and a dismissal nobody can see is not a dismissal.

#### When the loop ends

**Fixes get reviewed too.** The loop ends on a round that found no must-fix issues *and reviewed
the code you are actually merging* — not on a round whose fixes you then applied unreviewed. A fix
written under review pressure is exactly where the next bug goes: on #66, both must-fix findings
in round two were round one's *fixes*, each a new guard that rejected valid input.

- **Must-fix means the reviewing agent called it must-fix**, not you. Otherwise the dismissal
  power above is quietly the power to end the loop: dismiss everything as not-must-fix, list
  reasons, report a clean round.
- **A must-fix finding is open** until it is fixed, or dismissed *and* not raised again by a later
  round on the post-fix code, or overruled by a named human in the PR. **A PR with an open
  must-fix finding does not merge, at any round count.** The named human is the escape hatch for a
  finding you believe is simply wrong.
- **Name the SHA the last round reviewed**, taken at dispatch. A clean round on code you then
  changed is not a clean round. Exactly two changes are carved out of that, and nothing else is: an
  update from `main`, if `git diff <reviewed-sha> HEAD -- $(git diff --name-only origin/main...HEAD)`
  is empty; and **that round's own non-blocking suggestions** — list which ones you applied, so the
  carve-out is checkable in the same way a dismissal is. Every round produces suggestions, so
  treating those as invalidating gives the loop no end. Anything else you push — a refactor, a bug
  you spotted yourself, a conflict resolution, work the round never saw — voids the round, whatever
  its severity would have been.
- **Three rounds is a ceiling that signals a design problem**, not a permit to merge on the third.
  If round three still produces must-fix findings, stop, leave the PR in draft, and raise it with a
  human; more rounds will keep finding symptoms.

Tell a later round it is reviewing fixes, name them, and name any finding you dismissed, so it can
rule on the dismissal rather than re-deriving the original list.

Verifying your own work is not a substitute — on #62 the tests, the demos and a hand-written sweep
all passed while the PR shipped a false claim about golden-file coverage (#63).

State in the PR that the step ran, so the gate is auditable rather than asserted — silence is
indistinguishable from having skipped it:

```
step 6: <route, and which agents ran; which table rows you judged not to apply>.
N rounds, M findings, K fixed, M-K dismissed with reasons.
Last round clean at <sha>.        (or: round 3 escalated to <human>, not merged.)
```

If a PR ever does merge without this step, that is a broken rule rather than a second route
through it: run the review on the merged commit and fix what it finds in a follow-up, promptly.
Planning to use that path is skipping the step.

## Principles

- **Understand before acting.** A PR that doesn't close the issue is wasted work.
- **Scope is sacred.** Fix the issue, nothing more. No drive-by refactors.
- **Tests prove intent.** Not coverage for coverage's sake — proof that the fix works. Written before the code, so they can fail.
- **No overengineering.** Three lines of straightforward code beats an abstraction.
- **A PR must be correct, and must claim only what is true.** Merging a bug is worse than merging nothing. Every number, every "unchanged", every checked box comes from a command you actually ran.
