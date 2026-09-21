# PR #8 integration validation

This is a source-change review record, not a Runtime ReviewResult, human approval,
or product deployment authorization.

## Reviewed inputs

- PR head: `d9a1f9ed3effb7cf01fafeac4a8bd92eda33ac33`.
- Upstream `integration/loop-next`: `09cf42175acbf62aa54f97b72fac703b68dacd39` (includes #7).
- Original P1/P2 regression repair: `23da89bd6f08de99776bf0e368cf264338fd64e0`.
- Validated index tree before adding this report: `db75aa407fe5744500af984ff8d367255e92f874`.
- Host: Linux amd64, Go 1.26.3, Git 2.34.1.

Both P1/P2 counterexamples failed on the original PR head before implementation.
Their fixed tests exercise production classification/projection, not a replacement
implementation. The combined candidate retains both branch histories.

## Changes and disproof checks

| Review concern | Resolution and counterevidence |
| --- | --- |
| P1: tracked product paths named temp/tmp/dist/coverage disappeared | Cache classification requires proven Git ignore status and no tracked descendants. Main index and HEAD protect tracked files; stored baseline membership protects committed deletions. Capture, exported freshness gate, authorized exact-digest claim, modifications and deletions are tested. Git absence retains uncertain content. |
| P2: directory entries silently reduced S7 coverage | Product directories produce an actionable diagnostic requiring a canonical file-level result. Only real evidence directories are omitted; symlinks into product paths cannot obtain that exemption. Missing artifacts and digest checks remain enforced. |
| Competing branch authorities | `bound_req.workspace` retains REQ root/dev/release authority. `ExecutionRegistry` describes execution instances and validates against that authority; missing authority or another target branch is rejected. Explicit relocation updates the root projection through CAS without changing the branch authority. |
| New document layout and locks | Canonical control/dev/architecture/release-audit paths are used across new workspace commands, recovery, protection, examples and packaging. Registered Workers cannot use the temporary-worktree exemption to write locked inputs. |
| Integration receipts and retries | Keep original scope/source ancestry, explicit preserved retries, frozen-path checks, canonical Result path+SHA, merge reachability, tested HEAD and check receipts. Refreshing a Result triggers checks without another merge. An unacknowledged verified result is rechecked when target HEAD advances. |
| Durable acknowledgement and cleanup | Main records `completion_acknowledged` through the Agent lifecycle only after all owned deliveries are verified. Cleanup failures retain `cleanup_pending`; cleanup-only retries do not rerun valid checks. New worker commits are preserved, never deleted during cleanup. |
| Lost receipt and concurrent work | Exact merge parents distinguish a lost receipt from an unrelated merge. A still-unmerged ready candidate is reinspected against its original scope before a moved target is accepted. One process-owned integration lock serializes writes; a contending caller retries the same assignment. |
| Check modes | `post_merge` skips only duplicate dynamic prechecks; both modes retain static scope/frozen-input checks and required Main merged-tree verification. Worker sandbox receipts do not replace Main checks. |
| Packaging and installation | Keep upstream fresh-target atomic install and blueprint exclusions, five native artifacts, shell/PowerShell launchers, and package SHA inventory. The installer verifies all packaged bytes, rewrites installed links, and records derived bytes separately. Extra/missing/corrupt/symlink assets fail validation; a tampered real release does not publish a target. |
| Test/CI compatibility | Fixtures now declare real REQ branch authority, activation/lifecycle information and canonical manifest references. Conflict resolutions retain safety assertions. PR CI now also targets `integration/loop-next`. |

Git 2.34 portability uses quoted porcelain worktree output when `worktree list -z`
is unavailable. It preserves custom worktree boundaries and reminder behavior.

## Verification

- `go test ./... -p 2 -count=1`: PASS, 51 tested packages; fixture-only packages have no test files.
- `go vet -p 2 ./...`: PASS.
- `gofmt` and `git diff --check`: PASS.
- Relevant race suite: PASS for integration, workspace, runtime, hook, controller, filelock, metrics, hookctx, repair and review (`go test -race -p 2` on those packages, `-count=1`).
- Five-platform release build: Darwin amd64/arm64, Linux amd64/arm64, Windows amd64 PASS.
- Actual packaged-binary install into a new directory: PASS.
- Installed `doctor`, `validate --all`, `release-graph validate --installed`, `docs check`: PASS.
- Installation receipt: all 206 listed file digests match; shell launcher remains a script.
- Source-tree init in the isolated clone, doctor, validate, docs check: PASS.
- Configured Hook process/JSON smoke: PASS. Detected Claude CLI 2.1.278; this proves installation/process interfaces only.

A preliminary full run exposed three case-sensitive fixture expectations, an
incorrectly populated missing-branch negative test, and a 100ms lock-budget failure
under high package parallelism. The fixture errors were corrected; the unchanged
bounded lock passed in the final lower-parallelism full run. Production lock
contention remains a retryable condition, not success.

The release acceptance artifact was built from the validated merge working tree
and truthfully reports `vcs.modified=true`. It is a local validation artifact, not
a signed release. The commit containing this report identifies the source delivery.

## Limits and follow-up

- Real Claude multi-agent continuation and the full interactive workflow were NOT RUN.
  CLI 2.1.278 differs from the documented 2.1.276 reference; no platform acceptance
  is inferred from cross-builds or process smoke tests.
- Native Windows/macOS execution was NOT RUN. The build matrix verifies compilation.
- Worker command isolation still requires Linux Bubblewrap/user namespaces/seccomp,
  blocks network access, and caps its check copy at 512 MiB. Validate actual project
  dependencies in a disposable project before adoption; do not weaken that boundary.
- Existing active installations are not upgraded by this PR. The current product
  REQ-054, its framework files, Runtime and evidence were not modified by this work.
- Reviewer/maintainer owns upstream re-review and merge. Project installer owner
  owns platform/project acceptance before deployment. Keep the existing matched
  asset group for recovery; do not overlay this layout onto an active old Runtime.

## Local log fingerprints

Logs are available in the task's isolated validation directory environment; the
commands above reproduce the checks. These fingerprints identify this run without
shipping generated Runtime state or large binaries in source control.

| Log | SHA256 |
| --- | --- |
| `pr8-full-final2.log` | `e402bfd40b68c32d23cf46bc588789b265eea053f8238a7fdae805c6bce8e066` |
| `pr8-vet.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `pr8-final-doctor.log` | `02d8f4028dae83b3f5c16d8c20cc0df6bdb64a7113d2b6da9e76436b3f336c13` |
| `pr8-final-validate.log` | `16e3bbb50d11d6de04878dd016227f280125582b449c85f8c4ec00770697b6ee` |
| `pr8-final-graph.log` | `20b011ec1423ecf292f92d302f6a6f5253bf7ecef6e7ebbc5bc939c85d46eae0` |
| `pr8-final-docs.log` | `e0351c01d7be547377ae3d46b36219ec63d66e59f8b820c574889632961f8fc4` |
| `pr8-install-negative.log` | `c46da3b8a75284973ed17cf0176311db8e1f7a4738d709cebe2c6a7d32e5314b` |
| `pr8-race.log` | `5603b56881103d3dd52c8c58bb5dd97736ab440c6436df25011994eb802fda3f` |
