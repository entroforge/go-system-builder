---
name: reliability-review
description: Use when retries, async work, concurrency, external dependencies, recovery, or multi-instance behavior changes
category: best-practice
version: 1.0.0
---
# Reliability Review
## Authority
Quality guidance within the assigned responsibility. Stage routing lives in `docs/agent-protocol.md`; runtime authority lives in `.claude/loop-state.json`; the reusable review method is inlined below.
## Applicability
Apply to `reliability` or `concurrency` risk.
## Required Inputs
Read failure model, ownership, timeout/retry policy, idempotency keys, recovery path, and observability.
## Quality Criteria
Check races, cancellation, duplicate delivery, retries, backoff, timeouts, partial commit, crash recovery, stale locks, multi-instance safety, metrics, and traces.
## Outputs
Failure-injection evidence and one reliability conclusion.
## N/A Criteria
N/A when no asynchronous, concurrent, external, or recoverable behavior changes.
## Stop Conditions
Stop on undefined ownership, irreconcilable partial state, or missing recovery evidence.
## Non-Goals
Do not infer reliability from happy-path tests.
