#!/usr/bin/env python3
"""Interface-level (contract) DELTA between two `consumes --json` snapshots.

`contract_view.py` shows one commit's contracts. This shows what a pull request
*did* to them: it diffs the interface facts of commit A against commit B and
paints each contract in Srinivasan's four colors:

  green  = added      (interface / implementer / consumer that appears only in B)
  red    = removed    (appears only in A)
  blue   = modified   (present in both, but its implementers/consumers/methods
                       changed)
  grey   = unchanged  (present in both, identical)

On top of the raw diff it calls out the one event a reviewer most needs to see:
a contract whose *smell state* changed. The BLIS case is exactly this: across
#1521 -> #1524, `sim.BatchClassifier` goes from healthy (0 implementers) to
STRANDED (a single implementer `saturation.Bank` in another package, consumed
only inside `sim`). Two separate snapshots hide that this is one flip; the delta
makes it a single verdict.

The smell rule and colors are shared with `contract_view.py`, so the delta and
the snapshot never disagree. Everything is derived from `consumes --json`, no
LLM, and every list is sorted, so the same pair of inputs yields byte-identical
output.

Usage:
  # build the consumes tool once (from the repo root):
  go build -o consumes ./consumes_tool

  # snapshot each commit (checkout, run, restore -- consumes is working-tree based):
  ./consumes <repo> ./... --json > A.json   # e.g. the PR's merge-base
  ./consumes <repo> ./... --json > B.json   # e.g. the PR head

  # text verdict (good for a PR comment):
  python3 reviewer/contract_delta.py A.json B.json \
      --label-a "#1521" --label-b "#1524" --format text

  # focused figure for one contract:
  python3 reviewer/contract_delta.py A.json B.json --interface BatchClassifier \
      --format dot | dot -Tpng -o delta.png

Flags:
  --label-a / --label-b   names for the two commits (default: "A" / "B")
  --interface SUBSTR       only interfaces whose fq name contains SUBSTR
  --all                    show every interface (default: only ones that changed
                           or whose smell state changed)
  --format text|dot        default text
  --title "..."            title on the DOT figure
"""
import argparse
import json
import sys

# reuse the exact smell rule + helpers from the snapshot view so they never drift
from contract_view import split_fq, classify

# Srinivasan four-color delta scheme
C_ADD = "#1a7f37"       # added, green
C_REMOVE = "#cf222e"    # removed, red
C_MOD = "#0969da"       # modified, blue
C_SAME = "#57606a"      # unchanged, neutral grey
C_SMELL_FILL = "#fdeceb"  # interface node fill when STRANDED in B
C_PKG = "#f6f8fa"
C_BORDER = "#b8c4d9"


def load(path):
    with open(path) as fh:
        return json.load(fh)


def index(data):
    """fq interface name -> fact."""
    return {f["interface"]: f for f in data.get("interfaces", [])}


def verdict_of(f):
    """Smell label for one fact, or '(healthy)' / '(absent)'."""
    if f is None:
        return "(absent)"
    v = classify(f)[0]
    return v if v else "(healthy)"


def diff_iface(fa, fb):
    """Return a dict describing what changed for one interface across A -> B."""
    if fa is None:
        status = "ADDED"
    elif fb is None:
        status = "REMOVED"
    else:
        status = "unchanged"

    impl_a = set((fa or {}).get("implementers") or [])
    impl_b = set((fb or {}).get("implementers") or [])
    cons_a = set((fa or {}).get("consumerPkgs") or [])
    cons_b = set((fb or {}).get("consumerPkgs") or [])
    meth_a = set((fa or {}).get("methods") or [])
    meth_b = set((fb or {}).get("methods") or [])

    impl_added, impl_removed = impl_b - impl_a, impl_a - impl_b
    cons_added, cons_removed = cons_b - cons_a, cons_a - cons_b
    meth_changed = meth_a != meth_b

    if status == "unchanged" and (impl_added or impl_removed or cons_added
                                  or cons_removed or meth_changed):
        status = "MODIFIED"

    sa, sb = verdict_of(fa), verdict_of(fb)
    return {
        "fq": (fb or fa)["interface"],
        "decl": (fb or fa)["pkg"],
        "status": status,
        "impl_a": impl_a, "impl_b": impl_b,
        "impl_added": impl_added, "impl_removed": impl_removed,
        "cons_a": cons_a, "cons_b": cons_b,
        "cons_added": cons_added, "cons_removed": cons_removed,
        "meth_changed": meth_changed,
        "smell_a": sa, "smell_b": sb,
        "newly_stranded": sb == "STRANDED" and sa != "STRANDED",
        "unstranded": sa == "STRANDED" and sb != "STRANDED",
        "fa": fa, "fb": fb,
    }


def changed(d):
    """A contract worth showing: structure moved or its smell state flipped."""
    return d["status"] != "unchanged" or d["smell_a"] != d["smell_b"]


