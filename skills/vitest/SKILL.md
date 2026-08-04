---
name: vitest
description: Use when Vitest unit tests, component tests, mocks, coverage, or test configuration change
category: best-practice
version: 0.3.0
---
# Vitest
## Authority
Technical practice only. Stage routing remains in `docs/agent-protocol.md`; acceptance clauses determine what behavior must be proved.
## Applicability
Apply to Vitest tests, test setup, mocks, coverage configuration, or test reliability.
## Required Inputs
Read behavior contracts, test environment, existing helpers, isolation requirements, and targeted risks.
## Quality Criteria
Test observable behavior, isolate external effects, avoid order dependence, and retain useful failure diagnostics.
## Outputs
Passing focused tests or a scoped test-quality review conclusion with command evidence.
## N/A Criteria
N/A when no JavaScript or TypeScript executable behavior is affected.
## Stop Conditions
Stop on nondeterministic tests, untestable required behavior, or mocks that contradict the contract.
## Non-Goals
Do not use coverage counts as a substitute for behavior assertions.

## Operating Procedure
1. Map the changed clause to a unit or component seam, observable outcome, and relevant failure branch; choose integration/E2E instead when the behavior crosses that seam.
2. Build fixtures at owned boundaries. Mock network, time, filesystem, or third-party modules only when their implementation is outside the test's responsibility.
3. Reset mocks, module state, environment, timers, and test data between cases; await all asynchronous assertions.
4. Run the focused suite, then the relevant regression suite; use coverage only to investigate untested risk paths.

## Evidence Checklist
- Clause-to-test mapping and the assertion that would fail for the intended defect.
- Fixture/mocking boundary and cleanup behavior.
- Exact focused and regression test commands with results.

## Common Failure Modes
- Tests assert private calls, snapshots, or implementation details instead of outcomes.
- Shared singleton state or fake timers leak across cases.
- A mock reproduces the implementation under test and makes a false green result.

## Primary Sources
- [Vitest mocking](https://vitest.dev/guide/mocking.html)
- [Vitest coverage](https://vitest.dev/guide/coverage.html)
