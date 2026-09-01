# L4 — 权威状态机与迁移事务核心（Authoritative State & Transition Core）

> 层：第四层｜机制域：权威状态存储模型、单写者写入协调、崩溃安全写序与恢复标记、实体生命周期索引、guard/action 迁移引擎、自动推进与候选仲裁、失效与预算事务、归档与周期边界、对账命令族
>
> 上游：L1 D1（权威外置）与 D7；L2 状态概念层；全部读写 runtime 的 L3 Stage
>
> 五家族分工：[调度](L4-agent-dispatch-governance.md)定义谁行动，[Hook 接线](L4-hook-platform-wiring.md)定义决策如何上总线，[运行时控制面](L4-runtime-control-plane.md)定义内容合规规则与词汇词典，[revision 使用篇](L4-revision-usage.md)定义内部提交序号与命令协调——**本文档是"事实住哪、怎样变更才算合法、崩了怎么自证"的唯一权威**。迁移 ID 五类形态学、guard/action 引擎、auto_trigger 仲裁的本体在此；L2 只保留生命周期概念与全局规则的"存在声明"，各 L3 只写消费。
>
> 状态：v0.1.0。【当前实现】均为代码核对结论（核对日 2026-08-28）；意图与现状的差距在 §11 披露。

## 0. 准入逻辑

基石五问：① 自有对象模型——`loop-state` 全部顶层实体 + 三台 phase 机 + 三套实体生命周期，是全系统最大的一台状态机；② 全部十二个 stage 直接读写；③ 独立失败面——stale 冲突、半写状态、journal 断尾、指纹漂移、pending 残留，每种都有专门恢复语义；④ 外部边界——文件系统持久化格式与锁协议即是接线面；⑤ 自带风险分级（guard/action/evidence 三级约束）与专属验收命令族。五问全中，成篇。

## 1. 存储模型对象总览

### 1.1 loop-state 顶层结构

`.claude/loop-state.json`（schema_version 常量 `1.1.0`）顶层键及其地位：

| 键 | 地位 | 要点 |
|:--|:--|:--|
| `schema_version` / `updated_at` | 必填 | 版本常量 / 写入时间 |
| `runtime_id` | 身份 | `^loop-[A-Za-z0-9._-]+$` |
| `definition` | 指纹引用 | path/version/sha256 指向 loop-definition |
| `revision` | Runtime Writer 提交序号 | 单调递增、无上限；由 Writer 内部生成，Agent 不手工提供 |
| `lifecycle` | 当前位置 | `{state, phase, phase_revision}`；12 个顶级 state × 可嵌套 phase |
| `authorization` | 授权记录 | mode（none/loop/binding）/command/actor/occurred_at |
| `bound_req` / `binding_receipt` | 基线 | status 固定 locked；receipt 存 TR-001 双指纹收据 |
| `change` | 变更记录 | CHG 条目 |
| `baseline` | 代际 | `{generation, captured_at}`；amend 时 +1 |
| `review` | 验证投影区 | round / clean_round / round_entry / plan / claims / assignments / observation_batch / investigation / repair 指针 |
| `configuration` | 预算 | 见 §7 |
| `hook_control` | 总线挂钩 | policy_ref（version+sha256）、mode（disabled/audit/enforce）、健康计数 |
| `documents[]` | 登记文档 | 14 类 documentReference（req/design/ui_baseline/ui_prototype/contract/task/review/qa/e2e/bug/acceptance/release_audit/team_manifest/prompt）|
| `entities` | 实体数组 | agents/tasks/bugs/teams/findings/finding_supplements 六组 |
| `evidence[]` | 证据账本 | 28 类 kind；不变式：valid ⇒ invalidated_* 三字段全空；invalid ⇒ 三者全非空 |
| `blockers[]` | 阻塞留痕 | 9 类 kind（missing_guard/document_conflict/req_change/security/compliance/irreversible_action/repair_limit/runtime_integrity/human_decision）|
| `pause` | 暂停检查点 | from_state/from_phase/phase_revision/baseline_generation/review_round/reason/required_human_action/**document_fingerprints**/paused_at |
| `journal` | 游标 | 仅 `{path=".claude/loop-events.jsonl", last_sequence, last_event_id}`——条目本体在外部 JSONL |
| `last_transition` / `milestone` | 快照 | 最近迁移快照 / quality_gate 里程碑 |

