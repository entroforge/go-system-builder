# DEFECT 调查报告:逐条根因 + 根因聚合

> **阶段**:`TEST-ADAPTATION-REPORT.md` 之后的「DEFECT 逐条调查与聚合」阶段。
> **对象**:总账里 **89 条 DEFECT**(`ledger_full.json`)。
> **性质**:本阶段**只调查、不改生产代码**。DEFECT 的处置约定仍是「只记录,测试保持红」。

## 0. 结论

89 条 DEFECT 全部逐条查清后,它们**并不是 89 个独立问题**:

| 调查结论 | 条数 | 说明 |
|---|---|---|
| **真缺陷(生产违反蓝图)** | **69** | 蓝图是要求类措辞,生产代码没做到;聚合为 **35 个根因组** |
| **应改判 STALE** | **12** | 蓝图已用**移除类**措辞删掉了测试依赖的前提 ⇒ 测试过时,不是缺陷 |
| **改写被阻塞(仍红)** | **1** | 判定是 STALE,但改写依赖一个生产侧决定或另一条真缺陷先落地 |
| **需 owner 裁定** | **0** | 蓝图内部有反向条款,判定方向取决于 owner |
| **实际已绿(总账误记)** | **6** | 早前已被 STALE 适配改写通过,总账没同步 |
| **幽灵条目(仓库中不存在)** | **1** | 占位/聚合行,应删 |
| **合计** | **89** | |

**最关键的一条**:前轮把 12 条测试判成 DEFECT,理由是「生产代码删掉了某个校验 / 不再做某项检查」。但**代码现状不是判据** —— 生产删掉校验,恰恰可能说明**缺陷存在**,也可能说明**蓝图已经移除了该要求**。逐条回到 blueprint 原文核对后,这批条目里有相当一部分是**蓝图用移除类措辞明确取消了旧门**,测试的断言才是过时的。

**红线**:本阶段**没有改任何生产代码**,也**没有改任何测试**;所有产出是调查报告与总账更正建议。

### 0.1 本轮 Lane 1:把已判 STALE 的测试从真缺陷里解耦

范围:把 14 条已判 STALE 但仍红的测试与生产缺陷解耦,使退役门禁真正从测试里退场。

| 结果 | 条数 | 条目 |
|---|---|---|
| **已适配并转绿** | **12** | `TestNativeS9OriginalStopUsesNewProfessionalInputAfterSourceGap`、`TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat`、`TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate`、`TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears`、`TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns`、`TestMainCheckReuseObservationIsReadonlyAndCannotSkipRequiredChecks`、`TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero`、`TestCT03911_DualPassDocumentVerificationCommitsTR003`、`TestCT03915_S11StopsAutoAdvance`、`TestCT03924_SameAgentDualDVLabelsBlocksTR003`、`TestS9PreparationCallbackFollowsOriginalCAS`、`TestNativeBuilderConventionsFixedReplayAndSourceDrift` |
| **已适配但仍红(归新根因组)** | **3** | `TestNativeS9PendingRecoveryReportsChangedPrivateOriginal`、`TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals`、`TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup` |
| **改写被阻塞** | **1** | `TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain` |

改写文件(共 9 个):`internal/cli/ct_11_24_test.go`、`internal/cli/initialization_format_test.go`、`internal/cli/check_capability_e2e_test.go`、`internal/cli/context_conventions_e2e_test.go`、`internal/cli/s9_pin_preparation_order_test.go`、`internal/cli/native_s9_recovery_test.go`、`internal/cli/native_s9_reception_split_test.go`、`internal/cli/native_s9_consumer_retention_test.go`、`internal/cli/s9_failure_handoff_e2e_test.go`。

**本轮新发现的根因组**:**G13**(见 §2 与 §8 的落地方案)。

**退役门禁在本轮真正从测试里退场**:revision / sha / hash / digest / round / CAS / 逐断言覆盖 / 证据来源存在性 / 来源变化散列门 / 复用输入等价分级 / 消费者阶段名白名单 —— 逐条按 blueprint 原文改写,**没有用放宽阈值、删断言、加 skip 或引回散列比对的方式转绿**。

**落地方案**:见仓库根目录 `DEFECT-REMEDIATION-PLAN.md`(36 个根因组 + 新增 G13,逐组给出「问题 → 解法 → 具体改动(file:line) → 验证 → 风险」)。

**红线**:本轮**仍然没有改任何生产代码**;改动只在测试文件与根目录报告文档内。

## 1. 总账体检(4 类账务更正)

这一节是调查的直接副产品:**总账的 DEFECT 桶有 4 类账务问题**。

### 1.1 幽灵条目(仓库中不存在该测试)

| 条目 | 问题 |
|---|---|
| `TestS9ReceptionFeedback` | 仓库中不存在该测试(是总账里未判定过的占位/聚合行,action 写着「并入 R05 产品缺陷」)。它与 TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical 指向同一根因(native_s9_original.go:80)。应从总账删除。 |

### 1.2 实际已绿(总账误记 DEFECT)

| 条目 | 说明 |
|---|---|
| `TestNativeS9MainCompletedReturnContinuesExactLiveDSource` | 已在 8144ba7 按 STALE 改写并通过(final4 实测 PASS,含全部子测试)。总账却仍记 DEFECT,应移出 DEFECT 桶。 |
| `TestNativeS9MainCompletedReturnContinuesOriginalSourceAfterCleanup` | 同上,final4 实测 PASS。应为 STALE(已改写通过),不是 DEFECT。 |
| `TestCheckProfilePrepareAndReplacementKeepExactProfile` | 当前 PASS(final4 实测);缺陷已被 commit 8144ba7 的共享断言吸收。总账应移出 DEFECT 桶。 |
| `TestMainCheckCapabilitiesObservationIsReadonlyAndPartial` | final4 实测 PASS,生产行为与蓝图一致;总账误记 DEFECT,应移出 DEFECT 桶。 |
| `TestMainCheckMissingToolRejectsPrepareBeforeReady` | final4 实测 PASS,生产行为与蓝图一致;总账误记 DEFECT,应移出 DEFECT 桶。 |
| `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity` | final4 实测 PASS,生产行为与蓝图一致;总账误记 DEFECT,应移出 DEFECT 桶。 |

### 1.3 应改判 STALE(蓝图用移除类措辞取消了测试前提)

| 条目 | 现判定 | 应改为 | 根因组 | 备注 | 本轮适配结果 |
|---|---|---|---|---|---|
| `TestMainCheckReuseObservationIsReadonlyAndCannotSkipRequiredChecks` | DEFECT | **STALE** | G11-D | 退役门禁 ⇒ 改写断言 | 已适配并转绿:复用决策不再按输入等价分级(L4-workflow-convergence.md:279),保留 action=run / would_reuse=false 与只读断言,定向 PASS |
| `TestNativeBuilderConventionsFixedReplayAndSourceDrift` | DEFECT | **STALE** | G03-B | 退役门禁 ⇒ 改写断言 | 已适配并转绿:来源变化不再是散列门(L4-workflow-convergence.md:250/:281),改为「可读即不拒收、原始冻结上下文不被重建」,定向 PASS |
| `TestNativeS9OriginalStopUsesNewProfessionalInputAfterSourceGap` | DEFECT | **STALE** | G02-B | 纯 STALE(改写后可绿) | 已适配并转绿:证据存在性门按 L3-S9-repair.md:236 退役,改为「缺证据来源不再拒收、但也不伪造该文件;已收事实不被后续编辑静默覆盖」,定向 PASS |
| `TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero` | DEFECT | **STALE** | G11-E | 退役门禁 ⇒ 改写断言 | 已适配并转绿:review.round 已退役(L4-state-transition-core.md:44/:48),改为「不拒收、历史值按原样保留、不制造」,定向 PASS |
| `TestCT03911_DualPassDocumentVerificationCommitsTR003` | DEFECT | **STALE** | G10-A | 纯 STALE(夹具过时;改用 MainPreToolUseBody 已实测通过);生产静默吞事件的缺陷独立存在 | 已适配并转绿:夹具改用 MainPreToolUseBody(root,…)(无 agent_id、有 cwd),定向 PASS |
| `TestCT03915_S11StopsAutoAdvance` | DEFECT | **STALE** | G10-A | 纯 STALE(夹具过时;改用 MainPreToolUseBody 已实测通过);生产静默吞事件的缺陷独立存在 | 已适配并转绿:同上,定向 PASS |
| `TestCT03924_SameAgentDualDVLabelsBlocksTR003` | DEFECT | **STALE** | G10-A | 纯 STALE(夹具过时;改用 MainPreToolUseBody 已实测通过);生产静默吞事件的缺陷独立存在 | 已适配并转绿:同上,定向 PASS |
| `TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate` | DEFECT | **STALE** | G06-D | 纯 STALE(改写后可绿) | 已适配并转绿:唯一作者路径语义,定向 PASS |
| `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat` | DEFECT | **STALE** | G06-D | 纯 STALE(改写后可绿) | 已适配并转绿:typed 副本不再是第二作者路径,改判后留反向断言(不得从 typed 副本恢复原消费者),定向 PASS |
| `TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears` | DEFECT | **STALE** | G06-E | 纯 STALE(改写后可绿) | 已适配并转绿:证据来源消失不再构成接收资格,定向 PASS |
| `TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns` | DEFECT | **STALE** | G06-E | 纯 STALE(改写后可绿) | 已适配并转绿:已收事实不被后续编辑静默覆盖(C02/L3-S9-repair.md:437),定向 PASS |
| `TestS9PreparationCallbackFollowsOriginalCAS` | DEFECT | **STALE** | G09-E | 退役门禁 ⇒ 改写断言 | 已适配并转绿:过期 revision 不再是拒收门(L4-revision-usage.md:141),改为「过期坐标不拒收、发行仍限定原责任坐标、已发行材料不二次准备」,定向 PASS |

### 1.3b 改判 STALE 但改写被阻塞(仍红)

| 条目 | 根因组 | 阻塞原因 |
|---|---|---|
| `TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain` | G01-C | 改写被阻塞:接收链已无非散列的可阻塞点(旧屏障依赖的 git 调用随散列门一并退役),需生产侧提供一个确定性的非散列屏障点;已在测试文件头如实标注,断言未削弱 |

### 1.4 需 owner 裁定(蓝图内部有反向条款)

| 条目 | 争议点 |
|---|---|

## 2. 根因聚合

### 2.1 真缺陷根因组

