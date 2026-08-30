# L4 — Hook 与平台事件接线治理（Hook & Platform Wiring Governance）

> 层：第四层｜机制域：Hook 事件注册、payload 契约、输出/退出码决策契约、失败态度（fail-open / fail-closed）、身份识别链、guidance 与执法分界
>
> 上游：L1 D2（自然路径执法）与公理五；Claude Code Hooks 平台能力基线；全部依赖事件总线的 L3 Stage
>
> 四家族分工：[L4 Agent 调度与治理](L4-agent-dispatch-governance.md)定义「谁在责任上行动」（含 TeammateIdle/SubagentStop 的**决策矩阵本体**）；[L4 权威状态机与迁移事务核心](L4-state-transition-core.md)定义「事实如何合法变更」（含 gate/auto_trigger 引擎机制）；[L4 运行时控制面](L4-runtime-control-plane.md)定义「写什么内容合规」（屏障规则表、脱敏闸等）；**本文档定义这些决策如何坐上同一条平台总线**——payload 怎么进来、结果怎么回去、出错时朝哪个方向失败。另外：平台共有 31 个 Hook 锚点，其全集、触发语义与选点审查流程的唯一权威是 [L4 Claude Code Hook 锚点全图](L4-hook-anchor-catalog.md)——本文 §2 的十类注册项是从该目录选出的**消费快照，不是平台边界**。
>
> 状态：v0.2.0。【当前实现】小节均为代码核对结论（核对日 2026-08-28），附证据位置；未经验证的平台假设在 §10 显式披露。

## 0. 准入逻辑：为什么这配一份独立 L4

基石五问自测：

1. 自有对象模型？——有且是全系统唯一的事件侧接口契约：`policy.Input`（官方 payload 无损入口）、`policy.Decision` 六值判定、wire envelope 输出形状（§3–§4）。
2. 五个以上 stage 深度消费？——全部 twelve stages 的每一次工具调用、会话启动、压缩、停机都经它（S6 写面、S7 提交与冻结、S8/S9 双屏障、S5 locked artifact、S11 自动化停机……无一例外）。
3. 独立失败面？——有：识别失败、控制面不可读、超时、payload 字段缺失；fail-open 还是 fail-closed 是本域的核心设计题且曾反复返工。
4. 平台边界接线契约？——它是四家族中与外部平台耦合最深的一个（Claude Code hooks 的 matcher/exit-code/payload 语义），且没有任何别的文档 owns 它。
5. 风险分级与验收？——有：事件分类学（observe/guidance/enforce）、迁移白名单治理、doctor 一致性检查、专项回归测试组。

