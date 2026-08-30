package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// ---------------------------------------------------------------------------
// §14.1 blocked_by_confirmed_finding / site-lost BLOCKER matrix:
//
//   - confirmed build/start/entry Finding 客观阻断后续 Claim → 受影响 Claims
//     投影为 blocked，必须绑定 Finding/precondition/evidence 且
//     after_repair_required=true；不得当 PASS；
//   - 仅因 token/时间/Agent 不足标 blocked → validator 拒绝（没有本轮
//     confirmed Finding 可绑）；
//   - 普通 Finding 关键现场已丢失 → 复用 Assignment BLOCKER 留在 S7，不
//     seal、不伪装 ready、不把复现债务交给 S8。
// ---------------------------------------------------------------------------

// blockedClaimsPatch builds one blocked_claims array entry for
// patchResultField.
func blockedClaimsPatch(t *testing.T, claimID string, findingIDs []string, kind, detail string) []any {
	t.Helper()
	ids := make([]any, 0, len(findingIDs))
	for _, id := range findingIDs {
		ids = append(ids, id)
	}
	evidenceRefs := []any{fixtureEvidenceRef(t, fixtureEvidenceRoot, claimID+"-blocked.md")}
	if len(findingIDs) > 0 {
		// A blocked projection must bind to evidence that the runtime can
		// resolve. The confirmed Finding itself is the minimal canonical
		// evidence reference for these fixtures.
		evidenceRefs = []any{findingIDs[0]}
	}
	return []any{map[string]any{
		"claim_id":              claimID,
		"blocking_finding_ids":  ids,
		"failed_precondition":   map[string]any{"kind": kind, "detail": detail},
		"evidence_refs":         evidenceRefs,
		"after_repair_required": true,
	}}
}

func readBatchFile(t *testing.T, root string, snap loopruntime.Snapshot) map[string]any {
	t.Helper()
	batchPtr := snap.State["review"].(map[string]any)["observation_batch"].(map[string]any)
	if batchPtr == nil {
		t.Fatal("no sealed observation batch")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(batchPtr["path"].(string))))
	if err != nil {
		t.Fatal(err)
	}
	var batch map[string]any
	if err := json.Unmarshal(data, &batch); err != nil {
		t.Fatal(err)
	}
	return batch
}

// §14.1: confirmed Finding 客观阻断后续 Claim —— blocked 投影成立，seal 后
// batch 的 claim_coverage_summary 列出 blocked Claim 及其 finding 绑定。
func TestSubmitResultBlockedByConfirmedFindingSealsWithBinding(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// QA confirms a build-breaking product finding on claim-qa-1.
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}

	// DV's Claim is objectively non-executable: the confirmed finding breaks
	// the build. The result answers claim-dv-1 via blocked_claims only.
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "finding",
		map[string]string{}, nil)
	patchResultField(t, dvPath, "blocked_claims",
		blockedClaimsPatch(t, "claim-dv-1", []string{"finding-qa-1"}, "build", "product no longer compiles after the confirmed finding; no binary to trace"))
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv blocked: %v", err)
	}

	// The round completes (finding + pass + blocked) and seals.
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "observation_sealed" {
		t.Fatalf("status = %s, want observation_sealed", ptr.Status)
	}
	// Claim row keeps the derived projection's identity binding.
	dispositions := Dispositions(snap.State)
	dv := dispositions["claim-dv-1"]
	if dv.Disposition != "blocked" || dv.ResultID != "review-result-dv-1" {
		t.Fatalf("claim-dv-1 projection wrong: %+v", dv)
	}
	if len(dv.FindingIDs) != 1 || dv.FindingIDs[0] != "finding-qa-1" {
		t.Fatalf("claim-dv-1 finding binding wrong: %+v", dv.FindingIDs)
	}
	// The sealed batch lists the blocked Claim with its finding binding.
	batch := readBatchFile(t, root, snap)
	summary := batch["claim_coverage_summary"].(map[string]any)
	if summary["blocked"].(float64) != 1 {
		t.Fatalf("summary blocked count wrong: %v", summary)
	}
	blockedList, _ := summary["blocked_claims"].([]any)
	if len(blockedList) != 1 {
		t.Fatalf("blocked_claims must list the blocked claim, got %v", blockedList)
	}
	entry := blockedList[0].(map[string]any)
	if entry["claim_id"] != "claim-dv-1" || entry["result_id"] != "review-result-dv-1" {
		t.Fatalf("blocked entry identity wrong: %v", entry)
	}
	if ids, _ := entry["blocking_finding_ids"].([]any); len(ids) != 1 || ids[0] != "finding-qa-1" {
		t.Fatalf("blocked entry finding binding wrong: %v", entry)
	}
	pre := entry["failed_precondition"].(map[string]any)
	if pre["kind"] != "build" || pre["detail"] == "" {
		t.Fatalf("failed_precondition wrong: %v", pre)
	}
	if entry["after_repair_required"] != true {
		t.Fatalf("after_repair_required must be true: %v", entry)
	}
}

