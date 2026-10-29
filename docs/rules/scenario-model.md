# Module Scenario Model Rule

---
rule_id: R-SCENARIO-MODEL-01
category: Design / Testing
status: locked
owner: Project Manager / Architect
scope: module facts, rules, cases, stories, flows, prototypes, fixtures, Playwright specifications, and verification evidence
---

## 1. Rule

每个受影响模块只维护一套当前最正确的事实场景包。它是模块的 current truth，
不是某个 REQ、review round 或历史版本的副本。`UI impact` 决定是否需要 UI 资产，
不决定 FR-targeted 业务行为是否进入 CASE 分母：没有 UI 的服务行为仍使用本模块
场景包和稳定 CASE ID。

```text
locked business facts/rules
  → generated current cases
  → behavior-role stories + operation flows
  → (browser_required=true) PATH + Playwright spec
  → round-scoped CASE execution evidence
```

REQ 只能出现在 `source_refs` 或证据追踪表中，说明规则/行为的来源；REQ 不拥有
独立的 cases、stories、flows、prototype 或 Playwright spec。

## 2. Module Package

受影响模块的唯一设计包路径为：

```text
docs/design/prototypes/<module>/
├── index.html
├── stories.md
├── flows.md
├── scenario-model.json
├── cross-matrix.json
├── cases.json            (generated)
├── scenario-coverage.json (generated)
├── fixture-contract.json
└── *.html
```

该完整包用于需要 UI 原型的模块行为。UI impact=`none` 但有 AC 指向 FR，或
模块的服务行为真相发生变化时，仍在相同模块目录维护 **7 文件服务行为包**：手写
`scenario-model.json`、`fixture-contract.json`、`cross-matrix.json`、
`stories.md`、`flows.md`，以及由 `scenario generate` 生成的 `cases.json`、
`scenario-coverage.json`。此路径不产出 `index.html`、页面 HTML、PATH 或
浏览器 spec。Story 描述 API caller/worker 的业务意图，F-NNN 描述请求、校验、
响应和持久化顺序；branch 使用 `browser_required:false`。AC→FR 的 CASE 仍是
S2→S7 的验证分母，不因 `ui_impact=none` 或 `browser_required:false` 消失。

UI impact=`none` 只有在没有 FR-targeted AC、所有 AC 均有受控 N/A、且无需变更模块行为真相时才可跳过
服务包；AC 的 N/A 仍只接受 REQ 已声明的 NFR ID 或 §A4 指针。不得用 `none`、
`browser_required:false` 或自由文本代替 N/A。

永久 Playwright 定义位于：

```text
web/e2e/<module>/**/*.spec.ts
```

上述定义文件不包含 `version`、`generation`、`round`、`status`、`req_owner` 或
类似历史副本字段。历史差异由 Git 保存；一次执行的 PASS/FAIL、截图、trace 和
观察结果由 round-scoped 报告保存。

## 3. Strict JSON Contract

第一版只接受严格 JSON，所有对象拒绝未知字段；文件必须是 UTF-8、两空格缩进、
末尾换行并按稳定 ID 排序（cross-matrix.json 与两个生成文件无此约束）。三个手写输入和两个生成输出的顶层允许字段如下：

| 文件 | 顶层允许字段 | 权威性 |
|:---|:---|:---|
| `scenario-model.json` | `module`, `coverage_profile`, `facts`, `rules` | 人工锁定的规则输入 |
| `cases.json` | `module`, `cases` | 生成器的当前输出，只读派生物 |
| `scenario-coverage.json` | `module`, `coverage_profile`, `counts`, `required_branch_coverage`, `ratio` | 生成器的当前输出，只读派生物；具体 CASE/branch 映射以 `cases.json` 的 `branch_id` / `id` 为准 |
| `fixture-contract.json` | `module`, `fixtures` | 人工声明的合成前置事实契约 |
| `cross-matrix.json` | `module`, `entries` | 手写的事实×需求×故事汇聚载体，引用真实分支或说明无分支原因 |

`module` 必须等于父目录名并符合 lowercase-kebab。禁止把 `REQ-{id}`、round 或
版本拼入模块目录或永久测试定义路径。

### 3.1 `scenario-model.json`

- `coverage_profile` 只能是 `ordinary`、`rule-dense`、`critical`。
- `facts` 非空；每个 fact 只允许 `id` 与非空 `partitions`。
- 每个 partition 只允许稳定 `id` 与合成 `value`；不得写生产 PII、生产快照或
  未声明的随机值。
- `rules` 非空；每条 rule 必须有唯一 `id`、非空 `source_refs`、`risk` 和非空
  `branches`。
- 每个 branch 必须有唯一 `id`、唯一 `case_id`、`title`、`polarity`、`required`、
  `witness`、`oracle`、`fixture_id`、`story_refs`、`flow_refs` 和
  `browser_required`；风险等级属于父级 rule 的 `risk`。
