# Proof Set

F5 只用最小而有张力的 HTML 样本证明语言可辐射。模块 `prototypes/` 证明当前业务全集；本目录证明 Kernel / Grammar 能否在真实业务和压力下成立。**HTML 为唯一合法载体，Figma 链接不得作为 Proof / Derivation 的权威引用**（仅可在 Evidence Field 中以 `R=外部参考` 提及方法）。

| 证据 | 目录 | 证明什么 |
|:---|:---|:---|
| Style Tile | `style-tiles/` | 设计世界和视觉语言是否一致 |
| Anchor Screen | `anchor-screens/` | 理想核心场景能否体现 Kernel 与 Grammar |
| Stress Screen | `anchor-screens/` | 高密度、长内容、错误或权限下是否仍成立 |
| Golden Flow | `golden-flows/` | 视觉与交互语法能否贯穿关键任务和状态变化 |

只在 Anchor 中成立的风格不具备系统资格，不能发布 Foundation。

## Style Tile

位于 Moodboard 与整页稿之间。**静态 HTML，不暗示设备尺寸**。每个候选方向一份：`style-tiles/direction-a.html`（必要时 `direction-b.html` / `direction-c.html`）。从 `STYLE-TILE-template.html` 复制，F2 期间各候选 1 份，F3 后保留落选文件供方向 ADR 追溯。人选择的是设计思想，不是“哪张图最好看”。

每张 Tile 至少包含：版式与字体关系、色彩角色、表面层级、图像/图标/材质、主次行动与状态、一段真实内容、一个成功态、一个失败或空态、对应的 Thesis / Laws / Anti-principles / 风险。

F2 Tile 可以使用候选 hex。F6 之后的 Anchor / Stress / Golden Flow 与模块原型必须引用 `packages/design-tokens/tokens.css`，不得再发明未登记色值。

## Anchor / Stress Screen

存放于 `anchor-screens/`，不替代 `prototypes/<module>/`。使用与模块原型相同的四字段 HTML 头（见 `docs/rules/ui-prototype.md`）。F5 后在此放置选定 HTML 证明。

## Golden Flow

存放于 `golden-flows/`，以 HTML 或关联流程证明关键任务的状态变化，而非单张漂亮截图。

## Portable 快照（可选派生，非权威）

目标项目可用 `loop-harness design-foundation export-portable --root .` 生成
`portable/DESIGN.md`。模板工厂**不检入**该快照，也不检入 Anchor / Golden Flow HTML。

不要手写该快照，不要把组件 API 塞入其中。查询现役组件请走 Storybook MCP（`tools/ui-lab/README.md`）。