| 根因组 | 根因 | 最小共同生产位置 | 波及测试数 | 修复代价 |
|---|---|---|---|---|
| **G01-A** | 原生 S9 交付执行记录丢失源分支 → integration.Inspect 入口即拒 | `internal/cli/native_s9_original.go:169 → internal/integration/inspect_responsibility.go:20-22` | 11 | |
| **G02-A** | 原生 Result 路径反向选责用目录基名匹配完整 packet ID;长 ID 被分段后基名只是片段,恒不命中 | `internal/cli/native_s9_recovery.go:87(触发源 internal/repair/native_s9_private.go:13-23、:29;报错点 :115)` | 8 | |
| **G03-A** | Builder 回执的原领域投影被删成桩,checks_source 由 original_report 改成 main_checkpoint | `internal/qualitygate/builder_receipt.go:79-96(:93 硬编码 ChecksSource:"main_checkpoint")` | 6 | |
| **G10-A** | hook 入口静默吞掉无法归属的事件(非 Main/Worker),不报「未知」 | `internal/cli/hook_reader.go:208-210(hookReaderObservation: reader.Kind != "Worker" ⇒ return true, 0)` | 4 | |
| **G04-A** | 「已声明但为空」的集合被拷成 nil slice → context.json 里写成 null,违反生产 schema 的 array 要求 | `internal/cli/assignment_context.go:328-346(:331-332、:339)→ 自校验 :313` | 3 | |
| **G05-A** | projectWorkProgress 不再投影 milestone(missing→Gap、read→Sources)⇒ 停滞诊断永不可达 | `internal/cli/work_progress.go:224 → internal/hook/work_progress.go:195` | 3 | |
| **G07-A** | schema 层把「旧记录是否含历史散列字段」变成资格差异 | `internal/schema/assets/repair-workspace-candidate.schema.json:4 + internal/schema/assets/hypothesis-result-receipt.schema.json:7` | 2 | |
| **G06-A** | Result/PlanReport 专业字段级校验被放宽,原消费者不再对 null/缺失/未知字段产出字段错误 | `internal/workpacket/repair_result.go:295-341(:297 宽松解码 decodeOneJSON)+ internal/workpacket/repair_plan_report.go:271-281` | 2 | |
| **G12-C** | 非 Worker/teammate 载荷跳过原消费者,未签发路径得不到 fail-closed 收据 | `internal/cli/hook_reader.go:208(Kind != "Worker" 早退)` | 2 | |
| **G09-D** | 平台事件被提前短路 / 身份信息被丢弃,接收方拿不到准确对象 | `internal/cli/hook_reader.go:208-210(Kind 既非 Main 也非 Worker ⇒ return true, 0,吞掉整类 PreToolUse);internal/cli/native_review_hooks.go:555(只读 transcript,读不到就 return "")` | 2 | |
| **G08-A** | S7 Stop 接收被「已接收 ⇒ return true, 0」早退吞掉,TR-009 出口不触发 | `internal/cli/native_review_hooks.go:358-359` | 2 | |
| **G08-C** | S7 manifest 准备指引未给出原 register-workgroup 入口/方向不对 | `internal/cli/s7_guidance.go:127(只打印 s7 manifest-draft 命令)` | 2 | |
| **G10-B** | recovery apply 无条件执行 ValidateRestore,与仍存在的损坏 Runtime 比对而中止 | `internal/runtime/recovery.go:521-525` | 2 | |
| **G01-B** | 完成度判定忽略真实 unit_results[].status,只信顶层 verdict + 硬编码 pass | `internal/repair/issued_repair_result.go:696(硬编码 Result:"pass")→ :820-822 → internal/submission/repair_result.go:535-538 → internal/cli/native_main_return.go:604-609` | 1 | |
| **G04-B** | 已绑定重放路径只做 verifyFixedContext,不重新核验声明的原始来源 | `internal/cli/assignment_context_binding.go:253-259 + :81-161` | 1 | |
| **G05-B** | actor 可解析前就写 f.Owner/f.Binding,且 S9 专业阶段早退被删 ⇒ 未匹配 platform actor 被半已知覆盖 | `internal/cli/work_progress.go:230-235(原 d0d948a^:240-245 的 S9 早退被删)` | 1 | |
| **G07-B** | S9 接收路径 Result 侧在 CLI 层用 json.Valid 短路,绕过原消费者字段校验 | `internal/cli/native_s9_original.go:80` | 1 | |
| **G07-C** | S8 人工批准模式门被摘除,validateS8ApprovalMode 0 调用者 | `internal/investigation/hypothesis_result_receipt.go:257(定义)/ :363(实际执行的责任校验)` | 1 | |
| **G06-B** | 扁平私人文件字段反馈映射未接线,nativeS9FlatArtifactError 成为死代码 | `internal/cli/native_s9_artifact_feedback.go:18(无调用点)` | 1 | |
| **G06-C** | 诊断存储识别规则未随无散列诊断布局更新 | `internal/diagnosticstorage/storage.go:18(路径正则)+ :78(scope 长度 64)` | 1 | |
| **G12-A** | 权威根绑定校验在接收链丢失 | `internal/runtime/contract_authority.go:70(不再调用 internal/fileview/contracts.go:42 的 fileview.ValidateAuthority)` | 1 | |
| **G12-B** | 原题面来源校验在接收链丢失 | `internal/submission/source.go:150-156(不再调用 internal/workspace/document_review.go:107 的 ValidateDocumentReviewView)` | 1 | |
| **G12-D** | 状态索引 schema 严格性被放宽,顶层 additionalProperties 由 false 改成 true | `internal/schema/assets/review-result-packet-index.schema.json:6(同族 repair-result-packet-index.schema.json:6、repair-plan-report-packet-index.schema.json:6)` | 1 | |
| **G12-E** | 独立验证者派发准入集合被放宽,S9 lifecycle 守卫被删 | `internal/cli/repair_verifier_dispatch_questionnaire.go:30(新增 repairing/ready_for_full_review)+ :26(删掉 lifecycle.state == "bug_resolution" 守卫)` | 1 | |
| **G12-F** | 已回收的 worker worktree 上无条件执行 git,git 128 直接终止清理重试 | `internal/cli/workspace_delivery.go:150(workspace.Git(ctx, e.Path, "branch", "--show-current"))+ :152(return fail(err))` | 1 | |
| **G11-A** | PreToolUse 在 Runtime 不可读时被原生路由提前返回,跳过质量投影渲染 | `internal/cli/native_hook_routes.go:254(经 internal/cli/run.go:2658 → native_hook_routes.go:117)` | 1 | |
| **G11-C** | S9 阶段交接文案使用泛称 "Plan" 而非蓝图规范名 "PlanReport" | `internal/cli/controller.go:515(与 :554 的正常路径)` | 1 | |
| **G09-A** | TR-006 当前批次复检失去 S6 Main 起点校验,缺原 TR-003 共同 Main 基线时门被放行 | `internal/buildsurface/read.go:23(不再调用 loopruntime.RequireCurrentS6MainStartContext)` | 1 | |
| **G09-B** | 原生 S6 接收链缺少 planned_paths 的 Assignment 写域校验 | `internal/cli/native_s6.go:439(receiveNativeS6)→ internal/assignment/completion_canonical.go:626(只核身份绑定)` | 1 | |
| **G09-C** | 集成 Inspect 只把脏工作树当瞬时观察,不进 gate/checkpoint | `internal/integration/inspect_responsibility.go:16 + internal/integration/types.go:97-98` | 1 | |
| **G09-F** | 责任歧义未与「未知」分别报告,多重精确匹配被折叠 | `internal/cli/native_main_return.go:177(selected, unknown = nil, true)` | 1 | |
| **G08-B** | 原责任未完成/未知的 Stop 检查从 deny 降级为 warn | `internal/hook/stopidle.go:153(warn/original_responsibility_unfinished)、:678/:686/:694(warn/*_original_responsibility_unknown)` | 1 | |
| **G08-D** | 独立验证者在 Targeted 未就绪时就被派发 | `internal/cli/repair_verifier_dispatch_questionnaire.go:30(准入集合含 repairing/ready_for_full_review)+ :26(删掉 lifecycle.state == "bug_resolution" 守卫)` | 1 | |
| **G08-E** | 旧团队 PLAN_REPORT 收尾门未按蓝图删除,仍拦住原生 S9 Builder 的 TeammateIdle | `internal/hook/stopidle.go:598` | 1 | |
| **G13** | nativeS9ConsumerAttempt 的消费者阶段白名单未随重构改名更新,专业拒绝原因在后续前置失败时被丢弃 | `internal/cli/native_s9_attempt.go:67-70(阶段名白名单)+ internal/cli/native_s9_original.go:95(实际消费阶段名)` | 1 | |

### 2.2 组间共享的生产位置(可再合并)

| 生产位置 | 涉及根因组 |
|---|---|
| `internal/cli/hook_reader.go:190-196` | G09-D, G10-A, G12-C |
| `internal/cli/workspace_delivery.go:150` | G01-A, G12-F |
| `internal/cli/run.go:2693` | G10-A, G12-C |
| `internal/cli/hook_reader.go:208-210` | G09-D, G12-C |
| `internal/cli/native_review_hooks.go:308-310` | G08-A, G09-D |
| `internal/cli/repair_verifier_dispatch_questionnaire.go:25-31` | G08-D, G12-E |
| `internal/cli/repair_verifier_dispatch_questionnaire.go:26` | G08-D, G12-E |
| `internal/cli/repair_verifier_dispatch_questionnaire.go:30` | G08-D, G12-E |

### 2.3 横切发现(不归属单条测试的系统性削弱)

#### X-01 — 26 个 schema 资产的顶层 additionalProperties 由 false 改成 true(或删除)

- **根因**:d0d948a 把 102 个 schema 资产里 26 个的顶层 additionalProperties 从 false 放宽为 true(repair-plan.schema.json 则整个删掉)。这不是针对散列字段的定点放开,而是**整层 schema 严格性的系统性拆除**;连带还有 12 个 schema 的嵌套对象被放宽(全仓共 38 个 schema 文件的 additionalProperties 有任何变化)。
- **证据**:逐文件 git diff 9bf73ca..HEAD 确认:25 个 false→true + 1 个 false→删除。
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:15「无散列验收同时检查有效题面/指引、**schema**、所有自然事件的处理器及可达消费者……」
    - blueprint/l4-mechanisms/L4-stage-form.md:56「所有阶段的题面、schema、自然 Hook 及原消费者均不要求或预填散列……**机器字段格式不能拒绝可理解的本人原答卷**」
- **运行时/索引/回执类(严格性应有依据,无散列条款不覆盖)**:
    - `loop-event.schema.json`, `builder-receipt.schema.json`, `repair-contract.schema.json`, `repair-session.schema.json`, `repair-result-receipt.schema.json`, `repair-plan-report-receipt.schema.json`, `review-result-packet-index.schema.json`, `repair-result-packet-index.schema.json`, `repair-plan-report-packet-index.schema.json`, `finding.schema.json`, `finding-supplement.schema.json`, `review-result.schema.json`, `hook-decision.schema.json`, `repair-plan-report.schema.json`, `repair-result-blocker.schema.json`, `repair-result-candidate-provenance.schema.json`, `contract-work-packet.schema.json`, `design-work-packet.schema.json`, `hypothesis-result-work-packet.schema.json`
- **答卷/工作包类(可能被 L4-stage-form.md:56「机器字段格式不能拒绝可理解的本人原答卷」覆盖,需 owner 判定)**:
    - `work-packet.schema.json`, `stage-answer-s6-result.schema.json`, `review-plan.schema.json`, `review-investigation-case.schema.json`, `task-planning-work-packet.schema.json`, `team-manifest.schema.json`, `repair-plan.schema.json(顶层 additionalProperties 被整个删除)`
- **波及面**:26 个 schema 文件的顶层;另有 12 个 schema 的嵌套对象被放宽。**只有 1 条测试(TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute)碰到了它** —— 这类系统性削弱通常远不止一条测试能暴露。
- **备注**:Runtime state 根 schema(loop-state.schema.json / loop-state-v1.1~1.3)的**顶层** additionalProperties 未变,仍为 false;放宽集中在事件/回执/索引与答卷层。
- **判定**:横切发现(非单条测试)

### 2.4 根因族(更高一层的归并)

把根因组按**根因性质**再归并成族,便于决定修复批次:

| 根因族 | 涉及根因组 | 说明 |
|---|---|---|
| **接收链丢了原权威/原题面来源校验** | G12-A、G12-B、G03-B、G12-G | 校验函数**本身仍在**(如 `fileview.ValidateAuthority`、`ValidateDocumentReviewView`、解析器摘要核对),只是重构时把**调用点**删了 ⇒ 单点丢失,修复代价低、收益直接。 |
| **schema 被放宽成「什么都能过」** | G07-A、G12-D、G04-A、X-01 | schema 侧要么 `additionalProperties` 由 false 改 true,要么空集合被写成 null。这类缺陷会让**整条 schema 校验链失效**,波及面比单个测试大。 |
| **专业字段/完成度判定被放宽或硬编码** | G06-A、G06-B、G01-B、G12-E、G09-E | 原消费者不再产出字段级错误,或完成度只看顶层 verdict 而忽略 `unit_results[].status`。共同后果:**不完整的答卷被投影成「已完成」**。 |
| **原生 S9 的路径/坐标算不出来** | G01-A、G02-A、G12-F、G12-E | 长 ID 被分段后无法反推、源分支被写死、已回收 worktree 上硬跑 git。共同后果:**S9 原生交付链在入口就断**。 |
| **无散列改造留下的「识别规则没跟着改」** | G06-C、G07-A、G03-B | 路径/文件名/识别正则仍按散列时代写,新布局下匹配不上。 |
| **准入集合/守卫被放宽** | G11-E、G12-E、G11-B、G08-D、G09-A | 本该守住的阶段守卫被删或扩了集合。 |

## 3. 逐条调查(按根因组)

每条给出:蓝图要求(带原文行号) → 生产代码实际行为(带 `file:line`) → 根因。

### G01-A — 原生 S9 交付执行记录丢失源分支 → integration.Inspect 入口即拒

