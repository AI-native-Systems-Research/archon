#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ARCHON="${ARCHON:-archon-go}"
BLIS_REPO="${BLIS_REPO:-}"
PASS=0
FAIL=0
SKIPPED=""

red()   { printf "\033[31m%s\033[0m\n" "$1"; }
green() { printf "\033[32m%s\033[0m\n" "$1"; }

# skip records a check that did not run, so the summary cannot say ALL DEMOS PASS
# while the only real-data golden in a flow went uncompared.
skip() {
    echo "  SKIP: $1"
    SKIPPED="$SKIPPED
  - $1"
}

check() {
    local name="$1" actual="$2" expected="$3"
    if diff -q "$actual" "$expected" > /dev/null 2>&1; then
        green "  PASS: $name"
        PASS=$((PASS + 1))
    else
        red "  FAIL: $name"
        diff --unified "$expected" "$actual" | head -20
        FAIL=$((FAIL + 1))
    fi
}

echo "=== Flow 1: PR Review (BLIS #1546) ==="
if [ -z "$BLIS_REPO" ]; then
    skip "Flow 1 (2 checks) — set BLIS_REPO=/path/to/blis"
    echo "  (e.g., BLIS_REPO=/abs/path/to/blis ./demo/run-all.sh)"
    echo "  NOTE: this flow's goldens record one absolute repo path, so they only"
    echo "        reproduce from that checkout; elsewhere expect these 2 to fail."
else
    rm -rf /tmp/demo-flow1
    $ARCHON pr-review "$BLIS_REPO" 70e9ba85 d77764f5 --out /tmp/demo-flow1 2>/dev/null
    check "review.md matches golden" /tmp/demo-flow1/review.md "$SCRIPT_DIR/flow1-pr-review/review.md"
    check "review.json matches golden" /tmp/demo-flow1/review.json "$SCRIPT_DIR/flow1-pr-review/review.json"
fi

echo ""
echo "=== Flow 2: Design Phase (kv-offload plan) ==="
F2="$SCRIPT_DIR/flow2-design-phase"

# Compile
$ARCHON plan compile "$F2/kv-offload.archon" > /tmp/demo-flow2-plan.json
check "plan compile deterministic" /tmp/demo-flow2-plan.json "$F2/kv-offload.plan.json"

# Dist vs empty
$ARCHON plan dist "$F2/kv-offload.plan.json" "$F2/empty-repo.json" > /tmp/demo-flow2-dist-empty.txt
check "dist vs empty repo" /tmp/demo-flow2-dist-empty.txt "$F2/expected-dist-empty.txt"

# Dist vs partial
$ARCHON plan dist "$F2/kv-offload.plan.json" "$F2/partial-repo.json" > /tmp/demo-flow2-dist-partial.txt
check "dist vs partial repo" /tmp/demo-flow2-dist-partial.txt "$F2/expected-dist-partial.txt"

# Delta with plan
$ARCHON delta "$F2/empty-repo.json" "$F2/partial-repo.json" --summary --plan "$F2/kv-offload.plan.json" > /tmp/demo-flow2-delta.txt
check "delta --plan summary" /tmp/demo-flow2-delta.txt "$F2/expected-delta-summary.txt"

# Slice
$ARCHON plan slice "$F2/kv-offload.plan.json" github.com/inference-sim/sim/kv/tierchain > /tmp/demo-flow2-slice.txt
check "plan slice tierchain" /tmp/demo-flow2-slice.txt "$F2/expected-slice.txt"

# Render
$ARCHON plan render "$F2/kv-offload.plan.json" > /tmp/demo-flow2-render.txt
check "plan render mermaid" /tmp/demo-flow2-render.txt "$F2/expected-render.txt"

# Stats
$ARCHON plan compile --stats "$F2/kv-offload.archon" > /dev/null 2> /tmp/demo-flow2-stats.txt
check "plan compile --stats" /tmp/demo-flow2-stats.txt "$F2/expected-stats.txt"

echo ""
echo "=== Flow 3: BLIS Design-Phase Tracking (real PRs) ==="
F3="$SCRIPT_DIR/flow3-blis-design"

# The plan is a file, so these two need no repo. Outside the guard on purpose:
# flow 3's source declares 19 clauses against flow 2's 5, and behind the guard it
# would never be compiled in CI, which does not set BLIS_REPO.
$ARCHON plan compile "$F3/kv-offload.archon" > /tmp/demo-flow3-plan.json
check "flow3 plan compile" /tmp/demo-flow3-plan.json "$F3/kv-offload.plan.json"

$ARCHON plan compile --stats "$F3/kv-offload.archon" > /dev/null 2> /tmp/demo-flow3-stats.txt
check "flow3 plan compile --stats (19)" /tmp/demo-flow3-stats.txt "$F3/expected-stats.txt"

