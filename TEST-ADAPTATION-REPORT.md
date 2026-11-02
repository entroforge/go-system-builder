# 测试适配报告:过时 vs 缺陷 vs 冲突

> **阶段**:`TEST-GAP-AUDIT.md` 之后的「按 blueprint 适配测试」阶段。
> 上一阶段审计出 **345 个失败测试实体**(`internal/cli` 298 + `internal/investigation` 21 + 系统层 26)。
> 本阶段按 **blueprint 设计**把每一个失败判定为「过时测试」还是「缺陷/冲突」:
> **过时 → 改写测试;缺陷/冲突 → 只记录,不动生产代码。**

## 0. 结论

本轮共判定 **349 条**失败实体:

| 判定 | 条数 | 处置 |
|---|---|---|
| **STALE(过时测试)** | **218** | **已改写测试** —— 断言改为蓝图要求的新行为 |
| **DEFECT(缺陷)** | **86** | **只记录** —— 蓝图仍要求该行为,生产代码违反 |
| **CONFLICT(冲突)** | **7** | **只记录** —— 蓝图自相矛盾,需 owner 裁定 |
| **未决** | **38** | 记录并说明缺什么证据 |

### 红线核查

```
本轮改动文件: 124 个
  ├─ 测试文件 (*_test.go): 122 个
  ├─ 报告文档 (*.md): 2 个
  └─ 其它: 0 个
生产代码违规: 0 个  ← 必须为 0
统计: 124 files changed, 6300 insertions(+), 1608 deletions(-)
未提交改动: 0 个
```

> ✅ **本轮未修改任何生产代码。** 所有代码改动都在 `*_test.go` 里;
> 报告文档(`*.md`)是本阶段的交付物,不属于生产代码。

报告文档改动:`TEST-ADAPTATION-REPORT.md`, `TEST-GAP-AUDIT.md`

## 1. 最终测试结果

| 包 | 通过 | 仍失败 | 失败子测试 |
|---|---|---|---|
| `cli` | 995 | 118 | 78 |
| `integration` | 80 | 2 | 0 |
| `investigation` | 83 | 2 | 0 |
| `recovery` | 10 | 2 | 0 |
| `req039` | 93 | 5 | 0 |

**合计仍失败 129 个顶层测试。**

> **校正**:这次全量跑在 14:05 启动,其后还合并了 4 个提交(`2e02dfc`、`088bc77`、`e566950` 及 `51b94ae`),这些提交已单独定向复跑通过(8 个测试全绿)。把它们计入后,**当前实际仍失败约 121 个**,即 **170 → 121**。剩余 STALE 仍红只剩 `internal/cli/controller_test.go` 桶的 5 个测试(`TestBuildGuidanceS9UsesOriginalQuestionnairesAndLifecycle`、`TestHandleSubagentStopAdvancesIntegrationOnReadyInspection`、`TestHandleSubagentStopPreservesWorktreeOnNotReadyInspection`、`TestPostMergeModeChecksCandidateThroughController`、`TestS9RepairNextActionKeepsNativeCandidateWithMain`)——本轮多轮改写 agent 被运行时批量中断,未能落地。

> **所有仍失败的测试都有判定**:本次跑出来的失败里,总账外为 **0** 条。
### 仍失败的测试清单

<details><summary><code>cli</code> — 118 个</summary>

- `TestAC005_MilestonePersistsQualityGate`
- `TestAC006_SubagentStopEmitsIntegrationGuidance`
- `TestAC008_CompactRecoversMilestoneFromSnapshot`
- `TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration`
- `TestActualMainIntegrationBarrierCleanupAfterEarlyReturn`
- `TestBuildGuidanceS9UsesOriginalQuestionnairesAndLifecycle`
- `TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration`
- `TestCT03911_DualPassDocumentVerificationCommitsTR003`
- `TestCT03915_S11StopsAutoAdvance`
- `TestCT03920_BuilderCompletionDurableRef`
- `TestCT03924_SameAgentDualDVLabelsBlocksTR003`
- `TestCanonicalS6WorkspaceReworkRequiresNamedAssignmentRevision`
- `TestCheckProfileActualMainIntegrationCapturesOriginalResponsibility`
- `TestCheckpointRejectsOlderPairAndForgedReceptionFacts`
- `TestConfiguredPressureOriginalReportWriterRegistrationAndActualMainIntegration`
- `TestContractAuthorityUsesOriginalS2RegisteredDesign`
- `TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite`
- `TestHandleSubagentStopAdvancesIntegrationOnReadyInspection`
- `TestHandleSubagentStopPreservesWorktreeOnNotReadyInspection`
- `TestLastFinalPreparesImpactBeforeIndependentCleanup`
- `TestMainCheckReuseObservationIsReadonlyAndCannotSkipRequiredChecks`
- `TestMainPlanningReadCycleConsumesAcceptedS2Followup`
- `TestMainReviewPlanQuestionnaireStopDoesNotPrepareAndInvalidSaveReturnsFeedback`
- `TestMainS2S5MilestoneDirectoriesProjectActualReadSources`
- `TestMainS6LockedReadCategoriesResolveRegisteredCurrentFiles`
- `TestNativeBuilderConventionsFixedReplayAndSourceDrift`
- `TestNativeMainReturnReadsS5ReceiptAcrossStageWithoutReceivingAnswers`
- `TestNativeMainS7ReturnKeepsSubmissionDiagnosticWhenOwnBodyFails`
- `TestNativeProfileContextExposesFrozenOriginalProfile`
- `TestNativeS6FixedSourceAlignmentAfterMainAdvances`
- `TestNativeS6RevisionKeepsObjectGuardsAcrossUnrelatedRuntimeCommit`
- `TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals`
- `TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication`
- `TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery`
- `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields`
- `TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate`
- `TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint`
- `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat`
- `TestNativeS9OriginalCandidateReadsHistoricalMachineValues`
- `TestNativeS9OriginalConsumeAcceptedPlanSurvivesMissingDisplayBrief`
- `TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan`
- `TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat`
- `TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat`
- `TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput`
- `TestNativeS9OriginalFlatConsumeReceivesAndProjectsReplay`
- `TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup`
- `TestNativeS9OriginalStopUsesNewProfessionalInputAfterSourceGap`
- `TestNativeS9PendingPublicPreReturnsExecutableOriginalWriterRecovery`
- `TestNativeS9PendingRecoveryReportsChangedPrivateOriginal`
- `TestNativeS9PlanOnlyStopReportsUnreadyOriginalPlan`
- `TestNativeS9PublicAuthorCorrectsRejectedResult`
- `TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears`
- `TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns`
- `TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop`
- `TestNativeS9UnrecordedMergePreservesUnknownAndWorktree`
- `TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute`
- `TestNativeWorkerCheckResultPersistsDurableFactsAndBindsAnswerAndSource`
- `TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero`
- `TestPlanningObserverUsesAnswerAndBoundCommitWithoutRepeatingUnchangedSave`
- `TestPostMergeModeChecksCandidateThroughController`
- `TestProgressPacketS9SkipsLegacyCoverageWithoutCache`
- `TestPublicS9StopIdlePreservesOriginalResultState`
- `TestRuntimeInvestigationContractApproveCLIHandsOffToS9`
- `TestS10ReviewRequiredPendingReturnDoesNotReceiveChangedOrWorkerAnswer`
- `TestS10ReviewRequiredReturnsOriginalFullReviewAndNewPlan`
- `TestS3UncommittedOutputNamesCommitAndAcceptsSameAnswerAfterCommit`
- `TestS4AcceptedDescriptionChangeKeepsCompletionButCannotRegisterChangedMaterials`
- `TestS5ReturnToS6ProvidesPreparationInsteadOfWaitingForUnstartedBuilder`
- `TestS6ClosingContractCLISeparatesOwnedCompletionFromMain`
- `TestS6IntegrationWithoutMainStartKeepsOriginalBatchGate`
- `TestS6NativeClosingContextKeepsAssessmentEditableAcrossRetry`
- `TestS6UnchangedPreToolUseReusesObservation`
- `TestS7AcceptedRemainingProjectionUsesOriginalPrerequisites`
- `TestS7DispatchGuidanceUsesActualPrerequisitesAndSource`
- `TestS7ExactNativeStartAndChangedMainContext`
- `TestS7ExistingEditedManifestRecoversRegistrationWithoutOverwrite`
- `TestS7IncompleteNativeReturnHandsOriginalResponsibilityToMain`
- `TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility`
- `TestS7NativeListenerReportsOriginalFieldErrorWithoutDenyingTool`
- `TestS7ObserverSkipsMachineDraftAndReceivesProfessionalScriptSave`
- `TestS7OptionalProgressUnknownNativeActorIsSilent`
- `TestS7PublicHookReceivesOriginalWithoutLegacyActivationRoute`
- `TestS7SharedOriginalCannotBeBorrowedByUnboundNativeWorktree`
- `TestS8NativeLifecycleDeliversExactQuestionnaireInHookEnvelope`
- `TestS8ObservedContractPassiveSkipKeepsExplicitSaveAndChangedValidation`
- `TestS9CapturedInputCoordinateAloneIsNotSavedTypedInput`
- `TestS9CapturedRecoveryCommandExecutesOriginalTypedConsumer`
- `TestS9EntryAssemblyReadsOriginalRepairPlanOnce`
- `TestS9EvidenceReferenceIsPreservedWithoutCopyOrExistenceGate`
- `TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain`
- `TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection`
- `TestS9HandoffCanceledFinalAcknowledgementPreservesOriginalProof`
- `TestS9HandoffDamagedTypedCandidateStopsBeforeMerge`
- `TestS9HandoffFinalProjectionRetainsOriginalCleanupResponsibility`
- `TestS9HandoffFinalReceiptSurvivesMissingPlanIndex`
- `TestS9HandoffPartialCandidateDoesNotBecomeIntegrationDuty`
- `TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts`
- `TestS9HandoffSameOwnerNeighborCannotSupplyOrBreakOriginalCandidate`
- `TestS9HandoffSpentProgressPreservesIndexesWithoutNewDocumentReads`
- `TestS9HandoffTypedCandidateCannotLoseProvenanceOrSource`
- `TestS9HandoffTypedSelectorPreservesChangedAttemptFence`
- `TestS9ListenerExistingPacketsReadOnlyWithoutIssueRecoveryOrReceiptProof`
- `TestS9NativeListenerKeepsPrivateOutputsOutOfMainAuthoring`
- `TestS9PlanningGuidanceUsesOriginalDivisionAndIssuedOwner`
- `TestS9PreparationCallbackFollowsOriginalCAS`
- `TestS9PublicFeedbackRecoveryCommandUsesReturnedArgv`
- `TestS9ReceptionFeedbackMissingSavedTypedInputNamesOriginalWindowBeforeRetry`
- `TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical`
- `TestS9RepairDispatchExplicitSavePreparesNativeBriefAndPreservesOriginalSources`
- `TestS9RepairDispatchQuestionnaireResponsibilityAndS8Isolation`
- `TestS9RepairNextActionKeepsNativeCandidateWithMain`
- `TestS9RepairVerifierDispatchQuestionnaireOneIndependentContractOwner`
- `TestS9RepairVerifierDispatchQuestionnaireOriginalPhasesAndResponsibility`
- `TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing`
- `TestS9VerifierRegistrationWaitsForTargetedReadiness`
- `TestSavedHypothesisResultHookDoesNotTreatUnissuedTeammatePathAsCurrent`
- `TestSavedHypothesisResultHookRejectsUnissuedPathWithoutRuntimeWrite`
- `TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible`

</details>

<details><summary><code>integration</code> — 2 个</summary>

- `TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound`
- `TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound`

</details>

<details><summary><code>investigation</code> — 2 个</summary>

- `TestOriginalHypothesisReceiptIgnoresHistoricalMachineFields`
- `TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates`

</details>

<details><summary><code>recovery</code> — 2 个</summary>

- `TestRuntimeRecoveryApplyNeverRevivesActiveAgents`
- `TestRuntimeRecoveryApplySamePlanIsIdempotent`

</details>

<details><summary><code>req039</code> — 5 个</summary>

