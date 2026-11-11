# `internal/cli` 包在 d0d948a 上的失败测试归因报告

**仓库**：`/home/sebs/go-system-builder`（module `github.com/entroforge/go-system-builder`）
**HEAD**：`d0d948a77d0e9aa1896aae4412fad32ad65351ef`
**父提交基线**：`9bf73ca5dec7fec2d8c90a17d7f00d16ccee7083`
**证据源**：`/tmp/fullcli-out/cli.json`（go test -json，2.4MB）；干净克隆 `/tmp/fullcli`；父提交 worktree `/tmp/ci-parent`（独立 gitdir，位于 `/tmp/ci-lane/.git/worktrees/ci-parent`，**未触碰原仓库索引**）
**产出方式**：只读解析 JSON + 读源码 + 少量定点重跑（共 6 批次父提交对照 / 2 批次 HEAD 抽样）。未重跑整包。

## 0. 交付门说明（重要）

本报告的落盘产物 `/tmp/fail-ledger/REPORT.md` 由**父会话**写入：本 Explore 角色为严格只读（工具集中无 `write`/`edit`，`bash` 仅允许只读检查代码与 Git 状态），无法自行创建文件。
**替代路径**：完整报告内容在本文件 `/tmp/pi-full-report.md` 与父会话的最终回复中返回。
若需要由本角色直接落盘，请将任务改派 `worker`；或移除「写入 REPORT.md」这一文件写入要求，改为直接消费 `final_text`。

## 1. 总览

| 指标 | 数值 | 来源 |
|---|---|---|
| `go test -list` 顶层测试总数 | 1197 | `/tmp/list-cli.txt` |
| 已启动顶层测试 | 1021 | cli.json `Action=run/pass/fail` 去重 |
| 顶层通过 | 768 | cli.json |
| **顶层失败** | **247** | cli.json `Action=fail` |
| **唯一失败子测试** | **138** | cli.json `Action=fail`（含 `/`） |
| 失败实体总数（顶层+子测试） | 385 | 去重后逐实体建表 |
| 因 30m 超时**未启动**的顶层测试 | **176** | 1197 − 1021 |
| 根因组数量 | **13**（12 个具名组 + 1 个长尾组） | 本文第 3 节 |

> **未启动 ≠ 通过**：176 个未启动测试在本次运行中根本没执行。第 6 节单独分析。

### 根因组规模一览

| 组 | 组名 | 实体数 | 顶层 | 子测试 | 判定 | 映射 |
|---|---|---|---|---|---|---|
| R01 | PreToolUse 丢失 quality_gate 投影 | 32 | 8 | 24 | **既有失败**（父提交同样失败） | 新发现（不在 9+3 之列） |
| R02 | 4 个 CLI 子命令被删除 | 7 | 5 | 2 | **本次重构引入的回归** | 新发现 |
| R03 | 原生 Result 无法解回原 Plan 包 | 11 | 7 | 4 | **本次重构引入的回归** | **H9** |
| R04 | Main Inspect 因缺 Worktree/Branch 拒绝 | 16 | 12 | 4 | **本次重构引入的回归** | 新发现 |
| R05 | Stop 未触达原 source gap 诊断 | 15 | 11 | 4 | **本次重构引入的回归** | **H1** |
| R06 | 绑定派发 owner 改写了其他 TASK 事实 | 11 | 11 | 0 | **本次重构引入的回归** | 新发现 |
| R07 | 候选 schema 拒绝历史 hash 字段 | 7 | 2 | 5 | **本次重构引入的回归** | 新发现（蓝图要求） |
| R08 | 同事件重试产生重复审计行 | 6 | 1 | 5 | **本次重构引入的回归** | 新发现 |
| R09 | WorkerChecks 空列表变 nil → JSON `null` | 4 | 3 | 1 | **本次重构引入的回归** | **H7** |
| R10 | review-plan 路径丢失导致 open 空路径 | 14 | 7 | 7 | **本次重构引入的回归** | 新发现 |
| R11 | 原 S9 questionnaire 路径未保留 | 5 | 5 | 0 | **本次重构引入的回归** | 新发现 |
| R12 | Assignment 缺当前 Record 投影 | 3 | 3 | 0 | **本次重构引入的回归** | 新发现 |
| R99 | 长尾：每个断言点 1–5 例的一次性失败 | 254 | 172 | 82 | **混合**（多数回归，见 3.13） | 分散于 H2/H3/H4/H5/H6/MED |

## 2. 与已知 9 条 HIGH + 3 条 MED 缺陷的映射

| 已知缺陷 | 落点（已核对行号） | 对应根因组 | 失败规模 | 证据 |
|---|---|---|---|---|
| **H1** `nativeReviewTaskText` 不读 `last_assistant_message` | `internal/cli/native_review_hooks.go:553-569` | **R05** | 15 实体 | S7 Stop/SubagentStop 兜底收不到答案 → Stop 不再触达 source gap 诊断 |
| **H2** `validateS8ApprovalMode` 0 调用者 | `internal/investigation/hypothesis_result_receipt.go:257` | R99（散点） | 未直接产生 cli 失败 | grep 全仓确认仅定义处 1 处命中 |
| **H3** `requireCompetingHypothesisClosure` 死代码 | `internal/investigation/case_workflow.go:1123`（调用点 `:1171` → `validateCausalClosure`） | R99（散点） | 未直接产生 cli 失败 | `validateCausalClosure` 本身在 HEAD 无调用者 → 整链死代码 |
| **H4** 删掉 `RequireCurrentS6MainStartContext` 调用 | `internal/controlproof/s6_main_baseline.go:176`（定义） | R99（散点） | 未直接产生 cli 失败 | 该包内 0 调用者 |
| **H5** `ValidateContractPacket` 丢失 `fileview.ValidateAuthority` | `internal/runtime/contract_authority.go:70` | R99（散点） | 未直接产生 cli 失败 | 父提交 `9bf73ca` 同文件 `:76` 有该调用，HEAD 已删 |
| **H6** S7 static/behavior 波次门整体删除 | `internal/assignment/review_readiness.go`（整文件删除）、`internal/review/plan.go:194` `StaticClaimsSettled` 残留 | R99（散点） | 未直接产生 cli 失败 | 蓝图 `blueprint/l3-stages/L3-S7-verification-round.md:149` 仍要求两波次 |
| **H7** `WorkerChecks` 空列表变 nil → JSON `null` | `internal/cli/assignment_context.go:339` | **R09** | 4 实体 | `append([]assignmentrecord.WorkerCheck(nil), ...)` 空输入产出 nil slice |
| **H8** `subsetOf` 取代 `sameStringSet` | `internal/investigation/case_workflow.go:1220` | R99（散点） | 未直接产生 cli 失败 | `sameStringSet` 在 HEAD 已无定义 |
| **H9** 路由层无法把原生 Result 解回 Plan 包 | `internal/cli/native_s9_routing.go:15` + `internal/runtime/repair_plan_report_routing.go:23` + 报错点 `internal/cli/native_s9_recovery.go:115` | **R03** | 11 实体（5 顶层 + 5 子测试 + 1 顶层） | 错误串 `native Result path has no exact current original Plan packet` 精确命中 10 个失败实体 |
| **MED-1** `observeMainS9Impact` 0 引用却短路整条链 | `internal/cli/s9_impact_observer.go:20` | R11 / R99 | 5 实体（R11） | grep 确认 0 引用；错误串 `the original S9 questionnaire path was not retained` 来自 `internal/cli/repair_lead_questionnaire.go:548` |
| **MED-2** PreToolUse 提前 return 致工具调用不留痕 | `internal/cli/run.go:2691` | R99（散点） | — | 需二次确认：与 R01 的 `run.go:2961` 条件同源，但未单独隔离出独立失败簇 |
| **MED-3** `native_s6.go` 合并中断后路径被堵死 | `internal/cli/native_s6.go` | R99（散点） | — | 需二次确认：未隔离出以 `native_s6` 为唯一根因的独立簇 |

**映射结论**：9 条 HIGH 中，**H1、H7、H9 直接、成组地解释了本次 30 个失败实体**（R05+R09+R03 = 15+4+11 = 30）。H2/H3/H4/H5/H6/H8 在 `internal/cli` 包内**未直接产生**可归组的失败（它们是控制面/调查包的死代码或门禁删除，表现为静默放行而非断言失败），散落在 R99 长尾里。**MED-1 对应 R11（5 实体）。**

## 3. 根因组明细

以下每组给出：成员规模、代表测试、稳定错误片段、生产代码落点（行号已逐条核对）、判定与依据。

### R01 — PreToolUse 丢失 `quality_gate` 投影

- **成员规模**：32 实体（顶层 8 / 子测试 24）
- **代表测试**：`TestAC001_PreToolUseAutoAdvancesOnSatisfiedGate`、`TestAC002_NotReadyQualityGateDoesNotBlockTool`、`TestAC005_MilestonePersistsQualityGate`、`TestCT03901_DesignGateSatisfiedCommitsPTR_PLAN_01`、`TestCT03902_ContractsMissingCoverageExposesNotReady`
- **稳定错误片段**：`hook output did not carry a quality_gate envelope` / `hook output did not surface a QUALITY_GATE projection`
- **修复落点**：`internal/cli/run.go:2961` — `if request.Event == "PreToolUse" && !graphObserved {` ；`envelopeWithQualityGateMap` 定义在 `internal/cli/run.go:3715-3731`
- **判定**：**既有失败（父提交同样失败）**
- **判定依据**：**父提交重跑对照**。在 `/tmp/ci-parent`（9bf73ca）执行 `go test ./internal/cli/ -run '^TestAC001_PreToolUseAutoAdvancesOnSatisfiedGate$'`，结果 `--- FAIL: TestAC001_PreToolUseAutoAdvancesOnSatisfiedGate (0.08s) / ac_test.go:279: hook output did not carry a quality_gate envelope` —— **错误消息与 HEAD 完全一致**。同批 `TestCT03901_DesignGateSatisfiedCommitsPTR_PLAN_01`、`TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible` 在父提交亦失败。
- **改测试 or 改产品？**：**先改测试**（这组是父提交就存在的失败，与本次重构无关；但需单独确认 `!graphObserved` 是否为期望设计 —— 若期望 quality_gate 始终出现在 PreToolUse 输出，则应改产品 `run.go:2961` 去掉 `!graphObserved` 短路）。**需要二次确认**：缺少设计意图文档明确 quality_gate 与 graph 观察路径的优先级。
- **新发现**：是 —— 不在已知 9+3 条之列。

### R02 — 4 个 CLI 子命令被删除

- **成员规模**：7 实体（顶层 5 / 子测试 2）
- **代表测试**：`TestAcceptanceExternalS7LastResultAndGuidance`、`TestNativeStopPendingS7ReceivesSameOriginalAnswer`、`TestPacketBodyCheckpointRecoveryRestoresMissingLiveBodiesThroughCLI`、`TestPacketBodyMaintenanceCLICompactsLegacyAndKeepsPrivateAnswers`、`TestReadOnlyCommandsValidateExplicitRuntimeLayout`
- **稳定错误片段**：`unknown runtime command "review-plan"` / `unknown command "verification"`
- **修复落点**：`internal/cli/run.go:1855`（`unknown runtime command %q` 报错点）；被删的 4 个 `case` 在父提交位于 `internal/cli/run.go` 的 runtime switch：`case "verification"`、`case "review-plan"`、`case "review-result"`、`case "fingerprint"`（HEAD 该 switch 的 `case` 总数由父提交 34 降到 26）
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交源码对照**（`git show 9bf73ca:internal/cli/run.go`）+ **父提交重跑对照**。这 4 个 `case` 在父提交确实存在（diff 中为 `-` 删除行），说明命令删除由本次重构造成；且父提交重跑 `TestNativeStopPendingS7ReceivesSameOriginalAnswer` → `--- PASS (1.99s)`、`TestReadOnlyCommandsValidateExplicitRuntimeLayout` → `--- PASS (0.07s)`，**父提交通过、HEAD 失败 ⇒ 回归**。
- **改测试 or 改产品？**：**先改测试**。蓝图未要求保留 `runtime review-plan` 等子命令入口（`L3-S9-repair.md` 走自然事件消费，不走这些 CLI 子命令），故断言旧子命令存在的测试属于**测试陈旧**；但需确认 `review-plan`/`review-result` 是否有真实用户在用的入口。
- **新发现**：是。

### R03 — 原生 Result 无法解回原 Plan 包（H9）

- **成员规模**：11 实体（顶层 7 / 子测试 4）；错误串精确命中 10 个失败实体
- **代表测试**：`TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals`、`TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery`、`TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan`、`TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat`、`TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat`
- **稳定错误片段**：`native Result path has no exact current original Plan packet`
- **修复落点**：报错点 `internal/cli/native_s9_recovery.go:115`；新增路由层 `internal/cli/native_s9_routing.go:15`（`nativeS9Routes`）、`internal/runtime/repair_plan_report_routing.go:23`
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交重跑对照**。在 `/tmp/ci-parent` 重跑 `TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals` → `--- PASS (4.47s)`；`TestS9EntryAssemblyReadsOriginalRepairPlanOnce` → `--- PASS (3.20s)`；`TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection` → `--- PASS (5.18s)`。父提交**不存在** `internal/cli/native_s9_routing.go`（`git show 9bf73ca:internal/cli/native_s9_routing.go` 报 ABSENT），是本次重构新增的路由层。
- **改测试 or 改产品？**：**改产品代码**。蓝图 `blueprint/l3-stages/L3-S9-repair.md:236`（§6.2）要求「自然事件从已有当前任务和原责任坐标准确选责，只读取该作者该责任的原答卷」，`:70` 要求「Result 不因 Plan 的非关键缺项拒收」—— 路由层必须能把原生 Result 精确解回其原 Plan 包。测试断言是蓝图行为，**测试正确**。
- **映射**：**H9**。注意：任务简报估「约 22 个 S9 CLI 测试」，本次实测精确命中该错误串的为 **10 个失败实体（5 顶层 + 5 子测试）**，实际影响面小于估算。

### R04 — Main Inspect 因缺 Worktree/Branch 拒绝

- **成员规模**：16 实体（顶层 12 / 子测试 4）
- **代表测试**：`TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput`、`TestNativeS9UnrecordedMergePreservesUnknownAndWorktree`、`TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection`、`TestS9HandoffCanceledFinalAcknowledgementPreservesOriginalProof`、`TestS9HandoffDamagedTypedStopsBeforeMerge`
- **稳定错误片段**：`original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] ...}`
- **修复落点**：`internal/integration/inspect_responsibility.go:20-22` — `if strings.TrimSpace(req.Assignment.WorktreePath) == "" || strings.TrimSpace(req.Assignment.Branch) == "" { return Inspection{}, errors.New("assignment worktree and branch are required") }`
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交重跑对照**。`TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection` 在 `/tmp/ci-parent` → `--- PASS (5.18s)`，在 HEAD 失败。`internal/integration/inspect.go` 在本次重构中大改（−326 行）并拆出 `inspect_responsibility.go`（+188 行）/ `integrate_responsibility.go`（+386 行），新的责任校验路径对 Assignment 的 Worktree/Branch 要求与父提交的装载路径不一致。
- **改测试 or 改产品？**：**改产品代码**。蓝图 `L3-S9-repair.md:71` 要求「Main 取得本人已收贡献、已知工作位置与目标」；装载路径丢失 Worktree/Branch 属产品侧信息传递断裂，**测试断言合理**。
- **新发现**：是。

### R05 — Stop 未触达原 source gap 诊断（H1）