- **根因**:登记原生交付的 workspace.Execution 时把 Branch 写死成空串;父提交 9bf73ca:internal/cli/native_s9.go:1164-1167 用 `git branch --show-current` 解析真实分支、解析不到就如实报错,:1182 写 `Branch: branch`。HEAD 改成写死后,Inspect 对该责任一律返回 `assignment worktree and branch are required`。
- **最小共同生产位置**:`internal/cli/native_s9_original.go:169 → internal/integration/inspect_responsibility.go:20-22`
- **相关位置**:`internal/cli/native_s9_original.go:169`、`internal/integration/inspect_responsibility.go:20-22`、`internal/cli/workspace_delivery.go:150`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-worktree-governance.md:33「S6 集成器核对实际分支」
    - blueprint/l4-mechanisms/L4-worktree-governance.md:119「原诊断给出准确路径、分支和 accepted SourceHead」
    - blueprint/l3-stages/L3-S9-repair.md:263/:275
- **波及面**:11 条测试全部死在共享夹具 internal/cli/repair_integration_recovery_test.go:41-44,各自断言体从未执行。
- **修复方向**:恢复真实分支解析(并保留父提交『取不到就报错』的语义);分支应进入责任记录或可解析的持久事实,而非只在 CLI 内存中临时算一次。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS9HandoffTypedCandidateCannotLoseProvenanceOrSource` | G01-A |
| `TestS9HandoffFinalProjectionRetainsOriginalCleanupResponsibility` | G01-A |
| `TestS9HandoffTypedSelectorPreservesChangedAttemptFence` | G01-A |
| `TestS9HandoffDamagedTypedCandidateStopsBeforeMerge` | G01-A |
| `TestS9HandoffSameOwnerNeighborCannotSupplyOrBreakOriginalCandidate` | G01-A |
| `TestS9HandoffFinalReceiptSurvivesMissingPlanIndex` | G01-A |
| `TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection` | G01-A |
| `TestS9HandoffCanceledFinalAcknowledgementPreservesOriginalProof` | G01-A |
| `TestS9HandoffSpentProgressPreservesIndexesWithoutNewDocumentReads` | G01-A |
| `TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput` | G01-A |
| `TestNativeS9UnrecordedMergePreservesUnknownAndWorktree` | G01-A |

### G02-A — 原生 Result 路径反向选责用目录基名匹配完整 packet ID;长 ID 被分段后基名只是片段,恒不命中

- **根因**:NativeS9PacketPath 对 >200 字符的 ID 切成 p/<c1>/<c2>/<c3>,而 native_s9_recovery.go:87 用 filepath.Base(filepath.Dir(path)) 去比对完整 route.ID ⇒ 永不匹配 ⇒ 「native Result path has no exact current original Plan packet」。父提交 packet ID 是 64 位 sha256(无分段),基名反查当时成立 ⇒ d0d948a 引入的回归。
- **最小共同生产位置**:`internal/cli/native_s9_recovery.go:87(触发源 internal/repair/native_s9_private.go:13-23、:29;报错点 :115)`
- **相关位置**:`internal/cli/native_s9_recovery.go:87`、`internal/cli/native_s9_recovery.go:115`、`internal/repair/native_s9_private.go:13-23`、`internal/workpacket/repair_plan_report.go:203-204`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-agent-dispatch-governance.md:135/:145「不能只按 cwd、角色或同名文件猜选」
    - blueprint/l3-stages/L3-S9-repair.md:134
- **波及面**:8 条测试复现同一错误串。
- **修复方向**:native_s9_recovery.go:87 改为与 repair.NativeS9PacketPath(route.ID) 对比,或直接依赖已正确的后缀判定(:98-101);但 :87 的循环兼作多责任歧义检测(:109-110),改法需 owner 评估。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeS9OriginalFlatConsumeReceivesAndProjectsReplay` | G02-A |
| `TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan` | G02-A |
| `TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery` | G02-A |
| `TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat` | G02-A |
| `TestNativeS9PendingRecoveryReportsChangedPrivateOriginal` | G02-A |
| `TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat` | G02-A |
| `TestNativeS9OriginalConsumeAcceptedPlanSurvivesMissingDisplayBrief` | G02-A |
| `TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals` | G02-A |

### G03-A — Builder 回执的原领域投影被删成桩,checks_source 由 original_report 改成 main_checkpoint

- **根因**:d0d948a 删除了 legacy(非 canonical)Builder 回执的原领域投影,回执退化为 WaitingOn:[{Owner:main,Reason:assignment_unavailable}] 桩,DeliveryRecorded/Accepted/WorkComplete/DomainRefs 全空;checks_source 从父提交 9bf73ca:internal/qualitygate/builder_receipt.go:94 的 "original_report" 改成 "main_checkpoint",本人自检来源被 Main 检查点冒充。生产代码自身的契约注释 builder_receipt.go:22-23「Check results retain their original report source.」与 :93 直接冲突。
- **最小共同生产位置**:`internal/qualitygate/builder_receipt.go:79-96(:93 硬编码 ChecksSource:"main_checkpoint")`
- **相关位置**:`internal/qualitygate/builder_receipt.go:79-96`、`internal/qualitygate/builder_receipt.go:93`、`internal/qualitygate/builder_receipt.go:22-23`、`internal/qualitygate/canonical_builder_result.go:397`、`internal/cli/workspace_core_profile_test.go:456`、`internal/cli/workspace_core_profile_test.go:480`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-worktree-governance.md:174「Worker 按 TASK 完成真实自检……Main 的集成检查验证合并后的实际树;二者不能互相代替」
    - blueprint/l4-mechanisms/L4-worktree-governance.md:178「Worker 仍按真实 TASK 自检,Result 如实引用」
    - blueprint/l4-mechanisms/L4-worktree-governance.md:182「S6 Main 检查消费原 Record 及其 Main CheckProfiles」
    - blueprint/l3-stages/L3-S6-build.md:242-243/:246/:367
- **波及面**:6 条测试,7 个失败回执字面相同;各测试的断言体都在首个回执断言处就致命失败,后续断言不可达。
- **修复方向**:恢复 legacy 分支的原领域投影,并把 checks_source 恢复为本人自检来源;注意 builder_receipt.go:93 与 canonical_builder_result.go:397 共用同一字面量。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration` | G03-A |
| `TestActualMainIntegrationBarrierCleanupAfterEarlyReturn` | G03-A |
| `TestCheckProfileActualMainIntegrationCapturesOriginalResponsibility` | G03-A |
| `TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration` | G03-A |
| `TestConfiguredPressureOriginalReportWriterRegistrationAndActualMainIntegration` | G03-A |
| `TestBuilderReceiptOriginalFailureReworkAndCurrentResubmission` | G03-A |

### G10-A — hook 入口静默吞掉无法归属的事件(非 Main/Worker),不报「未知」

- **根因**:载荷带未注册的 agent_id 时,reader.Kind 既不是 Main 也不是 Worker,hookReaderObservation 直接 `return true, 0` —— 不写任何诊断,stdout 全空。该分支位于 Controller 之前(run.go:2693),对所有自然事件生效。
- **最小共同生产位置**:`internal/cli/hook_reader.go:208-210(hookReaderObservation: reader.Kind != "Worker" ⇒ return true, 0)`
- **相关位置**:`internal/cli/hook_reader.go:190-196`、`internal/cli/hook_reader.go:204-210`、`internal/cli/run.go:2693`、`internal/cli/run.go:2680-2691`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52(C03「缺字段指向本人实际路径与字段,未知就报告未知」)
    - blueprint/l4-mechanisms/L4-hook-platform-wiring.md:162
- **波及面**:跨组共享:4 条 CT-039 + G09 的 TestS6UnchangedPreToolUseReusesObservation + G12-C 的 2 条 = 7 条测试落在同一处。
- **修复方向**:无法归属的事件要产出「未知」诊断而不是静默返回;这是生产缺陷,独立于测试夹具。
- **判定**:DEFECT(生产:应报告未知)

| 测试 | 所属调查组 |
|---|---|
| `TestCT03911_DualPassDocumentVerificationCommitsTR003` | G10-A |
| `TestCT03915_S11StopsAutoAdvance` | G10-A |
| `TestCT03920_BuilderCompletionDurableRef` | G10-A |
| `TestCT03924_SameAgentDualDVLabelsBlocksTR003` | G10-A |

### G04-A — 「已声明但为空」的集合被拷成 nil slice → context.json 里写成 null,违反生产 schema 的 array 要求

- **根因**:copyContextProfiles / copyContextDispatch 用 append([]T(nil), ...) 拷空集,得到 nil slice;JSON 无 omitempty ⇒ 发布出的 context.json 把空集合写成 null,而生产 schema 要求 array ⇒ `runtime assignment-context prepare` 退出 1,上下文根本发不出去。仓库内已有正确写法可对照(internal/workspace/check_profile.go:29-30、internal/assignmentrecord/record.go:487 与 :1149-1153)。
- **最小共同生产位置**:`internal/cli/assignment_context.go:328-346(:331-332、:339)→ 自校验 :313`
- **相关位置**:`internal/cli/assignment_context.go:313`、`internal/cli/assignment_context.go:331-332`、`internal/cli/assignment_context.go:339`、`internal/checkexec/profile.go:32-33`、`internal/assignmentrecord/record.go:102`、`internal/schema/assets/native-assignment-context.schema.json:1004-1078`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S6-build.md:149「文件集合为空」「声明空的 runner 检查集」(语义层)
- **波及面**:3 条测试。
- **修复方向**:空集合输出为 [] 而非 null(或在 schema 侧接受 null)。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeProfileContextExposesFrozenOriginalProfile` | G04-A |
| `TestS6ClosingContractCLISeparatesOwnedCompletionFromMain` | G04-A |
| `TestS6NativeClosingContextKeepsAssessmentEditableAcrossRetry` | G04-A |

### G05-A — projectWorkProgress 不再投影 milestone(missing→Gap、read→Sources)⇒ 停滞诊断永不可达

- **根因**:自 d0d948a 起 projectWorkProgress 不投影当前 milestone,缺口事实恒缺 ⇒ hook 层 `facts.Gap != ""` 的产出条件永不成立,重复计数也无法累积。旁证:projectMilestoneReadSources 与 applyResponsibilityProgress 现均无生产调用方(只被测试引用)。
- **最小共同生产位置**:`internal/cli/work_progress.go:224 → internal/hook/work_progress.go:195`
- **相关位置**:`internal/cli/work_progress.go:224`、`internal/hook/work_progress.go:179-183`、`internal/hook/work_progress.go:195`、`internal/cli/work_progress_tools.go:84`、`internal/cli/work_progress.go:111`、`internal/cli/work_progress.go:24`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-procedure-reduction.md:193「重复读取同一范围、只查询状态而缺项不变,可以形成停滞提示」
    - blueprint/l4-mechanisms/L4-procedure-reduction.md:197「当前相同缺口的重复 Read／status 达到五次时可产生一次诊断」
    - blueprint/l4-mechanisms/L4-hook-anchor-catalog.md:148「未知、等待、已完成或读取范围不足不能被强判停滞」
- **波及面**:3 条测试;另有 internal/cli/work_progress_milestone_directory_test.go:109-115 期望同一投影,实测已红,同根因。
- **修复方向**:恢复 milestone 投影(缺口 → Gap、必要读源 → Sources)。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestMainOrdinaryReadAndNonCheckpointStatusProgressWire` | G05-A |
| `TestDefaultProgressShadowRecordsWithoutContext` | G05-A |
| `TestS5WorkerReadProgressAndReceivedPeerWaitWire` | G05-A |

### G07-A — schema 层把「旧记录是否含历史散列字段」变成资格差异

- **根因**:additionalProperties:false 拒绝历史散列字段,而蓝图要求保留历史原字段(只禁止携带 digest 值)。
- **最小共同生产位置**:`internal/schema/assets/repair-workspace-candidate.schema.json:4 + internal/schema/assets/hypothesis-result-receipt.schema.json:7`
- **相关位置**:`internal/schema/assets/repair-workspace-candidate.schema.json:4`、`internal/schema/assets/hypothesis-result-receipt.schema.json:7`、`internal/runtime/repair_result_checkpoint.go:70`、`internal/runtime/hypothesis_result_checkpoint.go:118`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:15
    - blueprint/l4-mechanisms/L4-worktree-governance.md:281/:285
- **波及面**:2 条测试(含 INV 桶 1 条)。
- **修复方向**:放宽 additionalProperties 或显式列出旧字段;修复面影响其他 schema 消费者,需做影响面分析。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeS9OriginalCandidateReadsHistoricalMachineValues` | G07-A |
| `TestOriginalHypothesisReceiptIgnoresHistoricalMachineFields` | G07-A |