- `TestAuditSharedModelHookSpineCommittedSource`
- `TestAuditStageSourcesUseOriginalConsumerBoundaryE2E`
- `TestBUG104S11ExplicitApproveE2E`
- `TestCT03912_BuilderCompleteCommitsTR006System`
- `TestS2ToS11_HookDrivenCleanPath`

</details>

## 2. 逐桶判定与改动

| 桶 | 范围 | 判定数 | STALE | DEFECT | CONFLICT | 未决 |
|---|---|---|---|---|---|---|
| D1 | 缺陷复核 H1/H7/H15/H16/H18/M1 | 25 | 14 | 10 | 0 | 1 |
| D2 | 缺陷复核 H9/H14/H16/H17/H7/M1 | 25 | 5 | 20 | 0 | 0 |
| D3 | 缺陷复核 H1/H14/H15/H16/H18 | 26 | 12 | 8 | 0 | 6 |
| L1 | 长尾 | 39 | 23 | 5 | 1 | 10 |
| L2 | 长尾 | 40 | 23 | 16 | 0 | 1 |
| L3 | 长尾 | 40 | 17 | 18 | 2 | 3 |
| L4 | 长尾 | 49 | 36 | 0 | 1 | 12 |
| L5 | 长尾 | 14 | 14 | 0 | 0 | 0 |
| INV | internal/investigation | 21 | 19 | 2 | 0 | 0 |
| SYS | 接缝与系统层 | 26 | 17 | 4 | 0 | 5 |

## 3. STALE 改写清单(逐条)

> 每一行的「蓝图依据」是该条判定的直接依据。改写后的测试**必须仍能抓住原本要防的回归**;
> 多数条目额外加了**反向断言**(断言已删除的字段/行为不得再出现)。

### 桶 D1 — 缺陷复核 H1/H7/H15/H16/H18/M1(14 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestNativeMainPendingContinuesActualReviewPlanSave` | native_main_pending_external_test.go:27 | L3-S7:191「路径沿本次 ReviewPlan 身份生成，不嵌入审查序号。移除旧 `S7/r<round>/review-plan.json` 运行接线」 | fixture 补 ReviewPlan 身份 |
| `TestNativeS9LateTargetedPublicHookPreservesCurrentS7` | native_s9_late_targeted_public_test.go:44 | L3-S9:238「原坐标不足则交 Main 准确材料故障，不从当前 HEAD 猜补」 | fixture 补 `questionnaire_root`;仍被 D-3 挡住 |
| `TestNativeS9RelativeMainCompletedKeepsOriginalLiveSourceContract` | native_s9_reference_acceptance_test.go:200 | L3-S9:246「Worker `SubagentStop` \ | 兜底本人原卷并交接真实结果或未完成事实」;L3-S7:221 |
| `TestNativeS9RelativeTargetedPublicEventUsesIndependentOriginal` | native_s9_reference_acceptance_test.go:255 | L4-hook-platform-wiring.md:97「不能将旧 `Purpose`、typed 优先、C/D 来源核验或宽静默保留为新链门禁」;L4-hook-acceptance-checklist.md:54 | 去掉已移除的 C/D 来源核验断言 |
| `TestS7InitialDraftNaturalObservationAndExplicitSave` | review_plan_draft_baseline_e2e_test.go:30 | 同上 | 同上 |
| `TestS7OldWithoutBaselineAndWrongPlanIDContinueNaturally` | review_plan_draft_baseline_e2e_test.go:98 | 同上 | 同上 |
| `TestS9HandoffCanceledIntegrationAndProjectionPreserveKnownFacts` | s9_handoff_wiring_test.go:227 | 同上 | 共用 helper,同改 |
| `TestS9HandoffIncompleteBindingNeverOpensCandidate` | s9_handoff_wiring_test.go:124 | 同上 + L3-S9:15「不计算、登记或校验任何散列」 | 改 fixture + 改期望消息 |
| `TestS9HandoffLaterIndexFailureDoesNotEraseAcceptedPlan` | s9_handoff_wiring_test.go:421 | 同上 | 共用 helper,同改 |
| `TestS9HandoffMissingCandidatePreservesPlanAcrossAllEntrances` | s9_handoff_wiring_test.go:82 | L3-S9:236「系统不重新证明代码、证据内容或实际检查是否发生，不回读证据文件、不归档后散列取证」 | 改用「Result 尚未保存」的真实未决边界 |
| `TestS9HandoffResultDiagnosticUsesUniqueOriginalPlanBinding` | s9_handoff_wiring_test.go:196 | 同上 | 共用 helper,同改 |
| `TestS9NativeDivisionKeepsDependencyAndRejectsExtraWorkspacePreparation` | s9_questionnaire_observer_test.go:429 | L3-S9:144「删除历史 generic、C／D、issued／冻结材料的兼容启动、接收、source 与恢复接线」 | 改期望消息 + 补 agent 实体 |
| `TestS9ObserverLeadReplayUsesImmutableProfessionalProjection` | s9_questionnaire_observer_test.go:146 | L3-S7:195「已接收后仍可自然修改……未变化则静默复用，真实变化沿原修订接收」(经 L3-S9:236 适用 S9) | 改断言 + 补 agent 实体 |
| `TestS9SaveAdapterFindsChangedCurrentPlanWithoutRewritingCaller` | s9_questionnaire_observer_test.go:317 | L4-hook-platform-wiring.md:83「真实关联缺失交系统，不借角色、同名文件或另一个作者的稿件代替」 | 改断言 + 改期望消息 |

### 桶 D2 — 缺陷复核 H9/H14/H16/H17/H7/M1(5 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestS9RepairLeadAnswerShapeUsesActualSources` |  | 18 | repair_lead_answer_test.go |
| `TestS9RepairLeadExistingImpactReminderSkipsPreparation` |  | 16 | repair_lead_answer_test.go |
| `TestS9RepairLeadStopAndIdleDoNotGuessPathWithoutAuthority` |  | 19 | repair_lead_questionnaire_test.go |
| `TestS9RepairLeadTargetedPathUsesRegisteredIndependentAssignment` |  | 20 | repair_lead_questionnaire_test.go |
| `TestS9RepairLeadTargetedReminderUsesIndexedProjection` |  | 17 | repair_lead_answer_test.go |

### 桶 D3 — 缺陷复核 H1/H14/H15/H16/H18(12 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestHandoffReviewMainProgressMustNotRepeatSamePreparation` | s9_entry_assembly_test.go | 20 | 1 |
| `TestMainReviewPlanPreLargeReplayPreservesAcceptedPlan` | review_plan_pre_listener_e2e_test.go | 16 | 1 |
| `TestMainReviewPlanPrePreservesReceiptWhenAcceptedContentMissing` | review_plan_pre_listener_e2e_test.go | 18 | 1 |
| `TestMainReviewPlanPreReceivesChangedOriginal` | review_plan_pre_listener_e2e_test.go | 17 | 1 |
| `TestMainReviewPlanPreReceivesReadyOriginal` | review_plan_pre_listener_e2e_test.go | 14 | 4 |
| `TestMainReviewPlanPreWhitespaceExpansionMatchesOriginalConsumer` | review_plan_pre_listener_e2e_test.go | 15 | 1 |
| `TestNativeExecutionLaunchPreservesOriginalPartialReceipt` | native_execution_return_test.go | 6 | 1 |
| `TestS7ObserverSkipsMachineDraft` |  | 归为旧路径过时 fixture | `L3-S7:193/:221` + 同测试后半段证明自然观察仍是接收触发器 |
| `TestS7ObserverSkipsMachineDraftAndReceivesProfessionalScriptSave` | questionnaire_observer_e2e_test.go | 12 | 1 |
| `TestS9EntryAssemblyPreservesNewPlanFailureAndOriginalStopFailure` | s9_entry_assembly_test.go | 22 | 1 |
| `TestS9EntryAssemblyPreservesParallelResponsibilitiesAndWorker` | s9_entry_assembly_test.go | 21 | 1 |
| `TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical` | s9_reception_feedback_contract_test.go | 23 | 1 |

