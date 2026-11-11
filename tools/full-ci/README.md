# Full CI verification

These helpers run the existing full verification responsibilities in separate
Linux jobs. They do not change the local `make ci-verify-full` contract:

- `bash tools/full-ci/run.sh normal` prepares a private Harness and fixture
  catalog, then runs the original normal full-repository test, formatting,
  vet, doctor, staged installation, validate and build checks. It compares the
  observed Go package results with `go list ./...`.
- `bash tools/full-ci/run.sh race` uses the same preparation for every package
  discovered by `go list -race ./...` except the exact CLI package. Each
  package retains the original 30-minute Go timeout.
- `bash tools/full-ci/run.sh cli N` runs CLI race shard `N` for `N=0..5`.
  It discovers top-level Test, Example and Fuzz cases, preserves their order,
  and assigns each to exactly one independently instrumented process.

Every invocation runs from the source checkout being verified. Set
`FULL_CI_RESULTS` to a writable job-private path outside the checkout; the
output includes per-phase status and logs. CLI jobs must upload that directory
as `cli-race-shard-N`. Once all six complete, download them under one directory
and run:

```sh
python3 tools/full-ci/race-summary.py aggregate <artifacts-dir> "$(git rev-parse HEAD)"
```

The aggregate checks source checkout identity, phase statuses, complete and
identical discovery, exact disjoint selections, Go event counts and actual
skips; it reports any missing, duplicate, extra, malformed or failed result.
The six processes do not prove single-process behavior. Live Claude checks
retain their original opt-in environment and skip when it is not configured.

Run the tool tests with:

```sh
python3 -m unittest discover -s tools/full-ci -p 'test_*.py'
```
