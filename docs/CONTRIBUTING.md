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

### 4. Implement + Test

Write the code. Include at least one test that proves the issue is resolved.

You don't need strict TDD, but the bar is: if someone reverts your fix, a test should fail.

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

**Open the PR as a draft, run the review, then mark it ready.** In that order, so that
"ready" means something other than the author has looked at this. Nothing merges without this
step having run — a draft merged directly has skipped it just as surely.

This step is not scoped by size: step 5 above applies to Medium+ feature PRs, this one has no
exceptions. What scales is depth, not whether the gate exists. The floor is mechanical — always
invoke the skill; on a small PR one reviewing agent and one round is enough.

Run the review skill with this prompt:

```
Review this PR against the linked issue. Check:
1. Does it actually resolve the issue's intent?
2. Is the implementation correct — no bugs or missed edge cases?
3. Is there any overengineering or unnecessary scope creep?
```

Reason about each finding first, then fix the valid ones. **List any finding you dismiss in the
PR, with the reason** — otherwise "fix if they are valid" lets an author dismiss everything and
still claim the step ran, and a dismissal nobody can see is not a dismissal.

Verifying your own work is not a substitute: your verification can be wrong in a way that looks
right. On #62 the tests, the demos and a hand-written sweep all passed while the PR shipped a
false claim about golden-file coverage; run late, the review found it in minutes (#63).

State in the PR that the step ran — "step 6: N findings, fixed" — so the gate is auditable.
Silence is indistinguishable from having skipped it.

If a PR ever does merge without this step, that is a broken rule rather than a second route
through it: run the review on the merged commit and fix what it finds in a follow-up, promptly.
Planning to use that path is skipping the step.

## Principles

- **Understand before acting.** A PR that doesn't close the issue is wasted work.
- **Scope is sacred.** Fix the issue, nothing more. No drive-by refactors.
- **Tests prove intent.** Not coverage for coverage's sake — proof that the fix works.
- **No overengineering.** Three lines of straightforward code beats an abstraction.
