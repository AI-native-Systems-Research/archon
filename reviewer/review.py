#!/usr/bin/env python3
"""archon review -- one command that produces the reviewer views for a PR.

This is a thin, deterministic orchestrator. It runs the `archon-go` / `consumes`
binaries and the sibling reviewer scripts in the right order, so a reviewer can
get every altitude of a PR with a single call instead of wiring the pipeline by
hand. It adds no analysis of its own and calls no model -- it only sequences
tools that are already deterministic, so the same repo + commits yield
byte-identical artifacts on re-run.

    python3 reviewer/review.py <repo> <commitA> <commitB> --level {1,2,3}

Levels (escalating altitude; each is self-contained, not cumulative):

  --level 1  SUMMARY   what changed at the surface: packages / exported symbols /
                       schemas / edges / invariants, with a triage verdict.
                       (surface_delta.py, from `archon-go delta --json`)

  --level 2  STRUCTURE where it landed: the system as auto-derived component
                       boxes, and the PR's change painted on them.
                       (component_view.py + component_delta.py, from `extract`)

  --level 3  CONTRACTS why edges changed: per-edge witnesses (full vs PARTIAL
                       decoupling) plus the interface-contract delta and any
                       stranded-contract smell flip.
                       (witness_delta.py + contract_delta.py, from `extract`
                        and `consumes`)

What each level needs, and how it is produced:

  delta.json      = archon-go delta  <repo> A B --json          (reads commits;
  X.extract.json  = archon-go extract <repo> <commit>            no checkout)
  X.consumes.json = consumes <repo> ./... --json                 (working-tree
                    run once per commit -- REQUIRES a checkout of that commit;
                    the wrapper checks the commit out and restores HEAD after)

Because `consumes` is working-tree based, level 3's contract half checks each
commit out. Do not point level 3 at a repo another job is using. If you already
have the JSON artifacts (e.g. from a prior run), pass --reuse and the wrapper
skips every binary call and just re-renders -- fully offline, no repo touched.
Use --skip-contract to get level 3's witness view without any checkout.

Flags:
  --outdir DIR       where artifacts + text + PNGs go (default: ./archon_review_A_B)
  --label-a / --label-b   names for the two commits in the output
  --depth N          component granularity for level 2 (default 2)
  --interface SUBSTR focus the level-3 contract view on one interface
  --from / --to SUBSTR   focus the level-3 witness view on one dependency
  --reuse            reuse any JSON artifact already in --outdir (no binary calls)
  --no-png           skip PNG rendering (text only)
  --skip-contract    level 3: witness view only, no `consumes`/checkout
  --archon PATH      path to the archon-go binary (default: <repo-root>/archon-go)
  --consumes PATH    path to the consumes binary (default: <repo-root>/consumes)
"""
import argparse
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))      # .../reviewer
ROOT = os.path.dirname(HERE)                            # repo root (has archon-go)


def short(commit):
    """A filesystem-safe short label for a commit-ish."""
    return "".join(c if c.isalnum() else "_" for c in commit)[:12]


def sh(msg):
    sys.stderr.write(msg + "\n")


def run_capture(cmd):
    """Run a command, return stdout as text. Raise with stderr on failure."""
    p = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if p.returncode != 0:
        raise RuntimeError("command failed: %s\n%s" % (" ".join(cmd), p.stderr.strip()))
    return p.stdout


def write_json_from(cmd, path, reuse):
    """Run `cmd`, capture stdout, write to `path`. Skip if reuse and path exists."""
    if reuse and os.path.exists(path):
        sh("  reuse  %s" % os.path.basename(path))
        return
    sh("  run    %s  ->  %s" % (cmd[0].split("/")[-1] + " " + cmd[1], os.path.basename(path)))
    with open(path, "w") as fh:
        fh.write(run_capture(cmd))


def git(repo, *args):
    return run_capture(["git", "-C", repo] + list(args)).strip()


