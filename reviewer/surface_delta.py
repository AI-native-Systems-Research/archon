#!/usr/bin/env python3
"""View 1 -- PR Summary / Surface Delta.

This is the top reviewer altitude: "what changed architecturally?" It reads the
JSON that `archon-go delta A B --json` already emits and renders it, so it adds
no new extraction and does not touch the Go binary. It answers the first
question a reviewer asks before diving into interfaces or call paths:

  * which packages appeared / disappeared,
  * which public (exported) symbols were added / removed -- types, funcs,
    methods, consts, vars,
  * which struct schemas changed,
  * which high-level package edges (import / implements) moved,
  * which invariants (tests) were added / removed / modified.

Everything is painted in Srinivasan's four colors, shared with the contract
views so the three altitudes never disagree:

  green = added      red = removed      blue = modified      grey = unchanged

The one judgement call this view makes is the headline verdict, and it is a
mechanical one, not an opinion:

  * surface ADDED only, invariants ADDED only, no package/edge/schema removed
      -> "surface widened (additive)"  -- the safe shape for a feature PR.
  * anything REMOVED or MODIFIED at the surface / schema / package / edge level,
    or an invariant removed/modified
      -> "surface changed (removal/modification present)" -- a human must look.

That mirrors `archon-go delta`'s own "needs architecture review" logic but states
*why* in reviewer terms. All lists are sorted, so the same delta JSON yields
byte-identical output.

Usage:
  # produce the delta JSON once:
  ./archon-go delta <repo> <commitA> <commitB> --json > delta.json

  # text PR summary (good for a PR comment):
  python3 reviewer/surface_delta.py delta.json --label-a base --label-b "#1538"

  # figure:
  python3 reviewer/surface_delta.py delta.json --label-a base --label-b "#1538" \
      --format dot | dot -Tpng -o surface.png

Flags:
  --label-a / --label-b   names for the two commits (default: A / B)
  --format text|dot        default text
  --title "..."            title on the DOT figure
  --no-invariants          omit the invariant (test) block from the figure
"""
import argparse
import json
import sys

# Srinivasan four-color scheme -- identical to contract_delta.py so the
# altitudes never disagree on what "added"/"removed"/"modified" look like.
C_ADD = "#1a7f37"       # added, green
C_REMOVE = "#cf222e"    # removed, red
C_MOD = "#0969da"       # modified, blue
C_SAME = "#57606a"      # unchanged, neutral grey
C_PKG = "#f6f8fa"
C_BORDER = "#b8c4d9"

# how kinds sort inside a package block (types first, tests last) and how they
# are labelled in text
KIND_ORDER = {"type": 0, "func": 1, "method": 2, "const": 3, "var": 4}


def load(path):
    with open(path) as fh:
        return json.load(fh)


def short_pkg(p):
    """Drop host/org/repo from a module path -> a readable package label.

    github.com/inference-sim/inference-sim/sim/latency -> sim/latency
    github.com/inference-sim/inference-sim            -> (root)
    """
    parts = p.split("/")
    if parts and "." in parts[0] and len(parts) >= 3:
        rel = parts[3:]
        return "/".join(rel) if rel else "(root)"
    return p or "(root)"


def sym_key(s):
    """Sort symbols: by kind bucket, then name. Methods sort under their type."""
    return (KIND_ORDER.get(s.get("kind", ""), 9), s.get("name", ""))


