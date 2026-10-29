# 开发合同：BE-{id}

> 类型：backend
> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 合同负责人：{Main / 实际负责本合同专业承诺的人员或角色}
> 实现责任：{实际后端提供方职责；S4 拆任务后再安排执行者}
> 关联需求：`docs/requirements/REQ-{id}.md`
> 关联设计：{当前 view 提供的实际 `docs/architecture/ARCHITECTURE-*.md` 及适用 ADR 路径/条款}
> 作者维护本合同的真实状态与专业版本；登记时间、摘要和锁定关联由原消费者记录到 Runtime documents[] 与 journal，不另填机器元数据。

## Shared model inputs

仅实际共享数据适用时保留权威定义链接，且与索引逐项一致。索引声明 `Shared model policy: none` 时，此节写具体 N/A 依据，删除 Schema 占位链接；不为填表创建 Schema 或示例。以下跨合同链接只引用实际适用合同；不存在独立分端合同须写 N/A 依据及真实参与者，不能虚构对端文件。

- [本操作的权威数据定义](../../architecture/data-model/{schema}.json)：类型、空值、单位和约束从此处读取，不在本合同重新定义。
- [合同索引](CONTRACTS-{id}.md#shared-model-baseline)：确认本操作、数据位置及消费者。
- [共享模型规则](../../rules/shared-model-contracts.md)：S3 可在既定设计含义内补足新模型结构；改变冻结 S2 内容或其成员须正式设计返工，改变 REQ 语义须需求修订。

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/NFR/验收条款 |
| related | `docs/design/prototypes/{module}/` | 实际 UI 或服务行为的数据、状态和错误需求 |
| related | {实际 SYNC 路径或有据 N/A} | 仅真实共享操作需要协议合同 |
| related | {适用消费方合同路径或有据 N/A} | 实际调用方行为，不为服务场景虚构 FE |
| downstream | `docs/dev/tasks/TASK-{id}.md` | 派生任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 合同和交付验证 |

## 2. 合同范围

### 交付模块

- [ ] {模块 A}
- [ ] {模块 B}

### 排除范围

- {明确不做的事项}

## 3. 输出契约

仅 BE 内部交付、无共享操作时，写明 SYNC 不适用的实际依据并删除协议占位行；仍保留本合同的真实承诺、条款与 CASE。

| 契约 | 类型 | 文档 | 消费者 |
|:---|:---|:---|:---|
| {实际 SYNC ID} | {实际 API/事件/任务边界} | {实际协议路径} | {API 调用方/worker/适用 FE} |

### 需求条款映射

| REQ source_ref | Rule / CASE / Story / Flow / 适用 PATH | 本合同条款 | 验收标准 |
|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} → CASE-{id} → S-{id} → F-{id}；仅适用浏览器分支加 PATH | §{n} | {标准} |

### UI / 服务行为包反推需求

| 模块当前真相文件 | 数据 / 状态 / 错误 / 权限 / 副作用 | 本合同条款 | SYNC 条款 |
|:---|:---|:---|:---|
| `scenario-model.json` / `cases.json` / `stories.md` / `flows.md` / 适用 UI 的 current `*.html` | {field/state/error/permission/side-effect} | §{n} | {实际 SYNC-{id} §{n} 或有据 N/A} |

### Rule → CASE → Story / Flow → 适用测试 → 后续证据

| REQ source_ref | Rule / CASE | Story / Flow / 适用 PATH | Spec | BE oracle / persistence assertion | Evidence |
|:---|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} / CASE-{id} | S-{id} / F-{id}；浏览器场景才有 PATH-{id} | {实际服务单元/集成/契约测试入口；浏览器场景才用 web/e2e spec} | {visible, terminal_state, persisted_effects, forbidden_side_effects; negative also rejection, expected_state, recovery} | 后续 REV/QA/E2E 责任在实际报告中回链 |

服务示例：API 调用者或 worker 经 F-{id} 调用操作，BR/CASE 约束响应、终态、持久化及禁止副作用，由实际服务契约/集成测试验证。`browser_required:false` 不要求 HTML、PATH 或浏览器 Spec；保留 CASE 和完整判据。只有实际 UI 分支才填写 PATH 与浏览器入口。

S3 声明验证要求与责任，不填写未来报告的轮次、时间或执行结果。

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
