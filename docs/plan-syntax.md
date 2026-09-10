# Plan Syntax Reference

Archon can read a **declared architecture** — what you intend to build — and compare
it against the actual code. You write a `.archon` file describing packages, their
surfaces, and their allowed dependencies. Archon compiles it into a graph and
computes how far the code is from the target.

## Why

Without this, architectural intent lives in issue bodies and design docs that no
tool can check. With it, you can:

- Declare "package X should exist with this API" before writing code
- Measure how far the code is from the plan (`dist`)
- Gate PRs: "did this move us closer or further?"

## Usage

```sh
# Compile a .archon file into graph JSON
archon-go plan compile myplan.archon > myplan.json

# Compile with clause statistics
archon-go plan compile --stats myplan.archon > myplan.json
# stderr: 8 clauses: 3 checked, 4 evidenced, 0 attested:external, 1 attested:design

# Compute distance between plan and actual code
archon-go plan dist myplan.json /path/to/repo

# Compute distance between plan and a specific commit
archon-go plan dist myplan.json /path/to/repo abc123

# Compute distance between plan and another graph JSON
archon-go plan dist myplan.json actual.json

# Extract one hole as a work order (surface, allow, contract)
archon-go plan slice myplan.json github.com/myorg/repo/internal/auth

# Render the plan as a Mermaid diagram (holes dashed, boxes solid)
archon-go plan render myplan.json
```

---

## The four block types

### 1. `hole` — a package that should exist but doesn't yet

A hole declares a public surface and allowed dependencies for a package with no
implementation. It's the core concept: a specification for code that hasn't been
written.

```
hole github.com/myorg/myrepo/internal/auth {
  surface:
    Authenticate(token string) (*User, error)
    Validate(req *Request) bool
  allow:
    import github.com/myorg/myrepo/internal/db
    import github.com/myorg/myrepo/internal/config
  contract:
    BC-A1 Authenticate never panics             [evidenced: fuzz]
    BC-A2 Validate is a pure function           [evidenced: property_test]
  evidence:
    fuzz (BC-A1)
    property_test (BC-A2)
  cites:
    invariant determinism
}
```

**Sections inside a hole:**

| Section | Required | What it declares |
|---------|----------|-----------------|
| `surface:` | Yes | Exported functions/types the package will provide |
| `allow:` | Yes | What this package is permitted to import |
| `contract:` | No | Behavioral promises, reported in PR review (see below) |
| `evidence:` | No | What kind of test proves each contract clause |
| `cites:` | No | References to top-level invariants |

#### Contract clauses in PR review

With `--plan`, `pr-review` lists the clauses declared on every package the change
touched, so a reviewer sees which promises are implicated:

```
### Contract clauses implicated (2, 1 with no bound test)

| Package | Clause | Promise | Declared evidence | Bound test |
|---|---|---|---|---|
| `tierchain` | **BC-C1** | only tier 0 exchanges blocks with GPU | evidenced: differential_test | `TestBC_C1` |
| `tierchain` | **BC-C2** | allocated + free = capacity, always | evidenced: property_test | — none found |
```

A clause line is `<ID> <prose> [<class>: <detail>]`. The class annotation is the
bracket group at the **end of the line**, so a bracket earlier in the prose is
preserved — `queue[0] stays ordered` survives intact. The flip side: prose that
itself *ends* in a bracket group is read as the annotation, so
`BC-C3 ordering holds for queue[0]` yields the promise `ordering holds for queue`
and a class of `0`. Put the annotation last, or reword so the prose does not end
in `]`.

**How a test is matched.** By name only: clause `BC-C2` looks for a test called
`TestBC_C2` (dashes become underscores) in the same package. A test named
`TestCacheConservesCapacity`, or one living in a different package, reports
**— none found** even though the clause may be genuinely tested. Name the test
after the clause if you want the link picked up.