# ------------------------------------------------------------ verdict --------
def summarize(d):
    """Mechanical counts + verdict string. No opinion, just the shape."""
    surface = d.get("surface") or []
    schema = d.get("schema") or []
    invs = d.get("invariants") or []

    surf_added = sum(len(s.get("added") or []) for s in surface)
    surf_removed = sum(len(s.get("removed") or []) for s in surface)
    schema_added = sum(len(s.get("added") or []) for s in schema)
    schema_removed = sum(len(s.get("removed") or []) for s in schema)
    inv_added = sum(len(i.get("added") or []) for i in invs)
    inv_removed = sum(len(i.get("removed") or []) for i in invs)
    inv_modified = sum(len(i.get("modified") or []) for i in invs)

    pkg_added = len(d.get("packagesAdded") or [])
    pkg_removed = len(d.get("packagesRemoved") or [])
    edge_added = len(d.get("edgesAdded") or [])
    edge_removed = len(d.get("edgesRemoved") or [])

    touched_pkgs = sorted({short_pkg(s["package"]) for s in surface}
                          | {short_pkg(s["package"]) for s in schema})

    breaking = bool(surf_removed or schema_removed or pkg_removed or edge_removed
                    or inv_removed or inv_modified)
    additive_only = not breaking and bool(
        surf_added or schema_added or pkg_added or edge_added or inv_added)

    if breaking:
        verdict = "surface changed (removal/modification present) -- review needed"
    elif additive_only:
        verdict = "surface widened (additive) -- safe feature shape"
    else:
        verdict = "no surface change"

    return {
        "verdict": verdict, "breaking": breaking,
        "surf_added": surf_added, "surf_removed": surf_removed,
        "schema_added": schema_added, "schema_removed": schema_removed,
        "inv_added": inv_added, "inv_removed": inv_removed,
        "inv_modified": inv_modified,
        "pkg_added": pkg_added, "pkg_removed": pkg_removed,
        "edge_added": edge_added, "edge_removed": edge_removed,
        "touched_pkgs": touched_pkgs,
    }


# --------------------------------------------------------------- text --------
def render_text(d, la, lb):
    s = summarize(d)
    lines = ["# Surface delta (PR summary altitude)",
             f"#   A: {la}   -> B: {lb}",
             ""]
    lines.append("VERDICT: " + s["verdict"])
    bits = [f"+{s['surf_added']}/-{s['surf_removed']} surface symbols",
            f"+{s['inv_added']}/-{s['inv_removed']}/~{s['inv_modified']} invariants"]
    if s["pkg_added"] or s["pkg_removed"]:
        bits.append(f"+{s['pkg_added']}/-{s['pkg_removed']} packages")
    if s["edge_added"] or s["edge_removed"]:
        bits.append(f"+{s['edge_added']}/-{s['edge_removed']} edges")
    if s["schema_added"] or s["schema_removed"]:
        bits.append(f"+{s['schema_added']}/-{s['schema_removed']} schema fields")
    lines.append("         " + "; ".join(bits))
    lines.append(f"         packages touched: {', '.join(s['touched_pkgs']) or '(none)'}")
    lines.append("")

    # packages
    for p in sorted(d.get("packagesAdded") or [], key=lambda x: x.get("path", "")):
        lines.append(f"+ package  {short_pkg(p.get('path',''))}")
    for p in sorted(d.get("packagesRemoved") or [], key=lambda x: x.get("path", "")):
        lines.append(f"- package  {short_pkg(p.get('path',''))}")

    # edges
    for e in sorted(d.get("edgesAdded") or [], key=lambda x: (x.get("from",""), x.get("to",""))):
        lines.append(f"+ edge     {short_pkg(e.get('from',''))} --{e.get('kind','')}--> {short_pkg(e.get('to',''))}")
    for e in sorted(d.get("edgesRemoved") or [], key=lambda x: (x.get("from",""), x.get("to",""))):
        lines.append(f"- edge     {short_pkg(e.get('from',''))} --{e.get('kind','')}--> {short_pkg(e.get('to',''))}")
    if (d.get("packagesAdded") or d.get("packagesRemoved")
            or d.get("edgesAdded") or d.get("edgesRemoved")):
        lines.append("")

    # per-package surface + schema + invariants, keyed by short package name
    inv_by_pkg = {short_pkg(i["package"]): i for i in (d.get("invariants") or [])}
    surf_by_pkg = {}
    for sc in (d.get("surface") or []):
        surf_by_pkg.setdefault(short_pkg(sc["package"]), {}).update(
            {"added": sc.get("added") or [], "removed": sc.get("removed") or []})
    schema_by_pkg = {}
    for sc in (d.get("schema") or []):
        schema_by_pkg.setdefault(short_pkg(sc["package"]), {}).update(
            {"added": sc.get("added") or [], "removed": sc.get("removed") or []})

    for pkg in sorted(set(surf_by_pkg) | set(schema_by_pkg) | set(inv_by_pkg)):
        surf = surf_by_pkg.get(pkg, {})
        sch = schema_by_pkg.get(pkg, {})
        inv = inv_by_pkg.get(pkg, {})
        na = len(surf.get("added") or []) + len(sch.get("added") or [])
        nr = len(surf.get("removed") or []) + len(sch.get("removed") or [])
        ni_a = len(inv.get("added") or [])
        ni_r = len(inv.get("removed") or [])
        ni_m = len(inv.get("modified") or [])
        head = f"{pkg}   +{na}/-{nr} surface"
        if ni_a or ni_r or ni_m:
            head += f", +{ni_a}/-{ni_r}/~{ni_m} invariants"
        lines.append(head)
        for sym in sorted(surf.get("added") or [], key=sym_key):
            lines.append(f"    + {sym.get('kind',''):7} {sym.get('name','')}"
                         + (f"   {sym.get('sig','')}" if sym.get("sig") else ""))
        for sym in sorted(surf.get("removed") or [], key=sym_key):
            lines.append(f"    - {sym.get('kind',''):7} {sym.get('name','')}"
                         + (f"   {sym.get('sig','')}" if sym.get("sig") else ""))
        for sym in sorted(sch.get("added") or [], key=sym_key):
            lines.append(f"    + schema  {sym.get('name','')}"
                         + (f"   {sym.get('sig','')}" if sym.get("sig") else ""))
        for sym in sorted(sch.get("removed") or [], key=sym_key):
            lines.append(f"    - schema  {sym.get('name','')}"
                         + (f"   {sym.get('sig','')}" if sym.get("sig") else ""))
        for t in sorted(inv.get("added") or []):
            lines.append(f"    + test    {t}")
        for t in sorted(inv.get("removed") or []):
            lines.append(f"    - test    {t}")
        for t in sorted(inv.get("modified") or []):
            lines.append(f"    ~ test    {t}")
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


