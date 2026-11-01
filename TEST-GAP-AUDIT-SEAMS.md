# Cross-Stage Seam Review — `internal/integration` + `tests/system/req039` + recovery/policy/assignment

Repo: `github.com/entroforge/go-system-builder` · HEAD `d0d948a` (frozen) · parent baseline `9bf73ca`
Work clone: `/tmp/xstage-seams2` (clean clone) · parent worktree: `/tmp/xstage-seams2-base` · `GOCACHE=/tmp/xstage-seams2-cache`
All runs: `go test -json -timeout=20m -p 2`. No SIGTERM / "signal: terminated" aborts occurred; no package timed out.

---

## Table 1 — Package results

| package | tests run (top-level) | pass | fail | timeout? | duration | exit code |
|---|---|---|---|---|---|---|
| `internal/integration` @ HEAD | 83 (+37 subtests) | 80 | **2** | no | 166.201 s | 1 |
| `internal/integration` @ parent `9bf73ca` | 115 | 114 | 0 | no | 405.978 s | 0 |
| `tests/system/req039` @ HEAD | 98 | 79 | **19** | no | 83.094 s | 1 |
| `tests/system/req039` @ parent `9bf73ca` | 114 | 67 | 45 | no | 77.692 s | 1 |
| `tests/system/recovery` @ HEAD | 12 | 7 | **5** | no | 6.614 s | 1 |
| `tests/system/recovery` @ parent `9bf73ca` | 12 | 12 | 0 | no | 7.174 s | 0 |
| `tests/policy` @ HEAD | 20 | 20 | 0 | no | 0.173 s | 0 |
| `tests/policy` @ parent `9bf73ca` | 20 | 20 | 0 | no | 0.145 s | 0 |
| `tests/assignment` @ HEAD | 6 | 6 | 0 | no | 0.893 s | 0 |
| `tests/assignment` @ parent `9bf73ca` | 10 | 10 | 0 | no | 1.046 s | 0 |
| `tests/fixtures/req039` | 0 (helper package, no `_test.go`) | — | — | — | — | build ok |
| `tests/fixtures/native-stage-simulation` | 0 (data-only, no Go files) | — | — | — | — | n/a |

**MEASURED.** 26 distinct top-level failing tests + 12 failing subtests = **38 fail events**. None were skipped for time; the only skip is `TestExportS7BrowserEvaluation` (`internal/integration/s7_browser_eval_export_test.go:26`, requires `GSB_S7_EVAL_ROOT`/`GSB_S7_PRODUCT_ROOT`).

> **Correction to the previous pass's premise.** The brief said "one parent failure shows `hook output is not JSON: EOF`". I measured the parent `internal/integration` = **`ok`, 0 failures**. The `hook output is not JSON: EOF` errors belong to the parent **`tests/system/req039`** package (45 parent failures, e.g. `tests/system/req039/ct_l4_precondition_test.go:33`). I re-ran them in a **separate standalone clone** of `9bf73ca` (`/tmp/xstage-parentclone`, not the linked worktree) and they reproduce identically ⇒ **genuine PRE-EXISTING parent failures, NOT a worktree artifact**. (STATIC+MEASURED)

---

## Table 2 — EVERY failure

### `internal/integration` (2)

| # | test | file:line | class | regression? | decisive error / expected-vs-actual | governing spec |
|---|---|---|---|---|---|---|
| 1 | `TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound` | `internal/integration/s7_issued_result_external_test.go:433` (via `:36`) | **PRODUCT-BUG** | REGRESSION (parent PASS) | `active S7 Stop did not report original reception: stdout= stderr=` — hook exit 0, durable receipt persisted, but stderr lacks `original consumer` and `lifecycle.state` stays `verification` (no S7→S10 outlet) | `blueprint/l4-mechanisms/L4-workflow-convergence.md:437` (S7 出口 → TR-009); `blueprint/l3-stages/L3-S7-verification-round.md:384` |
| 2 | `TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound` | `internal/integration/s7_issued_result_external_test.go:433` (via `:40`) | **PRODUCT-BUG** | REGRESSION (parent PASS) | identical message | same |

Root cause (STATIC): the new "already accepted → silent no-op" branch at `internal/cli/native_review_hooks.go:358-372` (`if stage != "S8" && sourceErr == nil && receipt.Accepted && receipt.WorkComplete { if stage != "S7" || !s7SuccessorPending(...) { return true, 0 } }`) short-circuits **before** the `received by the original consumer` diagnostic at `internal/cli/native_review_hooks.go:396` and before the TR-009 promotion. New helpers `s7SuccessorPending` / `reviewPacketAppliesToCurrent` do not exist at the parent.

### `tests/system/req039` (19 top-level + 10 subtests)

