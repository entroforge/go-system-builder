# 开发合同总览

> 需求：REQ-{id}
> 状态：draft / reviewed / locked
> 版本：v0.1.0
> PM / Architect：{name}
> Contractor：{Contractor}
> 锁定日期：YYYY-MM-DD

> 锁定依据：`{runtime-id}@{revision}` / `{transition-id}` / `{document-verification-evidence}`。

## 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | locked REQ 条款 |
| related | `docs/design/{path}.md` | 设计约束或 N/A |
| related | `docs/design/prototypes/{module}/` | UI impact = changed 时必填 |
| downstream | `docs/tasks/index.md` | 派生任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 文档审核 |
| runtime | `.claude/loop-state.json` | 当前状态仅通过 runtime revision 引用 |

## 合同锁定声明

合同 locked 后是 Builder 唯一执行依据。范围、接口、字段、错误码、状态机、副作用、验收标准变更必须走 change-control。

## 合同清单

| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |
|:---|:---|:---|:---|:---|:---|
| FE-001 | 前端 | {Builder-FE-01} | draft | v0.1.0 | SYNC-001 |
| BE-001 | 后端 | {Builder-BE-01} | draft | v0.1.0 | SYNC-001 |

## 联调点矩阵

| 联调点 | 前端合同 | 后端合同 | 契约文档 | 状态 |
|:---|:---|:---|:---|:---|
| SYNC-001 | FE-001 | BE-001 | `SYNC-001.md` | draft |

## UI 设计包输入

| REQ | UI impact | UI 设计包 | Fingerprint (SHA-256) | 合同影响 | E2E 来源 |
|:---|:---|:---|:---|:---|:---|
| REQ-{id} | none / changed / unknown | `docs/design/prototypes/{module}/` (index.html + *.html + stories.md + flows.md) / N/A | `{sha256}` / N/A | FE / BE / SYNC / N/A | `E2E-USER-FLOW` / N/A |

## 需求覆盖矩阵

| REQ 条款 | UI 设计包 | FE 合同条款 | BE 合同条款 | SYNC 条款 | 派生 TASK |
|:---|:---|:---|:---|:---|:---|
| FR-{id} | UI design package / N/A | FE-{id} §{n} | BE-{id} §{n} | SYNC-{id} §{n} | TASK-{id} |

## 合同变更记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 影响合同 |
|:---|:---|:---|:---|:---|:---|
| | | | | | |
