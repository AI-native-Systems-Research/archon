# `archon-go pr-review` — the CI review command

This is the one command CI runs on a pull request. It looks at what the PR
changed **at the architecture level** (packages, boundaries, interfaces,
promises) and writes a small folder of results. CI just posts one of those files
and, if it wants, fails the check on a flag inside another.

Everything is deterministic and uses no LLM: the same repo and the same two
commits always produce the same bytes.

---

## The idea in one line

> CI calls `archon pr-review base head`. Archon decides whether the PR is a
> boring internal change (fast-track) or actually moved the architecture, and
> hands back a ready-to-post review. **No review logic lives in CI** — when
> Archon grows new views, CI gets them for free just by upgrading the binary.

The target behavior, exactly as scoped:

> CI calls `archon pr-review base head`. Archon returns either
> **`✓ No architectural change. Internal-only — fast-track eligible.`**
> or the three views (**component_delta + contract_delta + witness_delta**),
> **all embedded directly in `review.md`** (no separate image files needed).

The verdict is **binary**: `NO_CHANGE` or `ARCHITECTURAL_CHANGE`.

---

## Status — what's done, what's left

**✅ Done (the Archon side — this is complete, tested, and on branch `repo-reorg`):**

- [x] `archon-go pr-review [repo] <base> <head> --out .archon` subcommand.
- [x] Reads both commits through a throwaway git worktree — **never touches the
      working tree**.
- [x] **Report-only: always exits 0.** The verdict is written into the bundle;
      the command never fails the build itself.
- [x] **Binary verdict: `NO_CHANGE` / `ARCHITECTURAL_CHANGE`.**
- [x] The exact fast-track line: `✓ No architectural change. Internal-only —
      fast-track eligible.`
- [x] The three views for architectural PRs: **component**, **witness**, and
      **contract** delta — **all embedded in `review.md` as inline Mermaid**.
- [x] **Self-contained bundle by default: just `review.md` + `review.json`.**
      Every diagram is inline Mermaid, so no separate image files are needed and
      a PR comment needs no external image hosting.
- [x] *(optional)* `--emit-artifacts` also drops the `.mmd`/`.dot`/`.md` sources
      and, if Graphviz (`dot`) is installed, `component.png`/`witness.png`/
      `contract.png` — for anyone who wants high-res pictures.
- [x] Unit tests (both verdicts, determinism, cycle detection) + a byte-identical
      rerun check, all passing.
- [x] Docs: `README.md`, `USERGUIDE.md` (§4), `reviewer/RENDERERS.md` (§5), and
      this file.
- [x] Verified end-to-end on inference-sim PR #1546 (see the example below).

**⬜ Left (not the Archon binary — the wiring around it):**

- [ ] **The GitHub Action YAML — the mentor's side.** Auto-detect base/head,
      run the command, and `cat review.md >> $GITHUB_STEP_SUMMARY`. Optionally
      fail the check on an `ARCHITECTURAL_CHANGE` verdict. See "How CI uses it"
      for the exact steps to hand over.
- [ ] **Land the repo reorg on upstream `main`.** `pr-review` currently lives on
      the `repo-reorg` branch, which also carries the Go-standard layout
      (`internal/`, `cmd/`, `docs/`). If the mentor wants `pr-review` against
      upstream, that reorg PR has to merge first, or the import paths won't line
      up.

---

## What you run

```sh
archon-go pr-review <base> <head> --out .archon
```

- `<base>` / `<head>` — the two commits to compare (merge-base and PR head).
- `--out .archon` — where to write the bundle (default `.archon/`).
- The repo defaults to the current directory (the CI checkout). You can also
  pass it explicitly: `archon-go pr-review <repo> <base> <head>`.

Other flags: `--allow allow.json` (record off-baseline dependencies as
violations against a baseline), `--depth N` (how coarse the component boxes are,
default 2), `--label-a/-b` (friendly names for the two commits),
`--emit-artifacts` (also write the `.mmd`/`.dot`/`.md` sources and PNGs — off by
default because everything is already embedded in `review.md`).

---

## What Archon returns to CI (the bundle)

