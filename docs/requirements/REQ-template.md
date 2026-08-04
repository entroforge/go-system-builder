# 需求：REQ-{id}

> 名称：{需求名称}
> 状态：discovery / draft / reviewed / locked / changed / archived
> Owner：{产品/用户}
> PM / Architect：{姓名}
> 版本：v0.1.0
> 创建日期：YYYY-MM-DD
> UI impact：none / changed / unknown

> `UI impact` 是 `状态：locked` 的强制顶部字段，由 `loop-harness req bind` 的 `parseUIImpact`（`internal/transition/engine.go`）读取。值必须三选一：`none` / `changed` / `unknown`。`unknown` 会触发规划暂停门禁，需在 §11 澄清后才能继续。

## 1. PM Todo

每个需求开始前必须先填写本区。阶段、负责人、阻塞或验收状态变化时必须更新。

| 字段 | 内容 |
|:---|:---|
| 当前阶段 | S0 / S1 / S2 / S3 / S4 / S5 / S6 / S7 / S8 / S9 / S10 / S11 |
| Runtime 引用 | `.claude/loop-state.json#{runtime-id}@{revision}` / inactive |
| Runtime 状态 | `{state}.{phase}` / inactive |
| Bound REQ | `REQ-{id}` / none |
| 已完成 | {已落盘的证据} |
| 下一步 | {一个具体动作} |
| Owner / 协作方 | {PM / 用户 / Architect / Frontend Builder / Backend Builder / Test Builder / Document Verifier / Delivery Verifier / QA / E2E Tester} |
| 阻塞 | {无 / 缺少门禁 / 未知项 / 依赖} |
| 验收剩余项 | {测试、报告、用户验收、交付事项} |

## 2. 背景

{问题和上下文}

## 3. 目标

| 目标 | 衡量方式 |
|:---|:---|
| {目标} | {指标或验收证据} |

## 4. 范围

范围内：

- {范围内事项}

范围外：

- {范围外事项}

## 5. 用户和利益相关者

| 角色 | 诉求 | 决策权 |
|:---|:---|:---|
| {角色} | {诉求} | 高 / 中 / 低 |

## 6. 术语

| 术语 | 含义 | 禁止叫法 |
|:---|:---|:---|
| {术语} | {含义} | {容易混淆的叫法} |

## 7. 功能需求

| 编号 | 模块 / 流程 | 需求 | 优先级 | 验收标准 |
|:---|:---|:---|:---|:---|
| FR-001 | {模块} | {需求} | Must / Should / Could / Won't | {可测试结果} |

## 8. 非功能需求

| 类型 | 要求 | 验收标准 |
|:---|:---|:---|
| 性能 | {要求} | {指标} |
| 安全 | {要求} | {证据} |
| 可靠性 | {要求} | {证据} |
| 合规 | {要求} | {依据} |

## 9. 流程和异常

| 流程 | 输入 | 输出 | 状态 | 异常 | 验收标准 |
|:---|:---|:---|:---|:---|:---|
| {流程} | {输入} | {输出} | {状态} | {异常} | {证据} |

## 10. 迁移范围清单

> 轻量模式、迁移类需求、重构类需求即使跳过独立设计文档，也必须填写本区。本区是 task 拆分边界依据。

| 路径 / 文件 | 类型 | 处理动作 | 所属任务 | 引用 / 依赖 | 说明 |
|:---|:---|:---|:---|:---|:---|
| `{path}` | source / config / script / generated / test | migrate / keep / delete / review | TASK-{id} | `{import/script}` | {说明} |

范围检查：

- [ ] 所有目标文件、脚本、配置、生成物和测试已列入。
- [ ] 删除项已列出引用清理标准。
- [ ] 每个文件都能映射到唯一 task，或明确为共享边界。
- [ ] 任务拆分前已检查重复 import、脚本引用、生成物入口和别名路径。

## 11. UI 影响和原型门禁

> 任何涉及前端页面、组件、交互、可见状态、错误展示、权限展示或响应式布局变化的需求，必须填写本区。
> `UI impact = changed` 时，必须先完成 UI Design Package Gate，才能锁定 FE/BE/SYNC 合同。
> §11 的 UI impact 行直接引用顶部 blockquote——顶部是唯一被 `parseUIImpact` 解析的位置，本节不允许再独立声明，只能回显顶部值。