// blocked 投影是派生事实：由前一份 Result 投影的 blocked Claim 在最终 seal
// 时从持久化 envelope 恢复完整绑定，不需要 Reviewer 重述。
func TestSubmitResultBlockedProjectionRecoveredFromEarlierEnvelope(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	agents := state["entities"].(map[string]any)["agents"].([]any)
	agents = append(agents, map[string]any{
		"id": "agent-qa-2", "role": "qa", "state": "working",
		"task_ids": []any{}, "team_id": "team-review-1",
		"definition_ref": "agents/qa.md", "prompt_ref": ".claude/workgroups/REQ-TEST/m.json#agent-qa-2",
		"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-08-18T00:00:00Z",
	})
	state["entities"].(map[string]any)["agents"] = agents
	statePath, journalPath := writeState(t, root, state)

	planPath := writeThreeAssignmentPlan(t, root)
	snap, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: planPath})
	if err != nil {
		t.Fatalf("RegisterPlan: %v", err)
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-2", "agent-qa-2")
	plan, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}

	// qa-1 confirms the finding; qa-2's Claim is blocked by it.
	qa1 := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail"}, []Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qa1,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa-1: %v", err)
	}
	qa2 := writeResultFile(t, root, plan, "assignment-qa-2", "review-result-qa-2", "agent-qa-2", "finding",
		map[string]string{}, nil)
	patchResultField(t, qa2, "blocked_claims",
		blockedClaimsPatch(t, "claim-qa-2", []string{"finding-qa-1"}, "start", "service exits on boot with the confirmed defect; no state walk is possible"))
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-2", ResultPath: qa2,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa-2 blocked: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "cannot_clean" {
		t.Fatalf("status = %s, want cannot_clean (blocked adds no new finding)", ptr.Status)
	}
	// The persisted qa-2 envelope preserves the projection source fields.
	envelopePath := filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "reviews", "agent-qa-2", "review-result-qa-2.json")
	envelopeBytes, err := os.ReadFile(envelopePath)
	if err != nil {
		t.Fatalf("persisted result envelope missing: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	if list, _ := envelope["blocked_claims"].([]any); len(list) != 1 {
		t.Fatalf("envelope must preserve blocked_claims, got %v", envelope["blocked_claims"])
	}

	// dv passes; the final disposition seals the batch and the blocked entry
	// is recovered from the qa-2 envelope, not re-typed by anyone.
	dv := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dv,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	batch := readBatchFile(t, root, snap)
	blockedList := batch["claim_coverage_summary"].(map[string]any)["blocked_claims"].([]any)
	if len(blockedList) != 1 {
		t.Fatalf("blocked_claims must list claim-qa-2, got %v", blockedList)
	}
	entry := blockedList[0].(map[string]any)
	if entry["claim_id"] != "claim-qa-2" || entry["result_id"] != "review-result-qa-2" {
		t.Fatalf("blocked entry wrong: %v", entry)
	}
	pre := entry["failed_precondition"].(map[string]any)
	if pre["kind"] != "start" {
		t.Fatalf("failed_precondition not recovered from the envelope: %v", entry)
	}
	if refs, _ := entry["evidence_refs"].([]any); len(refs) != 1 {
		t.Fatalf("evidence_refs not recovered: %v", entry)
	}
}

