# N/A 清单模板（S7 not_applicable 检查单）

> 状态：locked
> 版本：v1.0.0
> Owner：Review Planner
> 关联需求：`docs/requirements/REQ-{id}.md`（bound REQ 的 `metadata.ui_impact`）
> 关联实现：`internal/review/plan.go`（S7_NA_CHECKLIST_MISSING 门禁）、`internal/review/draft.go`（`na-checklist-template-1` 示例 id）
> 日期：2026-08-27

## 用途

一条 Claim 只有在按本清单逐项核实后，才允许声明 `applicability: not_applicable`
（S7-9 / RC-07：裸 `na_rationale` 不是结论）。登记门禁
`S7_NA_CHECKLIST_MISSING` 要求每条 N/A Claim 携带 `na_checklist_id`；
本模板就是该 id 指向的检查单。计划里的 N/A claim 通常填
`na-checklist-template-1#bound_req#ui_impact`（模板 + 依据字段），并在本文件
（或其制品副本）中留下填写痕迹。

填完五节后，把 `na_checklist_id` 写进 Claim、把人读摘要写进 `na_rationale`，
再注册：`runtime review-plan --file plan.json --expected-revision <N>`。

## 检查单（五节，逐项必填）

### 1. scope —— N/A 范围

- Claim：`{claim_id}`（lens：delivery / qa / e2e）
- 声明不适用的是哪个维度/入口面：{例如 E2E 浏览器面 / 该 lens 的全部 Claims}
- 明确不在本 N/A 范围内、仍由其他 Claim 覆盖的部分：{列表，不允许真空}

### 2. impact —— 影响分析

- 判定依据（权威字段或制品）：{例如 `bound_req.metadata.ui_impact = none`}
- 为什么该依据覆盖本次变更的全部用户可见面：{一句话 + 指向 REQ §C/§D 或
  change-impact 制品}
- 若依据是 `unknown`：本 N/A 无效 —— 先走 REQ §D 澄清门禁
  （`ui_impact_resolved`）再回到本清单。

### 3. evidence —— 证据

- 支持结论的具体证据（文件/制品/digest，不接受"凭印象"）：
  {例如 `docs/requirements/REQ-039.md` 顶部 ui_impact 字段、
  `.claude/evidence/change-impact.json` sha256=…}
- 每条证据如何被 `source_refs` 引用：{列表，与 Claim.source_refs 一致}

### 4. alternative —— 已考虑的替代结论

- 为什么"required"不成立：{对照该 lens 的正常验收面，说明本次变更触碰不到}
- 若 ui_impact 实为 `changed`，本节必须说明改走哪条路径：
  {例如 regression_available 复用 E2E 资产，或 cold_start 建验证工作区}

### 5. sign-off —— 签署

- 填写人（Planner）：{agent / 人名}
- 复核人（Reviewer，独立于填写人）：{agent / 人名}
- 日期与结论：YYYY-MM-DD —— checked / checked-with-exception（异常须在 §4 记录）

## 快速示例（ui_impact=none 的 E2E N/A）

| 节 | 填写 |
| --- | --- |
| scope | claim-e2e-na-1；E2E 浏览器面；delivery/qa Claims 照常覆盖 |
| impact | `bound_req.metadata.ui_impact = none`（REQ 顶部强制字段） |
| evidence | `docs/requirements/REQ-039.md` ui_impact 字段；`source_refs: [bound_req]` |
| alternative | 变更仅触达服务端内部路径，无路由/组件/交互面变化 |
| sign-off | Planner: agent-planner；Reviewer: agent-reviewer；2026-08-27 checked |

对应 Claim 形态（注册门禁要求的最低字段集）：

```json
{
  "claim_id": "claim-e2e-na-1",
  "lens": "e2e",
  "applicability": "not_applicable",
  "na_rationale": "bound REQ declares no UI impact; no entry point or browser-observable behavior is in scope",
  "na_checklist_id": "na-checklist-template-1#bound_req#ui_impact",
  "source_refs": ["bound_req"]
}
```

## 版本历史

| 日期 | 变更 | 决定 | Owner |
| --- | --- | --- | --- |
| 2026-08-27 | 首版：五节结构（scope/impact/evidence/alternative/sign-off），绑定 S7_NA_CHECKLIST_MISSING 门禁与 draft 示例 id | locked | Review Planner |
