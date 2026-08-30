# 开发合同：FE-{id}

> 类型：frontend
> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 负责 Builder：{Builder-FE-01}
> 关联需求：`docs/requirements/REQ-{id}.md`
> 关联设计：`docs/design/architecture/ARCHITECTURE.md`
> 锁定日期：YYYY-MM-DD

> 锁定状态与依据见 runtime documents[] 与 journal（.claude/loop-events.jsonl）——文件内不再手填

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/NFR/验收条款 |
| upstream | `docs/design/prototypes/{module}/` | UI impact = changed 时必填 |
| related | `docs/contracts/SYNC-{id}.md` | 联调约束 |
| related | `docs/contracts/BE-{id}.md` | 对端实现边界 |
| downstream | `docs/tasks/TASK-{id}.md` | 派生任务 |
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
| 联调合同 | `docs/contracts/SYNC-{id}.md` | locked |

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
| CASE-{id} | positive / negative · BR-{id} | {visible / terminal_state / persisted_effects / forbidden_side_effects; negative also rejection / expected_state / recovery} | F-{id} / PATH-{id} | `web/e2e/{module}/*.spec.ts` | QA/E2E round {n} |

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

| 联调点 | 对端合同 | 接口 | 验收标准 |
|:---|:---|:---|:---|
| SYNC-001 | BE-001 | `{METHOD} {PATH}` | {标准} |

## 7. 验收标准

- [ ] 合同范围内模块全部完成。
- [ ] 所有关联 SYNC 合同通过。
- [ ] 错误码处理符合联调合同。
- [ ] 单元/组件测试覆盖新增逻辑。
- [ ] 无 P0/P1 缺陷。

## 8. 派生任务

| TASK | 路径 | 覆盖条款 | 状态 |
|:---|:---|:---|:---|
| TASK-{id} | `docs/tasks/TASK-{id}.md` | §{n} | pending |

## 9. 变更申请记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 结论 |
|:---|:---|:---|:---|:---|:---|
| | | | | | pending/approved/rejected |
