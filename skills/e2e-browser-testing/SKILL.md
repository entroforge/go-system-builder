---
name: e2e-browser-testing
description: Use when translating module flows.md into Playwright + CDP browser tests, running cross-REQ regression sweeps, or reviewing e2e evidence
category: best-practice
version: 1.1.0
---
# E2E Browser Testing
## Authority
Tests prove flow behavior in a real browser; they do not redefine the design. This reusable method was distilled from prior project E2E work without retaining instance-artifact references. This Skill owns S7 flow-to-spec coverage and evidence-bundle requirements; `playwright-e2e` owns Playwright test-code, fixture, locator, and debugging practice. The flow-to-spec translation, hybrid CDP wiring, and evidence-bundle contracts below are the canonical shape a QA Reviewer checks during S8.
## Applicability
Apply to `behavior-change` risk on UI-impacting REQs, the QA-E2E responsibility, and any cross-REQ module-flow regression sweep.
## Required Inputs
Read the assigned module's `docs/design/prototypes/{module}/{flows.md, stories.md, *.html, index.html}`. `flows.md` and `stories.md` aggregate entries from every REQ that touches the module; each `F-NNN` / `S-NNN` entry carries its own `REQ-id` so cross-REQ traceability holds. Also read existing specs under `web/e2e/`, the locked SYNC contract (for wire-shape parity assertions), the running frontend URL, and the auth strategy.
## Quality Criteria
Map every flow `F-NNN` to a Playwright spec; cover happy path + 异常分支 + permission denial + terminal state; capture CDP events for network / console / runtime exceptions; produce idempotent re-runnable evidence. Pass the flow translation, hybrid CDP, and evidence contracts in §Flow-to-Spec Translation through §Evidence Bundle below.
## Outputs
Playwright spec files under `web/e2e/{REQ-id}-round{N}-cdp-*.spec.ts`; evidence bundle under `docs/reports/e2e/{AGENT-ID}.md` + JSONL traces + PNG screenshots; BUG drafts under `docs/reports/bugs/` for any failure.
## N/A Criteria
N/A only when no UI-impacting behavior change occurred AND no cross-REQ module sweep is required.
## Stop Conditions
Stop when `flows.md` fingerprint drifts from the read-back, when frontend is unreachable, when `data-test` hooks are absent blocking deterministic selection, or when a flow step contradicts the actual UI (surface as DV finding, do not work around).
## Non-Goals
Do not use Cypress or other frameworks when the project standard is Playwright. Do not bind selectors to CSS classes or copy text (use `data-test`). Do not skip the cross-REQ regression sweep. Do not mask CDP-captured errors to force a green.

## Technology Boundary

Apply `playwright-e2e` together with this Skill whenever Playwright specs,
fixtures, locators, traces, or execution failures are in scope. It does not
decide which routes S7 must run, whether evidence is sufficient, or the
clean-round outcome; those decisions remain here and in the assigned S7
responsibility.

## Inlined Methodology

Team planning uses a mandatory responsibility baseline plus risk-triggered responsibilities plus scope partitioning plus conflict-graph constraints yielding one single-responsibility assignment per teammate. The `QA-E2E` responsibility maps to this skill. Risk-tag derivation: UI-impacting change or `flows.md` modified -> `ui`, `behavior-change`; existing module touched -> `regression`. Conflict-graph `must_separate` edges when responsibilities require different conclusion enums, one responsibility checks the other's authored or repaired output, or combining them would exceed workload limits. One assignment may not exceed 30 files or 3 material modules. Teammate reuse requires same role family and responsibility ID, unchanged scope, matching fingerprints, and non-stale teammate state.

## Core Principle

E2E tests are the **executable transcription of `flows.md`**. The flow is the source of truth; the spec is its compiled, runnable form. Therefore:

