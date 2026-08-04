---
name: integration-verification
description: Use when two or more components, services, data stores, queues, caches, or frontend/backend paths interact
category: best-practice
version: 1.0.0
---
# Integration Verification
## Authority
The SYNC and component contracts own expected data flow. Stage routing lives in `docs/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
## Applicability
Apply to `cross-component` risk and integration responsibilities.
## Required Inputs
Read all participating contracts, environments, credentials boundaries, and failure semantics.
## Quality Criteria
Exercise real serialization, auth, data flow, transactions, timeouts, retries, errors, event ordering, caches, queues, rollback, and E2E paths.
## Outputs
Integration evidence with environment and observed results.
## N/A Criteria
N/A only when evidence proves no integration boundary exists.
## Stop Conditions
Stop on mocked-away critical boundaries or unavailable authoritative environment.
## Non-Goals
Do not substitute isolated unit tests for integration evidence.

## Inlined Methodology

Team planning uses a mandatory responsibility baseline plus risk-triggered responsibilities plus scope partitioning plus conflict-graph constraints yielding one single-responsibility assignment per teammate. Agent count is the number of resulting assignments, not a fixed target. Delivery Verification responsibilities include `VER-INTEGRATION` and `VER-REGRESSION`; these map to integration verification. Risk-tag derivation: API, event or shared schema -> `api`, `cross-component`; existing behavior or historical BUG touched -> `regression`; latency/batch/query/scale -> `performance`; goroutine/async/queue/retry/external service -> `reliability`, possibly `concurrency`. Scope partitioning rules: distinct write ownership or repository boundary; independently testable Closing Contracts; high merge-conflict paths. Conflict-graph `must_separate` edges when one responsibility checks the other's authored or repaired output, scopes require incompatible Skills or tools, or combining them would exceed workload limits. Teammate reuse requires unchanged scope, matching fingerprints, and non-stale teammate state. Delivery Verification workgroup gate requires every triggered integration/regression responsibility to be covered.
