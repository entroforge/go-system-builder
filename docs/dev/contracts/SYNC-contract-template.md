# 联调合同：SYNC-{id}

> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 前端合同：FE-{id}
> 后端合同：BE-{id}
> 锁定日期：YYYY-MM-DD

> 锁定状态与依据见 runtime documents[] 与 journal（.claude/loop-events.jsonl）——文件内不再手填

## Shared model inputs

- [本操作的权威数据定义](../../architecture/data-model/{schema}.json)：类型、空值、单位和约束从此处读取，不在本合同重新定义。
- [合同索引](CONTRACTS-{id}.md#shared-model-baseline)：确认本操作、数据位置及消费者。
- [共享模型规则](../../rules/shared-model-contracts.md)：遇到模型缺口时返回所属设计层。

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/流程/验收条款 |
| related | `docs/design/prototypes/{module}/` | 模块当前真相：字段、错误码、状态、规则和交互 |
| related | `docs/dev/contracts/FE-{id}.md` | 调用方行为 |
| related | `docs/dev/contracts/BE-{id}.md` | 提供方行为 |
| downstream | `docs/dev/tasks/TASK-{id}.md` | 联调任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 契约和联调验证 |

## 2. 接口定义

### 上游需求

| REQ | 本合同条款 | 前端合同 | 后端合同 |
|:---|:---|:---|:---|
| REQ-{id} | SYNC-{id} §{n} | FE-{id} §{n} | BE-{id} §{n} |

> 「本合同条款」列是 SYNC 自己的条款号唯一声明居所——CONTRACTS 索引的 `SYNC-{id} §{n}` cell 必须与此列一致（contracts check 机检）。

### UI 设计包映射

| 模块当前真相文件 | 字段 / 错误 / 状态 / 权限 / 副作用 | 前端行为 | 后端行为 |
|:---|:---|:---|:---|
| `scenario-model.json` / `cases.json` / `stories.md` / `flows.md` / current `*.html` | {field/error/state/permission/side-effect} | {behavior} | {behavior} |

### Rule → CASE → Story → PATH → Spec → Evidence

| REQ source_ref | Rule / CASE | Story / PATH | Spec | contract assertion | Evidence |
|:---|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} / CASE-{id} | S-{id} / F-{id} / PATH-{id} | `web/e2e/{module}/*.spec.ts` | {wire shape, error, idempotency and state assertion} | REV/QA/E2E round {n} |

### 操作与数据投影

| 操作 | 方法/路径或事件 | 请求模型引用 | 响应模型引用 | 前置条件与结果 |
|:---|:---|:---|:---|:---|
| {operationId} | {METHOD PATH} | {Shared model inputs 中的链接} | {Shared model inputs 中的链接} | {行为承诺} |

## 3. 数据语义

字段、必填/可空、单位、默认值及枚举以权威 Schema 为准；示例引用经过校验的文件，不手写第二份结构。无法由 Schema 表达的跨端语义在此说明并关联 CASE。

## 4. 错误与恢复

| 权威错误定义链接 | 触发条件 | 提供方承诺 | 消费方反馈/恢复 | 禁止副作用 |
|:---|:---|:---|:---|:---|
| {模型中的错误} | {状态/权限/并发} | {协议结果} | {用户行为} | {不得发生的效果} |

## 5. 状态机关联

| 实体 | 事件 | 当前状态 | 下一状态 | 文档 |
|:---|:---|:---|:---|:---|
| {实体} | {event} | {state} | {state} | `docs/architecture/state/{entity}.md` |

## 6. 幂等、限流和权限

| 项 | 规则 |
|:---|:---|
| 幂等 | {规则} |
| 限流 | {规则} |
| 权限 | {规则} |

## 7. 契约测试

| 用例 | 输入 | 预期 |
|:---|:---|:---|
| CT-001 | {输入} | {预期} |

## 8. 消费与完成出口

任务产生后从 [任务索引](../tasks/index-REQ-{id}.md) 查看消费者；实际证据由 TASK/报告回链本合同条款，不回填未来任务或结果。

## 9. 变更申请记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 结论 |
|:---|:---|:---|:---|:---|:---|
| | | | | | pending/approved/rejected |
