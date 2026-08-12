#!/usr/bin/env python3
"""Auto-derive a component-altitude view of a system from an ARCHON graph.

This raises the package-altitude graph one level: it groups packages into
component boxes *by directory* (no hand-written grouping, no declared layer
order, no LLM), aggregates the package edges up to component-to-component edges,
and renders the result as Mermaid (for a PR comment) or DOT (for a figure).

Everything is deterministic: same graph in, byte-identical bytes out. The only
thing highlighted is objective at this altitude, a component-level dependency
CYCLE (drawn red). There is deliberately no "up = violation" coloring: directory
components have no declared order, so calling an edge a violation would smuggle
in intent, which this tool does not take.

Input is the JSON from `archon-go extract <repo> [commit]`.

Usage:
  archon-go extract <repo> [commit] > graph.json

  # render the stable component map
  component_view.py graph.json [--depth N] [--format mermaid|dot]
      [--external] [--members] [--title "..."]
      | dot -Tpng -o out.png        # (only for --format dot)

  # or emit the auto-derived grouping for component_delta.py (the delta figure)
  component_view.py graph.json [--depth N] --emit-components > components.json

`--depth N` (default 2): how many leading path segments define a component.
  depth 1 = top-level dirs (all sim/* collapse into one `sim`, the coarse view).
  depth 2 = `sim`, `sim/cluster`, `sim/kv`, ..., `sim/internal` (the useful view).
"""
import json
import sys

# ---- colors, mirrored from archon-go/render/render.go so the component view
# ---- looks like the package view already posted on PRs.
C_DEP = "#7a7a7a"      # a plain dependency edge (import/call), grey
C_CONTRACT = "#4a6fa5" # an implements-only edge, blue, dotted
C_CYCLE = "#cf222e"    # an edge inside a dependency cycle, red
C_BOX = "#eef3fb"      # component box fill
C_BORDER = "#4a6fa5"   # component box border
C_EXT = "#c9820a"      # external world node (env:/cap:/flag:/service:)


def load(path):
    with open(path) as fh:
        return json.load(fh)


def last_seg(s):
    return s.rsplit("/", 1)[-1] if s else s