- **成员规模**：15 实体（顶层 11 / 子测试 4）
- **代表测试**：`TestHandoffReviewMainProgressMustNotRepeatSamePreparation`、`TestS9EntryAssemblyPreservesNewPlanFailureAndOriginalStopFailure`、`TestS9EntryAssemblyPreservesParallelResponsibilitiesAndWorker`
- **稳定错误片段**：`original Stop did not reach source gap`
- **修复落点**：`internal/cli/native_review_hooks.go:553-569`（`nativeReviewTaskText` 只读 `in.AgentTranscriptPath`，不读 `last_assistant_message`）；诊断消费方 `internal/cli/s9_handoff_wiring_test.go:57` 期望 diagnostic 含 `S9 local evidence source gap`
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交重跑对照**。`TestHandoffReviewMainProgressMustNotRepeatSamePreparation` 在 `/tmp/ci-parent` → `--- PASS (5.05s)`；`TestS9NativeDivisionKeepsDependencyAndRejectsExtraWorkspacePreparation` → `--- PASS (2.54s)`。
- **改测试 or 改产品？**：**改产品代码**。蓝图 `L3-S9-repair.md:238`：「文件不存在、草稿未就绪或字段缺失时保留原稿，**反馈实际文件与字段**」「合法 blocked 或其他非终结表达可以交接」。Stop 收不到兜底答案 ⇒ 无法反馈真实缺口，**测试断言是蓝图行为**。
- **映射**：**H1**。

### R06 — 绑定派发 owner 改写了其他 TASK 事实

- **成员规模**：11 实体（顶层 11 / 子测试 0）
- **代表测试**：`TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration`、`TestActualMainIntegrationBarrierCleanupAfterEarlyReturn`、`TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration`
- **稳定错误片段**：`binding a dispatched owner changed other TASK facts`（断言点 `internal/cli/workspace_core_profile_test.go:48`）
- **修复落点**：`internal/assignment/register.go:472-475` —
  ```go
  row["owner_agent_ids"] = owners
  if err := workstate.RecordTaskActivity(state, row, "in_progress", mutationEvent); err != nil {
      return fmt.Errorf("record registered TASK work state: %w", err)
  }
  ```
  该 `RecordTaskActivity` 调用为本次重构**新增**（`git diff 9bf73ca d0d948a -- internal/assignment/register.go` 中为 `+` 行，父提交对应分支只写 `row["owner_agent_ids"] = owners`）
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交源码对照**。父提交 `9bf73ca:internal/assignment/register.go` 对应分支（`:409` 附近）只赋值 `owner_agent_ids`，**不调用** `RecordTaskActivity`；新增调用会把 TASK 行的 `state` / `last_lifecycle_event` 一并改写，违反「keep all existing TASK facts」注释承诺。
- **重要区分**：代表测试 `TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration` 在父提交**也失败**，但失败原因**完全不同** —— 父提交报 `integration_observation_e2e_test.go:75: original integration did not complete: ... canonical S6 product baseline requires the original Runtime root and Assignment registry`（S6 基线/Report 问题），HEAD 报 `binding a dispatched owner changed other TASK facts`。即该测试是**失败原因被替换**，不是原失败延续。
- **改测试 or 改产品？**：**改产品代码**。`register.go:454-455` 的注释明确承诺「keep all existing TASK facts and prior owners」，新增调用直接违背该不变量；蓝图 `L4-agent-dispatch-governance.md`（见 §4 引用）亦要求派发只接线责任事实而不改写既有事实。**测试断言合理**。
- **新发现**：是。

### R07 — 候选 schema 拒绝历史 hash 字段

- **成员规模**：7 实体（顶层 2 / 子测试 5）
- **代表测试**：`TestNativeS9OriginalCandidateReadsHistoricalMachineValues`、`TestS9ObserverLeadReplayUsesImmutableProfessionalProjection`
- **稳定错误片段**：`jsonschema validation failed with '.../repair-workspace-candidate.schema.json#' - at '': additional properties 'contract_sha256', 'base_commit', 'source_head' not allowed`
- **修复落点**：`internal/schema/assets/repair-workspace-candidate.schema.json:4`（`"additionalProperties": false`）与 `:16-53`（properties 已无 `contract_sha256` / `base_commit` / `source_head`）；校验入口 `internal/runtime/repair_result_checkpoint.go:70`
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交源码对照**。父提交同名 schema 的 properties 为 `['assignment_id','base_commit','baseline_generation','contract_sha256','execution_generation','plan_id','proposal','provenance','record_type','runtime_id','session_id','source_head','version']`；HEAD 删除了 3 个 hash 字段但保留 `additionalProperties: false` ⇒ 读取历史记录时被拒。
- **改测试 or 改产品？**：**改产品代码**。蓝图 `blueprint/l3-stages/L3-S9-repair.md:15` 明确：「本阶段和其自然事件调用的原消费者、Writer、状态、恢复及后继**不计算、登记或校验任何散列**……**旧记录中已有散列值只作为历史原字段读取**，不重新计算、登记或作为继续资格」。schema 必须**容忍并忽略**历史散列字段，而非因 `additionalProperties: false` 拒绝整份记录。**测试断言是蓝图行为**。
- **新发现**：是（不在 9+3 之列，但属蓝图明确要求 → 是真缺陷而非陈旧测试）。

### R08 — 同事件重试产生重复审计行

- **成员规模**：6 实体（顶层 1 / 子测试 5）
- **代表测试**：`TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity`（子测试 `/failed_command_credential`、`/second_credential_same_visible_category`、`/credential_crosses_audit_bound`、`/config_source`、`/config_path`）
- **稳定错误片段**：`same-event retry duplicated audit: rows=%d unique=%d`（断言点 `internal/cli/native_observer_test.go:207`）
- **修复落点**：审计写入点 `internal/cli/native_review_hooks.go:130`、`internal/cli/native_s6.go:355`、`internal/cli/native_s9.go:285`、`internal/cli/run.go:2971`、`internal/cli/run.go:3185`；去重函数 `internal/cli/hook_audit.go:13` `appendHookAudit`
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交重跑对照**。在 `/tmp/ci-parent` 重跑 `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity` → `--- PASS (0.20s)`（5 个子测试全过）。
- **改测试 or 改产品？**：**改产品代码**。蓝图 `L3-S9-repair.md:74`：「同一交付重复观察**复用已收事实**，仅真实新表达保存新事实」；`L3-S9-repair.md:489`「依据已存完成事实，只补未完成后继」。重复审计行 = 重复保存同一事实，违反幂等要求。**测试断言合理**。
- **新发现**：是。

### R09 — `WorkerChecks` 空列表变 nil → JSON `null`（H7）

- **成员规模**：4 实体（顶层 3 / 子测试 1）
- **代表测试**：`TestNativeS9MainCompletedReturnContinuesExactLiveDSource`、`TestNativeS9RelativeMainCompletedKeepsOriginalLiveSourceContract`、`TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing`
- **稳定错误片段**：`WorkerChecks:[]`（期望 `[]`，实得 `null`）
- **修复落点**：`internal/cli/assignment_context.go:339` — `copyOf.WorkerChecks = append([]assignmentrecord.WorkerCheck(nil), in.ClosingContract.WorkerChecks...)`（空输入 ⇒ nil slice ⇒ 序列化为 `null`）
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交源码对照**。`git diff 9bf73ca d0d948a -- internal/cli/assignment_context.go` 显示该行为本次重构引入/改动；空列表 → `null` 会违反 schema 对 array 类型的约束。
- **改测试 or 改产品？**：**改产品代码**。schema 要求 `array` 而非 `null`；`append([]T(nil), ...)` 对空输入返回 nil，正确写法应为 `make([]T, 0, len(...))` 预分配。**测试断言合理**。
- **映射**：**H7**。

### R10 — review-plan 路径丢失导致 open 空路径

- **成员规模**：14 实体（顶层 7 / 子测试 7）
- **代表测试**：`TestMainReviewPlanPreLargeReplayPreservesAcceptedPlan`、`TestMainReviewPlanPrePreservesReceiptWhenAcceptedContentMissing`、`TestMainReviewPlanPreReceivesChangedOriginal`
- **稳定错误片段**：`open : no such file or directory`（空路径 ⇒ `open ""`）
- **修复落点**：`internal/cli/s7_status.go:425` — `if strings.HasPrefix(requested, materials+string(filepath.Separator)) && filepath.Base(out) == "review-plan.json" && filepath.Clean(out) != filepath.Clean(reviewPlanQuestionnairePath(root, snapshot.State))`
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交源码对照**（`s7_status.go` 的 review-plan 路径解析在本次重构中改动）+ **父提交重跑对照**（`TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite`、`TestS7AcceptedRemainingProjectionUsesOriginalPrerequisites` 在父提交 PASS）。`TestMainReviewPlanPrePreservesReceiptWhenAcceptedContentMissing` 是 HEAD **新增**测试（父提交无此函数），属新测试直接暴露产品缺陷。
- **改测试 or 改产品？**：**改产品代码**。空路径 `open ""` 是明确的路径解析缺陷，不是合理的「文件不存在」反馈。
- **新发现**：是。

### R11 — 原 S9 questionnaire 路径未保留（MED-1）

- **成员规模**：5 实体（顶层 5 / 子测试 0）
- **代表测试**：`TestNativeS9LateTargetedPublicHookPreservesCurrentS7`、`TestS9RepairLeadExistingImpactReminderSkipsPreparation`、`TestS9RepairLeadStopAndIdleDoNotGuessPathWithoutAuthority`
- **稳定错误片段**：`the original S9 questionnaire path was not retained; Main/system must locate the actual existing responsibility`
- **修复落点**：报错点 `internal/cli/repair_lead_questionnaire.go:548`；短路链 `internal/cli/s9_impact_observer.go:20`（`observeMainS9Impact`，grep 确认 **0 引用**）
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交源码对照**（`internal/cli/s9_impact_observer.go` 为 HEAD 新增文件，父提交不存在）+ **父提交重跑对照**（`TestS9NativeDivisionKeepsDependencyAndRejectsExtraWorkspacePreparation` 在父提交 PASS）。
- **改测试 or 改产品？**：**改产品代码**。蓝图 `L3-S9-repair.md:71` 要求「Main 按实际工程进度自然更新原 Impact」，`:74`「同一交付重复观察复用已收事实」；`observeMainS9Impact` 0 引用却短路整条链，导致原路径不被保留。**测试断言合理**。
- **映射**：**MED-1**。

### R12 — Assignment 缺当前 Record 投影

- **成员规模**：3 实体（顶层 3 / 子测试 0）
- **代表测试**：`TestHandleSubagentStopAdvancesIntegrationOnReadyInspection`、`TestHandleSubagentStopPreservesWorktreeOnNotReadyInspection`、`TestPostMergeModeChecksCandidateThroughController`
- **稳定错误片段**：`Assignment %s has no current Record projection`
- **修复落点**：`internal/cli/integration_ack.go:127` — `return "", fmt.Errorf("Assignment %s has no current Record projection", a.AssignmentID)`（`checkpointPathForLoadedAssignment`）
- **判定**：**本次重构引入的回归**
- **判定依据**：**父提交源码对照**（`internal/cli/integration_ack.go` 属本次重构新增文件；父提交无同名文件）与 HEAD 单独失败事实。**需要二次确认**：未单独重跑这 3 个测试的父提交对照（受 15 次重跑预算约束）。
- **改测试 or 改产品？**：**改产品代码**（倾向）。蓝图要求「实际责任与原 Session/Assignment 关联」应被保留；`checkpointPathForLoadedAssignment` 拿不到 Record 投影属装载链断裂。**需要二次确认**：需读父提交 `integration_ack.go` 等价实现确认语义是否曾为「无 Record 投影时按 revision slot 回退」。

### R99 — 长尾：一次性失败（172 顶层 / 82 子测试）

- **成员规模**：254 实体（顶层 172 / 子测试 82）
- **代表测试**：`TestAC006_SubagentStopEmitsIntegrationGuidance`、`TestAC008_CompactRecoversMilestoneFromSnapshot`、`TestAcceptedS4FormalTaskFeedsS5WorkspacePrepare`
- **稳定错误片段**：**无统一稳定片段**。断言点高度分散，每个站点仅 1–5 例；最高频站点：`workspace_cleanup_test.go:46`(5)、`recovery_checkpoint_e2e_test.go:288`(4)、`native_s9_artifact_feedback_test.go:60`(4)、`native_s9_reception_split_test.go:80`(4)、`run_test.go:490`(4)、`review_plan_guidance_identity_test.go:84`(4)、`work_progress_s7_actor_test.go:45`(4)
- **修复落点**：**分散**。本组需按测试文件逐一归因，不能作为单一根因组处理。
- **判定**：**混合**。多数为回归（本次重构大改 `internal/integration`、`internal/cli` 多个文件），但其中已识别出与 H2/H3/H4/H5/H6/MED-2/MED-3 相关的散点。
- **改测试 or 改产品？**：**需要二次确认**。本组是后续「重新删减和适配全部测试」的主战场：建议先按测试文件分片，再逐片判定。**缺少的证据**：对每个断言点做父提交对照需要约 100+ 次重跑，超出本次 15 次预算。


## 4. 「测试陈旧 vs 真缺陷」判定汇总

| 组 | 改测试 / 改产品 | 依据（蓝图行号 / 父提交对照） |
|---|---|---|
| R01 | **先改测试**（或改产品 `run.go:2961`，取决于设计意图 —— 需二次确认） | 父提交同错失败 → 既有失败；蓝图未涉及 quality_gate 与 graph 的优先级 |
| R02 | **改测试** | 蓝图 `L3-S9-repair.md` 走自然事件消费，未要求保留 `runtime review-plan` 等子命令入口；断言旧入口属陈旧 |
| R03 | **改产品** | `L3-S9-repair.md:236` §6.2 自然事件精确选责；`:70` Result 不因 Plan 非关键缺项拒收 |
| R04 | **改产品** | `L3-S9-repair.md:71` Main 取得已知工作位置与目标 |
| R05 | **改产品** | `L3-S9-repair.md:238` 缺字段时保留原稿并反馈实际文件与字段 |
| R06 | **改产品** | `register.go:454-455` 自有注释承诺 keep all existing TASK facts；`L4-agent-dispatch-governance.md` |
| R07 | **改产品** | `L3-S9-repair.md:15` 旧记录散列值**只作为历史原字段读取**，不得因 schema 拒绝整份记录 |
| R08 | **改产品** | `L3-S9-repair.md:74` 同一交付重复观察复用已收事实；`:489` 依据已存完成事实只补未完成后继 |
| R09 | **改产品** | schema 要求 array，`null` 不合法 |
| R10 | **改产品** | 空路径 `open ""` 是路径解析缺陷，非合法反馈 |
| R11 | **改产品** | `L3-S9-repair.md:71` Main 自然更新原 Impact；`:74` 复用已收事实 |
| R12 | **改产品**（需二次确认） | 装载链断裂；需读父提交等价实现确认回退语义 |
| R99 | **需二次确认** | 需按测试文件分片逐片做父提交对照（超出本次 15 次预算） |

**蓝图依据行号汇总（均已逐条核对）**
- `blueprint/l3-stages/L3-S7-verification-round.md:147` §5.2 两个执行波次；`:149`「交付与 QA 属于 `static` 波次，行为执行属于 `behavior` 波次。静态工作先取得完整处置，再进入行为执行」→ **H6（波次门删除）违背此要求，属真缺陷，不是蓝图有意改掉**
- `blueprint/l3-stages/L3-S9-repair.md:15`「本阶段…不计算、登记或校验任何散列，包括 hash／SHA、散列形式的 `digest`／`fingerprint` 和 Git 对象 ID…旧记录中已有散列值只作为历史原字段读取」→ **R07 判为真缺陷**
- `blueprint/l3-stages/L3-S9-repair.md:70`「不对题面、来源、答案、packet 或证据计算／登记散列」；`:71` Main 原 Impact 更新；`:74` 复用已收事实；`:236` §6.2 自然事件精确选责；`:238` 缺字段保留原稿并反馈；`:489` 只补未完成后继