### 1.2 journal 条目形状

JSONL 公共字段：schema_version、runtime_id、event_id、idempotency_key、sequence、event、outcome、actor{type,id}、request_id、baseline_generation、before_revision/after_revision（Runtime Writer 内部提交序号）、from/to、evidence_ids、message、occurred_at；transition_committed 类追加 transition_id、gate_id（默认 `MANUAL`）、gate_fingerprint（默认 `sha256:manual`）、producer_responsibility、guard_results、action_results。一致性不变式：tail 的 sequence/event_id 必须等于 cursor 二元组。超过 10k 行时自动归档为段文件 `loop-events.jsonl.archive.<tailSeq>.jsonl`（见 §11，`maybeRotateJournalLocked`，marker 事务段感知）。

### 1.3 runtime-archive

终局归档目录 `<archiveRoot>/<runtimeID>-r<revision>-<UTCStamp>/`：其中 `revision` 仅作 Writer 生成的归档排序元数据；目录包含 `loop-state.json` + `loop-events.jsonl` + `rollover.json` 清单（含 state/journal 双 sha256、approved_by/approval_evidence_id、disposition）。disposition 取值：空串（TR-001 边界复位）与 `unbound`（撤销授权）。弃周期全程可审计，撤销≠抹除。

## 2. 状态全集与生命周期索引

- **12 个顶级 state**：inactive / planning / document_verification / building / verification / bug_resolution / acceptance / release_audit / paused / awaiting_human_release / release_authorized / aborted；终态仅 release_authorized 与 aborted。
- **三台 phase 机**（cursor = `state.phase` 复合坐标）：planning.{design→contracts→tasks}；verification.{planned→running→cannot_clean→discovery_draining→observation_sealed→clean}；bug_resolution.{investigation→bug_report_review→repair_readback→planning→reproducing→fixing→targeted_reverification→ready_for_full_review}。
- **实体生命周期**（entity_lifecycles）：agent 初始 spawned 终态 done/stopped 共十一态十三转移（readback_started→reading；readback_submitted→understanding_submitted；understanding_approved / understanding_rejected 回 reading / document_conflict_reported→blocked；activation_sent 双入口（submitted 直通、approved 后备用）→activated；work_started→working；completion_reported→reported；completion_acknowledged→done；work_blocked→blocked；blocker_resolved→working；shutdown_approved→stopped）；task 八态（candidate…done/cancelled）；bug 十态（draft→…→closed/rejected/duplicate）。
- **REQ 七动词的机械落点**（生命周期概念仍以 L2 为概念源，此处是唯一机械形态表）：bind=TR-001（boundary reset，双指纹收据）；pause=GTR-001 及七个人闸变体共用同一 capture_pause_checkpoint；resume=TR-019（sentinel `RESUME_FROM_PAUSE` 解 pause.from_*，逐指纹核对，漂移即拒）；amend=TR-020（generation+1 + 换基线 + 下游全失效）；abort 双入口（paused 态 TR-021；S11 处置 TR-030）；approve=TR-025（只授权不发布）；**rollover 与 unbind 不是 transition**——它们是 Store API（跨文件原子操作），这正是 L2「三条裁决」②③ 在代码层的形态。

## 3. 单写者与内部提交一致性

- 读写分型：`NewStore` 只读快照；**`NewWriter` 强制注入 CandidateValidator**（缺失或恒空校验直接拒启）——语义验证器不是可选装饰，是写入资格的一部分。
- 写主流程（当前实现）`Update(expectedRevision, mutation)`：加文件锁 → recoverPendingWritesLocked → revision 对比（不等即 `ErrStaleRevision`）→ applyMutation。目标态由 [revision 使用篇](L4-revision-usage.md)规定：正常命令在锁内读取当前 Runtime 并提交，显式 `expectedRevision` 只保留给高级调用者；身份轴另有一条：`ErrStaleRuntimeIdentity`。这里保留 CAS 仅为底层兼容实现，不构成单主会话的业务并发模型。
- 锁协议：`statePath+".lock"` 的 O_CREATE|O_EXCL owner 文件（PID+纳秒），5ms 轮询，超 30 秒视为陈旧回收。
- 定向错误词表：ErrStaleRevision / ErrStaleRuntimeIdentity / ErrPendingRuntimeOperation / ErrBaselineDrift（resume 指纹漂移，路由 amend）/ RepairLimitError。每条 stale 都必须携带"观察到什么/期望什么/下一个动词"（词典与铁律见控制面篇 §2，两文按"规则/引擎"分工引用同表）。

