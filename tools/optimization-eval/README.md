# Optimization evaluation factory runner

This factory tool freezes twelve named cases and runs their original CLI/Hook
mechanism tests in a separate Git source checkout. It does not ship in template
releases or write production Runtime. Seeded fixtures and a Claude process
substitute are not autonomous model acceptance.

```sh
GOPROXY=off go run ./tools/optimization-eval list
GOPROXY=off go run ./tools/optimization-eval run --root . --case all --mode mechanism
GOPROXY=off go run ./tools/optimization-eval run --root . --case G08-02 --mode real_host
```

`--output` selects a parent for unique `run-*` directories; the default is
`.local/optimization-evals`. Every run preserves its source, source manifest,
catalogue, public input views, original Go JSON events/stderr and per-case result.
An existing experiment is never overwritten. Failed/interrupted artifacts are
kept for diagnosis. Delete only a run you have reviewed and no longer need.

Source capture includes actual dirty factory source, not just HEAD. Each file's
bytes, mode and aggregate digest are frozen; linked paths, sensitive/live roots
and unbounded snapshots are refused or excluded. Tests execute in the copy;
changed tracked inputs or new factory source after execution invalidate the run.
Go commands use fixed argv, disable test caching and preserve exact Test/subtest
events. Package success without the target is an error; skip is not pass.
Command/output budgets apply and cancellation kills the owned process tree.

`pass` proves only the case's stated mechanism scope. It does not prove model
Read/Skill consumption, usability, throughput, quality improvement or token cost.
Usage and model traces are unavailable until an actual provider is implemented.
Case variables are currently frozen metadata, not changes to model or production
configuration; all first catalogue cases declare an empty variable set.

`real_host` currently records `not_run` and exits nonzero because original live
issuance/oracle isolation adapters are still pending. It does not silently run
mechanism tests. `public-inputs.json` omits the oracle and fixture paths; this
separation is not a claim that a live host filesystem sandbox exists.

Live host adapters, actual business project inputs, oracle isolation, repeated
A/B evaluations and production metrics remain outside the current runner. The
shared observation and validation boundaries are defined in the
[L4 runtime control plane](../../blueprint/l4-mechanisms/L4-runtime-control-plane.md#7-错误观测与成本).
