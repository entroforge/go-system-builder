---
name: e2e-browser-testing
description: Use when translating a module's current flows and scenario cases into Playwright + CDP browser tests, running full-module regression sweeps, or reviewing e2e evidence
---
# E2E Browser Testing
## Authority
Tests prove flow behavior in a real browser; they do not redefine the design. This reusable method was distilled from prior project E2E work without retaining instance-artifact references. The current ReviewPlan owns assigned coverage and the original S7 consumer owns round qualification. This Skill explains flow-to-spec methods and evidence quality; `playwright-e2e` owns Playwright test-code, fixture, locator, and debugging practice. The flow-to-spec translation, hybrid CDP wiring, and evidence-bundle contracts below are the canonical shape a QA Reviewer checks during S7.
## Applicability
Apply to the browser portion of `behavior-change`, QA-E2E and cross-REQ module-flow regression responsibilities. An E2E lens may instead verify API, command or state-based behavior: use its assigned contract and actual request/command/state evidence, with `testing-strategy` and applicable `integration-verification`. Browser URLs, HTML, selectors, CDP and Playwright assets are prerequisites only for browser work.
## Required Inputs
For assigned browser work, read the module's complete current package: `scenario-model.json`, `cases.json`,
`scenario-coverage.json`, `fixture-contract.json`, `flows.md`, `stories.md`, `*.html`,
and `index.html`. REQs appear only in `source_refs`; module definitions are not copied per
REQ or round. Also read existing specs under `web/e2e/{module}/`, the locked SYNC contract
(for wire-shape parity assertions), the running frontend URL, and the auth strategy.
Main owns full-round module and affected-surface coverage. Each Reviewer executes the exact assigned Claims/CASEs and reports a missing surface to Main; complete inventory checks do not assign every CASE to every Reviewer. S7 reads locked scenario/flow sources; it does not regenerate or rewrite them.

## Scenario Inventory and Path Contract

- Treat `cases.json` as the sole inventory of current scenario cases. Main enumerates
  every required browser CASE for the round; the Reviewer selects the assigned CASEs with their branch mapping,
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

Map every assigned browser-required CASE and its branch mapping from `cases.json` to
the actual selected Playwright spec, whether an existing regression asset or a
cold-start script in the declared verification workspace. Each spec binding must carry the exact CASE ID,
and every referenced `PATH-*` ID in the same actual test callback body. Retain
the separate `F-NNN` ref in `cases.json` only for structural membership
validation against `flows.md`; it is not part of the callback-binding gate.
Cover positive paths and assert every negative oracle's `visible`,
`terminal_state`, `persisted_effects`,
`forbidden_side_effects`, `rejection`, `expected_state`, and `recovery`;
represent the no-recovery value exactly as `recovery: "N/A"` and require its
source refs and reason. A passing expected rejection is recorded in `ClaimResult.observed`
and real evidence references; Finding encounter fields are used only for a failed
Claim. Capture CDP events for network / console / runtime
exceptions and produce idempotent re-runnable evidence. Required allow/reject
branch coverage must be 100%.
## Outputs
For `e2e_coverage_state=cold_start`, Playwright spec/fixture files belong only under the
ReviewPlan's `verification_artifact_workspace/`, a repository-relative surface under
`e2e-workspace/` whose planning and preparation belong to Main; existing regression specs under
`web/e2e/{module}/` are read/run-only during S7. Put the evidence bundle under
`.claude/evidence/<runtime>/g<generation>/reviews/<agent>/` (the report projection may be
under `docs/reports/`, but never under `docs/reports/bugs/`). Every failure becomes a
structured ReviewResult Finding, not a BUG draft; S8 owns root-cause and canonical BUG
mapping. Evidence reports may be review-round scoped; product test assets may not be copied
into a round-named location.
For cold-start work, include the actual scripts/fixtures used from the declared
workspace in the original Claim/check `evidence_refs` as `path:<repository-relative file>`;
the original consumer supplies hashes. Output logs, screenshots and command_output
show what happened, but do not identify those asset dependencies. Cite only real
dependencies, not every other Reviewer's workspace file. Existing selected reuse
assets keep their original Plan bindings. Unknown dependencies after workspace
change are a Main recovery gap, not proof that the accepted result is still valid.
## N/A Criteria
N/A only when no UI-impacting behavior change occurred AND no cross-REQ module sweep is required.
## Stop Conditions
Return the affected Claim and concrete source/environment blocker to Main when frozen input drift, an unreachable frontend or missing required selectors prevents a conclusion. Stop the affected checks and continue other safe assigned coverage. An observed flow/spec mismatch becomes a Finding; continue the remaining safe assigned checks without silently rewriting the specification. Real P0 stops dangerous work and retains unobserved coverage as described below.
## Non-Goals
Do not use Cypress or other frameworks when the project standard is Playwright. Do not bind selectors to CSS classes or copy text (use `data-test`). Do not skip the cross-REQ regression sweep. Do not mask CDP-captured errors to force a green.

