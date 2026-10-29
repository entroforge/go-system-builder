# 需求：REQ-{id}

> Dispatch policy: waves-v1

> 名称：{需求名称}
> 状态：draft / locked / changed / archived
> （此状态跟踪 REQ 文档自身的生命周期，不记录责任进度；责任进度使用 `docs/rules/state-machine.md` 的五态语义。）
> （draft→locked 的翻转由 agent 依据人类在对话中的明确"锁定"拍板执行，见 skills: requirement-funnel）
> Owner：{产品/用户}
> PM / Architect：{姓名}
> 版本：v0.1.0
> 创建日期：YYYY-MM-DD
> UI impact：none / changed / unknown

> `UI impact` 是 `状态：locked` 的强制顶部字段，原绑定接收只解析这一处（`req bind` CLI仅兼容恢复）（§C 的回显必须与之一致，不一致会被拒绝）。值必须三选一：`none` / `changed` / `unknown`。`unknown` 会触发规划暂停门禁，需在 §D 澄清后才能推进 S2。

<!-- 给 agent（固定阅读，2 行）：
若预期效果可能改界面：先按 design-foundation 判定 local/core/extended；local 使用模块推导，core/extended 缺覆盖才走 F0–F6；再按 §A→§B→§C 推进。
提案纪律与自审见 requirement-funnel skill——先完成调查与完整提案，再把真实未决的意图和价值选择交人；已明确的决定不重复索取。 -->

> S0负责真实需求判断和人的明确锁定，不另交S0问卷/证据。已明确锁定并启动可直接承接S1原四问题，无需机械再次批准；只有锁定时，启动授权仍留空等待。姓名/日期是原决定记录，不是身份认证。下游设计/合同/测试栏按责任阶段维护，不要求S0提前填写或造N/A。

## §A 理念（Why）

> A1 是对真实意图的结构化理解：消除口语歧义，核实隐含前提，依据已有明确决定形成整理稿。仅在真实意图仍有关键歧义时交人澄清；已确认的同一内容不为填写 A1 再确认。原始材料可作来源，需求共识以经消歧的整理稿为准，不能将推断写成已获批准的事实。

| 字段 | 内容 |
|:--|:--|
| A1 预期效果（agent 整理稿，人确认） | {做什么、为什么值得做——两三句} |
| A2 背景与痛点 | {问题和上下文}{现在没有它时用户怎么凑合} |
| A3 成功长什么样 | {完成后用户可感知的变化——验收的精神锚} |
| A4 明确不做（负空间/negative space） | {范围外事项——与 A1 同等重要，每条一句不做的原因；编号供 AC 指向列做 N/A 背书（`§A4-2` 即本表第 2 条）} |

用户和利益相关者（决策权"高"的诉求冲突 = 拍板点候选）：

| 角色 | 诉求 | 决策权 |
|:--|:--|:--|
| {角色} | {诉求} | 高 / 中 / 低 |

术语（分歧当场定名；统一叫法供全部下游文档使用）：

| 术语 | 含义 | 禁止叫法 |
|:--|:--|:--|
| {术语} | {含义} | {容易混淆的叫法} |

## §B 方向与约束（How 的边界——需求级选型，架构归 S2）

方向取舍（每个被否方向必须有一句否决理由——ADR 雏形，S2 承接）：

| 方向 | 一句话方案 | 采纳/否决 | 理由 |
|:--|:--|:--|:--|
| {方向 1（推荐标 ★）} | | 采纳 | {为什么} |
| {方向 2} | | 否决 | {一句否决理由} |

硬约束（agent 对人给的每条"必须"追问打破的代价；答不出代价的不是必须，降级为偏好并入下方假设与风险表）：

| 硬约束 | 为什么是必须 |
|:--|:--|
| {约束} | {代价/理由} |

关键假设与风险：

| 假设 / 风险 | 影响 | 缓解措施 / 验证方式 |
|:--|:--|:--|
| {假设或风险} | 高 / 中 / 低 | {缓解或验证} |

## §C 具体需求（What——逐条可追溯）

> 正向范围 = 本节需求点全集（FR + NFR）；§A4 只登记负向。

需求点（"服务于"指不回任何 §A 条目 = 范围蔓延，当场质疑）：

| 编号 | 模块 / 流程 | 需求 | 服务于 §A_ | 优先级 |
|:--|:--|:--|:--|:--|
| FR-001 | {模块} | {需求} | {A1/A2/…} | Must / Should / Could / Won't |

流程和异常：

| 流程 | 输入 | 输出 | 状态 | 异常 | 权限 | 验收标准 |
|:--|:--|:--|:--|:--|:--|:--|
| {流程} | {输入} | {输出} | {状态} | {异常} | {谁能做/谁被拒} | {证据} |

验收标准（换个人来判，结果一样；**指向列填写规则**——多个引用用逗号分隔，可混合 `FR-{id}`、`NFR-{id}`、`§A4-{条目}`。FR/NFR 须已声明；含 FR 仍须对应真实覆盖，附带 NFR/§A4 不免除 FR 覆盖。S0 只填引用，CASE 由后续设计完成。无 FR 时才可用 NFR 或 §A4 背书 N/A；自由文本（如“人工检查”）不是合法引用）：