- **One spec file per flow family** (or per module sweep round) — not per ad-hoc scenario.
- **Hybrid Playwright + CDP**: Playwright drives user-journey actions; CDP captures low-level events (network, console, runtime exceptions) on the same Chrome instance for forensic evidence.
- **Deterministic selectors via `data-test`** — never bind to CSS classes, copy text, or DOM structure that visual redesigns would break.
- **Evidence is idempotent**: anyone can rerun the spec and get the same shape of evidence (JSONL + PNG + status counts).
- **Cross-REQ regression is mandatory**: when a REQ touches a module, run every `F-NNN` in that module's `flows.md`, not just the new flow.

## Flow-to-Spec Translation

Each flow `F-NNN` in `<module>/flows.md` becomes a `test.describe` block. Each numbered step becomes a `test()` step. Exception branches become separate `test()` cases under the same describe.

### Mapping table

| flows.md element | Playwright spec element |
|---|---|
| `## F-001 KYC 审核员处理 P0 case` | `test.describe('F-001 KYC 审核员处理 P0 case', () => { ... })` |
| `**REQ-id**: REQ-031` | recorded in the narrative report header so regression sweep can locate the source REQ |
| `**触发**: 审核员登录后看到 P0 case 标记` | `test.beforeEach` login + navigate |
| `**角色**: kyc-reviewer` | auth fixture (per-persona token) |
| `**前置**: ≥1 P0 case` | seed step via API before the test, not via UI |
| `**PATH-1** 从工作台点击“待办案例” ...` | `test('F-001 PATH-1 - 从工作台打开 case 列表', async ({ page }) => { ... })` |
| `→ 看到 case 列表, P0 行高亮 → investor-list.html` | `await page.getByTestId('nav-pending-cases').click(); await expect(page.getByTestId('case-row').filter({ has: page.locator('[data-priority="p0"]') }).first()).toBeVisible()` |
| `**异常分支**: case 被他人抢先认领` | `test('F-001 PATH-x 异常 - case 已被认领', async ({ page }) => { ... })` |
| `**E2E 转录**: data-test hooks` | enumerate the `data-test` IDs used in the spec for cross-check |
| module interaction-coverage map entry | controls without a `PATH-*` step surface as a coverage finding |

### Selector strategy

```typescript
// GOOD — data-test hooks, robust to visual redesign
await page.click('[data-test="decision-submit"]')
await expect(page.locator('[data-test="case-row"]').first()).toBeVisible()

// BAD — breaks on copy changes, refactor, theme swap
await page.click('button:has-text("提交决策")')           // copy-bound
await page.click('.el-button--primary')                    // CSS-framework-bound
await page.click('div.card > div.footer > button')         // structure-bound
```

If `data-test` is missing on an element the flow needs, **stop and surface as a frontend-engineering finding** — do not fall back to brittle selectors. Add the `data-test` requirement to the FE contract before the spec can pass.

## Hybrid CDP Wiring

CDP gives forensic visibility that Playwright alone lacks: every network response, every console message, every uncaught exception, in real time on the same browser instance Playwright drives.

### Standard wiring (copy-paste template)

```typescript
import { test, type Page, type CDPSession } from '@playwright/test'

async function wireCDP(page: Page): Promise<CDPSession> {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Network.enable')
  await cdp.send('Page.enable')
  await cdp.send('Runtime.enable')

  cdp.on('Network.responseReceived', (e) =>
    rec('cdp', 'Network.responseReceived', {
      requestId: e.requestId,
      url: e.response.url,
      status: e.response.status,
      mimeType: e.response.mimeType,
    }),
  )
  cdp.on('Network.loadingFailed', (e) =>
    rec('cdp', 'Network.loadingFailed', { requestId: e.requestId, errorText: e.errorText }),
  )
  cdp.on('Page.frameNavigated', (e) =>
    rec('cdp', 'Page.frameNavigated', { url: e.frame.url }),
  )
  cdp.on('Runtime.consoleAPICalled', (e) =>
    rec('cdp', 'Runtime.consoleAPICalled', { type: e.type, args: e.args.map(a => a.value) }),
  )
  cdp.on('Runtime.exceptionThrown', (e) =>
    rec('cdp', 'Runtime.exceptionThrown', {
      text: e.exceptionDetails.text,
      url: e.exceptionDetails.url,
      line: e.exceptionDetails.lineNumber,
    }),
  )
  return cdp
}

// Usage inside a test:
test('F-001 step 1', async ({ page }) => {
  const cdp = await wireCDP(page)
  await page.goto(WEB_BASE) // application entry only, never a deep business route
  await page.getByTestId('nav-pending-cases').click()
  await expect(page.getByTestId('case-list')).toBeVisible()
  // CDP events stream into evtStream[] for later evidence dump
})
```