**Nothing is verified.** A bound test is one found by name — archon does not check
that it ran, passed, or asserts anything relevant. An unbound clause is a gap to
close, not a build failure: clauses never affect the verdict, the plan distance,
or the exit code.

### 2. `box` — an existing package the plan depends on

Declares that a package must exist (but it's not a hole — it already has or will
have an implementation). Used for packages the plan references that aren't holes.

```
box github.com/myorg/myrepo/internal/db
box github.com/myorg/myrepo/cmd
```

### 3. `arrow` — a declared dependency

Declares that a dependency edge must exist between two packages. Arrows count
toward plan distance: if the edge is missing from the actual code, `dist` goes up.

```
arrow github.com/myorg/myrepo/cmd -> github.com/myorg/myrepo/internal/auth : import
arrow github.com/myorg/myrepo/internal/auth -> github.com/myorg/myrepo/internal/db : import
```

Format: `arrow <from> -> <to> : <kind>`

Valid kinds: `import`, `call`, `implements`, `config`, `service`, `capability`, `protocol`

### 4. `invariant` — a cross-cutting property declared once

Declares a property that multiple holes share. A hole can cite it instead of
repeating it. If a hole cites an undeclared invariant, the compiler errors.

```
invariant determinism {
  statement: same input yields byte-identical output
  evidence: property_test, replay_trace
}
```

A hole cites it:

```
hole github.com/myorg/myrepo/internal/plan {
  surface:
    Compile(src []byte) (*Graph, error)
  allow:
    import github.com/myorg/myrepo/internal/graph
  cites:
    invariant determinism
}
```

---

## Comments and blank lines

Lines starting with `#` or `//` are comments. Blank lines are ignored.

```
# This is a comment
// This is also a comment

hole github.com/myorg/myrepo/internal/auth {
  # Comments work inside blocks too
  surface:
    Login(email, password string) (*Session, error)
  allow:
    import github.com/myorg/myrepo/internal/db
}
```

---

## Complete example

Here's a full `.archon` file for a web service with an auth layer and a database:

```
# Plan: user authentication subsystem

invariant no-panics {
  statement: no exported function panics on any input
  evidence: fuzz
}

invariant determinism {
  statement: same input yields same output
  evidence: property_test
}

# Existing packages (already implemented)
box github.com/myorg/app/internal/db
box github.com/myorg/app/internal/config
box github.com/myorg/app/cmd

# New packages to build
hole github.com/myorg/app/internal/auth {
  surface:
    Login(email, password string) (*Session, error)
    Logout(sessionID string) error
    Validate(token string) (*Claims, error)
  allow:
    import github.com/myorg/app/internal/db
    import github.com/myorg/app/internal/config
  contract:
    BC-A1 Login returns error on empty credentials  [evidenced: property_test]
    BC-A2 Validate never panics                     [evidenced: fuzz]
    BC-A3 Sessions expire after configured TTL      [evidenced: property_test]
  evidence:
    property_test (BC-A1, BC-A3)
    fuzz (BC-A2)
  cites:
    invariant no-panics
    invariant determinism
}

hole github.com/myorg/app/internal/middleware {
  surface:
    RequireAuth(next http.Handler) http.Handler
  allow:
    import github.com/myorg/app/internal/auth
  contract:
    BC-M1 unauthenticated requests get 401  [evidenced: property_test]
  evidence:
    property_test (BC-M1)
  cites:
    invariant no-panics
}

# Declared dependencies
arrow github.com/myorg/app/cmd -> github.com/myorg/app/internal/auth : import
arrow github.com/myorg/app/cmd -> github.com/myorg/app/internal/middleware : import
arrow github.com/myorg/app/internal/middleware -> github.com/myorg/app/internal/auth : import
```

**Compile it:**

```sh
$ archon-go plan compile auth-plan.archon
{
  "packages": [
    {"path": "github.com/myorg/app/cmd", "name": "cmd", "internal": true},
    {"path": "github.com/myorg/app/internal/auth", "name": "auth", "internal": true, "hole": true,
     "surface": [...], "allow": ["github.com/myorg/app/internal/db", "github.com/myorg/app/internal/config"]},
    ...
  ],
  "edges": [...]
}
```

