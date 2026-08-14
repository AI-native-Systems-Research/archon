#!/usr/bin/env python3
"""Interface-level (contract) view of a Go system, from `consumes --json`.

The package-altitude graph tells a reviewer that `saturation` depends on `sim`.
It does not tell them *which* contract that dependency is, nor whether the
contract is healthy. This view drops one altitude: it shows each interface as a
node inside its declaring package, the concrete types that implement it as
edges (`saturation.Bank -> sim.BatchClassifier`), and the packages that consume
it as dashed edges. Each interface node carries its implementation count and
method signatures.

On top of that it flags one deterministic design smell:

  STRANDED CONTRACT: an interface with exactly one implementer that lives in a
  *different* package, and which is consumed only inside its own declaring
  package. That is the BLIS `sim.BatchClassifier` / `saturation.Bank` case: an
  interface kept alive by a single cross-package implementer that nobody outside
  its home package actually uses. Removing the interface and inlining the type
  would delete a whole cross-package dependency.

Everything is derived from `consumes --json`; no LLM, no heuristics beyond the
rule above, and every list is sorted, so the same input yields byte-identical
output.

Usage:
  # build the consumes tool once (from the repo root):
  go build -o consumes ./cmd/consumes
  ./consumes <module-dir> ./... --json > consumes.json

  # text verdict (good for a PR comment):
  python3 reviewer/contract_view.py consumes.json --format text

  # focused figure for one interface:
  python3 reviewer/contract_view.py consumes.json --interface BatchClassifier \
      --format dot | dot -Tpng -o contract.png

Flags:
  --interface SUBSTR   only interfaces whose fq name contains SUBSTR
  --all                show every interface (default: only cross-boundary or
                       flagged contracts, to keep the figure legible)
  --format text|dot    default text
  --title "..."        title on the DOT figure
"""
import argparse
import json
import sys

# colors mirrored from archon-go/render/render.go so this matches the package view
C_IMPL = "#4a6fa5"      # implements edge, blue
C_CONSUME = "#7a7a7a"   # consumes edge, grey, dashed
C_SMELL = "#cf222e"     # stranded contract, red
C_WATCH = "#c9820a"     # single cross-package implementer but consumed across boundaries, amber
C_IFACE = "#eef3fb"     # interface node fill
C_PKG = "#f6f8fa"       # package cluster fill
C_BORDER = "#b8c4d9"


def load(path):
    with open(path) as fh:
        return json.load(fh)


def split_fq(fq):
    """module-relative 'sim/saturation.Bank' -> ('sim/saturation', 'Bank')."""
    if "." not in fq:
        return fq, fq
    pkg, name = fq.rsplit(".", 1)
    return pkg, name


def classify(f):
    """Return (label, color) for an interface fact: smell / watch / plain."""
    decl = f["pkg"]
    impls = f.get("implementers") or []
    impl_pkgs = {split_fq(i)[0] for i in impls}
    ext_impl_pkgs = impl_pkgs - {decl}
    ext_consumer_pkgs = set(f.get("consumerPkgs") or []) - {decl}

    single_external = len(impls) == 1 and bool(ext_impl_pkgs)
    if single_external and not ext_consumer_pkgs:
        return "STRANDED", C_SMELL
    if single_external and ext_consumer_pkgs:
        return "single-impl cross-boundary", C_WATCH
    if f.get("candidateUnconsumed"):
        return "unconsumed", C_WATCH
    return "", ""


def interesting(f):
    """Default filter: contracts that cross a package boundary or are flagged."""
    decl = f["pkg"]
    impl_pkgs = {split_fq(i)[0] for i in (f.get("implementers") or [])}
    consumer_pkgs = set(f.get("consumerPkgs") or [])
    crosses = bool((impl_pkgs | consumer_pkgs) - {decl})
    flagged = classify(f)[0] != ""
    return crosses or flagged


def select(facts, args):
    out = []
    for f in facts:
        if args.interface and args.interface not in f["interface"]:
            continue
        if not args.all and not args.interface and not interesting(f):
            continue
        out.append(f)
    return sorted(out, key=lambda f: f["interface"])


# ---------------------------------------------------------------- text -------
def render_text(module, facts):
    lines = [f"# Contract view (interface altitude)   module: {module}", ""]
    if not facts:
        lines.append("  (no matching interfaces)")
        return "\n".join(lines)
    for f in facts:
        decl = f["pkg"]
        impls = f.get("implementers") or []
        verdict, _ = classify(f)
        tag = f"   [{verdict}]" if verdict else ""
        lines.append(f"{f['interface']}  ({len(impls)} impl){tag}")
        for m in f.get("methods") or []:
            lines.append(f"    method: {m}")
        if impls:
            for i in impls:
                ip = split_fq(i)[0]
                where = "same package" if ip == decl else f"in {ip}"
                lines.append(f"    impl:   {i}   ({where})")
        else:
            lines.append("    impl:   (none)")
        cpkgs = f.get("consumerPkgs") or []
        if cpkgs:
            home = [p for p in cpkgs if p == decl]
            away = [p for p in cpkgs if p != decl]
            desc = []
            if away:
                desc.append("cross-package: " + ", ".join(sorted(away)))
            if home:
                desc.append("own package")
            lines.append(f"    used:   {'; '.join(desc)}")
        else:
            lines.append("    used:   (no consumers found)")
        if verdict == "STRANDED":
            lines.append("    WHY:    one implementer in another package, consumed only at home.")
            lines.append("            Removing the interface would drop a cross-package dependency.")
        lines.append("")
    return "\n".join(lines)