def select(a_idx, b_idx, args):
    fqs = sorted(set(a_idx) | set(b_idx))
    out = []
    for fq in fqs:
        if args.interface and args.interface not in fq:
            continue
        d = diff_iface(a_idx.get(fq), b_idx.get(fq))
        if not args.all and not args.interface and not changed(d):
            continue
        out.append(d)
    return out


# ---------------------------------------------------------------- text -------
def render_text(mod, la, lb, diffs):
    lines = ["# Contract delta (interface altitude)",
             f"#   A: {la}   module {mod['a']}",
             f"#   B: {lb}   module {mod['b']}",
             ""]

    # headline: the events a reviewer must not miss
    stranded = [d["fq"] for d in diffs if d["newly_stranded"]]
    healed = [d["fq"] for d in diffs if d["unstranded"]]
    added = [d["fq"] for d in diffs if d["status"] == "ADDED"]
    removed = [d["fq"] for d in diffs if d["status"] == "REMOVED"]
    modified = [d["fq"] for d in diffs if d["status"] == "MODIFIED"]
    verdict = []
    if stranded:
        verdict.append(f"{len(stranded)} contract(s) became STRANDED: "
                       + ", ".join(sorted(stranded)))
    if healed:
        verdict.append(f"{len(healed)} contract(s) stopped being stranded: "
                       + ", ".join(sorted(healed)))
    verdict.append(f"{len(added)} added, {len(removed)} removed, "
                   f"{len(modified)} modified")
    lines.append("VERDICT: " + "; ".join(verdict))
    lines.append("")

    if not diffs:
        lines.append("  (no contract changed between these two commits)")
        return "\n".join(lines)

    for d in diffs:
        na, nb = len(d["impl_a"]), len(d["impl_b"])
        head = f"{d['fq']}   {d['status']}"
        if d["fa"] is not None and d["fb"] is not None:
            head += f"   impl {na} -> {nb}"
        elif d["status"] == "ADDED":
            head += f"   {nb} impl"
        elif d["status"] == "REMOVED":
            head += f"   was {na} impl"
        if d["smell_a"] != d["smell_b"]:
            head += f"   smell: {d['smell_a']} -> {d['smell_b']}"
        lines.append(head)

        for i in sorted(d["impl_added"]):
            lines.append(f"    + impl:  {i}   (in {split_fq(i)[0]})")
        for i in sorted(d["impl_removed"]):
            lines.append(f"    - impl:  {i}   (was in {split_fq(i)[0]})")
        for p in sorted(d["cons_added"]):
            where = "own package" if p == d["decl"] else "cross-package"
            lines.append(f"    + used by: {p}   ({where})")
        for p in sorted(d["cons_removed"]):
            where = "own package" if p == d["decl"] else "cross-package"
            lines.append(f"    - used by: {p}   ({where})")
        if d["meth_changed"]:
            lines.append("    ~ method set changed")

        if d["newly_stranded"]:
            lines.append("    !! this PR turned it into a STRANDED contract:")
            lines.append("       one implementer in another package, consumed only at home.")
            lines.append("       Removing the interface would drop a cross-package dependency.")
        elif d["unstranded"]:
            lines.append("    ok this PR cleared the stranded-contract smell.")
        lines.append("")
    return "\n".join(lines)


# ----------------------------------------------------------------- dot -------
def esc(s):
    return s.replace("\\", "\\\\").replace('"', "'")


def edge_color(in_a, in_b):
    if in_b and not in_a:
        return C_ADD
    if in_a and not in_b:
        return C_REMOVE
    return C_SAME


def iface_border(d):
    return {"ADDED": C_ADD, "REMOVED": C_REMOVE,
            "MODIFIED": C_MOD}.get(d["status"], C_SAME)


