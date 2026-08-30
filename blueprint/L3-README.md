# L3 README — 第三层：各 Stage 详细设计（索引与共用词表）

> 层结构（以 L1 §六 为准）：第一层哲学蓝图 → 第二层生命周期实战目标 → **第三层 = 每个 stage 的详细落地设计**（含该环节的内容→工具映射与门禁逻辑）→ 第四层跨 Stage 工具机制与治理设计 → 第五层实现规格与实现 → 第六层运营回灌。

## 文件索引

| 文件 | Stage | 一句话 |
|:--|:--|:--|
| L3-S0-requirement-design.md | S0 需求设计 | 把人的意图固化为可锁定的需求基线 |
| L3-S1-bind.md | S1 绑定与授权生命周期 | 显式授权、权威状态起点，与七动词生命周期控制面（进入/暂停/恢复/修订/退出/结束/重绑） |
| L3-S2-design.md | S2 设计 | 架构决策 + 模块全量场景真相包 |
| L3-S3-contracts.md | S3 契约 | 分端执行契约与双向追溯 |
| L3-S4-task-split.md | S4 任务拆分 | 单职责任务 + 可判定收尾契约 |
| L3-S5-document-verification.md | S5 文档验证 | 独立双路审查 + 原子锁定 |
| L3-S6-build.md | S6 构建 | 计划回执后的连续实现、隔离集成与如实报告 |
| L3-S7-verification-round.md | S7 完整验证轮 | 单一 required Claim set + DV/QA/E2E 1..N 派发 + CleanRound/ObservationBatch |
| L3-S8-finding-investigation.md | S8 发现调查 | Finding encounter → InvestigationCase → CausalModel → RepairContract |
| L3-S9-repair.md | S9 修复 | 边界约束修复 + 证据失效 + 定向重验 |
| L3-S10-acceptance-audit.md | S10 验收与审计 | 验收汇编 + 系统不变量审计 + 债务登记 |
| L3-S11-release-gate.md | S11 人工发布闸 | 不可默认的人闸与回滚路 |

## 共用机制清单（真实载体——L3 只允许引用本清单的真实机制名，禁用抽象代号）

抽象代号（如 T1/T4）会迫使读者查对照表=制造猜测成本（L1 公理五）。第三层直接写真实机制：

### A. Hook（`.claude/settings.json` 注册的生命周期事件——状态切换与派发执法的触发器）

| 事件 | 承载的职责 |
|:--|:--|
| `SessionStart` | 重投影当前状态：发恢复包（当前 stage/单一下一步/读序），从权威状态重入座（compact 恢复正门） |
| `PreToolUse` | **状态切换主力**：控制循环→质量门评估→满足则自动迁移（CAS）→安全决策（越界写拦截/锁定产物阻断） |
| `PreToolUse`（子代理派发匹配） | **派发前预检提醒**：单人 vs 团队？角色模板选对没？worktree 隔离？team_name 带了吗？ |
| `SubagentStart` | 派发瞬间提醒（预检答案落地、任务简报要求） |
| `PostToolUse`（`SendMessage`） | 捕获 PLAN_REPORT/BLOCKER/COMPLETION，写入 Assignment checkpoint；计划不是 final response |
| `SubagentStop` | 按 L4 判定是否已有 canonical Result、结果是否待消费，以及应允许停止、阻止停止还是进入恢复路由 |
| `TeammateIdle` | 按 L4 区分正常交卷、计划缺失、异常 idle 与阻塞；只在责任仍可继续时唤醒同一 Worker，不自动派发下一任务 |
| `PreCompact` | 持久化可恢复检查点（给下一个 SessionStart） |

> 平台共有 31 个 Hook 锚点，全集与选点审查见 [L4 Claude Code Hook 锚点全图](L4-hook-anchor-catalog.md)；事件注册、payload 契约、输出/退出码与失败态度（fail-open/closed）的唯一权威是 [L4 Hook 与平台事件接线](L4-hook-platform-wiring.md)。上表只列各事件承载的职责，不复制定义。

### B. Harness（`loop-harness` 二进制——确定性引擎）

| 能力 | 关键命令/机制 |
|:--|:--|
| 状态机迁移（唯一写者，CAS） | `req bind` / `runtime transition --id TR-xxx` |
| Agent 生命周期事件（12 事件，L4 plan_checkpoint 直通已落地） | `runtime agent-event`（readback_started/readback_submitted → activation_sent → … → shutdown_approved）；默认 dispatch_mode=`plan_checkpoint` 下 understanding_submitted 绑计划回执直通 activated；`plan_approval_required` 仍走 understanding_approved 中间态（受支持的兼容路径，非"待迁移"） |
| 证据登记+指纹 | `runtime evidence add`（登记 id/kind/path/sha256/produced_by） |
| 读回信封生成 | `team launch --manifest --request-template`（指纹化读回请求） |
| 门禁/健康 | `ready`（门清单）/ `doctor` / `validate --all` |
| 缺陷事件 | `runtime bug-event`；干净轮求值 `verification clean-round` |