### G06-A — Result/PlanReport 专业字段级校验被放宽,原消费者不再对 null/缺失/未知字段产出字段错误

- **根因**:九个专业字段「必须显式存在且非 null」的检查被删,改用无 DisallowUnknownFields 的宽松解码,JSON null 被静默解入 string 变 "" ⇒ migration_ref: null 被当成 "",接收成功且诊断里没有字段错误。
- **最小共同生产位置**:`internal/workpacket/repair_result.go:295-341(:297 宽松解码 decodeOneJSON)+ internal/workpacket/repair_plan_report.go:271-281`
- **相关位置**:`internal/workpacket/repair_result.go:295-341`、`internal/workpacket/repair_result.go:297`、`internal/workpacket/repair_plan_report.go:271-281`、`internal/workpacket/review_result.go:528`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:238「字段缺失时保留原稿,反馈实际文件与字段」
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52/:53
    - blueprint/l3-stages/L3-S9-repair.md:198「不适用时可为空字符串」
    - blueprint/l3-stages/L3-S9-repair.md:171「red_checks pass|fail|blocked」
- **波及面**:2 条测试。修时注意 :198 允许 migration_ref 为空字符串 ⇒ 只补「声明为 string 的字段不得为 null/缺失」,不要恢复九字段全必填。
- **修复方向**:恢复直接结构检查(声明为 string 的字段不得为 null/缺失),不必恢复「九字段全必填」。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeS9PublicAuthorCorrectsRejectedResult` | G06-A |
| `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields` | G06-A |

### G12-C — 非 Worker/teammate 载荷跳过原消费者,未签发路径得不到 fail-closed 收据

- **根因**:载荷带 teammate_name ⇒ 判为 nested ⇒ Kind="Helper" ⇒ hookReaderObservation 直接返回 (true, 0),原消费者未被调用。父提交**也已失败** ⇒ 既有缺陷,非本次重构引入。
- **最小共同生产位置**:`internal/cli/hook_reader.go:208(Kind != "Worker" 早退)`
- **相关位置**:`internal/cli/hook_reader.go:76`、`internal/cli/hook_reader.go:190-196`、`internal/cli/hook_reader.go:208-210`、`internal/cli/stage_answer.go:336-338`、`internal/cli/stage_answer.go:736`、`internal/cli/run.go:2693`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-runtime-control-plane.md:237「S7～S9 原生 Worker 的 PreToolUse 监听当前本人原卷……均调用同一适用原消费者」
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52(C03)
- **波及面**:2 条测试。
- **修复方向**:让 teammate/nested 载荷也进入原消费者路由,并如实报告未知。
- **判定**:DEFECT(既有缺陷,非本轮重构引入)

| 测试 | 所属调查组 |
|---|---|
| `TestSavedHypothesisResultHookRejectsUnissuedPathWithoutRuntimeWrite` | G12-C |
| `TestSavedHypothesisResultHookDoesNotTreatUnissuedTeammatePathAsCurrent` | G12-C |

### G09-D — 平台事件被提前短路 / 身份信息被丢弃,接收方拿不到准确对象

- **根因**:(a) 非 Main/Worker 的 PreToolUse 被整体短路 ⇒ S6 观察/委派预检里程碑从未写入;(b) S7/S8 原生 Stop 的责任识别只依赖平台 transcript,缺 agent_transcript_path 时识别不出责任并静默返回 ⇒ 未接收的责任没有交 Main(实测 Stop code 0、诊断为空)。
- **最小共同生产位置**:`internal/cli/hook_reader.go:208-210(Kind 既非 Main 也非 Worker ⇒ return true, 0,吞掉整类 PreToolUse);internal/cli/native_review_hooks.go:555(只读 transcript,读不到就 return "")`
- **相关位置**:`internal/cli/hook_reader.go:208-210`、`internal/cli/hook_reader.go:190-196`、`internal/cli/run.go:2703`、`internal/cli/controller.go:340-342`、`internal/cli/controller.go:827`、`internal/cli/native_review_hooks.go:553`、`internal/cli/native_review_hooks.go:555`、`internal/cli/native_review_hooks.go:308-310`、`internal/cli/native_s8_responsibility.go:396`、`internal/cli/native_feedback.go:65`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-agent-guidance.md:279/:288
    - blueprint/l4-mechanisms/L4-agent-dispatch-governance.md:167「保留原诊断、准确对象与已有产物;接收失败不能冒充完成」
    - blueprint/l4-mechanisms/L4-runtime-control-plane.md:237
- **波及面**:2 条测试。**同一生产位置 internal/cli/hook_reader.go:208 也导致 G10 的 TestCT03911 失败** ⇒ 跨组共享位置,应合并为一个修复点。
- **修复方向**:(a) 不要整体吞掉非 Main/Worker 的 PreToolUse;(b) 保留平台回退文本源,让责任识别失败时如实交 Main。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS6UnchangedPreToolUseReusesObservation` | G09-D |
| `TestNativeMainS7ReturnKeepsSubmissionDiagnosticWhenOwnBodyFails` | G09-D |

### G08-A — S7 Stop 接收被「已接收 ⇒ return true, 0」早退吞掉,TR-009 出口不触发

- **根因**:新增早退 `if stage != "S7" || !s7SuccessorPending(root, snap.State) { return true, 0 }` 在 S7 接收与 TR-009 提升之前就把事件吞掉,原消费者诊断永不产生;父提交没有这个早退,直接 service.SaveReviewResultFrom 并打印「S7 native professional answer received by the original consumer.」。
- **最小共同生产位置**:`internal/cli/native_review_hooks.go:358-359`
- **相关位置**:`internal/cli/native_review_hooks.go:358-359`、`internal/cli/native_review_hooks.go:308-310`、`internal/cli/native_review_hooks.go:396`、`internal/cli/s7_handoff.go:165`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:437「S7 出口…适用必要责任完成且无 Finding 时经 TR-009 准备 S10」
    - blueprint/l3-stages/L3-S7-verification-round.md:384「…沿 TR-009 自动进入 S10」
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:225「S7/S8/S9 共用 Pre 观察、成功 Post 和 Stop 兜底的接收语义…返回接收或具体缺项」
- **波及面**:2 条测试(实测 stdout/stderr 全空)。既有审计 TEST-GAP-AUDIT-SEAMS.md Table 7 #1 已记同一根因,判 HIGH,父提交 PASS。
- **修复方向**:把早退改成「不重复接收,但仍输出原消费者诊断与出口准备」,不要静默 return。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound` | G08-A |
| `TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound` | G08-A |

### G08-C — S7 manifest 准备指引未给出原 register-workgroup 入口/方向不对

- **根因**:S7 指引只产出 `s7 manifest-draft` 一条命令,没有把「登记工作组」的原入口 `runtime register-workgroup` 带出来;而生产侧 review/submit.go:774/:782 的拒绝信息正是指向该入口 ⇒ 指引与消费者要求对不上。
- **最小共同生产位置**:`internal/cli/s7_guidance.go:127(只打印 s7 manifest-draft 命令)`
- **相关位置**:`internal/cli/s7_guidance.go:127`、`internal/cli/s7_manifest_draft.go:1`、`internal/cli/s7_status.go:43`、`internal/cli/s7_status.go:52`、`internal/review/manifest_draft.go:179`、`internal/review/submit.go:774`、`internal/review/submit.go:782`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S7-verification-round.md:205「系统为已声明且已解锁的原 Assignment 提供准确的 manifest 准备路径和原 `register-workgroup` 入口」
- **波及面**:2 条测试。
- **修复方向**:S7 指引同时给出 manifest 准备路径与原 register-workgroup 入口。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS7DispatchGuidanceUsesActualPrerequisitesAndSource` | G08-C |
| `TestS7ExistingEditedManifestRecoversRegistrationWithoutOverwrite` | G08-C |

### G10-B — recovery apply 无条件执行 ValidateRestore,与仍存在的损坏 Runtime 比对而中止

- **根因**:HEAD 把 ValidateRestore 提到 `if request.ValidateRestore != nil` 下无条件执行;父提交 9bf73ca 把它放在 `if hasIssuedIndices(state)` 分支内 —— 损坏 Runtime 先被隔离,比对被跳过。HEAD 拿恢复候选与仍然损坏的活动 Runtime 比对 ⇒ 报 LOOP_RECOVERY_INPUT_DRIFT,「恢复损坏 Runtime」这个首要场景直接不可用。
- **最小共同生产位置**:`internal/runtime/recovery.go:521-525`
- **相关位置**:`internal/runtime/recovery.go:521-525`、`internal/cli/recovery_checkpoint.go:180`、`internal/runtime/checkpoint.go:550`、`internal/runtime/recovery.go:648`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-runtime-control-plane.md:72「checkpoint 恢复和具名历史继续按故障模型检查实际保存材料、版本和事务配对…恢复承诺实际需要的材料缺失或冲突时,保留准确系统故障,不跳过或改用相似对象」
    - blueprint/l4-mechanisms/L4-state-transition-core.md:196
- **波及面**:2 条测试。既有审计 TEST-GAP-AUDIT-SEAMS.md Table 7 #2 / TEST-GAP-AUDIT.md H11 已记同一根因,判 HIGH,父提交 PASS;该报告还记录了「把 HEAD 的调用重新门控到 hasIssuedIndices 就能让 TestRuntimeRecoveryApplySamePlanIsIdempotent 通过(已在一次性克隆里验证过)」。
- **修复方向**:把 ValidateRestore 重新门控到 hasIssuedIndices 分支内(审计已验证可行)。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestRuntimeRecoveryApplyNeverRevivesActiveAgents` | G10-B |
| `TestRuntimeRecoveryApplySamePlanIsIdempotent` | G10-B |

### G01-B — 完成度判定忽略真实 unit_results[].status,只信顶层 verdict + 硬编码 pass

- **根因**:候选提案里 Result 被写死为 "pass",于是永远判定 WorkComplete=true,部分完成的答卷被投影成「Main 集成义务」;原本会拒绝非 pass 单元的守卫 ensureRepairResultCompleted(issued_repair_result.go:566-580)在 d0d948a 中已无调用点,成为死代码。
- **最小共同生产位置**:`internal/repair/issued_repair_result.go:696(硬编码 Result:"pass")→ :820-822 → internal/submission/repair_result.go:535-538 → internal/cli/native_main_return.go:604-609`
- **相关位置**:`internal/repair/issued_repair_result.go:566-580`、`internal/repair/issued_repair_result.go:696`、`internal/repair/issued_repair_result.go:820`、`internal/cli/s9_handoff_projection.go:342-351`、`internal/submission/repair_result.go:535-538`、`internal/cli/native_main_return.go:604-609`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:238「正常保存、工具成功、Stop 或 completed 字样本身不等于完成」
    - blueprint/l3-stages/L3-S9-repair.md:195「unit_results | 原 unit ID、真实 pass/fail/blocked」
- **波及面**:1 条测试(独有根因)。
- **修复方向**:完成度必须读 unit_results[].status,而不是硬编码 pass;恢复守卫调用点。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS9HandoffPartialCandidateDoesNotBecomeIntegrationDuty` | G01-B |

### G04-B — 已绑定重放路径只做 verifyFixedContext,不重新核验声明的原始来源

- **根因**:重放路径缺源时仍报 ready,没有重新核验 c.Inputs 里声明的题面/指引来源。
- **最小共同生产位置**:`internal/cli/assignment_context_binding.go:253-259 + :81-161`
- **相关位置**:`internal/cli/assignment_context_binding.go:81-161`、`internal/cli/assignment_context_binding.go:253-259`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-worktree-governance.md(来源核验要求)
- **波及面**:1 条测试。
- **修复方向**:重放前重新核验声明来源。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite` | G04-B |

### G05-B — actor 可解析前就写 f.Owner/f.Binding,且 S9 专业阶段早退被删 ⇒ 未匹配 platform actor 被半已知覆盖

- **根因**:半已知状态被写出并落进度缓存,Main 分支与 lifecycle 无关 ⇒ S9 态下也写出缓存。
- **最小共同生产位置**:`internal/cli/work_progress.go:230-235(原 d0d948a^:240-245 的 S9 早退被删)`
- **相关位置**:`internal/cli/work_progress.go:230-235`、`internal/cli/work_progress.go:236-240`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-procedure-reduction.md:195「它只在实际支持的成功 PostToolUse 与可解析责任上工作」
- **波及面**:1 条测试(第 27 行先失败,该断言未执行到,系推断)。
- **修复方向**:恢复 actor 解析前的早退与「半已知不写缓存」语义。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestProgressPacketS9SkipsLegacyCoverageWithoutCache` | G05-B |

