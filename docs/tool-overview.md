# ARCHON — code

Implementation workspace for ARCHON, starting **evaluation-first**: build the
minimum needed to answer RQ2 — *do architectural deltas improve review, and do
they shrink the review surface without hiding real architectural changes?*

## What exists now (M0 + M1)

`archon-go/` — a Go tool that extracts a **package-altitude architecture graph**
from a repo, **renders** it as a diagram, and diffs two commits into an
**architectural delta**.

Edges are typed: **`import`** (A can reach B), **`call`** (A references an
exported func/method of internal B), **`implements`** (a concrete type in A
satisfies an interface in B — invisible to imports, and the "architecture as
contract" seam), and **`config`** (A reads an env var or defines a CLI flag; the
key becomes an `env:KEY` / `flag:NAME` node). Each edge carries a witness set so
witness-only changes stay empty.

Boxes also carry **invariants** — the tests that guard them (Test*/Fuzz*),
tracked with a gofmt-normalized hash. The delta reports invariants added /
modified / removed as a separate axis, so a PR that changes or deletes a system
promise is flagged even when the structure is unchanged.

The delta also reports **contract coverage**: an interface is a contract and the
types that implement it are bound to it (read from the `implements` edges). When
a PR adds a new implementer of an interface, ARCHON flags that it must be covered
by that interface's contract test. (Today this flags the *obligation*; verifying
the test actually covers it is the next step.)

Boxes also carry a **structural contract**: their surface plus an `Allow`-list
of the internal packages they may depend on (`main.tex`'s `SC(v)=(surf,Allow)`).
Snapshot the baseline with `contract`, then `delta --allow` flags any dependency
outside it (marking ones a change introduced) — the import-linter / dependency-
cruiser idea, as the paper's structural contract.

Deliberately deferred (documented so we don't pretend they're solved):

- **Bind invariants to the contract they guard** (interface-level, via the
  surface a test exercises) so every implementer of an interface must be covered
  by that interface's test. v0 binds a test to its declaring package.
- **More operational edges** (protocol, permissions, traces) — `config` (env /
  CLI flags) is done; protocol/permission/trace edges are the next additions.
- **Node identity / GumTree** — at the package altitude, identity is the import
  path, so refactor noise is already small. Leaf-level identity comes later.
- **Contracts, property testing, the two doors, the theorem** — RQ4 / agent
  safety, layered on only after RQ2 holds.

### Build

```sh
git clone https://github.com/AI-native-Systems-Research/archon.git && cd archon
go build -o archon-go .
```

### Use

```sh
# Extract a graph (JSON to stdout) from a working tree or a specific commit
./archon-go extract <repo-dir> [commit]

# Diff two commits of a repo
./archon-go delta <repo-dir> <commitA> <commitB>

# Diff two saved graph JSON files
./archon-go delta a.json b.json

# Machine-readable delta
./archon-go delta <repo-dir> <commitA> <commitB> --json

# Draw the architecture (DOT or Mermaid; add --external for third-party boxes)
./archon-go render <repo-dir> [commit] --format=dot     | dot -Tpng -o arch.png
./archon-go render <repo-dir> [commit] --format=mermaid > arch.mmd

# Draw the DELTA between two commits: added=green, removed=red, unchanged=grey
./archon-go render <repo-dir> <commitA> <commitB> --format=dot | dot -Tpng -o delta.png
./archon-go render <graphA.json> <graphB.json> --format=dot    | dot -Tpng -o delta.png

# Structural contract: snapshot the allow-list baseline, then flag stray deps
./archon-go contract <repo-dir> [commit] > allow.json
./archon-go delta <repo-dir> <commitA> <commitB> --allow allow.json
```

### What the delta reports

- `+ / - box` — an internal package appeared or disappeared
- `+ / - arrow A -> B [import]` — a package dependency appeared or disappeared,
  with the count of **witness files** that realize it
- `+ / - surface P.X` — a package's public surface widened or narrowed
- **empty at package altitude** — nothing above changed: the change is internal
  and needs no architecture review

The key property: witness-only changes (same edge, different files) do **not**
register, so a pure file move or a function-body rewrite diffs to *empty*.

## Verified behaviors (`fixtures/sample`)

A small 3-package module (`api`, `auth`, `db`) with a scripted git history:

| Commit step | Change | Expected delta | Result |
|---|---|---|---|
| c1 → c2 | rename `routes.go` → `handlers.go` (pure move) | **empty** | ✅ |
| c2 → c3 | `api` imports `db` | `+ arrow api -> db` | ✅ |
| c3 → c4 | `auth` gains exported `HashToken` | `+ surface auth.HashToken` | ✅ |
| HEAD → HEAD | same commit (reproducibility) | **empty** | ✅ |

## Next: the RQ2 measurement harness (M2 / M3)

1. **M2 — Number A (compression), no labeling.** Run `delta` over a sample of
   merged BLIS PRs (merge-base vs. head); report delta size vs. textual diff
   size and the empty-delta fraction.
2. **M3 — Number B (false negatives), with labeling.** On a stratified sample,
   have two people label each PR routine / boundary-changing /
   architecture-significant, and measure how often an "empty" delta hides a PR
   humans considered architectural. This is the number that tests the safety
   claim.

Subject order: **BLIS** (clean, single-language Go) first; vLLM and Tauri later
as multi-language / operational-edge stress tests.