### C. 模板（`docs/**/*-template*`——字段即逼问，D4 主体）

REQ / TASK（读序、允许路径、收尾契约、职责）/ BE·FE·SYNC 契约 / BUG（七步）/ REV·QA·E2E·ACC 报告 / 场景四件套（scenario-model·cases·coverage·fixture-contract）/ 原型包头部。

### D. 角色定义（`agents/*.md`）

frontend/backend/test-builder（构建者）；document/delivery-verifier、qa、e2e-tester（验证者）——各带工具集/写路径/预载技能。

### E. 技能（`skills/*/SKILL.md`——方法论按需加载）

`agent-dispatch`（L4 plan_checkpoint 派发；旧 `two-phase-activation` 已删除）/ `team-planning`（组队）/ `loop-orchestration`（驱动）/ `bug-resolution`（深查）/ `clean-round-evaluation` 等。

Agent 调度是首个进入 L4 的共用机制。S5/S6/S7/S8/S9 只声明消费 `one_shot`、`plan_checkpoint` 或 `plan_approval_required` 及本阶段完成条件；派发对象、计划回执、消息、等待、idle/stop、恢复和结果消费统一以 [L4 Agent 调度与治理机制](L4-agent-dispatch-governance.md) 为目标态。两阶段授权事件（readback_submitted → understanding_approved → activated）仍是 `plan_approval_required` 模式下的真实代码路径（internal/assignment/lifecycle.go 的 12 事件生命周期），并非待迁移的死代码；`two-phase-activation` Skill 已被 `agent-dispatch` 取代并删除。

其余横跨多个 Stage 的机制——权威状态与单写者 CAS、revision 语义词典、指纹体系、证据链与失效家族、追溯分母链（单一验证分母）、精确集求值纪律、门禁与迁移分类学、写屏障家族、观测采集与脱敏、暂停/Blocker/终止保障、会话恢复投影、错误信息契约、债务与兼容性登记——统一沉淀在 [L4 运行时控制面与横切治理](L4-runtime-control-plane.md)。各 L3 只声明消费方式，不再内联定义；发现正文与该文冲突时，先核对代码现状，再按演化协议回改落后的一方。另有两份基石机制篇：[Hook 与平台事件接线](L4-hook-platform-wiring.md) 与 [权威状态机与迁移事务核心](L4-state-transition-core.md) 分别是事件总线契约与存储/迁移引擎的唯一权威——各 L3 的 hook 表格与 TR 表格只描述本阶段消费，不复制定义。

**编排三原则**：①模板负责"声明结构"（字段逼问），harness 负责"事实求值"（指纹/门/迁移），hook 负责"在自然事件上执法与提醒"；②同一职责多机制必须声明主备；③优先写机制的真实命令/事件名。

## 注意力分配原则（各 L3-Sn 的共用判定尺——本节是唯一权威，各 stage 只引用不复述）

agent 的注意力与人的一样稀缺且更贵（C4 的推广）：每一段常读叙述都是持续支付的 token 成本，每一条"请自觉遵守"都在 C1/C2 上裸奔。机制的载体分配过三问：

1. **机械可判吗**（存在性/指纹/链接/形状/一致性/计数）→ harness 校验承载，不写文字；
2. **不可逆吗** → hook block 或人闸；
3. **是思考逼问吗**（逼作者想清楚语义判断）→ 模板字段或 skill。

三者都不是的叙述段落 → 删。从未被真实生命周期观测过的强制 → 冻结待观测，不加码（C5/公理四）。

**渐进披露三原则**：

- 进入 stage 只读最小集（模板 + 当前相位角色定义），不读协议全文——hook 投影给出单一下一步；
- 做到具体步骤才载该步骤的方法论（skill 按需加载）；字段级规范活在模板行内注释，不进常读文档；
- 机制已承载的规范**永不成文要求阅读**——agent 在被拦截、被校验、被拒绝中学会规范，而不是在阅读中记住规范。

**载体词表**（各 stage 的逐步设计与职责审计中使用）：叙述（读）/ 模板（填）/ harness（算）/ hook（拦）/ 人闸（批）。

## 文档骨架（每份 L3-Sn 统一——从阶段立意漏斗到机制落点）

L3 文档自身也必须遵循漏斗思考。读者应先理解这个 stage 为什么存在、要把什么问题搞清楚，再看到任务和工作流；模板、skill、harness、hook 等机制只能在其服务的步骤中出现，不能先以工具盘点占据主线。