## 4. 崩溃安全写序与恢复标记

提交固定四步：**pending marker 落盘 → state 原子写 → journal 追加 → 清除 marker**。三个专用 marker（路径均为 state 文件旁路）：commitPending（`.pending.json`）、statePending（`.fingerprint.json`，指纹刷新专用且不 bump revision）、rolloverPending（`.rollover.json`）。恢复器按 **commit → fingerprint → rollover** 顺序尝试收敛，各自校验源/目标哈希配对后才放行——半写状态永远可从磁盘重建（D1 的物理兜底）。底层原语统一为临时文件+rename+syncDir；journal 追加以 O_APPEND+Sync 收口。

> **RC-10d 决策（RC-17 收尾）：review-result 提交的巨事务不拆段。** S7 的 result submit 在单 CAS 事务内串行完成结果消耗、findings 落账、轮次推进、seal/clean/pause 分支（见 `internal/review/submit.go`），审计曾建议拆为多段 CAS。决策：**接受巨事务**。理由：拆段会在段间引入半完成窗口——各段之间轮次可见但未收敛，消费方（轮次调度、退出守卫、P0 抑制）会在中间态上做出错误仲裁，而崩溃安全本就由 §4 的四步写序兜底，拆段不增加任何恢复能力，只增加中间态类别。替代方案是观测不拆分：`RecordS7SubmitPhase` 按 `result_consumption / findings / advance / pause / seal / clean` 六相记录段内耗时（`loop_s7_submit_phase_ms`，经 FormatS7 渲染）。**拆分阈值：仅当六相观测显示 p95 > 100ms**（锁持有时间成为调度瓶颈）才重开拆段议题；当前无证据达到该阈值，故不拆。

## 5. 迁移引擎

### 5.1 注册表架构与两类 guard

guard 与 action 都是**注册表模式**（编译后实测：59 个 guard / 48 个 action 各一表，由 `transition.GuardNames()` / `transition.ActionNames()` 枚举），LoadCatalog 启动期 fail-closed 校验：definition 里声明的每个标识符必须在注册表中存在。注意 registry 与 definition 是两个集合：代码注册表是全集（59/48），`docs/loop-definition.json`（顶层 + global + phase 机 + entity lifecycle 合计 81 条 transition 声明）只引用其中被实际接线的子集（58 个 unique guard / 33 个 unique action 引用）——registry ⊇ definition 是不变式，两者数字不一致不是漂移。guard 分两类：`GuardSemanticCheck`（真实语义判定：clean-round 七检查、baseline 指纹比对、exact-set 封批等）与 `GuardEvidenceAttestation`（要求必填 evidence 槽位已解析的在场性检查，槽位合法性由 evidence catalog 另行守护）。**面向新迁移的纪律：禁止再挂接标注为旧模式的 guard 影子**（guards.go 中有显式的防过度工程警示注释：新 transition 只接两类注册项之一，其余一律走 action 或不做）。

### 5.2 on_guard_failure 的真实现状

definition 现在只允许 `reject | pause` 两值（RC-06/C-12：`warn_and_retry` 声明已被整体删除——引擎从不分支于此，guard 失败一律返回错误交给调用方；名不符实的枚举值只会制造虚假承诺）。整改后重试的语义仍由 PreToolUse 触发路径本身承载；若未来要引入真正的退避策略，属于本文件的修订事项而不是 handler 的局部发明。

### 5.3 迁移内校验

Apply 前置三查：cursor 匹配（含 human_boundary 动词的 actor 白名单）、required_evidence 逐一过 `validateCurrentEvidence`（存在于 runtime 且 valid、kind 兼容、baseline_generation 同代、review_round 同轮、path 安全、sha 一致）、human_decision 类证据的 semantic identity 与一次性消费规则。revision 的内部提交检查和 Agent-facing 默认语义见 [revision 使用篇](L4-revision-usage.md)，不在此重复定义。

## 6. 自动推进与候选仲裁