- 服务行为 branch 仍必须引用至少一个 S-NNN 和一个 F-NNN；当前 schema 不接受空
  `story_refs` / `flow_refs`。纯服务 flow 只需 F-NNN，可不含 PATH；`browser_required`
  必须显式设为 `false`。该标记只表示不要求浏览器 spec，不是 N/A 或 CASE 豁免。
- `polarity` 只能是 `positive` 或 `negative`。`witness` 的每个键必须引用现有
  fact，值必须引用该 fact 的 partition。
- 所有 oracle 的公共字段必须包含 `visible`、`terminal_state`、
  `persisted_effects`、`forbidden_side_effects`。真实无持久变化时 `persisted_effects` 可为 `[]`，字段仍须保留，不用填充“none”等文字。
- negative oracle 必须同时包含上述四个公共字段，并额外包含 `rejection`、
  `expected_state`、`recovery`。
- negative oracle 没有恢复路径时必须写 `recovery: "N/A"`，同时提供非空
  `recovery_source_refs` 与 `recovery_reason`；每个 recovery source ref 必须属于
  当前 rule 的 `source_refs`。
- 每个 `required` branch 都是一项不可省略的 coverage obligation；不能通过不生成
  branch、静默丢弃约束组合或把未知项写成 N/A 来规避门禁。

### 3.2 `fixture-contract.json`

顶层只允许 `module` 和 `fixtures`。每个 fixture 只允许稳定 `id`、`persona`、
`synthetic: true`、非空 `setup` 和非空 `cleanup`。每个 required branch 必须引用
一个 fixture；未被使用的 required fixture、空 cleanup、生产数据依赖均失败。

Fixture 只能建立被测业务动作开始前本来就应存在的事实，例如 persona、权限、草稿、
字典、时钟或受控外部响应。Fixture 不得执行登录后的页面操作、填写、保存、提交、
审批、取消、重试或其他被测动作来替代浏览器路径。

### 3.3 Generated outputs

`cases.json` 与 `scenario-coverage.json` 只能由确定性生成器刷新，不能人工编辑一条
case 来绕过缺口。生成器按 branch witness、稳定 ID、约束和 set-cover 规则输出；
同一 `scenario-model.json` 与 `fixture-contract.json` 必须得到字节稳定的输出。

生成器不得从生产实现反推 oracle，不得调用生产服务决定 expected outcome。无法满足
的事实、未知引用、重复 ID 或规则冲突必须 fail-closed，并指向 source/ref。

## 4. Positive / Negative Coverage

正反数量比仅作风险参考，不是生成、校验或接收门槛；不得为达到比例凑分支，也不得删掉真实 allow/reject 分支。

| `coverage_profile` | positive:negative 风险参考比 |
|:---|:---:|
| `ordinary` | `1:1` |
| `rule-dense` | `1:2` |
| `critical` | `1:3` |

现有 counts/ratio 生成输出保留；`minimum_negative_to_positive` 为兼容字段名，表示上述参考值，不是拒收阈值。统计引用生成物，不另行手抄多份计数或数量证明。

真实分支覆盖仍必须满足：

- 每个 required allow branch 至少有一个 positive CASE；
- 每个 required reject branch 至少有一个 negative CASE；
- required allow branch coverage = 100%；
- required reject branch coverage = 100%；
- negative CASE 必须断言 visible、terminal state、persisted effects、forbidden side
  effects、rejection、expected state 和 recovery；recovery N/A 必须携带来源与理由；
- 没有拒绝语义的规则必须有来源可追溯的 N/A 说明，不能通过遗漏 negative branch
  获得 PASS。

CASE 可以在低层测试中承担多个输入组合，但不同角色、拒绝原因、终态、权限结果或
恢复语义不得被合并为一个含糊的 case。browser-required CASE 必须至少有一个
required PATH；每个 PATH 必须有明确主 CASE。服务 CASE 的 F-NNN 表达操作顺序，
无需 PATH，也无需浏览器页面。

## 5. Traceability Chain

设计验证必须能沿以下链路双向检查：

```text
source_refs → Rule → required branch/CASE → S-NNN Story → F-NNN operation flow
browser_required=true → PATH-* → web/e2e/<module>/*.spec.ts → round Evidence
browser_required=false → suitable service-level CASE execution → round Evidence
```

`stories.md`、`flows.md`、适用的原型 HTML、scenario JSON 和模块 spec 都表达当前
模块完整集合。任意 REQ 触及模块时，必须更新并重新核对该模块全集，并执行
full-module regression；只补本 REQ 的路径不满足规范。服务 CASE 不要求 UI 原型或
Playwright，但必须按服务合同/TASK 指定的非浏览器入口执行，并由 S7 记录 CASE 级证据。

## 6. Fail-closed Gates