**Measure distance (before any implementation):**

```sh
$ archon-go plan dist auth-plan.json /path/to/repo
dist(P,G) = 7
  unfilled holes (C1): 2
  absent boxes   (C2): 0
  absent arrows  (C3): 5
  disallowed     (C4): 0

  [C1] hole declared, package absent in actual
  [C1] hole declared, package absent in actual
  [C3] declared arrow github.com/myorg/app/cmd -> github.com/myorg/app/internal/auth (import) absent
  ...
```

**After implementing `auth` (dist decreases):**

```sh
$ archon-go plan dist auth-plan.json /path/to/repo
dist(P,G) = 3
  unfilled holes (C1): 1
  absent boxes   (C2): 0
  absent arrows  (C3): 2
  disallowed     (C4): 0
```

**After implementing everything:**

```sh
$ archon-go plan dist auth-plan.json /path/to/repo
dist(P,G) = 0
```

---

## Plan distance — what `dist` counts

| Class | Meaning | Example |
|-------|---------|---------|
| C1 | Unfilled hole — declared but not implemented | You declared `auth` but haven't written it |
| C2 | Absent box — declared existing package missing | You said `db` should exist but it doesn't |
| C3 | Absent arrow — declared dependency missing | You said `cmd -> auth` but that import doesn't exist |
| C4 | Disallowed arrow — dependency exists outside Allow | `auth` imports `middleware` but Allow only permits `db` and `config` |

`dist = 0` means the code fully realizes the plan.

### Surface drift — reported, never counted

The four classes above compare entity **names**. A declared entity can exist under
the right name with a different signature, which leaves `dist = 0` while the plan
no longer describes what shipped. That is reported separately:

```
$ archon-go plan dist plan.json .
dist(P,G) = 0
  unfilled holes (C1): 0
  ...

surface drift (1) — declared signature is not what shipped:
  example.com/y/pkg Do
    plan: (s string) string
    code: func(parts ...string) string
```

`pr-review --plan` prints the same as a table under the distance ratchet.

Drift is **not** added to `dist`, so `dist = 0` keeps meaning "structure realized"
and a parameter rename cannot fail a merge gate. Two rules keep the report quiet
enough to be worth reading:

- Signatures are compared after normalizing the `func` prefix and whitespace, so
  the plan's `(s string) string` and extraction's `func(s string) string` agree.
- A signature missing on **either** side means "not recorded", not "different" —
  a hand-written or older graph JSON carries names with no signature at all.

A parameter rename does show as drift. That is intended: the plan and the code
disagree, and updating the plan is the cheap fix.

---

## Plan verdicts

When `--plan` is used with `pr-review`, archon also classifies the PR:

| Verdict | Meaning |
|---------|---------|
| **REALIZES** | PR fills a hole or adds a declared arrow — progress toward the plan |
| **EXCEEDS** | PR adds unplanned structure touching plan-declared packages |
| **CONFLICTS** | PR adds a disallowed arrow or increases dist — moves away from the plan |
| **UNRELATED** | PR touches nothing the plan mentions |

Precedence: CONFLICTS > EXCEEDS > REALIZES > UNRELATED (worst wins).

---

## Error messages

If your `.archon` file has syntax errors, the compiler reports them with line numbers:

```sh
$ archon-go plan compile broken.archon
broken.archon: line 3: malformed arrow: "arrow broken line"
broken.archon: line 7: undeclared invariant "nonexistent" cited by github.com/myorg/app/internal/auth
```

Common errors:
- Missing `{` at end of `hole` or `invariant` declaration
- Arrow format wrong (must be `arrow <from> -> <to> : <kind>`)
- Citing an invariant that wasn't declared with a top-level `invariant` block
- Content outside a section inside a hole (missing `surface:` / `allow:` header)