| 编号 | 标准 | 指向 |
|:--|:--|:--|
| AC-001 | {可判定的验收表述，含衡量指标} | FR-{id} / NFR-{id} / §A4-{条目编号} |
| AC-002 | 示例：提交成功后 3 秒内可见回执 | FR-003 |
| AC-003 | 示例：不承诺移动端适配 | §A4-2 |

非功能需求：

| 编号 | 类型 | 要求 | 验收标准 |
|:--|:--|:--|:--|
| NFR-001 | 性能 | {要求} | {指标} |
| NFR-002 | 安全 | {要求} | {证据} |
| NFR-003 | 可靠性 | {要求} | {证据} |
| NFR-004 | 合规 | {要求} | {依据} |

数据迁移范围清单（轻量模式、迁移类、重构类需求即使跳过独立设计文档也必须填写本区；本区是 task 拆分边界依据）：

| 路径 / 文件 | 类型 | 处理动作 | 所属任务 | 引用 / 依赖 | 说明 |
|:--|:--|:--|:--|:--|:--|
| `{path}` | source / config / script / generated / test | migrate / keep / delete / review | TASK-{id} | `{import/script}` | {说明} |

填写纪律：删除项在"说明"列注引用清理标准；每个文件映射到唯一 task 或明确标注共享边界；任务拆分前检查重复 import、脚本引用、生成物入口和别名路径。

UI 影响（任何涉及前端页面、组件、交互、可见状态、错误展示、权限展示或响应式布局变化的需求必须填写本区；`UI impact = changed` 时必须先完成 UI Design Package Gate 才能锁定 FE/BE/SYNC 合同）：