## Technology Boundary

Apply `playwright-e2e` together with this Skill whenever Playwright specs,
fixtures, locators, traces, or execution failures are in scope. It does not
decide which routes S7 must run, whether evidence is sufficient, or the
clean-round outcome. The ReviewPlan defines assigned coverage, this Skill guides
evidence assessment, and the original domain consumer and exit gate decide the round.

## Responsibility boundary

Use the assigned CASE/PATH set and its current module source package. Main chooses
compatible grouping and independent execution under the original ReviewPlan;
reading this Skill does not create another dispatch, file-count or tool-permission
gate. Preserve browser coverage and real independence when work is partitioned.

## Core Principle

Main 提供原 `s7-review-result-v1` 题目与准确私人答案位置。读取真实材料，完成本人的独立浏览器验证，只填写专业 `answer`，保存本人原答卷；PreToolUse 高频监听就绪原答卷，成功 PostToolUse 与 Stop／SubagentStop 兜底调用原消费者，不增加提交手续，不等待收尾消息或返回引用；完成后向 Main 汇报真实结果。新原生责任不增加通用 PLAN／激活或保存回执前置。保留机器预填与 `verification_artifact_digest=null`；消费者从实际执行来源固定并校验摘要，作者不计算或抄写。浏览器责任仍须完成本人分配 Claims/CASE/PATH 的真实执行、证据来源和专业结论；完整当前模块覆盖由 Main 在整轮计划与结果中核对。

E2E tests are the **executable transcription of current module `flows.md` and CASE oracle**.
The current module package is the source of truth; the spec is its compiled, runnable form.
Therefore:

- **Existing permanent specs live under the module path** and bind `CASE ID + PATH ID`.
  S7 reads/runs those assets there; permitted cold-start scripts live in the declared
  verification workspace. Neither branch creates a REQ-owned or duplicate scenario
  specification to replace current module truth.
- **Exact callback binding is mandatory**: only exact CASE and PATH tokens inside
  the actual `test()` callback body count. IDs present only in a test title,
  `test.describe` label, comment, fixture, or another callback do not satisfy
  the binding gate.
- **Hybrid Playwright + CDP**: Playwright drives user-journey actions; CDP captures low-level events (network, console, runtime exceptions) on the same Chrome instance for forensic evidence.
- **Deterministic selectors via `data-test`** — never bind to CSS classes, copy text, or DOM structure that visual redesigns would break.
- **Evidence is idempotent**: anyone can rerun the spec and get the same shape of evidence (JSONL + PNG + status counts).
- **Cross-REQ regression is mandatory**: Main plans every required browser CASE
  and referenced PATH in affected modules across the round. Each Reviewer runs
  its assigned coverage, validating the separate F-NNN membership against flows.md;
  the full round must not shrink to only the newly changed behavior.

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
| `source_refs: [REQ-031/FR-003]` | retained in the original answer or referenced case-mapping evidence as origin, never as spec ownership |
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

If `data-test` is missing on an element the assigned flow needs, stop that affected check and surface the observed gap with its evidence; continue other safe assigned checks without brittle-selector workarounds. During implementation, add the required `data-test` hook through the assigned FE contract work. During read-only S7 review, report the observed missing hook and affected check to Main; do not edit the locked FE contract or product to obtain PASS.

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

Each run retains actual browser and test evidence in the locations supplied by the
current responsibility. Reference those files from the original S7 answer; an
existing narrative report may be reused, but a second report is not a prerequisite
for reception. Do not invent a parallel round-named report or copy Runtime revision
and source hashes into it.

### Information to retain

Keep these facts legible in the answer and its referenced evidence, where applicable:

- The actual spec and CASE/PATH coverage, execution command and relevant environment.
- The frontend/API instance and build actually exercised; real versus mocked dependencies.
- User actions, observed request outcomes, console/runtime events and terminal state.
- Evidence paths for actual JSONL, screenshots, trace or video; reuse existing capture.
- Findings with the first deviation, persistence/forbidden effects and genuine capture gaps.
- Safe setup, test-data/fixture and rerun instructions sufficient to continue the original work.

A sequence diagram or status-code summary can help explain a complex run, but neither
is a mandatory second deliverable or a substitute for actual observation. Counts and
machine metadata come from existing outputs; the author explains their significance.

