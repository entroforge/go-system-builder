# 开发合同：BE-{id}

> 类型：backend
> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 负责 Builder：{Builder-BE-01}
> 关联需求：`docs/requirements/REQ-{id}.md`
> 关联设计：`docs/architecture/ARCHITECTURE.md`
> 锁定日期：YYYY-MM-DD

> 锁定状态与依据见 runtime documents[] 与 journal（.claude/loop-events.jsonl）——文件内不再手填

## Shared model inputs

- [本操作的权威数据定义](../../architecture/data-model/{schema}.json)：类型、空值、单位和约束从此处读取，不在本合同重新定义。
- [合同索引](CONTRACTS-{id}.md#shared-model-baseline)：确认本操作、数据位置及消费者。
- [共享模型规则](../../rules/shared-model-contracts.md)：遇到模型缺口时返回所属设计层。

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/NFR/验收条款 |
| related | `docs/design/prototypes/{module}/` | UI 数据、状态和错误需求 |
| related | `docs/dev/contracts/SYNC-{id}.md` | 输出契约 |
| related | `docs/dev/contracts/FE-{id}.md` | 调用方行为 |
| downstream | `docs/dev/tasks/TASK-{id}.md` | 派生任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 合同和交付验证 |

## 2. 合同范围

### 交付模块

- [ ] {模块 A}
- [ ] {模块 B}

### 排除范围

- {明确不做的事项}

## 3. 输出契约

| 契约 | 类型 | 文档 | 消费者 |
|:---|:---|:---|:---|
| SYNC-001 | REST API | `SYNC-001.md` | FE-001 |

### 需求条款映射

| REQ source_ref | Rule / CASE / Story / PATH | 本合同条款 | 验收标准 |
|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} → CASE-{id} → S-{id} → F-{id} → PATH-{id} | §{n} | {标准} |

### UI 设计包反推需求

| 模块当前真相文件 | 数据 / 状态 / 错误 / 权限 / 副作用 | 本合同条款 | SYNC 条款 |
|:---|:---|:---|:---|
| `scenario-model.json` / `cases.json` / `stories.md` / `flows.md` / current `*.html` | {field/state/error/permission/side-effect} | §{n} | SYNC-{id} §{n} |

### Rule → CASE → Story → PATH → Spec → Evidence

| REQ source_ref | Rule / CASE | Story / PATH | Spec | BE oracle / persistence assertion | Evidence |
|:---|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} / CASE-{id} | S-{id} / F-{id} / PATH-{id} | `web/e2e/{module}/*.spec.ts` | {visible, terminal_state, persisted_effects, forbidden_side_effects; negative also rejection, expected_state, recovery} | REV/QA/E2E round {n} |

## 4. 技术约束

| 项 | 约束 |
|:---|:---|
| 语言 | {Go/Python/Java/Rust/etc} |
| 框架 | {框架} |
| 数据库 | {数据库} |
| 缓存/队列 | {组件} |

## 5. 数据与持久化映射

共享字段与请求响应从 Shared model inputs 读取。仅在存在真实转换时记录存储映射及相应测试；状态转换引用权威状态机，不另抄一份。

## 6. 必须遵守的规则

- `docs/rules/api-design.md`
- `docs/rules/state-machine.md`
- `docs/rules/error-handling.md`
- `docs/rules/security.md`

## 7. 验收标准

- [ ] 所有接口通过契约测试。
- [ ] 状态转换符合状态机文档。
- [ ] 数据迁移可执行且可回滚。
- [ ] 单元和集成测试通过。
- [ ] 无 P0/P1 缺陷。

## 8. 消费与完成出口

任务产生后从 [任务索引](../tasks/index-REQ-{id}.md) 查看消费者；实际证据由 TASK/报告回链本合同条款，不回填未来任务或结果。

## 9. 变更申请记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 结论 |
|:---|:---|:---|:---|:---|:---|
| | | | | | pending/approved/rejected |
