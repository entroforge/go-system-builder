# 联调合同：SYNC-{id}

> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 合同负责人：{Main / 实际负责本合同专业承诺的人员或角色}
> 消费方合同：{实际 FE/BE/其他消费方合同；无独立分端合同时说明真实参与者}
> 提供方合同：{实际 BE/其他提供方合同；无独立分端合同时说明依据与真实参与者}
> 作者维护本合同的真实状态与专业版本；登记时间、摘要和锁定关联由原消费者记录到 Runtime documents[] 与 journal，不另填机器元数据。

## Shared model inputs

仅实际共享数据适用时保留权威定义链接，且与索引逐项一致。索引声明 `Shared model policy: none` 时，此节写具体 N/A 依据，删除 Schema 占位链接；不为填表创建 Schema 或示例。以下跨合同链接只引用实际适用合同；不存在独立分端合同须写 N/A 依据及真实参与者，不能虚构对端文件。

- [本操作的权威数据定义](../../architecture/data-model/{schema}.json)：类型、空值、单位和约束从此处读取，不在本合同重新定义。
- [合同索引](CONTRACTS-{id}.md#shared-model-baseline)：确认本操作、数据位置及消费者。
- [共享模型规则](../../rules/shared-model-contracts.md)：S3 可在既定设计含义内补足新模型结构；改变冻结 S2 内容或其成员须正式设计返工，改变 REQ 语义须需求修订。

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/流程/验收条款 |
| related | `docs/design/prototypes/{module}/` | 模块当前真相：字段、错误码、状态、规则和交互 |
| related | {适用消费方合同路径或有据 N/A} | 实际调用方行为，不为服务场景虚构 FE |
| related | {实际提供方合同路径或有据 N/A} | 提供方行为；无独立合同时说明实际参与者 |
| downstream | `docs/dev/tasks/TASK-{id}.md` | 联调任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 契约和联调验证 |

## 2. 接口定义

### 上游需求

| REQ | 本合同条款 | 消费方合同 | 提供方合同 |
|:---|:---|:---|:---|
| REQ-{id} | SYNC-{id} §{n} | {实际消费方条款或有据 N/A} | {实际提供方条款或有据 N/A} |

> 「本合同条款」列是 SYNC 自己的条款号唯一声明居所——CONTRACTS 索引的 `SYNC-{id} §{n}` 必须与此列一致。机检读取带表头和分隔行的普通 Markdown 表格，只核编号结构；消费方、提供方引用列及代码示例不声明 SYNC 自身条款。实际承诺和跨端映射仍需专业审查。

### UI / 服务行为包映射

| 模块当前真相文件 | 字段 / 错误 / 状态 / 权限 / 副作用 | 消费方行为 | 提供方行为 |
|:---|:---|:---|:---|
| `scenario-model.json` / `cases.json` / `stories.md` / `flows.md` / 适用 UI 的 current `*.html` | {field/error/state/permission/side-effect} | {behavior} | {behavior} |

### Rule → CASE → Story / Flow → 适用测试 → 后续证据

| REQ source_ref | Rule / CASE | Story / Flow / 适用 PATH | Spec | contract assertion | Evidence |
|:---|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} / CASE-{id} | S-{id} / F-{id}；浏览器场景才有 PATH-{id} | {实际服务单元/集成/契约测试入口；浏览器场景才用 web/e2e spec} | {wire shape, error, idempotency and state assertion} | 后续 REV/QA/E2E 责任在实际报告中回链 |

服务示例：API 调用者或 worker 经 F-{id} 调用操作，BR/CASE 约束响应、终态、持久化及禁止副作用，由实际服务契约/集成测试验证。`browser_required:false` 不要求 HTML、PATH 或浏览器 Spec；保留 CASE 和完整判据。只有实际 UI 分支才填写 PATH 与浏览器入口。

S3 声明验证要求与责任，不填写未来报告的轮次、时间或执行结果。

### 操作与数据投影

| 操作 | 方法/路径或事件 | 请求模型引用 | 响应模型引用 | 前置条件与结果 |
|:---|:---|:---|:---|:---|
| {operationId} | {METHOD PATH} | {Shared model inputs 中的链接} | {Shared model inputs 中的链接} | {行为承诺} |

## 3. 数据语义

适用共享数据时，字段、必填/可空、单位、默认值及枚举以权威 Schema 为准；示例引用经过校验的文件，不手写第二份结构。无法由 Schema 表达的跨端语义在此说明并关联 CASE。无共享数据时写具体 N/A 依据，并在操作表的模型列填写有据 N/A；实际时序、错误、状态和恢复承诺仍须保留。

## 4. 错误与恢复

| 权威错误定义链接 | 触发条件 | 提供方承诺 | 消费方反馈/恢复 | 禁止副作用 |
|:---|:---|:---|:---|:---|
| {模型中的错误} | {状态/权限/并发} | {协议结果} | {实际调用者的反馈、重试或恢复行为} | {不得发生的效果} |

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