### Bind dynamic observations to the service actually running

In the existing answer or referenced execution evidence, identify the frontend/API URL and the build actually serving it, plus which database, queue and external dependencies are real or mocked. Reuse an existing build ID, version endpoint, container image identity, or controlled startup/build log that connects the running instance to the claimed product revision. A checkout's current HEAD, a spec digest or a successful HTTP response alone does not identify the running service. Read the existing evidence; do not add a mandatory endpoint, probe command or second provenance registry.

If a run targets an older build, mark its result against that build; do not use it as proof of the new change. If the mapping cannot be established, state the evidence limitation and return the environment/source gap to Main before claiming the current behavior verified. Keep observations already obtained and any original receipt; do not silently relabel them or change hashes. After the intended service is available, repeat only the affected dynamic observations through their original responsibility.

Mocked dependencies can support the boundary they actually exercise, but do not prove the omitted real integration. Distinguish real browser/server/persistence observations from fixtures, local stubs and static source checks in the original answer or referenced evidence. This runtime-version check applies to browser/API/service execution; static review or schema checks do not require a service launch, network probe or health endpoint.

### JSONL event stream shape

One JSON object per line:

```json
{"ts":"2026-07-09T08:01:23.456Z","src":"cdp","kind":"Network.responseReceived","payload":{"requestId":"A1","url":"http://.../funds","status":200}}
{"ts":"2026-07-09T08:01:23.789Z","src":"pw","kind":"login.click","payload":{"ok":true}}
```

`src` is `"cdp"` or `"pw"` so a reviewer can filter by source. `kind` names the event type. `ts` is ISO-8601 UTC.