## 5. 未启动的 176 个顶层测试

### 5.1 清单

来源：`/tmp/list-cli.txt`（1197 个顶层测试名）减去 cli.json 中已启动的 1021 个 ⇒ **176 个未启动**。
完整清单已生成于 `/tmp/pi-notstarted.txt`（176 行）。抽样（前 60 条见该文件）：

```
TestS5MainObservationIsReadOnlyAndRedeliversChangedResponsibility
TestS6CleanupPendingRecoversBeforeFirstRuntimeProjection
TestS6PrepareUsesCurrentTaskWithoutHistoricalDigestGate
TestS7BudgetStatusDoesNotPrepareDecisionOrGateNormalAcceptance
TestS10RecoveryRejectsDirectoryAndObservationRemainsReadOnly
...
```

### 5.2 是否落在已知失败组里？

**结论：不落在。** 逐项检查（对 176 个未启动测试的源文件做内容检索）：

| 检查项 | 命中数 | 方法 |
|---|---|---|
| 引用被删的 runtime 子命令（`review-plan`/`review-result`/`verification`/`fingerprint`） | **0** | 对 176 个测试源文件 grep 子命令字面量 |
| 断言 `quality_gate` / `QualityGate` | **0** | grep 关键词（R01 组特征） |
| 断言 `StaticClaimsSettled` / static-behavior 波次（H6 组特征） | **0** | grep 关键词 |
| 引用 `native Result path ... Plan packet`（R03/H9 组特征） | **0** | grep 关键词 |

即：176 个未启动测试与 R01–R12 的特征**均无交集**。

### 5.3 抽样重跑验证（10 个上限内，实际抽 5 个）

在 `/tmp/fullcli`（HEAD）单独重跑未启动测试：

```
--- PASS: TestS5MainObservationIsReadOnlyAndRedeliversChangedResponsibility (1.53s)
--- PASS: TestS6CleanupPendingRecoversBeforeFirstRuntimeProjection (4.96s)
--- PASS: TestS6PrepareUsesCurrentTaskWithoutHistoricalDigestGate (1.30s)
--- PASS: TestS7BudgetStatusDoesNotPrepareDecisionOrGateNormalAcceptance (0.02s)
--- PASS: TestS10RecoveryRejectsDirectoryAndObservationRemainsReadOnly (0.50s)
```

5/5 通过。**判定**：抽样证据表明这 176 个测试大概率通过；未启动的原因是整包 30m 超时（`make test` 用 `-timeout=30m`），而非它们本身失败。

### 5.4 需要二次确认

抽样仅 5 个（占 176 的 2.8%），**不能断言全部 176 个都会通过**。R99 长尾组有 172 个顶层失败、长尾特征分散，剩余 171 个未启动测试中理论上仍可能有个别落在长尾里。建议后续在整包超时放宽（如 `-timeout=90m`）或按文件分片重跑后复核。

## 6. 完整失败清单（385 实体：247 顶层 + 138 子测试）

「所属源文件」列为测试函数所在文件（`internal/cli/<file>_test.go`），已用 `func Test…(` 正则在 `/tmp/fullcli/internal/cli/*_test.go` 中逐个定位；「失败层级」中 TOP = 顶层测试失败，SUB = 唯一子测试失败（父测试必然同时计为 TOP，故本表 385 行中含父子双重计入）。