### G07-B — S9 接收路径 Result 侧在 CLI 层用 json.Valid 短路,绕过原消费者字段校验

- **根因**:Plan 侧仍走 ValidateRepairPlanReportAnswer → author 错误,Result 侧却用 `resultErr == nil && !json.Valid(raw)` 直接走 pendingResult(),两侧不对称 ⇒ 作者写坏 JSON 只收到「未提交或未完成」,拿不到蓝图要求的「哪个文件哪一段 JSON 非法」。
- **最小共同生产位置**:`internal/cli/native_s9_original.go:80`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:238「文件不存在、草稿未就绪或字段缺失时保留原稿,反馈实际文件与字段」
    - :240 免拒收清单只含 red_checks、证据引用、proposed_paths
- **波及面**:1 条测试的 /author 子测试(与 /system 子测试混合)。
- **修复方向**:Result 侧接入与 Plan 侧对称的字段校验与错误分类。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical` | G07-B |

### G07-C — S8 人工批准模式门被摘除,validateS8ApprovalMode 0 调用者

- **根因**:接收路径只做责任归属校验,不再校验人工批准模式 ⇒ 未获批准的 plan_approval_required Investigator 答卷被接受。
- **最小共同生产位置**:`internal/investigation/hypothesis_result_receipt.go:257(定义)/ :363(实际执行的责任校验)`
- **相关位置**:`internal/investigation/hypothesis_result_receipt.go:257`、`internal/investigation/hypothesis_result_receipt.go:363`、`internal/investigation/hypothesis_result_receipt.go:41`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S8-investigation.md(人工批准门要求)
- **波及面**:1 条测试。这是本轮新发现的、审计编号之外的缺陷。
- **修复方向**:在 ReceiveIssuedHypothesisResult 链上接入 validateS8ApprovalMode。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates` | G07-C |

### G06-B — 扁平私人文件字段反馈映射未接线,nativeS9FlatArtifactError 成为死代码

- **根因**:d0d948a 删掉了 Result/PlanReport 两处调用点,字段错误原样返回 ⇒ 旧信封指针「/answer」与「填写 answer」文案照旧泄漏,缺少私人文件路径与字段指针。
- **最小共同生产位置**:`internal/cli/native_s9_artifact_feedback.go:18(无调用点)`
- **相关位置**:`internal/cli/native_s9_artifact_feedback.go:18`、`internal/cli/native_s9_artifact_feedback.go:32`、`internal/cli/native_s9.go:514`、`internal/cli/native_s9_original.go:110`、`internal/cli/native_feedback.go:163`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52「缺字段指向本人实际路径与字段」
    - blueprint/l4-mechanisms/L4-agent-dispatch-governance.md:167「原字段错误沿接收器传递」
- **波及面**:1 条测试(与 G06-A 叠加)。
- **修复方向**:在 internal/cli/native_s9.go:514 与 internal/cli/native_s9_original.go:110 重新接上 nativeS9FlatArtifactError。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields` | G06-B |

### G06-C — 诊断存储识别规则未随无散列诊断布局更新

- **根因**:诊断路径改成 .claude/diagnostics/native-s9/responsibility/<base64 分段>/observation.json 且 scope 改成坐标 JSON 数组,但 storage 的识别正则仍要求 64 位十六进制文件名且 scope 长度 64 ⇒ diagnosticstorage.Recognizes 对真实诊断文件返回 false。
- **最小共同生产位置**:`internal/diagnosticstorage/storage.go:18(路径正则)+ :78(scope 长度 64)`
- **相关位置**:`internal/diagnosticstorage/storage.go:18`、`internal/diagnosticstorage/storage.go:22-28`、`internal/diagnosticstorage/storage.go:29-31`、`internal/diagnosticstorage/storage.go:78`、`internal/cli/native_s9_diagnostic.go:45`、`internal/cli/native_s9_attempt.go:268-273`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:161「诊断存储识别同步核对」
    - blueprint/l3-stages/L3-S9-repair.md:493
- **波及面**:1 条测试。
- **修复方向**:同步 diagnosticstorage 的路径/scope 识别规则与新的无散列诊断布局。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication` | G06-C |

### G12-A — 权威根绑定校验在接收链丢失

- **根因**:ValidateContractPacket 只剩 CurrentContractBinding + samePlanningResponsibility;files 形参完全未使用,是删改残留 ⇒ 任意 root 都能通过 S3 契约包校验。父提交实测 PASS ⇒ d0d948a 回归。
- **最小共同生产位置**:`internal/runtime/contract_authority.go:70(不再调用 internal/fileview/contracts.go:42 的 fileview.ValidateAuthority)`
- **相关位置**:`internal/runtime/contract_authority.go:70-79`、`internal/fileview/contracts.go:42`、`internal/fileview/contracts.go:57-59`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-state-transition-core.md:31「不能复制一对 Runtime 文件就成为第二个控制面」
    - blueprint/l4-mechanisms/L4-state-transition-core.md:339
- **波及面**:1 条测试。审计编号 H5。
- **修复方向**:在 ValidateContractPacket 恢复 fileview.ValidateAuthority 调用。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestContractAuthorityUsesOriginalS2RegisteredDesign` | G12-A |

### G12-B — 原题面来源校验在接收链丢失

- **根因**:!accepted 分支只核对 lifecycle.state/phase_revision,不再比对 Worker view 与注册 packet/execution 的字节 ⇒ 被替换成非题面 JSON 的 Worker view 仍被当作有效来源接收。父提交实测 PASS ⇒ 回归。
- **最小共同生产位置**:`internal/submission/source.go:150-156(不再调用 internal/workspace/document_review.go:107 的 ValidateDocumentReviewView)`
- **相关位置**:`internal/submission/source.go:125-158`、`internal/submission/source.go:150-156`、`internal/workspace/document_review.go:107`、`internal/workspace/document_review.go:120-126`、`internal/cli/workspace_answer.go:64`、`internal/cli/workspace_launch.go:85`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:15「无散列验收同时检查有效题面/指引、schema……」
- **波及面**:1 条测试(view 子例失效;owner/symlink/old entry 子例仍正确拒收;frozen_input 子例按散列判据应为 STALE,需 owner 裁定是否拆分)。
- **修复方向**:恢复 ValidateDocumentReviewView 调用;实现时按「题面/schema 有效性」而非「字节相等」。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestIndependentReviewRejectsCorruptSourceWithoutReception` | G12-B |

### G12-D — 状态索引 schema 严格性被放宽,顶层 additionalProperties 由 false 改成 true

- **根因**:非法字段 title 因此通过 schema 校验,Snapshot() 返回 nil,状态索引非法字段不再被拒。父提交实测 PASS ⇒ 回归。
- **最小共同生产位置**:`internal/schema/assets/review-result-packet-index.schema.json:6(同族 repair-result-packet-index.schema.json:6、repair-plan-report-packet-index.schema.json:6)`
- **相关位置**:`internal/schema/assets/review-result-packet-index.schema.json:6`、`internal/runtime/packet_body.go:189`、`internal/runtime/packet_body.go:384`、`internal/runtime/packet_body.go:404`、`internal/runtime/store.go:1705`、`internal/runtime/store.go:1713`、`internal/runtime/store.go:1726`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:15「……schema……」
- **波及面**:1 条测试;同族另外两个 schema 有相同改动。
- **修复方向**:恢复三个 *-packet-index.schema.json 的 additionalProperties:false。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute` | G12-D |

### G12-E — 独立验证者派发准入集合被放宽,S9 lifecycle 守卫被删

- **根因**:非 S9 阶段(S8 investigation)、或 Builder 实施/影响处置/已就绪全审阶段,仍出现 Main 的独立验证者派发入口。父提交实测 PASS ⇒ 回归。
- **最小共同生产位置**:`internal/cli/repair_verifier_dispatch_questionnaire.go:30(新增 repairing/ready_for_full_review)+ :26(删掉 lifecycle.state == "bug_resolution" 守卫)`
- **相关位置**:`internal/cli/repair_verifier_dispatch_questionnaire.go:25-31`、`internal/cli/repair_verifier_dispatch_questionnaire.go:26`、`internal/cli/repair_verifier_dispatch_questionnaire.go:30`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:72「独立复验与 S7 后继」
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:53(C04)
- **波及面**:1 条测试。
- **修复方向**:恢复 lifecycle 守卫并把准入集合收回 blueprint 规定的阶段。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS9RepairVerifierDispatchQuestionnaireOriginalPhasesAndResponsibility` | G12-E |

### G12-F — 已回收的 worker worktree 上无条件执行 git,git 128 直接终止清理重试

- **根因**:worktree 已被回收 ⇒ git 报 128「cannot change to ... No such file or directory」⇒ 检查点无法从 StateCleanupPending 续到 StateComplete,已回收 worktree 堵住后继。父提交实测 PASS ⇒ 回归。
- **最小共同生产位置**:`internal/cli/workspace_delivery.go:150(workspace.Git(ctx, e.Path, "branch", "--show-current"))+ :152(return fail(err))`
- **相关位置**:`internal/cli/workspace_delivery.go:138-155`、`internal/cli/workspace_delivery.go:150`、`internal/cli/workspace_delivery.go:152`、`internal/workspace/workspace.go:124`、`internal/workspace/workspace.go:152`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:212「工作区清理单独处理,不撤销接收或堵住后继」
    - blueprint/l4-mechanisms/L4-worktree-governance.md:115「worktree 已回收不撤销已接收专业事实」
- **波及面**:1 条测试。
- **修复方向**:清理重试路径要能容忍 checkout 已不存在(按 workspace.go:124 的原始错误恢复原检查点)。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint` | G12-F |

### G11-A — PreToolUse 在 Runtime 不可读时被原生路由提前返回,跳过质量投影渲染

- **根因**:Runtime 读不到时 nativeHookRoutes 提前返回,只写一条纯文本 additionalContext/systemMessage,不经过唯一产出 QUALITY_GATE 行的渲染器 internal/hook/pretooluse.go:43 ⇒ 质量投影与恢复包从未输出。
- **最小共同生产位置**:`internal/cli/native_hook_routes.go:254(经 internal/cli/run.go:2658 → native_hook_routes.go:117)`
- **相关位置**:`internal/cli/native_hook_routes.go:254-261`、`internal/cli/native_hook_routes.go:117`、`internal/cli/run.go:2658-2660`、`internal/cli/run.go:3059-3064`、`internal/cli/run.go:3063`、`internal/hook/pretooluse.go:38-45`、`internal/hook/pretooluse.go:43`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-platform-wiring.md:391「适用的质量投影与原 Hook 输出共同来自相应控制结果」
    - blueprint/l4-mechanisms/L4-hook-platform-wiring.md:184(§7.2 质量投影保留原 status/候选/观察 revision/缺项/冲突/transition_committed)
- **波及面**:1 条测试。注:与仓库根 TEST-GAP-AUDIT-FAILURES.md 的 R01 组症状相同(PreToolUse 缺 QUALITY_GATE)但落点不同(R01 记 internal/cli/run.go:2961 的 !graphObserved 短路),是否并组需父级判定。
- **修复方向**:原生快照失败路径也要走质量投影渲染,产出 QUALITY_GATE 行与恢复指引。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible` | G11-A |

### G11-C — S9 阶段交接文案使用泛称 "Plan" 而非蓝图规范名 "PlanReport"

- **根因**:合同与交接本身都已落盘,仅交接文案不含规范名,导致 `runtime investigation contract approve` 的 stderr 不含 PlanReport。
- **最小共同生产位置**:`internal/cli/controller.go:515(与 :554 的正常路径)`
- **相关位置**:`internal/cli/controller.go:514-515`、`internal/cli/controller.go:552`、`internal/cli/controller.go:554`、`internal/cli/controller.go:558`、`internal/cli/controller.go:573`、`internal/cli/investigation_command.go:1087`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S8-finding-investigation.md:410「原 S9 合同批准同时保存权威合同与阶段交接」
    - blueprint/l3-stages/L3-S9-repair.md:70
    - blueprint/l4-mechanisms/L4-agent-dispatch-governance.md:115
