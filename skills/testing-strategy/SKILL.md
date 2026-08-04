---
name: testing-strategy
description: Use when executable behavior changes or unit, integration, regression, or failure-path evidence is reviewed
category: best-practice
version: 1.0.0
---
# Testing Strategy
## Authority
Tests prove contract behavior; they do not redefine it. Stage routing lives in `docs/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable testing method is inlined below.
## Applicability
Apply to `behavior-change`, QA unit-test, and QA integration-test responsibilities.
## Required Inputs
Read acceptance clauses, Closing Contract, changed paths, failure modes, and integration boundaries.
## Quality Criteria
Map clauses to tests; cover branches, boundaries, errors, states, side effects, rollback, retries, regressions, and meaningful assertions. Verify tests fail for the intended defect.
## Outputs
Test plan, executable evidence, coverage gaps, or one scoped QA conclusion.
## N/A Criteria
N/A only with evidence that no executable behavior changed.
## Stop Conditions
Stop on untestable acceptance, hidden dependencies, or misleading green tests.
## Non-Goals
Do not equate line coverage with behavioral completeness.

## Inlined Methodology

Team planning uses a mandatory responsibility baseline plus risk-triggered responsibilities plus scope partitioning plus conflict-graph constraints yielding one single-responsibility assignment per teammate. Agent count is the number of resulting assignments, not a fixed target. QA responsibilities include `QA-UNIT-TEST`, `QA-INTEGRATION-TEST`; these map to the testing-strategy scope. Risk-tag derivation: existing behavior or historical BUG touched -> `regression`; API, event or shared schema -> `api`, `cross-component`; persistence or migration -> `database`, `migration`; goroutine, async, queue, retry or external service -> `reliability`, possibly `concurrency`. Scope partitioning rules: distinct write ownership or repository boundary; independently testable Closing Contracts. Conflict-graph `must_separate` edges when responsibilities require different conclusion enums, one responsibility checks the other's authored or repaired output, scopes require incompatible Skills or tools, or combining them would exceed workload limits. One assignment may not exceed 30 files or 3 material modules. Teammate reuse requires same role family and responsibility ID, unchanged scope, matching fingerprints, and non-stale teammate state. QA workgroup gate must cover every triggered architecture, security, performance, reliability, and migration responsibility.