| # | test | file:line | class | regression? | decisive error | governing spec |
|---|---|---|---|---|---|---|
| 3 | `TestRuntimeAuthorityProtectsRecoveryAndStorageCoordinates` (+ subtests `fingerprint`, `pending-recovery`, `alternate-state`, `alternate-journal`, `state-symlink`; `reconcile` passes) | `tests/system/req039/audit_authority_e2e_test.go:92` | **STALE-TEST** | REGRESSION (parent PASS) | `expected authority rejection, code=2 stderr=unknown runtime command "fingerprint"` — `runtime fingerprint` was deleted (`internal/runtime/fingerprint.go`) | `internal/runtime/authority.go:12` still defines the guard; command removal is the blueprint's intent (`blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:7`) |
| 4 | `TestS4ResultBindingThroughTaskIntegrate` | `tests/system/req039/audit_dispatch_result_binding_test.go:71` | **STALE-FIXTURE** | REGRESSION | `runtime task-integrate: Assignment assignment-ti has no current Record projection` | `blueprint/l4-mechanisms/L4-workflow-convergence.md:293` (delete legacy wiring, do not add a compatibility layer) |
| 5 | `TestS3AuditFrozenModelCannotMergeFromWorker` (subtests `modify`,`delete`,`rename`) | `tests/system/req039/audit_sharedmodel_integration_test.go:131` | **STALE-FIXTURE** | REGRESSION | same `no current Record projection` | idem |
| 6 | `TestS3AuditUnrelatedImplementationChangeMerges` | `tests/system/req039/audit_sharedmodel_integration_test.go:169` | **STALE-FIXTURE** | REGRESSION | same | idem |
| 7 | `TestAuditSharedModelHookSpineCommittedSource` | `tests/system/req039/audit_sharedmodel_spine_test.go:280` | **STALE-TEST** | **PRE-EXISTING** (parent FAIL) | `shared model document has invalid content hash` — content-hash proof removed | `blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:7`; `blueprint/l4-mechanisms/L4-worktree-governance.md:316` |
| 8 | `TestAuditStageSourcesUseOriginalConsumerBoundaryE2E` | `tests/system/req039/audit_sources_e2e_test.go:112` | **STALE-TEST** | **PRE-EXISTING** | expected `S3 frozen input differs from original checkout SHA`, got `<nil>` | idem |
| 9 | `TestCT03917_SubagentStopIdempotentResumeAfterMerge` | `tests/system/req039/ct_integration_resume_test.go:22` | **STALE-FIXTURE** | REGRESSION | `no current Record projection` | idem |
| 10 | `TestCT03912_BuilderCompleteCommitsTR006System` | `tests/system/req039/ct_l4_precondition_test.go:105` | **PRE-EXISTING** | **PRE-EXISTING** | `CT-039-12 TR-006 must land verification.planned, got "building"/""`, gate `GATE-BUILDER-BATCH-READY` missing `builder_responsibility:TASK-039-01/02` | `blueprint/l4-mechanisms/L4-workflow-convergence.md:485` |
| 11 | `TestFinalAuditVerifiedTargetRewriteViaTaskIntegratePreservesWorker` | `tests/system/req039/final_audit_delivery_test.go:68` | **STALE-FIXTURE** | REGRESSION | `no current Record projection`; checkpoint shows `AssignmentRecord:<nil> CanonicalMainFacts:<nil>` | idem |
| 12 | `TestBUG104ApproveThenRolloverE2E` | `tests/system/req039/s11_human_decision_branches_test.go:19` | **STALE-FIXTURE** | REGRESSION | `guard release_audit_approved failed: no valid release_audit evidence entry for the current baseline/round` | `blueprint/l3-stages/L3-S10-acceptance-audit.md` (release_audit evidence) |
| 13 | `TestBUG104DeferResumeS11E2E` | `tests/system/req039/s11_human_decision_branches_test.go:52` | **STALE-FIXTURE** | REGRESSION | same | idem |
| 14 | `TestBUG104RejectDefectRoutesHumanProblemToInvestigationE2E` | `tests/system/req039/s11_human_decision_branches_test.go:105` | **STALE-FIXTURE** | REGRESSION | same | idem |
| 15 | `TestBUG104RejectAcceptanceAndAuditInvalidateOnlyScopedEvidenceE2E` (subtests `acceptance`, `release_audit`) | `tests/system/req039/s11_human_decision_branches_test.go:281` | **STALE-FIXTURE** | REGRESSION | same | idem |
| 16 | `TestBUG104AbortThenRolloverE2E` | `tests/system/req039/s11_human_decision_branches_test.go:321` | **STALE-FIXTURE** | REGRESSION | same | idem |
| 17 | `TestBUG104S11ExplicitApproveE2E` | `tests/system/req039/s11_human_decision_test.go:30` | **PRE-EXISTING** | **PRE-EXISTING** | `normal Hook must stop at S11: got "release_audit"` | idem |
| 18 | `TestS2ToS11_HookDrivenCleanPath` | `tests/system/req039/s2_to_s11_hook_driven_test.go:60` | **PRE-EXISTING** | **PRE-EXISTING** | `hook spine-s6 want TR-006→verification.planned (last="TR-003"), got building` (same `builder_responsibility` gap as #10) | idem |
| 19 | `TestTaskIntegrateMergesAcknowledgesAndCleansWorktree` | `tests/system/req039/task_integrate_test.go:67` | **STALE-FIXTURE** | REGRESSION | `no current Record projection` | idem |
| 20 | `TestTaskIntegrateConflictPreservesWorktree` | `tests/system/req039/task_integrate_test.go:146` | **STALE-FIXTURE** | REGRESSION | idem | idem |
| 21 | `TestIntegrationRecoversLostMergeReceiptWithoutMergingAgain` | `tests/system/req039/worktree_governance_test.go:87` | **STALE-FIXTURE** | REGRESSION | idem | idem |

### `tests/system/recovery` (5 top-level + 2 subtests)

| # | test | file:line | class | regression? | decisive error | governing spec |
|---|---|---|---|---|---|---|
| 22 | `TestRuntimeRecoveryInspectAndPlanSurviveMalformedOrBOMState` (subtests `malformed-json`, `utf8-bom`) | `tests/system/recovery/runtime_recovery_test.go:68` | **STALE-TEST** | REGRESSION | `plan must content-address or retain damaged runtime bytes` — recovery plan digest removed | `blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:7` (digest proof dropped) — **removal intentional** |
| 23 | `TestRuntimeRecoveryApplyRebindsREQAndQuarantinesCorruptRuntime` | `tests/system/recovery/runtime_recovery_test.go:123` | **STALE-TEST** | REGRESSION | `recovered Runtime bound_req.sha256 = <nil>, want current REQ digest 2879088b…` | idem — **removal intentional** |
| 24 | `TestRuntimeRecoveryApplyQuarantinesAndClearsLegacyPendingMarker` | `tests/system/recovery/runtime_recovery_test.go:153` | **PRODUCT-BUG** (HIGH) | REGRESSION | `runtime recover apply: original pending Runtime operation is unresolved; preserve the existing files: unsupported pending runtime commit schema ""` | `internal/runtime/store.go:1803` (schema check now fatal) — the refactor turned "invalid marker falls through" into "hard abort" (`internal/cli/recover_command.go` diff: *"an unresolved marker must remain in place rather than guessing or retiring it"*) |
| 25 | `TestRuntimeRecoveryApplyNeverRevivesActiveAgents` | `tests/system/recovery/runtime_recovery_test.go:367` | **PRODUCT-BUG** (HIGH) | REGRESSION | `runtime recover apply: LOOP_RECOVERY_INPUT_DRIFT: recovery input drift: checkpoint disagrees with current Runtime identity/baseline/REQ` | see Table 7 #2 |
| 26 | `TestRuntimeRecoveryApplySamePlanIsIdempotent` | `tests/system/recovery/runtime_recovery_test.go:478` | **PRODUCT-BUG** (HIGH) | REGRESSION | identical drift message | see Table 7 #2 |

`tests/policy` and `tests/assignment`: **0 failures**.

---

## Table 3 — Parent-baseline comparison (`d0d948a` vs `9bf73ca`)

| failing test (HEAD) | exit @ `d0d948a` | exit @ `9bf73ca` | verdict |
|---|---|---|---|
| `TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound` | 1 | **0** | REGRESSION |
| `TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound` | 1 | **0** | REGRESSION |
| `TestRuntimeAuthorityProtectsRecoveryAndStorageCoordinates` (+4 subtests) | 1 | **0** | REGRESSION |
| `TestS4ResultBindingThroughTaskIntegrate` | 1 | **0** | REGRESSION |
| `TestS3AuditFrozenModelCannotMergeFromWorker` (+3 subtests) | 1 | **0** | REGRESSION |
| `TestS3AuditUnrelatedImplementationChangeMerges` | 1 | **0** | REGRESSION |
| `TestCT03917_SubagentStopIdempotentResumeAfterMerge` | 1 | **0** | REGRESSION |
| `TestFinalAuditVerifiedTargetRewriteViaTaskIntegratePreservesWorker` | 1 | **0** | REGRESSION |
| `TestBUG104ApproveThenRolloverE2E` | 1 | **0** | REGRESSION |
| `TestBUG104DeferResumeS11E2E` | 1 | **0** | REGRESSION |
| `TestBUG104RejectDefectRoutesHumanProblemToInvestigationE2E` | 1 | **0** | REGRESSION |
| `TestBUG104RejectAcceptanceAndAuditInvalidateOnlyScopedEvidenceE2E` (+2) | 1 | **0** | REGRESSION |
| `TestBUG104AbortThenRolloverE2E` | 1 | **0** | REGRESSION |
| `TestTaskIntegrateMergesAcknowledgesAndCleansWorktree` | 1 | **0** | REGRESSION |
| `TestTaskIntegrateConflictPreservesWorktree` | 1 | **0** | REGRESSION |
| `TestIntegrationRecoversLostMergeReceiptWithoutMergingAgain` | 1 | **0** | REGRESSION |
| `TestRuntimeRecoveryInspectAndPlanSurviveMalformedOrBOMState` (+2) | 1 | **0** | REGRESSION |
| `TestRuntimeRecoveryApplyRebindsREQAndQuarantinesCorruptRuntime` | 1 | **0** | REGRESSION |
| `TestRuntimeRecoveryApplyQuarantinesAndClearsLegacyPendingMarker` | 1 | **0** | REGRESSION |
| `TestRuntimeRecoveryApplyNeverRevivesActiveAgents` | 1 | **0** | REGRESSION |
| `TestRuntimeRecoveryApplySamePlanIsIdempotent` | 1 | **0** | REGRESSION |
| `TestAuditSharedModelHookSpineCommittedSource` | 1 | **1** | PRE-EXISTING |
| `TestAuditStageSourcesUseOriginalConsumerBoundaryE2E` | 1 | **1** | PRE-EXISTING |
| `TestCT03912_BuilderCompleteCommitsTR006System` | 1 | **1** | PRE-EXISTING |
| `TestBUG104S11ExplicitApproveE2E` | 1 | **1** | PRE-EXISTING |
| `TestS2ToS11_HookDrivenCleanPath` | 1 | **1** | PRE-EXISTING |

**22 regressions, 4 pre-existing.** The refactor also *fixed* 40 parent failures by **deleting or rewriting** the tests — see Tables 4/5/8.

---

## Table 4 — Stale tests safe to delete / rewrite

| file:line | test | why obsolete | what it used to assert | what covers it now |
|---|---|---|---|---|
| `tests/system/req039/audit_authority_e2e_test.go:92` | `TestRuntimeAuthorityProtectsRecoveryAndStorageCoordinates` (`fingerprint`, `pending-recovery`, `alternate-state`, `alternate-journal`, `state-symlink`) | `runtime fingerprint` command deleted (`internal/runtime/fingerprint.go`) | authority rejection for a copied Runtime via `runtime fingerprint` | **nothing** — see Table 6 #4 |
| `tests/system/req039/audit_sharedmodel_spine_test.go:280` | `TestAuditSharedModelHookSpineCommittedSource` | content-hash proof dropped | shared-model doc content hash | nothing (hash is not a blueprint guarantee) |
| `tests/system/req039/audit_sources_e2e_test.go:112` | `TestAuditStageSourcesUseOriginalConsumerBoundaryE2E` | SHA comparison dropped | `S3 frozen input differs from original checkout SHA` | nothing (SHA is not a blueprint guarantee) |
| `tests/system/recovery/runtime_recovery_test.go:68` | `TestRuntimeRecoveryInspectAndPlanSurviveMalformedOrBOMState` | recovery plan content-addressing removed | plan retains damaged runtime bytes | `internal/cli/recovery_checkpoint.go:66-90` (size-only inventory) |
| `tests/system/recovery/runtime_recovery_test.go:123` | `TestRuntimeRecoveryApplyRebindsREQAndQuarantinesCorruptRuntime` (digest half only) | `bound_req.sha256` removed | recovered Runtime rebinds REQ **by digest** | rebind by id/path/version: `internal/cli/recovery_checkpoint.go:36-38` |
| `internal/integration/s7_issued_result_external_test.go:433` | both S7 issued-result tests (stderr clause only) | the "already accepted" branch no longer re-announces on stderr | `stderr` contains `original consumer` | the durable receipt at `internal/integration/s7_issued_result_external_test.go:428` (already asserted) — **but see Table 7 #1: the TR-009 promotion is separately broken** |

---

## Table 5 — Stale fixtures to update

| file:line | missing/removed field or path | the fixture code to change |
|---|---|---|
| `tests/system/req039/task_integrate_test.go:21-48` (`seedIntegrableAssignment`) | Assignment `assignment-ti` has no canonical `assignmentrecord.Record` projection → `internal/cli/integration_ack.go:127` rejects | create a canonical Assignment Record (`assignmentrecord.Record` + `AssignmentRecordSHA256` + `AssignmentRevision`) for `assignment-ti`, or migrate the fixture to `internal/assignment` opt-in |
| `tests/system/req039/audit_sharedmodel_integration_test.go` (fixture `seedIntegrableAssignment` reuse), `tests/system/req039/ct_integration_resume_test.go`, `tests/system/req039/final_audit_delivery_test.go`, `tests/system/req039/worktree_governance_test.go`, `tests/system/req039/audit_dispatch_result_binding_test.go` | same legacy-Assignment shape | same fix, or introduce one shared `req039fixtures.SeedCanonicalAssignment` |
| `tests/system/req039/s11_human_decision_branches_test.go:19,52,105,281,321` + `s11_human_decision_test.go:30` | `release_audit` evidence row missing for the current baseline/round | seed a `release_audit` evidence entry (guard `internal/transition/guards.go:290+` `guardACCCurrentFn` → `acceptance.ValidateCurrentS10Evidence`) |
| `tests/system/req039/ct_l4_precondition_test.go:82` (`TestCT03912_BuilderCompleteCommitsTR006System`) + `tests/system/req039/s2_to_s11_hook_driven_test.go` | `builder_responsibility:TASK-039-01/02` guidance rows missing | register the S6 builder responsibility entries before the TR-006 PreToolUse |

---

## Table 6 — UNENFORCED-NEW gaps (relaxed behaviour, nothing catches it)

| # | relaxed behaviour | spec file:line | why nothing catches it now | proposed test + package |
|---|---|---|---|---|
| 1 | `ValidateCheckpointIndices` gutted from **271 → 17 lines**; 26 error paths removed (journal-tail linkage, packet decode, duplicate packet/evidence/reception, frozen binding, identity, domain evidence, answer completeness + digest, original-issuance/journal/control commits, transition binding, supersession/resurrection, old-review invalidation) | `internal/runtime/checkpoint_indices.go:8-17` (HEAD) vs parent `internal/runtime/checkpoint_indices.go:14-271` | **All 26 removed error strings appear in ZERO test files at BOTH HEAD and parent** (verified by exhaustive grep). `internal/runtime/checkpoint_checks_test.go:13-32` asserts only the *relaxed* behaviour (historical receipts without digest proof accepted; state/journal mismatch reported) | `TestCheckpointIndexRejectsForgedReceptionChain` in `internal/runtime` |
| 2 | `internal/runtime/contract_checkpoint.go` (192 lines) deleted — no replacement | blueprint `blueprint/l4-mechanisms/L4-runtime-control-plane.md:166` ("Hook 不扫描来源变化、刷新散列…") | `internal/runtime` is green (679 tests) so the deletion is invisible | `internal/runtime/checkpoint_contract_test.go` |
| 3 | `internal/runtime/review_result_read_cache.go` gutted 209 → 76 lines; `WithReviewResultReadCache` is now a **no-op passthrough** | — | The removed part was a memoization cache + a "JSON-equivalent values must not bypass structural checks after a cache hit" guard; with no cache the structural checks always run ⇒ **no correctness guarantee lost** (performance only) | none needed |
| 4 | Authority protection of `runtime` writers for `--state` / `--journal` overrides and the `.fingerprint-pending.json` marker | `internal/runtime/authority.go:12` (`ErrAuthorityMismatch` still exists) | the only test that drove those coordinates used the deleted `runtime fingerprint` command | `TestRuntimeAuthorityRejectsAlternateCoordinates` in `tests/system/recovery` (use `runtime reconcile --state/--journal`) |
| 5 | `internal/review/workspace.go` gutted **1173 → 44 lines**: `detectUndeclaredProductDriftWithState`, `verifyFrozenSubjects`, `verifySealedArtifactDigests`, `verifyCanonicalFrozenWorktree`, `WorkspaceDigest`, `prepareVerificationWorkspace`, `CaptureFile`/`SanitizeCapture` all deleted with **no successor** (grep for each symbol returns nothing at HEAD) | `blueprint/l4-mechanisms/L4-workflow-convergence.md:260` + `blueprint/l4-mechanisms/L4-hook-acceptance-checklist.md:7` ⇒ **removal intentional** (the blueprint explicitly drops real-time drift verification) | 5 workspace test files deleted with it (`workspace_test.go` -307, `workspace_dependency_test.go` -131, `workspace_context_test.go` -125, `workspace_packet_index_test.go` -84, `workspace_publication_staging_test.go` -58) | none — intentional; but see Table 7 #4 |
| 6 | `internal/runtime/hypothesis_result_continuity.go` + `internal/runtime/review_result_control_paths.go` deleted; **both symbols now have zero references at HEAD** | `blueprint/l4-mechanisms/L4-workflow-convergence.md:260` (S8 no longer needs control-path re-verification) | no test, no caller | see Table 11 |

---

## Table 7 — PRODUCT-BUG candidates (most important)

| # | file:line | assertion / defect | what the spec requires | severity | why it matters |
|---|---|---|---|---|---|
| **1** | `internal/cli/native_review_hooks.go:358-372` (early return) + `internal/cli/native_review_hooks.go:396` | The new "already accepted ⇒ `return true, 0`" branch swallows the S7 Stop/SubagentStop reception **before** the TR-009 promotion. The S7→S10 outlet no longer fires on the Stop fallback, and the `original consumer` diagnostic is never emitted. Both `internal/integration` S7 issued-result tests fail at HEAD and pass at parent. | `blueprint/l4-mechanisms/L4-workflow-convergence.md:437` — "适用必要责任完成且无 Finding 时经 **TR-009** 准备 S10"; `blueprint/l3-stages/L3-S7-verification-round.md:384` — "沿 TR-009 自动进入 S10"; `blueprint/l4-mechanisms/L4-workflow-convergence.md:392` — "不能要求 Main 另用普通工具触发 TR-008／TR-009" | **HIGH** | The S7 exit is a **stage exit**, not a nicety. With the branch in place a clean S7 round never leaves S7 through the natural Stop path — the loop stalls in `verification`. This is the single seam break that makes the whole S7→S10 chain unobservable. |
| **2** | `internal/runtime/recovery.go:522-526` (`ValidateRestore` now called **unconditionally** in `applyRecoveryLocked`) → `internal/cli/recovery_checkpoint.go:180` → `internal/runtime/checkpoint.go:550` | `runtime recover apply` now compares the **recovery candidate** against the **still-present corrupt active Runtime** and aborts with `LOOP_RECOVERY_INPUT_DRIFT`. The parent only invoked the guard for indexed candidates (`internal/runtime/recovery.go:370-375`, inside `if hasIssuedIndices(state)`), so a corrupt Runtime was quarantined first and the comparison was skipped. **MEASURED**: instrumenting `internal/runtime/checkpoint.go:550` at HEAD shows `current={"runtime":"corrupt-idempotency"} pair_runtime_id=loop-REQ-039`; at the parent `ValidateCheckpointRestore` is never called at all. Re-gating HEAD's call on `hasIssuedIndices` makes `TestRuntimeRecoveryApplySamePlanIsIdempotent` **pass** (verified in a throwaway clone). | `blueprint/l4-mechanisms/L4-runtime-control-plane.md` (recovery must not require digest proof) but the *behaviour* (recover a corrupt Runtime) is still required by `tests/system/recovery/runtime_recovery_test.go:75-80` ("RED contract for RR-002, RR-004, RR-008, RR-010") | **HIGH** | Recovery of a **corrupt** Runtime is the entire point of the recovery command. At HEAD the command is unusable for its primary scenario. Second-order: `internal/runtime/recovery.go:648` adds a new `RecoveryConflictError` ("current Runtime has later or different committed progress") that has no parent counterpart and surfaces in the same flow. |
| **3** | `internal/cli/recover_command.go` (apply) + `internal/runtime/store.go:1803` | A pending-commit marker with an unknown/legacy `schema_version` is now a **hard abort** ("original pending Runtime operation is unresolved; preserve the existing files"). The parent deliberately let an invalid marker "fall through so the approved recovery plan can quarantine it". | `internal/runtime/store.go:1803` `unsupported pending runtime commit schema %q`; parent behaviour documented in the refactor's own diff comment | **MED** | A single stale marker (e.g. `""` schema from an older release) now blocks recovery permanently, with no quarantine path. |
| **4** | `internal/assignment/completion_canonical.go:682-688` (HEAD) vs parent `internal/assignment/completion_canonical.go:700-717` | **Removed enforcement** = parent `internal/assignment/completion_canonical.go:711` `err = plancheckpoint.Validate(root, snapshot, agentID, planRef)` (and the `plancheckpoint.ActivatedRef` lookup at `:700`). At HEAD the plan is read (`internal/assignment/completion_canonical.go:689`) but **never validated** ⇒ an out-of-scope S6 Plan is accepted. Secondary enabler: the **legacy-Assignment fallback** in `checkpointPathForLoadedAssignment` was removed (parent `internal/cli/integration_ack.go:131-134` → HEAD `internal/cli/integration_ack.go:126-128`), so assignments without a canonical Record now hard-fail. | `blueprint/l3-stages/L3-S6-build.md:188` — `planned_paths` "**必须落在登记范围，不能扩张 TASK**"; `blueprint/l3-stages/L3-S6-build.md:226` — "当前原生接收…**能拒绝范围**、关联和完成条件映射缺项" | **HIGH** | Scope expansion at S6 is the exact defect the blueprint forbids; the machine check that rejected it was deleted and the system-layer test that would have caught it (`TestS4AuditOutputPathsCannotExpandReviewedScope`, parent `tests/system/req039/audit_dispatch_registration_test.go:152`) was deleted in the same commit. Confirmed by the previous pass (parent `EXIT=0` → HEAD `EXIT=1`, out-of-scope Plan accepted). |
| **5** | `internal/cli/run.go:2693-2700` → `internal/cli/s9_impact_observer.go:20` | `observeMainS9Impact` has **zero test references** (grep over `**/*_test.go` returns none) and short-circuits the S7/S9 observers: `run.go:2696` returns `code` before `run.go:2699 observeMainS9Brief` and `run.go:2700 observeMainS7ReviewPlan`. The new `hook_reader_s5.go:39-46` sets `reader.Kind="Worker"` + `reader.SelectionIssue`, and `internal/cli/hook_reader.go:211-218` returns `true, 0` for `PreToolUse` — swallowing the event before `runControlCycleForHook` (`internal/cli/run.go:3425`) can render the graph diagnostic (`internal/cli/run.go:3446`, `LOOP_RUNTIME_INVALID`). | `blueprint/l4-mechanisms/L4-workflow-convergence.md` §3.3.5 (S7/S9 natural-event observation) | **MED** (HIGH if the S9 Impact path is required to be observed) | Two new surfaces with **no test at all**; the S9 Impact observation can be silently skipped, and the "graph diagnostic" CLI assertion failure reported by the previous pass (`internal/cli/run_test.go:1194`, "missing runtime must surface its graph diagnostic") is explained by this short-circuit. |

---

## Table 8 — Deleted `internal/integration` files

| file | capability | previously covered by | now covered by | verdict |
|---|---|---|---|---|
| `check_receipt.go` | per-check machine receipts bound to the integration checkpoint | parent `internal/integration/canonical_main_integration_external_test.go:208-219,719-720` | `internal/mainreceipt/reader.go:90-91` (`CheckReceipts`, `ToolCheckReceipts`) read-only projection; `internal/review` tool-check receipts | **partially covered** — receipt *storage* survives; `CheckReceipts:[]` is now empty in the failing checkpoint at `tests/system/req039/final_audit_delivery_test.go:68` |
| `cleanup_ignored.go` | inventory of ignored-but-untracked work that must not be silently cleaned | parent `internal/integration/cleanup_ignored_test.go:16,104,175` (3 tests) | HEAD `internal/integration/cleanup_ignored_test.go:11 TestCleanupUnknownIgnoredBlocksOtherwiseCleanShortcut` (1 test); `internal/integration/cleanup_nohash_test.go` | **coverage shrank** 3→1 |
| `native_delivery.go` | native-delivery contract wiring for the integration layer | parent `internal/integration/canonical_*_external_test.go` | `internal/submission/repair_result.go:121`, `internal/submission/repair_result_resolution.go:139` (`workspace.NativeDeliveryPurpose`) | **covered elsewhere** |
| `result_binding.go` | binding of a canonical Result to the Main facts consumed by `Integrate` | parent `internal/integration/result_binding_test.go:33,73,136` (file deleted) | `internal/workpacket` `ReviewResultBinding`/`RepairResultBinding`; `internal/assignment/completion_canonical.go:626-709` | **CAPABILITY LOST at the seam** — the result-binding contract that `checkpointPathForLoadedAssignment` depended on is what broke (Table 7 #4) |
| `worker_private_area.go` | observation of the worker's private area (no cross-boundary writes) | parent `internal/integration/worker_private_area_test.go:56,71,124` (file deleted) | `internal/integration/inspect_responsibility.go`, `integrate_responsibility.go` (new); `internal/integration/main_private_files_external_test.go` | **partially covered** — renamed, but the parent had 3 dedicated assertions now gone |
| `canonical_predecessor.go` | canonical predecessor execution generation from original workspace history | parent `internal/integration/canonical_predecessor_test.go:87`, `canonical_predecessor_external_test.go` (both deleted) | **NOTHING** — grep for `CanonicalPredecessor` / `CanonicalPredecessorFor` at HEAD returns zero hits outside the deleted files | **SILENT SEAM HOLE (HIGH)** — the predecessor-generation chain has no producer and no test |
| *plus 16 deleted test files* | — | `audit_scratch_projection_test.go`, `canonical_dirty_pending_test.go`, `canonical_predecessor{,_external}_test.go`, `canonical_revision_external_test.go`, `canonical_surface_failed_revision_external_test.go`, `captured_log_projection_test.go`, `cleanup_recovery_test.go`, `frozen_paths_test.go`, `git_worktree_projection_test.go`, `metrics_temporary_test.go`, `ready_merge_history{,_external}_test.go`, `result_binding_test.go`, `retry_real_test.go`, `worker_private_area_test.go` | — | **32 top-level tests removed** (115 → 83) with **no replacement assertions** |

---

## Table 9 — CT-039 layer-coverage matrix

| CT ID | `internal/cli` test (in-process `controller.RunControlCycle`) | `tests/system/req039` test (real hook path) | verdict |
|---|---|---|---|
| CT-039-01 | `ct_test.go:33` | `ct_l4_precondition_test.go:16` | **BOTH** |
| CT-039-02 | `ct_test.go:65` | `ct_l4_residual_test.go:16` | **BOTH** |
| CT-039-03 | `ct_test.go:102` | `concurrent_pretooluse_cas_test.go:21` | **BOTH** |
| CT-039-04 | `ct_test.go:135` | — | **ONLY-IN-PROCESS** |
| CT-039-05 | `ct_test.go:162` | `ct_l4_residual_test.go:46` | **BOTH** |
| CT-039-06 | `ct_test.go:191` | `ct_l4_residual_test.go:70` | **BOTH** |
| CT-039-07 | `ct_test.go:218` | `ct_l4_residual_test.go:85` | **BOTH** |
| CT-039-08 | `ct_test.go:259` | `ct_l4_precondition_test.go:146` | **BOTH** |
| CT-039-09 | `ct_test.go:310` | **deleted** (parent `ct_clean_integrate_test.go:20`; file is now helper-only) | **ONLY-IN-PROCESS** ⚠ |
| CT-039-10 | `ct_11_24_test.go:92` | `ct_integration_preserve_test.go:17` | **BOTH** |
| CT-039-11 | `ct_11_24_test.go:19` | `ct_l4_precondition_test.go:52` | **BOTH** |
| CT-039-12 | `ct_11_24_test.go:51`, `ct_only_test.go:11` | `ct_l4_precondition_test.go:82` (**fails at HEAD**) | **BOTH** (system variant currently red) |
| CT-039-13 | `ct_11_24_test.go:107` | `ct_verification_chain_test.go:22`, `:44` | **BOTH** |
| CT-039-14 | `ct_11_24_test.go:135` (asserts only "no deny / no transition") | **deleted** (parent `ct_bug_correction_test.go:14 TestCT03914_FindingToBugRepairFullReview` — the *full* loop) | **ONLY-IN-PROCESS** ⚠⚠ (weakest variant) |
| CT-039-15 | `ct_11_24_test.go:182` | `ct_s11_stop_test.go:12` | **BOTH** |
| CT-039-16 | `ct_11_24_test.go:225` | `ct_l4_residual_test.go:109` | **BOTH** |
| CT-039-17 | `ct_11_24_test.go:254` | `ct_integration_resume_test.go:17` (**fails at HEAD**) | **BOTH** (system variant red) |
| CT-039-18 | `ct_11_24_test.go:260` | `ct_rework_test.go:97` | **BOTH** |
| CT-039-19 | `ct_11_24_test.go:304` | — (deliberately L3, `ct_l4_residual_test.go:3`) | **ONLY-IN-PROCESS** (by design) |
| CT-039-20 | `ct_11_24_test.go:384` | **deleted** (parent `TestCT03920_BuilderCompletionDurableRefSystem`) | **ONLY-IN-PROCESS** ⚠ |
| CT-039-21 | `ct_11_24_test.go:426`, `ct_only_test.go:30` | `ct_verification_chain_test.go:100` | **BOTH** |
| CT-039-22 | — (only a comment at `ct_l4_residual_test.go:3`) | — (comment only) | **NEITHER** ⚠⚠ |
| CT-039-23 | `ct_11_24_test.go:454` | `ct_bug_correction_test.go:12` | **BOTH** |
| CT-039-24 | `ct_11_24_test.go:482` | `ct_l4_precondition_test.go:112` | **BOTH** |

⚠ = covered only by the weaker in-process variant. ⚠⚠ = CT-039-14 lost its *full-loop* system test and CT-039-22 is covered by nobody.
Also note `tests/system/req039/helpers_test.go:8` claims `ct_bug_correction_test.go` holds CT-039-14 and CT-039-22 — it does not (the file holds only CT-039-23). The doc header is stale.

---

## Table 10 — The 12 cross-stage checks

| # | check | verdict | evidence file:line | gap |
|---|---|---|---|---|
| 1 | S6 → S7 (TR-006 Builder completion) | **BROKEN at HEAD** (pre-existing) | `tests/system/req039/ct_l4_precondition_test.go:105`; `tests/system/req039/s2_to_s11_hook_driven_test.go:60` | `builder_responsibility:*` guidance rows never materialise; `GATE-BUILDER-BATCH-READY` stays `not_ready` |
| 2 | S6 scope enforcement (out-of-scope Plan rejected) | **BROKEN (PRODUCT-BUG)** | removed parent `internal/assignment/completion_canonical.go:711`; `internal/plancheckpoint/validate.go:227` still exists but is unreachable | see Table 7 #4 |
| 3 | S7 reception (Reviewer answer → S7) | **BROKEN (PRODUCT-BUG)** | `internal/cli/native_review_hooks.go:358-372` | Stop/SubagentStop fallback is a silent no-op once accepted |
| 4 | S7 → S10 outlet (`RequiredResponsibilitiesComplete` → TR-009) | **BROKEN via #3**; outlet itself **is** exercised from the system layer | `internal/review/completion.go` (`RequiredResponsibilitiesComplete`); `internal/transition/guards.go:282` (TR-009 guard); exercised via `tests/system/req039/ct_verification_chain_test.go:22` and `tests/system/req039/s2_to_s11_hook_driven_test.go:15`; **not** exercised from `internal/integration` (its only S7→S10 driver fails) | no integration-layer coverage of the outlet |
| 5 | S7 → S8 (Finding → investigation) | **PARTIAL** | parent `tests/system/req039/ct_bug_correction_test.go:14` (TR-008) **deleted**; HEAD `tests/system/req039/ct_bug_correction_test.go:12` seeds a *pre-existing* targeted-recheck failure; `internal/cli/ct_11_24_test.go:135` only asserts "no deny" | no system-layer test drives a real Finding into S8 |
| 6 | S8 → S9 (investigation → repair) | **NOT COVERED** | `tests/system/req039/ct_rework_test.go` covers TR-023/TR-024 only; `internal/cli/ct_11_24_test.go:135` asserts only "no deny / no transition" | **no test drives this hop** |
| 7 | S9 → S7 re-verification (full loop) | **NOT COVERED** | parent `TestCT03914_FindingToBugRepairFullReview` (TR-008→PTR-BUG-01→02→04→05→…) **deleted**; `req039fixtures.SeedSealedObservationBatch` **deleted** | **no test drives the full S7→S8→S9→S7 loop**; every hop is covered only in isolation with seeded state |
| 8 | S9 successor release after repair | **NOT COVERED** | parent `tests/system/req039/recheck_delivery_e2e_test.go:22` (deleted), `branch_delivery_regression_test.go:18` (deleted), `recheck_repair_hooks_e2e_test.go:21` (deleted), `native_workspace_chain_test.go:25` (deleted) | the whole S9 delivery/re-check chain lost its L4 layer |
| 9 | S9 Impact observation (`observeMainS9Impact`) | **UNTESTED** | `internal/cli/s9_impact_observer.go:20` (zero test references); short-circuit at `internal/cli/run.go:2696` | see Table 7 #5 |
| 10 | Runtime recovery (corrupt Runtime → apply) | **BROKEN (PRODUCT-BUG)** | `tests/system/recovery/runtime_recovery_test.go:153,367,478`; `internal/runtime/recovery.go:522-526` | see Table 7 #2/#3 |
| 11 | Review identity (S7 plan_id → S10 acceptance/release_audit → S11 decision) | **OK — not a gap** | `internal/cli/audit_identity_hook_e2e_test.go:14 TestUnnumberedS10AndS11ConsumeTheirOriginalSource` (exists at HEAD, asserts `review_plan_id` propagation and the "old PASS does not become current" negative case); new surfaces `internal/reviewmeta/identity.go:16-58`, `internal/acceptance/review_identity.go:7-21` | none (STATIC; the package run is the parallel `internal/cli` reviewer's scope) |
| 12 | Task-Integrate seam (Main integrates the worker branch) | **BROKEN (STALE-FIXTURE, 6 tests)** | `internal/cli/integration_ack.go:126-128` vs parent `:131-134`; failures #4,#5,#6,#9,#11,#19,#20,#21 | legacy-Assignment fixtures must be migrated |

---

## Table 11 — Removed continuity guarantees

| guarantee | removed at | enforcement now | test now | verdict |
|---|---|---|---|---|
| Checkpoint/reception chain coherence (26 checks: journal-tail linkage, duplicate packet/evidence/reception, frozen binding, identity, domain evidence, answer completeness, original-issuance/journal/control commits, transition binding, supersession, resurrection, old-review invalidation) | `internal/runtime/checkpoint_indices.go` 271 → 17 lines | **none** | **none** (0 of the 26 error strings appear in any test at HEAD *or* parent) | **LOST — silent** |
| Runtime checkpoint contract (192 lines) | `internal/runtime/contract_checkpoint.go` deleted | none | none | **LOST — silent** |
| S8 hypothesis result continuity ("an unanswered packet's own sources stayed unchanged across sibling Case revisions") | `internal/runtime/hypothesis_result_continuity.go` deleted | **none** — `VerifyHypothesisResultContinuity` has **zero references at HEAD** | none | **LOST — silent** |
| S7 review-result control-path verification inside the drift scan | `internal/runtime/review_result_control_paths.go` deleted (call site was parent `internal/review/workspace.go:502`) | none — the whole drift scan was removed | none (`internal/review/workspace*_test.go` × 5 deleted, 705 lines) | **LOST — intentional per blueprint** (`L4-workflow-convergence.md:260`) but now **unasserted** |
| Review-result read cache (memoization + "cached JSON must not bypass structural checks") | `internal/runtime/review_result_read_cache.go` 209 → 76 lines; `WithReviewResultReadCache` is a no-op | structural checks always run (no cache ⇒ no bypass) | `internal/runtime` green | **NOT LOST** (performance-only removal) |
| Cross-stage Result → Main-facts binding | `internal/integration/result_binding.go` deleted | `internal/workpacket` bindings + `internal/assignment/completion_canonical.go:626-709` | `internal/integration/result_binding_test.go` deleted | **LOST at the seam** (Table 7 #4) |
| Canonical predecessor execution generation | `internal/integration/canonical_predecessor.go` deleted | **none** | none (`canonical_predecessor*_test.go` deleted) | **LOST — silent seam hole** |
| Main recheck checkpoint / merge receipt recovery | `internal/integration/canonical_revision_external_test.go`, `ready_merge_history*_test.go` deleted | partially in `internal/integration/target_recovery_regression_test.go` | reduced | **SHRUNK** |

---

## Caveats

1. **Scope**: `internal/cli` was **not run** (hard constraint). Items 4, 5, 7 and the `observeMainS9Impact` / `hook_reader_s5.go` / graph-diagnostic findings are **STATIC** and are corroborated by the previous pass's CLI ledger.
2. **`internal/cli/audit_identity_hook_e2e_test.go`** (Table 10 #11) was verified **STATICALLY** (file present at HEAD, assertions match the identity chain, new `reviewmeta`/`acceptance` identity surfaces exist). I did **not** execute it because `internal/cli` is out of scope; the parallel `internal/cli` reviewer owns that datum. The previous pass recorded it as passing.
3. **Parent baseline caveat**: the parent `tests/system/req039` was already broken (45 failures). "REGRESSION" in Table 3 therefore means *parent PASS → HEAD FAIL*, which is the strongest available signal; the parent failures were reproduced in a standalone clone to exclude worktree artifacts.
4. **Durations**: `internal/integration` HEAD 166 s vs parent 406 s — the refactor removed ~60 % of the integration-layer work (digest proofs, capture/install-manifest, drift scans). No timeouts, no SIGTERM, no "signal: terminated" aborts; every package in scope completed.
5. The measurement clone `/tmp/xstage-seams2` contains only `REPORT.md` as an untracked addition; **no file in `/home/sebs/go-system-builder` was modified**. Two throwaway clones (`/tmp/xstage-scratch`, `/tmp/xstage-parentclone`) were used for the root-cause experiments and have been deleted.
6. **Table 2 completeness**: 26 top-level failing tests and 12 failing subtests — all listed. The only skipped test in scope is `TestExportS7BrowserEvaluation` (env-gated, `internal/integration/s7_browser_eval_export_test.go:26`).
7. `tests/fixtures/native-stage-simulation` is consumed by the **Python** driver `tools/claude-full-stage-simulation.py` (its own README), not by Go tests; the S7→S8→S9→S7 loop is currently exercised only there, outside the Go test suite.