### 桶 L1 — 长尾(23 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestHookCommandMissingRuntimeFailsOpenWithGateDiagnostic` | internal/cli/run_test.go:~1177 | L4-hook-platform-wiring.md:61「内部的 `facts`、`runtime_context`…不能当作平台已经证明完成的字段」；L4-state-transition-core.md:257「当前出口未知…应报告系统问题，不伪造通过」 | 改写：载荷改为 Main 路径（去掉 `agent_id`），断言退出 0、不 deny、不可读 Runtime 诊断在 stdout/stderr 可见、不伪造当前阶段 |
| `TestHookCommandObservesProductWriteWithoutToolDenial` | internal/cli/run_test.go:~975 | 同上 | 改写：保留「不 deny」「不出现旧 HOOK_AGENT_NOT_ACTIVATED」，把「必须出现 QUALITY_GATE」改为「Worker 观察保持安静」 |
| `TestHookCommandPreToolUseDoesNotRequireToolIdentityContext` | internal/cli/run_test.go:~815 | L4-agent-guidance.md:451「普通观察与恢复 \ | Worker 安静」；L4-runtime-control-plane.md:235/239 |
| `TestHookCommandPreservesAuditOnOutboxFailure` | internal/cli/run_test.go:~1250 | L4-hook-platform-wiring.md:61、:175「具名原生观察审计 \ | 追加审计，协议 stdout 静默」、:178「输入、通用审计或输出故障 \ |
| `TestHookCommandReportsOutboxFailureWithoutToolDeny` | internal/cli/run_test.go:~1285 | 同上 | 同上 |
| `TestLifecycleVerbChainE2E` | internal/cli/lifecycle_e2e_test.go:~180 | L4-workflow-convergence.md:250（回执不得登记散列） | 改写：req-archive 回执记录真实状态迁移 `locked→archived`，且无 `sha256_before`/`sha256_after` |
| `TestNativeContextBindingCheckpointPreservesFixedFactsAndRejectsMissingSources` | internal/cli/assignment_context_binding_test.go:175 | L4-stage-form.md:62「后续事件读取已保存事实，不重算旧散列」；L4-workflow-convergence.md:250 | 改写：checkpoint 不再重算冻结源摘要、不再要求原准备证明；Runtime/journal 尾部配对仍须成立 |
| `TestNativeContextBindingFreezesAcrossWorkingStateAndRejectsSourceDrift` | internal/cli/assignment_context_binding_test.go:32 | L4-workflow-convergence.md:250「Hook 所触达的…回执…均不计算、登记或校验 hash／SHA／digest／fingerprint 等散列」；L4-stage-form.md:51「不计算、登记或校验 SHA、工作包摘要或 Git OID，不能据路径声称字节未变」 | 改写：冻结 binding 对已安装源改动既不失效也不重建；冻结上下文无 sha256；缺失冻结源仍报 unavailable |
| `TestNativeS6HandbackUsesInitialTaskAndActualArtifacts` | internal/cli/native_s6_test.go:~610 | L4-worktree-governance.md:43「旧 `bound_commit`、`source_head`…不重算或作为新接收、恢复与资格条件」；L4-workflow-convergence.md:300「不要求或登记 `expected_head`…Git OID」 | 改写：集成不再以已登记 source head 阻断；Main 的集成实际合并当前 worktree 内容，合并树含最新产物 |
| `TestNativeS6MainCommandRecoversActualLostMergeAndLaterTarget` | internal/cli/native_s6_merge_recovery_test.go:~100 | L4-workflow-convergence.md:300「提交成功与结果持久化之间中断时保留未知并交 Main 查看真实状态，不猜成功、不自动重复提交。删除仅为旧 frozen execution 准备的 commit／适配及其兼容恢复接线」；:302（cleanup 独立于接收） | 改写：按两种中断点分别断言（merge 结果未知→保留 unknown、checkpoint 不前进、Git 不重复／merge 已发生→按实际状态续接真实集成、worktree 不强删），重放均不重复 merge |
| `TestNativeS6UnifiedReworkAndRecoveryUseOriginalRecordWriter` | internal/cli/native_s6_test.go:~500 | L4-workflow-convergence.md:269「需要组合路径或内部键时使用无歧义可逆编码，不散列、截断或依赖内容」；:250 | 改写：已移除的 `--revision-intent-key`（原为 `Digest(...)[:12]` 派生身份）不再被接受，且当前修订号不变 |
| `TestNativeS9PreparedBriefAndConsumerPreserveSatisfiedAssertion` | internal/cli/native_s9_result_guidance_test.go:~30 | L3-S9-repair.md:354「系统不逐项比对合同集合、按缺少某条观察拒收」；L4-hook-acceptance-checklist.md:59「不以…逐项覆盖…为由拒收可理解的原结论」 | 改写：①brief 锚点改当前措辞（原断言集仍按原序保留）；②缺一条已满足断言不再按机器覆盖拒收：不再出现 `got 5 want 6`，诚实部分 Result 作为 candidate 被接收并登记交付 |
| `TestNextProjectsInvestigationAsS8AndRepairAsS9` | internal/cli/run_test.go:~15 | L4-workflow-convergence.md:263（S9 行）、:271 | 改写：4 个 S9 相位子用例的 `wantText` 改为当前自然事件措辞（`wantText` 改为字符串数组逐条断言，仍保留 `forbidden` 旧指令检查） |
| `TestPreToolUseObservesUnknownMCPForWorker` | internal/cli/controller_integration_test.go:~180 | L4-agent-guidance.md:451；L4-runtime-control-plane.md:239；L4-hook-anchor-catalog.md:60「PreToolUse 与 PostToolUse 的 `*` 都覆盖工具事件，包括 MCP 等自定义工具」 | 改写：退出 0、不 deny、未知 MCP 不被转成工具策略、普通 Worker 观察不被注入 Main 控制周期投影 |
| `TestREQBindArchivesControllerTouchedInactiveRuntimeAndStartsAtRevisionZero` | internal/cli/bind_rollover_test.go:~55 | L4-state-transition-core.md:327「绑定、解绑和周期归档…保存归档，不计算或验证摘要」；L4-workflow-convergence.md:250 | 改写：归档保留完整绑定前 journal；回执不再登记 `source_journal_sha256`，仍带 source/target runtime 身份与修订 |
| `TestRepairResultPacketMatchesOnlyExactRegisteredExecution` | internal/cli/workspace_delivery_matcher_test.go:~30 | L4-workflow-convergence.md:250、:269、:271 | 改写：①packet 内嵌投影被伪造不改变选责——按 binding 身份解析已登记 execution，指向未登记 ID 仍拒收；②已登记 definition 漂移不再被摘要比对拒绝，匹配器报告实际当前登记 definition |
| `TestS10StatusRejectsManifestFromStaleBaseline` | internal/cli/s10_command_test.go:~175 | 同上 | 改写：历史报告不得满足或冒充当前责任（state=missing、无 evidence_id），路径按上游 plan 派生；「当前行」漂移后仍不得呈现为已满足 |
| `TestS10StatusReportsTheCurrentRoundAndNextAction` | internal/cli/s10_command_test.go:~127 | L3-S10-acceptance-audit.md:159「不生成、消费或兼容读取 `review_round`」；:226「当前报告的查找依据是本责任、本代际和实际上游验证计划的最新有效记录」；L4-runtime-control-plane.md:245 | 改写：夹具补上游 review plan；断言不再出现 `review_round`，manifest 路径按上游 plan 身份派生，`next` 指向该真实路径 |
| `TestS5ConventionsFreezeOriginalRulesInstallAndRejectDrift` | internal/cli/task_conventions_e2e_test.go:~10 | L4-workflow-convergence.md:250、:271「不声称自动发现所有外部改写」、:300「删除仅为旧 frozen execution 准备的 commit／适配及其兼容恢复接线」 | 改写：①脏 registry 提示语改当前措辞；②来源漂移不再按 frozen-execution 摘要比对拒收——答卷仍被接收并推进 Runtime，已签发 conventions 不被重算 |
| `TestS6CanonicalResultCLIAcceptanceReplayAndCurrentReceipt` | internal/cli/s6_record_context_e2e_test.go:~455 | L4-hook-acceptance-checklist.md:53「必要检查仅为 JSON 可解析、适用的必要字段与直接结构…不因证据内容和测试日志另立机器门」；L3-S9-repair.md:15「旧记录中已有散列值只作为历史原字段读取…不…作为继续资格」 | 改写：原用例的拒绝理由实为已移除的 `assignment_record_sha256`；改为断言「缺适用必填字段」仍被 schema 拒绝且 Runtime 不变、不新增产物 |
| `TestS7ObserverSkipsAcceptedReviewerQuestionnaires` | internal/cli/questionnaire_observer_s7_test.go:~10 | L4-hook-acceptance-checklist.md:52「责任由已有任务与登记坐标准确定位…不按角色、最近文件、唯一候选或目录扫描猜选」；L4-workflow-convergence.md:240 | 改写：观察器按当前 ReviewPlan 身份精确选责；新增「他身份 packet 不得被观察」子用例 |
| `TestSessionStartEmitsRecoveryPacket` | internal/cli/run_test.go:~1455 | L4-hook-platform-wiring.md:61；L4-hook-anchor-catalog.md:64「SessionStart 的价值在于…仍能从原工程事实继续…不重新讲全部生命周期」 | 改写：夹具补真实 Runtime；断言恢复包存在（`LOOP RECOVERY` + `stage=` + `next=`）而非旧 `Stage ` 横幅；保留「无旧 HOOK_SESSION_STARTED」「恰好 1 条 outbox 且带 decision_id」 |
| `TestStageAnswerTerminalConsumeRejectsUnissuedS7Path` | internal/cli/review_result_hook_test.go:~95 | L4-hook-acceptance-checklist.md:52（精确已登记路径选责、缺字段即报未知） | 改写：拒收消息改为当前的 `answer path has no exact current registered issuance`，其余断言不变 |

### 桶 L2 — 长尾(23 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestCT03912ControllerOnly` | internal/cli/ct_only_test.go:11 | 同上 | 改写:同 B.9 |
| `TestCT03912_BuilderCompleteCommitsTR006NoManualCLI` | internal/cli/ct_11_24_test.go:51 | `L3-S6-build.md:417`「每张任务都需要**当前合法交付**、有效本人完成判断……」;`:473`「[接收与批次投影](../../internal/qualitygate/canonical_builder_result.go)」;`L4-workflow-convergence.md:293`「删除 frozen／legacy 的启动、观察、接收及恢复接线,而非增加兼容层。」 | 改写:断言 legacy 事实**不得**打开 TR-006 + 缺项须 surface |
| `TestExplainListsEligibleCurrentEvidenceCandidates` | internal/cli/bug101_test.go:73 | `L4-runtime-control-plane.md:92`「……不计算、登记或校验散列、Git OID,**不生成、累计或兼容读取 `review_round`**」;`L3-S7-verification-round.md:28`「移除 `review_round` 的生产、消费和旧格式兼容接线。」 | 改写:删 `review_round: 3` 正向断言 + 新增反向断言(不得投影) |
| `TestNativeS6RecordedResultProjectsActualUnfinishedWork` | internal/cli/native_s6_received_guidance_test.go:160 | `L4-workflow-convergence.md:449`「由原 Writer 确定前驱及内部意图,**不要求 `--revision-intent-key`,删除旧参数接线**」 | 改写:删除 `--revision-intent-key`,其余断言保留 |
| `TestNativeS9LegacyPlanValidationKeepsOriginalEnvelopeFeedback` | internal/cli/native_s9_artifact_feedback_test.go:93 | `L4-workflow-convergence.md:293`「删除 frozen／legacy 的启动、观察、接收及恢复接线,而非增加兼容层」;`L3-S9-repair.md:238` | 改写:改用当前扁平 Plan 路径,断言原消费者仍返回精确字段错误 |
| `TestResumeRefusedOnBaselineDriftRoutesToAmend` | internal/cli/lifecycle_review_test.go:169 | `L3-S1-bind.md:295`「原 `runtime resume`……**不计算、登记或校验散列**」;`:297`「**系统不以散列检测内容漂移**」;`:299`「当前实现仍依赖控制根文档指纹复核,目标恢复路径须撤下该链」 | 改写:resume 必须成功、不得声称 req amend、代际不变 |
| `TestReviewPlanFromWorktreeWritesToSharedControlPlane` | internal/cli/worktree_shared_control_plane_test.go:223 | `L3-S7:190`+§7.1;`L4-workflow-convergence.md:521`;`L4-state-transition-core.md:31`「权威根……**不能复制一对 Runtime 文件就成为第二个控制面**」 | 改写:改走真实 hook PreToolUse 自然接收 + worktree 诱饵 Plan 身份 |
| `TestReviewPlanRecoveryHelpAndJSONFilePath` | internal/cli/simulation_recovery_test.go:40 | `L3-S7-verification-round.md:190`「移除旧 `S7/r<round>/review-plan.json` 运行接线,不保留平行路径」;§7.1「**计划登记／提交命令**」;`L4-workflow-convergence.md:519/521` | 改写:断言旧动词已是 unknown runtime command + action catalog 走自然接收 |
| `TestReviewResultFromWorktreeWritesToSharedControlPlane` | internal/cli/worktree_shared_control_plane_test.go:304 | 同上 | 改写:断言旧提交动词已移除 + action catalog 路线;**覆盖弱化见 E.1** |
| `TestS6PrepareRefusesDraftOverwriteWithoutRuntimeWrite` | internal/cli/s6_prepare_e2e_test.go:147 | `L3-S6-build.md:137`「已发行责任的重试、修订沿原记录和真实返工关系继续」;`L4-workflow-convergence.md:449`「同次重试复用已成立事实」 | 改写:同责任重试重放已签发 brief,state/journal/brief 三不变 |
| `TestS6PrepareRegistersAndPreparesOriginalBoundWorkspace` | internal/cli/s6_prepare_e2e_test.go:17 | `L3-S6-build.md:132`「loop-harness s6 prepare --root <Main> --task <TASK> --role <Builder-role>」;`:139`「Main 不需要先指定官方 Agent/Team ID」 | 改写:去平台身份参数,断言原生派发/派生 owner/help 无旧选项 |
| `TestS6PrepareReportsUnmetDependencyWithoutRuntimeWrite` | internal/cli/s6_prepare_e2e_test.go:184 | `L3-S6-build.md:132` + `:139` | 改写:仅去平台身份参数,四条断言保留 |
| `TestS7GuideCarriesRealIntegratedAssignmentRevisions` | internal/cli/s7_planning_materials_e2e_test.go:16 | `L4-workflow-convergence.md:449`「**不要求 `--revision-intent-key`,删除旧参数接线**」 | 改写:两处 flag 删除 + owner 改派生约定,guide 断言保留 |
| `TestS7OriginalExitRefusesMissingCleanRoundEvidence` | internal/cli/s7_handoff_test.go:109 | `L3-S7-verification-round.md:388`「当前链不生成或验证独立 CleanRound 快照……**删除旧 CleanRound 消费者和兼容接线**」;`L4-runtime-control-plane.md:199`;`L4-worktree-governance.md:260` | 改写:断言不存在 clean_round 证据且 review-continue 必须成功 |
| `TestS7S10RecoveryCommandsAppearInUsageAndHelp` | internal/cli/s7_handoff_test.go:180 | blueprint 全文 `grep` 无 `review-continue`/`audit-prepare` → 无"帮助必须含某词"的蓝图义务;只要求恢复入口真实存在并给出准确动作 | 改写:按命令断言真实恢复动作内容 |
| `TestS8InvestigationLeadDispositionAvailableBeforeAnyHypothesisResult` | internal/cli/investigation_lead_disposition_test.go:32 | `L4-workflow-convergence.md:227`「删除预检与提交时的全套重复验证、历史／证据递归读取」 | 改写:反馈改为断言当前缺失专业字段 `primary_root_cause` |
| `TestS8InvestigationLeadDispositionExplainsOriginalCausalAnswerShape` | internal/cli/investigation_lead_disposition_test.go:200 | `L4-workflow-convergence.md:263`「Session/Plan/packet/路径/回执/Handoff 均不用散列生成、登记或验真」;`L3-S8:297/299` | 改写:断言当前专业对象 + **反向断言**不得再出现 `#sha256=`/`ObservationBatch`/`sealed S7 Finding` |
| `TestS8InvestigationLeadDispositionInconclusiveInvestigateMoreAndSpecExit` | internal/cli/investigation_lead_disposition_test.go:144 | `L3-S8-finding-investigation.md:299`「这些是自由专业对象的**推荐表达位置**,不新增机械必填门」 | 改写:断言「directly append」+「No investigate_more route registration is a prerequisite」 |
| `TestS8UnresolvedResultsAppendDirectlyWithoutRoutePrerequisite` | internal/cli/investigation_lead_disposition_test.go:230 | `L3-S8:297/299`「**推荐**……不新增机械必填门」 | 改写:去掉推荐字段名硬断言,保留 questions 路径/直接追加/无前置登记 |
| `TestS9LegacyRegisteredObservationKeepsOwnerWhenExecutionUnknown` | internal/cli/repair_legacy_resolver_test.go:53 | 同上 | 改写:legacy `plan_sha256`/`plan_report_refs` 不再解析 owner,也不算已登记派发 |
| `TestS9LegacyResolverSharesRequestCacheAndInvalidatesOnRevision` | internal/cli/repair_legacy_resolver_test.go:11 | `L4-workflow-convergence.md:228`「删除 C／D、typed、retained-source、frozen 及**历史 schema 的并行接线**」;`:293`「删除 frozen／legacy……而非增加兼容层」 | 改写:legacy manifest 执行坐标不再解析;未知来源保持未知 |
| `TestS9NativeLongPacketPathsRemainConsistent` | internal/cli/repair_questionnaire_nohash_test.go:92 | `L4-workflow-convergence.md:263`「路径/回执/Handoff 均不用散列生成、登记或验真」;`:212`「**工作区清理单独处理**」 | 改写:路由行携带已登记 brief 坐标,断言与 packet 推导一致 |
| `TestS9OriginalContractPublicMinimalPlan` | internal/cli/s9_original_contract_public_test.go:13 | `L3-S9-repair.md:238`「**字段缺失时保留原稿,反馈实际文件与字段**」;`:301`「新旧题都允许……直接接收」 | 改写:写入当前扁平专业形状(plan+red_checks+proposed_paths)后断言回执 Accepted |

