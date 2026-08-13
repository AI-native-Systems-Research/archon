#!/usr/bin/env python3
"""View 6 -- Witness delta (why a package edge survived, weakened, or died).

The package/component views answer "does saturation still depend on workload?"
with a yes/no edge. That is too coarse for a reviewer whose PR is *supposed* to
cut a dependency: an edge that is still present looks like "nothing happened",
even when the PR removed most of what the edge carried.

ARCHON's `extract` already records, for every package edge, the concrete symbols
(for `call`/`implements`) or files (for `import`) that *witness* it -- the reasons
the edge exists. This view diffs those witness sets across two extract snapshots
and classifies each edge by what the PR did to its reasons:

  * REMOVED    edge gone entirely            -> full decoupling            (red)
  * WEAKENED   edge kept, some witnesses cut,
               but witnesses remain          -> PARTIAL decoupling         (blue)
  * STRENGTHENED edge kept, witnesses only added -> coupling grew          (green)
  * CHURNED    edge kept, some cut AND some added                          (blue)
  * ADDED      new edge                                                    (green)
  * unchanged  identical witness set                                       (grey)

The headline it exists for: an edge that is WEAKENED is the honest picture of a
"partial decoupling" -- the thing the coarse component graph cannot show. On BLIS
#1546 the `sim/saturation --call--> sim/workload` edge is WEAKENED: the PR removed
the `NewBacklogClassifier` call (the interface coupling) but `DefaultBacklogDrift-
Config` / `NewBacklogDriftConfig` remain (the config coupling). The edge stays; the
reason changed.

Everything is derived from two `extract --json` outputs, no LLM, and every list is
sorted, so the same pair of inputs yields byte-identical output. Colors match the
other reviewer views:

  green = added   red = removed   blue = modified   grey = unchanged

Usage:
  ./archon-go extract <repo> <commitA> --json > A.json
  ./archon-go extract <repo> <commitB> --json > B.json

  # text (good for a PR comment):
  python3 reviewer/witness_delta.py A.json B.json --label-a base --label-b "#1546"

  # figure:
  python3 reviewer/witness_delta.py A.json B.json --label-a base --label-b "#1546" \
      --format dot | dot -Tpng -o witness.png

Flags:
  --label-a / --label-b   names for the two commits (default: A / B)
  --from SUBSTR            only edges whose source package contains SUBSTR
  --to SUBSTR              only edges whose target package contains SUBSTR
  --kind call|import|implements   restrict to one edge kind
  --all                    show every edge, not just the ones that changed
  --format text|dot        default text
  --title "..."            title on the DOT figure
"""
import argparse
import json
import sys

# Srinivasan four-color scheme -- shared with the other reviewer views.
C_ADD = "#1a7f37"       # added, green
C_REMOVE = "#cf222e"    # removed, red
C_MOD = "#0969da"       # modified, blue
C_SAME = "#57606a"      # unchanged, neutral grey
C_PKG = "#f6f8fa"
C_BORDER = "#b8c4d9"


def load(path):
    with open(path) as fh:
        return json.load(fh)


def short_pkg(p):
    """Drop host/org/repo -> readable package label (matches surface_delta.py)."""
    parts = p.split("/")
    if parts and "." in parts[0] and len(parts) >= 3:
        rel = parts[3:]
        return "/".join(rel) if rel else "(root)"
    return p or "(root)"


def edge_map(data):
    """(from, to, kind) -> sorted set of witnesses, internal packages only."""
    internal = {p["path"] for p in data.get("packages", []) if p.get("internal")}
    m = {}
    for e in data.get("edges", []):
        f, t = e.get("from", ""), e.get("to", "")
        if f in internal and t in internal:
            m[(f, t, e.get("kind", ""))] = set(e.get("witnesses") or [])
    return m


