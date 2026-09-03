# L5 — 项目级设计基础落地方案（Project Design Foundation Implementation）

> 层：第五层｜机制域：把 [L4 项目级设计基础](L4-project-design-foundation.md) 翻译为模板、Skill、S0/S2 消费协议、Token、UI Lab 与有限机械检查
>
> 上游：L4 Design Kernel / Grammar / Surface Profiles / Proof Set；L1 D1～D7；L3-S0、L3-S2
>
> 下游：`docs/design/**` 模板、`skills/design-foundation`、S0/S2 补丁、`packages/design-tokens`、`tools/ui-lab`、`tools/visual-qa`、顾问型 `loop-harness design-foundation`
>
> 状态：v1.0.2，实现规格。本文是 L4 §15 十二条的唯一落地权威。P0～P4 已在本仓库落地；L6 是观察协议而非假产品回放。Runtime 仍无 Foundation 硬门。

## 0. 决策摘要

### 0.1 要落地的是什么

L4 已经固定因果链：产品真相 → Kernel → Grammar → Surface → Proof → REQ 推导 → 回灌。L5 只回答三件事：

1. 这些对象在本仓库的**哪个路径、哪份模板、哪个 Skill**里被写出；
2. Agent 在**哪条已有自然路径**上读取它们，而不新增 Loop Stage；
3. 哪些检查可以立刻用提示词和字段逼问，哪些必须等观察后再下沉为机械门。

### 0.2 对参考范式的转译结论

读完 L4 §12 所列实践后，落地时只采用其机制，不复制其审美或产品原则：

| 实践 | 本仓库怎么用 | 明确不做什么 |
| --- | --- | --- |
| IBM Design Language / Carbon | `DESIGN.md` + `design-language.md` 是语言；Token、组件、Storybook 是系统。语言先于系统 | 不把 Element Plus / Shadcn / 任何默认库当 Kernel |
| Google DESIGN.md | 作为**派生便携快照**，从已发布 Foundation + Token **生成**，供跨工具原型 | 不把 Google 的 YAML+八节正文当成项目权威；不把全部 Token 塞进入口文档 |
| Atlassian DESIGN.md 实测 | 生产路径用 Skill 按需加载 + 组件查询 + 零 Token 的 lint；避免巨型一次性上下文 | 不让 Agent 按规格重写 Button，必须先查现有组件 |
| Style Tiles | F2 用 2～3 张 HTML Style Tile 比较设计世界 | 不把 Tile 当最终规范和完整页面 |
| Double Diamond | F1/F2 发散、F3/F5 收敛；失败按 L4 §2.3 有界回退 | 不新增 S0.5 / Runtime 阶段 |
| NN/g Journey Mapping | 只在 `evidence-field.md` 组织角色、阶段、行动、心智、情绪、机会 | 不把旅程图当视觉方向 |
| Aesop / Work & Co | F1 先做品牌考古与服务仪式，再转译数字关系 | 不抄其版式、字体或配色 |
| USWDS Principles | Design Law 必须含信念、依据、后果、代价、Do/Don't、证明 | 不复制政府产品的具体原则 |
| USWDS Tokens | 有限语义角色，禁止页面级任意值 | Token 不能替代 Grammar |
| DTCG 2025.10 | P2 起 Token 源文件用社区 JSON 格式 | P0/P1 不冻结色值和构建链 |
| Atomic Design | Style Tile 与 Anchor/Stress Screen 并行证明，组件只在页面上下文中成立 | 不按 atoms→pages 流水线生产 |
| Storybook MCP | P3 让 Agent 查询现役组件、Stories、测试 | 实时组件知识不能改写 Kernel |
| Playwright snapshots / Storybook visual | P4 只查实现漂移 | 像素相等 ≠ 方向正确 |
| Atlassian Motion | Grammar 的 Motion 维：高频安静、关键时刻强化 | 不为每个 hover 加动效 |

Google DESIGN.md 与 Atlassian 的冲突，按 L4 §9.2 分层权威解决：入口文档保持短而有判断力；值、组件 API、实时知识分别住在 Token、UI Lab 和代码里。

### 0.3 非目标

- 不新增 Loop Stage、cursor 或 `PTR-*`；
- 不在 P0/P1 增加审美硬门或“字段完整度即设计合格”；
- 不改 `UI impact` 三值解析（`none/changed/unknown` 仍是 S1/S2 的唯一机器锚）；
- 不把现有 `docs/design/prototypes/` 改名为 L4 示意中的 `modules/`；
- 不把本模板仓库自己做成一个消费端产品的 Design Kernel——模板只提供**空壳与方法**，目标项目在首个 UI REQ 前填写。

### 0.4 与现有事实的对齐

| 现有事实 | 落地时如何接 |
| --- | --- |
| REQ 顶部 `UI impact` 由 `parseUIImpact` 解析 | 只**追加** Foundation 引用字段；三值 ENUM 与 parser 不动 |
| S2 模块包在 `docs/design/prototypes/<module>/` | L4 §9.1 的 `modules/<module>/` **映射到该路径**，不新建第二套包 |
| `ui-prototyping` 内联了一套 slate/blue CSS 变量 | P1 起改为读取 Foundation Token；P0 先在 sidebar 增加推导区 |
| `specification-planning` Step 0 按 UI impact 分流 | 在 `changed` 分支增加“读 Foundation → 写 Derivation Note” |
| `requirement-funnel` 在 S0 开始 | 增加 F0 检查；缺失且 `UI impact=changed` 时先走 `design-foundation`，再继续漏斗 |
| PTR-PLAN-01 不检查 Foundation | P0～P3 保持顾问态；P4 才考虑存在性检查 |
| 模板项目尚无统一 Token/Storybook 接线 | P2/P3 才引入；目标项目若已有 UI 库，Grammar 约束库，库不定义 Kernel |