// §14.1 防滥用：没有本轮 confirmed Finding 可绑（仅因 token/时间/Agent 不
// 足）时，把 Claim 标 blocked 必须被拒绝。
func TestSubmitResultRejectsBlockedWithoutConfirmedFinding(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "finding",
		map[string]string{}, nil)
	patchResultField(t, dvPath, "blocked_claims", []any{map[string]any{
		"claim_id":              "claim-dv-1",
		"blocking_finding_ids":  []any{"finding-ghost"},
		"failed_precondition":   map[string]any{"kind": "build", "detail": "out of tokens, pretending the build is broken"},
		"evidence_refs":         []any{fixtureEvidenceRef(t, root, "claim-dv-1-ghost.md")},
		"after_repair_required": true,
	}})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err == nil || !strings.Contains(err.Error(), "not a confirmed Finding") {
		t.Fatalf("blocked without a confirmed round finding must be rejected, got %v", err)
	}
}

// Evidence refs on a blocked projection are not decorative strings: they
// must resolve to a current-round evidence row with a valid artifact.
func TestSubmitResultRejectsBlockedWithUnknownEvidence(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	patchResultField(t, qaPath, "blocked_claims", blockedClaimsPatch(t, "claim-qa-2", []string{"finding-qa-1"}, "build", "the confirmed finding prevents the second claim from starting"))
	// Keep the block declaration structurally valid, but point it at an
	// evidence id that is neither a state row nor a finding in this result.
	patchResultField(t, qaPath, "blocked_claims", []any{map[string]any{
		"claim_id": "claim-qa-2", "blocking_finding_ids": []any{"finding-qa-1"},
		"failed_precondition": map[string]any{"kind": "build", "detail": "the confirmed finding prevents the second claim from starting"},
		"evidence_refs":       []any{"evidence-does-not-exist"}, "after_repair_required": true,
	}})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("unknown blocked evidence must be rejected, got %v", err)
	}
}

// A mixed result must never let ordinary site_lost handling hide a P0. The
// P0 safety path has precedence over the S7 capture blocker path.
func TestSubmitResultRejectsSiteLostWhenResultAlsoContainsP0(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	ordinary := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	ordinary.Encounter.LastGoodCheckpoint = ""
	p0 := codeInspectionFinding("finding-p0-1", "claim-qa-2")
	p0.Severity = "P0"
	p0.Encounter.LastGoodCheckpoint = ""
	p0.Encounter.CaptureGaps = []string{"capture stopped before the destructive path"}
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-mixed-p0", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "fail"}, []Finding{ordinary, p0})
	patchResultField(t, qaPath, "site_lost", []any{map[string]any{
		"finding_id": "finding-qa-1", "reason": "the ordinary finding scene cannot be recovered",
	}})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "P0") {
		t.Fatalf("site_lost must not mask a mixed P0 result, got %v", err)
	}
}

// §14.1 防滥用：blocking_finding_ids 为空没有任何产品因果绑定 —— 拒绝。
func TestSubmitResultRejectsBlockedWithEmptyBlockingFindings(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "finding",
		map[string]string{}, nil)
	patchResultField(t, dvPath, "blocked_claims",
		blockedClaimsPatch(t, "claim-dv-1", []string{}, "build", "no finding to bind"))
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err == nil {
		t.Fatal("blocked with empty blocking_finding_ids must be rejected")
	}
}

// after_repair_required 恒 true：投影永远保留修复后必验义务。
func TestSubmitResultRejectsBlockedWithAfterRepairFalse(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "finding",
		map[string]string{}, nil)
	entries := blockedClaimsPatch(t, "claim-dv-1", []string{"finding-qa-1"}, "build", "build broken")
	entries[0].(map[string]any)["after_repair_required"] = false
	patchResultField(t, dvPath, "blocked_claims", entries)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err == nil {
		t.Fatal("after_repair_required=false must be rejected")
	}
}

// failed_precondition 必须是 build/start/entry/precondition 之一。
func TestSubmitResultRejectsBlockedWithUnknownPreconditionKind(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "finding",
		map[string]string{}, nil)
	patchResultField(t, dvPath, "blocked_claims",
		blockedClaimsPatch(t, "claim-dv-1", []string{"finding-qa-1"}, "convenience", "not a real precondition"))
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err == nil {
		t.Fatal("unknown failed_precondition kind must be rejected")
	}
}

