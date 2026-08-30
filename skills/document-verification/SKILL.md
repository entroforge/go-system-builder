---
name: document-verification
description: Use when S5 document verification assigns you one review responsibility — write the envelope first (REV-template §0), findings only when found
category: methodology
version: 2.2.0
---
# Document Verification

## Authority
You produce evidence; the gate evaluates it. Runtime authority lives in `docs/loop-definition.json`; the S5 stage contract lives in `docs/agent-protocol.md #s5`（三步：派活→审查→收口三岔路）; the envelope skeleton and findings format live in `docs/reports/review/REV-template.md`.

## Role Contract

You are one of two independent reviewers of the frozen spec chain. You verify
exactly one responsibility — `DV-SPEC-CONSISTENCY`（规格链自洽吗？）or
`DV-TASK-EXECUTABILITY`（Builder 照着能干吗？）— assigned in your activation
envelope. You never repair what you review.

## Entry Conditions
- The Loop is in `document_verification` and you hold an activation envelope naming exactly one responsibility.
- The spec chain carries fingerprints (path + version + SHA-256 + baseline generation).

## Required Inputs

Activation envelope (responsibility, report path, fingerprints, skills to
load), the locked spec chain (REQ → design → contracts → tasks), and the
module current-truth package when the REQ touches UI.

## Procedure — 3 mandatory, 1 triggered

1. **落信封骨架（激活后第一件事）**：复制 `docs/reports/review/REV-template.md` §0 到你的报告路径 `docs/reports/review/REV-{runid}-{resp}.json`——每个字段带一行填写指引，写骨架即读懂要交什么。`subject_refs` 从 `.claude/loop-state.json` 的 documents[] 逐条复制（故意没有自动命令——逐条抄写就是"我签的是哪一版"的对峙，这一步的笨拙是审查的锚）。
2. **审查（按职责）**：
   - SPEC-CONSISTENCY：自底向上读 TASK → 主契约 → 关联契约 → locked REQ → 设计/rules，核对验收↔条款映射、跨文档引用指纹、FE/BE/SYNC 边界一致（数据形状/错误码/状态机/API 面）、场景包与契约映射不矛盾。三项深挖：
     - **需求覆盖端到端**：抽 2-3 条 AC 走完最后一公里——AC → 契约条款 → TASK §3 声明 → 收尾契约 assert 行，全程指名到位；任何一跳"对不上号"即 finding（机器只保到 AC↔CASE，这最后一公里归你）；
     - **NFR 落地追踪**：REQ 非功能表的每一行有没有落进某张契约的条款（`NFR-{id}` 被 CONTRACTS 索引引用或条款显式声明）？NFR 是最易静默掉队的——没落地的每一行都是 finding；
     - **负向与错误路径对账**：契约错误码表 ↔ 场景包负向分支 ↔ 权限拒绝用例三方对得上吗？REQ 流程表声明了权限列的，场景包里必须有"无权限被拒"的负向分支。
   - TASK-EXECUTABILITY：跑 `go run ./cmd/loop-harness tasks check --root .` 消费机检结论（覆盖双向/DAG 无环机器已判，不重算），再审机器判不了的**五问+半问**（代入 builder 视角——"我拿到这个任务单能顺利干完吗"）：
     1. **单一职责**：一句话测试——说不成交付物、跨层混拆（FE+BE+SYNC 同任务）、收尾契约 assert 超四行，都是拆分信号；
     2. **单窗口可行**：**builder 中途 compact 丢任务信息是灾难性表现**——按 §2 清单量与 §4 写路径量判断会不会撞上（`tasks check` 输出的 reference load 是参考数字不是门槛——它同时供第五问判断证据产出成本）；能拆小/裁清单的都不是"设计如此"而是缺陷；
     3. **语义连贯**：地基（类型/schema/迁移）先于依赖者？有没有 B 用 A 产物却没声明依赖的缺失边？有没有复制粘贴来的假边？
     4. **自包含**：任务单给的是精确锚点（路径+条款号）还是"自己去理解模块"？builder 要 grep 找活干 = 任务书写得不合格；
     5. **可测性前向**：收尾契约要求的证据在 S7 三角度（DV/QA/E2E）下真的能产出吗？验收标准本身可判定吗——"响应快""体验好"这类无指标的表述，到 S7 只能靠猜，finding；
     批次节奏（附加半问）：DAG 机器已判无环——再看一眼关键路径：有没有一串任务全是单链依赖（串行瓶颈拖死并行分派）？有没有本可并行的任务被假依赖串起来了？