## 1. 仓库物理落点

权威资产按“语言 / 证明 / 实现”三层放置。实现层可以晚于语言层存在。

```text
docs/design/
├── DESIGN.md                                 # 入口：Kernel、状态、版本、继承说明
├── DESIGN-template.md                        # 模板（本仓库分发）
├── research/
│   ├── evidence-field.md
│   └── evidence-field-template.md
├── design-language.md                        # Grammar
├── design-language-template.md
├── surface-profiles/
│   ├── surface-profile-template.md
│   ├── consumer.md                           # 目标项目填写；模板可给示例名
│   └── operations.md
├── proof/
│   ├── README.md
│   ├── style-tiles/                          # 候选与选定 HTML
│   ├── anchor-screens/
│   └── golden-flows/
├── decisions/                                # 方向选择与 breaking change；沿用 ADR-template
├── exceptions/
│   └── EXCEPTION-template.md
├── derivation/
│   └── DERIVATION-template.md                # REQ 级 Design Derivation Note
└── prototypes/<module>/                      # 现有 S2 模块包，不改名
    └── derivation.md                         # 可选：模块当前推导摘要；权威仍在 derivation/

docs/rules/design-foundation.md               # Agent 默认行为（提示词层）

skills/design-foundation/SKILL.md             # F0～F6 方法论
skills/design-critic/SKILL.md                 # 独立方向评审（D6）

# P2 起才创建，P0 不得假装已存在
packages/design-tokens/                       # DTCG JSON 源
packages/ui/                                  # 组件与 Stories
tools/ui-lab/                                 # Storybook / MCP 入口说明
tools/visual-qa/                              # Golden Screen 与 Playwright snapshot

# 派生、非权威
docs/design/portable/DESIGN.md                # Google DESIGN.md 导出，由 Token+Kernel 生成
```

路径纪律：

- Agent 与人始终从 `docs/design/DESIGN.md` 进入；
- `prototypes/` 继续回答“这个模块现在长什么样、有哪些 CASE/PATH”；
- `derivation/` 回答“这个 REQ 为何这样长”；
- `portable/DESIGN.md` 可以缺失；缺失时禁止手写一份与 Grammar 冲突的 Google 格式文件。

## 2. 对象 → 模板 → 消费者

| L4 对象 | 权威文件 | 引导模板 | 主消费者 | 机器在 P0/P1 做什么 |
| --- | --- | --- | --- | --- |
| Evidence Field | `research/evidence-field.md` | `evidence-field-template.md` | F1、方向评审 | 不检查 |
| Design Kernel | `DESIGN.md` | `DESIGN-template.md` | 全部 UI Agent | 不检查审美；P4 可查文件存在 |
| Design Grammar | `design-language.md` | `design-language-template.md` | S2、原型、前端实现 | 不检查 |
| Surface Profile | `surface-profiles/*.md` | `surface-profile-template.md` | S0 Surface 声明、S2 推导 | 不检查 |
| Proof Set | `proof/**` | `proof/README.md` 清单 | F5 人验收、S7 走查 | 不把截图当方向事实 |
| Derivation Note | `docs/design/derivation/REQ-<id>.md` | `DERIVATION-template.md` | S2 T2/T3、S5 语义审查 | P1 起作为 S2 清单项，非 gate |
| Exception | `exceptions/EX-<id>.md` | `EXCEPTION-template.md` | S0、S2、修订 | 不检查 |
| Decision | `docs/design/decisions/ADR-*` 或 `DFD-*` | 现有 `ADR-template.md` | F3/F6 人确认 | 沿用 ADR 人闸约定 |
| Token | `packages/design-tokens` | P2 schema | 原型 CSS、实现、portable 导出 | P2 起 lint 引用 |
| UI Lab | Storybook + MCP | P3 | 实现 Agent | 查询，不生成第二套组件 |

## 3. 分阶段落地

观察信号一律来自 L4 §11.4。未观察到稳定失效，不得跳阶段加硬门（D5、成本公理）。

```text
P0 模板与主动切入     → 证明 Agent 会停下来做 Foundation
P1 S0/S2 消费协议     → 证明每个 UI REQ 能写出推导而不是猜风格
P2 Token 分层         → 证明值从 Grammar 来，页面不再写任意色
P3 UI Lab / MCP       → 证明 Agent 复用组件而不是重写
P4 有限机械检查       → 只公证存在性、引用版本、重复组件、截图漂移
```

### 3.1 P0 — 模板、规则、主动切入（本批次应完成的规格）

**目标**：目标项目在理解产品或遇到首个 UI 工作前，Agent 会检查 Foundation；缺失时进入 F0～F6，而不是直接画页面。

**要新增的文件**