By default the bundle is **two self-contained files**. `review.md` is the human
interface (post it to the PR / Job Summary); `review.json` is the same result,
machine-readable.

```
.archon/
├── review.md        ← the main thing. Post this to the PR / Job Summary.
│                       All three views are embedded inline as Mermaid.
└── review.json      ← the same result, machine-readable (schema archon.pr-review/v1).
```

With `--emit-artifacts` you *also* get the Graphviz sources and PNGs, for anyone
who wants a high-res picture:

```
.archon/
├── review.md, review.json
├── component.mmd, component.dot, component.png
├── witness.dot, witness.png
└── contract.md, contract.dot, contract.png   (PNGs only if `dot` is installed)
```

> ### ⚠️ Important — why Mermaid, not PNG, is the visual
>
> A PNG that is only a *workflow artifact* **cannot be embedded in a PR comment**
> — GitHub needs an accessible image URL, and an artifact doesn't have one. So
> ARCHON embeds the diagrams as **Mermaid directly inside `review.md`**: GitHub
> renders Mermaid inline in comments and Job Summaries, so the visuals reviewers
> actually see need no image hosting.
>
> That is why the default bundle has **no image files at all** — it doesn't need
> them. The `--emit-artifacts` PNGs are a bonus for offline/high-res use (and
> only render if Graphviz `dot` is on PATH); they are *not* what appears in the
> comment.

---

## The verdict — two outcomes

Archon triages every PR into exactly one of two outcomes: the package boundary
either moved, or it didn't.

| Verdict | What it means | What `review.md` shows |
|---|---|---|
| **`NO_CHANGE`** | No package boundary moved — internal-only. Fast-track eligible. (If a guarded promise or schema field also changed *within* the existing boundary, a one-line note points to `review.json`; the verdict stays `NO_CHANGE`.) | One line: `✓ No architectural change. Internal-only — fast-track eligible.` (+ the optional note). |
| **`ARCHITECTURAL_CHANGE`** | A **package boundary moved** (a dependency, interface, or package was added/removed). | The full picture: the three views (component + witness + contract) as inline Mermaid, plus their detail tables and the invariant/schema/surface tables. |

How it decides:

```
a package boundary moved (EmptyAtPackageAltitude == false)?  → ARCHITECTURAL_CHANGE
otherwise                                                    → NO_CHANGE
```

A touched invariant/schema on its own does **not** flip the verdict — the boundary
is what matters. That detail still rides along in `review.json` regardless, and a
one-line note surfaces it in `review.md`. If you pass `--allow`, an off-baseline
dependency is recorded as a *violation* and shown in a table (it implies an added
edge, so the PR is already `ARCHITECTURAL_CHANGE`); there is no separate blocking
verdict — the command is report-only and CI decides what to do.

---

## The three views (only shown when it matters)

For a boundary-moving PR, `review.md` includes three complementary views. They
all share one color scheme: **green = added, red = removed, blue = modified,
grey = unchanged**.

1. **Component view** — the system as directory-grouped boxes, with the changed
   boxes highlighted (green = a boundary moved here, blue-dashed = only
   surface/schema/tests changed here). Cycles are marked. This is the embedded
   Mermaid diagram.

2. **Witness delta** — for each package connection that changed, *why*. This is
   the view that tells apart a **full** decoupling (the connection and all its
   reasons are gone → `REMOVED`) from a **partial** one (the connection remains
   because some reasons still cross the boundary → `WEAKENED`).

3. **Contract delta** — which interfaces gained or lost an implementer, and
   whether a new implementer is actually covered by a contract test (an
   "evidence gap" if not).

---

## The flow (end to end)

```
        ┌─────────────────────────── CI runner ───────────────────────────┐
        │                                                                   │
 PR ───►│  archon-go pr-review $BASE $HEAD --out .archon                    │
        │        │                                                          │
        │        ▼                                                          │
        │   ┌──────────────┐   extract base + head (throwaway worktrees)    │
        │   │   Archon      │   → compute architectural delta                │
        │   │   (1 binary)  │   → pick verdict → embed views in review.md    │
        │   └──────┬───────┘                                                 │
        │          ▼                                                          │
        │   .archon/  (review.md, review.json)                               │
        │          │                                                          │
        │          ├─► cat review.md >> $GITHUB_STEP_SUMMARY  (Mermaid inline)│
        │          └─► (optional) fail if .verdict == ARCHITECTURAL_CHANGE    │
        └───────────────────────────────────────────────────────────────────┘
```