3. **收口（登记 + 回填）**：信封回填 `conclusion`（pass / fix_required / req_change_required——与 gate 同词，全流程没有第二套枚举）与 `requested_event`（仅 fix_required 填 document_fix_required；req_change_required 留空走人闸）；然后按 REV-template §0 注 2 的命令行把信封**登记进 runtime**（`runtime evidence add`，`--kind document_review` 与信封同词）——未登记的信封 gate 看不见；**重签（fix 回路第二轮起）须用带 `-r2` 后缀的新 ID**（同 ID 会被拒——旧条目即使已 invalid 也占 ID）。此后不调用任何 transition 命令——PreToolUse 按你的 conclusion 自动路由。
4. **触发——有 finding 才写 REV 报告**：按 `REV-template.md` §1-§5 写（findings 表带 P0-P3/定位/预期/实测/证据；N/A 须记理由与证据）。双 pass 不产报告——没有人读"都挺好"。

## Triggered Deep-Dives（按需触发——激活信封指名或触发条件命中时才审，不均匀付费）

**归属**：三项全归 **TASK-EXECUTABILITY 审查者**（数据迁移任务在批里、外部依赖看 SYNC 契约、critical 看 REQ——都是任务/REQ 视角）。主会话派活时按 REQ/契约特征核对本表并在激活信封指名（写在 expected_outputs 或附言均可）；若信封漏指名而你在审查中命中触发条件，**自查自救直接开审**——漏审比越权严重。

| 触发条件 | 专项审查 |
|:--|:--|
| BE 契约含数据模型/schema 变更（新增/删除/改型字段、建表、迁移） | **迁移与破坏性变更处置**：默认**干净断裂，不默认向后兼容**（owner 裁定——兼容是必须辩护的技术债）。逐项核对：该变更选了兼容还是断裂？选兼容的有没有登记（决策理由/影响面/移除路径+责任人，入 REQ 债务记录或 §A4）？破坏性变更的数据迁移任务在不在 TASK 批里？**无登记的兼容 = 无声负债，直接 finding** |
| SYNC 契约存在，或 REQ/契约声明外部依赖（第三方 API/服务） | **外部集成韧性**：每个外部调用点有没有超时、失败重试策略、降级行为的声明？SYNC 契约的字段映射把外部错误翻译成本地错误码了吗？没有声明的调用点 = 生产环境的第一批故障源 |
| REQ 绑定的场景包 `coverage_profile: critical`（或 REQ 风险声明为高） | **风险触发验证就位**：S7 会按风险加派验证维度——现在预检它的"座位"在不在：场景包的 required 分支覆盖是否声明到位、每条 critical 需求有没有对应的 E2E 路径（PATH）座位？（负向分支齐备性归职责 A 深挖项，不在此重复）座位缺失在 S5 补，比 S7 临时加人便宜 |

触发段产出的结论照常进信封与 findings——专项维度算进你 assigned conclusion 的审查范围。

## Outputs

- 必产物：`REV-{runid}-{resp}.json` 证据信封（11 字段、10 个机器校验 + review_round 故意不写）+ runtime 登记（§0 注 2）。
- 条件产物：`REV-{runid}-{resp}.md` findings 报告（仅有 finding 时）。

## Exit Conditions

- 信封 conclusion 已回填且 subject_refs 精确等于当前 documents[] 指纹集（多一少一 gate 都拒）。

## Stop Conditions

Stop immediately and surface to the human if any of:

- A fingerprint changed mid-review (the artifact was edited after you started).
- A required document layer is missing entirely.
- Two authorities contradict and cannot be reconciled without a REQ decision → `req_change_required`.
- You recognize you authored (or materially drafted) an artifact under review — independence is lost（纪律层：机器无法替你判这一条，你不说没人知道——这是 S5 对你的唯一诚信要求）。

## Non-Goals

- Do not repair the reviewed documents — your fix_required routes them back to planning.
- Do not lock the batch or activate Builders — TR-003 does that after both reviewers pass.
- Do not treat missing coverage as N/A, and do not accept an unverifiable closing contract as executable.