def main():
    args = sys.argv[1:]
    if not args:
        sys.exit(__doc__)

    graph_path = None
    depth = 2
    fmt = "mermaid"
    include_external = False
    show_members = False
    emit_components = False
    title = None

    i = 0
    while i < len(args):
        a = args[i]
        if a == "--depth" and i + 1 < len(args):
            depth = max(1, int(args[i + 1])); i += 2
        elif a.startswith("--depth="):
            depth = max(1, int(a.split("=", 1)[1])); i += 1
        elif a == "--format" and i + 1 < len(args):
            fmt = args[i + 1]; i += 2
        elif a.startswith("--format="):
            fmt = a.split("=", 1)[1]; i += 1
        elif a == "--external":
            include_external = True; i += 1
        elif a == "--members":
            show_members = True; i += 1
        elif a == "--emit-components":
            emit_components = True; i += 1
        elif a == "--title" and i + 1 < len(args):
            title = args[i + 1]; i += 2
        elif a.startswith("--title="):
            title = a.split("=", 1)[1]; i += 1
        elif graph_path is None and not a.startswith("--"):
            graph_path = a; i += 1
        else:
            sys.exit(f"unexpected argument: {a}\n\n{__doc__}")

    if graph_path is None:
        sys.exit("need a graph.json (from `archon-go extract`)\n\n" + __doc__)
    if fmt not in ("mermaid", "dot"):
        sys.exit(f"unknown --format {fmt!r} (want mermaid or dot)")

    g = load(graph_path)
    module = g.get("module", "")

    def rel(path):
        # relative package path; "" is the module root (matches component_delta.py)
        if path == module:
            return ""
        if path.startswith(module + "/"):
            return path[len(module) + 1:]
        return path  # external / world node, handled separately

    def comp_key(relpath):
        if relpath == "":
            return "(root)"
        return "/".join(relpath.split("/")[:depth])

    # ---- internal packages -> component ----
    internal = {p["path"] for p in g.get("packages", []) if p.get("internal")}
    comp_of = {}                 # full pkg path -> component key
    members = {}                 # component key -> set of relative pkg paths
    for p in g.get("packages", []):
        if not p.get("internal"):
            continue
        r = rel(p["path"])
        k = comp_key(r)
        comp_of[p["path"]] = k
        members.setdefault(k, set()).add(r)

    # ---- emit-components short-circuit (feeds component_delta.py) ----
    if emit_components:
        comps = [
            {"name": k, "members": sorted(members[k])}
            for k in sorted(members)
        ]
        print(json.dumps({"module": module, "components": comps}, indent=2))
        return

    # ---- aggregate package edges to component edges ----
    DEP = {"import", "call"}
    CONTRACT = {"implements"}
    EXT = {"config", "capability", "service", "protocol"}

    # (cfrom, cto) -> set of underlying kinds  (internal->internal only)
    agg = {}
    # (cfrom, worldnode) -> set of kinds       (internal->external, --external)
    ext = {}
    for e in g.get("edges", []):
        frm, to, kind = e.get("from"), e.get("to"), e.get("kind")
        if frm not in comp_of:
            continue
        cf = comp_of[frm]
        if to in comp_of:
            ct = comp_of[to]
            if cf == ct or kind not in (DEP | CONTRACT):
                continue
            agg.setdefault((cf, ct), set()).add(kind)
        elif include_external and kind in EXT:
            ext.setdefault((cf, to), set()).add(kind)

    # ---- component-level cycles (Tarjan SCC over the aggregated digraph) ----
    nodes = sorted(members)
    adj = {n: [] for n in nodes}
    for (cf, ct) in agg:
        adj[cf].append(ct)
    for n in adj:
        adj[n] = sorted(set(adj[n]))
    scc_of = tarjan(nodes, adj)
    scc_size = {}
    for n, s in scc_of.items():
        scc_size[s] = scc_size.get(s, 0) + 1

    def in_cycle(cf, ct):
        return scc_of[cf] == scc_of[ct] and scc_size[scc_of[cf]] > 1

    # Ordered edge list. A pair carrying both a plain dependency and a contract
    # emits BOTH (solid + dotted), so the implements relation is never masked by
    # a co-occurring import, exactly as the package-altitude view draws it.
    edges_out = []  # (cfrom, cto, "dep" | "contract")
    for (cf, ct) in sorted(agg):
        kinds = agg[(cf, ct)]
        if kinds & DEP:
            edges_out.append((cf, ct, "dep"))
        if kinds & CONTRACT:
            edges_out.append((cf, ct, "contract"))

    if fmt == "mermaid":
        sys.stdout.write(render_mermaid(
            nodes, members, edges_out, ext, in_cycle, show_members, include_external))
    else:
        sys.stdout.write(render_dot(
            module, g, nodes, members, edges_out, ext, in_cycle, show_members,
            include_external, title, depth))


def tarjan(nodes, adj):
    """Return node -> scc-id. Deterministic: nodes and neighbors pre-sorted."""
    index = {}
    low = {}
    onstack = {}
    stack = []
    scc_of = {}
    counter = [0]
    scc_id = [0]

    def strongconnect(v):
        # iterative to avoid recursion-limit surprises on big graphs
        work = [(v, 0)]
        while work:
            node, pi = work[-1]
            if pi == 0:
                index[node] = counter[0]
                low[node] = counter[0]
                counter[0] += 1
                stack.append(node)
                onstack[node] = True
            recursed = False
            neigh = adj[node]
            for j in range(pi, len(neigh)):
                w = neigh[j]
                if w not in index:
                    work[-1] = (node, j + 1)
                    work.append((w, 0))
                    recursed = True
                    break
                elif onstack.get(w):
                    low[node] = min(low[node], index[w])
            if recursed:
                continue
            if low[node] == index[node]:
                sid = scc_id[0]; scc_id[0] += 1
                while True:
                    w = stack.pop()
                    onstack[w] = False
                    scc_of[w] = sid
                    if w == node:
                        break
            work.pop()
            if work:
                parent = work[-1][0]
                low[parent] = min(low[parent], low[node])

    for v in nodes:
        if v not in index:
            strongconnect(v)
    return scc_of


def _box_label(key, members, show_members):
    if not show_members:
        return key
    mems = sorted(last_seg(m) if m else "(root)" for m in members[key])
    return key + "<br/><small>" + " · ".join(mems) + "</small>"


