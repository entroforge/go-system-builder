# 模块用户故事：stories.md

> 模块：{module}
> 当前真相：本文件维护模块完整故事集合；不创建 per-REQ/per-round 副本
> 关联原型：`docs/design/prototypes/{module}/index.html` + 当前 `*.html`
> 关联场景：`scenario-model.json` / `cases.json` / `scenario-coverage.json` / `fixture-contract.json`

## 使用方式

本文件按模块维护完整、当前且唯一的真实用户故事集合，而不是需求增量或
历史版本集合。一个故事描述“谁在什么情境下，为何要完成什么目标”；它可
关联一条或多条 `PATH-*`，并必须引用 scenario branch / CASE。REQ 只写入
`source_refs`，不出现在文件名或所有权字段中。

## 1. 故事目录

| 故事 ID | 用户角色 | 触发场景 | 用户目标 | CASE / PATH | source_refs |
|:---|:---|:---|:---|:---|:---|
| S-001 | {role} | {real-world trigger} | {goal} | CASE-001 / PATH-001 | REQ-{id}/FR-{id} |
| S-002 | {role} | {rejection or recovery trigger} | {safe recovery goal} | CASE-002 / PATH-002 | REQ-{id}/FR-{id} |

## 2. 用户和场景

<!-- 复制第 2 至 8 节，为故事目录中的每个故事补充必要内容。故事可共享
     页面跳转和 UI/UX 审查结论；无须为了套模板重复相同背景。 -->

| 项 | 内容 |
|:---|:---|
| 故事 ID | S-{id} |
| source_refs | REQ-{id}/FR-{id}, decision/state/rule refs |
| 用户角色 | {role} |
| 触发背景 | {why this user starts the flow} |
| 业务目标 | {what success means} |
| 前置条件 | {permissions, data, state} |
| 覆盖分支 | BR-{id}-ALLOW, BR-{id}-REJECT |
| 覆盖 CASE | CASE-{id} |

## 3. 功能设计背景

{为什么需要这个功能，当前业务问题是什么，页面或模块为什么这样组织。}

## 4. 页面跳转逻辑

| 起点 | 用户意图 | 动作 | 目标页面 / 状态 | 设计理由 | 原型区域 |
|:---|:---|:---|:---|:---|:---|
| {page/state} | {intent} | {action} | {page/state} | {why this transition exists} | {prototype section} |

## 5. 成功故事

{用自然语言描述用户从入口到完成目标的完整故事。}

## 6. 异常和边缘故事

| 场景 | 用户看到什么 | 系统如何恢复 | 覆盖条款 |
|:---|:---|:---|:---|
| {empty/error/permission/cancel/retry} | {visible state} | {recovery} | FR-{id} / NFR-{id} |

## 7. UI/UX 审查问题

| 问题 | 影响 | 决策 | 状态 |
|:---|:---|:---|:---|
| {navigation/layout/copy/accessibility concern} | {impact} | {decision or owner} | open / closed |

> 状态为 `open` 且影响用户理解、页面跳转、权限、数据含义或错误恢复时，不得进入 S3 合同锁定。

## 8. REQ 映射

| source_ref | 原型区域 | 用户价值 | 验收方式 |
|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | {screen/section} | {value} | {evidence} |

## 9. 与原型和动线的一致性

| 检查项 | 结果 | 证据 |
|:---|:---|:---|
| 成功故事在当前 `*.html` 中可见 | pass / fail | {section/state} |
| 异常和边缘故事在当前 `*.html` 中可见或明确 N/A | pass / fail | {section/state/N-A reason} |
| 页面跳转均有 `F-NNN` / `PATH-*` 路径覆盖 | pass / fail | {PATH ids} |
| required allow/reject branch coverage = 100% | pass / fail | {scenario-coverage.json} |
| browser-required CASE 均绑定 module spec | pass / fail | `web/e2e/{module}/` |
| 合同需要的字段、状态、错误、权限和副作用已标出 | pass / fail | {contract notes} |