| 文件 | 作用 |
| --- | --- |
| 本节 §5 列出的全部 `*-template.md` | D4 引导性产物 |
| `docs/rules/design-foundation.md` | 项目理解 / S0 / S2 的默认行为 |
| `skills/design-foundation/SKILL.md` | F0～F6 过程 |
| `skills/design-critic/SKILL.md` | 独立评审候选方向，产出者不得自签 |

**要修改的文件（补丁规格见 §6）**

| 文件 | 补丁 |
| --- | --- |
| `docs/project-map-template.md` | Baseline Index 增加 Design Foundation 一行 |
| `docs/agent-protocol.md` S0 inputs / actions | 增加 Foundation 检查；不改 `done_when` |
| `docs/agent-protocol.md` S2 inputs / actions | `changed` 时读取 Foundation 并写 Derivation Note |
| `skills/requirement-funnel/SKILL.md` | Required Inputs + Procedure 增加 F0 |
| `skills/specification-planning/SKILL.md` | `changed` 路径增加消费步骤 |
| `skills/ui-prototyping/SKILL.md` | sidebar 增加「设计推导」；Visual Tokens 改为引用 Foundation |
| `blueprint/L3-S0-requirement-design.md` | T3 增加 Foundation 引用，不重写 Kernel |
| `blueprint/L3-S2-design.md` | T2/T3 增加 Derivation Note |
| `blueprint/README.md` | L5 索引 |

**P0 Definition of Done**

- 模板可被目标项目复制后直接填写；
- `design-foundation` Skill 能独立走完 F0～F6，三次人确认点写进 `DESIGN.md` 与 `decisions/`；
- `requirement-funnel` 在可能的 UI 工作前会停下来检查 `DESIGN.md`；
- 文档与 Skill 都不声称 Runtime 会阻断缺失的 Foundation；
- 用 §8 工作示例能填出一份合格 Kernel，而不是形容词清单。

**P0 明确不做**：Token 包、Storybook、Hook、REQ parser 新字段、视觉回归。

### 3.2 P1 — S0/S2 字段协议

**目标**：前端相关 REQ 声明继承关系；S2 在画模块包之前写出 Derivation Note。

**字段（全部为人/Agent 语义字段，S1 parser 不读）**

在 `REQ-template.md` §C「UI 影响」表追加：

| 字段 | 合法值 | 谁填 |
| --- | --- | --- |
| Foundation reference | `docs/design/DESIGN.md@vX.Y.Z` / `pending-foundation` / `N/A` | S0；`UI impact=none` 可为 N/A |
| Surface | `consumer` / `operations` / `{profile}` / `N/A` | S0 |
| Design posture | `inherit` / `extend` / `exception` | S0 初判，S2 可细化 |
| Derivation note | `docs/design/derivation/REQ-{id}.md` / `N/A` | S2 在 `changed` 时必填 |

纪律：

- `UI impact=changed` 且 Foundation 为 `pending-foundation`：先完成 F6 发布，再锁定 REQ 或从 S0 返回补齐；当前 Runtime 不阻断，Skill 必须把这写成默认行为；
- `extend` 必须在 Derivation Note 写清新增语法及其是否候选晋升；
- `exception` 必须同时有 `exceptions/EX-*.md`；
- S0 不复制 Kernel/Grammar 正文。

**S2 顺序（插在现有 T2 之后、双轨之前）**

```text
读 DESIGN.md + design-language.md + 当前 Surface Profile
  → 写 docs/design/derivation/REQ-<id>.md
  → 先做当前模块一张宏观构图 + 一个压力状态
  → 推导成立后再扩展其余 HTML / stories / flows
```

`ui-prototyping` 每个 HTML 的 `aside.proto-notes` 在「本原型目标」之后插入一节 `设计推导`：Foundation 版本、活跃 Laws、Experience role、Exception。这是对 Atlassian「页面必须携带意图」和本项目传达公理的落地，不是第二份 Foundation。

**P1 DoD**

- 一份 `UI impact=changed` 的示例 REQ 能指到 Foundation 版本和 Derivation Note；
- S5 文档审查清单增加“推导是否回指 Laws”，仍由人/Agent 判断，不写 schema 分数；
- 观察：新 REQ 是否还在问“喜欢什么颜色”。

### 3.3 P2 — Design Token

**目标**：值成为 Grammar 的实现载体。

源文件建议：

```text
packages/design-tokens/
  tokens.json                 # DTCG 2025.10
  README.md                   # 语义角色 ↔ Grammar 维对照
```

分层（L4 §5.6）：

```text
Design Law
  → Semantic Role          # 例如 action.promise / evidence.support / status.blocking
  → Primitive Token        # 色板、字号阶、间距阶
  → Semantic Token         # 角色到原语的别名
  → Component Token        # 仅组件内部
  → CSS variables / Tailwind theme
```

命名约束（USWDS 的“有限离散选择”）：

- 颜色语义只允许：`surface.*` `content.*` `action.*` `status.*` `brand.*`；
- 禁止 `page-login-button-hover` 这种页面级 Token；
- 每个 semantic token 必须在 `design-language.md` 对应维度有一条 Grammar Rule 可回指；
- 原型 HTML 使用 `var(--color-action-promise)` 一类变量，删除 `ui-prototyping` 里写死的 `#2563eb`。

