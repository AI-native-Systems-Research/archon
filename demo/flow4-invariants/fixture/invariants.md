# Fixture Invariant Registry

A miniature registry for `archon-go invariants`, small enough that the expected
output is obvious by inspection and needs no external checkout.

## Index

Index rows are not declarations; the entries below are.

| ID | Group |
|---|---|
| [INV-1](#inv-1-request-conservation) Request conservation | Run-level |

## Run-level invariants

### INV-1: Request Conservation

**Statement:** Every request that enters the system is accounted for exactly once.

### INV-2: Request Lifecycle

**Statement:** Requests transition `queued -> running -> completed`, with no invalid transitions.

## Subsystem invariants

#### INV-6: Determinism

**Statement:** Two runs of one configuration produce byte-identical output.

### Declared elsewhere

Stated in full in a design plan; a pointer line here so a citation resolves.

| ID | Statement (abbreviated) | Relationship | Code citations |
|---|---|---|---|
| **INV-99** | Capacity bound: at every point in the run, `\|resident items\| <= configured capacity`. | — | 0 |
