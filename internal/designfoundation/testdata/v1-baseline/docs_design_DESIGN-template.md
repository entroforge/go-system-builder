# Project Design Foundation

> 状态：draft / in-review / published / superseded
> 版本：v0.1.0
> 发布日期：YYYY-MM-DD
> 主 Surface：consumer / operations / {name}
> 确认记录：方向 {date} · 内核 {date} · 发布 {date}

本文是项目级设计入口：只保留可生成、可排除的 Kernel。组件 API、Token 表和页面清单不准写入。
完整语法在 `design-language.md`；Surface 适配见本文件 §8；证明在 `proof/`。

**后续 `UI impact=changed` 的 Agent 开工前只强制读本节 §0 + 本 REQ 的 Derivation Note。** 需要时再打开 Thesis / Grammar。禁止把上一页施工用的 hex 或组件库 Primary 当成品牌锁。

## 0. Next-agent card

> 换手契约。填不满这一节不得 `published`。下一任不得跳过这一节去读世界观作文。

| 项 | 可执行内容（禁止形容词） |
|:--|:--|
| 产品角色（一句话） | {例：柜台管家，不是行情销售} |
| Laws — 必须做 | {3～7 条 Do，抄自 §5 的后果句} |
| Laws — 不得做 | {与上面对应的 Don't} |
| Anti-principles | {明确拒绝的类别默认} |
| 禁止出现的 CTA / 承诺 | {例：列表与对比页不得出现购买；主行动可以是带时间戳的回执} |
| 数字与「更优」着色 | {例：收益/费率用正文色，不用红绿当气氛；禁止「更低」绿徽章} |
| 强调色何时才出现 | {例：证据齐了之后，且一页只有一个主焦点} |
| 库默认主色 | {语义角色名，例如 `--color-action`。此处不写 hex。实现值在 tokens.json} |
| 本 Kernel 不回答 | {架构、模块边界、数据模型、契约 — 走 S2} |

## 1. Design Worldview

{产品如何看待用户、行业、服务和价值。禁止只写“高级/简约/科技”。}

## 2. Relationship Model

{产品以什么角色与用户相处：服务者 / 工具 / 导师 / 管家 / …}

## 3. Design Thesis

{使用下列句式：

我们不是把【产品】设计成【类别默认角色】，
而是把它设计成【独特关系/世界】；
依靠【真实业务或品牌证据】，
让用户在【核心任务】中获得【目标状态/能力】。
因此产品始终【关键设计姿态】，并拒绝【反向边界】。
}

自检：可生成？可排除？可组合？可适配？可追溯？五项中任一项失败则回到 F1，不要发布。

## 4. Core Tensions

| 张力 | 当前偏向 | 不可越过的边界 | 允许反向的场景 |
|:--|:--|:--|:--|
| {克制 vs 丰盛} | | | |

## 5. Design Laws

| ID | 名称 | 一句话后果 | 详细定义 |
|:--|:--|:--|:--|
| LAW-01 | {短名} | {它改变什么关系} | `design-language.md` 中的编译节 |

每条 Law 的完整字段（信念/依据/后果/适用/代价/Do/Don't/证明）写在 `design-language.md`。此处只保留可扫描索引。3～7 条。没有 Do/Don't 的条目不能进入 F3 确认。

## 6. Anti-principles

- {明确拒绝的设计世界、行业惯性或表达方式}

## 7. Signature Relations

- {少数跨页面可识别的关系或品牌动作}

## 8. Surfaces in force

单 Surface 项目直接填下表即可；多 Surface 时再为新增 Surface 复制 `surface-profiles/surface-profile-template.md`。

| Surface | 继承 | 密度/姿态差异 | 对比证明 |
|:--|:--|:--|:--|
| {consumer} | 本 Kernel/Grammar | {主 Surface 基准} | `proof/anchor-screens/{consumer}.html` |
| {operations} | 同上 | {更密/更任务导向} | `proof/anchor-screens/{operations}.html` |

## 9. Proof Set

| 类型 | 路径 | 证明什么 |
|:--|:--|:--|
| Style Tile | `proof/style-tiles/` | 设计世界（HTML，F2 每候选 1 份） |
| Anchor | `proof/anchor-screens/` | 理想核心场景（HTML） |
| Stress | `proof/anchor-screens/` | 高密度 / 失败 / 权限（HTML） |
| Golden Flow | `proof/golden-flows/` | 状态变化（HTML） |

Figma 链接不得作为本表任何一项的权威引用。

## 10. Open design debt

| ID | 项 | 影响 | 复查条件 |
|:--|:--|:--|:--|

## 11. How to inherit

后续 UI REQ **先读 §0**，再读当前 Surface 行，需要时打开 `design-language.md`。
在 `docs/design/derivation/REQ-{id}.md` 声明 inherit / extend / exception，并填写 Must not。
施工笔记里的 hex 不是品牌。例外与组件提案走 `docs/design/decisions/`（见 `decisions/README.md`）；禁止把局部冠军组件静默升为全局规范。
