# ARCHON

ARCHON reads a Go codebase and shows you its architecture: which packages depend
on which, which interfaces are implemented, what a pull request changed at the
boundary level, and whether the contract tests actually cover those boundaries.
It is fully deterministic — same input, same output, byte for byte — and uses no
LLM. It treats software architecture as a checked, versioned contract for code
review and agentic software evolution.

## Quick start

```sh
git clone https://github.com/AI-native-Systems-Research/archon.git
cd archon
go build -o archon-go .

R=/path/to/your/repo
./archon-go health $R                                   # is it healthy?
./archon-go render $R --full --format=dot | dot -Tpng -o arch.png   # draw it
./archon-go delta  $R HEAD~1 HEAD --summary             # what did the last commit change?
./archon-go pr-review $R <base> <head> --out .archon    # CI review bundle (review.md + json)
```

The full walkthrough — every command, with example output — is in
[`USERGUIDE.md`](USERGUIDE.md). For declaring an intended architecture before code
exists, see the [**plan syntax reference**](docs/plan-syntax.md). For a PR review
at three altitudes in one command, see the reviewer wrapper:
[`reviewer/review.py`](reviewer/review.py) and
[`reviewer/RENDERERS.md`](reviewer/RENDERERS.md).

## Repository layout

- **`main.go`** — the `archon-go` CLI (subcommands: `extract`, `delta`, `render`,
  `contract`, `evidence`, `impact`, `health`, `reflexion`, `pr-review`, `plan`).
- **`internal/`** — the analysis libraries (`extract`, `graph`, `delta`,
  `evidence`, `impact`, `health`, `reflexion`, `render`, `plan`, `gate`).
- **`cmd/`** — auxiliary CLI tools (`consumes`, `callgraph`, `eventflow`), each
  built separately, e.g. `go build -o consumes ./cmd/consumes`.
- **`reviewer/`** — deterministic, no-LLM Python views for PR review
  (`review.py` wrapper + the per-view scripts) and a worked example under
  `reviewer/examples/`.
- **`scripts/`** — helper scripts.
- **`fixtures/`** — test fixtures.
- **`results/`** — the evaluation harness and experiment artifacts.
- **`docs/`** — prose: the paper (`docs/paper`), related work
  (`docs/related-work`), design notes (`docs/notes`), and write-ups.

## Framing

ARCHON is intended for both greenfield and brownfield repositories. In greenfield
projects, the intended architecture graph can be written alongside the initial
implementation. In brownfield projects, ARCHON starts by snapshotting the actual
graph, then uses reviewed graph deltas and ratcheting policies to make
architecture visible and incrementally repair it.
