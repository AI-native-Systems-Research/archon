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

# Run golden-file demos to catch output regressions
ARCHON=./archon-go ./demo/run-all.sh
```

If the demo fails, your change altered archon's output — fix the bug. Do NOT update golden files yourself; flag the diff to the human reviewer and justify why the output changed. Only a human may approve golden-file updates.

Commit atomically — each commit should be a coherent unit.

### 5. PR Description Checklist (Medium+ feature PRs)

Your PR description must include:

1. **Context** — what is this about in the big picture? Few sentences linking to the issue/tracking issue.
2. **What the PR delivers** — what changed, concretely (files, behavior, flags).
3. **Input/output proof** — which test to run (e.g., `go test -v -run TestXxx ./internal/pkg/`) and what the output demonstrates.
4. **No-regression proof** — which existing tests still pass, and what guarantees previous behavior is unchanged (e.g., "without `--flag`, output is byte-identical").

### 6. Review with pr-review-toolkit (/pr-review-toolkit:review-pr)

Before marking the PR ready, run the review skill with this prompt:

```
Review this PR against the linked issue. Check:
1. Does it actually resolve the issue's intent?
2. Is the implementation correct — no bugs or missed edge cases?
3. Is there any overengineering or unnecessary scope creep?
```

Please first reason about findings, Fix if they are valid, then mark ready for human review.

## Principles

- **Understand before acting.** A PR that doesn't close the issue is wasted work.
- **Scope is sacred.** Fix the issue, nothing more. No drive-by refactors.
- **Tests prove intent.** Not coverage for coverage's sake — proof that the fix works.
- **No overengineering.** Three lines of straightforward code beats an abstraction.