Google DESIGN.md 导出（可选脚本，非权威）：

| 我们的源 | 导出到 Google 节 |
| --- | --- |
| `DESIGN.md` Thesis + Relationship | Overview |
| Grammar Color / Type / Shape / Elevation | Colors / Typography / Shapes / Elevation |
| DTCG semantic colors/type/spacing/rounded | YAML frontmatter |
| Laws Do/Don't + Anti-principles | Do's and Don'ts |
| **不导出**组件 API | 避免 Atlassian 发现的“按规格重写组件” |

**P2 DoD**：至少一条 Law 能从 Kernel 追到 semantic token；原型不再引入新的 hex。

### 3.4 P3 — UI Lab

当目标项目存在 Vue/React UI 时：

1. 用现有组件库搭 Storybook，开启 `componentsManifest`（Vue 需 `@storybook/vue3-vite` + `experimentalDocgenServer`）；
2. 安装 `@storybook/addon-mcp`，Agent 指令写明：改 UI 前先 `docs-list` / `docs-show`；
3. `frontend-engineering` 增加硬性质量条：现役组件能覆盖的语义，禁止平行实现；
4. 组件提案流程：新组件先作为模块局部 → 重复出现再提交 Grammar/Pattern 修订，禁止 REQ 静默升格。

**P3 DoD**：同一语义的按钮在两个模块中来自同一组件；Agent 不再从默认 Demo 生成第三种主按钮。

### 3.5 P4 — 视觉回归与有限机械检查

只公证可机判事实：

| 检查 | 判据 | 不判 |
| --- | --- | --- |
| Foundation 存在 | `UI impact=changed` 时 `DESIGN.md` 存在且状态为 `published` | Thesis 好不好 |
| 引用版本 | REQ 的 Foundation reference 能解析到 `DESIGN.md` 版本标题 | 推导是否忠实 |
| Derivation 文件 | `changed` 时路径存在 | 内容质量 |
| Token 引用 | 新 CSS 不引入未登记 hex（lint） | 配色是否品牌正确 |
| 组件重复 | 同名/近义组件超过阈值时警告 | 哪个冠军好看 |
| Golden Screen | Playwright/Storybook snapshot 相对上次基线漂移 | 方向是否过时 |

升级条件：P0～P3 在真实项目中出现「Agent 跳过 F0」「REQ 无引用仍继续」的稳定失效后，才把前三项挂到投影或顾问型 ready 提示。即使升级，也保持 D3：缺失时给出补齐路径，不把审美当 `fail-closed`。

## 4. Agent 在自然路径上的切入

不新增 Stage。切入点只有四条已有路径。

```text
项目理解 / 填写 project-map
  └─ 产品含用户可见界面？ → 读 DESIGN.md
        ├─ published 且覆盖当前 Surface → 记录继承，继续
        ├─ 缺失 / draft / 过期 / 新 Surface → skills/design-foundation（F0～F6）
        └─ 纯后端 → 只读，不启动定向

S0 requirement-funnel
  └─ 将出现 UI impact=changed
        ├─ Foundation published → §C 填引用
        └─ 否则暂停漏斗，先 F1～F6，人发布后再锁 REQ

S2 specification-planning（changed）
  └─ 读 Kernel/Grammar/Profile → 写 Derivation Note → 再双轨

S2 之后的发现
  └─ 按 L4 §7.4 回灌：一次性构图留模块；反复关系提修订；冲突走 Exception 或 breaking change
```

三次人确认（对话职责，不是 Gate）：

1. F2 结束：选哪个设计世界（Direction Set）；
2. F3 结束：确认 Thesis、Tensions、Laws、Anti-principles；
3. F6 前：确认 Proof Set 足以被后续 REQ 继承。

提问形态固定为 L4 §8.2：带推荐的 A/B，一次 ≤3 个价值问题。禁止问“喜欢什么颜色”。

独立 Critic：F2 候选方向提交给人之前，主会话应加载 `design-critic`（或派一个只读评审），检查 L4 §4.7 六维。产出者不得自己宣布方向合格（D6）。

## 5. 模板规格（P0 直接可复制）

以下模板是 D4 载体。花括号为填写位；注释行说明逼问目的，不是可删字段。

### 5.1 `docs/design/DESIGN-template.md`