- auto_trigger 形状被 schema 加载器硬约束：event 必须 PreToolUse、actor 必须 hook_controller、max_per_event==1、human_required 不允许为 true——人闸在结构上进不了自动通道（与 hook-policy protected_events、definition 三道一致才构成完整防线）。
- 候选仲裁：按 cursor 过滤出自动候选后，由事件与已满足 gate 双因子解析出 **0 或 1 个**迁移；多候选同时命中返回稳定错误码 `LOOP_TRIGGER_CONFLICT`——控制器永不靠声明顺序猜优先级（原则表述归控制面篇 §7.5，本文管仲裁机制）。
- 人工终局三态（awaiting_human_release/release_authorized/aborted）在 Controller 层有硬 switch 直接返回空候选，与 auto_trigger 缺失互为冗余防线。
- 启动期还强制同一 cursor 下全部自动候选共享同一 selector——声明层就不给"多事实源竞争"留入口。

## 7. 失效与预算事务

### 7.1 五条失效 action 的归属

| action | 用在哪 | 语义 |
|:--|:--|:--|
| `increment_baseline_generation` + `invalidate_all_downstream_evidence` | TR-020 amend | generation+1；凡 baseline_generation ≤ 旧代的证据全部 invalid（invalidated_by=TR-020，rule=baseline_generation_change）|
| `invalidate_affected_evidence` | TR-007/013/023 规格缺陷回流 | 以 AffectedPaths 经影响计算选 victims（排除本次迁移自带证据）；REQ 基线未变的担保来自 req_baseline_unchanged guard |
| `invalidate_consumed_review_evidence` | TR-004 S5 返工 | 本轮被消费的 document_review 按 consumed_fix_record 失效 |
| `invalidate_human_release_{acceptance,release_audit}_evidence` | TR-028/029 | 按 kind 集合粗粒度失效（与人闸型家族对应）|
| `reset_s7_review_after_governance` | GTR-006 预算治理 | 重置 review 全部投影区并失效除自身人闸决定外的全部有效证据 |

失效家族的四分类框架（代际/轮际/修复型/人闸型）见控制面篇 §4.2——那里是分类学，这里是执行体。

### 7.2 预算配置全表

| 字段 | 默认 | 消费点 |
|:--|:--|:--|
| configuration.repair.max_attempts_per_bug | 3 | 超限抛 RepairLimitError（升级为人信号）|
| configuration.repair.max_same_contract_failures | 2 | 已接入：internal/assignment/bug_lifecycle.go:341 checkRetryLimits → readBugInt(repair, "max_same_contract_failures")，超限拒绝并报 exceeded max_same_contract_failures |
| configuration.repair.max_full_review_rounds | 5 | 开新轮 action 内做超限判定 |
| …last_budget_decision | null | increase_budget / return_to_governance 决策的完整审计对象（证据 id、前后预算值、authorized_by、committed_revision 十字段）|

预算调整本身是一笔事务：`ApplyS7BudgetDecision` 在同一个 Writer 事务内同时写证据、改预算、落决策记录；return_to_governance 经 GTR-006 把 Runtime 送回 planning 重开。

## 8. 归档与周期边界

- **rollover 前置五验**：Runtime 处于终态二选一；ApprovedBy 与 EvidenceID 齐备；该证据必须是 kind=human_decision、valid、scope 含当前 Runtime 的语义动作 `runtime_rollover:<id>`；fresh inactive 新 Runtime 通过三零校验（revision=0、last_sequence=0、event_id=nil）；最后走 rolloverPending 三阶段恢复协议完成目录化归档与新周期落盘。
- **unbind** 是非终态可用的姊妹操作（scope=`runtime_unbind`，在飞实体软门信息进清单 extras）；与 rollover 共享 archiveAndReset 机器，只有 disposition 不同。
- **pause/resume 的指纹闸**：capture 落下的 document_fingerprints 被 restoreFromPause 逐文件重算比对，漂移即 ErrBaselineDrift——这就是 L2「恢复前校验基线未漂移」的引擎形态。

## 9. 对账命令族的契约