def render_dot(mod, la, lb, diffs, title, with_context):
    """Draw the PR delta only: added/removed/modified facts.

    The graph is deliberately raw facts. It carries NO smell verdict -- STRANDED
    and friends are derived warnings that belong in the text, not painted onto
    the graph. By default only edges the PR changed are drawn; --with-context
    adds the unchanged surrounding edges back in (grey), which is really the job
    of the contract-context view.
    """
    # decide which implements / consumes edges to draw
    impl_edges, cons_edges = [], []
    for d in diffs:
        for i in sorted(d["impl_a"] | d["impl_b"]):
            ch = (i in d["impl_a"]) != (i in d["impl_b"])
            if ch or with_context:
                impl_edges.append((i, d["fq"], edge_color(i in d["impl_a"], i in d["impl_b"]), ch))
        for p in sorted(d["cons_a"] | d["cons_b"]):
            ch = (p in d["cons_a"]) != (p in d["cons_b"])
            if ch or with_context:
                cons_edges.append((p, d["fq"], edge_color(p in d["cons_a"], p in d["cons_b"]), ch))

    drawn_types = {e[0] for e in impl_edges}
    cons_pkgs = {e[0] for e in cons_edges}

    # only the packages that actually hold a drawn node / are a consuming source
    pkgs = {d["decl"] for d in diffs}
    pkgs |= {split_fq(i)[0] for i in drawn_types}
    pkgs |= cons_pkgs
    pkgs = sorted(pkgs)
    pid = {p: f"p{i}" for i, p in enumerate(pkgs)}

    iface_id = {d["fq"]: f"i{i}" for i, d in enumerate(sorted(diffs, key=lambda d: d["fq"]))}
    impl_id = {i: f"t{n}" for n, i in enumerate(sorted(drawn_types))}

    out = ["digraph contract_delta {", "  rankdir=LR;", "  compound=true;",
           '  node [fontname="Helvetica", fontsize=10];',
           '  edge [fontname="Helvetica", fontsize=9];']
    cap = title or f"contract delta   {la} -> {lb}"
    out.append(f'  labelloc="t"; label="{esc(cap)}";')

    impls_in = {p: [] for p in pkgs}
    for i in impl_id:
        impls_in[split_fq(i)[0]].append(i)
    ifaces_in = {p: [] for p in pkgs}
    for d in diffs:
        ifaces_in[d["decl"]].append(d)

    for p in pkgs:
        out.append(f"  subgraph cluster_{pid[p]} {{")
        out.append(f'    label="{esc(p)}"; style=filled; fillcolor="{C_PKG}"; color="{C_BORDER}";')
        # only emit a package header node if it is the source of a drawn consumes edge
        if p in cons_pkgs:
            out.append(f'    hdr_{pid[p]} [label="{esc(p)}", shape=folder, fillcolor="white", style=filled];')
        for d in sorted(ifaces_in[p], key=lambda d: d["fq"]):
            nid = iface_id[d["fq"]]
            name = split_fq(d["fq"])[1]
            nb, na = len(d["impl_b"]), len(d["impl_a"])
            count = f"{na}->{nb} impl" if na != nb else f"{nb} impl"
            tag = d["status"].lower() if d["status"] in ("ADDED", "REMOVED", "MODIFIED") else ""
            sub = count + (f" | {tag}" if tag else "")
            border = iface_border(d)
            pw = "2" if changed(d) else "1"
            out.append(f'    {nid} [label="«interface»\\n{esc(name)}\\n{esc(sub)}", '
                       f'shape=ellipse, style=filled, fillcolor="white", '
                       f'color="{border}", penwidth={pw}];')
        for i in sorted(impls_in[p]):
            in_a = any(i in d["impl_a"] for d in diffs)
            in_b = any(i in d["impl_b"] for d in diffs)
            out.append(f'    {impl_id[i]} [label="{esc(split_fq(i)[1])}", shape=box, '
                       f'fillcolor="white", style=filled, color="{edge_color(in_a, in_b)}"];')
        out.append("  }")

    for i, fq, c, _ in impl_edges:
        style = "dashed" if c == C_REMOVE else "solid"
        out.append(f'  {impl_id[i]} -> {iface_id[fq]} '
                   f'[color="{c}", style={style}, label="implements"];')
    for p, fq, c, _ in cons_edges:
        out.append(f'  hdr_{pid[p]} -> {iface_id[fq]} '
                   f'[color="{c}", style=dashed, label="consumes"];')

    # legend: raw delta colours only, no smell
    out.append("  subgraph cluster_legend {")
    out.append('    label="delta"; style=filled; fillcolor="white"; color="#d0d7de";')
    out.append(f'    lg_add [label="added", shape=box, style=filled, fillcolor="white", color="{C_ADD}"];')
    out.append(f'    lg_rem [label="removed", shape=box, style=filled, fillcolor="white", color="{C_REMOVE}"];')
    out.append(f'    lg_mod [label="modified", shape=box, style=filled, fillcolor="white", color="{C_MOD}"];')
    chain = "lg_add -> lg_rem -> lg_mod"
    if with_context:
        out.append(f'    lg_ctx [label="unchanged", shape=box, style=filled, fillcolor="white", color="{C_SAME}"];')
        chain += " -> lg_ctx"
    out.append(f"    {chain} [style=invis];")
    out.append("  }")
    out.append("}")
    return "\n".join(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("a_json", help="consumes --json at commit A (older)")
    ap.add_argument("b_json", help="consumes --json at commit B (newer)")
    ap.add_argument("--label-a", default="A")
    ap.add_argument("--label-b", default="B")
    ap.add_argument("--interface", default=None, help="only interfaces whose fq contains this")
    ap.add_argument("--all", action="store_true", help="show every interface, not just changed ones")
    ap.add_argument("--with-context", action="store_true",
                    help="also draw the unchanged surrounding edges (grey); default is changed-only")
    ap.add_argument("--format", choices=["text", "dot"], default="text")
    ap.add_argument("--title", default=None)
    args = ap.parse_args()

    a, b = load(args.a_json), load(args.b_json)
    diffs = select(index(a), index(b), args)
    mod = {"a": a.get("module", ""), "b": b.get("module", "")}

    if args.format == "dot":
        sys.stdout.write(render_dot(mod, args.label_a, args.label_b, diffs,
                                    args.title, args.with_context) + "\n")
    else:
        sys.stdout.write(render_text(mod, args.label_a, args.label_b, diffs) + "\n")


if __name__ == "__main__":
    main()
