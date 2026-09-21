# 开发合同总览

> 需求：REQ-{id}
> Shared model policy: json-schema-v1
> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> PM / Architect：{name}
> Contractor：{Contractor}
> 锁定日期：YYYY-MM-DD

> 锁定状态与依据见 runtime documents[] 与 journal（.claude/loop-events.jsonl）——文件内不再手填

## 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | locked REQ 条款 |
| related | `docs/design/{path}.md` | 设计约束或 N/A |
| related | `docs/design/prototypes/{module}/` | UI impact = changed 时必填；模块当前真相包 |
| downstream | `docs/dev/tasks/index-REQ-{id}.md` | 派生任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 文档审核 |
| runtime | `.claude/loop-state.json` | 当前状态通过 Runtime identity、状态和指纹引用；commit revision 仅作可选审计元数据 |

## 合同锁定声明

合同 locked 后与其引用的共同设计基线构成 Builder 的执行依据。范围、接口、字段、错误码、状态机、副作用、验收标准变更必须走 change-control。

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

| REQ source_ref | UI impact | 模块当前真相包 | Fingerprint (SHA-256) | 合同影响 | E2E 来源 |
|:---|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | none / changed / unknown | `docs/design/prototypes/{module}/` (index.html + stories.md + flows.md + scenario-model.json + cross-matrix.json + cases.json + scenario-coverage.json + fixture-contract.json + *.html) / N/A | `{sha256}` / N/A | FE / BE / SYNC / N/A | CASE/PATH → `web/e2e/{module}/` / N/A |

## 需求覆盖矩阵

条款列每条款一个 `{id} §{n}` 记号，可并列多个（如 `BE-001 §2, BE-001 §3`）——本矩阵是条款宇宙唯一居所；每个 `§n` 必须与目标契约正文任一「本合同条款」列声明的条款号一致（contracts check 机检，数字精确匹配）；任务侧覆盖声明（TASK §3）由 `tasks check` 对此聚合对账。

| REQ source_ref | Rule → CASE → Story → PATH → Spec | FE 合同条款 | BE 合同条款 | SYNC 条款 |
|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} → CASE-{id} → S-{id} → F-{id} → PATH-{id} → `web/e2e/{module}/*.spec.ts` | FE-{id} §{n} | BE-{id} §{n} | SYNC-{id} §{n} |

## 合同变更记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 影响合同 |
|:---|:---|:---|:---|:---|:---|
| | | | | | |


## Shared model baseline

先阅读 [共享模型规则](../../rules/shared-model-contracts.md)，从 [模型入口](../../architecture/data-model/MODEL-{id}.md) 定位现有权威定义。先共同收敛数据与 SYNC，再派生 FE/BE；不得按 REQ 复制模型。

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| {operationId} | {request/response-200} | [权威定义](../../architecture/data-model/{schema}.json) | [前端](FE-{id}.md) [后端](BE-{id}.md) [协议](SYNC-{id}.md) | [合法数据](../../architecture/data-model/{valid}.json) | N/A |

不涉及共享数据时改为 `Shared model policy: none` 并添加具体 `Shared model reason`，删除占位行。既有其他协议不得假装已被 JSON Schema 适配器验证。
