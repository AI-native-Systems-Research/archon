#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ARCHON="${ARCHON:-archon-go}"
BLIS_REPO="${BLIS_REPO:-}"
PASS=0
FAIL=0

red()   { printf "\033[31m%s\033[0m\n" "$1"; }
green() { printf "\033[32m%s\033[0m\n" "$1"; }

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
    echo "  SKIP: set BLIS_REPO=/path/to/blis to run Flow 1"
    echo "  (e.g., BLIS_REPO=../main-repo-blis ./demo/run-all.sh)"
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
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ $FAIL -gt 0 ]; then
    red "DEMO FAILED — output differs from golden files."
    exit 1
fi
green "ALL DEMOS PASS"