def render_mermaid(nodes, members, edges_out, ext, in_cycle, show_members, include_external):
    nid = {n: f"n{i}" for i, n in enumerate(nodes)}
    out = ["graph LR"]
    for n in nodes:
        out.append(f'  {nid[n]}["{_box_label(n, members, show_members)}"]')

    ext_nodes = sorted({t for (_, t) in ext})
    eid = {t: f"x{i}" for i, t in enumerate(ext_nodes)}
    for t in ext_nodes:
        out.append(f'  {eid[t]}["{t}"]')

    line = 0
    styles = []
    for (cf, ct, etype) in edges_out:
        if in_cycle(cf, ct):
            color, width = C_CYCLE, "3px"
        elif etype == "contract":
            color, width = C_CONTRACT, "2px"
        else:
            color, width = C_DEP, "2px"
        if etype == "contract":
            out.append(f"  {nid[cf]} -. implements .-> {nid[ct]}")
        else:
            out.append(f"  {nid[cf]} --> {nid[ct]}")
        styles.append(f"  linkStyle {line} stroke:{color},stroke-width:{width}")
        line += 1

    if include_external:
        for (cf, t) in sorted(ext):
            out.append(f"  {nid[cf]} -.-> {eid[t]}")
            styles.append(f"  linkStyle {line} stroke:{C_EXT},stroke-width:2px")
            line += 1

    out.extend(styles)
    out.append(f"  classDef comp fill:{C_BOX},stroke:{C_BORDER};")
    out.append(f"  classDef ext fill:#fff4e5,stroke:{C_EXT};")
    if nodes:
        out.append("  class " + ",".join(nid[n] for n in nodes) + " comp")
    if include_external and ext_nodes:
        out.append("  class " + ",".join(eid[t] for t in ext_nodes) + " ext")
    return "\n".join(out) + "\n"


def render_dot(module, g, nodes, members, edges_out, ext, in_cycle, show_members,
               include_external, title, depth):
    nid = {n: "c__" + n.replace("/", "__").replace("(", "").replace(")", "")
           for n in nodes}
    head = title or f"Component view (depth {depth})"
    commit = g.get("commit", "")
    caption = head + (f"\\n{module} @ {commit[:12]}" if commit else f"\\n{module}")

    out = ["digraph component_view {"]
    out.append("  rankdir=LR;")
    out.append('  labelloc="t"; fontname="Helvetica-Bold"; fontsize=16;')
    out.append(f'  label="{caption}";')
    out.append('  node [shape=box, style="rounded,filled", fontname="Helvetica", '
               f'fontsize=12, fillcolor="{C_BOX}", color="{C_BORDER}"];')
    out.append('  edge [fontname="Helvetica", fontsize=10];')
    out.append("  nodesep=0.5; ranksep=0.9;")

    for n in nodes:
        if show_members:
            mems = sorted(last_seg(m) if m else "(root)" for m in members[n])
            lbl = n + "\\n(" + " · ".join(mems) + ")"
        else:
            lbl = n
        out.append(f'  {nid[n]} [label="{lbl}"];')

    if include_external:
        ext_nodes = sorted({t for (_, t) in ext})
        for t in ext_nodes:
            eidt = "x__" + t.replace("/", "__").replace(":", "_")
            out.append(f'  "{eidt}" [shape=note, label="{t}", '
                       f'fillcolor="#fff4e5", color="{C_EXT}"];')

    for (cf, ct, etype) in edges_out:
        if in_cycle(cf, ct):
            color, width, style, ah = C_CYCLE, "3.0", "solid", "normal"
        elif etype == "contract":
            color, width, style, ah = C_CONTRACT, "2.0", "dashed", "onormal"
        else:
            color, width, style, ah = C_DEP, "2.0", "solid", "normal"
        out.append(f'  {nid[cf]} -> {nid[ct]} [color="{color}", penwidth={width}, '
                   f'style={style}, arrowhead={ah}];')

    if include_external:
        for (cf, t) in sorted(ext):
            eidt = "x__" + t.replace("/", "__").replace(":", "_")
            out.append(f'  {nid[cf]} -> "{eidt}" [color="{C_EXT}", style=dotted, '
                       f'penwidth=2.0, arrowhead=vee];')

    out.append("}")
    return "\n".join(out) + "\n"


if __name__ == "__main__":
    main()