def classify(wa, wb, in_a, in_b):
    """Return (status, added_witnesses, removed_witnesses)."""
    if in_a and not in_b:
        return "REMOVED", set(), wa
    if in_b and not in_a:
        return "ADDED", wb, set()
    added, removed = wb - wa, wa - wb
    if not added and not removed:
        return "unchanged", set(), set()
    if removed and not added:
        return "WEAKENED", set(), removed          # partial decoupling
    if added and not removed:
        return "STRENGTHENED", added, set()
    return "CHURNED", added, removed


# how a status paints and sorts
STATUS_COLOR = {"REMOVED": C_REMOVE, "WEAKENED": C_MOD, "CHURNED": C_MOD,
                "STRENGTHENED": C_ADD, "ADDED": C_ADD, "unchanged": C_SAME}
STATUS_ORDER = {"REMOVED": 0, "WEAKENED": 1, "CHURNED": 2, "STRENGTHENED": 3,
                "ADDED": 4, "unchanged": 5}
# what a witness *is*, per edge kind (for honest labelling)
WITNESS_NOUN = {"call": "symbol", "implements": "type", "import": "file"}


def diffs(a, b, args):
    ma, mb = edge_map(a), edge_map(b)
    out = []
    for key in sorted(set(ma) | set(mb)):
        f, t, kind = key
        if args.from_ and args.from_ not in short_pkg(f):
            continue
        if args.to and args.to not in short_pkg(t):
            continue
        if args.kind and kind != args.kind:
            continue
        wa, wb = ma.get(key, set()), mb.get(key, set())
        status, added, removed = classify(wa, wb, key in ma, key in mb)
        if not args.all and status == "unchanged":
            continue
        out.append({"from": short_pkg(f), "to": short_pkg(t), "kind": kind,
                    "status": status, "added": added, "removed": removed,
                    "remaining": wa & wb, "wa": wa, "wb": wb})
    out.sort(key=lambda d: (STATUS_ORDER[d["status"]], d["from"], d["to"], d["kind"]))
    return out


# --------------------------------------------------------------- text --------
def render_text(d, la, lb):
    lines = ["# Witness delta (why each package edge survived / weakened / died)",
             f"#   A: {la}   -> B: {lb}", ""]
    weak = [x for x in d if x["status"] == "WEAKENED"]
    gone = [x for x in d if x["status"] == "REMOVED"]
    grew = [x for x in d if x["status"] == "STRENGTHENED"]
    added = [x for x in d if x["status"] == "ADDED"]
    churn = [x for x in d if x["status"] == "CHURNED"]
    v = []
    if gone:
        v.append(f"{len(gone)} edge(s) fully decoupled")
    if weak:
        v.append(f"{len(weak)} edge(s) PARTIALLY decoupled (weakened)")
    if churn:
        v.append(f"{len(churn)} churned")
    if grew or added:
        v.append(f"{len(grew)} strengthened, {len(added)} new")
    lines.append("VERDICT: " + ("; ".join(v) if v else "no edge changed"))
    lines.append("")
    if not d:
        lines.append("  (no internal package edge changed between these commits)")
        return "\n".join(lines) + "\n"
    for x in d:
        noun = WITNESS_NOUN.get(x["kind"], "witness")
        head = f"{x['from']} --{x['kind']}--> {x['to']}   {x['status']}"
        if x["status"] in ("WEAKENED", "CHURNED", "STRENGTHENED"):
            head += f"   ({len(x['wa'])} -> {len(x['wb'])} {noun}s)"
        lines.append(head)
        for w in sorted(x["removed"]):
            lines.append(f"    - {noun}: {w}")
        for w in sorted(x["added"]):
            lines.append(f"    + {noun}: {w}")
        if x["status"] == "WEAKENED":
            rem = sorted(x["remaining"])
            shown = ", ".join(rem[:4]) + (" ..." if len(rem) > 4 else "")
            lines.append(f"    = still coupled via: {shown}")
            lines.append("    !! PARTIAL decoupling: edge remains; some reasons removed, others persist.")
        elif x["status"] == "REMOVED":
            lines.append("    ok FULL decoupling: edge and all its reasons removed.")
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