| 字段 | 内容 |
|:---|:---|
| UI impact（引自顶部） | none / changed / unknown |
| 影响页面 / 模块 | {页面、路由、组件或 N/A} |
| 模块原型集 | `docs/design/prototypes/<module>/` (index.html + stories.md + flows.md + *.html) / N/A |
| 原型门禁状态 | N/A / pending / ready |

门禁检查：

- [ ] 已判断 UI impact 为 `none` / `changed` / `unknown`。
- [ ] `UI impact = unknown` 时，不进入合同锁定。
- [ ] `UI impact = changed` 时，受影响模块的 `docs/design/prototypes/<module>/` 已存在或已创建。
- [ ] 模块原型集齐备：`index.html` + `stories.md` + `flows.md` + ≥1 页面 HTML。
- [ ] 每个 HTML 文件携带 4 字段 header（设计代数 / 更新 / 路由 / index 链接）。
- [ ] `stories.md` 覆盖 persona + S-NNN story；`flows.md` 覆盖 F-NNN flow。
- [ ] 模块原型集已链接到 FE/BE/SYNC 合同输入（目录路径 + 当前 fingerprint）。

## 12. 待澄清问题

| 编号 | 问题 | 影响范围 | 状态 | 结论 |
|:---|:---|:---|:---|:---|
| Q-001 | {问题} | 范围 / 流程 / 状态 / API / 权限 / 验收 | open / closed | {结论} |

## 13. 风险

| 风险 | 影响 | 缓解措施 |
|:---|:---|:---|
| {风险} | 高 / 中 / 低 | {缓解措施} |

## 14. 完整性检查

需求进入 `reviewed` 前必须全部满足：

- [ ] 用户、价值和范围清楚。
- [ ] 每个模块都有输入、输出、状态、异常、权限和验收标准。
- [ ] Must / Should / Could / Won't 已确认。
- [ ] 未关闭问题不阻塞当前范围。
- [ ] PM todo 已更新。
- [ ] 如采用轻量模式或迁移/重构类需求，迁移范围清单已完成。
- [ ] 顶部 blockquote 已声明 UI impact 取值为三选一之一（被 `parseUIImpact` 读取的权威位置；§11 行只是回显）。
- [ ] UI impact 已明确；若为 `changed`，已列出最终 UI 设计包计划。

## 15. 锁定记录

| 日期 | 操作 | 操作人 | 说明 |
|:---|:---|:---|:---|
| YYYY-MM-DD | locked | {PM / Architect} | 用户确认范围；UI impact={none/changed/unknown} 已随顶部字段固化 |

## 16. 派生文档与覆盖矩阵

### 派生 UI 设计包

| 模块原型集 | 路径 | 状态 | 覆盖需求 |
|:---|:---|:---|:---|
| <module> | `docs/design/prototypes/<module>/` | N/A / pending / ready | FR-{id} |

### 派生合同

| 合同 | 路径 | 状态 | 覆盖需求 |
|:---|:---|:---|:---|
| CONTRACTS-{id} | `docs/contracts/CONTRACTS-{id}.md` | draft / reviewed / locked | 当前 REQ 全量索引 |
| FE-{id} | `docs/contracts/FE-{id}.md` | draft / reviewed / locked | FR-{id} |

### 条款覆盖

| REQ 条款 | UI 设计包 | 合同条款 | TASK | 验证证据 | 状态 |
|:---|:---|:---|:---|:---|:---|
| FR-{id} | UI design package / N/A | FE/BE/SYNC-{id} §{n} | TASK-{id} | REV/QA/E2E/BUG/ACC-{id} | planned / covered / verified |

### 批次下游索引

| 关系 | 文档 | 路径 | 状态 |
|:---|:---|:---|:---|
| downstream | 任务看板 | `docs/tasks/index.md` | draft / executing / completed |
| evidence | Review round manifest | `{team-manifest-path}` | pending / complete |
| evidence | Delivery verification | `docs/reports/review/REV-{id}.md` | pending / PASS |
| evidence | QA evidence | `docs/reports/qa/QA-{id}.md` | pending / PASS |
| evidence | E2E Tester evidence | `docs/reports/e2e/E2E-{id}.md` | pending / PASS |
| evidence | Clean round | `{review-evidence-path}` | pending / valid |
| evidence | 验收 | `docs/reports/acceptance/ACC-{id}.md` | pending / passed |
| downstream | 发布审计 | `docs/release_audits/{file}.md` | pending / approved |

## 17. 变更记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 |
|:---|:---|:---|:---|:---|
| | | | | |
