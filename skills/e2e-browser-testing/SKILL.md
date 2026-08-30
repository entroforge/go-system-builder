---
name: e2e-browser-testing
description: Use when translating a module's current flows and scenario cases into Playwright + CDP browser tests, running full-module regression sweeps, or reviewing e2e evidence
---
# E2E Browser Testing
## Authority
Tests prove flow behavior in a real browser; they do not redefine the design. This reusable method was distilled from prior project E2E work without retaining instance-artifact references. This Skill owns S7 flow-to-spec coverage and evidence-bundle requirements; `playwright-e2e` owns Playwright test-code, fixture, locator, and debugging practice. The flow-to-spec translation, hybrid CDP wiring, and evidence-bundle contracts below are the canonical shape a QA Reviewer checks during S7.
## Applicability
Apply to `behavior-change` risk on UI-impacting REQs, the QA-E2E responsibility, and any cross-REQ module-flow regression sweep.
## Required Inputs
Read the assigned module's complete current package: `scenario-model.json`, `cases.json`,
`scenario-coverage.json`, `fixture-contract.json`, `flows.md`, `stories.md`, `*.html`,
and `index.html`. REQs appear only in `source_refs`; module definitions are not copied per
REQ or round. Also read existing specs under `web/e2e/{module}/`, the locked SYNC contract
(for wire-shape parity assertions), the running frontend URL, and the auth strategy.
## Scenario Inventory and Path Contract

- Treat `cases.json` as the sole inventory of current scenario cases. Enumerate
  every `browser_required: true` CASE together with its branch mapping,
  polarity, oracle, and `flow_refs`.
- Keep the flow and path references separate: a case may reference `F-NNN` and
  one or more `PATH-*` IDs as independent `flow_refs`; never concatenate these
  refs with a slash into one combined `flow_ref`.
- Use each `F-NNN` ref to validate structural flow membership against
  `flows.md`, including that the CASE's referenced `PATH-*` IDs belong to that
  flow. Then execute the referenced paths from their numbered actions, roles,
  preconditions, checkpoints, and recovery behavior. Do not use `flows.md` as
  the CASE inventory, and do not require the `F-NNN` token in a Playwright
  callback.
- Treat `scenario-coverage.json` as an aggregate gate only. Use its counts,
  required-branch coverage, and polarity ratio to verify the gate; never use it
  to enumerate CASEs, branches, or Flow/PATH references.

## Quality Criteria

Map every browser-required CASE and its branch mapping from `cases.json` to a
current module Playwright spec. Each spec binding must carry the exact CASE ID,
and every referenced `PATH-*` ID in the same actual test callback body. Retain
the separate `F-NNN` ref in `cases.json` only for structural membership
validation against `flows.md`; it is not part of the callback-binding gate.
Cover positive paths and assert every negative oracle's `visible`,
`terminal_state`, `persisted_effects`,
`forbidden_side_effects`, `rejection`, `expected_state`, and `recovery`;
represent the no-recovery value exactly as `recovery: "N/A"` and require its
source refs and reason. Capture CDP events for network / console / runtime
exceptions and produce idempotent re-runnable evidence. Required allow/reject
branch coverage must be 100%.
## Outputs
For `e2e_coverage_state=cold_start`, Playwright spec/fixture files belong only under the
ReviewPlan's `verification_artifact_workspace/`; existing regression specs under
`web/e2e/{module}/` are read/run-only during S7. Put the evidence bundle under
`.claude/evidence/<runtime>/g<generation>/reviews/<agent>/` (the report projection may be
under `docs/reports/`, but never under `docs/reports/bugs/`). Every failure becomes a
structured ReviewResult Finding, not a BUG draft; S8 owns root-cause and canonical BUG
mapping. Evidence reports may be review-round scoped; product test assets may not be copied
into a round-named location.
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

E2E tests are the **executable transcription of current module `flows.md` and CASE oracle**.
The current module package is the source of truth; the spec is its compiled, runnable form.
Therefore:

- **Specs live under the module path** and bind `CASE ID + PATH ID`; never create a REQ,
  round, v1/v2, or ad-hoc scenario copy.
- **Exact callback binding is mandatory**: only exact CASE and PATH tokens inside
  the actual `test()` callback body count. IDs present only in a test title,
  `test.describe` label, comment, fixture, or another callback do not satisfy
  the binding gate.
- **Hybrid Playwright + CDP**: Playwright drives user-journey actions; CDP captures low-level events (network, console, runtime exceptions) on the same Chrome instance for forensic evidence.
- **Deterministic selectors via `data-test`** — never bind to CSS classes, copy text, or DOM structure that visual redesigns would break.
- **Evidence is idempotent**: anyone can rerun the spec and get the same shape of evidence (JSONL + PNG + status counts).
- **Cross-REQ regression is mandatory**: when a REQ touches a module, run every
  current browser-required CASE and every separately referenced `PATH-*` journey
  from `cases.json`, after validating each `F-NNN` structural membership against
  `flows.md`; do not limit execution to the newly changed behavior.