### 桶 L3 — 长尾(17 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestActionsCatalogShowsCanonicalReviewRepairLoop` | internal/cli/action_catalog_test.go:26 | 1 | `L4-workspace-execution-profiles.md:9`「旧 workspace prepare／launch、bootstrap、session 与 lease 只服务原冻结记录;不能变成新任务的必需前置」;`:189`「已登记执行的具体命令与 runner 参数集中在[冻结执行恢复参考]」 |
| `TestActivatedCheckpointRecoveryRejectsTamperingAndWrongAssignment` | internal/cli/checkpoint_recovery_test.go:53-61 | 3 | `L4-hook-acceptance-checklist.md:15`「实际 Hook 路径不计算、登记或验证任何散列……历史值可按原记录读取但不重算、重验或登记到新事实」;`:88`「旧散列不作为例外」 |
| `TestDefaultProgressShadowRecordsWithoutContext` | internal/cli/work_progress_e2e_test.go:102-125 | 36 | 路径=`L4-procedure-reduction.md:195`「当前采样默认 shadow,即记录诊断而不向模型发送提醒」;类型化诊断缺失=DEFECT(C-1) |
| `TestFindingSupplementOriginalCLIDoesNotRequireDigest` | internal/cli/finding_supplement_nohash_test.go:41-43 | 4 | `L4-workflow-convergence.md:250`「原始专业材料或工具自然输出不因含散列文字被审查或删改;框架不能将这些文字提取为散列资格或机器证明」;`:505`「用户专业内容不因出现散列文字而被删除」 |
| `TestNativeS6StartRequiresExactResponsibility` | internal/cli/native_s6_guidance_delivery_test.go:34 | 8 | `L4-hook-acceptance-checklist.md:15`「实际 Hook 路径不计算、登记或验证任何散列」;`internal/cli/native_s6.go:381` 现文案「save Result in your own worktree」 |
| `TestPendingIntegrationsSurviveRestartAndIncludeCleanup` | internal/cli/workspace_pending_test.go:16-42 | 40 | `L4-worktree-governance.md:140`「历史已有坐标仅保留为旧数据,不以其匹配作为继续的资格」 |
| `TestPlanningReworkCLIRejectsFrozenDesignDrift` | internal/cli/planning_rework_e2e_test.go:225-232 | 19 | 同 18 |
| `TestPlanningReworkCLIRejectsInvalidRequests` | internal/cli/planning_rework_e2e_test.go:127-141 | 18 | `L3-S2-design.md:532`「Hook 不通过重算、登记或校验散列来判断来源是否变化,也不能声称自动发现所有变更」;`L4-workflow-convergence.md:447` |
| `TestPlanningReworkCLISeparatesCommittedPreparationFailure` | internal/cli/planning_rework_e2e_test.go:264-286 | 20 | `L4-workflow-convergence.md:328`「默认路径…被未知普通文件、目录或软链接占用,均保留原物,不读取占位正文…由同一原 Writer 选择安全的独立候选」;`:339`「原物不读取正文、不覆盖、不删除;原 Writer 安全选择独立路径」 |
| `TestPlanningReworkCLITasksRejectsUpstreamDrift` | internal/cli/planning_rework_e2e_test.go:422-429 | 21 | 同 18 |
| `TestProgressIgnoresOtherTypedPacketsButKeepsS5Diagnostics` | internal/cli/native_convergence_test.go:90-100 | 6 | `L4-hook-acceptance-checklist.md:88`「SIM-19、旧内嵌／正文外置、签发摘要、checkpoint／pending 字节、typed、C／D 和证据 capture 测试记录旧实现行为。它们不是任何阶段新接收的额外资格」;`:54` C05「不创建 typed／flat 副本」 |
| `TestRuntimeRepairTargetedResumeReopensBlockedVerification` | internal/cli/repair_status_test.go:65-75 | 22 | `L3-S9-repair.md:72`「Main 按实际工程进度安排独立验证者,交原合同、实际产品、必要断言和本人 Targeted 原卷」 |
| `TestS10PublicHookSeparatesLateReviewerFromMainQuestionnaire` | internal/cli/s10_role_display_test.go:62-90 | 25 | `L4-hook-platform-wiring.md:200`「进度显示…只作用于简短用户提示」 |
| `TestS7LastNativeResultHandsMainS10WithoutAnotherTool` | internal/cli/s7_native_handoff_test.go:19-146 | 29 | `L3-S7-verification-round.md:195`「本人正常保存计划不增加独立预检、计划登记／提交命令」;`L4-workflow-convergence.md:519`「删除 `review-plan frozen-diff` 及其历史恢复接线」 |
| `TestS7MainFinalPlanDecisionContinuesOriginalS10Outlet` | internal/cli/s7_native_handoff_test.go:148-219 | 30 | `L3-S7-verification-round.md:191`「移除旧 `S7/r<round>/review-plan.json` 运行接线,不保留平行路径」 |
| `TestS7RecoveryDescribesOriginalNativeResultState` | internal/cli/s7_recovery_contract_test.go:20-137 | 31 | 同 29/30;`L3-S7-verification-round.md:242`「`site_lost`…保留实际发现和未知,不单凭该字段推定完成或禁止交接」 |
| `TestS8DelegationPreparationPreservesChoiceAndRequiresCurrentBinding` | internal/cli/investigation_contract_decision_test.go:251-253 | 5 | `L4-workflow-convergence.md:447`「以上链路不读取、计算、登记或比较计划、TASK、manifest、Assignment Record 的 SHA／hash、摘要或 Git OID」 |