**All the intelligence is in the binary.** CI is three dumb steps. When Archon
learns a new view or sharpens the verdict, CI changes nothing — it just uses the
newer binary.

---

## How CI uses it (the steps to hand to the mentor)

The Archon side is fixed. The CI job does three things:

```sh
# 1. run it (repo = the checkout, so just base + head)
archon-go pr-review "$BASE" "$HEAD" --out .archon

# 2. post the human-readable review (Mermaid + tables render inline)
cat .archon/review.md >> "$GITHUB_STEP_SUMMARY"

# 3. (optional) require a human review on an architectural change
test "$(jq -r .verdict .archon/review.json)" = NO_CHANGE
```

The bundle is just `review.md` + `review.json`, so there's nothing extra to
upload. (If you *want* the high-res PNGs, add `--emit-artifacts` to step 1 and
`upload-artifact` the `.archon/` folder.)

**That's the whole contract.** Everything else (which events trigger it, how
base/head are detected, whether to post a sticky PR comment vs. a Job Summary) is
ordinary CI plumbing on the mentor's side — none of it needs Archon to change.

---

## A real example — inference-sim PR #1546

That PR set out to decouple `sim/saturation` from `sim` and from `sim/workload`.

```sh
archon-go pr-review $R 70e9ba8 5e28e00b --out .archon --label-a base --label-b '#1546'
# → archon pr-review: ARCHITECTURAL_CHANGE — bundle written to .archon
```

`review.md` embeds the component, witness, and contract diagrams inline as
Mermaid, each followed by its detail table. The witness table shows the two
decouplings landed differently:

| Edge | Kind | Status | Removed | Still coupled via |
|---|---|---|---|---|
| `sim/saturation → sim` | implements | **REMOVED** (full) | `Bank ⊨ BatchClassifier` | — |
| `sim/saturation → sim/workload` | call | **WEAKENED** (partial) | `NewBacklogClassifier` | `DefaultBacklogDriftConfig`, `NewBacklogDriftConfig` |

One seam is **fully** cut; the other is only **partially** cut — two config calls
still cross the boundary even though the interface call was removed. A plain
before/after diff can't show that distinction; the witness view can.

To reproduce it yourself (needs a local checkout of inference-sim):

```sh
R=/path/to/inference-sim
archon-go pr-review $R 70e9ba8 5e28e00b --out /tmp/ar --label-a base --label-b '#1546'
open /tmp/ar/review.md          # or: cat /tmp/ar/review.md
```

An internal-only range (e.g. `428982c 3340de7`) instead prints `NO_CHANGE` and a
short `review.md` — the one-line fast-track note, plus a pointer to the touched
guarded promise in `review.json`. No diagrams.

---

## How it's built (for maintainers)

- **`main.go`** — the `pr-review` subcommand: parse flags, extract both commits
  via ephemeral worktrees, compute the delta, call the review package, exit 0.
- **`internal/review/`** — all the logic, kept out of `main.go` and unit-tested:
  - `review.go` — the result schema (`archon.pr-review/v1`), the binary verdict,
    and `WriteBundle` (self-contained by default; `--emit-artifacts` for sources).
  - `components.go` — directory grouping, edge aggregation, cycle detection
    (Tarjan), delta painting, Mermaid + DOT.
  - `witness.go` — the per-connection witness diff, its inline Mermaid, and DOT.
  - `contract.go` — the interface-contract table, its inline Mermaid, and DOT,
    read straight from the delta (no separate `consumes` step, no checkout).
  - `markdown.go` — assembles `review.md`, embedding all three views as Mermaid.
  - `review_test.go` — covers both verdicts, determinism, and cycles.

This is the pure-Go equivalent of the interactive Python wrapper
`reviewer/review.py --level 3`. The Python wrapper stays as the by-hand
exploration tool; `pr-review` is the single-binary path for CI.