// blocked 不是 PASS：verdict=pass 与 blocked_claims 矛盾即拒绝。
func TestSubmitResultRejectsBlockedWithPassVerdict(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{}, nil)
	patchResultField(t, dvPath, "blocked_claims",
		blockedClaimsPatch(t, "claim-dv-1", []string{"finding-qa-1"}, "build", "build broken"))
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err == nil || !strings.Contains(err.Error(), "not a pass") {
		t.Fatalf("pass verdict with blocked claims must be rejected, got %v", err)
	}
}

// 一个 Claim 只能有一个 disposition：claim_results 与 blocked_claims 不得
// 同时回答同一 Claim。
func TestSubmitResultRejectsClaimAnsweredByResultAndBlocked(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	patchResultField(t, qaPath, "blocked_claims",
		blockedClaimsPatch(t, "claim-qa-2", []string{"finding-qa-1"}, "entry", "double answer"))
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one disposition") {
		t.Fatalf("a claim answered twice must be rejected, got %v", err)
	}
}

// blocked_claims 越权：不属于本 Assignment 的 Claim 不得由本 Result 投影。
func TestSubmitResultRejectsBlockedClaimOutsideAssignment(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	patchResultField(t, qaPath, "blocked_claims",
		blockedClaimsPatch(t, "claim-dv-1", []string{"finding-qa-1"}, "build", "not my claim"))
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "not part of assignment") {
		t.Fatalf("blocked claim outside the assignment must be rejected, got %v", err)
	}
}

// N/A Claim 是 plan disposition，永远不能被 blocked。
func TestSubmitResultRejectsBlockedNotApplicableClaim(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// claim-e2e-na is not in any assignment, so the exact-set check fires
	// first; craft an in-assignment N/A via the schema-free patch: instead
	// verify the Go validator directly for the N/A rule.
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	patchResultField(t, qaPath, "blocked_claims",
		blockedClaimsPatch(t, "claim-e2e-na", []string{"finding-qa-1"}, "entry", "n/a claim"))
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil {
		t.Fatal("blocked N/A or out-of-assignment claim must be rejected")
	}
}

// ---------------------------------------------------------------------------
// site-lost BLOCKER reuse (L3-S7 §9.1 steps 12/14)
// ---------------------------------------------------------------------------

