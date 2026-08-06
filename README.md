# RepoEvolve / ARCHON

This repository contains an FSE paper draft and tooling for **ARCHON**, a substrate for treating software architecture as a checked, versioned contract for code review and agentic software evolution.

## Repository Layout

- **code/** - The ARCHON tool, fixtures, and evaluation harness
- **paper-draft/** - LaTeX paper source (main.tex, refs.bib, figures), ACM template files, and slides (Archon.pptx)
- **rel-work/** - Categorized related-work PDFs organized into thematic subfolders; README.md lists all papers with links and one-line descriptions; synthesis/ contains our analysis write-ups
- **notes/** - Project planning and design documents

## Building the Paper

```sh
cd paper-draft
latexmk -pdf -interaction=nonstopmode -halt-on-error main.tex
```

## Current Framing

ARCHON is intended for both greenfield and brownfield repositories. In greenfield projects, the intended architecture graph can be written alongside the initial implementation. In brownfield projects, ARCHON starts by snapshotting the actual graph, then uses reviewed graph deltas and ratcheting policies to make architecture visible and incrementally repair it.