# ---------------------------------------------------------------- dot --------
def esc(s):
    # escape backslashes and quotes, then turn real newlines into DOT's \n so a
    # multi-line --title (passed with actual newlines) wraps in the figure.
    return str(s).replace("\\", "\\\\").replace('"', "'").replace("\n", "\\n")


def render_dot(d, la, lb, title):
    pkgs = sorted({x["from"] for x in d} | {x["to"] for x in d})
    pid = {p: f"p{i}" for i, p in enumerate(pkgs)}
    out = ["digraph witness_delta {", "  rankdir=LR;",
           '  node [fontname="Helvetica", fontsize=11, shape=box, style="filled,rounded", fillcolor="white", color="#b8c4d9"];',
           '  edge [fontname="Helvetica", fontsize=9];']
    cap = title or f"witness delta   {la} -> {lb}\n(why each package edge survived / weakened / died)"
    out.append(f'  labelloc="t"; label="{esc(cap)}";')
    for p in pkgs:
        out.append(f'  {pid[p]} [label="{esc(p)}"];')

    for x in d:
        c = STATUS_COLOR[x["status"]]
        noun = WITNESS_NOUN.get(x["kind"], "witness")
        # build a compact edge label: kind + status, then -removed / +added lines
        parts = [f"{x['kind']} · {x['status']}"]
        for w in sorted(x["removed"]):
            parts.append(f"- {w}")
        for w in sorted(x["added"]):
            parts.append(f"+ {w}")
        if x["status"] == "WEAKENED":
            rem = sorted(x["remaining"])
            shown = ", ".join(rem[:3]) + (" ..." if len(rem) > 3 else "")
            parts.append(f"= {shown}")
        label = "\\n".join(esc(p) for p in parts)
        style = "dashed" if x["status"] == "REMOVED" else "solid"
        pw = "2" if x["status"] in ("REMOVED", "WEAKENED") else "1"
        out.append(f'  {pid[x["from"]]} -> {pid[x["to"]]} '
                   f'[label="{label}", color="{c}", fontcolor="{c}", style={style}, penwidth={pw}];')

    # legend
    out.append("  subgraph cluster_legend {")
    out.append('    label="witness delta"; style=filled; fillcolor="white"; color="#d0d7de";')
    out.append(f'    lg_rem [label="REMOVED (full decouple)", color="{C_REMOVE}", fontcolor="{C_REMOVE}"];')
    out.append(f'    lg_weak [label="WEAKENED (partial decouple)", color="{C_MOD}", fontcolor="{C_MOD}"];')
    out.append(f'    lg_add [label="STRENGTHENED / ADDED", color="{C_ADD}", fontcolor="{C_ADD}"];')
    out.append("    lg_rem -> lg_weak -> lg_add [style=invis];")
    out.append("  }")
    out.append("}")
    return "\n".join(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("a_json", help="extract --json at commit A (older)")
    ap.add_argument("b_json", help="extract --json at commit B (newer)")
    ap.add_argument("--label-a", default="A")
    ap.add_argument("--label-b", default="B")
    ap.add_argument("--from", dest="from_", default=None, help="only edges whose source pkg contains this")
    ap.add_argument("--to", default=None, help="only edges whose target pkg contains this")
    ap.add_argument("--kind", choices=["call", "import", "implements"], default=None)
    ap.add_argument("--all", action="store_true", help="show every edge, not just changed ones")
    ap.add_argument("--format", choices=["text", "dot"], default="text")
    ap.add_argument("--title", default=None)
    args = ap.parse_args()

    a, b = load(args.a_json), load(args.b_json)
    d = diffs(a, b, args)
    if args.format == "dot":
        sys.stdout.write(render_dot(d, args.label_a, args.label_b, args.title) + "\n")
    else:
        sys.stdout.write(render_text(d, args.label_a, args.label_b))


if __name__ == "__main__":
    main()