### When to drop into CDP (versus pure Playwright)

| Need | Tool |
|---|---|
| Click, type, navigate, assert visible | Playwright (higher-level) |
| Capture network response status / headers / timing | CDP `Network.responseReceived` |
| Catch silent failures (4xx/5xx that the UI swallows) | CDP `Network.loadingFailed` |
| Catch unhandled console errors during a flow | CDP `Runtime.consoleAPICalled` + `Runtime.exceptionThrown` |
| Capture redirect chains / SPA route transitions | CDP `Page.frameNavigated` |
| Performance trace, coverage, DOM snapshot | CDP `Performance.startTracing` / `DOM.captureSnapshot` |

Rule of thumb: if Playwright's `expect()` can prove it, use Playwright. If you need forensic evidence of *what the browser did*, use CDP.

## Evidence Bundle

Every e2e spec run produces a bundle that a Verifier walks to sign off the round. Shape:

```text
docs/reports/e2e/{AGENT-{ROLE}-{FEATURE}-R{N}}.md   ← narrative report
docs/reports/e2e/evidence/{RUN_ID}.jsonl             ← CDP + PW event stream
docs/reports/e2e/screenshots/{RUN_ID}-{step}.png     ← per-step screenshot
```

### Narrative report sections (mandatory)

1. **Header** — REQ ID + round + bound runtime revision + spec file path
2. **Stack-state table** — containers / ports / running versions (proves the environment)
3. **Test-architecture diagram** — agent assignments + journey sequence (one box per `test()`)
4. **API CRUD walk table** — `# · Method · Path · Status · Notes` with real HTTP codes observed (not expected — observed)
5. **CDP findings section** — bugs caught only at browser level (silent 4xx, uncaught exceptions, console errors). Each finding cites the JSONL line.
6. **Evidence inventory** — paths to JSONL + PNG + (optional) video
7. **Status-code distribution counts** — sanity check on coverage (e.g. `2xx: 47, 3xx: 3, 4xx: 2 (expected), 5xx: 0`)
8. **BUG drafts surfaced** — pre-formatted BUG report bodies (see §Bug Surfacing)
9. **Idempotent re-execution instructions** — env vars, start commands, exact `pnpm playwright test` invocation

### JSONL event stream shape

One JSON object per line:

```json
{"ts":"2026-07-09T08:01:23.456Z","src":"cdp","kind":"Network.responseReceived","payload":{"requestId":"A1","url":"http://.../funds","status":200}}
{"ts":"2026-07-09T08:01:23.789Z","src":"pw","kind":"login.click","payload":{"ok":true}}
```

`src` is `"cdp"` or `"pw"` so a reviewer can filter by source. `kind` names the event type. `ts` is ISO-8601 UTC.

## Cross-REQ Module-Flow Regression Sweep

When a REQ touches `docs/design/prototypes/<module>/`, **every** flow in `<module>/flows.md` must be exercised, not only the new one. This is the regression sweep mandated by `docs/rules/ui-prototype.md §8`.

### Sweep mechanics

1. Enumerate flow IDs: `grep -E '^## F-[0-9]+' docs/design/prototypes/<module>/flows.md`
2. For each `F-NNN`, ensure a `test.describe('F-NNN ...')` block exists in some spec file under `web/e2e/`.
3. If the flow's spec is missing (predates this rule), add a stub that fails loudly: `test('F-NNN regression — spec missing', () => { throw new Error('regression spec not yet transcribed') })`. This forces transcription rather than silent skip.
4. Run the full sweep: `pnpm playwright test web/e2e/ --grep 'F-'`
5. Capture pass/fail counts per flow in the narrative report's §status-code distribution equivalent.

