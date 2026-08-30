---
name: playwright-e2e
description: Use when Playwright browser tests, end-to-end flows, browser fixtures, or UI test evidence change
category: best-practice
version: 0.3.0
---
# Playwright E2E
## Authority
Technical practice only. S7 evidence ownership remains in `docs/agent-protocol.md`; route dispatch and clean-round adjudication remain in `e2e-browser-testing`.
## Applicability
Apply to browser automation, executable user flows, fixtures, selectors, or E2E evidence.
## Required Inputs
Read the module current-truth package (`scenario-model.json`, generated cases/coverage,
fixture contract, `stories.md`, `flows.md`, and current HTML), browser support target,
test data, and environment contract.
## Quality Criteria
Execute human navigation steps, use stable semantic selectors, isolate test data, assert
common oracle fields (`visible`, `terminal_state`, `persisted_effects`,
`forbidden_side_effects`) and, for negative cases, `rejection`, `expected_state`, and
`recovery`; retain traceable evidence and source-backed recovery N/A.
## Outputs
Repeatable browser tests or a scoped E2E review conclusion with browser evidence.
## N/A Criteria
N/A only when the affected behavior has no browser-visible user path.
## Stop Conditions
Stop on URL-jump-only coverage, missing required control selectors, or an unrepeatable environment.
## Non-Goals
Do not replace S7 responsibility assignment or clean-round decisions.

## Operating Procedure
1. Read the assigned CASE, branch polarity, oracle, PATH, controls, and fixture contract from the module package; define isolated persona/data fixtures and the initial browser state.
2. Implement each human action using role, label, or approved `data-test` locators. Use direct API/URL operations only to establish fixtures, never to skip the route under test.
3. Assert visible checkpoints and relevant request outcomes with auto-waited conditions, not sleeps. Add trace, screenshot, or video on failure for diagnosis.
4. Rerun the spec independently and hand route-level evidence to `e2e-browser-testing` for S7 reporting.

## Evidence Checklist
- Module, CASE ID, branch polarity, flow/PATH ID, personas, fixture setup/cleanup, and control-to-locator map.
- Assertions for each checkpoint, exception/permission branch, and terminal user-visible result.
- Repeatable command plus trace/screenshot references for any failure.

## Common Failure Modes
- `page.goto()` reaches an assertion while bypassing buttons and navigation logic.
- CSS/text/DOM-chain locators or arbitrary `waitForTimeout` hide real regressions.
- One shared account or record makes tests order-dependent or masks permission defects.

## Primary Sources
- [Playwright best practices](https://playwright.dev/docs/best-practices)
- [Playwright locators](https://playwright.dev/docs/locators)
