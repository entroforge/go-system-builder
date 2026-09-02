# Blueprint — 分层设计蓝图（项目定调文档）

> 本文件夹是整个模板项目的**分层设计文档永久居所**：自顶向下、逐层推导，每一层可对照上层验证。安装时随模板分发，放到 `.claude/bin/` 旁边，作为设计理念与规划的双向查询备用。

## 层结构与文件索引

| 层 | 文件 | 内容 | 状态 |
|:--|:--|:--|:--|
| 第一层 | `L1-design-principles.md` | 工程哲学与设计蓝图：场景、物理约束 C1-C5、使命与六项把控、主干设计决策 D1-D7、五公理、失效模式目录、演化协议、权威层结构定义 | ✅ v2.4.0 |
| 第二层 | `L2-lifecycle-plan.md` | 生命周期实战目标：三条铁律 + 能量函数、S0-S11 每阶段（任务/把控/风险对应/理论根据/入口/出口/失败路由）、全局规则、层间校验 | ✅ v1.4.4 |
| 第三层 | `L3-README.md` + 12 份 `L3-Sn-<stage名>.md`（S0-requirement-design … S11-release-gate） | 各 Stage 详细落地设计：角色/产物/过程/门禁判定/工具承载（T1-T7）/失败处置/反作弊/度量 | ✅ |
| 第四层 | 六份机制篇：`L4-agent-dispatch-governance.md`（谁在责任上行动）、`L4-state-transition-core.md`（事实住哪与合法变更引擎）、`L4-hook-platform-wiring.md`（决策如何上平台总线）、`L4-hook-anchor-catalog.md`（平台 31 锚点全图与选点审查）、`L4-runtime-control-plane.md`（内容合规规则与词汇词典）、`L4-revision-usage.md`（revision 的内部语义与 Agent-facing 命令协调）；后续按机制域扩展 | 横跨多个 L3 的工具机制与治理设计：统一对象模型、状态、消息、Hook、Harness、恢复和验收契约；每份是其覆盖域的唯一权威定义处，各 L3 只声明消费 | ✅ v0.3.0 / v0.1.0 / v0.1.1 / v0.1.0 / v0.3.0 / v0.1.0 |
| 第五层 | `L5-*.md`（待建） | 实现规格与实现：Schema、Skill、Agent Definition、模板、代码、测试和迁移任务 | ⏳ |
| 第六层 | `L6-*.md`（待建） | 实战运营记录与回灌（失效数据 → 第一层演化协议） | ⏳ |

## 阅读纪律

1. **从上往下读**：先 L1 后 L2；任何下层困惑先回上层找根据。
2. **映射纪律**（L1 §六）：下层每条设计必须能指认它承载的 D1-D7 与通过的公理；指认不出 = 越层设计，退回。
3. **修订走 L1 第五部分演化协议**：任何层的修订须引用实战证据、过准入五问、带版本与 changelog。

2026-09-01 新增 [`L4-revision-usage.md`](L4-revision-usage.md)：统一 revision 的内部语义、Agent-facing 命令边界和复杂度准入；各 L3 不再把手工 revision 计算写成正常操作步骤。

## 安装位置

目标项目应用本模板时，把本文件夹整体复制到 Manual 旁边，供 agent 与人随时查证设计意图：

```bash
cp -R blueprint <target-project>/.claude/bin/blueprint
```

## 变更记录