```markdown
# Project Design Foundation

> 状态：draft / in-review / published / superseded
> 版本：v0.1.0
> 发布日期：YYYY-MM-DD
> 主 Surface：consumer / operations / {name}
> 确认记录：方向 {date} · 内核 {date} · 发布 {date}

## 1. Design Worldview
{产品如何看待用户、行业、服务和价值。禁止只写“高级/简约”。}

## 2. Relationship Model
{产品以什么角色与用户相处：服务者 / 工具 / 导师 / 管家 / …}

## 3. Design Thesis
{使用 L4 §4.2 句式：不是类别默认角色，而是独特关系；依靠何种证据；
让用户在核心任务中获得何种状态；始终采取的姿态；拒绝的反向边界。}

自检：可生成？可排除？可组合？可适配？可追溯？五项中任一项失败则回到 F1。

## 4. Core Tensions

| 张力 | 当前偏向 | 不可越过的边界 | 允许反向的场景 |
|:--|:--|:--|:--|
| {克制 vs 丰盛} | | | |

## 5. Design Laws

| ID | 名称 | 一句话后果 | 详细定义 |
|:--|:--|:--|:--|
| LAW-01 | {短名} | {它改变什么关系} | `design-language.md` 中的编译节 |

每条 Law 的完整字段（信念/依据/后果/适用/代价/Do/Don't/证明）写在 `design-language.md`，此处只保留可扫描索引。

## 6. Anti-principles
- {明确拒绝的设计世界、行业惯性或表达方式}

## 7. Signature Relations
- {少数跨页面可识别的关系或品牌动作}

## 8. Surfaces in force
| Surface | Profile | 与主 Surface 的对比证明 |
|:--|:--|:--|
| {consumer} | `surface-profiles/consumer.md` | `proof/...` |

## 9. Proof Set
| 类型 | 路径 | 证明什么 |
|:--|:--|:--|
| Style Tile | `proof/style-tiles/` | 设计世界 |
| Anchor | `proof/anchor-screens/` | 理想核心场景 |
| Stress | `proof/anchor-screens/` | 高密度/失败/权限 |
| Golden Flow | `proof/golden-flows/` | 状态变化 |

## 10. Open design debt
| ID | 项 | 影响 | 复查条件 |
|:--|:--|:--|:--|

## 11. How to inherit
后续 UI REQ 读取本文 + `design-language.md` + 当前 Surface Profile，
在 `docs/design/derivation/REQ-{id}.md` 声明 inherit/extend/exception。
禁止把局部冠军组件静默升为全局规范。
```

`DESIGN.md` 正文建议控制在人一次能审完的长度（约 80～150 行）。组件 API、Token 表、页面清单一律不准写入。

### 5.2 `docs/design/research/evidence-field-template.md`

```markdown
# Evidence Field

> 对应 Foundation 版本：v0.x
> 更新：YYYY-MM-DD

来源标记：F = 已确认事实 · I = Agent 推断 · R = 外部参考 · U = 未知。
参考不得冒充本项目事实。

## 产品本质
| 条目 | 标记 | 来源 |
|:--|:--|:--|

## 用户关系
| 角色 | 希望被如何对待 | 标记 | 来源 |
|:--|:--|:--|:--|

## 业务证据
| 能力 / 数据 / 流程 / 标准 | 为何难以复制 | 标记 |
|:--|:--|:--|

## 服务仪式
| 时刻 | 线下/既有动作 | 可转译的数字关系 |
|:--|:--|:--|

## 类别惯例
| 惯例 | 必须保留的可用性 | 可以反转的默认 |
|:--|:--|:--|

## 约束条件
| 约束 | 对设计世界的影响 |
|:--|:--|

## 旅程摘要（可选，NN/g 结构）
| 角色 | 场景 | 阶段 | 行动 | 心智 | 情绪 | 机会 |
|:--|:--|:--|:--|:--|:--|:--|

## 未知项
| ID | 问题 | 阻塞 F2？ |
|:--|:--|:--|
```

### 5.3 `docs/design/design-language-template.md`

每条规则使用 L4 §5.1 句式。九个维度各至少一条，允许声明“本产品该维暂弱”并写入债务，而不是用数值表充数。

```markdown
# Design Grammar

> 编译自：DESIGN.md@vX.Y.Z
> 不变量 / 受控变量 / 选择规则 / 例外 必须齐全。

## LAW-01 {名称}

信念：
依据：{Evidence Field 条目}
设计后果：
适用条件：
代价：
Do：
Don't：
证明：{Proof 路径}

### 编译

因为【LAW-01】所以在【场景】优先【关系】避免【冲突】允许【受控变化】通过【Proof】判断。

| 维度 | 消费者端 | 运营端 |
|:--|:--|:--|
| Information | | |
| Composition | | |
| Color | | |
| Typography | | |
| Shape & Surface | | |
| Image & Icon | | |
| Interaction | | |
| Content | | |
| Motion | | |

## Invariants
- {所有 Surface 必须保持的关系}

## Variants
- {可随 Surface/任务/密度变化的部分}

## Selection Rules
| 条件 | 选用模式 | 不要选用 |
|:--|:--|:--|
| 探索 | | |
| 判断 | | |
| 承诺 | | |
| 等待 | | |
| 恢复 | | |

## Semantic roles（P2 起与 Token 对齐）
| 角色 | 含义 | Grammar 回指 | Token（P2） |
|:--|:--|:--|:--|
| action.promise | 用户将做出可追踪承诺 | LAW-01 | |
```

### 5.4 `docs/design/surface-profiles/surface-profile-template.md`

```markdown
# Surface Profile — {name}

> 继承：DESIGN.md@vX.Y.Z
> 对比证明：{proof path}

## 用户、任务、关系
## 密度 / 解释深度 / 决策速度 / 错误成本
## 导航、输入、反馈、恢复姿态
## 必须保留的 Invariants
## 允许调整的 Variants
## 品牌表达可以出现的位置与强度
## 与其他 Surface 的同源证明
```

### 5.5 Proof 清单 `docs/design/proof/README.md`

Style Tile 最低内容（L4 §4.6）：版式与字体关系、色彩角色、表面层级、图像/图标/材质、主次行动与状态、一段真实内容、一个成功态、一个失败/空态、对应 Thesis/Laws/Anti-principles/风险。