- **波及面**:1 条测试。
- **修复方向**:交接文案改用规范名 PlanReport。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestRuntimeInvestigationContractApproveCLIHandsOffToS9` | G11-C |

### G09-A — TR-006 当前批次复检失去 S6 Main 起点校验,缺原 TR-003 共同 Main 基线时门被放行

- **根因**:父提交 internal/buildsurface/read.go:57 有 RequireCurrentS6MainStartContext 调用,HEAD 删掉 ⇒ 严格边界的唯一来源(controlproof/s6_main_baseline.go:196「no original TR-003 common Main baseline」)在生产代码中已无调用者。实测 TR-006 直接提交、S7 ReviewPlan 已生成。
- **最小共同生产位置**:`internal/buildsurface/read.go:23(不再调用 loopruntime.RequireCurrentS6MainStartContext)`
- **相关位置**:`internal/buildsurface/read.go:23`、`internal/buildsurface/read.go:57`、`internal/transition/engine.go:509`、`internal/controlproof/s6_main_baseline.go:176`、`internal/controlproof/s6_main_baseline.go:196`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S6-build.md:46「正常入口为 building,前提是 TR-003 已实际提交」
    - blueprint/l4-mechanisms/L4-worktree-governance.md:117「canonical S6 首次集成仍要求真实 source 提交」
- **波及面**:1 条测试。
- **修复方向**:恢复 S6 Main 起点校验,并让 controlproof 的严格边界有生产调用点。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS6IntegrationWithoutMainStartKeepsOriginalBatchGate` | G09-A |

### G09-B — 原生 S6 接收链缺少 planned_paths 的 Assignment 写域校验

- **根因**:范围校验只存在于 internal/plancheckpoint/validate.go:227「S6 PLAN path %s is outside the canonical Assignment」,而它只由 readback_submitted/plan_report 事件与 cli/run.go:3328、hookctx/loader.go:592 调用,原生 Stop 完成路径不经过 ⇒ 越界 Plan 被接受(实测 Stop 返回 code 0)。
- **最小共同生产位置**:`internal/cli/native_s6.go:439(receiveNativeS6)→ internal/assignment/completion_canonical.go:626(只核身份绑定)`
- **相关位置**:`internal/cli/native_s6.go:439`、`internal/cli/native_s6.go:502`、`internal/assignment/completion_canonical.go:626`、`internal/plancheckpoint/validate.go:211-228`、`internal/plancheckpoint/validate.go:227`、`internal/assignment/lifecycle.go:332`、`internal/assignment/lifecycle.go:354`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S6-build.md:188「planned_paths 必须落在登记范围,不能扩张 TASK」
    - blueprint/l3-stages/L3-S6-build.md:226「它能拒绝范围、关联和完成条件映射缺项」
- **波及面**:1 条测试。
- **修复方向**:原生接收路径也要跑写域校验(把 plancheckpoint 的校验接到 native_s6 链上)。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestTaskScopeOriginalS4SaveThroughNativeAssignmentAndBrief` | G09-B |

### G09-C — 集成 Inspect 只把脏工作树当瞬时观察,不进 gate/checkpoint

- **根因**:ErrDirtyWorktree 只在 Main checkout 与清理阶段产生;checkpoint 的 Blockers 只由 scope/locked/target/check 失败填充 ⇒ 脏 Builder 工作树永远不可能产生 dirty blocker(实测 state=preserved 但 Blockers 空、FailureReason 是 check failed exit 127)。
- **最小共同生产位置**:`internal/integration/inspect_responsibility.go:16 + internal/integration/types.go:97-98`
- **相关位置**:`internal/integration/inspect_responsibility.go:16`、`internal/integration/inspect_responsibility.go:36`、`internal/integration/inspect_responsibility.go:97`、`internal/integration/inspect_responsibility.go:138`、`internal/integration/types.go:97-98`、`internal/integration/closing_contract.go:62`、`internal/integration/integrate.go:61`、`internal/integration/integrate_responsibility.go:380`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S6-build.md:352「Inspect …工作区状态…」
    - blueprint/l3-stages/L3-S6-build.md:454「是 dirty、冲突、越界、命令失败,还是已合并待恢复」
    - blueprint/l4-mechanisms/L4-worktree-governance.md:168「冲突与未提交修改保留现场」
- **波及面**:1 条测试。
- **修复方向**:把工作区脏状态纳入 Inspect 的 gate/checkpoint 条件。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestCanonicalS6WorkspaceReworkRequiresNamedAssignmentRevision` | G09-C |

### G09-F — 责任歧义未与「未知」分别报告,多重精确匹配被折叠

- **根因**:len(selected)>1 且含非 S7/S8 类责任时,把所有精确匹配清空并归入 unknown ⇒ 歧义类别被抹掉(实测输出只有通用提示,无「multiple exact」)。
- **最小共同生产位置**:`internal/cli/native_main_return.go:177(selected, unknown = nil, true)`
- **相关位置**:`internal/cli/native_main_return.go:174-179`、`internal/cli/native_main_return.go:215-216`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-agent-guidance.md:297「准确责任与未知/多重引用分别覆盖」
    - blueprint/l4-mechanisms/L4-agent-dispatch-governance.md:131「不能按路径出现顺序猜选」