### Why mandatory

A module's flow set is shared across REQs. REQ-A adds a tab; REQ-B refactors data loading. If REQ-B only tests its own flow, REQ-A's flow may regress silently. The sweep prevents this by treating the entire flow set as the regression contract.

## Auth Strategy

Document the token strategy in the narrative report up front:

- **localStorage key**: e.g. `localStorage 'token'`
- **Request header**: e.g. `x-token: <jwt>`
- **Persona fixtures**: one auth fixture per persona in `stories.md` (e.g. `kyc-reviewer`, `fund-admin`)
- **Seed tokens**: per-persona JWTs minted via a setup script (NOT shared across tests — pollutes state)

Anti-patterns:
- Hardcoded `currentUserId=1` in test setup
- Single admin token used for every persona (defeats permission testing)
- Tokens minted via UI login for every test (slow; use API setup)

## Bug Surfacing from Failures

When a spec fails, the e2e-tester produces a pre-formatted BUG draft inside the narrative report (§BUG drafts surfaced). Shape matches `docs/reports/bugs/BUG-NNN.md` so filing is a copy-paste:

```markdown
### BUG draft: {short title}
- 严重度: P0 / P1 / P2
- 关联 flow: F-NNN
- 关联 step: 步骤 N
- 关联 prototype: {file}.html
- 现象: {what happened, with screenshot ref}
- 期望: {what flows.md said should happen}
- 复现: {exact `pnpm playwright test --grep` command}
- CDP evidence: {JSONL path + line number}
- 关联代码: {file:line if CDP stack trace identified it}
- 建议根因: {hypothesis, marked as such — do not assert without code review}
```

Severity rules:
- **P0** — flow completely blocked, no workaround; or data corruption; or security hole
- **P1** — flow partially blocked or wrong behavior with workaround
- **P2** — cosmetic, perf, or edge-case-only

The e2e-tester does NOT file the BUG; that's the Orchestrator's job after reviewing the draft. The tester produces the draft.

## Idempotent Re-Execution

The evidence bundle must be reproducible. Include in the narrative report:

```bash
# Environment
export WEB_BASE_URL=http://127.0.0.1:58080
docker compose up -d   # if backend services needed

# Seed (per-persona tokens + test data)
pnpm run seed:test

# Run
pnpm playwright test web/e2e/REQ-{id}-round{N}-cdp-{feature}.spec.ts

# Evidence lands at
ls docs/reports/e2e/evidence/*.jsonl
ls docs/reports/e2e/screenshots/*.png
```

Anyone running these commands should see the same flow pass/fail pattern and produce the same shape of evidence (timestamps differ, but counts and verdicts match).

## Quality Bar (QA Reviewer checklist)

An e2e evidence bundle clears S8 when ALL hold:

- [ ] Every `F-NNN` in the affected module's `flows.md` has a corresponding `test.describe` block (regression sweep complete)
- [ ] Selectors use `data-test` exclusively; no CSS-class or copy-text binding
- [ ] Hybrid CDP wiring captures Network + Page + Runtime events for every test
- [ ] Narrative report has all 9 mandatory sections with real observed data (not expected)
- [ ] JSONL event stream lands at the documented path; one JSON object per line; sortable by `ts`
- [ ] Screenshots at every step that asserts visibility
- [ ] Auth strategy documented; per-persona fixtures; no shared admin token; no hardcoded user IDs
- [ ] BUG drafts (if any) follow the canonical shape with severity + flow ref + repro command + CDP evidence ref
- [ ] Idempotent re-execution instructions produce the same pass/fail pattern
- [ ] Cross-REQ regression sweep results enumerated in the report (every `F-NNN` listed with PASS/FAIL)

Failing any item is a QA finding; the bundle returns to S6 / S9 rework. No Clean Round without a passing e2e bundle.
