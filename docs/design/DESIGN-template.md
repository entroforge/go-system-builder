# Project Design Foundation

> 状态：draft / in-review / published / superseded
> 版本：v0.1.0
> 发布日期：YYYY-MM-DD
> 主 Surface：consumer / operations / {name}
> 确认记录：方向 {date} · 内核 {date} · 发布 {date}

本文是项目级设计入口：只保留可生成、可排除的 Kernel。组件 API、Token 表和页面清单不准写入。
完整语法在 `design-language.md`；Surface 适配在 `surface-profiles/`；证明在 `proof/`。

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

| Surface | Profile | 与主 Surface 的对比证明 |
|:--|:--|:--|
| {consumer} | `surface-profiles/consumer.md` | `proof/...` |

## 9. Proof Set

| 类型 | 路径 | 证明什么 |
|:--|:--|:--|
| Style Tile | `proof/style-tiles/` | 设计世界 |
| Anchor | `proof/anchor-screens/` | 理想核心场景 |
| Stress | `proof/anchor-screens/` | 高密度 / 失败 / 权限 |
| Golden Flow | `proof/golden-flows/` | 状态变化 |

## 10. Open design debt

| ID | 项 | 影响 | 复查条件 |
|:--|:--|:--|:--|

## 11. How to inherit

后续 UI REQ 读取本文 + `design-language.md` + 当前 Surface Profile，
在 `docs/design/derivation/REQ-{id}.md` 声明 inherit / extend / exception。
禁止把局部冠军组件静默升为全局规范。