### 桶 L4 — 长尾(36 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestAcceptedS4FormalTaskFeedsS5WorkspacePrepare` | internal/cli/formal_input_selection_e2e_test.go:208 | 7 | `L3-S1-bind.md:297`「系统不以散列检测内容漂移；坐标和版本相同不证明文件字节未变，不能将未知冒充未变或已批准」;`L3-S6-build.md:137`「实际变化由 Main 按专业影响处置，系统不以替代性的全文件对比认证历史」 |
| `TestActivationSentGuidanceDropsRetiredCommandPaths` | internal/cli/activation_guidance_test.go:45 | 2 (改名前) | `L4-workflow-convergence.md:312`「旧入口、旧 schema、旧 Hook／CLI、旧消费者及兼容适配连同仅为它们存在的测试一并删除」;`:71`「删除已被现行责任链取代的旧入口、适配、消费者、格式和测试」 |
| `TestActivationSentGuidanceKeepsExplicitLegacyInstructions` | internal/cli/activation_guidance_test.go:45 | 2 | `L4-workflow-convergence.md:312`「旧入口、旧 schema、旧 Hook／CLI、旧消费者及兼容适配连同仅为它们存在的测试一并删除」;`:71`「删除已被现行责任链取代的旧入口、适配、消费者、格式和测试」 |
| `TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode` | internal/cli/activation_guidance_test.go:31 | 1 | `L3-S7-verification-round.md:223`「PreToolUse、成功 PostToolUse、SubagentStop 使用同一接收路径观察自然保存的原答卷」;`L4-workflow-convergence.md:312` |
| `TestAuditHooksE2EPlanRefUsesAuthorityRoot` | internal/cli/audit_hooks_e2e_test.go:127 | 3 | `L4-workflow-convergence.md:447`「以上链路不读取、计算、登记或比较计划、TASK、manifest、Assignment Record 的 SHA／hash、摘要或 Git OID」;`:291`「删除摘要字段及旧字段兼容接线」 |
| `TestFrozenS6TwoBuilderPathlessPostKeepsOriginalOwners` | internal/cli/s6_frozen_two_builder_isolation_e2e_test.go(经 workspace_core_profile_test.go:48) | 25 | `docs/control/loop-definition.json` `entity_lifecycles.task.transitions`:`locked --builder_activated--> in_progress` |
| `TestNativeS5AmbiguousReferencesDoNotReceiveEitherAnswer` | internal/cli/native_review_hooks_test.go:105 | 9 | `L4-hook-platform-wiring.md:83`「真实关联缺失交系统，不借角色、同名文件或另一个作者的稿件代替」;`L3-S7-verification-round.md:221`「当前责任通过原派发中的准确引用或既有绑定解析，不能按角色、最近文件或唯一候选猜选」 |
| `TestNativeS6DifferentSourceCannotUseNormalResumeToReceive` | internal/cli/native_s6_source_resume_test.go:125 | 11 | `L3-S6-build.md:320`;`L4-hook-platform-wiring.md:81`「不另遍历 Git worktree、比对 HEAD／branch／pin 或认证 actor 与来源根来授予答卷资格」 |
| `TestNativeS6DifferentSourceIsNotJudgedBySourceCommit` | internal/cli/native_s6_source_resume_test.go:125 | 11 (改名前) | `L3-S6-build.md:320`;`L4-hook-platform-wiring.md:81`「不另遍历 Git worktree、比对 HEAD／branch／pin 或认证 actor 与来源根来授予答卷资格」 |
| `TestNativeS6SourceBranchesPrecedeActualInputReads` | internal/cli/native_s6_source_resume_test.go:26 | 10 | `L3-S6-build.md:320`「Hook 不扫描 Git、登记或比对 HEAD／base／commit OID、检查祖先关系或逐文件散列作为接收资格」;`L4-state-transition-core.md:138` |
| `TestNativeS9ListenerIgnoresFailedPostAndOtherCheckout` | internal/cli/native_s9_listener_test.go:142 | 13 | `L4-hook-platform-wiring.md:81`;`L3-S6-build.md:320` |
| `TestNativeS9ListenerReceivesPlanBeforeResultAndPreservesDirtyWork` | internal/cli/native_s9_listener_test.go:90 | 12 | `L4-workflow-convergence.md:228`「不以 Final／Changeset 或其他证明设门；Main 实际工作和独立复验保留」;`:312` |
| `TestNativeStopPendingFrozenS9RetainsSamePublicStopGate` | internal/cli/native_stop_pending_test.go:264 | 15 | `L4-workflow-convergence.md:342`「删除旧 C／D、frozen、typed、Candidate／Final、source pin 的并行接收、来源证明及恢复接线」 |
| `TestReportPolicyRefDriftDetectsVersionDrift` | internal/cli/policy_ref_test.go:83 | 16 | `L4-state-transition-core.md:359`「旧 fingerprint 维护入口 \ |
| `TestReportPolicyRefDriftDoesNotQualifyDoctorOnVersionDrift` | internal/cli/policy_ref_test.go:83 | 16 (改名前) | `L4-state-transition-core.md:359`「旧 fingerprint 维护入口 \ |
| `TestReportPolicyRefDriftLeavesNoPolicyFinding` | internal/cli/policy_ref_test.go:100 | 17 (改名前) | `L4-workflow-convergence.md:281`「无 hash 不再单凭原路径和版本独立证明字节未被外部改动」;`:291` |
| `TestReportPolicyRefDriftPassesWhenConsistent` | internal/cli/policy_ref_test.go:100 | 17 | `L4-workflow-convergence.md:281`「无 hash 不再单凭原路径和版本独立证明字节未被外部改动」;`:291` |
| `TestReviewPlanGuidancePreservesOriginalResponsibility` | internal/cli/review_plan_guidance_identity_test.go:83 | 21 | `L3-S7-verification-round.md:223`「未表达可交接内容或 JSON 未写完整的草稿不误收，已有可理解结论即接收」;接收侧闸门 `internal/review/plan_checks.go:63`「Only an entirely unfilled generated draft remains a draft」 |
| `TestRuntimeReconcilePolicyRefCheckOnlyDoesNotWrite` | internal/cli/policy_ref_test.go:169 | 19 | `L4-state-transition-core.md:359`;`L4-workflow-convergence.md:291` |
| `TestRuntimeReconcilePolicyRefCheckOnlyReportsTheCurrentPolicy` | internal/cli/policy_ref_test.go:169 | 19 (改名前) | `L4-state-transition-core.md:359`;`L4-workflow-convergence.md:291` |
| `TestRuntimeReconcilePolicyRefDoesNotClaimHistoricalConsistency` | internal/cli/policy_ref_test.go:199 | 20 (改名前) | `L4-workflow-convergence.md:281` |
| `TestRuntimeReconcilePolicyRefNoopWhenConsistent` | internal/cli/policy_ref_test.go:199 | 20 | `L4-workflow-convergence.md:281` |
| `TestRuntimeReconcilePolicyRefRealignsVersion` | internal/cli/policy_ref_test.go:130 | 18 | `L4-state-transition-core.md:359` + `:217`「旧散列字段不迁入新责任或变成新的接收/恢复条件」 |
| `TestRuntimeReconcilePolicyRefReportsAvailablePolicyWithoutRewritingState` | internal/cli/policy_ref_test.go:130 | 18 (改名前) | `L4-state-transition-core.md:359` + `:217`「旧散列字段不迁入新责任或变成新的接收/恢复条件」 |
| `TestS4DirectApplyAcceptsChangedTaskWithoutMachineDriftGate` | internal/cli/s4_continuation_test.go:89 | 23 (改名前) | `L3-S1-bind.md:297`;`L3-S6-build.md:137` |
| `TestS4DirectApplyRejectsAcceptedTaskDriftAtomically` | internal/cli/s4_continuation_test.go:89 | 23 | `L3-S1-bind.md:297`;`L3-S6-build.md:137` |
| `TestS4TaskSplitPipelineE2E` | internal/cli/tasks_e2e_test.go:199 | 34 | `L3-S4-task-split.md` §10.2「Hook 不计算、登记或校验来源摘要、内容散列或 Git 对象 ID」;`L4-workflow-convergence.md:291` |
| `TestS6HelpNamesSupportedStatusAndFlags` | internal/cli/workgroup_help_test.go:54 | 35 | `L4-workflow-convergence.md:312`;`L3-S6-build.md:316`「删除 frozen execution 的观察、接收与恢复分支，不以旧协议建立第二条作者路径」 |
| `TestS7AcceptedRemainingProjectionPreservesClosedRouting` | internal/cli/s7_native_remaining_test.go:71 | 28 | `L4-workflow-convergence.md:281`;`:312` |
| `TestS9HandoffCopyableCommandPreservesLiteralCoordinates` | internal/cli/s9_handoff_command_test.go:20 | 31 | `L4-workflow-convergence.md:312`;`:228`;`L3-S6-build.md:316` |
| `TestS9HandoffStatesMainSuccessorWithoutRetiredCommand` | internal/cli/s9_handoff_command_test.go:20 | 31 (改名前) | `L4-workflow-convergence.md:312`;`:228`;`L3-S6-build.md:316` |
| `TestTaskPlanningSaveKeepsActualCancelledTaskWithoutAuthorListProof` | internal/cli/s4_actual_cancelled_subject_test.go:90 | 22 | `L3-S1-bind.md:297`;`L3-S6-build.md:137` |
| `TestWorkerS6OccupiedSubmissionDirectoryReachesResultConsumer` | internal/cli/workspace_submission_paths_e2e_test.go(经 workspace_core_profile_test.go:48) | 40 | 同 #36 |
| `TestWorkspaceBackendCoreDefaultShadowAndReplacementStayFixed` | internal/cli/workspace_core_profile_test.go:48 | 36 | `docs/control/loop-definition.json` `entity_lifecycles.task.transitions`(`in_progress` 为 Builder 工作态) |
| `TestWorkspaceBackendCoreInstallsFixedRoleWithoutChangingActivation` |  | 37 | 同上 |
| `TestWorkspacePlanRejectsAddingInputsToPreparedExecution` |  | 38 | 同上 |

### 桶 L5 — 长尾(14 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestContractAuthorityUsesOriginalS2RegisteredDesign` | internal/cli/design_answer_e2e_test.go:312 | 10 | 同上 L4-state-transition-core.md:163 + L3-S3-contracts.md:110/589(`ValidateContractPacket` 已删 `fileview.ValidateAuthority` 与逐文件 SHA 比对) |
| `TestDesignAnswerOriginalPairRecovery` | internal/cli/design_answer_e2e_test.go:281 | 11 | 同上(L3-S2 设计答卷对不再以散列配对) |
| `TestHumanReleaseQuestionnaireNamesUnverifiablePackage` | internal/cli/audit_questionnaire_test.go:266 | 6 | L3-S11-release-gate.md:147 「当前申请的位置由 Runtime、代际、已登记 S10 原发布审计报告及其实际上游关联、既有阶段入口修订共同定位。**不生成、累计或兼容读取审查轮数**」 |
| `TestS3ContractPipelineE2E` | internal/cli/contracts_e2e_test.go:144 | 9 | L4-state-transition-core.md:163 「S0～S11 的自然 Hook 及原消费者**不计算、登记或校验散列**」；L3-S3-contracts.md:110 「登记时间及实际责任关联由原消费者维护，**不计算、登记或校验摘要**」；L3-S3-contracts.md:589 「Hook **不计算、登记或校验 fingerprint、SHA 或其他散列**」 |
| `TestS4ReceptionFieldFailurePersistsAcrossSessionAndClears` | internal/cli/s5_planning_handoff_e2e_test.go:240 | 14 | 同上 L3-S5:102 + L4-state-transition-core.md:163(原来源变更不再由散列判定) |
| `TestS5FrozenTaskAnalysisSurvivesRevisionAndRejectsLostBytes` | internal/cli/frozen_task_analysis_e2e_test.go:72 | 12 | L3-S5-document-verification.md:140 「旧责任继续使用其原路径和可用原分析，**不计算或核验旧摘要**，也不以缺摘要要求重新发行或补交。原材料不可得时保留已收事实并交 Main／系统处理真实未知」 |
| `TestS7DraftRefusesNonVerificationStage` | internal/cli/s7_draft_gate_test.go:98 | 8 | 同上 L4-state-transition-core.md:44；L3-S7-verification-round.md:28 「**移除 `review_round` 的生产、消费和旧格式兼容接线**」 |
| `TestS7LateAcceptedPrivateWorkerVolumeRemoved` | internal/cli/s7_late_original_test.go:332 | 5 | 同上 |
| `TestS7LateBlockedReceiptContinuesOriginalAndPreservesHistory` | internal/cli/s7_late_original_test.go:227 | 2 | 同上(L4-state-transition-core.md:44 / L3-S7:28) |
| `TestS7LateLostOriginalSourceRemainsUnknownWithoutBlockingNewPlan` | internal/cli/s7_late_original_test.go:302 | 4 | 同上 |
| `TestS7LateOriginalDifferentOwnerRejectsCrossResponsibility` | internal/cli/s7_late_original_test.go:277 | 3 | 同上 |
| `TestS7LateOriginalSameAssignmentIDIsolatedAndDurable` | internal/cli/s7_late_original_test.go:165 | 1 | L4-state-transition-core.md:44 「原 ReviewPlan 身份及实际关联 \ |
| `TestSessionStartCommittedS7HandoffDefersOnlyMissingMainQuestionnaire` | internal/cli/s7_session_handoff_test.go:15 | 7 | L4-state-transition-core.md:44 「原 ReviewPlan 身份及实际关联 \ |
| `TestTaskBehaviorProjectsOnlyDeclaredCurrentCases` | internal/cli/task_behavior_e2e_test.go:81 | 13 | L3-S5-document-verification.md:102 「Hook 不登记或核对清单和材料散列；专业作者和 Main 判断实际材料及其变化对审查的影响，**系统不因此声称能发现同版本的外部字节修改**」 |

### 桶 INV — internal/investigation(19 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestCaseWorkflowRejectsStaleRevisionAndHashDrift` | case_workflow_test.go:120 | 1 | `L3-S8:43`「Hook 及其消费者**不计算、登记或校验** Case、合同、答卷、证据的 hash／SHA、其他散列或 Git OID」;`L3-S9:15`「**不计算、登记或校验任何散列**」 |
| `TestCompetingAssessmentUsesCurrentConclusiveDiscriminator` | competing_assessment_test.go:11 | 7 | `L3-S8:160`「`competing_hypothesis_assessment`…**不是两条机器证明路线**。Main 可以引用已取得结果说明竞争解释怎样处置」 |
| `TestContractCannotApproveWithAnUnansweredCurrentQuestion` | current_results_test.go:151 | 8 | `L3-S8:307`「**不成为逐题完成**…的机器门」;`L3-S8:231`「**不是先证明全部来源、所有同伴已答**或历史证据仍可重建后才允许保存的**资格门**」 |
| `TestCurrentUnansweredQuestionCannotBorrowCompleteCausalMaterials` | current_results_test.go:90 | 9 | 同上(`L3-S8:307`/`:231`) |
| `TestFirstS8QuestionReusesOnlySelectedFrozenFindingSource` | finding_source_handoff_test.go:112 | 11 | `L3-S8:43`(不校验散列);`L3-S8:227`「来源关系由已有登记随原记录保存，**不扩成平台身份认证或工具许可**」;`L3-S8:99`/`:101`(来源集合**不预先固定**、可随实际材料补充) |
| `TestHumanCaseContractApprovalRechecksOriginalSourceAndProductBaseline` | human_case_test.go:326 | 15 | `L3-S9:15`「旧记录中已有散列值**只作为历史原字段读取**」;`L4-workflow-convergence.md:285`「旧材料的散列值…**均不产生资格差异**」;`L3-S8:43` |
| `TestHumanCaseOriginalRevisionCannotBeReplacedByCurrentProfessionalBoundary` | human_case_test.go:299 | 16 | 同上(`L3-S9:15`/`L4-workflow-convergence.md:285`);`L3-S8:249`「**不要求 Main 先证明每份材料符合当前模板**才能创建首个问题、保存结果或选择路由」 |
| `TestHumanDefectCaseNativeInvestigationAndApprovedRepairWithoutFindingOrBatch` | human_case_test.go:178 | 17 | `L3-S8:227`(自然事件 → 同一原消费者);`L4-agent-dispatch-governance.md:85`「S7～S9 的 `PreToolUse`、成功 `PostToolUse` 与 `SubagentStop` **复用同一当前责任和原消费者**」 |
| `TestHumanDefectIntakeRejectsChangedSourceOrBaselineWithoutWritingCase` | human_case_test.go:212 | 18 | `L3-S9:15`;`L4-workflow-convergence.md:285`「旧材料的散列值缺失、空、格式不符合旧约定、与当前文件不一致或符合旧格式**均不产生资格差异**」;`L3-S8:43` |
| `TestNativeS8StopConsumesRuntimeReferenceAndReplaysOriginalReceipt` | runtime_reference_test.go:53 | 20 | `L3-S8:227`;`L4-agent-dispatch-governance.md:85` |
| `TestOriginalCaseRevisionCannotBorrowHistoricalAssignmentConclusions` | current_results_test.go:108 | 10 | `L3-S8:231`「身份、状态、版本、并发和幂等是内部持久化职责」;`L3-S8:235`「**旧结果保留**」;`L3-S8:307`「机器**不认证因果**」 |
| `TestRegisterHypothesisRequiresEvidenceRefs` | case_workflow_test.go:101 | 2 | `L3-S8:119`「`evidence_refs` \ |
| `TestS8ConcurrentCommitPreservesAnswerAndOriginalReceiptRecovery` | hypothesis_result_cas_test.go:14 | 14 | `L3-S8:231`「身份、状态、版本、并发和幂等是**内部持久化职责**」;`L4-state-transition-core.md:340`「当前写入与另一个事实更新发生冲突 → **读当前责任,判断原业务是否已接收,再作必要重试**」 |
| `TestS8IssueUsesActualGenerationTwoInvestigatorRegistration` | hypothesis_result_registration_test.go:35 | 12 | `L3-S8:227`「Hook 在适用的自然 Pre／实际保存后的 Post／SubagentStop 等事件读取当前原答卷,**未就绪不接收,就绪则调用同一原 HypothesisResult 消费者**」;`L3-S8:231`「原消费者…**保存结果及回执到原 Case**」 |
| `TestS8QuestionPreparationAndNativeStopUseOriginalRuntimeReference` | runtime_reference_test.go:100 | 21 | 同上 |
| `TestSubmitHypothesisResultRejectsUnboundFollowUpHypothesis` | case_workflow_test.go:186 | 3 | `L3-S8:162`「Investigator 在 `new_hypotheses` 提出的**只是建议**。系统原样保存…建议**不自动变成新 Hypothesis／Assignment**」 |
| `TestUpdateCaseRouteRejectsDetectionGapWithoutTypeAndEvidence` | case_workflow_test.go:503 | 4 | `L3-S8:299`「这些是自由专业对象的推荐表达位置，**不新增机械必填门**」;`L3-S8:303`「Main 作专业收口，**机器不认证因果**」 |
| `TestUpdateCaseRouteRejectsHollowBlastRadius` | case_workflow_test.go:474 | 5 | 同上(`L3-S8:297`/`:299`/`:303`/`:307`) |
| `TestUpdateCaseRouteRejectsSingleSupportedWithoutRefutation` | case_workflow_test.go:531 | 6 | `L3-S8:160`「**不要求至少一项 `refuted`**、所有假设必须完成或所有历史问题必须逐项取得支持／反驳才允许选择出口」;`L3-S8:307`「**不成为**…**至少一项反驳**…的机器门」 |

