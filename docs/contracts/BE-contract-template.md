# 开发合同：BE-{id}

> 类型：backend
> 状态：draft / reviewed / locked
> 版本：v0.1.0
> 负责 Builder：{Builder-BE-01}
> 关联需求：`docs/requirements/REQ-{id}.md`
> 关联设计：`docs/design/architecture/ARCHITECTURE.md`
> 锁定日期：YYYY-MM-DD

> 锁定依据：`{runtime-id}@{revision}` / `{transition-id}` / `{document-verification-evidence}`。

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/NFR/验收条款 |
| related | `docs/design/prototypes/{module}/` | UI 数据、状态和错误需求 |
| related | `docs/contracts/SYNC-{id}.md` | 输出契约 |
| related | `docs/contracts/FE-{id}.md` | 调用方行为 |
| downstream | `docs/tasks/TASK-{id}.md` | 派生任务 |
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

| REQ 条款 | 本合同条款 | 验收标准 |
|:---|:---|:---|
| FR-{id} | §{n} | {标准} |

### UI 设计包反推需求

| UI 设计文件 | 数据 / 状态 / 错误 / 权限 / 副作用 | 本合同条款 | SYNC 条款 |
|:---|:---|:---|:---|
| prototype.html / USER-STORY / USER-FLOW | {field/state/error/permission/side-effect} | §{n} | SYNC-{id} §{n} |

## 4. 技术约束

| 项 | 约束 |
|:---|:---|
| 语言 | {Go/Python/Java/Rust/etc} |
| 框架 | {框架} |
| 数据库 | {数据库} |
| 缓存/队列 | {组件} |

## 5. 数据模型

| 实体 | 表/集合 | 状态机 |
|:---|:---|:---|
| {实体} | {table} | `docs/design/state/{entity}.md` |

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

## 8. 派生任务

| TASK | 路径 | 覆盖条款 | 状态 |
|:---|:---|:---|:---|
| TASK-{id} | `docs/tasks/TASK-{id}.md` | §{n} | pending |

## 9. 变更申请记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 结论 |
|:---|:---|:---|:---|:---|:---|
| | | | | | pending/approved/rejected |
