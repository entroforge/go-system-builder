---
name: performance-review
description: Use when latency, throughput, scale, query volume, batching, memory, CPU, or resource budgets may change
category: best-practice
version: 1.0.0
---
# Performance Review
## Authority
Measured budgets and requirements own conclusions. Stage routing lives in `docs/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
## Applicability
Apply to the `performance` risk tag.
## Required Inputs
Read workload assumptions, budgets, critical paths, data size, and baseline measurements.
## Quality Criteria
Measure before concluding; check algorithms, N+1 work, unbounded loops/queues, allocation, I/O, contention, degradation, and representative load.
## Outputs
Reproducible measurements, budget comparison, and one scoped conclusion.
## N/A Criteria
N/A when no critical path or resource profile changes.
## Stop Conditions
Stop on absent workload definition or non-reproducible measurements.
## Non-Goals
Do not optimize without evidence.

## Inlined Methodology

Team planning uses a mandatory responsibility baseline plus risk-triggered responsibilities plus scope partitioning plus conflict-graph constraints yielding one single-responsibility assignment per teammate. Agent count is the number of resulting assignments, not a fixed target. QA responsibilities include `QA-PERFORMANCE`; this maps to the performance-review scope. Risk-tag derivation: latency, batch, query or scale requirement -> `performance`; API, event or shared schema -> `api`, `cross-component`; goroutine, async, queue, retry or external service -> `reliability`, possibly `concurrency`. Scope partitioning rules: distinct write ownership or repository boundary; independently testable Closing Contracts. Conflict-graph `must_separate` edges when responsibilities require different conclusion enums, one responsibility checks the other's authored or repaired output, security/release/permission enforcement needs independent evidence, scopes require incompatible Skills or tools, or combining them would exceed workload limits. One assignment may not exceed 30 files or 3 material modules. Teammate reuse requires same role family and responsibility ID, unchanged scope, matching fingerprints, and non-stale teammate state. QA workgroup gate must cover every triggered architecture, security, performance, reliability, and migration responsibility.
