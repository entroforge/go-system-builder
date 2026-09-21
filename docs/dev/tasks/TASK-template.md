# Task: TASK-{id}

> Status: draft
> Reading policy: linked-v1
> Dispatch policy: waves-v1
> （draft=still writing；complete=the document is finished——required across the whole batch at TR-002, says nothing about implementation（那是 S6 的事）；cancelled=out of the batch, its §3 clause declarations drop out of coverage so any gap resurfaces）
> Version: v1.0.0
> Source REQ refs: REQ-{id} / none
> Module current truth: `docs/design/prototypes/{module}/` / N/A
> Primary contract: {FE/BE/SYNC-id}
> Closing Contract: TASK-{id}#closing-contract
> Runtime ref: `{runtime-id}`
> Execution: runtime owns assignment, owner, completion and integration; do not fill them into this frozen TASK.

## 1. Objective

{One testable objective and user-visible value.}

<!-- 一句话测试：能用一句话说出本任务的交付物吗？说不成一句、或出现"以及/然后"——回去拆。
builder 应能在单个上下文区间内完成本任务（中途 compact 丢任务信息是灾难性表现；按 §2/§4 的量感觉会撞，拆小或裁清单）。拆分纪律全文见 specification-planning step 11。 -->

## 2. Document Manifest

Read order for the builder. Fingerprints, versions, and lock state live in
runtime documents[] (.claude/loop-state.json) — this table never hand-copies
them.

| Order | Kind | ID | Path | Clauses | Purpose | Mode |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | contract | {contract-id} | [Local responsibility](../contracts/{contract-id}.md#{stable-anchor}) | §{n} | Understand this delivery | required |
| 2 | sync | SYNC-{id} | [Operation and recovery](../contracts/SYNC-{id}.md#{stable-anchor}) | §{n} | Understand shared behavior | required |
| 3 | model | {native-definition} | [Shared model entry](../../architecture/data-model/MODEL-{id}.md) | {definition} | Locate the exact data source | required |

Remove inapplicable rows; keep the smallest complete ordered reading set. Use
`conditional` only with an explicit condition in Purpose, and `optional` for
background. Follow [shared-model reading rules](../../rules/shared-model-contracts.md).
Do not add a foundation dependency unless another task actually produces an
input this implementation needs. Returning to this TASK closes the reading path.

Repair assignments prepend the canonical BUG as order 1 and shift the remaining
documents. The request remains the authority for exact order.

## 3. Delivered Clauses

Which clauses of the primary contract this TASK delivers. The CONTRACTS index
is the clause universe; `tasks check` aggregates these declarations against it.

| Contract | Delivered clauses |
|:---|:---|
| {FE/BE/SYNC-id} | §{n}, §{n} |

An empty clause list means a support TASK — legitimate, but excluded from
coverage aggregation.

### 3.1 Module Impact

Modules touched: `{module}` / N/A. Scenario truth lives in the module package
(`docs/design/prototypes/{module}/`) — never create a per-REQ or per-round
scenario/spec copy. Any change to a current-module truth file triggers a full
module regression sweep.

## 4. Scope

| Type | Paths / Commands |
|:---|:---|
| read paths | `{path}` |
| prospective write paths | `{path}` |
| forbidden paths | `.claude/loop-state.json`, `{path}` |
| allowed command classes | test / lint / build / read-only |
| output paths | `{implementation/test/report paths}` |

Dynamic permission is the intersection of Agent Definition, manifest, this
scope, activation, runtime state, and Hook policy.

## 5. Selected Skills

| Skill | Category | Source | Version | Applicability |
|:---|:---|:---|:---|:---|
| agent-dispatch | methodology | `.claude/skills/agent-dispatch/SKILL.md` | 1.0.0 | teammate dispatch (plan_checkpoint) |
| {skill} | best-practice | `.claude/skills/{skill}/SKILL.md` | {version} | {risk/responsibility} |

## 6. Outputs And Evidence

| Output | Path | Acceptance |
|:---|:---|:---|
| implementation | `{path}` | {contract assertion} |
| tests | `{path}` | {behavior/failure coverage} |
| completion report | `{agent-message-path}` | schema-valid |
| delivery evidence | `docs/reports/review/REV-{id}.md` | assigned dimension result |
| QA evidence | `docs/reports/qa/QA-{id}.md` | assigned dimension result |
| E2E evidence | `docs/reports/e2e/E2E-{id}.md` | real-browser flow result |

## 7. Closing Contract

```text
assert {contract clause} == satisfied
assert {verification command} == pass
assert changed_paths subset_of activated_write_paths
assert scope_deviations == []
```

## 8. Dependencies

| Dependency | Required artifact / verification |
|:---|:---|
| TASK-{id} | {committed artifact integrated and verified on the bound development branch} |

依赖列只认 `TASK-*` 引用——只有 TASK 引用进入 DAG（`tasks check` 的环检测与拓扑）。assignment 级依赖不被机检追踪，需要跨任务顺序时写成 TASK 依赖或在收尾契约中声明。

## 9. Execution entry

Read the REQ's [overall plan](index-REQ-{id}.md) and
[dispatch rules](../../rules/dispatch-plan.md). Runtime owns lifecycle evidence.

### Resources

Declare only mutable external resources requiring exclusive use (not read-only
contracts/models). Use the same stable name across tasks; leave empty if none.

| Resource | Purpose |
|:---|:---|

## 10. Findings And Repairs

Findings, canonical BUGs, repair assignments and re-verification are runtime
records. Follow the BUG procedure and the execution entry; do not append
execution history to this frozen task document.

## 11. Document History

| Date | Version | Design change |
|:---|:---|:---|
| | | |