1. **阶段立意与目标**：为什么需要这个 stage；它要解决哪些根问题；输入、完成定义、边界和下游是什么。
2. **任务分解**：为达成目标一共需要完成哪些有限任务；每项任务分别要搞清楚什么、做什么、产出什么。
3. **完整工作流**：任务之间的依赖顺序、分支、返工回路与人闸；先让读者看见一条从入口到出口的路。
4. **逐步设计与机制承载**：沿工作流逐步说明如何引导、模板哪个字段承载、skill 何时加载、harness/hook 在哪里接力、谁判定、该步产出什么。
5. **职责分布与覆盖审计**：把职能映射到人、agent、模板、skill、harness、hook 和下游消费者；检查遗漏、重复、真假重叠与诚实缺口，并保留关键取舍及否决理由。
6. **L1 准则嵌入**：逐项说明相关 D1-D7 与公理通过什么实际结构进入 stage；无真实落点时如实写“不在本阶段激活”，不做装饰性引用。
7. **产出、出口门槛与失败路由**：明确最终交付物、可判定出口、失败归因和返回哪一层，避免只写“未通过”。
8. **易错点与渐进披露**：集中整理最容易忽视的语义、边界和时序；按角色/时机给最小阅读集，机制已承载的内容不要求背诵。

**迁移状态**：S0～S11 已完成按“阶段立意→任务分解→完整工作流→逐步机制承载→职责审计→准则嵌入→出口路由→易错点/渐进披露”的统一迁移；其中 S6～S11 保留当前未优化机制、正文未被 gate 消费的字段及路由/实体不同步等事实边界，不将设计意图表述成已有能力。

**减法纪律**：每个机制的选择都要过"复杂度 vs 收益"；优先复用现有机制；说不出收益的机制不进编排。横跨多个 Stage 的统一机制契约不写在 L3（属第四层）；具体字段、模板和代码实现不写在 L3（属第五层）。L3 只讲清该 Stage 为什么消费机制、消费哪种模式、如何进入与如何收口。

## 纪律

- 每份文档的每条设计标注承载的 D/公理（映射纪律）。
- 引用 L2 条目时只引用不复述；发现与 L2 冲突 → 停下修订 L2，不在 L3 私改。
- 修订走 L1 演化协议。

## 变更记录

| 日期 | 变更 |
|:--|:--|
| 2026-08-14 | 建立第三层：README（T1-T7 词表 + 骨架 + 索引）与 L3-S0…L3-S11 共 12 份 stage 详细设计 | owner 指示：第三层=每 stage 详细落地设计 |
| 2026-08-14 | 文件名补全 stage 英文名（L3-Sn-xxx），索引同步 | owner 指示 |
| 2026-08-14 | 骨架 §6 升级为「机制编排与职责覆盖」（含覆盖/重叠对账要求） | owner 指示 |
| 2026-08-14 | 删除 T1-T7 抽象代号，改为真实机制清单（hook 事件表/harness 能力/模板/角色/技能）——公理五：不制造猜测成本 | owner 复核 |
| 2026-08-14 | 骨架改为五问叙事（目标/手头工具/选择含否决/编排/期望效果）；确立减法纪律；字段级规格移至第四层 | owner 复核：讲清楚一件事 > 机制堆砌 |
| 2026-08-15 | 新增「注意力分配原则」共用判定尺（载体三问 + 渐进披露三原则 + 载体词表）；文档骨架增第六节「注意力预算与渐进披露」，12 份 L3-Sn 同步新增 §6 | owner 指示：渐进披露、以机制承载规范、削减平白叙述 |
| 2026-08-18 | 文档骨架改为“阶段立意→任务分解→完整工作流→逐步设计与机制承载→职责审计→准则嵌入→出口路由→易错点/渐进披露”的漏斗结构；S0 作为首个迁移样板 | owner 指示：L3 文档自身必须形成体系化逻辑，不能以机制盘点代替 stage 设计 |
| 2026-08-20 | S2～S11 完成统一漏斗结构迁移；S6～S11 对当前未闭合机制、正文未被 gate 消费的字段及路由/实体不同步等现状做显式标注 | owner 指示：串行完成 S2～S11 |
| 2026-08-20 | L4 建立后收敛层级边界：Agent 调度细节上移至跨 Stage 机制层，L3 只保留各 Stage 的消费模式和完成条件；两阶段授权标为迁移入口 | owner 指示：统一治理 Sub-agent / Agent Team，不在每个 L3 重复协议 |
| 2026-08-28 | 新增第二份 L4《运行时控制面与横切治理》并在共用机制清单挂链接：Agent 调度之外的十三个跨 Stage 机制域上移（v0.2.0 定稿，含追溯分母链与精确集求值两域的本体化）；同步修正 L3-S5 的 two-phase-activation 现役残留与 L3-S10 干净轮分母旧口径（angle） | owner 指示：跨 Stage 贯穿机制沉淀为单独的 L4 设计汇总 |
| 2026-08-28 | 基石抽取批次：新立《Hook 与平台事件接线》《权威状态机与迁移事务核心》两份 L4，A/Hook 节与本节均改挂权威指针——各 stage 的 hook 表与 TR 表自此只描述消费 | owner 批准：按五问判据筛出并立篇（顺序 Hook→状态机） |