# ---------------------------------------------------------------- dot --------
def esc(s):
    return str(s).replace("\\", "\\\\").replace('"', "'")


def render_dot(d, la, lb, title, show_invariants):
    s = summarize(d)
    out = ["digraph surface_delta {", "  rankdir=LR;", "  compound=true;",
           '  node [fontname="Helvetica", fontsize=10, shape=box, style=filled, fillcolor="white"];',
           '  edge [fontname="Helvetica", fontsize=9];']
    cap = title or f"surface delta   {la} -> {lb}\\n{esc(s['verdict'])}"
    out.append(f'  labelloc="t"; label="{cap}";')

    inv_by_pkg = {short_pkg(i["package"]): i for i in (d.get("invariants") or [])}
    surf_by_pkg = {}
    for sc in (d.get("surface") or []):
        surf_by_pkg.setdefault(short_pkg(sc["package"]), {"added": [], "removed": []})
        surf_by_pkg[short_pkg(sc["package"])]["added"] += sc.get("added") or []
        surf_by_pkg[short_pkg(sc["package"])]["removed"] += sc.get("removed") or []
    schema_by_pkg = {}
    for sc in (d.get("schema") or []):
        schema_by_pkg.setdefault(short_pkg(sc["package"]), {"added": [], "removed": []})
        schema_by_pkg[short_pkg(sc["package"])]["added"] += sc.get("added") or []
        schema_by_pkg[short_pkg(sc["package"])]["removed"] += sc.get("removed") or []

    pkgs = sorted(set(surf_by_pkg) | set(schema_by_pkg)
                  | (set(inv_by_pkg) if show_invariants else set()))
    nid = 0
    for pi, pkg in enumerate(pkgs):
        out.append(f"  subgraph cluster_p{pi} {{")
        out.append(f'    label="{esc(pkg)}"; style=filled; fillcolor="{C_PKG}"; color="{C_BORDER}";')
        surf = surf_by_pkg.get(pkg, {})
        sch = schema_by_pkg.get(pkg, {})
        for sym in sorted(surf.get("added") or [], key=sym_key):
            lbl = f"+ {sym.get('kind','')}  {sym.get('name','')}"
            out.append(f'    n{nid} [label="{esc(lbl)}", color="{C_ADD}", fontcolor="{C_ADD}"];'); nid += 1
        for sym in sorted(surf.get("removed") or [], key=sym_key):
            lbl = f"- {sym.get('kind','')}  {sym.get('name','')}"
            out.append(f'    n{nid} [label="{esc(lbl)}", color="{C_REMOVE}", fontcolor="{C_REMOVE}", style="filled,dashed"];'); nid += 1
        for sym in sorted(sch.get("added") or [], key=sym_key):
            out.append(f'    n{nid} [label="+ schema  {esc(sym.get("name",""))}", color="{C_ADD}", fontcolor="{C_ADD}"];'); nid += 1
        for sym in sorted(sch.get("removed") or [], key=sym_key):
            out.append(f'    n{nid} [label="- schema  {esc(sym.get("name",""))}", color="{C_REMOVE}", fontcolor="{C_REMOVE}", style="filled,dashed"];'); nid += 1
        if show_invariants:
            inv = inv_by_pkg.get(pkg, {})
            for t in sorted(inv.get("added") or []):
                out.append(f'    n{nid} [label="+ test  {esc(t)}", shape=note, color="{C_ADD}", fontcolor="{C_ADD}", fontsize=8];'); nid += 1
            for t in sorted(inv.get("removed") or []):
                out.append(f'    n{nid} [label="- test  {esc(t)}", shape=note, color="{C_REMOVE}", fontcolor="{C_REMOVE}", style="filled,dashed", fontsize=8];'); nid += 1
            for t in sorted(inv.get("modified") or []):
                out.append(f'    n{nid} [label="~ test  {esc(t)}", shape=note, color="{C_MOD}", fontcolor="{C_MOD}", fontsize=8];'); nid += 1
        out.append("  }")

    # (package add/remove and high-level edge changes are listed in the text
    #  view; for feature PRs the per-package surface blocks above carry the
    #  story. The figure stays focused on the symbol-level delta.)

    # legend
    out.append("  subgraph cluster_legend {")
    out.append('    label="delta"; style=filled; fillcolor="white"; color="#d0d7de";')
    out.append(f'    lg_add [label="added", color="{C_ADD}", fontcolor="{C_ADD}"];')
    out.append(f'    lg_rem [label="removed", color="{C_REMOVE}", fontcolor="{C_REMOVE}", style="filled,dashed"];')
    out.append(f'    lg_mod [label="modified", color="{C_MOD}", fontcolor="{C_MOD}"];')
    out.append("    lg_add -> lg_rem -> lg_mod [style=invis];")
    out.append("  }")
    out.append("}")
    return "\n".join(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("delta_json", help="output of `archon-go delta A B --json`")
    ap.add_argument("--label-a", default="A")
    ap.add_argument("--label-b", default="B")
    ap.add_argument("--format", choices=["text", "dot"], default="text")
    ap.add_argument("--title", default=None)
    ap.add_argument("--no-invariants", action="store_true",
                    help="omit the invariant/test block from the figure")
    args = ap.parse_args()

    d = load(args.delta_json)
    if args.format == "dot":
        sys.stdout.write(render_dot(d, args.label_a, args.label_b, args.title,
                                    not args.no_invariants) + "\n")
    else:
        sys.stdout.write(render_text(d, args.label_a, args.label_b))


if __name__ == "__main__":
    main()
