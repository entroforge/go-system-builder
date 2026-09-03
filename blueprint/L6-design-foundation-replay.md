# L6 — 项目级设计基础实战回放协议

> 层：第六层｜机制域：用真实 UI 项目观察 L4/L5 是否减少横向返工与风格追问，并决定顾问型检查要不要升级
>
> 上游：L4 §11.4 观察信号、L4 §13.1 硬门延迟理由、L5 P0～P4 落地、`docs/rules/design-foundation.md`
>
> 下游：填好的 `docs/design/research/FOUNDATION-REPLAY-template.md`；仅在稳定失效后考虑 `--strict` CI 或 Runtime 提示
>
> 状态：v1.0.0，观察协议。本模板仓库**不能**代替一次真实产品回放；本文件规定如何回放、记什么、何时才允许把检查升级为门。

## 0. 为什么本仓库写不成“已经回放成功”

Project Design Foundation 的经验证据来自**带用户可见界面的目标产品**，不是模板工厂。本仓库故意不填写 `DESIGN.md`、不发明消费端 Kernel、不挂 Foundation 到 `validate --all`。因此 DF-T13 在本仓库的完成态是：

1. 有一份可执行的观察协议（本文）；
2. 有一份目标项目可填写的记录模板（`docs/design/research/FOUNDATION-REPLAY-template.md`）；
3. 升级规则写清：未观察到稳定失效，不得把审美或空壳文档做成 fail-closed 硬门（D3、D5、成本公理）。

把模板仓库自己涂成一个假产品来“跑通 F0～F6”不算 L6。那只会制造形容词 Kernel 和形式主义。

## 1. 回放对象

一次合格回放至少覆盖：

- 一个已发布（或正在首次发布）的 Foundation；
- **两个** `UI impact=changed` 的 REQ（证明继承，而不只是第一次设计）；
- 若产品有消费者端与后台，至少各触及一个 Surface；
- 人只做三次高杠杆确认：方向（F2）、内核（F3）、发布（F6）。

对照物是同一产品在 Foundation 之前的工作方式（默认 UI 库、局部冠军、逐页问颜色），不是另一个品牌的截图。

## 2. 观察信号（唯一分母：L4 §11.4）

记录必须回答这些问题，而不是打审美分：

| 信号 | 改善看起来像什么 | 失效看起来像什么 |
| --- | --- | --- |
| 风格追问 | 新 REQ 不再问“喜欢什么颜色/什么风格”；人只面对带推荐的价值 A/B | Agent 仍把色值和按钮形状交给人逐项挑选 |
| 可解释性 | Agent 能用 Thesis / Law / Surface 说明页面为何这样组织 | 只能说“看起来比较现代”或“跟库的 Demo 一样” |
| 默认 UI / 一次性样式 | 语义角色来自 Token 与现役组件；hex 不在页面里发明 | 每个模块一套内联色、第三种主按钮 |
| 人的审查负担 | 人审宏观方向和例外，不逐页改间距 | 人继续当 CSS 审核员 |
| 横向返工 | 同类语义共用一个组件；晋升走 CP-* | 两个模块各写一套 dialog/button |
| Surface | Profile 能解释消费者端与后台的密度/姿态差异 | 后台被逼复制营销站留白 |
| 回灌 | 局部发现变成提案或 Grammar 修订 | REQ 静默把局部冠军写成全局规范 |

辅助计数（可选，不作质量分）：风格追问次数、跨模块重写的同义组件数、未登记 hex 数、`design-foundation check` 警告数。像素 snapshot 只计入“实现漂移”，不计入“方向正确”。

## 3. 方法

1. 目标项目按 L5 §10 启用模板，跑 `skills/design-foundation` F0～F6。
2. 每个 changed REQ：S0 填 Foundation 引用；S2 先写 Derivation Note 再扩模块包。
3. 实现前查 UI Lab（`tools/ui-lab/README.md`）：`docs-list` / `docs-show`，禁止按 portable DESIGN.md 重写组件。
4. 定期跑：

   ```bash
   loop-harness design-foundation check --root .
   ```

   默认顾问态（exit 0 + 警告）。不要接到 `validate --all`。
5. Golden Screen / Storybook snapshot 按 `tools/visual-qa/README.md` 只公证漂移。
6. 在第二个 UI REQ 合并后填写 replay 记录；后续 REQ 追加第 2、3 节，不要另起炉灶改信号定义。

## 4. 什么时候允许升级检查

| 升级 | 准入 |
| --- | --- |
| 保持顾问 `check` | 默认。模板工厂、纯后端、首个 UI REQ 之前都走这条 |
| CI `--strict` | 同一类跳过（无 DESIGN.md 仍锁 changed REQ、无 Derivation 仍扩包）在**两个以上 REQ**重复出现，且补提示词/模板后仍发生 |
| Runtime / PTR 硬门 | 仅当 `--strict` 已经在该产品稳定工作，并且失败修复路径短（补文档即可），不把 Thesis 质量机判 |

明确永不升级为硬门的：Thesis 好不好、配色是否“品牌正确”、snapshot 是否等于好设计。

降级：若硬门制造空壳 `DESIGN.md` 或阻塞无 UI 产品，立刻退回顾问态。

## 5. 回放结论如何回灌

| 发现 | 回灌层 |
| --- | --- |
| 信号改善，偶发提示词遗漏 | 补 Skill / `docs/rules/design-foundation.md`，L4 不动 |
| 模板字段被跳过 | 改 L5 模板或 REQ §C 引导，不改 `UI impact` parser |
| 稳定跳过 F0 | 该产品启用 `--strict`；跨多个产品后才考虑 L4 §13 Runtime 行 |
| 像素过、方向错 | 回到 F1/F3 与真实用户证据，不收紧 snapshot |
| 协议本身问错问题 | 修订本文与 L4 §11.4，走 L1 演化协议 |

## 6. 本仓库对 DF-T13 的诚实状态

| 项 | 状态 |
| --- | --- |
| 观察协议 | 本文 |
| 记录模板 | `docs/design/research/FOUNDATION-REPLAY-template.md` |
| 顾问型机械检查 | `loop-harness design-foundation check`（不进 `validate --all`） |
| 真实产品回放数据 | **无**；等目标项目填写记录 |
| Runtime Foundation 硬门 | **无** |

## 变更记录

### v1.0.0 — 2026-09-03

- 将 L4 §11.4 / §13.1 / L5 DF-T13 写成可执行回放协议；
- 固定模板工厂不能冒充产品回放；
- 给出检查升级与回灌规则。
