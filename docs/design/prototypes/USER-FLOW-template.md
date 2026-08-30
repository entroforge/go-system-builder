# 模块用户动线：flows.md

> 模块：{module}
> 当前真相：本文件维护模块完整动线集合；不创建 per-REQ/per-round 副本
> 关联原型：`docs/design/prototypes/{module}/index.html` + 当前 `*.html`
> 关联场景：`scenario-model.json` / `cases.json` / `scenario-coverage.json` / `fixture-contract.json`

## 使用方式

本文件是一个模块的完整、当前、可执行动线目录，不把模块限制为一条“主路径”和
若干附属“分支”。每个 `PATH-*` 都是一条完整、可独立复跑的用户路径：
它有明确的用户目标、入口、前置条件、操作序列和终点，并绑定 CASE 与 branch。

当不同结果需要不同的用户操作、前置数据或恢复动作时，将其拆成独立
`PATH-*`，例如“创建成功”“表单校验失败”“取消编辑”“权限拒绝”和
“失败后重试”。共享的登录、种子数据或准备动作写在本文档的共享前置中，
路径只引用它们，避免重复。路径数量不设上限；可按真实业务场景增删字段，
但当前仍成立的路径 ID 不可随意复用；不再成立的路径从当前集合移除，历史由 Git 和 round 报告保存。

## 1. 模块范围与路径目录

| 路径 ID | 用户目标 / 场景 | 类型 | 起点 | 终点 | Story / CASE | 原型区域 | source_refs | Spec |
|:---|:---|:---|:---|:---|:---|:---|:---|:---|
| PATH-001 | {complete a real business goal} | positive | {page/state} | {success state} | S-001 / CASE-001 | {section/state} | REQ-{id}/FR-{id} | `web/e2e/{module}/*.spec.ts` |
| PATH-002 | {recover from invalid input} | negative | {page/state} | {visible rejection and correction state} | S-001 / CASE-002 | {section/state} | REQ-{id}/FR-{id} | `web/e2e/{module}/*.spec.ts` |

`类型` 可使用 happy / validation / empty / error / permission / cancel / retry /
destructive / boundary / other；它只用于检索，不限制路径设计。

## 2. 共享前置与测试数据

| ID | 类型 | 内容 | 适用路径 |
|:---|:---|:---|:---|
| PRE-AUTH-EDITOR | 登录 / 权限 | {role and permissions} | PATH-CREATE-* |
| DATA-EMPTY | 数据状态 | {seed or fixture condition} | PATH-EMPTY |
| DATA-VALID | 输入数据 | `{field: value}` | PATH-CREATE-SUCCESS |

> 仅业务入口本身是深链时，路径才可声明 URL 直跳。其他路径必须从其
> 声明的上游页面逐步点击进入；共享前置不能借此绕过页面导航。

## 3. 路径定义

<!-- 复制本节，为每一条 PATH 创建一个完整路径单元。可按复杂度添加
     “业务说明”“辅助断言”“可访问性检查”等小节；不要把不同终点的
     用户决策压缩回同一张分支表。 -->

### PATH-CREATE-SUCCESS：{path title}

| 项 | 内容 |
|:---|:---|
| 用户目标 | {what the user is trying to accomplish} |
| 用户角色 | {role} |
| 关联故事 | S-{id} |
| 关联 CASE | CASE-{id} |
| 覆盖分支 | BR-{id} |
| 入口 | {visible page, menu, or declared deep link} |
| 允许 URL 直跳 | no / yes: {business reason} |
| 共享前置 | PRE-AUTH-EDITOR, DATA-VALID |
| 路径专属前置 | {state that is not shared} / N/A |
| 预期终点 | {visible success, persisted state, or next page} |
| 原型和合同映射 | {prototype region}; source_refs; Rule/CASE; FE/BE/SYNC §{n} |

| 步骤 ID | 用户动作 | 目标控件 / 稳定标签 | 输入或选择 | 期望可见结果 | 原型区域 |
|:---|:---|:---|:---|:---|:---|
| PATH-CREATE-SUCCESS-001 | 点击 | {menu/button/link label or selector hint} | N/A | {next visible state} | {section} |
| PATH-CREATE-SUCCESS-002 | 输入 | {field label} | `{value}` | {validation or entered state} | {section} |
| PATH-CREATE-SUCCESS-003 | 点击 | {confirm/save/submit label} | N/A | {success feedback and resulting state} | {section} |

**路径完成断言**

- {business result is visibly confirmed}
- {persisted data or follow-up page is visible}
- {no unexpected console or network failure}

**控件覆盖说明（按需填写）**

| 已操作控件 | 未在本路径操作的相关控件 | 由哪条路径覆盖 / N/A 理由 |
|:---|:---|:---|
| {control labels} | {control labels} | PATH-{id} / {reason} |

## 4. 模块交互覆盖核对

该表用于避免“路径都跑了但某个可操作控件从未被验证”。只列出原型中
声明的交互控件；纯展示元素不必列入。

| 原型区域 / 控件 | 用户意图 | 覆盖路径 | 状态 / N/A 理由 |
|:---|:---|:---|:---|
| {screen} / {button, menu, tab, link, dialog action} | {intent} | PATH-{id}, PATH-{id} | covered / N/A: {reason} |

## 5. E2E 执行规则

- [ ] 路径目录中的每个 `required` 路径均有独立的步骤级 E2E 证据，或有已批准的 N/A 理由。
- [ ] 每个 browser-required CASE 都有至少一个 required PATH，且 module spec 同时引用 CASE ID 与完整 PATH ID。
- [ ] required allow/reject branch coverage = 100%；比例门满足 module `coverage_profile`。
- [ ] 每条 negative PATH 逐项断言 `visible`、`terminal_state`、`persisted_effects`、`forbidden_side_effects`、`rejection`、`expected_state`、`recovery`；`recovery: N/A` 携带 rule `source_refs` 中的来源与非空理由。
- [ ] E2E 从每条路径声明的入口开始；没有使用未声明的 URL 直跳、API 调用、手改状态或隐藏浏览器状态。
- [ ] 每个步骤后的可见结果均被断言；重要终点保留截图、trace、console/network 摘要或等价证据。
- [ ] 模块交互覆盖核对中的每个控件都由至少一条路径覆盖，或明确 N/A。
- [ ] 实际实现与当前 `*.html`、路径定义、CASE oracle 或合同不一致时，记录为 S7 finding，不在 E2E 中自行改变路径标准。
