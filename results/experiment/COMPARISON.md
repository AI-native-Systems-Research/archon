# AI Reviewer Experiment: with vs without ARCHON (re-run, transcripts saved)

I re-ran the with/without-ARCHON reviewer experiment and saved every input and
every output to disk this time. This doc records exactly what was run, where the
files are, and an honest read of the result (which is more nuanced than the first
write-up in `POC_RESULTS.md`).

## What I ran

For three todo-api commits (cache-aside, kafka, vault) I gave the SAME reviewing
task to two independent AI reviewers, changing only the information each received:
- **Reviewer A (diff only):** just the code diff, with the author's `docs/*.md`
  architecture write-up held out.
- **Reviewer B (diff + ARCHON):** the same diff plus ARCHON's `delta` report.

Each reviewer wrote its review to its own file. Same model, one reviewer per
condition, three commits. All six reviews plus the exact inputs are on disk.

## Exact file paths (all under `results/experiment/`)

Inputs given to the reviewers:
- `cache-aside.diff`, `cache-aside.archon.txt`
- `kafka.diff`, `kafka.archon.txt`
- `vault.diff`, `vault.archon.txt`

Reviewer outputs:
- `cache-aside.reviewA_diffonly.md`, `cache-aside.reviewB_archon.md`
- `kafka.reviewA_diffonly.md`, `kafka.reviewB_archon.md`
- `vault.reviewA_diffonly.md`, `vault.reviewB_archon.md`

## Result, commit by commit

### Cache-aside
- **Both** reviewers led with the real bug: **no cache invalidation on writes**,
  so up to 25s of stale reads. Both also flagged "no tests for the new package,"
  silent `Set()` failure, and the TTL/expiration bug.
- **What ARCHON added (B only):** it named the exact contract gap in interface
  terms: `memcached.Task implements service.TaskSearchRepository` and
  `elasticsearch.Task implements memcached.Datastore`, each with **no contract
  test**. B reframed "no tests" into "these specific interface contracts are
  compile-time only, behavior unverified."

### Kafka
- **Reviewer A (diff only) was very strong here.** It caught: no tests for the
  Kafka code, producer async-produce with no delivery confirmation (silent
  message loss), no dead-letter handling, a shutdown race, single partition, and
  even "interface parity not enforced between `rabbitmq.Task` and `kafka.Task`."
- **What ARCHON added (B only):** it stated the contract gap precisely,
  `kafka.Task implements service.TaskMessageBrokerRepository` with no contract
  test, and framed it as **behavioral drift risk between the two broker
  implementations**. B also cleanly enumerated the new service node and the
  package rename from ARCHON's delta.
- Honest note: on this commit A arguably surfaced MORE operational risk than the
  ARCHON framing did. ARCHON's value was making the contract gap precise, not
  catching something A missed wholesale.

### Vault
- **Both** caught the security issues (token in env, HTTP not TLS, committed
  example token), the thread-unsafe `vault.Provider` cache, and `newDB()` using
  `log.Fatal`.
- **What ARCHON added (B only):** the `envvar.Provider` interface has **three
  implementers and no contract test** (named explicitly), and the **config
  boundary change**: `env:DATABASE_URL` removed, `env:VAULT_TOKEN/ADDRESS/PATH`
  and `cap:net` added. B laid this out as a table because ARCHON handed it that
  structure directly.

## Honest conclusion

ARCHON's consistent, repeatable contribution across all three commits was **not**
that the diff-only reviewer was blind. The diff-only reviewer was strong and
often caught "there are no tests here" on its own. ARCHON's lift is narrower and
sharper:
1. It turns a vague "no tests" into a **precise, certain contract-coverage
   statement**: which interface, how many implementers, guarded or not. That is a
   fact ARCHON computes, not a judgment the reviewer has to make.
2. It reliably surfaces the **boundary and config deltas** (a removed config key,
   a new service node, a new capability) that are easy to miss in a large diff.
3. It framed the contract gap as **behavioral-drift risk between implementations**
   of one interface, which is the architectural way to see it.

This refines the earlier `POC_RESULTS.md` claim ("the diff-only reviewer missed
the evidence gap 3/3"). With transcripts saved, the more honest statement is:
**both reviewers catch the missing tests; ARCHON makes the contract gap precise
and certain, and adds the boundary/config delta.** That is still a real,
repeatable value, but it is an assist, not a rescue.

## Caveats
- Small sample: 3 commits, one reviewer per condition, same underlying model.
- Possible contamination: todo-api is public, so the model may recall it.
- No dependency-graph baseline condition; token cost and review time not measured.
- The reviewers are AI subagents, not human reviewers.