## Flow-to-Spec Translation

Use each `F-NNN` from `cases.json` to validate structural flow membership against
`flows.md`; a matching `test.describe` label may group the specs but does not
satisfy the callback-binding gate. Transcribe numbered PATH actions into the
actual `test()` callback. Exception branches and materially distinct
positive/negative/recovery outcomes become separate `test()` cases under the
same module spec. The case inventory still comes from `cases.json`; `flows.md`
supplies the path actions and checkpoints.

### Mapping table

| flows.md element | Playwright spec element |
|---|---|
| `## F-001 KYC 审核员处理 P0 case` | validates the CASE's structural flow membership; an optional `test.describe` label does not count as callback binding |
| `source_refs: [REQ-031/FR-003]` | recorded in the narrative report and case mapping as origin, never as spec ownership |
| `**触发**: 审核员登录后看到 P0 case 标记` | `test.beforeEach` login + navigate |
| `**角色**: kyc-reviewer` | auth fixture (per-persona token) |
| `**前置**: fixture FX-...` | seed step via API before the test, not via UI |
| `**PATH-1** 从工作台点击“待办案例” ...` | the actual callback executes that path and contains exact `CASE-*` and `PATH-1` markers in its body |
| `→ 看到 case 列表, P0 行高亮 → investor-list.html` | Playwright actions and assertions using `data-test` hooks inside that same callback |
| `**异常分支**: case 被他人抢先认领` | a separate callback for the mapped negative CASE, with the exact CASE and every `PATH-*` marker in its body |
| `**E2E 转录**: data-test hooks` | enumerate the `data-test` IDs used in the spec for cross-check |
| module interaction-coverage map entry | controls without a `PATH-*` step surface as a coverage finding |

### Canonical case binding example

Every canonical Playwright example must use the real Playwright import and put
the exact CASE and all PATH IDs for that case inside the same executable
callback. Use an executable `test.step` marker when the journey spans several
paths:

```typescript
import { test, expect } from '@playwright/test'

test.describe('F-001 investor review', () => {
  test('allow branch journey', async ({ page }) => {
    await test.step('CASE-ALLOW-001 PATH-001 PATH-002', async () => {
      await page.getByTestId('nav-pending-cases').click()
      await expect(page.getByTestId('case-list')).toBeVisible()
      // Continue every user action required by PATH-001 and PATH-002 here.
    })
  })
})
```

The marker must be replaced with the exact `case_id` and every `PATH-*` value
from that CASE's `flow_refs`. Keep the separate `F-NNN` ref for structural
membership validation against `flows.md`; the engine does not require it in the
callback body. IDs only in the test title, `test.describe` label, comments, or
a different callback do not count.

### Selector strategy

