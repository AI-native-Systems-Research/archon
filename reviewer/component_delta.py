#!/usr/bin/env python3
"""Turn an ARCHON delta into a component-altitude delta figure, deterministically.

This is the reviewer-facing view: the system as a handful of labeled subsystem
boxes, with a single PR's architectural change painted on top. Nothing here is a
judgment call. Everything drawn is read straight out of the delta JSON:

  green node    a package this PR changed at a boundary (added/removed edge, or
                a changed contract implementer)
  green-dashed  a package this PR changed only in surface / schema / tests
  red edge      an edge this PR REMOVED (a severed dependency or contract)
  green edge    an edge this PR ADDED to something external (a new boundary:
                third-party module, service, capability, config, or a notable
                stdlib package like os/net/syscall)
  grey          unchanged context

Usage:
  archon-go delta <repo> <before> <after> --json > delta.json
  component_delta.py delta.json components.json ["Title"] [--graph graph.json]
    | dot -Tpng -o out.png

`components.json`: { "module": "<module path>", "components":
  [ { "name": "...", "members": ["<pkg path relative to module>", ...] }, ... ] }
`--graph graph.json` (optional): the after-graph from `archon-go extract`, used
only to draw grey context edges between the changed packages and their
neighbors. The figure is complete without it.
"""
import json
import sys

# stdlib packages that are architecturally notable enough to show as a new
# boundary (filesystem, network, process, low-level). Everything else in the
# stdlib (fmt, bytes, encoding/json, strings, ...) is treated as trivial.
NOTABLE_STDLIB = {
    "os", "os/exec", "net", "net/http", "syscall", "plugin", "unsafe",
    "reflect", "io/ioutil", "os/signal", "runtime",
}


def load(path):
    with open(path) as fh:
        return json.load(fh)


def basename(short):
    if short == "":
        return "(main)"
    return short.rsplit("/", 1)[-1]


def is_external_target(to, module):
    return not (to == module or to.startswith(module + "/"))


def notable_external(kind, to):
    # world-node edges are always meaningful boundaries
    if kind in ("capability", "service", "config", "protocol"):
        return True
    # third-party module import (path has a dotted host segment)
    if "." in to.split("/")[0]:
        return True
    # a small allowlist of stdlib packages worth flagging
    return to in NOTABLE_STDLIB


