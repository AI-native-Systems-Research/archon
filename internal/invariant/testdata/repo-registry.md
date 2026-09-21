# Fixture Invariant Registry

A miniature of BLIS's registry shape: `###` and `####` heading entries plus a
table-family, for `testdata/repo`.

## Index

Index rows are not declarations — the entries below are.

| ID | Group |
|---|---|
| [INV-1](#inv-1-request-conservation) Request conservation | Run-level |
| [INV-2](#inv-2-request-lifecycle) Request lifecycle | Run-level |

## Run-level invariants

### INV-1: Request Conservation

**Statement:** Every request that enters the system is accounted for exactly once.

### INV-2: Request Lifecycle

**Statement:** Requests transition `queued -> running -> completed`, with no invalid transitions.

### INV-13: Run/Replay Parity

**Statement:** A replay of a recorded run reproduces that run's output byte-for-byte.

## Subsystem invariants

### Determinism

#### INV-6: Determinism

**Statement:** Two runs of one configuration produce byte-identical output.

#### INV-42: Named-Only Evidence

**Statement:** Nothing spells this ID out; a test is named for it and nothing more.

### Declared elsewhere

Stated in full in a design plan; listed here as pointer lines so a citation can
be resolved.

| ID | Statement (abbreviated) | Relationship | Code citations |
|---|---|---|---|
| **INV-99** | Capacity bound: at every point in the run, `\|resident items\| <= configured capacity`. | — | 0 |
