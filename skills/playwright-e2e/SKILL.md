---
name: playwright-e2e
description: Use when Playwright browser tests, end-to-end flows, browser fixtures, or UI test evidence change
category: best-practice
version: 0.3.1
---
# Playwright E2E
## Authority
Technical practice only. S7 responsibility and evidence ownership remain in `docs/control/agent-protocol.md`; `e2e-browser-testing` supplies browser verification methods. Main dispatches under the current ReviewPlan, and the original S7 consumer and exit gate decide clean-round qualification.
## Applicability
Apply to browser automation, executable browser flows, fixtures, selectors and browser evidence. API, command or state-based E2E responsibilities without a browser path use their actual methods; this Skill does not require a frontend or browser assets for them.
## Required Inputs
Read the module current-truth package (`scenario-model.json`, generated cases/coverage,
fixture contract, `stories.md`, `flows.md`, and current HTML), browser support target,
test data, and environment contract.
## Quality Criteria
Execute human navigation steps, use the current selector contract, isolate test data, assert
common oracle fields (`visible`, `terminal_state`, `persisted_effects`,
`forbidden_side_effects`) and, for negative cases, `rejection`, `expected_state`, and
`recovery`; retain traceable evidence and source-backed recovery N/A. Playwright or
Storybook snapshot equality proves implementation drift only (`tools/visual-qa/README.md`);
it does not prove the Design Thesis.
## Outputs
Repeatable browser tests or a scoped E2E review conclusion with browser evidence.
## N/A Criteria
N/A only when the affected behavior has no browser-visible user path.
## Stop Conditions
Stop the affected check on URL-jump-only coverage, missing required control
selectors or an unrepeatable environment. Report the actual observation or
missing prerequisite with the affected Claim and real evidence to Main; continue
other safe assigned checks without rewriting product or locked sources. A real
P0 or unsafe operation stops dangerous work and retains actual observations and
capture gaps; ordinary Findings do not stop the whole Assignment.
## Non-Goals
Do not replace S7 responsibility assignment or clean-round decisions.

## Operating Procedure
1. Read the assigned CASE, branch polarity, oracle, PATH, controls and fixture contract. During S7, read existing sources/specs without editing them; for cold_start create only needed scripts/fixtures in the declared verification_artifact_workspace. Use the actual selected paths and runner configuration, preserving existing web/e2e assets as read/run-only.
2. For S7 browser review and its permitted cold-start scripts, use the original `data-test` selector contract from `e2e-browser-testing`; role/label locators are not a fallback for a missing required hook. Report that affected check to Main and continue other safe assigned checks without editing product code. Outside this S7 contract, implementation may use role/label locators only when the assigned project's selector contract allows them. Use direct API/URL operations only to establish fixtures, never to skip the route under test.
3. Assert visible checkpoints and relevant request outcomes with auto-waited conditions, not sleeps. Add trace, screenshot, or video on failure for diagnosis.
4. Run only the assigned Claims/CASEs through their actual selected specs; Main owns full-round module coverage. Record passing negative observations in ClaimResult.observed and referenced real evidence; Finding encounter fields apply only to failed Claims. Run the assigned independent verification, retain its actual browser evidence and reference it in the original S7 answer using the methods in `e2e-browser-testing`. Repeat only when needed to resolve a real observation; do not repeat unsafe P0 actions or create another report.

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
