# Task: TASK-{id}

> Status: draft
> Reading policy: linked-v1
> Dispatch policy: waves-v1
> （document status: draft=still writing；complete=the planning document is finished and eligible for its original gate——required for every non-cancelled TASK at TR-002, says nothing about implementation；cancelled=excluded from active dispatch and clause coverage, retained in the full S4 delivery batch so any coverage gap resurfaces. Responsibility progress uses the separate five-state model in `docs/rules/state-machine.md`; do not infer implementation `done` from this line.）
> Version: v1.0.0
> Source REQ refs: REQ-{id}
> Module current truth: `docs/design/prototypes/{module}/` / N/A
> Primary contract: {FE/BE/SYNC-id}
> Closing Contract: TASK-{id}#closing-contract
> Execution: runtime owns assignment, owner, responsibility status and integration; do not fill them into this frozen TASK.

TASK ID 使用数字段：例如 `TASK-009` 或 `TASK-009-01`，文件名与标题一致；业务或模块名放在 Objective，不放进 ID。模板中的 `{id}` 按对应 REQ、合同或 TASK 的实际编号替换。当前 REQ 批次中的每份 TASK（含支撑或取消任务）均在 Source REQ refs 声明实际 REQ；支撑任务的空条款声明不等于没有来源。

## 1. Objective

{One testable objective and user-visible value.}

<!-- 用一句话解释本任务的交付目标，再按独立完成判据、责任和真实变更耦合判断是否拆分。
结合 §2 读集、§4 写入范围及执行中的 compact/返工观察调查粒度；句式、文件数或一次 compact 不能单独决定拆分，也不能据此省略必要输入。拆分纪律全文见 specification-planning step 11。 -->

## 2. Document Manifest

Read order for the builder. Fingerprints, versions, and lock state live in
runtime documents[] (.claude/loop-state.json) — this table never hand-copies
them. Paths in this manifest resolve relative to this TASK file's directory,
not the repository root.

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

| Type | Paths / Applicable operations |
|:---|:---|
| read paths | `{path}` |
| prospective write paths | `{path}` |
| forbidden paths | `.claude/loop-state.json`, `{path}` |
| applicable checks / operations | {necessary test, lint, build or inspection operations and their purpose} |
| output paths | `{implementation/test/professional-evidence paths}` |

This scope records the planned dispatch and integration-check boundaries for
the TASK. Cover the product outputs promised in §6, including necessary tests,
wiring and saved professional evidence, with concrete repository-relative paths.
Use comma-separated concrete paths, preferably one inline code span per path:
`internal/a.go`, `tests/a_test.go`. Plain `internal/a.go, tests/a_test.go`
(without code delimiters in the actual cell) is also supported. A complete code
list may end with one paired Chinese or English parenthetical note, for example
`simulation/products/`（仿真产品输出区）; the note is not part of the scope.
Keep other explanations after this table. Do not mix bare and code paths, insert
notes between paths, leave trailing text or unclosed delimiters, use placeholders,
globs, repository-root scope or escaping paths. Only prospective write paths
supply the reviewed write boundary; other rows do not enlarge it. `tasks check`
and the original S4 reception report malformed cells at their TASK and Scope line
before freezing; S5 and Assignment preparation consume those same parsed paths.

Applicable operations explain the work and check responsibility; they are not
a command whitelist. Runtime Plan/Result locations are supplied with the later
actual responsibility and are not future paths to invent here.
Hook events observe progress and provide reminders; they do not
authorize or deny individual tool calls by identity, path, or scope.

## 5. Selected Skills

Record additional skills needed for this TASK's actual risk or responsibility.
The original role and assignment supply default skills; do not repeat their
metadata here. Remove the example row when no additional selection is needed.

| Skill | Category | Source | Version | Applicability |
|:---|:---|:---|:---|:---|
| {skill} | best-practice | `.claude/skills/{skill}/SKILL.md` | {version} | {risk/responsibility} |

## 6. Outputs And Evidence

### 6.1 This TASK's assigned deliverables

Keep only outputs owned by this TASK's dispatched responsibility. A Builder
produces implementation, its scoped tests, actual self-check evidence and a
professional completion result; each independent reviewer receives a separate original assignment.

| Output | Path | Acceptance |
|:---|:---|:---|
| implementation | `{path}` | {contract assertion} |
| tests | `{path}` | {behavior/failure coverage} |
| professional evidence (when saved separately) | `{actual product/test evidence path}` | {observable result and conditions} |
| completion result | original Result entry supplied by the actual later responsibility | explain Closing Contract conditions, actual checks and blockers |

Plan/Result paths, schemas and submission details belong to that later supplied
entry. S4 states the handoff meaning without pre-creating an Assignment, runtime
message path or unavailable schema. Remove inapplicable product/evidence rows;
keep concrete paths for every actual file deliverable.

### 6.2 Subsequent consumption and independent verification

These responsibilities retain their original gates and required coverage.
They are not additional Builder outputs or permission to author another role's
evidence. Keep only applicable downstream responsibilities and describe the
necessary input, coverage and handoff. Runtime owns the actual assignment and
receipt references; the later responsibility supplies its result entry. The
legacy report paths below apply only when that original responsibility actually
uses them; they create no default separate report obligation.

| Responsibility / consumer | Evidence or action | Acceptance |
|:---|:---|:---|
| Main / integration | bound development branch and integration receipt | original required checks pass after integration |
| Delivery Verifier | original result entry (legacy: `docs/reports/review/REV-{id}.md`) | assigned dimension result |
| QA | original result entry (legacy: `docs/reports/qa/QA-{id}.md`) | assigned dimension result |
| E2E | original result entry (legacy: `docs/reports/e2e/E2E-{id}.md`) | assigned behavior verification; real-browser flow where browser verification applies |

## 7. Closing Contract

Cover observable behavior, this responsibility's own checks, its reviewed
scope, and a usable handoff. Use as many assertions as the actual conditions
need; the four examples below are not a line-count requirement. These are
professional completion criteria, not commands executed by the checker.

```text
assert {contract clause} == satisfied
assert {verification command} == pass
assert actual changes stay within the reviewed TASK and assigned write scope
assert completion_result explains each condition and any actual scope deviation
```

An actual deviation must be reported and resolved through the original scope
and change process; reporting it alone does not authorize a wider scope.

## 8. Dependencies

When there are no dependencies, keep the table body empty; do not add placeholder
rows such as `（无）` or `N/A`. Only valid `TASK-*` references are machine-tracked.

| Dependency | Required artifact / verification |
|:---|:---|
| TASK-{id} | {committed artifact integrated and verified on the bound development branch} |

依赖列使用合法数字 TASK ID（如 `TASK-009-01`）——只有 TASK 引用进入 DAG（`tasks check` 的环检测与拓扑）。assignment 级依赖不被机检追踪，需要跨任务顺序时写成 TASK 依赖或在收尾契约中声明。

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
