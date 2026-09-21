# Flow 4 — Declared Invariants

`archon-go invariants` reads a repository's declared invariant registry and reports
what the repository actually has behind each entry: **LINKED** (cited in non-test
code), **TEST ONLY** (only in tests, or only a test named for it), **UNLINKED**
(nothing anywhere).

Two checks, deliberately different in kind.

## `fixture/` — runs in CI

A four-entry registry and three tiny Go files, small enough that the expected
output is obvious by inspection. It needs no external checkout, so unlike Flow 1
and most of Flow 3 it runs on every PR — and it is what pins the `--json` shape
that the `pr-review` integration will consume.

The fixture's `.go` files carry `//go:build ignore` so the Go tool does not build
them as part of this module. The scanner reads `.go` files as text and is
unaffected.

## BLIS at a pinned commit — needs `BLIS_REPO`

```sh
archon-go invariants "$BLIS_REPO" docs/contributing/standards/invariants.md \
    --at 73a17c00f84f28623e254a625f1f5298bb8c8a38
```

`--at` reads the registry *and* the code at one commit, so the numbers cannot
drift as BLIS moves. The golden records 34 declared invariants, 32 anchored, and
`INV-L6`/`INV-L7` UNLINKED — which is what BLIS's own registry says about itself
("INV-L6 and INV-L7 have no code anchor"). Its citation counts also reproduce the
count column that registry maintains by hand.

Unlike Flow 1, this golden contains no absolute path, so it reproduces from any
checkout of BLIS that has the pinned commit.