- **波及面**:1 条测试。
- **修复方向**:把「多重精确引用」与「未知」分别表达。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeMainReturnReadsS5ReceiptAcrossStageWithoutReceivingAnswers` | G09-F |

### G08-B — 原责任未完成/未知的 Stop 检查从 deny 降级为 warn

- **根因**:git diff 9bf73ca..HEAD 显示三处 `Decision: "deny"` 被换成 `Decision: "warn"`。isDenyingHookDecision(internal/cli/run.go:3733) 只认 deny/block ⇒ 原责任未完成不再阻止事件结束,只提示。父提交 stopidle.go 只有 4 处 deny、没有 warn。
- **最小共同生产位置**:`internal/hook/stopidle.go:153(warn/original_responsibility_unfinished)、:678/:686/:694(warn/*_original_responsibility_unknown)`
- **相关位置**:`internal/hook/stopidle.go:153`、`internal/hook/stopidle.go:139`、`internal/hook/stopidle.go:678`、`internal/hook/stopidle.go:686`、`internal/hook/stopidle.go:694`、`internal/cli/run.go:3733`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:225「…返回接收或具体缺项」
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52(C03)
- **波及面**:1 条测试(2 个子用例 SubagentStop/TeammateIdle)。
- **修复方向**:恢复 deny 语义;warn 不足以表达「原责任未完成,不得收尾」。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility` | G08-B |

### G08-D — 独立验证者在 Targeted 未就绪时就被派发

- **根因**:实测 hook 直接输出「Registration is complete; Main arranges actual integration and independent verification through the original Impact responsibility.」—— 在 Targeted 尚未就绪时就出现了独立验证者派发入口。与 G12-E、G11-E 同一根因(准入集合放宽 / lifecycle 守卫被删)。
- **最小共同生产位置**:`internal/cli/repair_verifier_dispatch_questionnaire.go:30(准入集合含 repairing/ready_for_full_review)+ :26(删掉 lifecycle.state == "bug_resolution" 守卫)`
- **相关位置**:`internal/cli/repair_verifier_dispatch_questionnaire.go:25-31`、`internal/cli/repair_verifier_dispatch_questionnaire.go:26`、`internal/cli/repair_verifier_dispatch_questionnaire.go:30`
- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:72「独立复验与 S7 后继 | Main 按实际工程进度安排独立验证者」
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:213
- **波及面**:1 条测试。
- **修复方向**:与 G12-E 合并为同一修复点。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestS9VerifierRegistrationWaitsForTargetedReadiness` | G08-D |

### G08-E — 旧团队 PLAN_REPORT 收尾门未按蓝图删除,仍拦住原生 S9 Builder 的 TeammateIdle

- **根因**:旧团队固定 execution 的 PLAN_REPORT 收尾门(deny,exit 2)仍在拦原生 S9 Builder,现场消息要求「SendMessage message_type=plan_report」补 checkpoint。蓝图 :162 明确要求删除这条接线。该门父提交就有(9bf73ca:internal/hook/stopidle.go:894)⇒ 既有缺陷,不是本轮重构引入。
- **最小共同生产位置**:`internal/hook/stopidle.go:598`
- **相关位置**:`internal/hook/stopidle.go:598`、`internal/hook/stopidle.go:62`、`internal/hook/stopidle.go:74`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-platform-wiring.md:162「删除旧 TeammateIdle 和固定 execution 的收尾接线,不为旧分支要求当前原生任务额外 Plan 或完成报告」
    - blueprint/l4-mechanisms/L4-hook-anchor-catalog.md:54
    - blueprint/l3-stages/L3-S9-repair.md:248
- **波及面**:1 条测试;属既有缺陷(父提交同款门)。
- **修复方向**:按蓝图删除旧 TeammateIdle/固定 execution 的收尾接线,或让它不作用于原生 Builder。
- **判定**:DEFECT(既有缺陷,非本轮重构引入)

| 测试 | 所属调查组 |
|---|---|
| `TestPublicS9StopIdlePreservesOriginalResultState` | G08-E |

### G13 — nativeS9ConsumerAttempt 的消费者阶段白名单未随重构改名更新,专业拒绝原因在后续前置失败时被丢弃

- **根因**:S9 原生 Result 消费点用 EnterConsumer("original_result_consumer") 标记消费者边界,而 nativeS9ConsumerAttempt 仍用重构前的旧阶段名白名单判定「上一条尝试是不是消费者尝试」。白名单里没有 original_result_consumer,于是专业答卷被拒后,后续一次技术性前置失败再次发布时,发布合并逻辑判定上一条「不是消费者尝试」,把保留的消费者原因置空 —— Main 收到的 ConsumerFailure 为 nil,真实的「哪个文件、哪个字段」被静默吞掉。阶段显示名被当成了结构性判据,改名即静默丢因。
- **最小共同生产位置**:`internal/cli/native_s9_attempt.go:67-70(阶段名白名单)+ internal/cli/native_s9_original.go:95(实际消费阶段名)`
- **相关位置**:`internal/cli/native_s9_attempt.go:67-70`、`internal/cli/native_s9_attempt.go:56-61`、`internal/cli/native_s9_original.go:95`、`internal/diagnosticstorage/storage.go:98-147`
- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52(缺字段指向本人实际路径与字段)
    - blueprint/l3-stages/L3-S9-repair.md:238(保留原稿,反馈实际文件与字段)
    - blueprint/l4-mechanisms/L4-agent-guidance.md:288(不能吞错误)
- **波及面**:1 条测试(本轮 Lane 1 适配退役门后暴露):TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup。
- **修复方向**:把「消费者边界」从阶段显示名里拆出来,作为显式持久化的结构字段(EnterConsumer 置位、ConsumerSucceeded 清位),nativeS9ConsumerAttempt 改判该字段;阶段名退回诊断标签职责。最小替代方案是白名单补一行 original_result_consumer,并补一条枚举本包全部 EnterConsumer 实参的防回归测试。落地方案见 DEFECT-REMEDIATION-PLAN.md §8。
- **判定**:DEFECT

| 测试 | 所属调查组 |
|---|---|
| `TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup` | G13 |

### 已归 STALE 的根因组(前轮判 DEFECT,本轮改判)

#### G01-C — 原生 S9 自然事件接收链不再执行 Git → 测试的 git 屏障永不触发

- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:140(移除类)
- **前轮为什么判错**:蓝图 L3-S9-repair.md:140 是移除类条款(不在每次自然事件重核 Git),d0d948a 删掉接收期 git 调用本身合规;但该测试用 git 调用当超时屏障,触发点已不存在。
- **波及面**:1 条测试;测试其余断言(deadline 传播、发布尾独立、fresh public Main 拿到已收 Plan)在生产侧仍在,只是未被触发。
- **应改成**:改判 STALE,改用非 git 的屏障手段。

成员:`TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain`

#### G02-B — 证据文件存在性回读门槛被蓝图移除,生产不再拒收缺证据的 Result

- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:238(移除类)
- **前轮为什么判错**:蓝图以移除类措辞取消了该门槛,测试前提已不存在。
- **波及面**:1 条测试。
- **应改成**:改判 STALE,交 STALE 改写流程。

成员:`TestNativeS9OriginalStopUsesNewProfessionalInputAfterSourceGap`

#### G02-C — 私有原卷字节漂移检出点随散列机制一并删除

- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:15(散列历史原字段保留,携带 digest 值禁止)
    - blueprint/l4-mechanisms/L4-state-transition-core.md §7(不计算、登记或校验散列)
- **前轮为什么判错**:蓝图明确「读取已收事实时不依赖私人文件仍存在或字节未变」,漂移断言的前提已被移除。
- **波及面**:2 条测试的部分断言(与 G02-A 混合)。
- **应改成**:漂移断言改判 STALE;但这 2 条仍被 G02-A 卡住,需先修 G02-A。

成员:`TestNativeS9PendingRecoveryReportsChangedPrivateOriginal`、`TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals`

#### G03-B — 来源变化判定被整体删除(解析器侧 + 消费者侧),只余「文件可读」检查

- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-project-design-foundation.md:298「当前解析器仍核对索引和来源摘要」
    - blueprint/l3-stages/L3-S4-task-split.md:368「系统不通过散列推断漂移」
- **前轮为什么判错**:【本轮按退役规则改判】测试 `TestNativeBuilderConventionsFixedReplayAndSourceDrift` 断言「改写来源文件后 replay 必须失败并报 'source changed'」——这道门只能由**内容摘要比较**得出。蓝图 `L4-workflow-convergence.md:281`「测试可记录既有历史数据但**不得以散列作为验收前置**」、`:250`「Hook 所触达…均不计算、登记或校验 hash／SHA／digest／fingerprint 等散列…历史记录内已存在的散列值只作为旧记录的原始数据保留…不重算或以其作资格」;本轮规则进一步明确 **sha/hash 不得作为门禁**。⇒ 生产「只查可读、来源变化交 Main 判断」是正确行为,本条应判 STALE。
- **波及面**:1 条测试。蓝图内部有张力(:298 要求核对摘要 vs :368 禁止用散列推断漂移),最小修法需 owner 裁定。
- **应改成**:改判 STALE:改写为「来源字节变化不再拒收(散列门禁已退役),真实来源影响由 Main 与专业责任判断」,并可加反向断言「不因来源变化而伪造资格」。

成员:`TestNativeBuilderConventionsFixedReplayAndSourceDrift`

#### G03-C — 派发登记改写已存在 TASK 的工作状态事实(已被 STALE 适配吸收,当前 PASS)

- **蓝图依据**:
    - blueprint/l3-stages/L3-S6-build.md:103
- **前轮为什么判错**:TASK 已存在分支调用 RecordTaskActivity 改写该行的 state/last_lifecycle_event,与同分支注释「keep all existing TASK facts and prior owners」自相矛盾。
- **波及面**:1 条测试,当前 PASS(final4 实测 pass);缺陷已被 commit 8144ba7 写进共享断言而被吸收。
- **应改成**:总账应把它从 DEFECT 桶移出(见 ledger_corrections)。

成员:`TestCheckProfilePrepareAndReplacementKeepExactProfile`

#### G06-D — flat／typed 适配已按 blueprint 移除,生产只读唯一作者路径是正确行为(前轮判定需修正)

- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:159(移除类)
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:54
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:51
    - blueprint/l3-stages/L3-S9-repair.md:134
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:100
- **前轮为什么判错**:测试期望「已保存 typed 优先于被改/被删的 flat」「缺 typed ⇒ 接收不完整」,而 blueprint L4-hook-acceptance-checklist.md:159 明确「不再适配 flat／typed」;:54「不创建 typed／flat 副本」;L3-S9-repair.md:134「不创建另一套 typed／flat 作者副本」。
- **波及面**:2 条测试 ⇒ 应改判 STALE。
- **应改成**:改判 STALE 并按「唯一作者路径」语义改写测试。

成员:`TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat`、`TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate`

#### G06-E — 证据文件存在性、逐断言覆盖、产品树重扫等机器门已按 blueprint 移除(前轮判定需修正)

- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:59(C10,移除类)
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:53
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:483
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:99
    - blueprint/l3-stages/L3-S9-repair.md:489/:490
- **前轮为什么判错**:测试期望「引用文件缺失必须拒收」「3/6 条断言必须拒收」「源文件消失 ⇒ 接收不完整」,而 blueprint L4-hook-acceptance-checklist.md:53/:59、L4-workflow-convergence.md:483/:99、L3-S9-repair.md:489/:490 明确移除了这些机器门。
- **波及面**:3 条测试 ⇒ 应改判 STALE。
- **应改成**:改判 STALE 并按「只做 JSON 可解析 + 必要字段 + 直接结构」改写测试。

成员:`TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears`、`TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns`、`TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup`

#### G11-D — 复用判定 reason 被压成单一 reuse_not_applicable,丢失「因责任独立而不可复用」的分级

- **蓝图依据**:
    - blueprint/l3-stages/L3-S6-build.md:149「采用整树输入、禁止复用」
    - blueprint/l4-mechanisms/L4-workspace-execution-profiles.md:39「不自动授予复用资格」
- **前轮为什么判错**:【本轮按退役规则改判】旧的分级 reason(internal_or_mandatory_domain_check / profile_reuse_never / unknown_input_or_environment_closure)是为「同输入的机器等价证明」服务的诊断标签。蓝图 `L4-workflow-convergence.md:279`「旧策略 `same_input_same_responsibility` 中『同输入』的机器等价证明**不再作为复用前置**…不计算、登记或比较散列、Git OID、整树内容或逐文件字节来补证」;`L4-workspace-execution-profiles.md:142` 同义。⇒ 单一 `reuse_not_applicable` 才是退役后的正确形状;测试钉死旧标签 ⇒ 应判 STALE。(owner 此前选「暂不裁定」,本轮退役规则把它解掉了,若仍要保留 DEFECT 请指出。)
- **波及面**:1 条测试。**判据提醒**:蓝图要求的是「禁止复用」语义,生产 action=run + would_reuse=false 已满足;分歧只在 reason 字面量,而 blueprint 无任何条款要求该字面量 ⇒ 很可能应重判 STALE。
- **应改成**:改判 STALE:改写为「复用判定只表达『不满足复用资格』,不再细分输入等价原因」。

成员:`TestMainCheckReuseObservationIsReadonlyAndCannotSkipRequiredChecks`

#### G11-PASS — 3 条实测已 PASS,总账误记 DEFECT

- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-workspace-execution-profiles.md:74
    - blueprint/l4-mechanisms/L4-workspace-execution-profiles.md:78
    - blueprint/l4-mechanisms/L4-hook-platform-wiring.md:397/:395
- **前轮为什么判错**:无可复现根因;生产行为与蓝图一致。
- **波及面**:3 条测试,final4 实测全 PASS。
- **应改成**:从 DEFECT 桶移出。

成员:`TestMainCheckCapabilitiesObservationIsReadonlyAndPartial`、`TestMainCheckMissingToolRejectsPrepareBeforeReady`、`TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity`

#### G09-E — 已发行 packet 的重放分支绕过 Writer 并发(revision CAS)检查

- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-workflow-convergence.md:447「修订或恢复使用当前记录、实际责任与 Writer 并发关系」
    - blueprint/l4-mechanisms/L4-runtime-control-plane.md:221「CAS 冲突表示当前事实已经变化……不能把旧结论直接套到新 revision」
- **前轮为什么判错**:【本轮按退役规则改判】测试 `TestS9PreparationCallbackFollowsOriginalCAS` 断言「传过期 revision 必须被拒」(t.Fatal("stale original CAS succeeded"))。但 `internal/repair/runtime.go:1224-1227` 的 `checkRevision` 在 expected<0 时**直接返回 nil**(不检查),而正常调用点 `internal/submission/repair_plan_report.go:33` 传的正是 `-1` ⇒ **生产里的 revision 门禁本来就默认关闭**。蓝图 `L4-workflow-convergence.md:138`「原事务采用已有责任、实际内容和内部版本协调,不以散列作为写入配对或恢复条件」、`L4-revision-usage.md:141`「Runtime revision 只是审计坐标,不证明专业正确」;本轮规则进一步明确 **revision 全线退役,不得作为门禁**。⇒ 测试要求的是一道已退役的门禁,生产行为正确,本条应判 STALE。
- **波及面**:1 条测试。反向条款 L3-S9-repair.md:142「已发行材料和已接收事实继续有效」支持「重放不重发」,但未授权在过期 revision 下返回成功。
- **应改成**:改判 STALE:改写为「过期 revision 不再拒收(门禁已退役)」,断言改为按现行合同表达。

成员:`TestS9PreparationCallbackFollowsOriginalCAS`

### 需 owner 裁定的根因组

#### G07-D — 恢复导航仍以「已登记 64 位摘要」+ 已删除的 typed 消费者阶段作为资格门

- **蓝图依据**:
    - blueprint/l3-stages/L3-S9-repair.md:238
- **根因**:摘要门恒不成立 ⇒ flat 路径可被改写命名;typed 输入边界也不再产生。
- **争议/风险**:1 条测试(该条在总账里是「未决」,不是 DEFECT;此处作为相邻缺陷记录)。
- **修复方向**:改判 STALE 并同时改写夹具(去掉 len(previous.Files[typed]) != 64 依赖),否则任何合规实现下都不可能通过。

成员:`TestS9ReceptionFeedbackMissingSavedTypedInputNamesOriginalWindowBeforeRetry`

#### G12-G — S9 候选登记无条件化 + 兼容入口诊断丢责任引用

- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52(C03「缺字段指向本人实际路径与字段,未知就报告未知」)
    - blueprint/l4-mechanisms/L4-hook-platform-wiring.md:126(反向:非关键缺项不阻止接收)
- **根因**:Result 缺 unit_results 也照样产生候选,测试前置「候选缺失」在现行合同下不可构造;随后兼容入口诊断不再点名该责任。
- **争议/风险**:1 条测试;前半段属「测试前置与现行合同冲突」,后半段才是可修的生产缺口,需 owner 裁定。
- **修复方向**:兼容入口诊断补回 assignment/agent/packet 引用。

成员:`TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing`

#### G11-B — S9 Targeted 的 checkout/来源绑定守卫调用点被删,守卫成死代码 ⇒ 隔离 checkout 可借走 Main 原卷

- **蓝图依据**:
    - blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:52(C03「不跨根、owner、责任或轮次借卷」)
    - blueprint/l4-mechanisms/L4-agent-dispatch-governance.md:57「不借同名卷或虚构来源」
- **根因**:d0d948a 删掉两处调用点(- sourceErr = nativeSharedCheckoutSourceError(... "S7")、- if err := nativeSharedCheckoutSourceError(... "S9 Targeted"); err != nil {),实测隔离 worktree 的 SubagentStop 直接消费了 Main 的原 Targeted 问卷。
- **争议/风险**:1 条测试。**冲突**:internal/cli/native_s9_targeted_listener_test.go:167-176 是同一提交 d0d948a 新增的用例,注释「A tool event's cwd alone does not prove which product it verified.」并断言「另一个 git worktree 仍应被接收」—— 两个用例按 cwd 对立,需 owner 裁定哪条代表蓝图。
- **修复方向**:恢复守卫调用点;但先解决与 native_s9_targeted_listener_test.go 的冲突。

成员:`TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop`

#### G11-E — fresh inactive 校验只查 review.clean_round,漏查 review.round

- **蓝图依据**:
    - blueprint/l3-stages/L3-S1-bind.md:207「必须没有已绑定需求、已建立基线或遗留业务责任」
    - blueprint/l3-stages/L3-S1-bind.md:209
- **根因**:ValidateFreshInactiveState 的「review must be empty」只检查 review["clean_round"],从不检查 review["round"] ⇒ 带 review.round=1 的 pending 状态被当作全新 inactive Runtime 发布。
- **争议/风险**:1 条测试。**判据提醒**:存在反向移除类条款 blueprint/l4-mechanisms/L4-state-transition-core.md:44「删除旧 review.round 兼容读取」与 :48「新 S7 责任不生成或累计轮数」;若 review.round 已废弃,则「round=1 必须拒绝」缺少蓝图依据 ⇒ 建议重判 STALE。
- **修复方向**:先由 owner 裁定 review.round 是否仍是有效字段;若有效,补校验。

成员:`TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero`

## 4. 与既有审计编号的对应

本轮调查与仓库里的既有审计(`TEST-GAP-AUDIT.md` 的 H 编号、`TEST-GAP-AUDIT-FAILURES.md` 的 R 编号)、审计工作区的 C-/D-/E- 编号)有大量重合,对应关系:

| 本轮根因组 | 既有编号 | 说明 |
|---|---|---|
| 本轮根因组 | 既有编号 | 说明 |
|---|---|---|
| **G01-A** | H16 | 审计已定位「生产方把 Branch 写死成空」,本轮确认并给出父提交对照。 |
| **G02-A** | H9 | 超长 ID 分段 + 目录基名反推 packet ID,恒不命中。 |
| **G05-A** | C-1 | 停滞诊断永不可达(facts.Gap 从不投影)。 |
| **G06-B** | D-5 | Result 侧 json.Valid 短路,绕过原消费者字段校验。 |
| **G07-B** | D-5 | 与 G06-B 同一根因族(接收链字段校验缺失)。 |
| **G12-A** | H5 | S3 契约包丢了权威根校验,校验函数仍在别处被调用。 |
| **G03-A** | D-1 / D-2 族 | Builder 回执原领域投影被删、checks_source 被改。 |
| **G08-A** | SEAMS Table 7 #1 | S7 Stop 早退吞掉接收,TR-009 出口不触发(HIGH)。 |
| **G10-B** | H11 / SEAMS Table 7 #2 | recovery apply 对损坏 Runtime 直接不可用(HIGH)。 |
| **G08-B** | — | 审计未覆盖:原责任未完成的 Stop 检查从 deny 降级为 warn。 |
| **G09-B** | SEAMS Table 7 #4 | S6 原生接收缺 planned_paths 写域校验(HIGH)。 |
| **G12-B** | C-4 / C-5 | 接收链原题面来源校验被删。 |
| **G11-B** | — | 审计未覆盖:S9 Targeted checkout 守卫成死代码,与新测试按 cwd 对立。 |
| **G07-C** | — | 审计未覆盖:S8 人工批准门 `validateS8ApprovalMode` 0 调用者。 |
| **G12-D** | — | 审计未覆盖:三个 *-packet-index.schema.json 的 additionalProperties 被放宽。 |
| **G01-B** | — | 审计未覆盖:完成度硬编码 `Result:"pass"`,忽略 unit_results[].status。 |

标注「—」的是**本轮新发现、既有审计没有记录**的缺陷。

## 5. 修复批次建议

按「代价低、波及面大」排序,建议分四批:

### 批次 A:恢复被误删的校验调用点(代价最低,直接对应蓝图要求)

- **G12-A** 权威根绑定校验在接收链丢失 —— 改 `internal/runtime/contract_authority.go:70(不再调用 internal/fileview/contracts.go:42 的 fileview.ValidateAuthority)`;波及 1 条测试。
- **G12-B** 原题面来源校验在接收链丢失 —— 改 `internal/submission/source.go:150-156(不再调用 internal/workspace/document_review.go:107 的 ValidateDocumentReviewView)`;波及 1 条测试。
- **G11-B** S9 Targeted 的 checkout/来源绑定守卫调用点被删,守卫成死代码 ⇒ 隔离 checkout 可借走 Main 原卷 —— 改 `internal/cli/native_s9.go:530-532(guardNativeS9TargetedSource 掏空)+ internal/cli/native_shared_source.go:13(死代码)`;波及 1 条测试。
- **G01-A** 原生 S9 交付执行记录丢失源分支 → integration.Inspect 入口即拒 —— 改 `internal/cli/native_s9_original.go:169 → internal/integration/inspect_responsibility.go:20-22`;波及 11 条测试。
- **G12-F** 已回收的 worker worktree 上无条件执行 git,git 128 直接终止清理重试 —— 改 `internal/cli/workspace_delivery.go:150(workspace.Git(ctx, e.Path, "branch", "--show-current"))+ :152(return fail(err))`;波及 1 条测试。
- **G10-B** recovery apply 无条件执行 ValidateRestore,与仍存在的损坏 Runtime 比对而中止 —— 改 `internal/runtime/recovery.go:521-525`;波及 2 条测试。
- **G08-A** S7 Stop 接收被「已接收 ⇒ return true, 0」早退吞掉,TR-009 出口不触发 —— 改 `internal/cli/native_review_hooks.go:358-359`;波及 2 条测试。

### 批次 B:收紧 schema 与字段校验

- **G07-A** schema 层把「旧记录是否含历史散列字段」变成资格差异 —— 改 `internal/schema/assets/repair-workspace-candidate.schema.json:4 + internal/schema/assets/hypothesis-result-receipt.schema.json:7`;波及 2 条测试。
- **G12-D** 状态索引 schema 严格性被放宽,顶层 additionalProperties 由 false 改成 true —— 改 `internal/schema/assets/review-result-packet-index.schema.json:6(同族 repair-result-packet-index.schema.json:6、repair-plan-report-packet-index.schema.json:6)`;波及 1 条测试。
- **G04-A** 「已声明但为空」的集合被拷成 nil slice → context.json 里写成 null,违反生产 schema 的 array 要求 —— 改 `internal/cli/assignment_context.go:328-346(:331-332、:339)→ 自校验 :313`;波及 3 条测试。
- **G06-A** Result/PlanReport 专业字段级校验被放宽,原消费者不再对 null/缺失/未知字段产出字段错误 —— 改 `internal/workpacket/repair_result.go:295-341(:297 宽松解码 decodeOneJSON)+ internal/workpacket/repair_plan_report.go:271-281`;波及 2 条测试。
- **G06-B** 扁平私人文件字段反馈映射未接线,nativeS9FlatArtifactError 成为死代码 —— 改 `internal/cli/native_s9_artifact_feedback.go:18(无调用点)`;波及 1 条测试。

### 批次 C:恢复事实投影与身份识别

- **G02-A** 原生 Result 路径反向选责用目录基名匹配完整 packet ID;长 ID 被分段后基名只是片段,恒不命中 —— 改 `internal/cli/native_s9_recovery.go:87(触发源 internal/repair/native_s9_private.go:13-23、:29;报错点 :115)`;波及 8 条测试。
- **G05-A** projectWorkProgress 不再投影 milestone(missing→Gap、read→Sources)⇒ 停滞诊断永不可达 —— 改 `internal/cli/work_progress.go:224 → internal/hook/work_progress.go:195`;波及 3 条测试。
- **G06-C** 诊断存储识别规则未随无散列诊断布局更新 —— 改 `internal/diagnosticstorage/storage.go:18(路径正则)+ :78(scope 长度 64)`;波及 1 条测试。
- **G03-B** 来源变化判定被整体删除(解析器侧 + 消费者侧),只余「文件可读」检查 —— 改 `internal/projectconventions/resolve.go:154-167(解析器侧,父提交 :161 有 "source digest changed")+ internal/cli/context_conventions.go:136-140(仅查可读)`;波及 1 条测试。
- **G03-A** Builder 回执的原领域投影被删成桩,checks_source 由 original_report 改成 main_checkpoint —— 改 `internal/qualitygate/builder_receipt.go:79-96(:93 硬编码 ChecksSource:"main_checkpoint")`;波及 6 条测试。
- **G10-A** hook 入口静默吞掉无法归属的事件(非 Main/Worker),不报「未知」 —— 改 `internal/cli/hook_reader.go:208-210(hookReaderObservation: reader.Kind != "Worker" ⇒ return true, 0)`;波及 4 条测试。
- **G08-B** 原责任未完成/未知的 Stop 检查从 deny 降级为 warn —— 改 `internal/hook/stopidle.go:153(warn/original_responsibility_unfinished)、:678/:686/:694(warn/*_original_responsibility_unknown)`;波及 1 条测试。

### 批次 D:准入集合与完成度语义

- **G12-E** 独立验证者派发准入集合被放宽,S9 lifecycle 守卫被删 —— 改 `internal/cli/repair_verifier_dispatch_questionnaire.go:30(新增 repairing/ready_for_full_review)+ :26(删掉 lifecycle.state == "bug_resolution" 守卫)`;波及 1 条测试。
- **G11-E** fresh inactive 校验只查 review.clean_round,漏查 review.round —— 改 `internal/runtime/store.go:2248-2251`;波及 1 条测试。
- **G11-D** 复用判定 reason 被压成单一 reuse_not_applicable,丢失「因责任独立而不可复用」的分级 —— 改 `internal/checkexec/reuse.go:45-46(旧 internal/checkexec/reuse.go 的分级 reason 被合并)`;波及 1 条测试。
- **G01-B** 完成度判定忽略真实 unit_results[].status,只信顶层 verdict + 硬编码 pass —— 改 `internal/repair/issued_repair_result.go:696(硬编码 Result:"pass")→ :820-822 → internal/submission/repair_result.go:535-538 → internal/cli/native_main_return.go:604-609`;波及 1 条测试。
- **G12-G** S9 候选登记无条件化 + 兼容入口诊断丢责任引用 —— 改 `internal/repair/issued_repair_result.go:519/:550(无条件生成候选并写 DeliveryRef)+ internal/cli/workspace_delivery.go:445(诊断不含 assignment/agent/packet)`;波及 1 条测试。
- **G11-C** S9 阶段交接文案使用泛称 "Plan" 而非蓝图规范名 "PlanReport" —— 改 `internal/cli/controller.go:515(与 :554 的正常路径)`;波及 1 条测试。
- **G08-C** S7 manifest 准备指引未给出原 register-workgroup 入口/方向不对 —— 改 `internal/cli/s7_guidance.go:127(只打印 s7 manifest-draft 命令)`;波及 2 条测试。
- **G08-E** 旧团队 PLAN_REPORT 收尾门未按蓝图删除,仍拦住原生 S9 Builder 的 TeammateIdle —— 改 `internal/hook/stopidle.go:598`;波及 1 条测试。
- **G09-B** 原生 S6 接收链缺少 planned_paths 的 Assignment 写域校验 —— 改 `internal/cli/native_s6.go:439(receiveNativeS6)→ internal/assignment/completion_canonical.go:626(只核身份绑定)`;波及 1 条测试。
- **G09-C** 集成 Inspect 只把脏工作树当瞬时观察,不进 gate/checkpoint —— 改 `internal/integration/inspect_responsibility.go:16 + internal/integration/types.go:97-98`;波及 1 条测试。
- **G09-D** 平台事件被提前短路 / 身份信息被丢弃,接收方拿不到准确对象 —— 改 `internal/cli/hook_reader.go:208-210(Kind 既非 Main 也非 Worker ⇒ return true, 0,吞掉整类 PreToolUse);internal/cli/native_review_hooks.go:555(只读 transcript,读不到就 return "")`;波及 2 条测试。
- **G09-F** 责任歧义未与「未知」分别报告,多重精确匹配被折叠 —— 改 `internal/cli/native_main_return.go:177(selected, unknown = nil, true)`;波及 1 条测试。

## 6. 方法与红线自检

### 方法

1. **先机器聚类,再人工深挖**:把 89 条 DEFECT 按「实测报错首条 + 断言位置 + 源文件」聚成 12 组,每组一份调查包(蓝图依据 + 实测输出 + 前轮线索)。
2. **每组一个只读调查 agent**,逐条产出「蓝图要求(原文+行号) → 生产行为(file:line) → 根因 → 共享根因组」。调查 agent **只读**,不碰仓库。
3. **parent 独立复核**:G01、G02、G04 三组由 parent 亲自查生产代码与父提交对照后确认,结论与 agent 一致;所有关键主张都有 `file:line`。
4. **父提交基线对照**:能用 `git show 9bf73ca` 判定的,区分「本次重构引入的回归」与「既有缺陷」。

### 红线自检

```
本阶段改动文件: 0 个生产代码,0 个测试文件
产出: DEFECT-INVESTIGATION-REPORT.md(本文件)+ 审计工作区的 findings.json
```

**总账建议更正**(待 owner 确认后执行):

- 删除幽灵条目 1 条;
- 把 6 条已绿条目从 DEFECT 桶移出(改记 STALE·已通过);
- 把 12 条改判 STALE,交回「STALE 适配」流程;
- 0 条等 owner 裁定方向。