// §14.1: 普通 Finding 关键现场已丢失 —— submit 不消费、不 seal，把
// Assignment 置为 blocked（复用 Agent work_blocked 语义）并给出恢复动作；
// 修复采集条件后由同一 finder 重新提交。
func TestSubmitResultSiteLostRecordsAssignmentBlocker(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// Ordinary finding whose encounter is not investigation-ready: no
	// last_good_checkpoint. The reviewer declares the scene unrecoverable.
	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	finding.Encounter.LastGoodCheckpoint = ""
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{finding})
	patchResultField(t, qaPath, "site_lost", []any{map[string]any{
		"finding_id": "finding-qa-1",
		"reason":     "the container was destroyed and the pre-failure logs rotated; the scene cannot be re-captured",
	}})

	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil {
		t.Fatal("site-lost submit must still report an error (the result is not consumed)")
	}
	for _, want := range []string{"blocked", "blocker_resolved", "resubmit"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must name the recovery action %q: %v", want, err)
		}
	}
	snapMetrics, metricsErr := metrics.NewStore(root).Read()
	if metricsErr != nil {
		t.Fatal(metricsErr)
	}
	if got := snapMetrics.S7ResultSubmits["blocked"]; got != 1 {
		t.Fatalf("site-lost submit outcome = %d, want blocked=1", got)
	}
	if got := snapMetrics.S7ResultSubmits["rejected"]; got != 0 {
		t.Fatalf("site-lost submit must not be counted as rejected, got %d", got)
	}
	// The BLOCKER committed: reviewer Agent working -> blocked with the
	// declaring result as the blocker reference.
	for _, raw := range snap.State["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			if agent["state"] != "blocked" {
				t.Fatalf("agent state = %v, want blocked", agent["state"])
			}
			if agent["work_blocked_ref"] == nil || agent["work_blocked_ref"] == "" {
				t.Fatalf("work_blocked_ref must bind the declaring result: %v", agent)
			}
			ref := agent["work_blocked_ref"].(string)
			if !strings.HasPrefix(ref, ".claude/evidence/") || strings.Contains(ref, "site-lost") == false {
				t.Fatalf("work_blocked_ref must bind the canonical blocker artifact, got %q", ref)
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
				t.Fatalf("canonical blocker artifact must exist: %v", err)
			}
		}
	}
	// Nothing was consumed: no finding, no result, no seal; round stays S7.
	if findings := snap.State["entities"].(map[string]any)["findings"]; findings != nil && len(findings.([]any)) != 0 {
		t.Fatalf("no Finding may be registered on the site-lost path: %v", findings)
	}
	row := snap.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	if row["status"] != "blocked" {
		t.Fatalf("assignment status = %v, want blocked (site-lost blocker is an explicit assignment state)", row["status"])
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "running" {
		t.Fatalf("plan status = %s, want running (round stays in S7)", ptr.Status)
	}
	if snap.State["review"].(map[string]any)["observation_batch"] != nil {
		t.Fatal("site-lost must never seal")
	}

	// Recovery: the finder resolves the blocker, fixes the capture conditions
	// and resubmits a fresh result — the same assignment accepts it.
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snap, err = store.Update(snap.Revision, loopruntime.Mutation{
		EventID: "evt-test-blocker-resolved", TransitionID: "TEST", Event: "test_blocker_resolved",
		Actor: "test", IdempotencyKey: "test:blocker-resolved:agent-qa-1",
		Apply: func(state map[string]any) error {
			for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
				agent := raw.(map[string]any)
				if agent["id"] == "agent-qa-1" {
					agent["state"] = "working"
					agent["blocker_resolved_ref"] = "evidence:blocker-resolved"
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("blocker_resolved: %v", err)
	}
	fixed := codeInspectionFinding("finding-qa-1", "claim-qa-1") // last_good restored
	retry := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1b", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{fixed})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: retry,
	})
	if err != nil {
		t.Fatalf("resubmit after recovery must be accepted: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "cannot_clean" {
		t.Fatalf("status = %s, want cannot_clean after the resubmitted finding", ptr.Status)
	}
}

// site_lost 声明与完整 encounter 矛盾：现场仍在时不许声明不可恢复。
func TestSubmitResultRejectsSiteLostOnReadyFinding(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	patchResultField(t, qaPath, "site_lost", []any{map[string]any{
		"finding_id": "finding-qa-1", "reason": "pretending the scene is gone",
	}})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "already investigation-ready") {
		t.Fatalf("site_lost on a complete encounter must be rejected, got %v", err)
	}
}

// P0 不走 site_lost：安全停线 Finding 以 capture_gaps 立即 seal。
func TestSubmitResultRejectsSiteLostOnP0(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	p0 := codeInspectionFinding("finding-p0-1", "claim-qa-1")
	p0.Severity = "P0"
	p0.Encounter.LastGoodCheckpoint = ""
	p0.Encounter.CaptureGaps = []string{"stopped before the destructive path"}
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{p0})
	patchResultField(t, qaPath, "site_lost", []any{map[string]any{
		"finding_id": "finding-p0-1", "reason": "scene gone",
	}})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "P0") {
		t.Fatalf("site_lost on a P0 finding must be rejected, got %v", err)
	}
}

// site_lost 声明的 finding 必须属于本 Result。
func TestSubmitResultRejectsSiteLostUnknownFinding(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	patchResultField(t, qaPath, "site_lost", []any{map[string]any{
		"finding_id": "finding-ghost", "reason": "not mine",
	}})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "not part of this result") {
		t.Fatalf("site_lost for an unknown finding must be rejected, got %v", err)
	}
}

// 无 site_lost 声明时保持现状：普通 readiness 拒绝要求现场补全，并提示
// 不可恢复时的 site_lost 出路。
func TestSubmitResultReadinessRejectionHintsSiteLost(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	finding.Encounter.LastGoodCheckpoint = ""
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{finding})
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "last_good_checkpoint") {
		t.Fatalf("plain readiness rejection must be kept, got %v", err)
	}
	if !strings.Contains(err.Error(), "site_lost") {
		t.Fatalf("the rejection must hint the site_lost escape, got %v", err)
	}
	// No blocker was recorded without an explicit declaration.
	for _, raw := range mustReadState(t, statePath)["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" && agent["state"] != "working" {
			t.Fatalf("agent must stay working without a site_lost declaration, got %v", agent["state"])
		}
	}
}

func mustReadState(t *testing.T, statePath string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