技术形态：单张静态 HTML，**不暗示设备尺寸**（Style Tiles 对 responsive 的原意）。候选方向各一文件：`proof/style-tiles/direction-a.html`。选定后保留落选文件，供决策追溯。

Anchor / Stress / Golden Flow 使用现有原型 HTML 约定（四字段头），但存放在 `proof/`，不替代模块 `prototypes/`。模块包证明“当前业务全集”；Proof Set 证明“语言能否辐射”。

### 5.6 `docs/design/derivation/DERIVATION-template.md`

```markdown
# Design Derivation — REQ-{id}

> Foundation：docs/design/DESIGN.md@vX.Y.Z
> Surface：{profile}
> Experience role：探索 / 理解 / 判断 / 承诺 / 等待 / 兑现 / 恢复
> Posture：inherit / extend / exception

## Active laws
- LAW-0X：{本 REQ 如何表现}

## Macro composition
{先写一页的信息顺序：证据 → 判断 → 行动，或本 Surface 的对应顺序}

## Stress state
{高密度 / 长内容 / 错误 / 权限 中至少一项}

## New language
{无 / 新增 Pattern 或 Token 候选；不得在此直接改 DESIGN.md}

## Exception
{无 / EX-id}

## Proof
{本 REQ 用哪个页面/状态/流程证明推导成立}
```

### 5.7 `docs/design/exceptions/EXCEPTION-template.md`

```markdown
# EX-{id}

> 范围：{模块/页面/状态}
> 期限：YYYY-MM-DD
> 复查条件：
> 是否可能晋升为全局：yes / no

## 偏离哪条 Law / Grammar
## 业务必须如此的理由
## 影响面
## 禁止扩散到
```

### 5.8 Design Law 详细卡（写入 Grammar，不写入 DESIGN.md 索引）

字段与 L4 §4.4 一一对应，缺任何一项视为形容词原则，F3 不得确认。

## 6. 现有文件补丁规格

### 6.1 `docs/project-map-template.md`

在 §3 Baseline Index 增加：

```markdown
| design foundation | `docs/design/DESIGN.md` | missing / draft / published / N/A | {version} |
```

在 §1 Project Facts 增加：

```markdown
| product surfaces | {consumer / operations / none} | confirmed / unknown |
```

### 6.2 `docs/rules/design-foundation.md`

规则要点（完整正文实现 P0 时按此写）：

- `rule_id`: `R-DESIGN-FOUNDATION-01`
- 范围：所有可能改变用户可见界面的工作
- 默认行为：先读 `DESIGN.md`；状态不是 `published` 且工作会改 UI，则加载 `design-foundation` Skill
- 禁止：用 REQ §B 几句风格描述替代 Foundation；问低信息审美问题；把 UI 库默认 Demo 当品牌
- 升级：本文是提示词层；机械门见 L5 §3.5

### 6.3 `skills/requirement-funnel/SKILL.md`

Required Inputs 增加一行：`docs/design/DESIGN.md`（若项目包含用户可见界面）。

Procedure 在现有第 0 条拍板手势之后插入：

```text
0.5 Foundation check: if the expected outcome may change screens, visual
    behavior, copy, motion, or user-visible states, read DESIGN.md.
    missing/draft/stale/uncovered surface → stop the funnel, load
    skills/design-foundation, finish F6 publish, then resume §A.
    published → continue; later §C only records the version reference.
```

Non-Goals 增加：不在 S0 重写 Kernel。

### 6.4 `skills/specification-planning/SKILL.md`

Required Inputs 增加 Foundation 三件套。`changed` 路径在 Track 开始前插入 Step 0.5：写 Derivation Note；宏观构图与压力态未完成前，不得铺满模块全部 HTML。

修正既有缺口（L3-S2 已记录）：`none` 不得跳过架构。本补丁不得复制该错误。

### 6.5 `skills/ui-prototyping/SKILL.md`

- Required Inputs 增加 Derivation Note 与 `design-language.md`；
- sidebar 强制节在「本原型目标」后增加「设计推导」；
- Visual Tokens 一节改为“从 Foundation semantic token 生成 CSS 变量”；P2 之前允许沿用变量名，但必须在注释写明“待替换的临时值”，禁止新增第二套调色盘；
- Quality Bar 增加：Derivation Note 的 Active laws 能在本页指出对应区域。

### 6.6 `docs/agent-protocol.md`

S0 `inputs` 增加 Design Foundation。`actions` 增加：判定 UI 时检查 Foundation；缺失则先 F0～F6。`done_when` **不**增加 Foundation 文件——避免空壳文档过门。

S2 `inputs` 增加 Foundation 与 Derivation Note。`actions` 在 UI 包之前增加推导步骤。`done_when` P0/P1 不把 Derivation 做成硬谓词；P4 再议。

### 6.7 L3-S0 / L3-S2

按 L4 §10 各加一小节“消费 Project Design Foundation”，只声明消费，不复制定义。S0：引用字段。S2：Derivation Note + 宏观先于细节。诚实缺口保留：PTR-PLAN-01 仍不检查 Foundation。

## 7. Skill 规格

### 7.1 `skills/design-foundation/SKILL.md`