def main():
    if len(sys.argv) < 3:
        sys.exit("usage: component_delta.py delta.json components.json [title] [--graph graph.json]")
    delta = load(sys.argv[1])
    spec = load(sys.argv[2])
    title = None
    graph = None
    rest = sys.argv[3:]
    i = 0
    while i < len(rest):
        if rest[i] == "--graph" and i + 1 < len(rest):
            graph = load(rest[i + 1])
            i += 2
        else:
            title = rest[i]
            i += 1

    module = spec.get("module", "")
    components = spec["components"]

    def short(path):
        if path == module:
            return ""
        if path.startswith(module + "/"):
            return path[len(module) + 1:]
        return path

    # package short-path -> component index
    comp_of = {}
    for ci, c in enumerate(components):
        for m in c["members"]:
            comp_of[m] = ci

    edges_added = delta.get("edgesAdded") or []
    edges_removed = delta.get("edgesRemoved") or []
    contracts = delta.get("contracts") or []

    def internal(path):
        return path == module or path.startswith(module + "/")

    # ---- classify packages ----
    boundary_changed = set()   # green: moved a dependency/contract edge
    minor_changed = set()      # green-dashed: only surface/schema/tests
    for e in edges_added + edges_removed:
        if internal(e["from"]):
            boundary_changed.add(short(e["from"]))
        if internal(e["to"]):
            boundary_changed.add(short(e["to"]))
    for c in contracts:
        for key in ("implementersRemoved", "implementersAdded", "uncovered"):
            for t in c.get(key) or []:
                pkg = t.rsplit(".", 1)[0]
                if internal(pkg):
                    boundary_changed.add(short(pkg))
    for axis in ("surface", "schema", "invariants"):
        for entry in delta.get(axis) or []:
            pkg = short(entry.get("package", ""))
            if (entry.get("added") or entry.get("removed")) and pkg not in boundary_changed:
                minor_changed.add(pkg)

    # ---- removed edges to draw (internal target) ----
    removed_draw = []   # (fromShort, toShort, kind, label)
    for e in edges_removed:
        if not internal(e["from"]) or not internal(e["to"]):
            continue
        fs, ts = short(e["from"]), short(e["to"])
        label = "REMOVED " + e["kind"]
        if e["kind"] == "implements":
            ifaces = []
            n = 0
            for c in contracts:
                iface = c.get("interface", "")
                if iface.startswith(e["to"] + "."):
                    ifaces.append(iface.rsplit(".", 1)[-1])
                    n += len(c.get("implementersRemoved") or [])
            if ifaces:
                label = "REMOVED: no longer implements\n" + ", ".join(sorted(set(ifaces)))
                if n:
                    label += f"\n({n} detached)"
        removed_draw.append((fs, ts, e["kind"], label))

    # ---- added external boundaries (grouped per source package) ----
    ext_by_src = {}  # fromShort -> list of target labels
    for e in edges_added:
        if internal(e["to"]) or not notable_external(e["kind"], e["to"]):
            continue
        ext_by_src.setdefault(short(e["from"]), []).append(e["to"])

    # ---- context edges (optional, from the after-graph) ----
    context = []  # (fromShort, toShort, kind)
    if graph:
        involved = boundary_changed | minor_changed
        drawn = {(f, t) for f, t, _, _ in removed_draw}
        for e in graph.get("edges", []):
            if e["kind"] not in ("call", "import", "implements"):
                continue
            if not internal(e["from"]) or not internal(e["to"]):
                continue
            fs, ts = short(e["from"]), short(e["to"])
            if fs == ts or (fs, ts) in drawn:
                continue
            if fs in involved or ts in involved:
                context.append((fs, ts, e["kind"]))

    # ---- which internal packages get their own node (vs the summary blob) ----
    exposed = set(boundary_changed) | set(minor_changed)
    for fs, ts, _, _ in removed_draw:
        exposed.add(fs); exposed.add(ts)
    for fs, ts, _ in context:
        exposed.add(fs); exposed.add(ts)
    exposed |= set(ext_by_src.keys())

    emit(delta, components, comp_of, short, basename,
         boundary_changed, minor_changed, removed_draw, ext_by_src, context,
         exposed, title)