if [ -z "$BLIS_REPO" ]; then
    skip "the rest of Flow 3 (8 checks) — needs BLIS_REPO=/path/to/blis"
else
    # Health. Checkable since #59 made the row order total; before that, rows
    # with an equal blast radius came out in a different order every run.
    $ARCHON health "$BLIS_REPO" 52161669 > /tmp/demo-flow3-health.txt 2>&1
    check "flow3 health" /tmp/demo-flow3-health.txt "$F3/expected-health.txt"

    # Impact
    $ARCHON impact "$BLIS_REPO" github.com/inference-sim/inference-sim/sim/kv 52161669 > /tmp/demo-flow3-impact.txt 2>&1
    check "flow3 impact on sim/kv" /tmp/demo-flow3-impact.txt "$F3/expected-impact.txt"

    # Dist at base
    $ARCHON plan dist "$F3/kv-offload.plan.json" "$BLIS_REPO" 52161669 > /tmp/demo-flow3-dist-base.txt 2>&1
    check "flow3 dist at base (13)" /tmp/demo-flow3-dist-base.txt "$F3/expected-dist-base.txt"

    # PR #1593
    $ARCHON delta "$BLIS_REPO" 52161669 2fc4fa53 --summary --plan "$F3/kv-offload.plan.json" > /tmp/demo-flow3-pr1593.txt 2>&1
    check "flow3 PR#1593 (13→13)" /tmp/demo-flow3-pr1593.txt "$F3/expected-pr1593.txt"

    # PR #1594
    $ARCHON delta "$BLIS_REPO" 2fc4fa53 3673d365 --summary --plan "$F3/kv-offload.plan.json" > /tmp/demo-flow3-pr1594.txt 2>&1
    check "flow3 PR#1594 (13→13)" /tmp/demo-flow3-pr1594.txt "$F3/expected-pr1594.txt"

    # PR #1592
    $ARCHON delta "$BLIS_REPO" 3673d365 82b64188 --summary --plan "$F3/kv-offload.plan.json" > /tmp/demo-flow3-pr1592.txt 2>&1
    check "flow3 PR#1592 (13→13)" /tmp/demo-flow3-pr1592.txt "$F3/expected-pr1592.txt"

    # PR #1595
    $ARCHON delta "$BLIS_REPO" 82b64188 eaba67fe --summary --plan "$F3/kv-offload.plan.json" > /tmp/demo-flow3-pr1595.txt 2>&1
    check "flow3 PR#1595 (13→13)" /tmp/demo-flow3-pr1595.txt "$F3/expected-pr1595.txt"

    # Final dist
    $ARCHON plan dist "$F3/kv-offload.plan.json" "$BLIS_REPO" eaba67fe > /tmp/demo-flow3-dist-final.txt 2>&1
    check "flow3 final dist (13)" /tmp/demo-flow3-dist-final.txt "$F3/expected-dist-final.txt"
fi

echo ""
echo "=== Flow 4: Declared Invariants ==="
F4="$SCRIPT_DIR/flow4-invariants"

# The fixture checks need no BLIS checkout, so unlike flow 1 and most of flow 3
# these two run in CI. They are what pin the --json shape.
$ARCHON invariants "$F4/fixture" invariants.md > /tmp/demo-flow4-fixture.txt
check "flow4 fixture table" /tmp/demo-flow4-fixture.txt "$F4/expected-fixture.txt"

$ARCHON invariants "$F4/fixture" invariants.md --json > /tmp/demo-flow4-fixture.json
check "flow4 fixture --json" /tmp/demo-flow4-fixture.json "$F4/expected-fixture.json"

if [ -z "$BLIS_REPO" ]; then
    skip "flow4 BLIS golden — needs BLIS_REPO=/path/to/blis"
else
    # Pinned with --at, so the registry and the code are both read at one commit
    # and the numbers cannot drift as BLIS moves.
    $ARCHON invariants "$BLIS_REPO" docs/contributing/standards/invariants.md \
        --at 73a17c00f84f28623e254a625f1f5298bb8c8a38 > /tmp/demo-flow4-blis.txt
    check "flow4 BLIS at pinned commit 73a17c00" /tmp/demo-flow4-blis.txt "$F4/expected-blis.txt"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ -n "$SKIPPED" ]; then
    # Named, not just counted: a skipped golden is an unverified claim, and a
    # banner over a silent skip is how one goes unnoticed. Printed before the
    # failure exit too, so a red run still says what it did not check.
    printf "NOT VERIFIED (skipped):%s\n" "$SKIPPED"
fi
if [ $FAIL -gt 0 ]; then
    red "DEMO FAILED — output differs from golden files."
    exit 1
fi
green "ALL CHECKS THAT RAN PASS"
