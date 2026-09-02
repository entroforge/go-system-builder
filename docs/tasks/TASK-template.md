# Task: TASK-{id}

> Status: draft
> （draft=still writing；complete=the document is finished——required across the whole batch at TR-002, says nothing about implementation（那是 S6 的事）；cancelled=out of the batch, its §3 clause declarations drop out of coverage so any gap resurfaces）
> Version: v1.0.0
> Source REQ refs: REQ-{id} / none
> Module current truth: `docs/design/prototypes/{module}/` / N/A
> Primary contract: {FE/BE/SYNC-id}
> Closing Contract: TASK-{id}#closing-contract
> Runtime ref: `{runtime-id}`
> Team manifest: `{team-manifest-path}`
> Assignment ID: `{assignment-id}`
> Builder Agent: `{agent-id}`

## 1. Objective

{One testable objective and user-visible value.}

<!-- 一句话测试：能用一句话说出本任务的交付物吗？说不成一句、或出现"以及/然后"——回去拆。
builder 应能在单个上下文区间内完成本任务（中途 compact 丢任务信息是灾难性表现；按 §2/§4 的量感觉会撞，拆小或裁清单）。拆分纪律全文见 specification-planning step 11。 -->

## 2. Document Manifest

Read order for the builder. Fingerprints, versions, and lock state live in
runtime documents[] (.claude/loop-state.json) — this table never hand-copies
them.

| Order | Kind | ID | Path | Clauses |
|:---|:---|:---|:---|:---|
| 1 | contract | {contract-id} | `docs/contracts/{contract-id}.md` | §{n} |
| 2 | req | REQ-{id} | `docs/requirements/REQ-{id}.md` | FR/NFR/acceptance |
| 3 | module scenario/design | {module/N/A} | `docs/design/prototypes/{module}/` | scenario/story/flow |
| 4 | rule | {rule-id} | `docs/rules/{rule}.md` | all |

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

| Dependency | Required evidence | Status |
|:---|:---|:---|
| TASK-{id} | `{evidence-ref}` | pending / satisfied |

依赖列只认 `TASK-*` 引用——只有 TASK 引用进入 DAG（`tasks check` 的环检测与拓扑）。assignment 级依赖不被机检追踪，需要跨任务顺序时写成 TASK 依赖或在收尾契约中声明。

## 9. Lifecycle Evidence

| Evidence | Reference |
|:---|:---|
| document verification | `{review-evidence-ref}` |
| phase-one request | `{message-ref}` |
| approved read-back | `{message-ref}` |
| activation | `{activation-ref}` |
| completion report | `{message-ref}` |

## 10. Findings And Repairs

| Finding | Canonical BUG | Impact record | Repair assignment | Targeted re-verification | Status |
|:---|:---|:---|:---|:---|:---|
| {finding-id} | BUG-{id} | `{impact-ref}` | `{assignment-id}` | `{evidence-ref}` | open / verified / closed |

BUG procedure is defined by `.claude/skills/bug-resolution/SKILL.md`; review
completion is defined by `.claude/skills/clean-round-evaluation/SKILL.md`.

## 11. History

| Date | Event | Actor | Runtime identity / state | Evidence |
|:---|:---|:---|:---|:---|
| | | | | |
