# Contracts

This directory stores contract templates and contract rules.

## Lock Authority

Contract templates record the runtime identity, transition, exact fingerprints,
and document-verification evidence that locked the batch. The Writer's commit
revision is optional audit metadata, not a handoff prerequisite. Legal guards are
defined in `docs/loop-definition.json`; this README does
not maintain a second gate list.

## Locked Contract Means

The contract is the Builder's source of truth.

Any change to scope, API, fields, errors, state, side effects, or acceptance must go through `docs/rules/change-control.md`.

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
