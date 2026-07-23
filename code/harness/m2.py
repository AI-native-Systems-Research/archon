#!/usr/bin/env python3
"""M2 harness: textual-diff size vs. architectural-delta size over merged PRs.

Answers RQ2's "Number A" (compression): for a sample of merged PRs, how much
smaller is ARCHON's package-altitude architectural delta than the textual diff,
and what fraction of PRs are empty at the package altitude (internal changes a
reviewer could fast-path)?

Strategy: BLIS squash-merges each PR to main, so first-parent commits on main
are ~one-per-PR. We extract each commit's graph ONCE (the expensive go/packages
step, cached to graphs/<sha>.json) then diff consecutive graphs cheaply.

Usage:
    python3 m2.py <repo> [N]         # analyze last N merged PRs (default 80)
"""

import csv
import json
import os
import re
import subprocess
import sys
import statistics

HERE = os.path.dirname(os.path.abspath(__file__))
ARCHON = os.path.join(HERE, "..", "archon-go", "archon-go")
GRAPHS = os.path.join(HERE, "graphs")
OUT = os.path.join(HERE, "out")
EXTRACT_TIMEOUT = 120  # seconds per commit; skip if a checkout won't typecheck

STRATA = ["feat", "fix", "refactor", "test", "hardening", "perf", "docs", "chore", "build"]


def sh(args, cwd=None, timeout=None):
    return subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=timeout)


def stratum(subject):
    head = subject.split("(")[0].split(":")[0].strip().lower()
    return head if head in STRATA else "other"


def textual_stats(repo, a, b):
    """(files, insertions, deletions) total, plus go-only (files, ins, del)."""
    files = ins = dele = 0
    r = sh(["git", "diff", "--numstat", a, b], cwd=repo)
    go_files = go_ins = go_del = 0
    for line in r.stdout.splitlines():
        parts = line.split("\t")
        if len(parts) != 3:
            continue
        add, rem, path = parts
        add = int(add) if add.isdigit() else 0
        rem = int(rem) if rem.isdigit() else 0
        files += 1
        ins += add
        dele += rem
        if path.endswith(".go") and "_test.go" not in path:
            go_files += 1
            go_ins += add
            go_del += rem
    return files, ins, dele, go_files, go_ins, go_del


def extract(repo, sha):
    """Extract graph for sha into cache; return (path, n_errors) or (None, -1)."""
    out_path = os.path.join(GRAPHS, sha + ".json")
    err_path = os.path.join(GRAPHS, sha + ".err")
    if os.path.exists(out_path):
        n = 0
        if os.path.exists(err_path):
            with open(err_path) as f:
                n = int(f.read() or 0)
        return out_path, n
    try:
        r = sh([ARCHON, "extract", repo, sha], timeout=EXTRACT_TIMEOUT)
    except subprocess.TimeoutExpired:
        return None, -1
    if r.returncode != 0:
        sys.stderr.write(f"  extract {sha[:8]} failed: {r.stderr.strip()[:120]}\n")
        return None, -1
    m = re.search(r"(\d+) package error", r.stderr)
    n = int(m.group(1)) if m else 0
    with open(out_path, "w") as f:
        f.write(r.stdout)
    with open(err_path, "w") as f:
        f.write(str(n))
    return out_path, n


def delta_counts(graph_a, graph_b):
    r = sh([ARCHON, "delta", graph_a, graph_b, "--json"])
    d = json.loads(r.stdout)
    boxes = len(d.get("packagesAdded", [])) + len(d.get("packagesRemoved", []))
    arrows = len(d.get("edgesAdded", [])) + len(d.get("edgesRemoved", []))
    surf = sum(len(s.get("added", [])) + len(s.get("removed", [])) for s in d.get("surface", []))
    return boxes, arrows, surf, bool(d["emptyAtPackageAltitude"])


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    repo = os.path.abspath(sys.argv[1])
    n = int(sys.argv[2]) if len(sys.argv) > 2 else 80
    os.makedirs(GRAPHS, exist_ok=True)
    os.makedirs(OUT, exist_ok=True)

    # first-parent commits on main, newest first: N PRs => N+1 commits
    r = sh(["git", "rev-list", "--first-parent", "-n", str(n + 1), "HEAD"], cwd=repo)
    commits = r.stdout.split()  # [newest, ..., oldest]
    commits.reverse()           # oldest -> newest
    print(f"analyzing {len(commits)-1} merged PRs on {repo}")

    rows = []
    for i in range(1, len(commits)):
        a, b = commits[i - 1], commits[i]
        subj = sh(["git", "log", "-1", "--format=%s", b], cwd=repo).stdout.strip()
        date = sh(["git", "log", "-1", "--format=%cs", b], cwd=repo).stdout.strip()
        ga, ea = extract(repo, a)
        gb, eb = extract(repo, b)
        if ga is None or gb is None:
            print(f"  [{i:3d}/{len(commits)-1}] {b[:8]} SKIP (extract failed)  {subj[:50]}")
            continue
        boxes, arrows, surf, empty = delta_counts(ga, gb)
        files, ins, dele, gf, gi, gd = textual_stats(repo, a, b)
        rows.append({
            "sha": b[:8], "date": date, "stratum": stratum(subj), "subject": subj,
            "files": files, "insertions": ins, "deletions": dele,
            "go_files": gf, "go_insertions": gi, "go_deletions": gd,
            "text_lines": ins + dele, "go_lines": gi + gd,
            "err_a": ea, "err_b": eb,
            "d_boxes": boxes, "d_arrows": arrows, "d_surface": surf,
            "d_items": boxes + arrows + surf, "empty": int(empty),
        })
        tag = "EMPTY" if empty else f"{boxes+arrows+surf} items"
        print(f"  [{i:3d}/{len(commits)-1}] {b[:8]} text {ins+dele:5d}L -> {tag:9s}  {subj[:46]}")

    csv_path = os.path.join(OUT, "m2.csv")
    with open(csv_path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)

    summarize(rows, csv_path)


def summarize(rows, csv_path):
    total = len(rows)
    empties = [r for r in rows if r["empty"]]
    boundary = [r for r in rows if not r["empty"]]
    print("\n" + "=" * 64)
    print(f"RQ2 / Number A — compression   ({total} merged PRs)")
    print("=" * 64)
    print(f"empty-delta fraction: {len(empties)}/{total} = {len(empties)/total:.0%}")
    if empties:
        el = [r["text_lines"] for r in empties]
        print(f"  empty PRs still changed a median of {int(statistics.median(el))} textual lines "
              f"(max {max(el)}) — internal work a reviewer could fast-path")
    if boundary:
        ratios = [r["text_lines"] / r["d_items"] for r in boundary if r["d_items"] > 0]
        print(f"boundary PRs: median {int(statistics.median([r['text_lines'] for r in boundary]))} textual lines "
              f"-> median {int(statistics.median([r['d_items'] for r in boundary]))} architectural items")
        if ratios:
            print(f"  median compression: {int(statistics.median(ratios))}x fewer items than textual lines")
    errs = [r for r in rows if r["err_a"] > 0 or r["err_b"] > 0]
    print(f"extraction: {total-len(errs)}/{total} PR endpoints clean, {len(errs)} with package errors")
    print("\nby stratum (count | empty%):")
    for s in STRATA + ["other"]:
        g = [r for r in rows if r["stratum"] == s]
        if g:
            e = sum(r["empty"] for r in g)
            print(f"  {s:10s} {len(g):3d}  {e/len(g):3.0%} empty")
    print(f"\nCSV: {csv_path}")


if __name__ == "__main__":
    main()