- S2：UI 行为包（含 UI 资产）或服务行为包（无 UI 资产）所需源文件齐备，schema 合法、
  当前模块全集已重算、required obligations 全覆盖、正反容量比满足、没有
  per-REQ/per-round 永久副本；每个 FR-targeted AC 可达 CASE 或受控 N/A。
- S5：独立验证 Rule→CASE→Story→F-flow 链；只有 browser-required CASE 继续检查
  PATH→Spec。核对 oracle 独立于实现、fixture 可复现且可清理、allow/reject branch 均 100%。
- S6：Test Builder 按 UI 或服务 TASK 在适用的 spec/test 入口实现 fixture 与检查；不得修改 locked
  oracle 或用 fixture 跳过被测动作。
- S7：从声明入口执行当前模块全部 required CASE，并对 browser-required CASE 执行其
  PATH；逐步断言可见结果、终态、
  持久化和禁止副作用，并记录 console/network/trace 证据。
- 缺少 case、branch、story、fixture cleanup 或证据时，结果为 FAIL，不是 N/A；
  仅当 CASE 标记 `browser_required:true` 时，缺少 PATH/spec 才是 FAIL。N/A 必须有
  可审查的来源和理由。

## 7. Forbidden

- `USER-STORY-{REQ-id}-*`、`USER-FLOW-{REQ-id}-*`、`web/e2e/{REQ-id}-round{N}-*`
  等永久定义。
- 按 REQ、round、v1/v2 或 addendum 复制模块事实、用例、故事、动线、原型或 spec。
- UI CASE 不得用直接提交 API、手改数据库、隐藏 URL 或共享可变账号绕过被测用户动作；
  服务 CASE 可从被测服务的正式入口调用，但不得手改持久化结果或绕过规则执行。
- 只断言 HTTP 200、toast、URL 或“没有抛错”而不验证业务终态和拒绝副作用。
- 失败后修改 expected outcome 使当前测试变绿。

## 覆盖、来源与可识别定义

- `scenario-coverage.json` 的 `required_branch_coverage` 是**构造性 100%**（引擎对每个 required case 同时计数 required 与 covered）——它是**设计时声明覆盖**，不是执行覆盖；**不得作为执行覆盖证据**。执行覆盖由 S7 的验证证据计数承担，并遵守对应需求的 CASE_ID 粒度完整性门。
- **非 UI 不等于没有 CASE 宇宙**：只要 AC 指向 FR，仍需在模块 current truth 建立服务 CASE；7 文件包不含 HTML、PATH、Playwright。仅当所有 AC 都以已声明 NFR 或 §A4 背书 N/A、且模块行为无变化时，才可不产出场景包。该边界保持 L2 的 CASE 单一分母与 PTR-PLAN-02 AC bridge。
- **case id 格式**：`^CASE-[A-Z0-9]+(-[A-Z0-9]+)*$`（引擎强制）——case id 是 S2→S7 的唯一验证分母（L2 全局规则「单一验证分母」）。
- **cross-matrix.json**：UI 包的一项手写输入；服务行为包也必须包含它。它承载汇聚①的事实×需求点×故事交叉清单——每格指向覆盖它的 branch 或记录无分支理由；"沉默不是不适用"。机器地板（`scenario generate/validate` 强制）：每个 fact、每个 story 至少出现在一格（fact×story 组合本身是猎杀判断，不要求笛卡尔积）；branch 格的 rule 必须在 `source_refs` 真实引用该格的 `REQ-<id>/FR-<id>`。当前需求的格连接绑定 REQ 的实际 FR 表，历史格保留自己的来源，不能挪作本次义务；无分支理由须 ≥8 字符且含字母（讲清 why——"."或"不需要"不是背书）。
- **AC↔CASE 桥**：`scenario bridge`（源头检查，汇聚①后即可跑）验证 AC→FR 源头可达；`scenario validate` 校验模块包、BR→CASE 与生成物；PTR-PLAN-02 检查完整 FR→BR→CASE 链。每条验收标准须达 CASE 或带背书 N/A（NFR 编号或 §A4 指针；自由文本不是 N/A）。
- **S-NNN 恰好三位数字**（`S-001`，引擎按 `S-[0-9]{3}` 匹配）：`S-1`/`S-1234` 不会被识别为 story 引用（引用校验与 cross-matrix 地板两侧同时不认，静默失效）。F-NNN 同理三位。
- **定义在标题中**：每个被引用的 Story 和操作动线 ID 都写入实际 Markdown 标题，例如 `## S-001：提交批次`、`### F-001：处理请求`。浏览器路径使用准确 `PATH-*` 标题。目录表、正文提及和元数据表不能替代定义；纯服务动线不需要 PATH。


## Shared model authority

Follow [shared-model contracts](shared-model-contracts.md). Data shape has one native authoring source; SYNC owns shared behavior, FE/BE derive responsibilities. Structural negative examples fail schema validation; business negatives remain valid data and are rejected by the business rule.