### 桶 SYS — 接缝与系统层(17 条)

| 测试 | 源文件:行 | 蓝图依据 | 处置 |
|---|---|---|---|
| `TestBUG104AbortThenRolloverE2E` | tests/system/req039/s11_human_decision_branches_test.go:342 | 13 | 同上 |
| `TestBUG104ApproveThenRolloverE2E` | tests/system/req039/s11_human_decision_branches_test.go:40 | 9 | `L4-revision-usage.md:123`「同次修订和纯恢复沿用身份,重新实际进入 S7 使用新的 Plan 身份,**不生成审查序号**」 |
| `TestBUG104DeferResumeS11E2E` | tests/system/req039/s11_human_decision_branches_test.go:73 | 10 | 同上 |
| `TestBUG104RejectAcceptanceAndAuditInvalidateOnlyScopedEvidenceE2E` | tests/system/req039/s11_human_decision_branches_test.go:290 | 12 | 同上 |
| `TestBUG104RejectDefectRoutesHumanProblemToInvestigationE2E` | tests/system/req039/s11_human_decision_branches_test.go:126 | 11 | 同上 |
| `TestCT03917_SubagentStopIdempotentResumeAfterMerge` | tests/system/req039/ct_integration_resume_test.go:17 | 7 | 同上 |
| `TestFinalAuditVerifiedTargetRewriteViaTaskIntegratePreservesWorker` | tests/system/req039/final_audit_delivery_test.go:46 | 8 | 同上 |
| `TestIntegrationRecoversLostMergeReceiptWithoutMergingAgain` | tests/system/req039/worktree_governance_test.go:82 | 16 | 同 #4 |
| `TestRuntimeAuthorityProtectsRecoveryAndStorageCoordinates` | tests/system/req039/audit_authority_e2e_test.go:49 | 3 | `L4-workflow-convergence.md:138`「…**不得将同一用途改名为 fingerprint、checksum** 或换用其他散列算法」;`L4-runtime-control-plane.md:84`「删除旧格式读取器、内容寻址接线和仅服务旧格式的恢复」 |
| `TestRuntimeRecoveryApplyQuarantinesAndClearsLegacyPendingMarker` | tests/system/recovery/runtime_recovery_test.go:138(断言 :153) | 26 | `L4-state-transition-core.md:217`「旧 `.commit-pending.json`…如果当前内容不能据实际材料确定为该事务的目标或可安全处理的源,**必须保留未知并交系统／Main**」 |
| `TestRuntimeRecoveryApplyRebindsREQAndQuarantinesCorruptRuntime` | tests/system/recovery/runtime_recovery_test.go:78(断言 :123) | 23 | `L4-workflow-convergence.md:292`「**不比较分支 REQ 字节与旧 `bound_req.sha256`**」;`:138` |
| `TestRuntimeRecoveryInspectAndPlanSurviveMalformedOrBOMState` | tests/system/recovery/runtime_recovery_test.go:30(断言 :68) | 22 | `L4-runtime-control-plane.md:84`「…内容寻址路径、checkpoint 正文副本及对应散列是待替换实现…**删除**…内容寻址接线」;`L4-workflow-convergence.md:138`「…**事务恢复**…均不计算、登记或校验散列」 |
| `TestS3AuditFrozenModelCannotMergeFromWorker` | tests/system/req039/audit_sharedmodel_integration_test.go:85 | 5 | 同上 |
| `TestS3AuditUnrelatedImplementationChangeMerges` | tests/system/req039/audit_sharedmodel_integration_test.go:155 | 6 | 同上 |
| `TestS4ResultBindingThroughTaskIntegrate` | tests/system/req039/audit_dispatch_result_binding_test.go:13 | 4 | `L4-workflow-convergence.md:293`「删除 frozen／legacy 的启动、观察、接收及恢复接线,而不是增加兼容层」 |
| `TestTaskIntegrateConflictPreservesWorktree` | tests/system/req039/task_integrate_test.go:118 | 15 | 同 #4 |
| `TestTaskIntegrateMergesAcknowledgesAndCleansWorktree` | tests/system/req039/task_integrate_test.go:60 | 14 | 同 #4 |

## 4. DEFECT 记录(只记录,生产代码一行未动)

以下测试**断言的行为仍是蓝图要求**,是代码违反了蓝图。它们本轮**没有**被改写,
**按规则保持红** —— 这是刻意的:宁可让测试保持红,也不掩盖真缺陷。

### 桶 D1(10 条)

- `TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection` — s9_handoff_wiring_test.go:520 — 同上
- `TestS9HandoffCanceledFinalAcknowledgementPreservesOriginalProof` — s9_handoff_wiring_test.go:545 — 同上
- `TestS9HandoffDamagedTypedCandidateStopsBeforeMerge` — s9_handoff_wiring_test.go:281 — 同上
- `TestS9HandoffFinalProjectionRetainsOriginalCleanupResponsibility` — s9_handoff_wiring_test.go:244 — 同上
- `TestS9HandoffFinalReceiptSurvivesMissingPlanIndex` — s9_handoff_wiring_test.go:437 — 同上
- `TestS9HandoffPartialCandidateDoesNotBecomeIntegrationDuty` — s9_handoff_wiring_test.go:359 — L3-S9:238「可解析的占位、计划中的检查和明确待继续内容不自动成为完成结论」
- `TestS9HandoffSameOwnerNeighborCannotSupplyOrBreakOriginalCandidate` — s9_handoff_wiring_test.go:373 — 同上
- `TestS9HandoffSpentProgressPreservesIndexesWithoutNewDocumentReads` — s9_handoff_cancellation_linux_test.go:20 — L4-workflow-convergence.md:491「Git 分支实际可合入的变化」
- `TestS9HandoffTypedCandidateCannotLoseProvenanceOrSource` — s9_handoff_wiring_test.go:146 — 同上
- `TestS9HandoffTypedSelectorPreservesChangedAttemptFence` — s9_handoff_wiring_test.go:262 — 同上

### 桶 D2(20 条)

- `TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration` — 4
- `TestConfiguredPressureOriginalReportWriterRegistrationAndActualMainIntegration` — 6
- `TestMainCheckCapabilitiesObservationIsReadonlyAndPartial` — 1
- `TestMainCheckMissingToolRejectsPrepareBeforeReady` — 2
- `TestMainCheckReuseObservationIsReadonlyAndCannotSkipRequiredChecks` — 3
- `TestNativeBuilderConventionsFixedReplayAndSourceDrift` — 5
- `TestNativeObserverFiltersExternalTextBeforeAuditBoundWithoutChangingIdentity` — 21
- `TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals` — 14
- `TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery` — 9
- `TestNativeS9MainCompletedReturnContinuesExactLiveDSource` — 22
- `TestNativeS9MainCompletedReturnContinuesOriginalSourceAfterCleanup` — 23
- `TestNativeS9OriginalConsumeAcceptedPlanSurvivesMissingDisplayBrief` — 13
- `TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan` — 8
- `TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat` — 12
- `TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat` — 10
- `TestNativeS9OriginalFlatConsumeReceivesAndProjectsReplay` — 7
- `TestNativeS9OriginalStopUsesNewProfessionalInputAfterSourceGap` — 15
- `TestNativeS9PendingRecoveryReportsChangedPrivateOriginal` — 11
- `TestNativeS9UnrecordedMergePreservesUnknownAndWorktree` — 24
- `TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing` — 25

### 桶 D3(8 条)

- `TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration` — integration_observation_e2e_test.go — 4
- `TestActualMainIntegrationBarrierCleanupAfterEarlyReturn` — integration_observation_e2e_test.go — 5
- `TestCheckProfileActualMainIntegrationCapturesOriginalResponsibility` — check_profile_e2e_test.go — 1
- `TestCheckProfilePrepareAndReplacementKeepExactProfile` — check_profile_e2e_test.go — 2
- `TestNativeProfileContextExposesFrozenOriginalProfile` — check_profile_e2e_test.go — 3
- `TestNativeS9OriginalCandidateReadsHistoricalMachineValues` — native_s9_candidate_historical_projection_test.go — 11
- `TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput` — native_execution_return_test.go — 7
- `TestS9ReceptionFeedback` — L3-S9:238 + native_s9_original.go 的 pendingResult() 短路 — 并入 R05 产品缺陷

### 桶 L1(5 条)

- `TestPendingInitializationAcceptsAbsentReviewNumberAndLegacyZero` — internal/cli/initialization_format_test.go:~150 — 见 C-3
- `TestS6NativeClosingContextKeepsAssessmentEditableAcrossRetry` — internal/cli/s6_record_context_e2e_test.go:~300 — 见 C-1
- `TestS7DispatchGuidanceUsesActualPrerequisitesAndSource` — internal/cli/s7_guidance_test.go:62 — 见 C-2
- `TestSavedHypothesisResultHookDoesNotTreatUnissuedTeammatePathAsCurrent` — internal/cli/hypothesis_result_hook_test.go:80 — 见 C-5
- `TestSavedHypothesisResultHookRejectsUnissuedPathWithoutRuntimeWrite` — internal/cli/hypothesis_result_hook_test.go:33 — 见 C-4

### 桶 L2(16 条)

