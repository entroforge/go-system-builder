---
name: code-quality
description: Use when code is authored or reviewed for maintainability, reuse, abstraction, hardcoding, readability, or complexity
category: best-practice
version: 1.0.0
---
# Code Quality
## Authority
Quality guidance only. Stage routing lives in `docs/agent-protocol.md`; team scope comes from the current manifest and runtime evidence; the reusable review method is inlined below.
## Applicability
Apply to code changes, QA module-code, and reuse/abstraction responsibilities.
## Required Inputs
Read changed code, local conventions, tests, public contracts, and dependency boundaries.
## Quality Criteria
Check correctness clarity, naming, cohesion, dependency direction, duplication, useful reuse, abstraction timing, hardcoding, magic values, oversized units, dead/debug code, and error handling.
## Outputs
Maintainable implementation or one auditable quality conclusion.
## N/A Criteria
N/A only when no executable or configuration code is in scope.
## Stop Conditions
Stop when improvement requires unrelated refactoring or contract change.
## Non-Goals
Do not optimize for fewer lines or speculative abstraction.

## Inlined Methodology

Team planning uses a mandatory responsibility baseline plus risk-triggered responsibilities plus scope partitioning plus conflict-graph constraints yielding one single-responsibility assignment per teammate. Agent count is the number of resulting assignments, not a fixed target. QA baseline responsibilities include `QA-MODULE-CODE` and `QA-REUSE-ABSTRACTION`; these map to the code-quality scope. Risk-tag derivation: frontend view or interaction -> `ui`, `frontend`; API, event or shared schema -> `api`, `cross-component`; persistence or migration -> `database`, `migration`; auth, permission or sensitive data -> `security`; shared package or multi-module design -> `architecture`; existing behavior or historical BUG touched -> `regression`. Scope partitioning rules: distinct write ownership or repository boundary; independent module or bounded-context responsibility; incompatible Best Practices or tool needs; high merge-conflict paths; independently testable Closing Contracts. One assignment may not exceed 30 files or 3 material modules (the §8.2 builder sizing rule). Conflict-graph `must_separate` edges when responsibilities require different conclusion enums, one responsibility checks the other's authored or repaired output, security/release/permission enforcement needs independent evidence, scopes require incompatible Skills or tools, or combining them would exceed workload limits. Teammate reuse requires same role family and responsibility ID, unchanged scope, matching fingerprints, current prior read-back/activation, no unresolved report/BUG/blocked-task conflicts, valid independence, and non-stale teammate state.
