# 开发合同：FE-{id}

> 类型：frontend
> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 合同负责人：{Main / 实际负责本合同专业承诺的人员或角色}
> 实现责任：{实际前端消费方职责；S4 拆任务后再安排执行者}
> 关联需求：`docs/requirements/REQ-{id}.md`
> 关联设计：{当前 view 提供的实际 `docs/architecture/ARCHITECTURE-*.md` 及适用 ADR 路径/条款}
> 作者维护本合同的真实状态与专业版本；登记时间、摘要和锁定关联由原消费者记录到 Runtime documents[] 与 journal，不另填机器元数据。

仅实际前端交付适用时创建本合同；无前端变化的服务场景不创建 FE 占位，也不要求浏览器 Spec。下游 TASK 与证据路径是后续消费入口，不代表 S3 已派发或执行验证。

## Shared model inputs

仅实际共享数据适用时保留权威定义链接，且与索引逐项一致。索引声明 `Shared model policy: none` 时，此节写具体 N/A 依据，删除 Schema 占位链接；不为填表创建 Schema 或示例。以下跨合同链接只引用实际适用合同；不存在独立分端合同须写 N/A 依据及真实参与者，不能虚构对端文件。

- [本操作的权威数据定义](../../architecture/data-model/{schema}.json)：类型、空值、单位和约束从此处读取，不在本合同重新定义。
- [合同索引](CONTRACTS-{id}.md#shared-model-baseline)：确认本操作、数据位置及消费者。
- [共享模型规则](../../rules/shared-model-contracts.md)：S3 可在既定设计含义内补足新模型结构；改变冻结 S2 内容或其成员须正式设计返工，改变 REQ 语义须需求修订。

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/NFR/验收条款 |
| upstream | `docs/design/prototypes/{module}/` | UI impact = changed 时必填 |
| related | {实际 SYNC 路径或有据 N/A} | 仅真实共享操作需要联调约束 |
| related | {实际 BE 路径或有据 N/A} | 仅实际后端交付需要对端合同 |
| downstream | `docs/dev/tasks/TASK-{id}.md` | 派生任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 合同和交付验证 |

## 2. 合同范围

### 交付模块

- [ ] {模块 A}
- [ ] {模块 B}

### 排除范围

- {明确不做的事项}

## 3. 输入依赖

| 依赖项 | 路径 | 状态 |
|:---|:---|:---|
| 需求 | `docs/requirements/REQ-{id}.md` | locked |
| 模块入口/页面 HTML | `docs/design/prototypes/{module}/index.html` + `*.html` | current / N/A |
| 模块场景输入/fixture | `docs/design/prototypes/{module}/scenario-model.json` + `fixture-contract.json` | current / N/A |
| 生成场景输出 | `docs/design/prototypes/{module}/cases.json` + `scenario-coverage.json` | current / N/A |
| 用户故事 | `docs/design/prototypes/{module}/stories.md` | current / N/A |
| 用户动线 | `docs/design/prototypes/{module}/flows.md` | current / N/A |
| 联调合同 | {实际 SYNC 路径；无共享操作写有据 N/A} | 适用时 locked；否则 N/A |

### 需求条款映射

| REQ source_ref | Rule / CASE / Story / PATH | 本合同条款 | 验收标准 |
|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} → CASE-{id} → S-{id} → F-{id} → PATH-{id} | §{n} | {标准} |

### UI 设计包映射

| UI/场景当前真相文件 | 页面 / 组件 / visible oracle | 本合同条款 | 验收标准 |
|:---|:---|:---|:---|
| current `*.html` / `stories.md` / `flows.md` / `cases.json` | {screen/component} | §{n} | {标准} |

### 场景与测试映射

| CASE | polarity / branch | visible oracle | PATH | module spec | evidence |
|:---|:---|:---|:---|:---|:---|
| CASE-{id} | positive / negative · BR-{id} | {visible / terminal_state / persisted_effects / forbidden_side_effects; negative also rejection / expected_state / recovery} | F-{id} / PATH-{id} | `web/e2e/{module}/*.spec.ts` | 后续 QA/E2E 责任在实际报告中回链 |

S3 声明验证要求与责任，不填写未来报告的轮次、时间或执行结果。

## 4. 技术约束

| 项 | 约束 |
|:---|:---|
| 框架 | {React/Vue/etc} |
| 语言 | {TypeScript/etc} |
| 状态管理 | {工具} |
| UI | {组件库} |

## 5. 必须遵守的规则

- `docs/rules/communication.md`
- `docs/rules/api-design.md`
- `docs/rules/error-handling.md`
- `docs/rules/security.md`

## 6. 联调点

仅填写实际共享边界；纯前端本地行为可写 N/A 及依据并删除占位行。SYNC、BE 各自判断，不因 FE 适用而自动要求二者。

| 联调点 | 对端合同 | 接口 | 验收标准 |
|:---|:---|:---|:---|
| SYNC-001 | BE-001 | `{METHOD} {PATH}` | {标准} |

## 7. 验收标准

- [ ] 合同范围内模块全部完成。
- [ ] 适用的关联 SYNC 合同通过；不适用时已记录具体依据。
- [ ] 适用的跨端错误码处理符合联调合同；本地错误仍须保留实际 CASE 判据。
- [ ] 单元/组件测试覆盖新增逻辑。
- [ ] 无 P0/P1 缺陷。

## 8. 消费与完成出口

任务产生后从 [任务索引](../tasks/index-REQ-{id}.md) 查看消费者；实际证据由 TASK/报告回链本合同条款，不回填未来任务或结果。

## 9. 变更申请记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 结论 |
|:---|:---|:---|:---|:---|:---|
| | | | | | pending/approved/rejected |