| 项 | 内容 |
| --- | --- |
| category | methodology |
| description | Use when a project has user-visible UI but Project Design Foundation is missing, stale, or cannot cover a new surface |
| Entry | 新项目理解；首个 `UI impact=changed`；新 Surface；风格无法解释新价值 |
| Inputs | 产品事实、`evidence-field`、既有品牌/线下体验、约束、L4 本文 |
| Non-Goals | 不画完全部页面；不改 Loop cursor；不代替 S2 模块包 |
| Stop | 人未确认方向/内核/发布；证据不足导致候选只是换皮；Thesis 无法排除方案 |

过程必须按 F0→F6 编号产出，回退表抄 L4 §2.3。每步交付物路径使用本文 §1。

F2 强制：2～3 个 Kernel 候选，差异至少落在产品角色、信息秩序、用户关系、审美世界、交互姿态、品牌表达位置中的三项。每个候选配一张 Style Tile。禁止三张换色皮肤。

创造力来源检查表（L4 §4.5）：证据放大、仪式迁移、类别反转、跨域类比、约束转资产、反例先行——每个候选至少使用其中两种，并在方向 ADR 里写明。

### 7.2 `skills/design-critic/SKILL.md`

| 项 | 内容 |
| --- | --- |
| category | best-practice |
| description | Use after design-foundation has produced 2–3 direction candidates and before asking the human to pick one |
| 六维 | Truth / Distinctiveness / Generativity / Elasticity / Usability / Feasibility |
| 输出 | 推荐、代价、落选理由、可保留元素；不得只写“A 获胜” |

Critic 只读 Evidence Field 与候选 Tile/Kernel，不改写成最终 Grammar。

## 8. 工作示例（合格样本，不是通用答案）

以下示例把 L4 §4.2 的专业服务命题写满，供模板自检。目标项目必须用自己的证据重写，禁止复制结论。

### 8.1 Thesis

> 我们不把产品设计成功能和促销的集合，而把它设计成一位有判断力、克制且愿意解释的专业服务者；依靠透明依据和可兑现流程，让用户在关键选择中保持从容与掌控。因此信息先建立依据再邀请行动，决定可预览、可确认、可恢复，并拒绝用装饰、压迫式强调和模糊承诺制造价值感。

### 8.2 张力

| 张力 | 偏向 | 边界 |
| --- | --- | --- |
| 权威 vs 可理解 | 先给依据，再给术语 | 不把专业内容藏进悬浮层 |
| 克制 vs 品牌时刻 | 高频操作安静 | 交付/确认可更丰盛 |
| 速度 vs 可恢复 | 承诺前可预览 | 不可逆操作必须二次确认且可追踪 |

### 8.3 LAW-01 编译摘录

因为「先建立依据，再邀请行动」，所以在核心选择流程中，证据区、判断区、行动区顺序固定；强调色不出现在证据之前；消费者端 CTA 在条件与后果齐备后出现；运营端批量执行在来源、状态、影响范围齐备后出现。Motion 只解释选择到结果的因果，不制造紧迫倒计时。

Anchor：核心选择 + 提交。Stress：支付/提交失败与恢复。运营对比：批量操作 + 部分失败。

### 8.4 Derivation Note 摘录（假想 REQ-014 结账确认）

```text
Surface: consumer
Experience role: 承诺
Active laws: LAW-01 先依据后行动；LAW-02 强调稀缺
New language: 无
Exception: 无
Proof: checkout-confirm.html 成功态 + 失败恢复态
```

若 Agent 写不出 Active laws，说明它在猜皮肤，F0 应判 Foundation 未被消费。

### 8.5 不合格反例（落地验收时用于对抗）

| 反例 | 为何失败 |
| --- | --- |
| Kernel = “现代、简约、科技蓝” | 不可生成、不可排除 |
| 三张 Tile 只换主色 | 三张皮肤 |
| REQ §B 写“参考淘宝但高级一点” | 用风格句替代 Foundation |
| 把 Element Plus 默认按钮当品牌主按钮 | UI 库反客为主 |
| 把 `DESIGN.md` 写成组件 API 大全 | 规范巨型化（Atlassian/Google 都反对以此为生产唯一事实） |

## 9. 验证分层如何接线

| L4 验证 | P0/P1 | P2 | P3 | P4 |
| --- | --- | --- | --- | --- |
| Derivation Fidelity | Derivation Note + 人宏观验收 + Critic | 同上 | 组件用法是否符合 Laws | 仍非像素 |
| System Coherence | Proof Set 人工对比 | Token lint | Storybook 查询与复用 | snapshot 漂移 |
| Experience Effectiveness | F5 场景走查 | — | — | S7 真实验证 / 业务反馈 |

S7 继续验证模块 CASE/PATH；Foundation 不替代单一验证分母。Golden Screen 失败时先判基准过期还是实现漂移（L4 §1.3）。

## 10. 安装、迁移与目标项目启用

模板仓库安装时，随 `docs/` 与 `skills/` 分发空模板。目标项目启用步骤：

