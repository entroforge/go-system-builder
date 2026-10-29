# Contracts

This directory stores contract templates and contract rules.

An active S3 batch consists of its original current-REQ index and declared
contracts. Other REQ contracts remain readable, but their text cannot fill a
missing CASE citation in this batch. The module retains its complete current
behavior; only CASE obligations owned by this exact REQ must close here.
Unqualified FR IDs belong to this REQ; qualified `REQ/FR` references resolve
their actual owner, without borrowing another requirement's same-numbered ID.
The checker uses the original bound requirement path and original delivery set.
FE, BE and SYNC applicability are independent professional decisions. Service
callers/workers need no invented FE, HTML, PATH or browser spec; preserve their
CASEs and real service validation sources. If all types are genuinely
not applicable, the current receiver cannot accept an empty delivery: report
the stage adaptation gap rather than fabricate a BE contract.
`contracts check` is available for autonomous self-review and diagnosis; it is
not an additional submission procedure. Without an active REQ it is an
all-project authoring diagnostic,
and does not certify any one S3 batch.

Clause references must resolve to the target contract's existing `本合同条款`
column in a Markdown pipe table with a header and separator. Bare `§n` and
the target's own qualified ID are declarations; prose, fenced examples and
other-contract references are not. This checks numbered structure; S3 authors
and S5 reviewers still judge promises and coverage. No separate clause registry
or author proof is required.

## Lock Authority

Authors record actual contract scope, clauses, applicability and necessary validation sources. Runtime and the original consumer supply machine fingerprints and registration associations; authors do not assemble or copy fingerprint, receipt or revision metadata. Legal stage transitions remain in `docs/control/loop-definition.json`; this README does not maintain a second gate list.

## Locked Contract Means

Document `locked` is the author's readiness declaration; it does not by itself
accept or freeze a delivery. Current unreceived S3 outputs may be corrected and
self-reviewed continuously, followed by required Git delivery and a normal save
of the original answer. Accepted contracts and their shared baseline are the
Builder's source of truth. Substantive changes to accepted scope, API, fields,
errors, state, side effects or acceptance, and changes to frozen upstream
inputs, follow the original formal rework / `docs/rules/change-control.md`
route. Preserve accepted facts rather than overwriting or refreshing them.

## Quality Gates

Locked contract quality gates, such as lint, typecheck, build, tests, or contract tests, must be explicit.

Builder command evidence belongs in `TASK-*`; delivery correctness belongs in `REV-*`; coverage and test quality belong in `QA-*`; real-browser user-flow evidence belongs in `E2E-*`; `ACC-*` links the final evidence.

## Templates

| Type | Template |
|:---|:---|
| Overview | `CONTRACTS-template.md` |
| Frontend | `FE-contract-template.md` |
| Backend | `BE-contract-template.md` |
| Sync/API | `SYNC-contract-template.md` |


## Shared model authority

Follow [shared-model contracts](../../rules/shared-model-contracts.md). For actual shared operations, data shape has one native authoring source and SYNC owns shared behavior; applicable FE/BE responsibilities derive from them. FE-only local behavior and BE-only internal work may need no SYNC. No shared data uses an explicit `none` policy and concrete reason, without inventing a Schema or examples. Mark inapplicable template references with their sourced N/A basis and remove placeholder rows; retain all actual clauses, CASEs and current-batch obligations. Structural negative examples fail schema validation; business negatives remain valid data and are rejected by the business rule.

Current S3/S4 professional corrections use the explicit
[planning rework boundary](../../control/agent-protocol.md#s3): design rework
for frozen S2 corrections, contract rework for accepted contracts with unchanged
S2. Technical issuance or outlet recovery cannot revise accepted judgments.