A complete witness → journey → persistence/forbidden-effects example, including the original RepairContract mapping, is in [S8/S9 examples](../../docs/examples/s7-s9/README.md#完整业务示例撤回后保留期间合法修订). Use the product's accepted rules rather than imposing the example's business policy.

## Full-Module Regression Sweep

When a REQ touches `docs/design/prototypes/<module>/`, Main must arrange execution
of **every** required browser CASE and its mapped branch/path journey from
`cases.json` across the complete round, not only newly changed behavior. A Reviewer
executes only the assigned Claim/CASE slice; a full sweep applies to that Reviewer
only when the original Assignment gives it that scope. Validate and execute each
referenced path against `flows.md`, and use `scenario-coverage.json` only as the
aggregate gate for counts, polarity ratio, and required-branch coverage. This is
the regression sweep mandated by `docs/rules/ui-prototype.md §8`.

### Sweep mechanics

1. Read Main's assigned slice of the required browser CASE inventory, branch mappings, and separate
   `F-NNN` and `PATH-*` refs from `docs/design/prototypes/<module>/cases.json`.
2. Use each separate `F-NNN` ref to validate structural membership against
   `flows.md`, including that every referenced `PATH-*` belongs to that flow;
   then execute each path's numbered actions. Do not derive the CASE inventory
   from `flows.md` or `scenario-coverage.json`.
3. Check `scenario-coverage.json` as an aggregate gate only: required allow/reject
   coverage and the configured polarity ratio must pass.
4. For each assigned CASE, verify the actual selected spec binds its exact CASE
   and every full PATH ID in one callback; F-NNN need not occur in the callback.
   Existing `web/e2e/<module>/` assets remain read/run-only. For `cold_start`,
   implement missing scripts/fixtures in `verification_artifact_workspace`,
   then run those real files alongside any selected reusable assets.
5. A missing required existing asset is a concrete gap; do not silently skip it
   or create a placeholder. A declared cold-start missing script is authored in
   its permitted workspace before execution, without editing product or contracts.
6. Use the actual selected paths and configured Playwright runner. For an assigned
   complete existing module sweep, for example: `pnpm playwright test web/e2e/<module>/ --grep 'CASE-|PATH-'`.
   For cold start, pass the real `<verification_artifact_workspace>/<spec>.spec.ts`
   and required project/config options; ensure the runner discovers that directory.
   A sliced Assignment uses its actual CASE selection, not an unassigned module sweep.
7. Record actual assigned coverage, command and per-CASE results in the original answer/evidence; Main assesses aggregate coverage.

### Why mandatory

A module's flow set is shared across REQs. REQ-A adds a tab; REQ-B refactors data loading. If REQ-B only tests its own flow, REQ-A's flow may regress silently. The sweep prevents this by treating the entire flow set as the regression contract.

## Auth Strategy

For assigned authenticated browser behavior, explain the actual token strategy
in the original answer or its referenced existing evidence:

- **localStorage key**: e.g. `localStorage 'token'`
- **Request header**: e.g. `x-token: <jwt>`
- **Persona fixtures**: one auth fixture per persona in `stories.md` (e.g. `kyc-reviewer`, `fund-admin`)
- **Seed tokens**: per-persona JWTs minted via a setup script (NOT shared across tests — pollutes state)

Anti-patterns:
- Hardcoded `currentUserId=1` in test setup
- Single admin token used for every persona (defeats permission testing)
- Tokens minted via UI login for every test (slow; use API setup)

## Finding Handoff from Failures

脚本失败时，在原答卷中记录可以交给调查者的 Finding：实际入口、首次偏差、终态和证据。S8 负责归并、因果判断及后续 BUG 投射，S7 不手写 BUG 文件。实际经过可以直接写入 timeline；已有 `capture step` 缓冲只补无作者时间线且关联准确的 Finding，原 `capture exec` 保留真正命令执行与输出来源。证据作者选择实际 `path:<相对路径>` 或 `command_output:<原捕获路径>`，摘要、字节数和截断元数据由原接收／捕获机制承接；不得让作者手填，也不得从文件存在推断命令执行。冷启动资产的摘要由原消费者从本人实际工作区绑定，执行后改动脚本或夹具仍须重做受影响流程。P0 Finding 如实写 `capture_gaps`，说明不能安全采集的内容和原因。同一 Reviewer 尚未执行的 Claim 可从答卷的空白项中移除，原消费者在接收真实 P0 后从计划推导未观察集合；不补造 pass/fail 或产品阻塞。以下清单帮助整理原答卷的事实，不要求另交 Markdown 报告：

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

Retain safe rerun instructions in the original answer or referenced evidence. Adapt
this example to the actual supplied environment and artifact paths; it is not an
instruction to start unassigned services or rerun a dangerous P0 operation:

```bash
# Environment
export WEB_BASE_URL=http://127.0.0.1:58080
docker compose up -d   # if backend services needed

# Seed (per-persona tokens + test data)
pnpm run seed:test

# Run
pnpm playwright test <actual-selected-spec-paths> --grep <assigned-CASE-pattern>

# Evidence lands at
ls <assigned-evidence-directory>
```

State the actual repeatability observed. Nondeterminism, changed state and unsafe
re-execution remain explicit limits; do not promise identical counts or outcomes
without evidence or repeat unsafe work merely to fill the record.

## Quality Bar (QA Reviewer checklist)

Assess the assigned E2E evidence against the applicable criteria below. The original
S7 consumer and exit gate decide whole-round qualification; an E2E bundle alone
cannot clear delivery and QA obligations or authorize repair:

- [ ] Every assigned browser-required CASE and every `PATH-*` ref from `cases.json` is bound in one actual callback; each separate `F-NNN` ref validates structural flow membership against `flows.md` but is not required in the callback (assigned slice complete; Main checks the full-module aggregate)
- [ ] Assigned required allow and reject branches have 100% execution evidence; Main checks the complete module aggregate and configured ratio
- [ ] Selectors use `data-test` exclusively; no CSS-class or copy-text binding
- [ ] Hybrid CDP wiring captures Network + Page + Runtime events for every test
- [ ] The original answer and referenced evidence contain actual environment, coverage, observations and safe continuation instructions; no duplicate narrative is required
- [ ] JSONL event stream lands at the documented path; one JSON object per line; sortable by `ts`
- [ ] Screenshots at every step that asserts visibility
- [ ] Auth strategy documented; per-persona fixtures; no shared admin token; no hardcoded user IDs
- [ ] Every failure is an investigation-ready Finding with operation path + evidence refs; no root-cause or BUG draft is asserted in S7
- [ ] Safe re-execution instructions and the actually observed repeatability or limits are recorded
- [ ] Assigned cross-REQ regression coverage is accounted for in the answer/evidence; unexecuted Claims are not relabeled PASS/FAIL

Report an observed product deviation as a Finding; missing environment or evidence
requires its actual recovery owner. Ordinary Findings continue safe coverage and
reach S8 through a sealed batch. Repair requires the original investigation and
approval route. Valid P0 early termination retains unobserved Claims and gaps; it
is not a clean round, and none of these criteria requires unsafe recapture.

## Selecting existing assets

Read the original ReviewPlan and its adjacent planning guidance. A script mentioning a CASE is only a candidate. Main selects the exact `cases.json#CASE` and script path with actual selector, route and environment facts; the original receiver fills omitted asset IDs and digests. Do not copy machine hashes or infer a usable browser environment from a module label. Unresolved planning remains a draft. Cold-start may reuse confirmed assets and author only missing scripts in its declared workspace; every selected reusable asset retains its original fingerprint checks.