| 失败层级 | 测试全名 | 所属源文件 | 关键错误消息（截断至 200 字符） |
|---|---|---|---|
| TOP | `TestAC001_PreToolUseAutoAdvancesOnSatisfiedGate` | `internal/cli/ac_test.go` | hook output did not carry a quality_gate envelope |
| TOP | `TestAC002_NotReadyQualityGateDoesNotBlockTool` | `internal/cli/ac_test.go` | hook output did not carry a quality_gate envelope |
| TOP | `TestAC005_MilestonePersistsQualityGate` | `internal/cli/ac_test.go` | AC-005: Milestone MUST project quality_gate (BUG-039-07 / REQ-039 §11): { |
| TOP | `TestCT03901_DesignGateSatisfiedCommitsPTR_PLAN_01` | `internal/cli/ct_test.go` | CT-039-01 must surface a quality_gate envelope |
| TOP | `TestCT03902_ContractsMissingCoverageExposesNotReady` | `internal/cli/ct_test.go` | CT-039-02 must surface a quality_gate envelope |
| TOP | `TestCT03905_UnlockedSiblingWriteIsObserved` | `internal/cli/ct_test.go` | CT-039-05 must surface a quality_gate envelope |
| TOP | `TestMainReviewPlanIdentityNaturalReceptionAndReplay` | `internal/cli/review_plan_identity_listener_e2e_test.go` | --- FAIL: TestMainReviewPlanIdentityNaturalReceptionAndReplay (0.43s) |
| SUB | `TestMainReviewPlanIdentityNaturalReceptionAndReplay/legacy` | `internal/cli/review_plan_identity_listener_e2e_test.go` | missing original planning duty: {"hookSpecificOutput":{"additionalContext":"QUALITY_GATE {\"status\":\"unknown\",\"gate_id\":\"\",\"candidate_transition\":\"\",\"observed_revision\":1,\"missing\":[\"R |
| SUB | `TestMainReviewPlanPreReceivesReadyOriginal/PreToolUse/plan/1` | `internal/cli/review_plan_pre_listener_e2e_test.go` | registered plan did not provide its actual preparation/dependency step "assignment-e2e-1 waits": {"hookSpecificOutput":{"additionalContext":"QUALITY_GATE {\"status\":\"unknown\",\"gate_id\":\"\",\"can |
| TOP | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible` | `internal/cli/policy_unavailable_test.go` | --- FAIL: TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible (0.04s) |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/Agent` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv1378399364/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/Bash` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv2771550541/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/Edit` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv3122047868/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/Glob` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv3406581883/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/Grep` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv1775724257/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/PowerShell` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv2382476694/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/Read` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv3173990043/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/TaskUpdate` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv3910792120/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/Write` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv3214464884/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/mcp__custom__read` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv2737003739/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/invalid/unknown-tool` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisibleinv1809698268/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/Agent` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis219807604/00 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/Bash` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis302897745/00 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/Edit` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis406874524/00 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/Glob` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis1407234435/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/Grep` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis4045901398/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/PowerShell` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis2422432612/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/Read` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis2805349516/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/TaskUpdate` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis3756886430/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/Write` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis2680833480/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/mcp__custom__read` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis1601178446/0 |
| SUB | `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible/missing/unknown-tool` | `internal/cli/policy_unavailable_test.go` | Hook output does not contain a QUALITY_GATE projection: {"hookSpecificOutput":{"additionalContext":"Runtime snapshot at /tmp/TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisiblemis3296732797/0 |
| TOP | `TestAcceptanceExternalS7LastResultAndGuidance` | `internal/cli/hook_acceptance_external_test.go` | --- FAIL: TestAcceptanceExternalS7LastResultAndGuidance (16.39s) |
| SUB | `TestAcceptanceExternalS7LastResultAndGuidance/1.2.0` | `internal/cli/hook_acceptance_external_test.go` | CLI [runtime review-plan --root /tmp/TestAcceptanceExternalS7LastResultAndGuidance1.2.0638016209/001 --file /tmp/TestAcceptanceExternalS7LastResultAndGuidance1.2.0638016209/001/review-plan.json]: code |
| SUB | `TestAcceptanceExternalS7LastResultAndGuidance/1.3.0` | `internal/cli/hook_acceptance_external_test.go` | CLI [runtime review-plan --root /tmp/TestAcceptanceExternalS7LastResultAndGuidance1.3.03782014421/001 --file /tmp/TestAcceptanceExternalS7LastResultAndGuidance1.3.03782014421/001/review-plan.json]: co |
| TOP | `TestNativeStopPendingS7ReceivesSameOriginalAnswer` | `internal/cli/native_stop_pending_external_test.go` | CLI [runtime review-plan --root /tmp/TestNativeStopPendingS7ReceivesSameOriginalAnswer2198409548/001 --file /tmp/TestNativeStopPendingS7ReceivesSameOriginalAnswer2198409548/001/review-plan.json]: code |
| TOP | `TestPacketBodyCheckpointRecoveryRestoresMissingLiveBodiesThroughCLI` | `internal/cli/packet_body_recovery_e2e_test.go` | CLI [runtime review-plan --root /tmp/TestPacketBodyCheckpointRecoveryRestoresMissingLiveBodiesThrough1270172580/001 --file /tmp/TestPacketBodyCheckpointRecoveryRestoresMissingLiveBodiesThrough12701725 |
| TOP | `TestPacketBodyMaintenanceCLICompactsLegacyAndKeepsPrivateAnswers` | `internal/cli/packet_body_recovery_e2e_test.go` | CLI [runtime review-plan --root /tmp/TestPacketBodyMaintenanceCLICompactsLegacyAndKeepsPrivateAnswers1410355056/001 --file /tmp/TestPacketBodyMaintenanceCLICompactsLegacyAndKeepsPrivateAnswers14103550 |
| TOP | `TestReadOnlyCommandsValidateExplicitRuntimeLayout` | `internal/cli/req_bind_ux_test.go` | current state unreadable by [verification clean-round --root /tmp/TestReadOnlyCommandsValidateExplicitRuntimeLayout595924757/001 --state external-state.json]: 2  unknown command "verification" |
| TOP | `TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals` | `internal/cli/native_s9_recovery_test.go` | native Result path has no exact current original Plan packet |
| TOP | `TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery` | `internal/cli/native_s9_recovery_test.go` | native Result path has no exact current original Plan packet |
| TOP | `TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan` | `internal/cli/native_s9_recovery_test.go` | native Result path has no exact current original Plan packet |
| TOP | `TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat` | `internal/cli/native_s9_recovery_test.go` | missing unadapted draft prevented original explicit flat adaptation: 1  native Result path has no exact current original Plan packet |
| TOP | `TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat` | `internal/cli/native_s9_recovery_test.go` | --- FAIL: TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat (6.49s) |
| SUB | `TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat/after_state_before_journal` | `internal/cli/native_s9_recovery_test.go` | public pending recovery: 1  native Result path has no exact current original Plan packet |
| SUB | `TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat/before_state_publication` | `internal/cli/native_s9_recovery_test.go` | public pending recovery: 1  native Result path has no exact current original Plan packet |
| TOP | `TestNativeS9OriginalFlatConsumeReceivesAndProjectsReplay` | `internal/cli/native_s9_recovery_test.go` | public original-input recovery: 1  native Result path has no exact current original Plan packet |
| TOP | `TestNativeS9PendingRecoveryReportsChangedPrivateOriginal` | `internal/cli/native_s9_recovery_test.go` | --- FAIL: TestNativeS9PendingRecoveryReportsChangedPrivateOriginal (6.24s) |
| SUB | `TestNativeS9PendingRecoveryReportsChangedPrivateOriginal/after_state_before_journal` | `internal/cli/native_s9_recovery_test.go` | private source drift was not reported: 1  native Result path has no exact current original Plan packet |
| SUB | `TestNativeS9PendingRecoveryReportsChangedPrivateOriginal/before_state_publication` | `internal/cli/native_s9_recovery_test.go` | private source drift was not reported: 1  native Result path has no exact current original Plan packet |
| TOP | `TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput` | `internal/cli/native_execution_return_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestNativeS9UnrecordedMergePreservesUnknownAndWorktree` | `internal/cli/repair_ready_merge_recovery_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffCanceledFinalAcknowledgementPreservesOriginalProof` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffDamagedTypedCandidateStopsBeforeMerge` | `internal/cli/s9_handoff_wiring_test.go` | --- FAIL: TestS9HandoffDamagedTypedCandidateStopsBeforeMerge (12.11s) |
| SUB | `TestS9HandoffDamagedTypedCandidateStopsBeforeMerge/invalid_JSON` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| SUB | `TestS9HandoffDamagedTypedCandidateStopsBeforeMerge/missing_provenance` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| SUB | `TestS9HandoffDamagedTypedCandidateStopsBeforeMerge/missing_source` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffFinalProjectionRetainsOriginalCleanupResponsibility` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffFinalReceiptSurvivesMissingPlanIndex` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts` | `internal/cli/s9_handoff_wiring_test.go` | --- FAIL: TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts (29.15s) |
| SUB | `TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts/accepted_Final` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffSameOwnerNeighborCannotSupplyOrBreakOriginalCandidate` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffSpentProgressPreservesIndexesWithoutNewDocumentReads` | `internal/cli/s9_handoff_cancellation_linux_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffTypedCandidateCannotLoseProvenanceOrSource` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestS9HandoffTypedSelectorPreservesChangedAttemptFence` | `internal/cli/s9_handoff_wiring_test.go` | original Main Inspect: {WorktreeObservation:[] Ready:false Blockers:[] AssignmentID: TaskID: WorktreePath: SourceBranch: TargetBranch: ChangedPaths:[] EffectiveWritePaths:[] AssignmentProjection:{Base |
| TOP | `TestHandoffReviewMainProgressMustNotRepeatSamePreparation` | `internal/cli/s9_entry_assembly_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestHandoffReviewMainProgressMustNotRepeatSamePreparation734708198/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdC |
| TOP | `TestS9EntryAssemblyPreservesNewPlanFailureAndOriginalStopFailure` | `internal/cli/s9_entry_assembly_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9EntryAssemblyPreservesNewPlanFailureAndOriginalStopFailure1131625417/001/.claude/submissions/s9/p/WyJsb29wLXJlcT |
| TOP | `TestS9EntryAssemblyPreservesParallelResponsibilitiesAndWorker` | `internal/cli/s9_entry_assembly_test.go` | --- FAIL: TestS9EntryAssemblyPreservesParallelResponsibilitiesAndWorker (5.97s) |
| SUB | `TestS9EntryAssemblyPreservesParallelResponsibilitiesAndWorker/original_Worker` | `internal/cli/s9_entry_assembly_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9EntryAssemblyPreservesParallelResponsibilitiesAndWorkeror3818508776/001/.claude/submissions/s9/p/WyJsb29wLXJlcTA |
| TOP | `TestS9EntryAssemblyReadsOriginalRepairPlanOnce` | `internal/cli/s9_entry_assembly_linux_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9EntryAssemblyReadsOriginalRepairPlanOnce495653183/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpc |
| TOP | `TestS9HandoffCanceledIntegrationAndProjectionPreserveKnownFacts` | `internal/cli/s9_handoff_wiring_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9HandoffCanceledIntegrationAndProjectionPreserveKnownFacts318964968/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAz |
| TOP | `TestS9HandoffIncompleteBindingNeverOpensCandidate` | `internal/cli/s9_handoff_wiring_test.go` | --- FAIL: TestS9HandoffIncompleteBindingNeverOpensCandidate (3.14s) |
| SUB | `TestS9HandoffIncompleteBindingNeverOpensCandidate/path` | `internal/cli/s9_handoff_wiring_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9HandoffIncompleteBindingNeverOpensCandidatepath616514772/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsIn |
| TOP | `TestS9HandoffLaterIndexFailureDoesNotEraseAcceptedPlan` | `internal/cli/s9_handoff_wiring_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9HandoffLaterIndexFailureDoesNotEraseAcceptedPlan951623609/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsI |
| TOP | `TestS9HandoffMissingCandidatePreservesPlanAcrossAllEntrances` | `internal/cli/s9_handoff_wiring_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9HandoffMissingCandidatePreservesPlanAcrossAllEntrances313348763/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1 |
| SUB | `TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts/no_candidate` | `internal/cli/s9_handoff_wiring_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9HandoffPublicMainProgressUsesOriginalReceptionFactsno_can645472398/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAz |
| TOP | `TestS9HandoffResultDiagnosticUsesUniqueOriginalPlanBinding` | `internal/cli/s9_handoff_wiring_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9HandoffResultDiagnosticUsesUniqueOriginalPlanBinding1657727594/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1j |
| TOP | `TestS9PublicFeedbackRecoveryCommandUsesReturnedArgv` | `internal/cli/native_execution_return_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9PublicFeedbackRecoveryCommandUsesReturnedArgv1594174268/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsInJ |
| TOP | `TestS9ReceptionFeedbackMissingSavedTypedInputNamesOriginalWindowBeforeRetry` | `internal/cli/s9_reception_feedback_contract_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9ReceptionFeedbackMissingSavedTypedInputNamesOriginalWindow2811935236/001/.claude/submissions/s9/p/WyJsb29wLXJlcT |
| SUB | `TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical/system` | `internal/cli/s9_reception_feedback_contract_test.go` | original Stop did not reach source gap: 0 Main original ChangeImpact answer: /tmp/TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistoricalsys2491726463/001/.claude/submissions/s9/p/WyJsb29wLXJlcTA |
| TOP | `TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration` | `internal/cli/integration_observation_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestActualMainIntegrationBarrierCleanupAfterEarlyReturn` | `internal/cli/integration_observation_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration` | `internal/cli/context_conventions_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestCheckProfileActualMainIntegrationCapturesOriginalResponsibility` | `internal/cli/check_profile_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestCheckProfilePrepareAndReplacementKeepExactProfile` | `internal/cli/check_profile_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestConfiguredPressureOriginalReportWriterRegistrationAndActualMainIntegration` | `internal/cli/dispatch_pressure_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestMainCheckCapabilitiesObservationIsReadonlyAndPartial` | `internal/cli/check_capability_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestMainCheckMissingToolRejectsPrepareBeforeReady` | `internal/cli/check_capability_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestMainCheckReuseObservationIsReadonlyAndCannotSkipRequiredChecks` | `internal/cli/check_capability_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestNativeBuilderConventionsFixedReplayAndSourceDrift` | `internal/cli/context_conventions_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestNativeProfileContextExposesFrozenOriginalProfile` | `internal/cli/check_profile_e2e_test.go` | binding a dispatched owner changed other TASK facts |
| TOP | `TestNativeS9OriginalCandidateReadsHistoricalMachineValues` | `internal/cli/native_s9_candidate_historical_projection_test.go` | --- FAIL: TestNativeS9OriginalCandidateReadsHistoricalMachineValues (7.28s) |
| SUB | `TestNativeS9OriginalCandidateReadsHistoricalMachineValues/array` | `internal/cli/native_s9_candidate_historical_projection_test.go` | original candidate receipt: ok=false err=validate data: jsonschema validation failed with 'file:///tmp/fullcli/internal/cli/repair-workspace-candidate.schema.json#' |
| SUB | `TestNativeS9OriginalCandidateReadsHistoricalMachineValues/nonstandard` | `internal/cli/native_s9_candidate_historical_projection_test.go` | original candidate receipt: ok=false err=validate data: jsonschema validation failed with 'file:///tmp/fullcli/internal/cli/repair-workspace-candidate.schema.json#' |
| SUB | `TestNativeS9OriginalCandidateReadsHistoricalMachineValues/null` | `internal/cli/native_s9_candidate_historical_projection_test.go` | original candidate receipt: ok=false err=validate data: jsonschema validation failed with 'file:///tmp/fullcli/internal/cli/repair-workspace-candidate.schema.json#' |
| SUB | `TestNativeS9OriginalCandidateReadsHistoricalMachineValues/number` | `internal/cli/native_s9_candidate_historical_projection_test.go` | original candidate receipt: ok=false err=validate data: jsonschema validation failed with 'file:///tmp/fullcli/internal/cli/repair-workspace-candidate.schema.json#' |
| SUB | `TestNativeS9OriginalCandidateReadsHistoricalMachineValues/object` | `internal/cli/native_s9_candidate_historical_projection_test.go` | original candidate receipt: ok=false err=validate data: jsonschema validation failed with 'file:///tmp/fullcli/internal/cli/repair-workspace-candidate.schema.json#' |
| TOP | `TestS9ObserverLeadReplayUsesImmutableProfessionalProjection` | `internal/cli/s9_questionnaire_observer_test.go` | ordinary event resubmitted a historical Impact: {"hookSpecificOutput":{"additionalContext":"Questionnaire: /tmp/TestS9ObserverLeadReplayUsesImmutableProfessionalProjection4251350570/001/.claude/submis |
| TOP | `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity` | `internal/cli/native_observer_test.go` | --- FAIL: TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity (0.24s) |
| SUB | `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity/config_path` | `internal/cli/native_observer_test.go` | same-event retry duplicated audit: rows=10 unique=5 |
| SUB | `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity/config_source` | `internal/cli/native_observer_test.go` | same-event retry duplicated audit: rows=8 unique=4 |
| SUB | `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity/credential_crosses_audit_bound` | `internal/cli/native_observer_test.go` | same-event retry duplicated audit: rows=6 unique=3 |
| SUB | `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity/failed_command_credential` | `internal/cli/native_observer_test.go` | same-event retry duplicated audit: rows=2 unique=1 |
| SUB | `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity/second_credential_same_visible_category` | `internal/cli/native_observer_test.go` | same-event retry duplicated audit: rows=4 unique=2 |
| SUB | `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields/completed_Result` | `internal/cli/native_s9_artifact_feedback_test.go` | invalid Result created a delivery: &{History:[] Version:1 MainRoot:/tmp/TestNativeS9FlatValidationFeedbackUsesPrivateFileFieldscomplete3203163204/001 CommonDir: Branch:test-development Executions:map[ |
| TOP | `TestNativeS9MainCompletedReturnContinuesExactLiveDSource` | `internal/cli/native_s9_main_continuation_test.go` | did not preserve incomplete D source: workspace.Execution{AssignmentRecord:(*assignmentrecord.RecordReference)(nil), ToolCheckReceipts:[]checkexec.ToolReceipt(nil), WorkerChecks:[]assignmentrecord.Wor |
| TOP | `TestNativeS9RelativeMainCompletedKeepsOriginalLiveSourceContract` | `internal/cli/native_s9_reference_acceptance_test.go` | fixture lost original live D boundary: {AssignmentRecord:<nil> ToolCheckReceipts:[] WorkerChecks:[] OwnedCheckReceipts:[] CheckProfiles:[] Purpose:native_delivery WorkPacketID: StageEntry:<nil> CheckL |
| TOP | `TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing` | `internal/cli/s9_task_integrate_handoff_test.go` | wrong failure window: {AssignmentRecord:<nil> ToolCheckReceipts:[] WorkerChecks:[] OwnedCheckReceipts:[] CheckProfiles:[] Purpose:native_delivery WorkPacketID: StageEntry:<nil> CheckLocation:main Rewo |
| TOP | `TestMainReviewPlanPreLargeReplayPreservesAcceptedPlan` | `internal/cli/review_plan_pre_listener_e2e_test.go` | open /tmp/TestMainReviewPlanPreLargeReplayPreservesAcceptedPlan1983971215/001/.claude/submissions/loop-REQ-WORKTREE/g1/S7/r1/review-plan.json: no such file or directory |
| TOP | `TestMainReviewPlanPrePreservesReceiptWhenAcceptedContentMissing` | `internal/cli/review_plan_pre_listener_e2e_test.go` | open : no such file or directory |
| TOP | `TestMainReviewPlanPreReceivesChangedOriginal` | `internal/cli/review_plan_pre_listener_e2e_test.go` | open : no such file or directory |
| TOP | `TestMainReviewPlanPreWhitespaceExpansionMatchesOriginalConsumer` | `internal/cli/review_plan_pre_listener_e2e_test.go` | open /tmp/TestMainReviewPlanPreWhitespaceExpansionMatchesOriginalConsumer809708050/001/.claude/submissions/loop-REQ-WORKTREE/g1/S7/r1/review-plan.json: no such file or directory |
| TOP | `TestNativeMainPendingContinuesActualReviewPlanSave` | `internal/cli/native_main_pending_external_test.go` | --- FAIL: TestNativeMainPendingContinuesActualReviewPlanSave (0.66s) |
| SUB | `TestNativeMainPendingContinuesActualReviewPlanSave/pre-receives-ready-original` | `internal/cli/native_main_pending_external_test.go` | open : no such file or directory |
| SUB | `TestNativeMainPendingContinuesActualReviewPlanSave/read-post-does-not-save` | `internal/cli/native_main_pending_external_test.go` | open : no such file or directory |
| SUB | `TestNativeMainPendingContinuesActualReviewPlanSave/session-does-not-receive` | `internal/cli/native_main_pending_external_test.go` | open : no such file or directory |
| SUB | `TestNativeMainPendingContinuesActualReviewPlanSave/write-post-receives` | `internal/cli/native_main_pending_external_test.go` | open : no such file or directory |
| TOP | `TestS7InitialDraftNaturalObservationAndExplicitSave` | `internal/cli/review_plan_draft_baseline_e2e_test.go` | --- FAIL: TestS7InitialDraftNaturalObservationAndExplicitSave (0.32s) |
| SUB | `TestS7InitialDraftNaturalObservationAndExplicitSave/changed` | `internal/cli/review_plan_draft_baseline_e2e_test.go` | open : no such file or directory |
| SUB | `TestS7InitialDraftNaturalObservationAndExplicitSave/edit` | `internal/cli/review_plan_draft_baseline_e2e_test.go` | open : no such file or directory |
| SUB | `TestS7InitialDraftNaturalObservationAndExplicitSave/write` | `internal/cli/review_plan_draft_baseline_e2e_test.go` | open : no such file or directory |
| TOP | `TestS7ObserverSkipsMachineDraftAndReceivesProfessionalScriptSave` | `internal/cli/questionnaire_observer_e2e_test.go` | open /tmp/TestS7ObserverSkipsMachineDraftAndReceivesProfessionalScriptSave3094136672/001/.claude/submissions/loop-REQ-WORKTREE/g1/S7/r1/review-plan.json: no such file or directory |
| TOP | `TestNativeS9LateTargetedPublicHookPreservesCurrentS7` | `internal/cli/native_s9_late_targeted_public_test.go` | the original S9 questionnaire path was not retained; Main/system must locate the actual existing responsibility |
| TOP | `TestS9RepairLeadExistingImpactReminderSkipsPreparation` | `internal/cli/repair_lead_answer_test.go` | the original S9 questionnaire path was not retained; Main/system must locate the actual existing responsibility |
| TOP | `TestS9RepairLeadStopAndIdleDoNotGuessPathWithoutAuthority` | `internal/cli/repair_lead_questionnaire_test.go` | Stop guessed a ChangeImpact path without current source authority: S9 ChangeImpact questionnaire path is unavailable from current Runtime: the original S9 questionnaire path was not retained; Main/sys |
| TOP | `TestS9RepairLeadTargetedPathUsesRegisteredIndependentAssignment` | `internal/cli/repair_lead_questionnaire_test.go` | the original S9 questionnaire path was not retained; Main/system must locate the actual existing responsibility |
| TOP | `TestS9RepairLeadTargetedReminderUsesIndexedProjection` | `internal/cli/repair_lead_answer_test.go` | the original S9 questionnaire path was not retained; Main/system must locate the actual existing responsibility |
| TOP | `TestHandleSubagentStopAdvancesIntegrationOnReadyInspection` | `internal/cli/controller_test.go` | HandleSubagentStop: Assignment assignment-039-06 has no current Record projection |
| TOP | `TestHandleSubagentStopPreservesWorktreeOnNotReadyInspection` | `internal/cli/controller_test.go` | HandleSubagentStop: Assignment assignment-039-06 has no current Record projection |
| TOP | `TestPostMergeModeChecksCandidateThroughController` | `internal/cli/controller_test.go` | HandleSubagentStop: Assignment assignment-039-06 has no current Record projection |
| TOP | `TestAC006_SubagentStopEmitsIntegrationGuidance` | `internal/cli/ac_test.go` | AC-006: SubagentStop integration guidance missing "SubagentStop": |
| TOP | `TestAC008_CompactRecoversMilestoneFromSnapshot` | `internal/cli/ac_test.go` | AC-008: post-Compact SessionStart must surface the prior milestone objective "complete the design phase", got {"hookSpecificOutput":{"additionalContext":"LOOP RECOVERY — source=startup; stage=S2 @ rev |
| TOP | `TestAcceptedS4FormalTaskFeedsS5WorkspacePrepare` | `internal/cli/formal_input_selection_e2e_test.go` | drifted accepted S4 output remained eligible in S5: docs/dev/tasks/TASK-039-01.md |
| TOP | `TestActionsCatalogShowsCanonicalReviewRepairLoop` | `internal/cli/action_catalog_test.go` | action catalog missing "runtime workspace pending": |
| TOP | `TestActivatedCheckpointRecoveryRejectsTamperingAndWrongAssignment` | `internal/cli/checkpoint_recovery_test.go` | tampered plan recovered |
| TOP | `TestActivationSentGuidanceKeepsExplicitLegacyInstructions` | `internal/cli/activation_guidance_test.go` | legacy reviewer command path was not preserved: complete the assigned professional review and save the exact ReviewResult questionnaire supplied by the listener; the original consumer records your ans |
| TOP | `TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode` | `internal/cli/activation_guidance_test.go` | --- FAIL: TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode (0.00s) |
| SUB | `TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode/builder` | `internal/cli/activation_guidance_test.go` | packet activation guidance lacks its supplied questionnaire flow: complete the assigned TASK using its supplied sources and checks, then save the exact Builder Result questionnaire supplied by the lis |
| SUB | `TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode/reviewer` | `internal/cli/activation_guidance_test.go` | packet activation guidance lacks its supplied questionnaire flow: complete the assigned professional review and save the exact ReviewResult questionnaire supplied by the listener; the original consume |
| TOP | `TestAuditHooksE2EPlanRefUsesAuthorityRoot` | `internal/cli/audit_hooks_e2e_test.go` | worker plan_ref was not normalized/imported: got=".claude/evidence/plan-handoffs/r/WyJsb29wLVJFUS0wMzkiLCJhc3NpZ25tZW50LXM5LXVuaXQiLCJhZ2VudC1zOSIsIm1zZy1wbGFuLTAwMDEiXQ/plan-0.json" want=".claude/evi |
| TOP | `TestBuildGuidanceS9UsesOriginalQuestionnairesAndLifecycle` | `internal/cli/controller_test.go` | --- FAIL: TestBuildGuidanceS9UsesOriginalQuestionnairesAndLifecycle (0.00s) |
| SUB | `TestBuildGuidanceS9UsesOriginalQuestionnairesAndLifecycle/handoff_has_no_author_form` | `internal/cli/controller_test.go` | Handoff guidance lost "Targeted SubagentStop": Successful reception of the final required Targeted answer through its original consumer attempts the original Handoff automatically. If it remains incom |
| TOP | `TestCT03904_LockedExactContractWriteIsObserved` | `internal/cli/ct_test.go` | CT-039-04 must retain the graph observation: |
| TOP | `TestCT03906_GitSquashMergeIsObserved` | `internal/cli/ct_test.go` | CT-039-06 must retain graph progress feedback: |
| TOP | `TestCT03908_SessionStartAfterCompactSurfacesSameMilestone` | `internal/cli/ct_test.go` | CT-039-08: post-Compact SessionStart must surface the persisted objective "complete the design phase", got {"hookSpecificOutput":{"additionalContext":"LOOP RECOVERY — source=startup; stage=S2 @ rev=1; |
| TOP | `TestCT03909_CleanWorktreeStopEmitsIntegrationGuidance` | `internal/cli/ct_test.go` | CT-039-09: SubagentStop integration guidance missing "SubagentStop": |
| TOP | `TestCT03911_DualPassDocumentVerificationCommitsTR003` | `internal/cli/ct_11_24_test.go` | CT-039-11 hook must surface GATE-DOCUMENT-PASS, stdout= |
| TOP | `TestCT03912ControllerOnly` | `internal/cli/ct_only_test.go` | status=not_ready err="" missing=[builder_responsibility:TASK-039-01 builder_responsibility:TASK-039-02] |
| TOP | `TestCT03912_BuilderCompleteCommitsTR006NoManualCLI` | `internal/cli/ct_11_24_test.go` | CT-039-12 TR-006 must land verification.planned, got "building"/"" |
| TOP | `TestCT03915_S11StopsAutoAdvance` | `internal/cli/ct_11_24_test.go` | CT-039-15 must surface human Gateway guidance, got |
| TOP | `TestCT03920_BuilderCompletionDurableRef` | `internal/cli/ct_11_24_test.go` | CT-039-20 must surface completion/integration guidance, got |
| TOP | `TestCT03924_SameAgentDualDVLabelsBlocksTR003` | `internal/cli/ct_11_24_test.go` | hook output is not JSON: EOF |
| TOP | `TestCheckpointPublishFailureRetainsPendingAndRetriesOnce` | `internal/cli/recovery_checkpoint_e2e_test.go` | checkpoint retained 1 directories; want bounded latest two |
| TOP | `TestCheckpointRejectsOlderPairAndForgedReceptionFacts` | `internal/cli/recovery_checkpoint_e2e_test.go` | --- FAIL: TestCheckpointRejectsOlderPairAndForgedReceptionFacts (2.12s) |
| SUB | `TestCheckpointRejectsOlderPairAndForgedReceptionFacts/duplicate-key` | `internal/cli/recovery_checkpoint_e2e_test.go` | forged indexed facts accepted |
| SUB | `TestCheckpointRejectsOlderPairAndForgedReceptionFacts/fake-transition` | `internal/cli/recovery_checkpoint_e2e_test.go` | forged indexed facts accepted |
| SUB | `TestCheckpointRejectsOlderPairAndForgedReceptionFacts/missing-lineage` | `internal/cli/recovery_checkpoint_e2e_test.go` | forged indexed facts accepted |
| SUB | `TestCheckpointRejectsOlderPairAndForgedReceptionFacts/old-pass` | `internal/cli/recovery_checkpoint_e2e_test.go` | forged indexed facts accepted |
| TOP | `TestContractAuthorityUsesOriginalS2RegisteredDesign` | `internal/cli/design_answer_e2e_test.go` | other root granted S3 authority |
| TOP | `TestDesignAnswerOriginalPairRecovery` | `internal/cli/design_answer_e2e_test.go` | original pair restoration changed S2 reception or cursor |
| TOP | `TestExplainListsEligibleCurrentEvidenceCandidates` | `internal/cli/bug101_test.go` | explain output must contain "review_round: 3", got: |
| TOP | `TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite` | `internal/cli/assignment_context_e2e_test.go` | missing installed source became ready: 0 {"version":1,"generation":1,"context_ref":{"path":".claude/assignment-context/native/bG9vcC1yZXEwMzktY3Q/g1/YXNzaWdubWVudC1zcGVj/cmV2aWV3ZXItc3BlYw/r4/context. |
| TOP | `TestExplicitLegacyStateCannotBeReconciled` | `internal/cli/req_bind_ux_test.go` | --- FAIL: TestExplicitLegacyStateCannotBeReconciled (0.17s) |
| SUB | `TestExplicitLegacyStateCannotBeReconciled/bound_req` | `internal/cli/req_bind_ux_test.go` | old explicit Runtime accepted: 0 policy available path=docs/control/hook-policy.json version=v2.0.0; historical contents are not verified |
| SUB | `TestExplicitLegacyStateCannotBeReconciled/definition` | `internal/cli/req_bind_ux_test.go` | old explicit Runtime accepted: 0 policy available path=docs/control/hook-policy.json version=v2.0.0; historical contents are not verified |
| TOP | `TestFindingSupplementOriginalCLIDoesNotRequireDigest` | `internal/cli/finding_supplement_nohash_test.go` | original CLI registered a new machine hash |
| TOP | `TestHookCommandMissingRuntimeFailsOpenWithGateDiagnostic` | `internal/cli/run_test.go` | missing runtime must surface its graph diagnostic: |
| TOP | `TestHookCommandObservesProductWriteWithoutToolDenial` | `internal/cli/run_test.go` | PreToolUse observer must retain graph progress feedback: |
| TOP | `TestHookCommandPreToolUseDoesNotRequireToolIdentityContext` | `internal/cli/run_test.go` | expected a graph-observation envelope, got "": unexpected end of JSON input |
| TOP | `TestHookCommandPreservesAuditOnOutboxFailure` | `internal/cli/run_test.go` | audit persistence failure must remain visible as exit 1, got 0 |
| TOP | `TestHookCommandReportsOutboxFailureWithoutToolDeny` | `internal/cli/run_test.go` | audit persistence failure must remain visible as exit 1, got 0 |
| TOP | `TestHumanReleaseQuestionnaireNamesUnverifiablePackage` | `internal/cli/audit_questionnaire_test.go` | S11 missing-package context "S11 current human decision path is unavailable: current Runtime has no positive baseline generation and review plan. Main must resolve the recorded material gap; do not re |
| TOP | `TestLastFinalPreparesImpactBeforeIndependentCleanup` | `internal/cli/workspace_cleanup_test.go` | --- FAIL: TestLastFinalPreparesImpactBeforeIndependentCleanup (49.61s) |
| SUB | `TestLastFinalPreparesImpactBeforeIndependentCleanup/failed-main-check` | `internal/cli/workspace_cleanup_test.go` | first Final: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW4tZXhlY3V0aW9uLWNo |
| SUB | `TestLastFinalPreparesImpactBeforeIndependentCleanup/impact-before-cleanup` | `internal/cli/workspace_cleanup_test.go` | first Final: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW4tZXhlY3V0aW9uLWNo |
| SUB | `TestLastFinalPreparesImpactBeforeIndependentCleanup/impact-prepare-failure` | `internal/cli/workspace_cleanup_test.go` | first Final: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW4tZXhlY3V0aW9uLWNo |
| SUB | `TestLastFinalPreparesImpactBeforeIndependentCleanup/view-change-during-check` | `internal/cli/workspace_cleanup_test.go` | first Final: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW4tZXhlY3V0aW9uLWNo |
| SUB | `TestLastFinalPreparesImpactBeforeIndependentCleanup/view-symlink-during-check` | `internal/cli/workspace_cleanup_test.go` | first Final: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW4tZXhlY3V0aW9uLWNo |
| TOP | `TestLifecycleVerbChainE2E` | `internal/cli/lifecycle_e2e_test.go` | receipt must carry distinct before/after fingerprints |
| TOP | `TestMainPlanningReadCycleConsumesAcceptedS2Followup` | `internal/cli/planning_followup_read_cycle_e2e_test.go` | ordinary Stop changed pending work: before=2 after=3 err=<nil> |
| TOP | `TestMainReviewPlanPreReceivesReadyOriginal` | `internal/cli/review_plan_pre_listener_e2e_test.go` | --- FAIL: TestMainReviewPlanPreReceivesReadyOriginal (0.75s) |
| SUB | `TestMainReviewPlanPreReceivesReadyOriginal/PostToolUse/legacy/2` | `internal/cli/review_plan_pre_listener_e2e_test.go` | missing planning guidance: <nil> # Existing historical guidance |
| SUB | `TestMainReviewPlanPreReceivesReadyOriginal/PostToolUse/plan/2` | `internal/cli/review_plan_pre_listener_e2e_test.go` | registered plan did not provide its actual preparation/dependency step "assignment-e2e-1 waits": {"hookSpecificOutput":{"additionalContext":"S7 Main ReviewPlan questionnaire observed at /tmp/TestMainR |
| SUB | `TestMainReviewPlanPreReceivesReadyOriginal/PreToolUse/legacy/1` | `internal/cli/review_plan_pre_listener_e2e_test.go` | missing planning guidance: <nil> # Existing historical guidance |
| TOP | `TestMainReviewPlanQuestionnaireStopDoesNotPrepareAndInvalidSaveReturnsFeedback` | `internal/cli/review_plan_questionnaire_e2e_test.go` | invalid draft did not return original correction feedback: {"systemMessage":"PostToolUse observed (no dispatch message captured: not a SendMessage payload)"} |
| TOP | `TestMainS2S5MilestoneDirectoriesProjectActualReadSources` | `internal/cli/work_progress_milestone_directory_test.go` | --- FAIL: TestMainS2S5MilestoneDirectoriesProjectActualReadSources (0.01s) |
| SUB | `TestMainS2S5MilestoneDirectoriesProjectActualReadSources/S2` | `internal/cli/work_progress_milestone_directory_test.go` | actual source path was not projected: /tmp/TestMainS2S5MilestoneDirectoriesProjectActualReadSourcesS2588392614/001/docs/architecture/ARCHITECTURE-001.md |
| SUB | `TestMainS2S5MilestoneDirectoriesProjectActualReadSources/S5` | `internal/cli/work_progress_milestone_directory_test.go` | actual source path was not projected: /tmp/TestMainS2S5MilestoneDirectoriesProjectActualReadSourcesS53075639779/001/docs/dev/tasks/TASK-001.md |
| TOP | `TestMainS6LockedReadCategoriesResolveRegisteredCurrentFiles` | `internal/cli/work_progress_milestone_directory_test.go` | category projection omitted the actual contract: map[] |
| TOP | `TestNativeContextBindingCheckpointPreservesFixedFactsAndRejectsMissingSources` | `internal/cli/assignment_context_binding_test.go` | checkpoint restored another/lost fixed profile source |
| TOP | `TestNativeContextBindingFreezesAcrossWorkingStateAndRejectsSourceDrift` | `internal/cli/assignment_context_binding_test.go` | source drift did not preserve binding with accurate source feedback: 0 |
| TOP | `TestNativeExecutionLaunchPreservesOriginalPartialReceipt` | `internal/cli/native_execution_return_test.go` | transition top-level TR-008: required evidence slot "observation_batch_record" is not declared in EvidenceCatalog |
| TOP | `TestNativeMainReturnReadsS5ReceiptAcrossStageWithoutReceivingAnswers` | `internal/cli/native_main_return_test.go` | ambiguous return selected one responsibility: Some original responsibilities remain unknown; Main inspects the original dispatch and preserves any accepted results. |
| TOP | `TestNativeMainS7ReturnKeepsSubmissionDiagnosticWhenOwnBodyFails` | `internal/cli/native_main_return_test.go` | original Stop did not record the unreceived responsibility: 0 |
| TOP | `TestNativeS5AmbiguousReferencesDoNotReceiveEitherAnswer` | `internal/cli/native_review_hooks_test.go` | ambiguous task was claimed: 0 S5 lifecycle handoff contains an unresolved original responsibility; Main must inspect the original dispatch |
| TOP | `TestNativeS6BriefMinimalBlockerWorksBeforeAndAfterPlan` | `internal/cli/native_s6_boundary_test.go` | minimal blocker bypassed original source checking: 0 Native S6 blocker returned for Main; no completion receipt created: Implementation cannot continue; fixture-based checks remain unfinished. / Requi |
| TOP | `TestNativeS6DifferentSourceCannotUseNormalResumeToReceive` | `internal/cli/native_s6_source_resume_test.go` | wrong source accepted as resume: 0 |
| TOP | `TestNativeS6FixedSourceAlignmentAfterMainAdvances` | `internal/cli/native_s6_guidance_delivery_test.go` | unexpected end of JSON input |
| TOP | `TestNativeS6HandbackUsesInitialTaskAndActualArtifacts` | `internal/cli/native_s6_test.go` | unreported source borrowed receipt: {"action":"integration verified; completion_ack recorded; Main must preserve and inspect the remaining worktree files before retrying cleanup","assignment_id":"assi |
| TOP | `TestNativeS6MainCommandRecoversActualLostMergeAndLaterTarget` | `internal/cli/native_s6_merge_recovery_test.go` | --- FAIL: TestNativeS6MainCommandRecoversActualLostMergeAndLaterTarget (7.77s) |
| SUB | `TestNativeS6MainCommandRecoversActualLostMergeAndLaterTarget/merged-before-verification-receipt` | `internal/cli/native_s6_merge_recovery_test.go` | public Main recovery did not preserve original merge/current Main checks: {PreviousMainRoots:[] VerifiedAt:2026-11-01T18:26:55.412115244Z CompletionReportPath:.claude/evidence/loop-req039-ct/g1/assign |
| SUB | `TestNativeS6MainCommandRecoversActualLostMergeAndLaterTarget/ready-before-merge-receipt` | `internal/cli/native_s6_merge_recovery_test.go` | original native S6 Main recovery: 1  runtime task-integrate: merge outcome is unknown after interruption; Main must inspect the original worktree and preserve the checkpoint |
| TOP | `TestNativeS6PrepareReplayCannotBorrowChangedTASK` | `internal/cli/native_s6_task_source_test.go` | --- FAIL: TestNativeS6PrepareReplayCannotBorrowChangedTASK (4.19s) |
| SUB | `TestNativeS6PrepareReplayCannotBorrowChangedTASK/changed-task` | `internal/cli/native_s6_task_source_test.go` | open /tmp/TestNativeS6PrepareReplayCannotBorrowChangedTASKchanged-task1852151125/001/.claude/assignments/native/assignment-build-TASK-039-01-g1/r1/source.json: no such file or directory |
| SUB | `TestNativeS6PrepareReplayCannotBorrowChangedTASK/changed-task-after-fingerprint` | `internal/cli/native_s6_task_source_test.go` | open /tmp/TestNativeS6PrepareReplayCannotBorrowChangedTASKchanged-task-af2626743211/001/.claude/assignments/native/assignment-build-TASK-039-01-g1/r1/source.json: no such file or directory |
| SUB | `TestNativeS6PrepareReplayCannotBorrowChangedTASK/same-task-later-main` | `internal/cli/native_s6_task_source_test.go` | open /tmp/TestNativeS6PrepareReplayCannotBorrowChangedTASKsame-task-later764509287/001/.claude/assignments/native/assignment-build-TASK-039-01-g1/r1/source.json: no such file or directory |
| TOP | `TestNativeS6RecordedResultProjectsActualUnfinishedWork` | `internal/cli/native_s6_received_guidance_test.go` | --- FAIL: TestNativeS6RecordedResultProjectsActualUnfinishedWork (9.50s) |
| SUB | `TestNativeS6RecordedResultProjectsActualUnfinishedWork/blocked` | `internal/cli/native_s6_received_guidance_test.go` | CLI [s6 prepare --root /tmp/TestNativeS6RecordedResultProjectsActualUnfinishedWorkblocked827400101/001 --task TASK-039-01 --role backend-builder --revise-assignment assignment-build-TASK-039-01-g1 --r |
| SUB | `TestNativeS6RecordedResultProjectsActualUnfinishedWork/incomplete` | `internal/cli/native_s6_received_guidance_test.go` | CLI [s6 prepare --root /tmp/TestNativeS6RecordedResultProjectsActualUnfinishedWorkincomplet2634895381/001 --task TASK-039-01 --role backend-builder --revise-assignment assignment-build-TASK-039-01-g1  |
| TOP | `TestNativeS6RevisionKeepsObjectGuardsAcrossUnrelatedRuntimeCommit` | `internal/cli/revision_boundary_e2e_test.go` | --- FAIL: TestNativeS6RevisionKeepsObjectGuardsAcrossUnrelatedRuntimeCommit (10.25s) |
| SUB | `TestNativeS6RevisionKeepsObjectGuardsAcrossUnrelatedRuntimeCommit/original-source-snapshot-changed` | `internal/cli/revision_boundary_e2e_test.go` | changed original authority was not rejected by the original consumer: change=original-source-snapshot-changed advanced=true code=0 stderr= stdout=Native S6 TASK registered: TASK-039-01 |
| SUB | `TestNativeS6RevisionKeepsObjectGuardsAcrossUnrelatedRuntimeCommit/unrelated` | `internal/cli/revision_boundary_e2e_test.go` | original Writer did not issue the exact native successor: {SchemaVersion: Contract: RuntimeID: BaselineGeneration:0 RegistrationRevision:0 AssignmentID: Revision:0 Task:{Kind: Ref: ID: Version:} Manif |
| TOP | `TestNativeS6SameTASKReworkClosesWithoutChangingSpecification` | `internal/cli/native_s6_task_source_test.go` | original rework basis missing: {PreviousMainRoots:[] VerifiedAt:2026-11-01T18:27:19.262471648Z CompletionReportPath:.claude/evidence/loop-req039-ct/g1/assignments/native-TASK-039-01/ev-completion-TASK |
| TOP | `TestNativeS6SourceBranchesPrecedeActualInputReads` | `internal/cli/native_s6_source_resume_test.go` | "First start, before any responsibility work:" must precede TASK reads: # TASK-039-01 — backend-builder |
| TOP | `TestNativeS6StartRequiresExactResponsibility` | `internal/cli/native_s6_guidance_delivery_test.go` | opaque-native Start: 0 {"hookSpecificOutput":{"additionalContext":"Native S6 TASK-039-01 (Assignment assignment-build-TASK-039-01-g1 revision 1): follow the original exact brief /tmp/TestNativeS6Start |
| TOP | `TestNativeS6UnifiedReworkAndRecoveryUseOriginalRecordWriter` | `internal/cli/native_s6_test.go` | CLI [s6 prepare --root /tmp/TestNativeS6UnifiedReworkAndRecoveryUseOriginalRecordWriter1775943691/001 --task TASK-039-01 --role backend-builder --revise-assignment assignment-build-TASK-039-01-g1 --re |
| TOP | `TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication` | `internal/cli/native_s9_consumer_retention_test.go` | bounded producer artifacts not recognized |
| TOP | `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields` | `internal/cli/native_s9_artifact_feedback_test.go` | --- FAIL: TestNativeS9FlatValidationFeedbackUsesPrivateFileFields (20.64s) |
| SUB | `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields/PlanReport` | `internal/cli/native_s9_artifact_feedback_test.go` | Runtime file 0 changed on a read-only replay |
| SUB | `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields/blocked_Result` | `internal/cli/native_s9_artifact_feedback_test.go` | missing "Main owns recovery" in feedback: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmV |
| SUB | `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields/unknown_Result_field` | `internal/cli/native_s9_artifact_feedback_test.go` | missing "Main owns recovery" in feedback: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmV |
| SUB | `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields/wrong_Result_type` | `internal/cli/native_s9_artifact_feedback_test.go` | feedback contains "填写 answer": Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW |
| TOP | `TestNativeS9LegacyPlanValidationKeepsOriginalEnvelopeFeedback` | `internal/cli/native_s9_artifact_feedback_test.go` | open /tmp/TestNativeS9LegacyPlanValidationKeepsOriginalEnvelopeFeedback3910207450/002/native-worker/.claude/submissions/loop-req039-ct/g1/S9-plan/repair-session-execution-choice/repair-assignment-unit |
| TOP | `TestNativeS9ListenerIgnoresFailedPostAndOtherCheckout` | `internal/cli/native_s9_listener_test.go` | source rejection did not reach actual Worker: |
| TOP | `TestNativeS9ListenerReceivesPlanBeforeResultAndPreservesDirtyWork` | `internal/cli/native_s9_listener_test.go` | natural Result: 0 Main original ChangeImpact answer: /tmp/TestNativeS9ListenerReceivesPlanBeforeResultAndPreservesDirtyWor1445320698/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZX |
| TOP | `TestNativeS9MainCompletedReturnContinuesOriginalSourceAfterCleanup` | `internal/cli/native_s9_main_continuation_test.go` | mkdir /tmp/TestNativeS9MainCompletedReturnContinuesOriginalSourceAfterClean4124590545/001/.claude/evidence/s9-source/packet-NDpwbGFuMTQ6bG9vcC1yZXEwMzktY3QzMTpyZXBhaXItc2Vzc2lvbi1leGVjdXRpb24tY2hvaWNl |
| TOP | `TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate` | `internal/cli/native_s9_reception_split_test.go` | PreToolUse did not explain the missing staged input: Main original ChangeImpact answer: /tmp/TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidat1423833105/001/.claude/submissions/s9/p/WyJ |
| TOP | `TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint` | `internal/cli/workspace_missing_private_test.go` | --- FAIL: TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint (13.76s) |
| SUB | `TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint/accepted-final` | `internal/cli/workspace_missing_private_test.go` | original public cleanup retry: git branch --show-current: exit status 128: fatal: cannot change to '/tmp/TestNativeS9MissingWorkerCleanupResumesOriginalCheckpointaccept3995547903/002/native-worker': N |
| TOP | `TestNativeS9MissingWorktreePlanDoesNotBorrowMainAnswer` | `internal/cli/native_s9_test.go` | --- FAIL: TestNativeS9MissingWorktreePlanDoesNotBorrowMainAnswer (3.47s) |
| SUB | `TestNativeS9MissingWorktreePlanDoesNotBorrowMainAnswer/current-private` | `internal/cli/native_s9_test.go` | missing actual source borrowed Main or lost "native private original must be exactly untracked": true 0  S9 reception for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2U |
| TOP | `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat` | `internal/cli/native_s9_reception_split_test.go` | --- FAIL: TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat (11.74s) |
| SUB | `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat/PreToolUse/changed` | `internal/cli/native_s9_reception_split_test.go` | original typed consumer not resumed: {SubmissionKey: PacketID:packet-NjpyZXN1bHQxNDpsb29wLXJlcTAzOS1jdDMxOnJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UyODpyZXBhaXItcGxhbi1leGVjdXRpb24tY2hvaWNlMjY6cmVwYWl |
| SUB | `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat/PreToolUse/missing` | `internal/cli/native_s9_reception_split_test.go` | original typed consumer not resumed: {SubmissionKey: PacketID:packet-NjpyZXN1bHQxNDpsb29wLXJlcTAzOS1jdDMxOnJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UyODpyZXBhaXItcGxhbi1leGVjdXRpb24tY2hvaWNlMjY6cmVwYWl |
| SUB | `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat/SubagentStop/changed` | `internal/cli/native_s9_reception_split_test.go` | original typed consumer not resumed: {SubmissionKey: PacketID:packet-NjpyZXN1bHQxNDpsb29wLXJlcTAzOS1jdDMxOnJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UyODpyZXBhaXItcGxhbi1leGVjdXRpb24tY2hvaWNlMjY6cmVwYWl |
| SUB | `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat/SubagentStop/missing` | `internal/cli/native_s9_reception_split_test.go` | original typed consumer not resumed: {SubmissionKey: PacketID:packet-NjpyZXN1bHQxNDpsb29wLXJlcTAzOS1jdDMxOnJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UyODpyZXBhaXItcGxhbi1leGVjdXRpb24tY2hvaWNlMjY6cmVwYWl |
| TOP | `TestNativeS9NullProfessionalReferenceIsNotConvertedToNotApplicable` | `internal/cli/native_s9_test.go` | unknown reference became an empty declaration: true 0 Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXI |
| TOP | `TestNativeS9OriginalConsumeAcceptedPlanSurvivesMissingDisplayBrief` | `internal/cli/native_s9_recovery_test.go` | display loss swallowed original responsibility: 1  Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcG |
| TOP | `TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup` | `internal/cli/native_s9_consumer_retention_test.go` | original consumer did not reject incomplete assertions: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBha |
| TOP | `TestNativeS9OriginalStopUsesNewProfessionalInputAfterSourceGap` | `internal/cli/native_s9_recovery_test.go` | missing actual evidence unexpectedly accepted |
| TOP | `TestNativeS9PendingPublicPreReturnsExecutableOriginalWriterRecovery` | `internal/cli/native_s9_pending_feedback_test.go` | same-event recovery did not name the exact original integration successor: S9 Main independent verifier division: /tmp/TestNativeS9PendingPublicPreReturnsExecutableOriginalWriterRecov3929827296/001/.c |
| TOP | `TestNativeS9PlanOnlyStopReportsUnreadyOriginalPlan` | `internal/cli/native_s9_plan_only_stop_test.go` | --- FAIL: TestNativeS9PlanOnlyStopReportsUnreadyOriginalPlan (4.74s) |
| SUB | `TestNativeS9PlanOnlyStopReportsUnreadyOriginalPlan/field_error` | `internal/cli/native_s9_plan_only_stop_test.go` | Runtime file 0 changed on a read-only replay |
| TOP | `TestNativeS9PreparedBriefAndConsumerPreserveSatisfiedAssertion` | `internal/cli/native_s9_result_guidance_test.go` | --- FAIL: TestNativeS9PreparedBriefAndConsumerPreserveSatisfiedAssertion (6.33s) |
| SUB | `TestNativeS9PreparedBriefAndConsumerPreserveSatisfiedAssertion/complete-original-set` | `internal/cli/native_s9_result_guidance_test.go` | prepared Result field lacks the original ordered assertion set |
| SUB | `TestNativeS9PreparedBriefAndConsumerPreserveSatisfiedAssertion/omitted-satisfied-assertion` | `internal/cli/native_s9_result_guidance_test.go` | prepared Result field lacks the original ordered assertion set |
| TOP | `TestNativeS9PublicAuthorCorrectsRejectedResult` | `internal/cli/native_s9_reception_split_test.go` | --- FAIL: TestNativeS9PublicAuthorCorrectsRejectedResult (8.55s) |
| SUB | `TestNativeS9PublicAuthorCorrectsRejectedResult/PostToolUse` | `internal/cli/native_s9_reception_split_test.go` | original professional rejection was not delivered: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcG |
| SUB | `TestNativeS9PublicAuthorCorrectsRejectedResult/PreToolUse` | `internal/cli/native_s9_reception_split_test.go` | original professional rejection was not delivered: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcG |
| SUB | `TestNativeS9PublicAuthorCorrectsRejectedResult/SubagentStop` | `internal/cli/native_s9_reception_split_test.go` | original professional rejection was not delivered: Original S9 PlanReport received for builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcG |
| TOP | `TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears` | `internal/cli/native_s9_reception_split_test.go` | PreToolUse hid the original unavailable source: Main original ChangeImpact answer: /tmp/TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceD1557564011/001/.claude/submissions/s9/p/WyJsb29w |
| TOP | `TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns` | `internal/cli/native_s9_reception_split_test.go` | old input did not actually fail on its missing source: Main original ChangeImpact answer: /tmp/TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns1287584788/001/.claude/submissions/s9/p/WyJsb29 |
| TOP | `TestNativeS9RejectsChangedDeliverySourcesBeforeAnyMainIntegration` | `internal/cli/native_s9_preintegration_test.go` | --- FAIL: TestNativeS9RejectsChangedDeliverySourcesBeforeAnyMainIntegration (7.56s) |
| SUB | `TestNativeS9RejectsChangedDeliverySourcesBeforeAnyMainIntegration/candidate_bytes` | `internal/cli/native_s9_preintegration_test.go` | Runtime file 0 changed on a read-only replay |
| SUB | `TestNativeS9RejectsChangedDeliverySourcesBeforeAnyMainIntegration/registered_definition` | `internal/cli/native_s9_preintegration_test.go` | Runtime file 0 changed on a read-only replay |
| TOP | `TestNativeS9RelativeTargetedPublicEventUsesIndependentOriginal` | `internal/cli/native_s9_reference_acceptance_test.go` | relative Targeted bypassed source guard: Questionnaire: /tmp/TestNativeS9RelativeTargetedPublicEventUsesIndependentOriginal4212588654/001/.loop-submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXN |
| TOP | `TestNativeS9ResumeReceivesLatestActualTaskWithoutToolResultGuess` | `internal/cli/native_s9_test.go` | unrelated return: false 0 |
| TOP | `TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop` | `internal/cli/native_s9_targeted_source_guard_test.go` | isolated Stop borrowed Main's original (cwd /tmp/TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop2558874/002/isolated-targeted): Questionnaire: /tmp/TestNativeS9SharedTargetedCannotBeBorrowedB |
| TOP | `TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute` | `internal/cli/native_snapshot_failure_external_test.go` | fixture did not cause the expected Snapshot failure: <nil> |
| TOP | `TestNativeStopPendingFrozenS9RetainsSamePublicStopGate` | `internal/cli/native_stop_pending_test.go` | fixture did not exercise original frozen unfinished Stop gate: 0 S9 Builder builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwY |
| TOP | `TestNextProjectsInvestigationAsS8AndRepairAsS9` | `internal/cli/run_test.go` | --- FAIL: TestNextProjectsInvestigationAsS8AndRepairAsS9 (0.02s) |
| SUB | `TestNextProjectsInvestigationAsS8AndRepairAsS9/S9_planning_uses_original_PlanReport_answers` | `internal/cli/run_test.go` | projection action should mention "private professional PlanReport": {"stage":"S9","protocol_ref":"docs/control/agent-protocol.md#s9","objective":"execute approved RepairContracts and target-reverify t |
| SUB | `TestNextProjectsInvestigationAsS8AndRepairAsS9/S9_reproduction_follows_saved_PlanReports` | `internal/cli/run_test.go` | projection action should mention "original consumer": {"stage":"S9","protocol_ref":"docs/control/agent-protocol.md#s9","objective":"execute approved RepairContracts and target-reverify them","action": |
| SUB | `TestNextProjectsInvestigationAsS8AndRepairAsS9/S9_targeted_answers_use_supplied_paths` | `internal/cli/run_test.go` | projection action should mention "exact TargetedReverification answer for SubagentStop": {"stage":"S9","protocol_ref":"docs/control/agent-protocol.md#s9","objective":"execute approved RepairContracts  |
| SUB | `TestNextProjectsInvestigationAsS8AndRepairAsS9/fixing_continues_dispatched_builders` | `internal/cli/run_test.go` | projection action should mention "SubagentStop receives them": {"stage":"S9","protocol_ref":"docs/control/agent-protocol.md#s9","objective":"execute approved RepairContracts and target-reverify them", |
| TOP | `TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero` | `internal/cli/initialization_format_test.go` | --- FAIL: TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero (0.25s) |
| SUB | `TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero/started-review` | `internal/cli/initialization_format_test.go` | pending initialization accepted a started review |
| TOP | `TestPendingIntegrationsSurviveRestartAndIncludeCleanup` | `internal/cli/workspace_pending_test.go` | lost or fabricated pending work: [] |
| TOP | `TestPlanningObserverUsesAnswerAndBoundCommitWithoutRepeatingUnchangedSave` | `internal/cli/questionnaire_observer_e2e_test.go` | --- FAIL: TestPlanningObserverUsesAnswerAndBoundCommitWithoutRepeatingUnchangedSave (2.43s) |
| SUB | `TestPlanningObserverUsesAnswerAndBoundCommitWithoutRepeatingUnchangedSave/S4` | `internal/cli/questionnaire_observer_e2e_test.go` | original consumer was not invoked: |
| TOP | `TestPlanningReworkCLIRejectsFrozenDesignDrift` | `internal/cli/planning_rework_e2e_test.go` | contracts rework washed changed design |
| TOP | `TestPlanningReworkCLIRejectsInvalidRequests` | `internal/cli/planning_rework_e2e_test.go` | REQ change accepted as rework |
| TOP | `TestPlanningReworkCLISeparatesCommittedPreparationFailure` | `internal/cli/planning_rework_e2e_test.go` | lost committed/preparation boundary {true design publication failure remains recoverable 2 ready map[answer_path:.claude/submissions-alt-1/loop-req039-ct/g1/S2/e2/packet-14-loop-req039-ct-g1-S2-e2-Mai |
| TOP | `TestPlanningReworkCLITasksRejectsUpstreamDrift` | `internal/cli/planning_rework_e2e_test.go` | --- FAIL: TestPlanningReworkCLITasksRejectsUpstreamDrift (10.19s) |
| SUB | `TestPlanningReworkCLITasksRejectsUpstreamDrift/REQ` | `internal/cli/planning_rework_e2e_test.go` | tasks rework washed upstream drift REQ |
| SUB | `TestPlanningReworkCLITasksRejectsUpstreamDrift/contract` | `internal/cli/planning_rework_e2e_test.go` | tasks rework washed upstream drift contract |
| SUB | `TestPlanningReworkCLITasksRejectsUpstreamDrift/design` | `internal/cli/planning_rework_e2e_test.go` | tasks rework washed upstream drift design |
| TOP | `TestPreToolUseObservesUnknownMCPForWorker` | `internal/cli/controller_integration_test.go` | Hook output is not JSON: unexpected end of JSON input; output= |
| TOP | `TestProgressIgnoresOtherTypedPacketsButKeepsS5Diagnostics` | `internal/cli/native_convergence_test.go` | supported S5 corruption disappeared: <nil> |
| TOP | `TestProgressPacketS9SkipsLegacyCoverageWithoutCache` | `internal/cli/work_progress_test.go` | repair_readback entered legacy actor/packet coverage or inferred completion: {Binding:["loop-s9-listener","bug_resolution","repair_readback",null,"native-platform-id"] Owner:native-platform-id Session |
| TOP | `TestPublicS9StopIdlePreservesOriginalResultState` | `internal/cli/s7_original_hook_nonblocking_test.go` | --- FAIL: TestPublicS9StopIdlePreservesOriginalResultState (12.73s) |
| SUB | `TestPublicS9StopIdlePreservesOriginalResultState/unreceived` | `internal/cli/s7_original_hook_nonblocking_test.go` | public TeammateIdle: 2  teammate_idle_resume_assignment — teammate builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW |
| TOP | `TestREQBindArchivesControllerTouchedInactiveRuntimeAndStartsAtRevisionZero` | `internal/cli/bind_rollover_test.go` | --- FAIL: TestREQBindArchivesControllerTouchedInactiveRuntimeAndStartsAtRevisionZero (1.88s) |
| SUB | `TestREQBindArchivesControllerTouchedInactiveRuntimeAndStartsAtRevisionZero/rotated=false` | `internal/cli/bind_rollover_test.go` | bind archive/receipt did not preserve the complete pre-bind journal: receipt=map[approved_by:user event:req_bound event_id:evt-tr-001-r9 occurred_at:2026-11-01T18:22:00.765039782Z source_revision:8 so |
| SUB | `TestREQBindArchivesControllerTouchedInactiveRuntimeAndStartsAtRevisionZero/rotated=true` | `internal/cli/bind_rollover_test.go` | bind archive/receipt did not preserve the complete pre-bind journal: receipt=map[approved_by:user event:req_bound event_id:evt-tr-001-r9 occurred_at:2026-11-01T18:22:01.542439808Z source_revision:8 so |
| TOP | `TestRepairResultPacketMatchesOnlyExactRegisteredExecution` | `internal/cli/workspace_delivery_matcher_test.go` | --- FAIL: TestRepairResultPacketMatchesOnlyExactRegisteredExecution (0.01s) |
| SUB | `TestRepairResultPacketMatchesOnlyExactRegisteredExecution/frozen_assignment_cannot_substitute_another_ID` | `internal/cli/workspace_delivery_matcher_test.go` | forged frozen execution accepted: map[agent_id:agent-builder assignment_id:assignment-registered baseline_generation:2 branch:s9-worker execution_generation:3 inputs:map[task:TASK-1] required_checks:[ |
| SUB | `TestRepairResultPacketMatchesOnlyExactRegisteredExecution/frozen_definition_must_equal_current_registered_definition` | `internal/cli/workspace_delivery_matcher_test.go` | drifted actual registered definition accepted: map[agent_id:agent-builder assignment_id:assignment-registered baseline_generation:2 branch:s9-worker execution_generation:3 inputs:map[task:TASK-1] requ |
| TOP | `TestReportPolicyRefDriftDetectsVersionDrift` | `internal/cli/policy_ref_test.go` | expected doctor to fail on version drift, got exit 0 (stdout="" stderr="") |
| TOP | `TestReportPolicyRefDriftPassesWhenConsistent` | `internal/cli/policy_ref_test.go` | expected consistency line, got "" |
| TOP | `TestResumeRefusedOnBaselineDriftRoutesToAmend` | `internal/cli/lifecycle_review_test.go` | resume must refuse on baseline drift |
| TOP | `TestReviewPlanGuidancePreservesOriginalResponsibility` | `internal/cli/review_plan_guidance_identity_test.go` | --- FAIL: TestReviewPlanGuidancePreservesOriginalResponsibility (0.03s) |
| SUB | `TestReviewPlanGuidancePreservesOriginalResponsibility/different_plan_same_number` | `internal/cli/review_plan_guidance_identity_test.go` | draft was mistaken for professional answer: {Context:S7 Main ReviewPlan questionnaire observed at /tmp/TestReviewPlanGuidancePreservesOriginalResponsibilitydifferent_3733490348/001/.claude/submissions |
| SUB | `TestReviewPlanGuidancePreservesOriginalResponsibility/no_plan` | `internal/cli/review_plan_guidance_identity_test.go` | draft was mistaken for professional answer: {Context:S7 Main ReviewPlan questionnaire observed at /tmp/TestReviewPlanGuidancePreservesOriginalResponsibilityno_plan1327360434/001/.claude/submissions/ru |
| SUB | `TestReviewPlanGuidancePreservesOriginalResponsibility/same_plan_historical_number` | `internal/cli/review_plan_guidance_identity_test.go` | draft was mistaken for professional answer: {Context:S7 Main ReviewPlan questionnaire observed at /tmp/TestReviewPlanGuidancePreservesOriginalResponsibilitysame_plan_2845232051/001/.claude/submissions |
| SUB | `TestReviewPlanGuidancePreservesOriginalResponsibility/seed` | `internal/cli/review_plan_guidance_identity_test.go` | draft was mistaken for professional answer: {Context:S7 Main ReviewPlan questionnaire observed at /tmp/TestReviewPlanGuidancePreservesOriginalResponsibilityseed789611965/001/.claude/submissions/runtim |
| TOP | `TestReviewPlanRecoveryHelpAndJSONFilePath` | `internal/cli/simulation_recovery_test.go` | help missing JSON file: Usage: loop-harness runtime review-plan revive |
| TOP | `TestRuntimeInvestigationContractApproveCLIHandsOffToS9` | `internal/cli/investigation_command_test.go` | approval output lacks S9 handoff: stdout={"case_id":"investigation-case-review-plan-r1","case_path":".claude/review/investigation/cases/investigation-case-review-plan-r1-r2.json","case_revision":2,"re |
| TOP | `TestRuntimeReconcilePolicyRefCheckOnlyDoesNotWrite` | `internal/cli/policy_ref_test.go` | --check must report drift with a non-zero exit, got 0 |
| TOP | `TestRuntimeReconcilePolicyRefNoopWhenConsistent` | `internal/cli/policy_ref_test.go` | expected no-op message, got "policy available path=docs/control/hook-policy.json version=v2.0.0; historical contents are not verified\n" |
| TOP | `TestRuntimeReconcilePolicyRefRealignsVersion` | `internal/cli/policy_ref_test.go` | expected version transition in output, got "policy available path=docs/control/hook-policy.json version=v2.0.0; historical contents are not verified\n" |
| TOP | `TestRuntimeRepairTargetedResumeReopensBlockedVerification` | `internal/cli/repair_status_test.go` | targeted resume code=2 stdout= stderr=runtime repair targeted requires <create\\|commit> |
| TOP | `TestS10PublicHookSeparatesLateReviewerFromMainQuestionnaire` | `internal/cli/s10_role_display_test.go` | opaque-returned-reviewer: unexpected end of JSON input |
| TOP | `TestS10ReviewRequiredPendingReturnDoesNotReceiveChangedOrWorkerAnswer` | `internal/cli/audit_review_required_test.go` | --- FAIL: TestS10ReviewRequiredPendingReturnDoesNotReceiveChangedOrWorkerAnswer (3.31s) |
| SUB | `TestS10ReviewRequiredPendingReturnDoesNotReceiveChangedOrWorkerAnswer/unsubmitted_change` | `internal/cli/audit_review_required_test.go` | unrelated tool received changed work or Worker consumed Main continuation |
| TOP | `TestS10ReviewRequiredReturnsOriginalFullReviewAndNewPlan` | `internal/cli/audit_review_required_test.go` | received negative decision did not return through TR-016: map[phase:planned phase_revision:2 state:verification] <nil> {"hookSpecificOutput":{"additionalContext":"S10 acceptance recorded at /tmp/TestS |
| TOP | `TestS10StatusRejectsManifestFromStaleBaseline` | `internal/cli/s10_command_test.go` | historical report must not satisfy or hide the current responsibility: map[string]interface {}{"metrics":map[string]interface {}{"audit_area_coverage":0, "blocking_finding_count":0, "changed_path_cove |
| TOP | `TestS10StatusReportsBlockedManifestRoute` | `internal/cli/s10_command_test.go` | status must expose the blocked route: map[string]interface {}{"metrics":map[string]interface {}{"audit_area_coverage":0, "blocking_finding_count":0, "changed_path_coverage":0, "contract_coverage":0, " |
| TOP | `TestS10StatusReportsTheCurrentRoundAndNextAction` | `internal/cli/s10_command_test.go` | unexpected S10 status: map[string]interface {}{"acceptance":map[string]interface {}{"metrics":map[string]interface {}{"audit_area_coverage":0, "blocking_finding_count":0, "changed_path_coverage":0, "c |
| TOP | `TestS3ContractPipelineE2E` | `internal/cli/contracts_e2e_test.go` | registered sha must match disk |
| TOP | `TestS3UncommittedOutputNamesCommitAndAcceptsSameAnswerAfterCommit` | `internal/cli/contract_followup_rework_e2e_test.go` | uncommitted professional output sent Main to a different route: {SubmissionKey:submission:packet-14-loop-req039-ct-g1-S3-e1-Main PacketID:packet-14-loop-req039-ct-g1-S3-e1-Main Accepted:true AttemptSt |
| TOP | `TestS4AcceptedDescriptionChangeKeepsCompletionButCannotRegisterChangedMaterials` | `internal/cli/s4_continuation_test.go` | historical completion or pending Main registration was lost: {SubmissionKey:submission:packet-14-loop-req039-ct-g1-S4-e2-Main PacketID:packet-14-loop-req039-ct-g1-S4-e2-Main Accepted:true AttemptStatu |
| TOP | `TestS4DirectApplyRejectsAcceptedTaskDriftAtomically` | `internal/cli/s4_continuation_test.go` | TR-002 accepted the committed post-reception TASK change: <nil> |
| TOP | `TestS4ReceptionFieldFailurePersistsAcrossSessionAndClears` | `internal/cli/s5_planning_handoff_e2e_test.go` | changed original source accepted |
| TOP | `TestS5FrozenTaskAnalysisSurvivesRevisionAndRejectsLostBytes` | `internal/cli/frozen_task_analysis_e2e_test.go` | corrupt original analysis was silently regenerated |
| TOP | `TestS5PreparationRecoveryRejectsAcceptedSourceDriftWithoutRefreshingSHA` | `internal/cli/s5_manifest_questionnaire_e2e_test.go` | original S5 preparation recovery: got 0 want 1: [{"task_analysis":{"path":".claude/work-packets/analysis/packet-s5-14-loop-req039-ct-g1-e3-32-assignment-REQ-039-g1-dv-e3-spec-30-reviewer-REQ-039-g1-dv |
| TOP | `TestS5RegisteredManifestCannotChangeBeforeFirstQuestionnaire` | `internal/cli/s5_manifest_questionnaire_e2e_test.go` | actual Register omitted the original manifest pin |
| TOP | `TestS5RegisteredManifestSaveRejectsChangedAssociationsAndReplaysUnchanged` | `internal/cli/s5_manifest_questionnaire_e2e_test.go` | original registration did not pin its manifest |
| TOP | `TestS7AcceptedRemainingProjectionPreservesClosedRouting` | `internal/cli/s7_native_remaining_test.go` | --- FAIL: TestS7AcceptedRemainingProjectionPreservesClosedRouting (0.00s) |
| SUB | `TestS7AcceptedRemainingProjectionPreservesClosedRouting/stale` | `internal/cli/s7_native_remaining_test.go` | closed routing dispatched old queue or lost next action:  S7 remaining responsibilities follow the original current handoff: Main addresses the actual change in the current ReviewPlan questionnaire; o |
| TOP | `TestS7AcceptedRemainingProjectionUsesOriginalPrerequisites` | `internal/cli/s7_native_remaining_test.go` | original prerequisite missing "required static Claims are not all dispositioned":  S7 current ReviewPlan status=running remaining responsibilities (Runtime registration does not prove a native Reviewe |
| TOP | `TestS7DispatchGuidanceUsesActualPrerequisitesAndSource` | `internal/cli/s7_guidance_test.go` | --- FAIL: TestS7DispatchGuidanceUsesActualPrerequisitesAndSource (0.01s) |
| SUB | `TestS7DispatchGuidanceUsesActualPrerequisitesAndSource/static-wait` | `internal/cli/s7_guidance_test.go` | incorrect preparation direction: S7 assignment=next: prepare the manifest with '/tmp/TestS7DispatchGuidanceUsesActualPrerequisitesAndSourcestatic-wa1581410859/001/.claude/bin/loop-harness' s7 manifest |
| TOP | `TestS7DraftRefusesNonVerificationStage` | `internal/cli/s7_draft_gate_test.go` | disclosure missing "Finish the current stage": |
| TOP | `TestS7ExistingEditedManifestRecoversRegistrationWithoutOverwrite` | `internal/cli/s7_manifest_draft_test.go` | existing edited manifest recovery lacks runtime register-workgroup: S7 assignment=assignment-qa-static: prepare the manifest with '/tmp/TestS7ExistingEditedManifestRecoversRegistrationWithoutOverwrite |
| TOP | `TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility` | `internal/cli/s7_lifecycle_listener_test.go` | --- FAIL: TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility (0.12s) |
| SUB | `TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility/SubagentStop` | `internal/cli/s7_lifecycle_listener_test.go` | graph projection bypassed original unfinished responsibility check |
| SUB | `TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility/TeammateIdle` | `internal/cli/s7_lifecycle_listener_test.go` | graph projection bypassed original unfinished responsibility check |
| TOP | `TestS7ObserverSkipsAcceptedReviewerQuestionnaires` | `internal/cli/questionnaire_observer_s7_test.go` | --- FAIL: TestS7ObserverSkipsAcceptedReviewerQuestionnaires (0.00s) |
| SUB | `TestS7ObserverSkipsAcceptedReviewerQuestionnaires/dispatched` | `internal/cli/questionnaire_observer_s7_test.go` | generic candidate=false, want true for original activity dispatched/ |
| SUB | `TestS7ObserverSkipsAcceptedReviewerQuestionnaires/pending` | `internal/cli/questionnaire_observer_s7_test.go` | generic candidate=false, want true for original activity pending/review_plan_registered |
| TOP | `TestS7OldWithoutBaselineAndWrongPlanIDContinueNaturally` | `internal/cli/review_plan_draft_baseline_e2e_test.go` | old draft without a baseline was blocked |
| TOP | `TestS7OptionalProgressUnknownNativeActorIsSilent` | `internal/cli/work_progress_s7_actor_test.go` | --- FAIL: TestS7OptionalProgressUnknownNativeActorIsSilent (0.02s) |
| SUB | `TestS7OptionalProgressUnknownNativeActorIsSilent/broken_agent_context` | `internal/cli/work_progress_s7_actor_test.go` | existing context errors must remain visible |
| SUB | `TestS7OptionalProgressUnknownNativeActorIsSilent/legacy` | `internal/cli/work_progress_s7_actor_test.go` | existing context errors must remain visible |
| SUB | `TestS7OptionalProgressUnknownNativeActorIsSilent/other_stage` | `internal/cli/work_progress_s7_actor_test.go` | existing context errors must remain visible |
| SUB | `TestS7OptionalProgressUnknownNativeActorIsSilent/registered_actor_missing_cwd` | `internal/cli/work_progress_s7_actor_test.go` | existing context errors must remain visible |
| TOP | `TestS7PublicHookReceivesOriginalWithoutLegacyActivationRoute` | `internal/cli/s7_original_hook_nonblocking_test.go` | original answer was not received: S7 Reviewer reviewer-delivery, Assignment assignment-dv-trace-1: the original responsibility context is unavailable or inconsistent. Main/system must inspect the actu |
| TOP | `TestS8DelegationPreparationPreservesChoiceAndRequiresCurrentBinding` | `internal/cli/investigation_contract_decision_test.go` | old finite grant application followed changed REQ hash |
| TOP | `TestS8InvestigationLeadDispositionAvailableBeforeAnyHypothesisResult` | `internal/cli/investigation_lead_disposition_test.go` | s9 qualification did not stay with the original consumer: {"hookSpecificOutput":{"additionalContext":"Questionnaire: /tmp/TestS8InvestigationLeadDispositionAvailableBeforeAnyHypothesisRe3857292258/001 |
| TOP | `TestS8InvestigationLeadDispositionExplainsOriginalCausalAnswerShape` | `internal/cli/investigation_lead_disposition_test.go` | route authoring guidance omitted original professional shape "\"trigger\"": S8 Main route questionnaire: /tmp/TestS8InvestigationLeadDispositionExplainsOriginalCausalAnswerSh3193092320/001/.claude/sub |
| TOP | `TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAndSpecExit` | `internal/cli/investigation_lead_disposition_test.go` | --- FAIL: TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAndSpecExit (1.19s) |
| SUB | `TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAndSpecExit/human_req_change` | `internal/cli/investigation_lead_disposition_test.go` | received inconclusive was treated as unfinished work: S8 Main route questionnaire: /tmp/TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAnd2924724251/001/.claude/submissions/s8/g1/source/ |
| SUB | `TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAndSpecExit/investigate_more` | `internal/cli/investigation_lead_disposition_test.go` | received inconclusive was treated as unfinished work: S8 Main route questionnaire: /tmp/TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAnd1045862545/001/.claude/submissions/s8/g1/source/ |
| SUB | `TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAndSpecExit/s2_spec_rework` | `internal/cli/investigation_lead_disposition_test.go` | received inconclusive was treated as unfinished work: S8 Main route questionnaire: /tmp/TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAnd383949151/001/.claude/submissions/s8/g1/source/b |
| TOP | `TestS8InvestigationLeadWorkerQuestionsMergeWithoutReplacingAuthorRows` | `internal/cli/investigation_lead_questionnaire_test.go` | worker question was not merged while preserving the author row: []cli.investigationLeadQuestion{cli.investigationLeadQuestion{HypothesisID:"hypothesis-investigation-case-review-plan-r1-1", AgentID:"", |
| TOP | `TestS8NativeLifecycleDeliversExactQuestionnaireInHookEnvelope` | `internal/cli/s8_questionnaire_listener_test.go` | ordinary Worker session managed native work: {"hookSpecificOutput":{"additionalContext":"Original Worker responsibility for investigator-01: /tmp/TestS8NativeLifecycleDeliversExactQuestionnaireInHookE |
| TOP | `TestS8ObservedContractPassiveSkipKeepsExplicitSaveAndChangedValidation` | `internal/cli/investigation_lead_contract_observer_test.go` | --- FAIL: TestS8ObservedContractPassiveSkipKeepsExplicitSaveAndChangedValidation (0.60s) |
| SUB | `TestS8ObservedContractPassiveSkipKeepsExplicitSaveAndChangedValidation/mismatched_final_draft` | `internal/cli/investigation_lead_contract_observer_test.go` | changed/missing/invalid draft bypassed original save feedback: {"hookSpecificOutput":{"additionalContext":"Questionnaire: /tmp/TestS8ObservedContractPassiveSkipKeepsExplicitSaveAndChangedVali920729909 |
| TOP | `TestS8UnresolvedResultsAppendDirectlyWithoutRoutePrerequisite` | `internal/cli/investigation_lead_disposition_test.go` | unresolved handoff omitted "inclusion_reason": S8 Main route questionnaire: /tmp/TestS8UnresolvedResultsAppendDirectlyWithoutRoutePrerequisite2253548307/001/.claude/submissions/s8/g1/source/bG9vcC1yZX |
| TOP | `TestS9CapturedInputCoordinateAloneIsNotSavedTypedInput` | `internal/cli/native_execution_return_test.go` | transition top-level TR-008: required evidence slot "observation_batch_record" is not declared in EvidenceCatalog |
| TOP | `TestS9CapturedRecoveryCommandExecutesOriginalTypedConsumer` | `internal/cli/native_execution_return_test.go` | transition top-level TR-008: required evidence slot "observation_batch_record" is not declared in EvidenceCatalog |
| TOP | `TestS9EvidenceReferenceIsPreservedWithoutCopyOrExistenceGate` | `internal/cli/s9_preferred_evidence_integration_test.go` | actual Main integration: 1  Original Final remains accepted; Main preserves completed integration/checks and resumes the original ACK/cleanup independently of the current repair responsibility. integr |
| TOP | `TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain` | `internal/cli/s9_failure_handoff_e2e_test.go` | original committed Plan never triggered the Git barrier: stat /tmp/TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain3089866431/003/git-blocked-after-plan: no such file or directory; Original  |
| TOP | `TestS9HandoffCopyableCommandPreservesLiteralCoordinates` | `internal/cli/s9_handoff_command_test.go` | missing copyable command Original Candidate is received. Main takes the actual contribution, performs necessary integration and checks, then saves its original ChangeImpact. The original Impact can be |
| TOP | `TestS9HandoffPartialCandidateDoesNotBecomeIntegrationDuty` | `internal/cli/s9_handoff_wiring_test.go` | partial receipt lost acceptance: Original S9 owner=builder-s9-WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW9uLWV4ZWN1dGlvbi1jaG9pY2UiLCIuY2xhdWRlL3Jldmlldy9yZXBhaXIvcGxhbnMvcmVwYWlyLXBsYW4tZXhlY3V0aW9uLW |
| SUB | `TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts/accepted_candidate` | `internal/cli/s9_handoff_wiring_test.go` | accepted background return lacks actual Main handoff: {"hookSpecificOutput":{"additionalContext":"Questionnaire: /tmp/TestS9HandoffPublicMainProgressUsesOriginalReceptionFactsaccept2403616626/001/.cla |
| SUB | `TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts/partial_candidate` | `internal/cli/s9_handoff_wiring_test.go` | partial background return became integrate duty: {"hookSpecificOutput":{"additionalContext":"Questionnaire: /tmp/TestS9HandoffPublicMainProgressUsesOriginalReceptionFactspartia1026678854/001/.claude/s |
| TOP | `TestS9LegacyRegisteredObservationKeepsOwnerWhenExecutionUnknown` | `internal/cli/repair_legacy_resolver_test.go` | owner and unknown coordinate not separated: cli.repairExecutionResolution{owner:"", id:"repair-a", execution:map[string]interface {}(nil), err:error(nil)} |
| TOP | `TestS9LegacyResolverSharesRequestCacheAndInvalidatesOnRevision` | `internal/cli/repair_legacy_resolver_test.go` | original selection: cli.repairExecutionResolution{owner:"owner", id:"repair-a", execution:map[string]interface {}(nil), err:error(nil)} |
| TOP | `TestS9ListenerExistingPacketsReadOnlyWithoutIssueRecoveryOrReceiptProof` | `internal/cli/s9_questionnaire_listener_test.go` | S9 entry used generic service/recovery: S9 current TargetedReverification obligations cannot be resolved: statat repair-plan.json: no such file or directory; Main/system must restore the missing sourc |
| TOP | `TestS9NativeDivisionKeepsDependencyAndRejectsExtraWorkspacePreparation` | `internal/cli/s9_questionnaire_observer_test.go` | native responsibility entered legacy workspace preparation:  bind the main workspace first; the target branch is never inferred from develop |
| TOP | `TestS9NativeListenerKeepsPrivateOutputsOutOfMainAuthoring` | `internal/cli/s9_native_listener_guidance_test.go` | owner=Main omitted "/tmp/TestS9NativeListenerKeepsPrivateOutputsOutOfMainAuthoring17571985/001/.claude/submissions/loop-s9-listener/g1/S9-plan/r1/repair-assignment-listener/packet-listener/builder-lis |
| TOP | `TestS9NativeLongPacketPathsRemainConsistent` | `internal/cli/repair_questionnaire_nohash_test.go` | route="" brief=".claude/work-packets/p/p/packet-LongIdentity-LongIdentity-LongIdentity-LongIdentity-LongIdentity-LongIdentity-LongIdentity-LongIdentity-LongIdent/ity-LongIdentity-LongIdentity-LongIden |
| TOP | `TestS9OriginalContractPublicMinimalPlan` | `internal/cli/s9_original_contract_public_test.go` | minimal original Plan: {SubmissionKey: PacketID: Accepted:false AttemptStatus: ResponsibilityStatus: WorkComplete:false ConsumerOwner: ConsumerPending:false CurrentStage: DomainRefs:[] WaitingOn:[] Ne |
| TOP | `TestS9PlanningGuidanceUsesOriginalDivisionAndIssuedOwner` | `internal/cli/s9_repair_status_guidance_test.go` | selected but genuinely unissued original not reported: For any other unselected Assignment, Main selects and saves each unselected Builder Assignment at /tmp/TestS9PlanningGuidanceUsesOriginalDivision |
| TOP | `TestS9PreparationCallbackFollowsOriginalCAS` | `internal/cli/s9_pin_preparation_order_test.go` | stale original CAS succeeded |
| TOP | `TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical` | `internal/cli/s9_reception_feedback_contract_test.go` | --- FAIL: TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical (8.73s) |
| SUB | `TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical/author` | `internal/cli/s9_reception_feedback_contract_test.go` | fixture did not preserve original author failure: {Scope:["loop-req039-ct",1,"investigation-case-execution-choice",null,"repair-contract-investigation-case-execution-choice",".claude/review/investigat |
| TOP | `TestS9RepairDispatchExplicitSavePreparesNativeBriefAndPreservesOriginalSources` | `internal/cli/repair_dispatch_questionnaire_test.go` | passive observation issued or lost a responsibility: packets=2 err=<nil> state=map[string]interface {}{"assignment_owners":map[string]interface {}{"repair-assignment-unit-one":"builder-s9-WyJsb29wLXJl |
| TOP | `TestS9RepairDispatchQuestionnaireResponsibilityAndS8Isolation` | `internal/cli/repair_dispatch_questionnaire_test.go` | S9 division leaked into S8: S9 Main Builder division: /tmp/TestS9RepairDispatchQuestionnaireResponsibilityAndS8Isolation73016383/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsInJlcGFpci1zZXNzaW |
| TOP | `TestS9RepairLeadAnswerShapeUsesActualSources` | `internal/cli/repair_lead_answer_test.go` | targeted=false omitted original field/choice "IM-[A-Z0-9-]+": |
| TOP | `TestS9RepairNextActionKeepsNativeCandidateWithMain` | `internal/cli/controller_test.go` | missing native recovery instruction "Existing frozen executions": native Builders use the supplied brief and save their private professional Plan/Result in the actual worktree. Natural PreToolUse and  |
| TOP | `TestS9RepairVerifierDispatchQuestionnaireOneIndependentContractOwner` | `internal/cli/repair_verifier_dispatch_questionnaire_test.go` | original verifier dispatch feedback lacks "wait for the original Final integration and ChangeImpact": {"hookSpecificOutput":{"additionalContext":"Questionnaire: /tmp/TestS9RepairVerifierDispatchQuesti |
| TOP | `TestS9RepairVerifierDispatchQuestionnaireOriginalPhasesAndResponsibility` | `internal/cli/repair_verifier_dispatch_questionnaire_test.go` | adapter invented verifier dispatch admission in repairing |
| TOP | `TestS9SaveAdapterFindsChangedCurrentPlanWithoutRewritingCaller` | `internal/cli/s9_questionnaire_observer_test.go` | original save adapter required a matching platform ID or Write event: |
| TOP | `TestS9VerifierRegistrationWaitsForTargetedReadiness` | `internal/cli/s9_dispatch_readonly_test.go` | premature dispatch: {"hookSpecificOutput":{"additionalContext":"Questionnaire: /tmp/TestS9VerifierRegistrationWaitsForTargetedReadiness2720102896/001/.claude/submissions/s9/p/WyJsb29wLXJlcTAzOS1jdCIsI |
| TOP | `TestSavedHypothesisResultHookDoesNotTreatUnissuedTeammatePathAsCurrent` | `internal/cli/hypothesis_result_hook_test.go` | unissued teammate path was not rejected by original packet routing: |
| TOP | `TestSavedHypothesisResultHookRejectsUnissuedPathWithoutRuntimeWrite` | `internal/cli/hypothesis_result_hook_test.go` | unissued S8 path did not receive a fail-closed S8 receipt: |
| TOP | `TestSessionStartCommittedS7HandoffDefersOnlyMissingMainQuestionnaire` | `internal/cli/s7_session_handoff_test.go` | missing questionnaire handoff: handled=true message=S7 Main questionnaire preparation gap: Main/system must restore the original current generation and round. |
| TOP | `TestSessionStartEmitsRecoveryPacket` | `internal/cli/run_test.go` | SessionStart must emit a Stage banner, got "{\"hookSpecificOutput\":{\"additionalContext\":\"Runtime snapshot at /tmp/TestSessionStartEmitsRecoveryPacket61559276/001/.claude/loop-state.json could not  |
| TOP | `TestStageAnswerTerminalConsumeRejectsUnissuedS7Path` | `internal/cli/review_result_hook_test.go` | S7 terminal rejection did not come from exact registered-path selection: answer path has no exact current registered issuance |
| TOP | `TestTargetedMainReturnPreservesUnknownHistoricalAssociation` | `internal/cli/s9_handoff_minimal_contract_test.go` | unknown history was described as an obligation or a result:  No original Targeted reception is recorded. Main owns the missing original input/source at original-answer.json: exact absolute input refer |
| TOP | `TestTaskPlanningSaveKeepsActualCancelledTaskWithoutAuthorListProof` | `internal/cli/s4_actual_cancelled_subject_test.go` | cancelled history source drift was silently accepted |