本模板中的 `pending/ready`、`open/closed`、`complete/PASS/approved` 等值依字段而定，表示资料包、问题、证据、专业结论或 Gate 事实，不是责任进度状态。责任 attempt 使用 `pending/active/blocked/done/cancelled`；其余字段仍按原文档/Schema/消费者填写，不从中推导实现责任 `done`。参见 [`docs/rules/state-machine.md`](../rules/state-machine.md#21-loop-responsibility-status)。

| 字段 | 内容 |
|:--|:--|
| UI impact（引自顶部） | none / changed / unknown（顶部 blockquote 是唯一被 `parseUIImpact` 解析的位置，本节只回显，不独立声明） |
| 影响页面 / 模块 | {页面、路由、组件或 N/A} |
| Foundation reference | `docs/design/DESIGN.md@vX.Y.Z` / `local` / `pending-foundation` / `N/A`（`none` 可为 N/A；`local` 表示本 REQ 使用模块级推导，不发布项目级 Foundation；`changed` 的 Core/Extended 必须为版本化引用，不得用风格句替代。S1 parser 不读本字段，仍只解析顶部 `UI impact`） |
| Surface | `SUR-0X@vX.Y.Z` / `local` / `N/A`（Core/Extended 使用 `SUR-*@version`；`local` 仅用于模块级推导；旧值 `consumer/operations` 视为 `SUR-01` 别名） |
| Design posture | `local` / `inherit` / `extend` / `exception`（`local` 仅配合 `local` Foundation；`extend` 须在 Derivation Note 写清新增语法及是否候选晋升；`exception` 须同时有 `docs/design/decisions/EX-*.md` (legacy `docs/design/exceptions/EX-*.md` still recognized)） |
| Derivation note | `docs/design/derivation/REQ-{id}.md` / `N/A`（`changed` 时由 S2 填写且必须为可解析路径；`local` 亦需模块 Derivation；S0 可留 pending） |
| 模块当前真相包 | UI impact=`changed`：`docs/design/prototypes/<module>/` 的 UI 包（含 HTML/index）；UI impact=`none` 但有 AC→FR 或服务行为真相变化：同目录 7 文件服务行为包（`scenario-model.json`、`fixture-contract.json`、`cross-matrix.json`、`stories.md`、`flows.md`、生成的 `cases.json`、`scenario-coverage.json`，无 HTML/PATH/浏览器 spec；若变化无可引用 FR 则走 REQ amendment）；仅当所有 AC 都指向已声明 NFR 或 §A4 且模块行为不变时为 N/A |
| 模块行为/原型包状态 | N/A / pending / ready（N/A 不能仅由 UI impact=`none` 推出；FR-targeted AC 必须保留 CASE） |

> 真相包齐备性、S/F-NNN 完整性与 branch 覆盖率由 S2 的 `loop-harness scenario validate` 校验，不在本节自查。

## §D 待澄清问题

| 编号 | 问题 | 影响范围 | 阻塞范围? | 状态 | 结论 |
|:--|:--|:--|:--|:--|:--|
| Q-001 | {问题} | 范围 / 流程 / 状态 / API / 权限 / 验收 | 是 / 否 | open / closed | {结论} |

## §E 锁定、必要决定与自审记录

锁定与真实价值选择由人决定，Main 如实记录。一次决定可以覆盖多个层次；没有发生过的独立确认不填写为事实。

| 日期 | 操作 | 操作人 | 说明 |
|:--|:--|:--|:--|
| YYYY-MM-DD | locked | {PM / Architect} | 用户确认范围；UI impact={none/changed/unknown} 已随顶部字段固化 |

实际必要决定的记录（复用已明确的人意，不要求§A/§B/§C分别重复批准；未发生的决定留空，不造拍板）：

| 层 | 日期 | 拍板人 | 方式 |
|:--|:--|:--|:--|
| {实际涉及的层，如 §A／§B／§C} | {实际决定日期} | {实际决定者} | {实际决定及适用范围；可引用同一次决定} |

{agent 自审结论——提议锁定前全审的输出，一段话：审过什么、修了什么、留了什么旁注}

## §F 派生文档与覆盖矩阵（**S0 不读不填**——整个 §F 由 S3 起的 CONTRACTS-{id} 索引维护，本表仅锁定时快照；跳到 §G/文末即可）

<!-- 活矩阵唯一居所在 CONTRACTS-{id} 索引；REQ 是锁定基线，agent 不填写本表 -->

### 派生模块行为 / UI 设计包

| 模块行为 / 原型集 | 路径 | 状态 | 覆盖需求 |
|:--|:--|:--|:--|
| <module> | `docs/design/prototypes/<module>/` | UI 包 / 7 文件服务行为包 / N/A（仅所有 AC 均有 NFR 或 §A4 背书且模块行为不变） | FR-{id} |

### 派生合同

| 合同 | 路径 | 状态 | 覆盖需求 |
|:--|:--|:--|:--|
| CONTRACTS-{id} | `docs/dev/contracts/CONTRACTS-{id}.md` | draft / reviewed / locked | 当前 REQ 全量索引 |
| FE-{id} | `docs/dev/contracts/FE-{id}.md` | draft / reviewed / locked | FR-{id} |

### 条款覆盖

| REQ 条款 / source_ref | Rule → CASE → Story → F-flow；browser-required 时再加 PATH → Playwright Spec | 合同条款 | TASK | 验证证据 | 状态 |
|:--|:--|:--|:--|:--|:--|
| REQ-{id}/FR-{id} | BR-{id} → CASE-{id} → S-{id} → F-{id}; 若 `browser_required=true` 再到 PATH-{id} → `web/e2e/{module}/*.spec.ts`；服务 CASE 用适用的原生服务测试验证 | FE/BE/SYNC-{id} §{n} | TASK-{id} | REV/QA/E2E/BUG/ACC-{id} | planned / covered / verified |

### 模块当前真相映射

REQ 只能作为来源，不能创建需求私有设计或测试副本。任何触及模块的 REQ 都必须维护模块全集并触发 full-module regression。

| 模块 | Rule / CASE | Story | PATH | Spec | Round evidence |
|:--|:--|:--|:--|:--|:--|
| {module} | `scenario-model.json` / `cases.json` | `stories.md` / S-{id} | `flows.md` / F-{id}; browser-required 才需 PATH-{id} | `web/e2e/{module}/`（browser-required 时）或服务原生测试 | REV/QA/E2E round {n} |

### 批次下游索引

下表的 `draft / executing / completed` 是下游索引或证据工件的原有跟踪值，
`pending / complete / PASS / passed / valid` 是对应证据/闸门结果值；它们都不是
责任状态。单项责任进度遵循 `docs/rules/state-machine.md` 的五态规则，并由原
接收者的当前 Runtime/receipt 投影；保留这些模板字段的原值，不让 Agent 手工填写通用状态。

| 关系 | 文档 | 路径 | 状态 |
|:--|:--|:--|:--|
| downstream | 任务看板 | `docs/dev/tasks/index-REQ-{id}.md` | draft / executing / completed |
| evidence | Review round manifest | `{team-manifest-path}` | pending / complete |
| evidence | Delivery verification | `docs/reports/review/REV-{id}.md` | pending / PASS |
| evidence | QA evidence | `docs/reports/qa/QA-{id}.md` | pending / PASS |
| evidence | E2E Tester evidence | `docs/reports/e2e/E2E-{id}.md` | pending / PASS |
| evidence | Clean round | `{review-evidence-path}` | pending / valid |
| evidence | 验收 | `docs/reports/acceptance/ACC-{id}.md` | pending / passed |
| downstream | 发布审计 | `docs/reports/release-audits/{file}.md` | pending / approved |

## 变更记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 |
|:--|:--|:--|:--|:--|
| 2026-08-15 | v2.0.0 | {示例行——首个真实变更从下一行开始} 重构为漏斗结构（§A 理念→§B 方向与约束→§C 具体需求→§D 澄清→§E 锁定→§F 覆盖矩阵）；删除 PM Todo、21 项自查 checklist 与设计公理五问（实质分别转入字段/S2 引擎）；状态枚举精简为 draft/locked/changed/archived；过程方法论由 requirement-funnel skill 承载；流程表补权限列、非功能表补 NFR 编号列 | owner | owner |