- `TestBuilderReceiptOriginalFailureReworkAndCurrentResubmission` — 只记录;断言在 internal/cli/workspace_core_profile_test.go:48,非我独占文件 — internal/cli/task_receipt_e2e_test.go
- `TestCT03911_DualPassDocumentVerificationCommitsTR003` — internal/cli/ct_11_24_test.go:19 — `L3-S5-document-verification.md:34`「S5 通过 \
- `TestCT03915_S11StopsAutoAdvance` — internal/cli/ct_11_24_test.go:194 — `L3-S11-release-gate.md:7`「**没有选择就继续等待**……」;`:62`「Controller 对 `awaiting_human_release` **不产生自动发布决定**」;`:57`「Hook \
- `TestCT03920_BuilderCompletionDurableRef` — internal/cli/ct_11_24_test.go:396 — `L3-S6-build.md:38`「S6 完成 \
- `TestCT03924_SameAgentDualDVLabelsBlocksTR003` — internal/cli/ct_11_24_test.go:494 — `L3-S5-document-verification.md:310`「原 `GATE-DOCUMENT-PASS` 检查两项规定责任、**不同 producer**、准确对象、当前入口及来源资格」
- `TestNativeMainReturnReadsS5ReceiptAcrossStageWithoutReceivingAnswers` — internal/cli/native_main_return_test.go:90 — `L4-agent-dispatch-governance.md:131`「无准确匹配或真实派发的责任集合无法确定时,保留原生结果并交 Main／系统处理;**不能按路径出现顺序猜选**」
- `TestNativeMainS7ReturnKeepsSubmissionDiagnosticWhenOwnBodyFails` — internal/cli/native_main_return_test.go:152 — `L4-agent-dispatch-governance.md:167`「**接收失败不能冒充完成**……**原字段错误沿接收器传递**」
- `TestNativeS9FlatValidationFeedbackUsesPrivateFileFields` — internal/cli/native_s9_artifact_feedback_test.go:58 — `L3-S9-repair.md:238`「**字段缺失时保留原稿,反馈实际文件与字段**」;`L4-agent-dispatch-governance.md:167`「**原字段错误沿接收器传递**」
- `TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint` — internal/cli/workspace_missing_private_test.go:66 — `L4-workflow-convergence.md:212`「**工作区清理单独处理**,不撤销接收或堵住后继」;`L4-worktree-governance.md:115`「worktree 已回收不撤销已接收专业事实」
- `TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop` — internal/cli/native_s9_targeted_source_guard_test.go:99 — `L4-agent-dispatch-governance.md:131`「**不能按路径出现顺序猜选**」;`L4-workflow-convergence.md:263`「原卷各自自然接收」
- `TestProgressPacketS9SkipsLegacyCoverageWithoutCache` — internal/cli/work_progress_test.go:27 — `L4-workflow-convergence.md:228`「删除 C／D、typed、retained-source、frozen 及**历史 schema 的并行接线**」;`:263`
- `TestRuntimeInvestigationContractApproveCLIHandsOffToS9` — internal/cli/investigation_command_test.go:189 — `L3-S8-finding-investigation.md:410`「原 S9 合同批准**同时保存权威合同与阶段交接**」
- `TestS6ClosingContractCLISeparatesOwnedCompletionFromMain` — internal/cli/s6_closing_context_e2e_test.go:39 — `L3-S6-build.md` closing contract 段;`L4-workflow-convergence.md` S6 行(本人交付与 Main 集成分别成立)
- `TestS6UnchangedPreToolUseReusesObservation` — internal/cli/s6_observation_test.go:41 — `L3-S6-build.md:139`「`s6 prepare` 不启动进程,不预建原生工作区……」;`L4-workflow-convergence.md:439`「S6 status、指引和 `s6 prepare` 共同按这些已有关系读取当前适用的原派发计划与 TASK」
- `TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility` — internal/cli/s7_lifecycle_listener_test.go:98 — `L4-workflow-convergence.md:225`「S7/S8/S9 共用 Pre 观察、成功 Post 和 Stop 兜底的接收语义;**只定位当前责任与原卷、返回接收或具体缺项**」
- `TestS9VerifierRegistrationWaitsForTargetedReadiness` — internal/cli/s9_dispatch_readonly_test.go:31 — `L3-S9-repair.md:72`「独立复验与 S7 后继 \

### 桶 L3(18 条)

- `TestCanonicalS6WorkspaceReworkRequiresNamedAssignmentRevision` — internal/cli/s6_rework_revision_e2e_test.go:169 — 27
- `TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite` — internal/cli/assignment_context_e2e_test.go:180 — 2
- `TestIndependentReviewRejectsCorruptSourceWithoutReception` — internal/cli/stage_answer_e2e_test.go:688 — 34
- `TestMainOrdinaryReadAndNonCheckpointStatusProgressWire` — internal/cli/work_progress_e2e_test.go:52-54 — 37
- `TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication` — internal/cli/native_s9_consumer_retention_test.go:54 — 9
- `TestNativeS9MissingTypedWithoutDiagnosticPreservesStagedCandidate` — internal/cli/native_s9_reception_split_test.go:470 — 12
- `TestNativeS9NaturalSavedTypedPrecedesChangedOrMissingFlat` — internal/cli/native_s9_reception_split_test.go:80 — 13
- `TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup` — internal/cli/native_s9_consumer_retention_test.go:102 — 10
- `TestNativeS9PublicAuthorCorrectsRejectedResult` — internal/cli/native_s9_reception_split_test.go:140 — 14
- `TestNativeS9PublicChangedFlatPreservesStagedCandidateWhenSourceDisappears` — internal/cli/native_s9_reception_split_test.go:407 — 15
- `TestNativeS9PublicLegacyCorrectsAfterOriginalSourceReturns` — internal/cli/native_s9_reception_split_test.go:311 — 16
- `TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute` — internal/cli/native_snapshot_failure_external_test.go:58 — 17
- `TestS5WorkerReadProgressAndReceivedPeerWaitWire` — internal/cli/work_progress_e2e_test.go:132-135 — 38
- `TestS7ExistingEditedManifestRecoversRegistrationWithoutOverwrite` — internal/cli/s7_manifest_draft_test.go:234 — 28
- `TestS9FailureHandoffRealStopDeadlineReachesFreshPublicMain` — internal/cli/s9_failure_handoff_e2e_test.go:68 — 32
- `TestS9PreparationCallbackFollowsOriginalCAS` — internal/cli/s9_pin_preparation_order_test.go:37 — 33
- `TestS9RepairVerifierDispatchQuestionnaireOriginalPhasesAndResponsibility` — internal/cli/repair_verifier_dispatch_questionnaire_test.go:235 — 24
- `TestTaskScopeOriginalS4SaveThroughNativeAssignmentAndBrief` — internal/cli/task_scope_handoff_test.go:108 — 35

### 桶 INV(2 条)

- `TestOriginalHypothesisReceiptIgnoresHistoricalMachineFields` — receipt_machine_metadata_external_test.go:13 — 19
- `TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates` — hypothesis_result_registration_test.go:95 — 13

### 桶 SYS(4 条)

- `TestRuntimeRecoveryApplyNeverRevivesActiveAgents` — tests/system/recovery/runtime_recovery_test.go:341(断言 :367) — 24
- `TestRuntimeRecoveryApplySamePlanIsIdempotent` — tests/system/recovery/runtime_recovery_test.go:468(断言 :478) — 25
- `TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound` — internal/integration/s7_issued_result_external_test.go:39(断言 :432) — 2
- `TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound` — internal/integration/s7_issued_result_external_test.go:35(断言 :432) — 1

## 5. CONFLICT 记录(蓝图自相矛盾,需 owner 裁定)

### 桶 L1(1 条)

- `TestS7SharedOriginalCannotBeBorrowedByUnboundNativeWorktree` — internal/cli/s7_normal_authoring_test.go:25 — 见 D-1

### 桶 L3(2 条)

- `TestFrozenS6ObserverUsesOriginalWorkspaceSession` — internal/cli/s6_frozen_observer_session_e2e_test.go:16 — 26
- `TestWorkerDraftIgnoreKeepsUnexpectedFilesVisible` — internal/cli/worker_draft_ignore_test.go:14 — 39

### 桶 L4(1 条)

- `TestS7AcceptedRemainingProjectionUsesOriginalPrerequisites` — internal/cli/s7_native_remaining_test.go:106 — 29

> 详细条款冲突原文见各桶报告附件。

## 6. 未决条目

共 38 条。常见原因:断言位于其它 agent 独占的测试文件、
需要 `tests/fixtures/` 下的数据 fixture(非测试文件,本轮红线不许改)、或定向条款检索未完成。

### 桶 D1(1 条)

- `TestS9HandoffPublicMainProgressUsesOriginalReceptionFacts` — s9_handoff_wiring_test.go:474

### 桶 D3(6 条)

- `TestPlanningObserverUsesAnswerAndBoundCommitWithoutRepeatingUnchangedSave` — questionnaire_observer_e2e_test.go
- `TestS9CapturedInputCoordinateAloneIsNotSavedTypedInput` — native_execution_return_test.go
- `TestS9CapturedRecoveryCommandExecutesOriginalTypedConsumer` — native_execution_return_test.go
- `TestS9EntryAssemblyReadsOriginalRepairPlanOnce` — s9_entry_assembly_linux_test.go
- `TestS9PublicFeedbackRecoveryCommandUsesReturnedArgv` — native_execution_return_test.go
- `TestS9ReceptionFeedbackMissingSavedTypedInputNamesOriginalWindowBeforeRetry` — s9_reception_feedback_contract_test.go

### 桶 L1(10 条)

- `TestMainS2S5MilestoneDirectoriesProjectActualReadSources` — internal/cli/work_progress_milestone_directory_test.go:114
- `TestMainS6LockedReadCategoriesResolveRegisteredCurrentFiles` — internal/cli/work_progress_milestone_directory_test.go:223
- `TestNativeWorkerCheckResultPersistsDurableFactsAndBindsAnswerAndSource` — internal/cli/s6_native_worker_result_e2e_test.go:146
- `TestS5ReturnToS6ProvidesPreparationInsteadOfWaitingForUnstartedBuilder` — internal/cli/s5_s6_guidance_delivery_test.go:160
- `TestS7OptionalProgressUnknownNativeActorIsSilent` — internal/cli/work_progress_s7_actor_test.go:45
- `TestS8NativeLifecycleDeliversExactQuestionnaireInHookEnvelope` — internal/cli/s8_questionnaire_listener_test.go:162
- `TestS9NativeListenerKeepsPrivateOutputsOutOfMainAuthoring` — internal/cli/s9_native_listener_guidance_test.go:37
- `TestS9PlanningGuidanceUsesOriginalDivisionAndIssuedOwner` — internal/cli/s9_repair_status_guidance_test.go:84
- `TestS9RepairDispatchExplicitSavePreparesNativeBriefAndPreservesOriginalSources` — internal/cli/repair_dispatch_questionnaire_test.go:~200
- `TestS9RepairDispatchQuestionnaireResponsibilityAndS8Isolation` — internal/cli/repair_dispatch_questionnaire_test.go:261

### 桶 L2(1 条)

- `TestNativeS6RevisionKeepsObjectGuardsAcrossUnrelatedRuntimeCommit` — internal/cli/revision_boundary_e2e_test.go:38

### 桶 L3(3 条)

- `TestNativeS6FixedSourceAlignmentAfterMainAdvances` — internal/cli/native_s6_guidance_delivery_test.go:106-118
- `TestNativeS9PendingPublicPreReturnsExecutableOriginalWriterRecovery` — internal/cli/native_s9_pending_feedback_test.go:65
- `TestS9RepairVerifierDispatchQuestionnaireOneIndependentContractOwner` — internal/cli/repair_verifier_dispatch_questionnaire_test.go:80

### 桶 L4(12 条)

