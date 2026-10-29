---
name: performance-review
description: Use when latency, throughput, scale, query volume, batching, memory, CPU, or resource budgets may change
category: best-practice
version: 1.0.0
---
# Performance Review
## Authority
Measured budgets and requirements own conclusions. Stage routing lives in `docs/control/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
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

## Review method

Identify the assigned workload, data sizes, resource budget and expected scaling
before choosing measurements. Compare actual baseline and candidate behavior under
representative conditions; account for caching, warmup, noise and dependencies.
Trace observed latency, allocation, query amplification or contention to the actual
path and report the budget consequence. Separate static complexity concerns from
measured regressions and keep unmeasured assumptions explicit.

Use the existing answer and referenced measurement outputs. Main owns responsibility
grouping and independent dispatch under the current ReviewPlan; this method adds no
actor count, file cap, activation or second report. A missing workload or test
environment goes to Main with the actual limitation rather than a fabricated PASS.