```typescript
import { test, expect } from '@playwright/test'

test('negative branch journey', async ({ page }) => {
  await test.step('CASE-REJECT-001 PATH-003 PATH-004', async () => {
    // GOOD — data-test hooks, robust to visual redesign
    await page.click('[data-test="decision-submit"]')
    await expect(page.locator('[data-test="case-row"]').first()).toBeVisible()
  })
})

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
import { test, expect, type Page, type CDPSession } from '@playwright/test'

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
test('allow branch browser journey', async ({ page }) => {
  const cdp = await wireCDP(page)
  await test.step('CASE-ALLOW-001 PATH-001 PATH-002', async () => {
    await page.goto(WEB_BASE) // application entry only, never a deep business route
    await page.getByTestId('nav-pending-cases').click()
    await expect(page.getByTestId('case-list')).toBeVisible()
    // Continue every user action required by PATH-001 and PATH-002.
    // CDP events stream into evtStream[] for later evidence dump.
  })
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
8. **Finding records** — operation path, visible symptom, terminal state, persisted and
   forbidden side effects, evidence refs, and capture gaps; no root-cause or BUG claim
9. **Idempotent re-execution instructions** — env vars, start commands, exact `pnpm playwright test` invocation

### JSONL event stream shape

One JSON object per line:

```json
{"ts":"2026-07-09T08:01:23.456Z","src":"cdp","kind":"Network.responseReceived","payload":{"requestId":"A1","url":"http://.../funds","status":200}}
{"ts":"2026-07-09T08:01:23.789Z","src":"pw","kind":"login.click","payload":{"ok":true}}
```

`src` is `"cdp"` or `"pw"` so a reviewer can filter by source. `kind` names the event type. `ts` is ISO-8601 UTC.

## Full-Module Regression Sweep

When a REQ touches `docs/design/prototypes/<module>/`, **every** current
browser-required CASE and its mapped branch/path journey from `cases.json` must
be exercised, not only the newly changed behavior. Validate and execute each
referenced path against `flows.md`, and use `scenario-coverage.json` only as the
aggregate gate for counts, polarity ratio, and required-branch coverage. This is
the regression sweep mandated by `docs/rules/ui-prototype.md §8`.

### Sweep mechanics

1. Enumerate browser-required CASE IDs, branch mappings, and their separate
   `F-NNN` and `PATH-*` refs from `docs/design/prototypes/<module>/cases.json`.
2. Use each separate `F-NNN` ref to validate structural membership against
   `flows.md`, including that every referenced `PATH-*` belongs to that flow;
   then execute each path's numbered actions. Do not derive the CASE inventory
   from `flows.md` or `scenario-coverage.json`.
3. Check `scenario-coverage.json` as an aggregate gate only: required allow/reject
   coverage and the configured polarity ratio must pass.
4. For each CASE, ensure a current module spec under `web/e2e/<module>/` binds the
   exact CASE ID and every full PATH ID in one actual callback. Do not require
   the `F-NNN` token in that callback.
5. If a required spec is missing, fail the sweep loudly; do not create a round-named
   placeholder or silently skip it.
6. Run the full sweep: `pnpm playwright test web/e2e/<module>/ --grep 'CASE-|PATH-'`
7. Capture positive/negative branch and per-flow pass/fail counts in the round report.

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

## Finding Handoff from Failures

When a spec fails, the e2e-tester records an investigation-ready Finding inside the
Canonical ReviewResult. S7 records the observable operation path and evidence only;
S8 owns clustering, causal proof and canonical BUG mapping (the machine creates one
BUG draft per Finding at seal — never hand-write BUG files). Record live observations
with `loop-harness capture step` while you investigate (its output prints the buffer
path; `--captures` merges it into a Finding's empty timeline at submit) and write the
timeline inline only when you author it after the fact. On a cold-start workspace,
bind `verification_artifact_digest` from `loop-harness s7 workspace-digest` after the
last spec/fixture write; a P0 Finding must populate `capture_gaps` (what could not be
recorded and why) or submit rejects it:

```markdown
### Finding record: {short title}
- 严重度: P0 / P1 / P2
- 关联 Claim / flow / step: {ids}
- 操作动线: {exact user action -> request/response or console observation}
- 现象与终态: {visible result + terminal state}
- 持久化/禁止副作用: {observed effects and forbidden effects}
- 证据: {JSONL / screenshot refs}
- 复跑命令: {exact `pnpm playwright test --grep` command}
- 现场缺口: {capture gaps, if any}
```

Severity rules:
- **P0** — flow completely blocked, no workaround; or data corruption; or security hole
- **P1** — flow partially blocked or wrong behavior with workaround
- **P2** — cosmetic, perf, or edge-case-only

The e2e-tester does not file a BUG or assert a root cause. The Orchestrator hands the
sealed ObservationBatch to S8, which may create a canonical BUG only after causal
investigation and an explicit repair contract.

## Idempotent Re-Execution

The evidence bundle must be reproducible. Include in the narrative report:

```bash
# Environment
export WEB_BASE_URL=http://127.0.0.1:58080
docker compose up -d   # if backend services needed

# Seed (per-persona tokens + test data)
pnpm run seed:test

# Run
pnpm playwright test web/e2e/{module}/ --grep 'CASE-|PATH-'

# Evidence lands at
ls docs/reports/e2e/evidence/*.jsonl
ls docs/reports/e2e/screenshots/*.png
```

Anyone running these commands should see the same flow pass/fail pattern and produce the same shape of evidence (timestamps differ, but counts and verdicts match).

## Quality Bar (QA Reviewer checklist)

An e2e evidence bundle clears S7 when ALL hold:

- [ ] Every browser-required CASE and every `PATH-*` ref from `cases.json` is bound in one actual callback; each separate `F-NNN` ref validates structural flow membership against `flows.md` but is not required in the callback (full-module sweep complete)
- [ ] Required allow and reject branches both have 100% execution/coverage evidence, and the module positive/negative ratio passes
- [ ] Selectors use `data-test` exclusively; no CSS-class or copy-text binding
- [ ] Hybrid CDP wiring captures Network + Page + Runtime events for every test
- [ ] Narrative report has all 9 mandatory sections with real observed data (not expected)
- [ ] JSONL event stream lands at the documented path; one JSON object per line; sortable by `ts`
- [ ] Screenshots at every step that asserts visibility
- [ ] Auth strategy documented; per-persona fixtures; no shared admin token; no hardcoded user IDs
- [ ] Every failure is an investigation-ready Finding with operation path + evidence refs; no root-cause or BUG draft is asserted in S7
- [ ] Idempotent re-execution instructions produce the same pass/fail pattern
- [ ] Cross-REQ regression sweep results enumerated in the report (every `F-NNN` listed with PASS/FAIL)

Failing any item is a QA finding; the bundle returns to S6 / S9 rework. No Clean Round without a passing e2e bundle.
