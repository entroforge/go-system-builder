# Project Design Foundation

> 状态：draft / in-review / provisional / published / superseded
> 版本：v0.1.0
> 发布日期：YYYY-MM-DD
> 投资档位：core / extended
> 方向路径：thin / full
> 主 Surface：consumer / operations / {name}
> 确认记录：方向 {date} · 内核 {date} · 发布 {date}（任一 PENDING 则状态只能是 provisional，不得 published）

后续 changed REQ **只打开两份文件**：本文件 **§0 + §8**，以及 `derivation/REQ-{id}.md`。
不要打开 `research/`、`design-language.md`、`surface-profiles/`，除非 §0 回答不了 Must not，或本 REQ 是第二 Surface。
整包目标同时满足 ≤120 行、≤12 KB。禁止把施工 hex 写成品牌。

Core+thin 只填 §0、§8、§10。§1–7 可留一句可排除的 Thesis，或整段标 skip。

## 0. Next-agent card

<!-- foundation-contract:v1 constraints -->
| ID | Status | 类型 | 可执行 Statement / Do | Don't / 反向边界 | Scope | Source | Binding | Checkability | Proof / review |
|:--|:--|:--|:--|:--|:--|:--|:--|:--|:--|
| LAW-01 | active | law | | | global | EVD-01 | GR-01, ROLE-action-promise | human, static | PROOF-01 |
| ANTI-01 | active | anti | — | | global | EVD-02 | GR-02 | human | PROOF-01 |
| INV-01 | active | invariant | | | all-surfaces | LAW-01 | GR-03 | static | PROOF-02 |

规则：active 行必须有 Binding 和 Proof/复查条件；暂未消费只能标 `reserved/debt`。库主色只允许写 semantic role，不写 hex。本 Kernel 不回答架构、模块边界、数据或契约。

§0 不完整，不得 `published`。换手会实际继承、因此必须写进卡的三件事：

1. 唯一实心 CTA 的语义角色（转述 / 打开这份单 / …），禁止 hex。
2. 结果**数值**和**状态标签**如何着色；库 success/danger 不是默认。
3. 咨询、加项、分享等承诺：哪些是按钮，哪些只能是句子。

## 1. Design Worldview
{1～3 句：产品如何看待用户、行业、服务和价值。禁止只写“高级/简约”。}

## 2. Relationship Model
{产品以什么角色与用户相处：服务者 / 工具 / 导师 / 管家 / …}

## 3. Design Thesis
{句式：不是类别默认角色，而是独特关系；依靠何种证据；
让用户在核心任务中获得何种状态；始终采取的姿态；拒绝的反向边界。}

自检：可生成？可排除？可组合？可适配？可追溯？五项中任一项失败则回到 F1。

## 4. Core Tensions

| 张力 | 当前偏向 | 不可越过的边界 | 允许反向的场景 |
|:--|:--|:--|:--|
| 严谨 vs 速度 | {偏向} | {边界} | {条件} |
| 透明 vs 威严 | {偏向} | {边界} | {条件} |

## 5. Constraint rationale index（可选解释，不是第二份定义）

| ID | 为什么长期保留 / 接受什么代价 | 详细解释或决策路径 |
|:--|:--|:--|
| LAW-01 | | `design-language.md#constraint-explanations` |

LAW/ANTI/INV 的 Statement、Do/Don't、Scope 与 Binding 只在 §0 定义。这里不得改写；没有 Do/Don't 的 Law 不得进入 F3 确认。

## 6. Rejected direction history

| 决策 | 被拒方向 | 原因 | 可保留元素 |
|:--|:--|:--|:--|
| DFD-01 | | 引用 §0 的 ANTI-* | |

## 7. Signature Relations
- {少数跨页面可识别的关系或品牌动作}

## 8. Surfaces in force
<!-- foundation-contract:v1 surfaces -->
单 Surface：**本表就是 SUR 差分**，Profile/version 填 `inline`。不要为消费者端再建 `surface-profiles/consumer.md`。
第二 Surface 出现时，才复制 `surface-profiles/surface-profile-template.md`，并把本表该行改成文件引用。

| ID | Surface | Profile/version | 密度/姿态（相对主 Surface） | 与主 Surface 的对比证明 |
|:--|:--|:--|:--|:--|
| SUR-01 | {consumer} | `inline` | 主 Surface 基准 | PROOF-0X |

## 9. Proof Set
<!-- foundation-contract:v1 proofs -->
| ID | 类型 | 路径 | 证明哪些约束 |
|:--|:--|:--|:--|
| PROOF-01 | Style Tile | `proof/style-tiles/` | LAW-01, ANTI-01 |
| PROOF-02 | Anchor/Stress | `proof/anchor-screens/` | INV-01, GR-01 |
| PROOF-03 | Golden Flow（Extended） | `proof/golden-flows/` | |

## 10. Open design debt
<!-- foundation-contract:v1 debts -->
| ID | 项 | 影响 | 复查条件 |
|:--|:--|:--|:--|

## 11. How to inherit
后续 UI REQ **只读 §0 + 本文件 §8 当前行** + `derivation/REQ-{id}.md`。
不要为了「找齐 Foundation」去打开 Grammar、Evidence Field 或 Profile 文件。
施工 hex 不是品牌。禁止把局部冠军组件静默升为全局规范。