| 命令 | 检查范围 | 自动修复程度 |
|:--|:--|:--|
| `doctor` | manual agreement + 仓库 schema 全集 + evidence catalog + hook policy_ref 漂移 + metrics | **零修复**，报错附下一步动词 |
| `validate --all` | schema/example 对、contracts/scenario 校验、catalog/skills/agents、migration 模板、runtime 状态可达性、evidence 槽位覆盖 | 零修复，exit 1 |
| `runtime reconcile` | journal 断尾检测 | **仅修 journal**：追加一条 `journal_reconciled` 补齐游标，绝不改 state/revision |
| `runtime reconcile-policy-ref` | hook_control.policy_ref 与磁盘漂移 | 重写 policy_ref |
| `runtime fingerprint` | documents[].sha256 与磁盘 | statePending 协议下重算刷新，不 bump revision |

设计准则：**能自我修复的限于"可从磁盘无损重建的事实"（journal 尾巴、policy_ref、指纹镜像）；凡涉及判断的事实漂移只能报告给人**。新增对账能力直接沿用此行。

## 10. 与其余文档的分工回收

五类迁移 ID 形态学表（TR/GTR/PTR/runtime-authority/cursor）与 guard/action 行为本体已收拢于本文件 §5；控制面篇相应章节降为指针。干净轮七检查、能量函数仍在控制面篇（它是求值规则不是状态机本体）；写屏障六规则在控制面篇（它是 policy 内容）；auto_trigger 在 wire 上的投影归 Hook 接线篇（§4.2 quality_gate 子对象）。

## 11. 当前事实边界（诚实条款）

核对日 2026-08-28：

1. journal 段轮转已落地（`internal/runtime/store.go: maybeRotateJournalLocked`，阈值 10k 行，`loop-events.jsonl.archive.<seq>.jsonl` 段感知、`inspectJournal`/`journalLineCount`/`journalContains` 跨段合并、marker 事务恢复；`JournalNeedsRotation` 仍作阈值探针，`LOOP_HARNESS_JOURNAL_DIAGNOSTIC` 输出段计数）。剩余边界：归档段不自动 purge，仅按段计数增长。
2. `max_same_contract_failures` 已接入（见 §7.2 与 bug_lifecycle.go:341），属 opt-in 限次保险，默认 0=unlimited。
3. integration checkpoint 的持久化通道未接线（internal/runtime/integration_register.go 自述"合同面已就绪待 BUG-06"，当前 loader 返回 nil）；S6 集成事实目前依赖任务侧投影。
4. GTR-004 桥无生产调用方（S1 已诚实登记）；此类"声明存在但不可达"的条目在 catalog 属合法遗留，但每个都必须有登记处，不允许无主孤儿。
5. `automation.eligible=false` 与 Controller 空 switch 是双防线而非重复——前者管声明完整性，后者管运行时硬保证；评估删除任一层均需修订本节。
6. ~~`warn_and_retry` 未被引擎消费~~ 已关闭（RC-06/C-12，2026-08-28）：全部 12 处声明改为 `reject`，schema 枚举收敛为 `reject | pause`，引擎行为与声明一致。

## 12. DoD

- 任何一次合法变更都能回答四个问题：谁授意的（actor/request_id）、基于哪个修订（before/after_revision）、动了哪些证据（evidence_ids/action_results）、崩溃到哪一步如何重建（marker+journal tail）；
- 三份骨架文件（state/journal/archive）任意时刻两两可通过 §9 命令族对账收敛；
- 所有 stale/drift/pending 错误都在定向错误词表内且携带下一动作；
- 人闸无法经任何自动通道触发（三层结构性拦截持续成立）；
- 新增实体、phase 或迁移先入本文件的索引节，再写代码。

## 变更记录

| 日期 | 版本 | 变更 | 依据 |
|:--|:--|:--|:--|
| 2026-08-28 | v0.1.0 | 初版：把散落在 runtime store/transition engine/catalog/controller 各处的存储模型、CAS 本体、崩溃协议、实体生命周期索引、迁移引擎、自动推进仲裁、失效与预算事务、归档边界、对账命令族收拢为单一权威；从控制面草案中接管迁移形态学与引擎细节；登记六项诚实缺口（含 warn_and_retry 名实不符） | owner 批准的基石抽取批次（其二）；五问自测见 §0 |
| 2026-08-29 | v0.1.1 | §4 追加 RC-10d 巨事务决策：submit 单 CAS 不拆段，以 RecordS7SubmitPhase 六相观测替代，p95>100ms 才重开 | RC-17 复杂度债收尾 |