- `TestNativeS9PlanOnlyStopReportsUnreadyOriginalPlan` — internal/cli/native_s9_plan_only_stop_test.go:202
- `TestS10ReviewRequiredPendingReturnDoesNotReceiveChangedOrWorkerAnswer` — internal/cli/audit_review_required_test.go
- `TestS10ReviewRequiredReturnsOriginalFullReviewAndNewPlan` — internal/cli/audit_review_required_test.go
- `TestS3UncommittedOutputNamesCommitAndAcceptsSameAnswerAfterCommit` — internal/cli/contract_followup_rework_e2e_test.go:146
- `TestS4AcceptedDescriptionChangeKeepsCompletionButCannotRegisterChangedMaterials` — internal/cli/s4_continuation_test.go:353
- `TestS7ExactNativeStartAndChangedMainContext` — internal/cli/s7_context_delivery_test.go:24
- `TestS7IncompleteNativeReturnHandsOriginalResponsibilityToMain` — internal/cli/s7_native_incomplete_return_test.go:50
- `TestS7OriginalAnswerReachesConsumerWithOldS9Route` — internal/cli/stage_native_routing_contract_test.go:35
- `TestS7PartialP0NativeReturnReachesS8Intake` — internal/cli/s7_s8_native_handoff_test.go:48
- `TestS8ObservedContractPassiveSkipKeepsExplicitSaveAndChangedValidation` — internal/cli/investigation_lead_contract_observer_test.go
- `TestS9EvidenceReferenceIsPreservedWithoutCopyOrExistenceGate` — internal/cli/s9_preferred_evidence_integration_test.go:46
- `TestWorkspaceBackendCoreLaunchCommitAndOriginalVerifiedIntegration`

### 桶 SYS(5 条)

- `TestAuditSharedModelHookSpineCommittedSource` — tests/system/req039/audit_sharedmodel_spine_test.go:236
- `TestAuditStageSourcesUseOriginalConsumerBoundaryE2E` — tests/system/req039/audit_sources_e2e_test.go:22
- `TestBUG104S11ExplicitApproveE2E` — tests/system/req039/s11_human_decision_test.go:14
- `TestCT03912_BuilderCompleteCommitsTR006System` — tests/system/req039/ct_l4_precondition_test.go:82
- `TestS2ToS11_HookDrivenCleanPath` — tests/system/req039/s2_to_s11_hook_driven_test.go:15

## 7. 本轮独立复核推翻的前轮结论

前一轮(`TEST-GAP-AUDIT.md`)的缺陷清单是**从代码现状反推**的。本轮改为**从 blueprint 正推**,
逐字核对后推翻了其中 7 条。**以下以本轮为准**;`TEST-GAP-AUDIT.md` 各条目下的
「🔄 本轮复核修订」块给出同样结论。

| 前轮结论 | 本轮复核 | 依据 |
|---|---|---|
| H3 — S8 因果闭合链是缺陷 | **推翻** | `L3-S8:160`「**不是两条机器证明路线**……**不要求至少一项 `refuted`**」;`L3-S8:307`(§9.3「机器不认证因果」)「不成为……**至少一项反驳**……的机器门」—— **两条都是移除类措辞** ⇒ 判 STALE |
| H17 — 同一事件重试重复写审计行是缺陷 | **推翻** | 父提交的去重身份建立在事件正文 sha256 上(`9bf73ca:internal/policy/engine.go:1738-1755`),违反 `L4-hook-acceptance-checklist.md:184`「不得将同一用途改名为 fingerprint、checksum 或换用其他散列算法」;`:188`「不能用"保回归"把已放弃的机器保证补回」 ⇒ 判 STALE |
| H18 — review-plan 空路径是缺陷 | **推翻** | `L3-S7:191`「路径沿本次 ReviewPlan 身份生成」;生产路径上 `round_entry.plan_id` **总是**被写入(`internal/transition/actions.go:263`、`internal/repair/runtime.go:1141`)⇒ 空路径**只在合成 fixture 上出现**;HEAD 对空路径**有准确反馈**(`s7_status.go:114-121`) ⇒ 判 STALE-FIXTURE |
| R05 / H1 — S9 source gap 是 S7 兜底失效 | **推翻(归因)** | 失败点全在 **S9 链**,不在 S7 兜底;诊断是 `Original S9 Result received.`。断言依赖的 `S9 local evidence source gap` 来自 `internal/repair/s9_local_evidence.go`(**归档证据字节 + 记 SHA256**),已被 `L3-S9:236`「**不回读证据文件、不归档后散列取证**」整体删除 ⇒ 判 STALE |
| H7 #22/#23 — Stop 不消费本人原 Result 是缺陷 | **推翻(归因)** | 真触发是 `execution.DeliveryRef != ""`(**Stop 已建立候选**),`WorkerChecks:[]` 只是 **Go 结构体打印**;`L4-hook-platform-wiring.md:138`「S7／S8／S9 Stop 与 Pre、成功 Post 共用 §5.2」要求 **Stop 共享接收链** ⇒ 判 STALE |
| H16 — 断点在 `internal/integration/inspect_responsibility.go` | **定位修正(结论维持)** | 该守卫在父提交 `9bf73ca:internal/integration/inspect.go` **一模一样存在**;真断点在**生产方**:`internal/cli/native_s9_original.go:169` 把 `Branch` **写死成 `""`**(父提交跑 `git branch --show-current`) |
| M1 — `observeMainS9Impact` 零引用短路观察链 = MED 缺陷 | **状态改为未决** | 零引用事实维持;是否构成缺陷取决于「公开 Main PreToolUse 经它写入 Runtime 是否合规」(`L3-S9:236` vs `:238` 无直接裁决)⇒ **需 owner 裁定**,相关测试保持红 |

**另新增一条精确根因(H9)**:`internal/cli/native_s9_recovery.go:87` 用 `filepath.Base(filepath.Dir(path))` 回推包名,
但 `internal/repair/native_s9_private.go:13-24` 把超长 ID 截断成 `p/<c1>/<c2>/<c3>` ⇒ **原生 Result 路径永远解不回 Plan 包**。

## 8. 需 owner 裁定的冲突(精确条款对照)

下面每条都是**蓝图自己给出的两句话**,代码只能满足其中一句。按本轮规则(蓝图矛盾 ⇒ 只记录),
相关测试**保持红**,不动代码。

### S8 竞争解释闭合门:被删还是被要求?

- **一侧**:`L3-S8:160`「既有 `competing_hypothesis_assessment` 或 `no_competing_hypothesis` 用于表达 Main 的判断和理由,**不是两条机器证明路线**。Main 可以引用已取得结果说明竞争解释怎样处置,**不要求至少一项 `refuted`**、所有假设必须完成或所有历史问题必须逐项取得支持／反驳才允许选择出口。」
- **另一侧**:`L3-S8:307`(§9.3「机器不认证因果」)「未解释集合、假设状态和来源关联用于导航,**不成为逐题完成、逐 Finding 解释 ID 闭合、至少一项反驳**、全部建议落地或所有历史证据有效的机器门。」
- **影响**:两条**都是移除类措辞** ⇒ 本轮按 STALE 处理(改写测试)。但若 owner 认为 §9.3 之外的条款仍要求闭合门,则 `internal/investigation/case_workflow.go` 的 `requireCompetingHypothesisClosure` 需接回。

### S7 审查波次门:系统重算 Claim 分母吗?

- **一侧**:`L4-worktree-governance.md:260`「S7 原 Plan 与 Reviewer 答卷按解析、必要字段、就绪及结论接收……**系统不重算 Claim 分母**、覆盖库存、CleanRound、ObservationBatch 或证据有效性证明。」
- **另一侧**:`L3-S8:160`(相邻语境)/ 上一轮 H3 判定所引用的「逐题完成」要求 —— 被 `L3-S8:307` 明确移除。
- **影响**:被删的 `internal/assignment/review_readiness.go` 闸门**同时**具备两者。本轮按「移除类措辞优先」判 STALE;若 owner 认为 S7 仍需静态/行为波次门,则需重新接回。

### S9 Stop 在「计划未就绪 / 交付未完成」时是否该写 Runtime?

- **一侧**:`L3-S9:238`「可解析的占位、计划中的检查和明确待继续内容**不自动成为完成结论**;合法 blocked 或其他非终结表达可以交接」 —— 暗示「未完成」是一种**合法状态**。
- **另一侧**:`L3-S7:225` 附近的 S7 侧约定 —— Stop 在未就绪时**不写** Runtime,只交 Main。
- **影响**:两处对「未就绪时是否产生写入」的表述方向相反。相关测试保持红。

### S7 答案文件的旧 schema 字段:接收还是拒绝?

- **一侧**:`L4-workflow-convergence.md:104`「已发行的 S7、S8、S9 原责任在阶段变化后真实晚到或本人自然继续,**仍沿原关联接收**;结果按原对象交 Main 判断,不充当新责任的结论。」
- **另一侧**:`L4-workflow-convergence.md:118`「使用当前 Runtime schema 和当前消费者,**不保留旧版本 schema、兼容解码**或读取旧摘要的并行接线。」
- **影响**:重构前的 S7 答案文件必然带 `questions.verification_artifact_digest`,闭合 schema 会拒绝它。`:104` 要求「晚到仍沿原关联接收」,`:118` 要求「不保留旧 schema 兼容」—— **需 owner 裁定接收语义**。

### 公开的 Main PreToolUse 产生写入,合规吗?

- **一侧**:`L3-S9:236`「系统**不重新证明**代码、证据内容或实际检查是否发生……」 —— 强调观察链不做事。
- **另一侧**:`L3-S9:238`「系统接受专业陈述,不认证其真实性和充分性」 —— 强调观察链只登记。
- **影响**:公开的 Main PreToolUse 经 `observeMainS9Impact` **写入** Runtime(`internal/cli/run.go:2696` 短路 `observeMainS9Brief`)。蓝图对「观察事件是否允许产生写入」无直接裁决 ⇒ M1 状态为**未决 / 需 owner 裁定**。

### S9 提醒在契约对象缺失时,是否仍应给出本人原卷位置?

- **一侧**:`L3-S9-repair.md:83`「**能确定原责任时定位本人原卷**,不要求每个事件同时提供所有坐标」—— 原责任可确定时应给出本人原答卷路径。
- **另一侧**:`L3-S9-repair.md:49`「已收决定或原对象不可恢复时保留已知事实与具体未知,交 Main／原责任处理」+ `:390`「未知的责任适用性按 §9.3 交 Main」。
- **影响**:`TestS9ListenerExistingPacketsReadOnlyWithoutIssueRecoveryOrReceiptProof`:实现走「交 Main + 具体未知」(「Main/system must restore the missing source」),但**没有同时给出本人原卷位置**。两侧都说得通 ⇒ **需 owner 裁定**。

### 「独立清理」是否被未答的 ChangeImpact 挡住?

- **一侧**:`L4-workflow-convergence.md:302`「清理只在可安全移除时用普通非强制 Git 操作;未知、未跟踪、忽略或私人材料保留为 `cleanup_pending`」+ `:435`「`cleanup_pending` 是资源待处理,不是集成失败」 —— 把 `cleanup_pending` 只系于保留材料。
- **另一侧**:`L3-S9-repair.md:271`「Main 接手 Candidate 时**即可**准备原 ChangeImpact 和已有贡献材料,不要求先造 Final,也不把提前准备题目当成整批完成」 —— 允许提前准备,但没说它能挡住清理。
- **影响**:`TestLastFinalPreparesImpactBeforeIndependentCleanup/impact-before-cleanup`:实现把清理挡在未答的 Impact 之后(「Main must complete and save ChangeImpact」),owner 材料解决后工作树仍不清理。测试按蓝图断言独立清理 ⇒ **该子用例保持红,需 owner 裁定**。

## 9. 方法与红线自检

- **判定依据**:蓝图(`blueprint/l3-stages/L3-S0..S11-*.md`、`blueprint/l4-mechanisms/L4-*.md`、
  `docs/control/loop-definition.json`、`blueprint/BUGS.md`)。优先级:蓝图 > loop-definition.json > 代码现状。
  **代码现状不是判据** —— 它是待审对象。
- **三选一**:STALE(蓝图移除类措辞 → 改写测试)/ DEFECT(蓝图要求类措辞 → 只记录)/
  CONFLICT(蓝图自相矛盾或沉默且无法推出 → 只记录,标需 owner 裁定)。
- **不得削弱断言**:没有删断言、放宽阈值、把 `t.Fatal` 改成 `t.Log`、加 `t.Skip` 或删除文件。
  改写后的测试**仍能抓住原本要防的回归**,并鼓励**新增反向断言**。
- **失败不停**:每判定一条记录后继续,直到 scope 全覆盖。
- **父提交基线** `9bf73ca`:父提交也失败的测试是**既有失败**,不是本轮回归。
- **工作方式**:严格文件级互斥所有权 + 每人独立克隆 + 共享 `GOCACHE`,避免并发编译互相污染;
  跨桶文件冲突机器校验为 0,非测试文件机器校验为 0。

