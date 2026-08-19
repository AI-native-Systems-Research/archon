#!/usr/bin/env bash
#
# archon-build.sh — Build archon-go from this repo at the current HEAD or a specified ref.
#
# Usage: scripts/archon-build.sh [ref]
#
# Prints the path to the built binary on stdout.

set -euo pipefail

REF="${1:-}"
BUILD_DIR="${RUNNER_TEMP:-/tmp}/archon-build"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

git -C "$REPO_ROOT" worktree prune 2>/dev/null || true
rm -rf "$BUILD_DIR"

if [[ -n "$REF" ]]; then
  echo "Building archon-go from ref $REF..." >&2
  git -C "$REPO_ROOT" worktree add --detach "$BUILD_DIR" "$REF" >&2
  trap 'git -C "$REPO_ROOT" worktree remove --force "$BUILD_DIR" 2>/dev/null || true' EXIT
  cd "$BUILD_DIR"
else
  echo "Building archon-go from working tree..." >&2
  mkdir -p "$BUILD_DIR"
  cd "$REPO_ROOT"
fi

go build -o "$BUILD_DIR/archon-go" . >&2
echo "Built: $BUILD_DIR/archon-go" >&2

echo "$BUILD_DIR/archon-go"
