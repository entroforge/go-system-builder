# 模块当前行为包

本目录保存模块唯一、完整且当前有效的业务行为设计。有界面与纯服务模块使用同一套事实、规则、故事、操作动线和 CASE 关系；界面原型是适用时增加的表达，不是建立行为包的前提。

当前实现用于调查事实，包内文件表达当前目标与约束。目标可以已经实现，也可以是下一次锁定变更，但不能为每个 REQ、round 或版本另存一份模块真相。历史由 Git 与轮次证据保存。

## 根据真实行为选择资产

同一模块使用一个目录：

```text
docs/design/prototypes/{module}/
  scenario-model.json      # 手写：事实、规则、分支、见证和 oracle
  fixture-contract.json    # 手写：隔离的合成数据、设置与清理
  cross-matrix.json        # 手写：事实 × 需求点 × 故事的汇聚载体
  stories.md              # 手写：完整 S-NNN 故事集合
  flows.md                # 手写：完整 F-NNN 操作动线及适用 PATH
  cases.json              # 确定性生成，不手改
  scenario-coverage.json  # 确定性生成，不手改
  index.html              # 有界面时：链接全部当前页面/对话框/向导
  *.html                  # 有界面时：页面、状态和交互原型
  assets/                 # 按实际需要：图片、图标、原型资源
```

无界面需求只维护前七个行为文件，以 API 调用方或 worker 为角色，使用 `F-NNN` 操作动线及 `browser_required:false` 的服务分支，不创建 HTML、PATH 或浏览器 spec。`UI impact=none` 免除的是界面资产，不能免除 FR 对应的行为建模与验证。既有服务行为真相变化时也需要更新；若当前绑定没有相应 FR 来源，先处理需求修订。

有界面时，独立浏览器路径使用 `PATH-*`，与 CASE、branch、故事及模块 spec 关联。不同操作、数据前提或恢复动作应形成可独立复跑的路径；不能只写一条成功路径，把其他真实终点压成备注。只有业务入口本身是深链时才允许声明 URL 直跳。

## 每份材料回答什么

| 文件 | 职责与来源模板 |
| --- | --- |
| `scenario-model.json` | 当前事实分区、带真实需求来源的规则、显式 allow/reject 分支、见证及 oracle；参考 [规则模板](scenario-model-template.json) |
| `fixture-contract.json` | 合成隔离的 fixture 设置、清理与命名空间；参考 [fixture 模板](fixture-contract-template.json)，不能代替用户旅程执行 |
| `cross-matrix.json` | 每个实际调查的事实×FR×故事格指向覆盖分支或解释无分支原因；参考 [矩阵模板](cross-matrix-template.json)，沉默不是 N/A |
| `stories.md` | 当前完整故事集合，从角色、情境与目标解释规则和结果；参考 [故事模板](USER-STORY-template.md) |
| `flows.md` | 当前完整操作动线，有界面时展开实际浏览器步骤；参考 [动线模板](USER-FLOW-template.md) |
| `cases.json` | 当前生成 CASE 目录及 branch 对应；[输出形状](cases-template.json)不是手写模板 |
| `scenario-coverage.json` | 生成的分支覆盖、counts 与 ratio；[输出形状](scenario-coverage-template.json)不是执行通过证明 |
| `index.html`、`*.html`、`assets/` | 适用的当前页面、对话框、向导、控件及可见状态；入口链接全部当前页面，不另建每 REQ 副本 |

JSON 的字段、分支 oracle、fixture 与命令以[场景规则](../../rules/scenario-model.md)和[设计方法](../../../skills/scenario-model-design/SKILL.md)为准。不要通过搜内部 Schema 或复制示范包来代替实际业务设计。

## 正确表达故事与动线引用

故事定义使用 `## S-001：故事标题` 等 Markdown 标题；动线使用 `### F-001：操作标题`，有界面时也可用 `### F-001 / PATH-CREATE：浏览器路径` 关联。S/F ID 恰好三位数字。目录表和元数据可以帮助导航，但其中出现 ID 不构成被引用对象的定义。

矩阵每格填写实际 `branch` 或 `no_branch_reason`，省略未用字段，不填 `null`。同一事实、需求与故事组合可以对应多个真实分支，各格保留实际 rule 来源。每个 fact/story 至少出现于一格，是否存在更多有意义组合由设计调查决定，不机械要求笛卡尔积。

## 更新过程与完成判断

1. 从需求的 UI impact、AC 和真实变化识别模块及适用资产，先理解完整既有包。
2. 将本次变化收敛到模块完整当前集合；保持原 `REQ/FR` 来源、仍有效的 required branch 和 CASE 身份，不把历史分支改署本次 REQ。
3. 在原故事、事实/规则与交叉矩阵中明确分支，再维护 oracle、fixture 和操作动线。由原生成器刷新 CASE 与覆盖输出；界面变化同时更新页面、入口和交互覆盖。
4. 原保存接收检查适用链路是否一致。诊断具体错误时可用原 `scenario generate/validate`，不另建手工指纹、统计或通过证明。
5. 将准确行为包交给 FE/BE/SYNC 契约和任务：字段、状态、错误、权限、副作用及恢复语义必须可消费。

应核对的内容包括：

- 每项 required allow/reject 分支都有 CASE，声明覆盖完整；正反数量比例仅作风险参考，不是拒收阈值，不为比例凑分支。
- Story 与动线覆盖真实成功、拒绝、恢复和边界；有界面时，这些终点与控件能在当前原型找到，每个可操作控件有实际覆盖或明确 N/A。
- browser-required CASE 对应实际 PATH 和模块 spec，其他服务 CASE 保留相应验证义务。
- 模块可以跨需求保留当前行为。本次矩阵格连接本次 FR 表，历史格保持原来源；AC 桥按准确 `REQ/FR` 关联本次义务，同号历史 FR 不能顶替。本检查也不能证明所有历史来源语义或无分支理由都正确。

S7 按原分工独立验证当前模块：适用浏览器路径用真实浏览器和轮次证据，服务路径验证实际服务行为。生成的构造性覆盖不能当作执行覆盖；统计、测试名称或合成样例也不能替代实际结果。工作量轻重不免除适用行为与完整模块回归，但不为无界面模块新增浏览器义务。