# ----------------------------------------------------------------- dot -------
def esc(s):
    return s.replace("\\", "\\\\").replace('"', "'")


def render_dot(module, facts, title):
    # collect every package that participates (declares / implements / consumes)
    pkgs = set()
    for f in facts:
        pkgs.add(f["pkg"])
        for i in f.get("implementers") or []:
            pkgs.add(split_fq(i)[0])
        for p in f.get("consumerPkgs") or []:
            pkgs.add(p)
    pkgs = sorted(pkgs)
    pid = {p: f"p{i}" for i, p in enumerate(pkgs)}

    # stable node ids for interfaces and implementer types
    iface_id, impl_id = {}, {}
    for f in facts:
        iface_id[f["interface"]] = f"i{len(iface_id)}"
    for f in facts:
        for i in f.get("implementers") or []:
            if i not in impl_id:
                impl_id[i] = f"t{len(impl_id)}"

    out = ["digraph contracts {", "  rankdir=LR;", "  compound=true;",
           '  node [fontname="Helvetica", fontsize=10];',
           '  edge [fontname="Helvetica", fontsize=9];']
    if title:
        out.append(f'  labelloc="t"; label="{esc(title)}";')

    # which impl types belong to each package
    impls_in = {p: [] for p in pkgs}
    for i in impl_id:
        impls_in[split_fq(i)[0]].append(i)
    ifaces_in = {p: [] for p in pkgs}
    for f in facts:
        ifaces_in[f["pkg"]].append(f)

    for p in pkgs:
        out.append(f"  subgraph cluster_{pid[p]} {{")
        out.append(f'    label="{esc(p)}"; style=filled; fillcolor="{C_PKG}"; color="{C_BORDER}";')
        # package header node, used as the source of consumes edges
        out.append(f'    hdr_{pid[p]} [label="{esc(p)}", shape=folder, fillcolor="white", style=filled];')
        for f in sorted(ifaces_in[p], key=lambda f: f["interface"]):
            verdict, color = classify(f)
            nid = iface_id[f["interface"]]
            name = split_fq(f["interface"])[1]
            nimpl = len(f.get("implementers") or [])
            sub = f"{nimpl} impl"
            if verdict:
                sub += f" | {verdict}"
            border = color or C_BORDER
            fill = "#fdeceb" if verdict == "STRANDED" else C_IFACE
            pw = "2" if verdict else "1"
            out.append(f'    {nid} [label="«interface»\\n{esc(name)}\\n{esc(sub)}", '
                       f'shape=ellipse, style=filled, fillcolor="{fill}", '
                       f'color="{border}", penwidth={pw}];')
        for i in sorted(impls_in[p]):
            out.append(f'    {impl_id[i]} [label="{esc(split_fq(i)[1])}", shape=box, '
                       f'fillcolor="white", style=filled];')
        out.append("  }")

    # implements edges: implementer type -> interface
    for f in facts:
        for i in sorted(f.get("implementers") or []):
            out.append(f'  {impl_id[i]} -> {iface_id[f["interface"]]} '
                       f'[color="{C_IMPL}", label="implements"];')
    # consumes edges: consuming package header -> interface (dashed)
    for f in facts:
        for p in sorted(f.get("consumerPkgs") or []):
            out.append(f'  hdr_{pid[p]} -> {iface_id[f["interface"]]} '
                       f'[color="{C_CONSUME}", style=dashed, label="consumes"];')
    out.append("}")
    return "\n".join(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("consumes_json", help="output of `consumes <dir> ./... --json`")
    ap.add_argument("--interface", default=None, help="only interfaces whose fq contains this")
    ap.add_argument("--all", action="store_true", help="show every interface, not just cross-boundary/flagged")
    ap.add_argument("--format", choices=["text", "dot"], default="text")
    ap.add_argument("--title", default=None)
    args = ap.parse_args()

    data = load(args.consumes_json)
    module = data.get("module", "")
    facts = select(data.get("interfaces", []), args)

    if args.format == "dot":
        sys.stdout.write(render_dot(module, facts, args.title) + "\n")
    else:
        sys.stdout.write(render_text(module, facts) + "\n")


if __name__ == "__main__":
    main()