def consumes_at(consumes_bin, repo, commit, path, reuse):
    """Run `consumes` at `commit` -- checks the commit out, then restores HEAD.

    consumes is working-tree based, so there is no way to snapshot a commit
    without checking it out. We record the current ref first and always restore
    it, even on failure, so the repo is left as we found it.
    """
    if reuse and os.path.exists(path):
        sh("  reuse  %s" % os.path.basename(path))
        return
    # remember where HEAD was (branch name if on one, else the exact commit)
    saved = git(repo, "rev-parse", "--abbrev-ref", "HEAD")
    if saved == "HEAD":                                # detached: pin the sha
        saved = git(repo, "rev-parse", "HEAD")
    try:
        sh("  checkout %s (for consumes)" % commit)
        git(repo, "checkout", "--quiet", commit)
        sh("  run    consumes  ->  %s" % os.path.basename(path))
        with open(path, "w") as fh:
            fh.write(run_capture([consumes_bin, repo, "./...", "--json"]))
    finally:
        sh("  restore HEAD -> %s" % saved)
        git(repo, "checkout", "--quiet", saved)


def render_png(dot_text, png_path, no_png):
    """Pipe DOT text through Graphviz to a PNG. No-op if disabled/unavailable."""
    if no_png:
        return
    if not shutil.which("dot"):
        sh("  (skip PNG: Graphviz `dot` not on PATH)")
        return
    p = subprocess.run(["dot", "-Tpng", "-o", png_path], input=dot_text,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if p.returncode != 0:
        sh("  (PNG failed: %s)" % p.stderr.strip())
    else:
        sh("  wrote  %s" % os.path.basename(png_path))


def script(name, *args):
    """Invoke a sibling reviewer script, return its stdout."""
    return run_capture([sys.executable, os.path.join(HERE, name)] + list(args))


def emit(section, text, txt_path):
    """Print a view to the console and save it alongside the artifacts."""
    with open(txt_path, "w") as fh:
        fh.write(text)
    print("\n" + "=" * 72)
    print("  %s   (saved: %s)" % (section, txt_path))
    print("=" * 72)
    print(text.rstrip())


def main():
    ap = argparse.ArgumentParser(description="Produce ARCHON reviewer views for a PR.")
    ap.add_argument("repo")
    ap.add_argument("commit_a")
    ap.add_argument("commit_b")
    ap.add_argument("--level", type=int, choices=[1, 2, 3], required=True)
    ap.add_argument("--outdir", default=None)
    ap.add_argument("--label-a", default=None)
    ap.add_argument("--label-b", default=None)
    ap.add_argument("--depth", type=int, default=2)
    ap.add_argument("--interface", default=None)
    ap.add_argument("--from", dest="from_", default=None)
    ap.add_argument("--to", default=None)
    ap.add_argument("--reuse", action="store_true")
    ap.add_argument("--no-png", action="store_true")
    ap.add_argument("--skip-contract", action="store_true")
    ap.add_argument("--archon", default=os.path.join(ROOT, "archon-go"))
    ap.add_argument("--consumes", default=os.path.join(ROOT, "consumes"))
    args = ap.parse_args()

    la = args.label_a or short(args.commit_a)
    lb = args.label_b or short(args.commit_b)
    outdir = args.outdir or os.path.join(
        os.getcwd(), "archon_review_%s_%s" % (short(args.commit_a), short(args.commit_b)))
    os.makedirs(outdir, exist_ok=True)

    # artifact paths (stable names, so --reuse can find them)
    a_ext = os.path.join(outdir, "A.extract.json")
    b_ext = os.path.join(outdir, "B.extract.json")
    delta = os.path.join(outdir, "delta.json")
    a_con = os.path.join(outdir, "A.consumes.json")
    b_con = os.path.join(outdir, "B.consumes.json")
    comps = os.path.join(outdir, "components.json")

    sh("archon review  L%d  %s..%s  ->  %s" % (args.level, la, lb, outdir))
    if not args.reuse and not os.path.exists(args.archon):
        sys.exit("archon-go binary not found at %s (build it, or pass --archon)" % args.archon)

    # ---- produce only the artifacts this level needs -----------------------
    if args.level == 1:
        write_json_from([args.archon, "delta", args.repo, args.commit_a, args.commit_b, "--json"], delta, args.reuse)
    elif args.level == 2:
        write_json_from([args.archon, "extract", args.repo, args.commit_b], b_ext, args.reuse)
        write_json_from([args.archon, "delta", args.repo, args.commit_a, args.commit_b, "--json"], delta, args.reuse)
    elif args.level == 3:
        write_json_from([args.archon, "extract", args.repo, args.commit_a], a_ext, args.reuse)
        write_json_from([args.archon, "extract", args.repo, args.commit_b], b_ext, args.reuse)
        if not args.skip_contract:
            if not args.reuse and not os.path.exists(args.consumes):
                sys.exit("consumes binary not found at %s (build `go build -o consumes ./cmd/consumes`, "
                         "or pass --consumes / --skip-contract)" % args.consumes)
            consumes_at(args.consumes, args.repo, args.commit_a, a_con, args.reuse)
            consumes_at(args.consumes, args.repo, args.commit_b, b_con, args.reuse)

    # ---- render the views for this level -----------------------------------
    if args.level == 1:
        txt = script("surface_delta.py", delta, "--label-a", la, "--label-b", lb)
        emit("LEVEL 1 -- SURFACE DELTA (what changed)", txt, os.path.join(outdir, "level1_surface.txt"))
        dot = script("surface_delta.py", delta, "--label-a", la, "--label-b", lb, "--format", "dot")
        render_png(dot, os.path.join(outdir, "level1_surface.png"), args.no_png)

    elif args.level == 2:
        # auto-derive the component grouping from the after-graph, no hand input
        with open(comps, "w") as fh:
            fh.write(script("component_view.py", b_ext, "--depth", str(args.depth), "--emit-components"))
        sh("  wrote  %s" % os.path.basename(comps))
        mermaid = script("component_view.py", b_ext, "--depth", str(args.depth), "--format", "mermaid")
        emit("LEVEL 2 -- COMPONENT MAP (the system as subsystem boxes)", mermaid,
             os.path.join(outdir, "level2_components.mmd"))
        dot_map = script("component_view.py", b_ext, "--depth", str(args.depth), "--format", "dot")
        render_png(dot_map, os.path.join(outdir, "level2_components.png"), args.no_png)
        title = "%s  %s..%s" % (os.path.basename(args.repo.rstrip("/")), la, lb)
        dot_delta = script("component_delta.py", delta, comps, title, "--graph", b_ext)
        render_png(dot_delta, os.path.join(outdir, "level2_component_delta.png"), args.no_png)

    elif args.level == 3:
        wargs = [a_ext, b_ext, "--label-a", la, "--label-b", lb]
        if args.from_:
            wargs += ["--from", args.from_]
        if args.to:
            wargs += ["--to", args.to]
        txt = script("witness_delta.py", *wargs)
        emit("LEVEL 3a -- WITNESS DELTA (why each edge survived / weakened / died)",
             txt, os.path.join(outdir, "level3_witness.txt"))
        render_png(script("witness_delta.py", *(wargs + ["--format", "dot"])),
                   os.path.join(outdir, "level3_witness.png"), args.no_png)

        if not args.skip_contract:
            cargs = [a_con, b_con, "--label-a", la, "--label-b", lb]
            if args.interface:
                cargs += ["--interface", args.interface]
            ctxt = script("contract_delta.py", *(cargs + ["--format", "text"]))
            emit("LEVEL 3b -- CONTRACT DELTA (interfaces, implementers, stranded-smell flips)",
                 ctxt, os.path.join(outdir, "level3_contract.txt"))
            render_png(script("contract_delta.py", *(cargs + ["--format", "dot"])),
                       os.path.join(outdir, "level3_contract_delta.png"), args.no_png)

    sh("\ndone. artifacts + text + PNGs in %s" % outdir)


if __name__ == "__main__":
    main()