def emit(delta, components, comp_of, short, basename,
         boundary_changed, minor_changed, removed_draw, ext_by_src, context,
         exposed, title):
    a, b = delta.get("commitA", "?"), delta.get("commitB", "?")
    n_boundary = len(boundary_changed)
    n_contracts = len(delta.get("contracts") or [])
    n_ext = sum(len(v) for v in ext_by_src.values())
    head = title or "Architectural delta (component altitude)"
    caption = (f"{head}\\n{a} -> {b}   |   "
               f"{n_boundary} boundary change(s), {n_contracts} contract(s) affected, "
               f"{n_ext} new external dep(s)")

    out = []
    out.append("digraph component_delta {")
    out.append("  rankdir=TB;")
    out.append('  labelloc="t"; fontname="Helvetica-Bold"; fontsize=18;')
    out.append(f'  label="{caption}";')
    out.append('  node [shape=box, style="rounded,filled", fontname="Helvetica", fontsize=12, fillcolor="#eef3fb", color="#4a6fa5"];')
    out.append('  edge [fontname="Helvetica", fontsize=10];')
    out.append("  nodesep=0.5; ranksep=0.9;")

    # membership per component
    members_by_comp = {ci: [] for ci in range(len(components))}
    for m, ci in comp_of.items():
        members_by_comp[ci].append(m)

    def node_id(short_path):
        return "n__" + short_path.replace("/", "__") if short_path else "n__main"

    for ci, c in enumerate(components):
        out.append(f"  subgraph cluster_c{ci} {{")
        out.append(f'    label="{c["name"]}"; labelloc="t"; fontname="Helvetica-Bold"; fontsize=13;')
        out.append('    style="rounded,filled"; color="#b0b0c0"; fillcolor="#fafafd"; margin=12;')
        mine = sorted(members_by_comp[ci])
        shown = [m for m in mine if m in exposed]
        hidden = [m for m in mine if m not in exposed]
        for m in shown:
            if m in boundary_changed:
                style = 'fillcolor="#e6f4ea", color="#1a7f37", penwidth=2.4'
                tag = "\\nCHANGED"
            elif m in minor_changed:
                style = 'fillcolor="#eef3fb", color="#1a7f37", style="rounded,filled,dashed", penwidth=1.8'
                tag = "\\n(surface/tests only)"
            else:
                style = 'fillcolor="#eef3fb", color="#4a6fa5"'
                tag = ""
            out.append(f'    {node_id(m)} [label="{basename(m)}{tag}", {style}];')
        if hidden:
            lbl = " \u00b7 ".join(basename(m) for m in hidden)
            out.append(f'    csum_{ci} [label="{lbl}\\n(unchanged)", fillcolor="#f4f4f7", color="#9aa0b0", fontcolor="#777777"];')
        if not shown and not hidden:
            out.append(f'    csum_{ci} [label="(empty)", fillcolor="#f4f4f7", color="#9aa0b0", fontcolor="#777777"];')
        out.append("  }")

    # external note nodes + green added edges
    for src, targets in sorted(ext_by_src.items()):
        eid = "ext__" + src.replace("/", "__")
        lbl = "\\n".join(sorted(set(targets)))
        out.append(f'  {eid} [shape=note, label="{lbl}", fillcolor="#fff4e5", color="#c9820a"];')
        out.append(f'  {node_id(src)} -> {eid} [color="#1a7f37", style=dotted, penwidth=2.0, arrowhead=vee, '
                   f'label="  NEW external\\n  boundary", fontcolor="#1a7f37", fontname="Helvetica-Bold"];')

    # red removed edges
    for fs, ts, kind, label in removed_draw:
        head_arrow = "onormal" if kind == "implements" else "normal"
        out.append(f'  {node_id(fs)} -> {node_id(ts)} [color="#c1121f", style=dashed, penwidth=2.4, '
                   f'arrowhead={head_arrow}, label="  {label}", fontcolor="#c1121f", fontname="Helvetica-Bold"];')

    # grey context edges
    for fs, ts, kind in context:
        head_arrow = "onormal" if kind == "implements" else "normal"
        out.append(f'  {node_id(fs)} -> {node_id(ts)} [color="#cccccc", arrowhead={head_arrow}, '
                   f'label="  {kind}", fontcolor="#aaaaaa"];')

    # legend
    out.append('  subgraph cluster_legend {')
    out.append('    label="Legend"; labelloc="t"; fontname="Helvetica-Bold"; fontsize=12;')
    out.append('    style="rounded,filled"; color="#cccccc"; fillcolor="#fbfbfb"; margin=10;')
    out.append('    Lx [label="changed", fillcolor="#e6f4ea", color="#1a7f37"];')
    out.append('    Lm [label="surface/tests only", fillcolor="#eef3fb", color="#1a7f37", style="rounded,filled,dashed"];')
    out.append('    Lu [label="unchanged", fillcolor="#f4f4f7", color="#9aa0b0", fontcolor="#777777"];')
    out.append('    La [label="A"]; Lb [label="B"];')
    out.append('    La -> Lb [color="#c1121f", style=dashed, penwidth=2, arrowhead=onormal, label="  edge REMOVED"];')
    out.append('    Lc [label="C"]; Ld [shape=note, label="external", fillcolor="#fff4e5", color="#c9820a"];')
    out.append('    Lc -> Ld [color="#1a7f37", style=dotted, penwidth=2, arrowhead=vee, label="  NEW boundary"];')
    out.append('    Lx -> Lu [style=invis]; Lm -> Lb [style=invis];')
    out.append('  }')
    out.append("}")
    sys.stdout.write("\n".join(out) + "\n")


if __name__ == "__main__":
    main()
