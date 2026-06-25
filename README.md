# RepoEvolve

This repository contains an early FSE paper draft for **ARCHON**, a substrate for
treating software architecture as a checked, versioned contract for code review
and agentic software evolution.

The paper is written in LaTeX using ACM's `acmart` class. The main source is
`main.tex`; figures are maintained as separate TikZ inputs.

## Building

Build the PDF with:

```sh
latexmk -pdf -interaction=nonstopmode -halt-on-error main.tex
```

The generated `main.pdf` and LaTeX build artifacts are intentionally ignored by
git. To remove generated files locally:

```sh
latexmk -C main.tex
```

## Source Layout

- `main.tex` - paper source
- `refs.bib` - bibliography
- `fig_aggregation.tex` - graph aggregation figure
- `fig_substrate.tex` - substrate/proposer separation figure
- `fig_doors.tex` - two-door evolution protocol figure

## Current Framing

ARCHON is intended for both greenfield and brownfield repositories. In
greenfield projects, the intended architecture graph can be written alongside
the initial implementation. In brownfield projects, ARCHON starts by snapshotting
the actual graph, then uses reviewed graph deltas and ratcheting policies to make
architecture visible and incrementally repair it.