五问全中。此前它的碎片散在 settings.json、internal/hook/*、internal/migration/templates.go、docs/hook-policy.json 和各 L3 的 hook 表格里——典型"人人消费、无人定义"。

## 1. 问题定义与本域目标

Hook 是 D2 的物理载体：所有把控必须挂在结构上必经的路径上。但若每个 handler 自行决定 payload 解析、反馈格式和失败方向，就会出现历史上真实发生过的失效：自造字段假装唤醒、warn 被悄悄升格成 block、audit 决策泄漏进用户可见通道、测试靠专用伪字段通过而真实平台不工作。

本域目标（全域不变量）：

- **单一总线**：所有事件走同一个二进制 `.claude/bin/loop-harness hook --event <E>`，路由收敛于 CLI 的唯一 evaluate 入口；不允许第二个直接注册的命令。
- **官方 payload 无损进入**：平台上有的字段一个不丢地到达策略层；策略层永不发明平台上不存在的输入。
- **输出可预测**：给定决策值与事件类别，envelope 形状、退出码、stderr 用途完全确定（§4 表）。
- **失败方向是设计而非事故**：每条路径的 fail-open / fail-closed 都有名有姓、有注释、有测试（§5）。
- **绝不模拟平台**：Runtime/Hook 都不得伪造"平台已被唤醒/已阻止"的事实（继承调度篇 §10.2 的铁律，本文管其在接线层的表达）。

## 2. 事件注册面

### 2.1 设计（当前实现）

settings 注册十类事件，统一 timeout=10s，matcher 如下：

| 事件 | matcher | 行为类别 |
|:--|:--|:--|
| `PreToolUse` | `Write\|Edit\|MultiEdit\|Bash\|NotebookEdit\|Task\|TaskUpdate\|Agent\|mcp__.*` | enforce（§4；PowerShell 仍按 B11 暂缓）|
| `PostToolUse` | `SendMessage` | observe（纯观察员）|
| `PostToolUseFailure` | `*` | observe（原生失败审计；不阻断）|
| `SessionStart` / `PreCompact` / `SubagentStart` | （全匹配） | guidance |
| `SubagentStop` / `TeammateIdle` | （全匹配） | enforce（stop/idle 决策，矩阵本体在调度篇）|
| `Stop` | （全匹配） | enforce（Main 收工门）|
| `ConfigChange` | `*` | observe（治理配置变更审计；不阻断）|

四条治理规则：

1. **占格先过选点审查**：平台上可用格子远不止当前十类（全集见[锚点全图](L4-hook-anchor-catalog.md)，含 PermissionRequest / TaskCompleted / FileChanged 等 ◐ 候选）。任何新增/退出都先过该目录 §5 六问，再同步三处：本表、迁移白名单、目录状态列——不允许只改 settings。迁移模板中的禁词（PermissionRequest / TaskCompleted）含义是"**未经评审不得接线**"；ConfigChange 已经完成本轮审查，作为只审计观察器接线。
2. **protected_events 对账**：docs/hook-policy.json 的 `protected_events` 必须与 settings 注册集完全一致；doctor 校验 `hook_control.policy_ref` 与磁盘 policy 文件的 version+sha256 一致性，漂移时提示 `runtime reconcile-policy-ref`。
3. ** TASK 模板中的 SendMessage 仍是禁词**：TASK 正文不得指挥 Worker 直接发消息——消息纪律属于 agent-message schema 与调度篇，模板只承载任务事实。
4. dry-run（`renderHook=false`）与真实 hook 走同一 evaluate 路径，仅 output 渠道不同——保证被测即所运。

## 3. Payload 契约（输入面）

`policy.Input` 是全部事件的唯一入口类型；官方字段清单及其地位（设计+当前实现一致）：

| 字段 | 来源 | 地位 |
|:--|:--|:--|
| session_id / hook_event_name / tool_name / tool_input | 平台通用 payload | 通用 |
| agent_id | SubagentStop 等携带 | 官方；允许缺失 |
| agent_type | SubagentStart 携带 | 官方；用于唯一匹配 Assignment 简报，不得凭它猜测多个责任 |
| tool_use_id | 工具事件携带 | 官方；用于区分同一回合内的原生失败审计 |
| file_path / error / source | ConfigChange、PostToolUseFailure/SessionStart 携带 | 官方可选；缺失时仍保留事件审计，不发明值 |
| teammate_name / team_name | TeammateIdle 官方 2.1.218 payload | 官方；识别链一级来源 |
| transcript_path / agent_transcript_path / last_assistant_message / stop_hook_active | SubagentStop 官方 payload | 官方；`stop_hook_active` 用于防 hook 自激循环 |

三条铁律：

1. **无损透传**：以上字段逐一进入 Input，可缺失、不可改写、不可丢弃（omitempty 只是序列化整洁，不是许可丢字段）。
2. **禁止发明输入**：任何"为了测试好写而在 payload 里约定私有 agent_id"的做法违宪；测试必须用官方 payload 形状（历史反面教材：自造 agent_id 曾让 TeammateIdle 测试全绿而真实环境不可用）。
3. **有效身份回退一次**：`EffectiveAgentID = AgentID 否则 TeammateName`；回退到此为止，再往下是 §6 识别链的事。

## 4. 输出与决策契约（Output 面）

### 4.1 决策值六集合的 wire 行为

| Decision | stdout | exit | stderr | 说明 |
|:--|:--|:--|:--|:--|
| allow | PreToolUse 带 hookSpecificOutput；其余 `{"systemMessage"}` 或空 | 0 | − | 默认兜底值 |
| info | 同 allow，body 为阶段横幅 | 0 | − | 状态投影 |
| warn | systemMessage 含 banner+missing+recovery+retry | 0 | − | **永远不许升格为 block**（adapter 戒律二）|
| audit | 空 | 0 | − | 只落 `.claude/hook-decisions.jsonl`，用户不可见（戒律三）；记录 `elapsed_ms` |
| deny / block @PreToolUse | permissionDecision="deny" + systemMessage=恢复包 | **2** | − | 唯一在 stdout 表达 deny 的事件类 |
| block @TeammateIdle/SubagentStop | **stdout 静默** | **2** | 单行具体下一步 | 反馈必须经 stderr 回到同一活会话 |

adapter 三戒律（本域宪法，源码注释已固化）：① adapter 不拥有生命周期合法性，只渲染 Controller 结论与正向指导；② 永不把 warn 提升为 block；③ audit 判定零协议输出。

### 4.2 PreToolUse 的 quality_gate 投影

deny 之外的一切 PreToolUse 输出必须携带顶层 `quality_gate` 子对象（status/gate_id/candidate_transition/observed_revision/fingerprint/missing/evidence_refs/error_code/conflicts/transition_committed/next_cursor）：机器推进的事实（含刚提交的迁移与下一 cursor）从同一条消息里回到 Main 会话。outbox 与 wire payload 双写同一投影，防两类消费者各看一套真相。`error_code`/`conflicts` 是 UNKNOWN 等诊断的机器字段，必须同时出现在 Hook envelope schema 与 agent 可读 recovery packet 中。

内部 rule anchor 可以继续使用代码常量的稳定内部命名；跨进程的
`DecisionEnvelope.rule_id` 和 `matched_rule_ids` 在序列化边界统一为
`HOOK_[A-Z0-9_]+`。这样既不破坏内部 recovery anchor，也避免不同消费者拿到
两套不可验证的规则身份。原生 `ConfigChange`/`PostToolUseFailure` 的审计 envelope
以稳定 payload fingerprint 参与 decision identity：同一 payload 重试幂等，不同
文件路径、错误或来源各自保留审计行；audit-only 写失败只报告而不改变原工具结果。

## 5. 失败态度矩阵（本域核心设计）

两条对偶原则，适用范围精确到事件×条件，不是全局开关：

- **屏障类fail-closed**：受控写入路径上当控制面不可读、阶段无法解析时宁可拦截（unreadable stage 不得静默解锁 locked artifact；mutating PreToolUse 在 runtime 不可读时产出 typed block）。
- **观察/停止判定类 fail-open**：对象无法识别、Payload 不足以定位责任时不猜绑不伪造——PostToolUse 识别失败=静默 observation、SubagentStop/TeammateIdle 无法定位已知 assignment 或属 one_shot/approval 派单/blocked 有据/completion 已报时一律放行。
- 共同下限：**两种方向的分支都必须有解释注释与测试**；出现第三种"没想清楚就放行/拦住"即为违宪。

当前实现分布（核对日 2026-08-28）：PostToolUse 全程 observer 永不阻断；stop/idle 六条 allow 快捷出口集中在 StopIdleDecision；dynamic Bash 写变异解析不出路径时反手 fail-closed；主策略 Evaluate 无规则命中默认 allow（fail-open 兜底），首写屏障等 agent-scoped 规则仅在 PreToolUse 且前置决策为 allow 时追加求值。

## 6. 身份识别链

四级顺序识别（PostToolUse 的发送者绑定为例，PreToolUse 经同一 CLI 入口做两级补齐后加载 hookctx）：

```text
payload agent_id → teammate_name（顶层）→ tool_input.teammate_name → 唯一活跃候选兜底
```

约束：兜底级只在恰好存在唯一处于计划形成态的 agent 时命中；authoring 占位符（`TODO(planner):…`）在任何一级都不得成为身份（有反向测试锁定）；四级全空 = 未识别，按 §5 矩阵行动；主会话（无 Agent 上下文）天然在 agent-scoped 规则之外，屏障对其一律豁免。

## 7. Guidance 与 Enforcement 的分界

| 类别 | 事件 | 物理表现 | 消费者 |
|:--|:--|:--|:--|
| observe | PostToolUse(SendMessage) | 不产 permissionDecision、不动 lifecycle、无 exit 信号 | CLI 落 plan checkpoint（控制面的屏障有了触发事实）|
| observe | PostToolUseFailure / ConfigChange | 原生信号只落 audit，不改变原操作 | S7/S8 证据消费者、治理审计 |
| guidance | SessionStart / SubagentStart / PreCompact | systemMessage +（SessionStart/SubagentStart）原生 `additionalContext`；短包只放当前位置/读序/唯一下一步 | Main/Worker 重入座 |
| enforce | PreToolUse；Stop；SubagentStop；TeammateIdle | deny/exit2/decision block | 工具调用方、Main、Worker |

判别尺（借用 L3-README 注意力分配三问）：观察信息喂给 Runtime 记录；指导喂给人与 Main 的注意力；只有确定非法/不可逆才用平台强制。guidance 文案的最小可执行性有专门测试锁定（deny 恢复包必须含九要素：LOOP RECOVERY/Next/ProtocolRef/ManualRef/Read in order/Agent Team 等）。

## 8. 与其余三家族的边界（防重复定义）

| 内容 | 权威所在 | 本文角色 |
|:--|:--|:--|
| TeammateIdle/SubagentStop 决策矩阵（何时 block 何时 idle） | 调度篇 §10.2/10.3 | 只管把 block 变成 exit 2 + stderr 到达同一会话 |
| 首写屏障等六条规则的内容与豁免面 | 控制面篇 §8 | 只管 evaluate 入口在 PreToolUse 上的追加求值次序 |
| gate/auto_trigger 的引擎机制与候选仲裁 | 状态机核心篇 §6 | 只管 quality_gate 结果投影回 wire |
| 采集/脱敏闸的内容规则 | 控制面篇 §9 | 本文不涉及 |

## 9. 治理流程

- 新增/修改任何 handler：先在本文件登记事件类别与失败态度 → 过 §5 矩阵问答 → 补三类测试（官方 payload 行为、失败方向、豁免面）→ migration 白名单如有变动双写 → doctor 过 policy_ref 一致性。
- 移除决策：如 protected_commands 从 hook 主链路退出那样（§10），必须在 hook-policy schema 保留兼容字段的同时把"`不再被咨询`"写成带出处的注释事实，防止后来者误以为清单仍在执法。

## 10. 当前事实边界（诚实条款）

核对日 2026-08-28：

1. **doctor 与平台 smoke 的职责分开**：`doctor` 只做仓库内静态一致性（manual agreement、repository schema 集合、evidence catalog、policy_ref 漂移）；`health` 读取累计运行信号和 Hook 时延。`make platform-smoke` 会在真实进程边界执行 settings 中的命令与官方 payload，Claude CLI 存在且可运行时还报告版本；`make platform-smoke-required` 在没有可运行 Claude CLI 时以环境债退出 77。它不伪造完整 Claude 会话，exit-2 是否让同一 teammate 继续仍需在 disposable project 中按本节验收步骤实测。
2. **protected_commands 已退出 hook 执法**：`extension_protected_commands` 置空且有测试注释锁死"Hook no longer loads docs/release_audits/protected_commands.json"；分类器仅供 Bash 变异路径分析使用。发布保护的现状 = S11 结构性停机 + 人操作纪律，这是有意为之的现状而非未完成事项（相应更正控制面篇 §15 的早期表述）。
3. Stop-idle 判定所依赖的全部决策逻辑收敛在 StopIdleDecision；Main 的 Stop 收工门收敛在 MainStopDecision，并在 Controller cycle 之前 preflight，避免已知非法收工先触发自动迁移或 milestone 写入；两者输出纪律（exit 2 stderr 单行下一步）由 CLI 层执行。
4. SessionStart 的 additionalContext 只承载压缩后的阶段/下一步；SubagentStart 只有唯一 Assignment 匹配时才承载 scope/完成条件，歧义时不猜。完整事实仍在 Guidance、Assignment Record 和 Runtime。
5. PostToolUseFailure/ConfigChange 是 audit-only：它们补平台原生信号，不替换已有 wrapper，也不假装拥有拦截能力；写 audit 失败保持 fail-open。
6. 一切 timeouts 固定 10s：若某 handler 未来需要更长预算，超时后的半途状态必须满足状态机篇的可恢复要求，而不是靠加长 timeout 掩盖。

> 本域的缺陷登记册属于目标项目的本地迭代产物，不随模板仓库发布。推进候选锚点时，应在目标项目的 `docs/bugs/` 登记册中建立工单，并在完成后回写本域状态列。

## 11. DoD

- 十类事件注册、白名单、protected_events 三处一致，doctor 可对账；
- 任何事件新增 handler 前都有类别与失败态度登记，任何失败分支都有测试；
- 输出契约表（§4.1）覆盖全部六种决策值且与源码注释一致；
- 无自造 payload 字段存活于任何测试；
- 平台验收入口：先运行 `make platform-smoke`，再在装有 Claude Code 的 disposable project 运行 `make platform-smoke-required` 并记录版本、官方 payload、SubagentStop/TeammateIdle 的 exit-2 同会话继续结果；不要把 `doctor passed` 解读为该实测已完成。

## 变更记录

| 日期 | 版本 | 变更 | 依据 |
|:--|:--|:--|:--|
| 2026-08-28 | v0.1.1 | 边界叙事修正：明确七个注册锚点是消费快照而非平台边界；事件准入规则改写为「占格先过《Hook 锚点全图》六问」，禁词重定性为"未经评审不得接线"；头部挂接锚点全图为平台侧唯一权威 | owner 指示：文档逻辑不应限制只能用现有 hook 挂点；建立平台锚点全集文档供迭代对照审查 |
| 2026-08-28 | v0.2.1 | 对齐质量门诊断字段与 Hook schema；明确内部 rule id 到 `HOOK_*` wire id 的边界转换、原生观察事件按 payload 去重、audit-only 写失败 fail-open，以及 Assignment 歧义不猜绑 | S10-WALK-A 与 HOOK-B07/B09/B06 回归审查 |
| 2026-08-28 | v0.2.0 | 接入 MCP PreToolUse 通配与未知工具兜底、Main Stop 收工门、elapsed_ms 时延审计、SessionStart/SubagentStart additionalContext，以及 PostToolUseFailure/ConfigChange 原生审计观察器；明确 B08/B10/B11 仍为按数据触发的后续项 | HOOK-B01/B03/B04/B05/B06/B07/B09 实装与契约测试 |
| 2026-08-28 | v0.1.0 | 初版：把散落在 settings/migration/internal.hook/hook-policy 各处的事件注册、payload、输出契约、失败态度、识别链、guidance 分界统一为本域唯一权威定义；核实并纠正 protected_commands 已退出 hook 主链路的事实；承接调度篇遗留的真实平台 doctor 缺口的验收归属 | owner 批准的基石抽取批次（其一）；五问自测见 §0 |