| 日期 | 变更 |
|:--|:--|
| 2026-08-14 | 建立 `blueprint/`；L1/L2 自 `docs/rules/` 迁入并按层级命名；接入发布白名单与安装流程 | owner 指示：层级文档永久保留、安装时置于 `.claude/bin/loop-harness` 旁备用 |
| 2026-08-20 | 建立首份 L4 `L4-agent-dispatch-governance.md`；L4 定位调整为横跨多个 L3 的工具机制与治理层，具体实现下沉 L5 | owner 指示：统一梳理 Sub-agent / Agent Team 调度与治理 |
| 2026-08-20 | L4 终审收敛为机制准入规则、单一事实链与可验证平台控制；Claims/Assignment/Result 为事实，coverage/board/ledger 为视图；普通计划回执连续执行，高风险才审批 | 删除重复控制面，避免把未验证的 Hook 唤醒能力写成既成事实 |
| 2026-08-22 | L4 P0 平台接线落地（官方 payload 字段、TeammateIdle/SubagentStop exit 2、TaskUpdate self-claim 门）；S7 侧落地 FindingSupplement、运营指标子集、`s7 manifest-draft`、oneOf 错误剪枝、SessionStart/PreCompact S7 恢复投影与兼容别名审计 | S7 §13 / L4 §15 各补 2026-08-22 审计记录；真实平台 doctor 与 Controller 假唤醒收敛仍待后续批次 |
| 2026-08-22 | 批次二：Controller idle 语义收敛（假唤醒删除、首写屏障入 wire 路径）、blocked_by_confirmed_finding 投影、site_lost BLOCKER 复用、overlap/cold-start overload validator、resource lock 排队、worktree 共享控制面核验、capture exec 自动采集、supplement 并入主 loop-state + discriminator 判别门 | S7 §13 审计记录；仍待：真实平台 doctor、产品侧浏览器 wrapper、regression_available 指纹复用校验 |
| 2026-08-22 | 五视角 E2E 代入测试（Planner/Reviewer/cold-start/对抗/恢复+复杂度）：对抗测试 12 类攻击 11 类 D3 合规零误伤；净复杂度判定持平（操作更简、词汇更重、无镀金）；修复 wire 路径 workspace 投影 P0 缺陷、`s7 workspace-digest` 落地、agent 状态错误文案诚实化 | S7 §13 审计记录；高优先未修：next/ready 矛盾、reading→working 断点、oneOf 剪枝生产路径休眠、恢复包三缺陷、S7 前置状态无引导 |
| 2026-08-22 | 高优先缺口根因修复批次：TR-008/009 文案对齐自动提交语义、恢复包 drain/来源/噪音三修复、剪枝推广到嵌套判别节点、plan_checkpoint 自动激活链（register-workgroup 预生成信封 + PostToolUse 链式推进 + `runtime agent-begin` 兜底）、s7 draft 前置门与 sandbox recipe | S7 §13 审计记录；剩余缺口收敛为环境/产品侧与低优先 UX 项 |
| 2026-08-23 | 验证轮（盲测五视角）确认第一轮修复生效，且发现两处回归：R1 资产未 go:embed、R2 链中途失败后不可恢复——均已修；R3 文案残留、R4 恢复包矛盾、R5 ready 非确定性、R9 文档/schema 矛盾——均已修。R6 CLI 路径解析、R7 controller 抢停 stop-idle 门、R8 错误指引弱、R10 capture exec 终端泄漏——待修 | S7 §13 审计记录；剩余缺口以 CLI/UX 为主，机制层已稳定 |
| 2026-08-23 | 验证轮后续修复批次：R6 `--root` 路径一致（register-workgroup + fingerprint + reconcile-policy-ref 同类 bug 同步）、R7 stop-idle 门不再被 controller 抢拍（lifecycle 各阶段回归）、R8 三处错误信息 next-action 加强（stale revision / verdict=fail / supplement 缺字段）、R10 capture exec 终端秘密不泄漏 | S7 §13 审计记录；剩余收敛为命令面拆分与产品侧 wrapper |
| 2026-08-28 | 新增第二份 L4《运行时控制面与横切治理》：对 S0～S11 做全量文档一致性审查后，把 Agent 调度之外的十三个机制域统一沉淀为唯一权威定义处（v0.1 九域起步，v0.2 按调度篇准入逻辑复审后扩入追溯分母链、精确集求值、观测脱敏、歧义裁决、债务登记五域）；承接 L2「能量函数权威定义归第四层」的悬空授权；同步修正 L3-S5 两处 two-phase-activation 旧引用、L3-S10 干净轮分母表对齐 EvaluateCleanRound 七检查现状（去除 angle 残留） | owner 指示：回看 S0~S9 梳理 L3 理念、核对 L1/L2 一致性、跨 Stage 机制沉淀为 L4 单独汇总；复核：L4 应定义机制而非综述 |
| 2026-08-28 | 基石抽取批次：按「自有对象模型 + 全 stage 深消费 + 独立失败面 + 平台边界 + 风险分级」五问筛出两份新基石 L4——《Hook 与平台事件接线》（事件注册/payload 无损透传/输出退出码契约/fail-open-closed 矩阵/四级识别链，并核实 protected_commands 已退出 Hook 主链路的事实更正）与《权威状态机与迁移事务核心》（loop-state 存储模型/CAS 本体/崩溃 marker 协议/12 态+三 phase 机+实体生命周期索引/guard-action 引擎/auto_trigger 仲裁/失效与预算事务/rollover 边界/对账命令族）；控制面篇相应章节降为指针（v0.3） | owner 追问"是否还有设计基石类机制未整理"，批准按 Hook→状态机顺序立篇 |
| 2026-08-28 | 新立《Claude Code Hook 锚点全图与选点审查》：对照官方 Hooks Reference（核对日 2026-08-28）与 owner 知识库 SebsVault/LLM/Hook，收录平台全部 31 个锚点（九段生命周期 × 触发/阻断/matcher/消费状态）、共享机制（输出三层字段/退出码含 JSON 首字符解析规则/并行最严优先/matcher 语法陷阱/五类 handler 支持矩阵/超时特例/异步/信任边界）、现役七锚点选取理由、十个候选评估备忘与六问选点准入流程；接线篇改写为"七锚点是消费快照非平台边界"（v0.1.1） | owner 指示：hook 挂点不应被现用集合限死，须有一份表述平台完整机制的文档供后续迭代对照审查锚点是否合适 |