1. 复制 §5 模板到 `docs/design/`，保持 `DESIGN.md` 为 `draft`；
2. 在 `project-map.md` 登记 Foundation 状态；
3. 若产品无用户可见界面：状态填 `N/A`，后续 UI 出现时再进入 F0；
4. 首个 UI REQ 前跑 `design-foundation`，三次确认后置 `published`；F4 后替换 `packages/design-tokens` 原语并 `emit-css`；
5. 已有页面的存量项目：允许先对**一个主 Surface**做 F1～F6，把现有页面当作 Stress 样本而非 Kernel 来源；禁止“搜集全部弹窗选冠军”；
6. 有 Vue/React UI 时按 `tools/ui-lab/README.md` 接入 Storybook MCP；视觉回归按 `tools/visual-qa/README.md`；
7. 定期 `loop-harness design-foundation check --root .`（顾问态）。不要把它挂到 `validate --all`。回放记入 L6 模板。

存量迁移禁令：不得把当前最好看的模块包改名为 Foundation。

## 11. 任务拆分（实现批次）

对应 L4 §15，但给出本仓库可执行任务。P0～P4 已落地；L6 提供观察协议，真实产品数据仍待目标项目填写。

| ID | 批次 | 任务 | 回指 L4 | 状态 |
| --- | --- | --- | --- | --- |
| DF-T01 | P0 | 落地 §5 全部模板到 `docs/design/` | §9 资产形状 | done |
| DF-T02 | P0 | 新增 `docs/rules/design-foundation.md` | §8 Agent 默认行为 | done |
| DF-T03 | P0 | 新增 `skills/design-foundation` 与 `design-critic` | §2.2、§4.7、§8 | done |
| DF-T04 | P0 | 补丁 project-map、requirement-funnel、agent-protocol S0 | §2.1、§10.2 | done |
| DF-T05 | P0 | 补丁 L3-S0 / L3-S2 消费声明 | §10 | done |
| DF-T06 | P1 | REQ §C 四字段 + DERIVATION 模板启用 | §7.2 | done |
| DF-T07 | P1 | specification-planning / ui-prototyping 补丁 | §7.1、§10.4 | done |
| DF-T08 | P2 | DTCG `tokens.json` + 语义角色表 + 原型 CSS 切换 | §5.6 | done |
| DF-T09 | P2 | portable DESIGN.md 导出（可选） | Google 格式采用边界 | done |
| DF-T10 | P3 | Storybook MCP 与 frontend-engineering 复用条 | Atlassian / Storybook MCP | done |
| DF-T11 | P3 | 组件提案与重复检测流程 | §7.4、失败模式「局部冠军」 | done |
| DF-T12 | P4 | Golden Screen + snapshot；顾问型存在性检查 | §11、§13.1 | done |
| DF-T13 | L6 | 用一个真实 UI 项目回放：是否减少横向返工与风格追问 | §15.12 | done（协议；产品数据待填） |

DF-T01～T13 已在本仓库落地。DF-T13 的完成态是观察协议与记录模板，不是一份虚构产品的 DESIGN.md。Skill 不得把 Runtime 写成已强制检查 Foundation。`design-foundation check` 默认顾问态，不进入 `validate --all`。

## 12. 验收

P0 规格本身通过，当且仅当：

1. 每个新对象都能指回 L4 的 Kernel/Grammar/Profile/Proof/Derivation，没有第三套概念；
2. 每个参考范式都有“采用 / 不照搬”的仓库落点；
3. 与 `UI impact` parser、`prototypes/` 路径、PTR-PLAN-01 范围的冲突都已显式和解；
4. 三次人确认仍是对话职责；
5. 工作示例能通过 L4 §1.4 五性质，反例会被 Skill 拒绝。

P0 实现通过，当且仅当：用工作示例走一遍 F0～F6，人只做三次确认，产出可被一份假想 REQ-014 的 Derivation Note 引用，且 Agent 不再询问颜色偏好。

P2～P4 实现通过，当且仅当：`tokens.css` 由 `tokens.json` 生成且原型 Skill 不再内联 hex；portable 导出省略组件节；`frontend-engineering` 要求复用现役组件；`design-foundation check` 在本模板工厂零 warning、缺 Foundation 的 changed REQ 上给出顾问警告且默认 exit 0；L6 写清本仓库不能冒充产品回放。

## 变更记录

### v1.0.2 — 2026-09-03

- P2～P4 落地：DTCG Token 包与 `emit-css`、portable DESIGN.md 导出（不含组件 API）、UI Lab / visual-qa 接线、组件提案与重复检测、顾问型 `loop-harness design-foundation check`（不进 `validate --all`）。
- DF-T13 以 [L6 回放协议](L6-design-foundation-replay.md) 完成；本仓库不填写假 Kernel。

### v1.0.1 — 2026-09-03

- P0/P1 模板、规则与 Skill 已落入本仓库：`docs/design/*-template.md`、`docs/rules/design-foundation.md`、`skills/design-foundation`、`skills/design-critic`，以及 S0/S2 消费补丁。Runtime 仍不机械检查 Foundation。

### v1.0.0 — 2026-09-03

- 将 L4 §15 十二条展开为本仓库可执行的 P0～P4 落地方案；
- 转译 IBM/Google/Atlassian/Style Tiles/USWDS/DTCG/Storybook MCP 等参考为具体路径与禁令；
- 和解 L4 示意路径 `modules/` 与现役 `prototypes/`；
- 规定 Google DESIGN.md 仅为派生快照；
- 给出模板全文、Skill 入口、S0/S2 补丁点、工作示例与任务切片。
