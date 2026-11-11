package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "blocked",
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
	if len(RoundFindings(snap.State)) != 1 {
		t.Fatalf("original Finding missing: %v", RoundFindings(snap.State))
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
	qa2 := writeResultFile(t, root, plan, "assignment-qa-2", "review-result-qa-2", "agent-qa-2", "blocked",
		map[string]string{}, nil)
	patchResultField(t, qa2, "blocked_claims",
		blockedClaimsPatch(t, "claim-qa-2", []string{"finding-qa-1"}, "start", "service exits on boot with the confirmed defect; no state walk is possible"))
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-2", ResultPath: qa2,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa-2 blocked: %v", err)
	}
	if findings := RoundFindings(snap.State); len(findings) != 1 || findings[0]["finding_id"] != "finding-qa-1" {
		t.Fatalf("blocked answer lost or invented the original Finding: %v", findings)
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
	if got := Dispositions(snap.State)["claim-qa-2"]; got.Disposition != "blocked" || got.ResultID != "review-result-qa-2" {
		t.Fatalf("blocked original responsibility lost: %+v", got)
	}

}

// A finding verdict needs an actual Finding; blocked work can use blocked.
func TestSubmitResultFindingVerdictRequiresActualFinding(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "finding requires an actual Finding") {
		t.Fatalf("finding verdict without a Finding must be rejected, got %v", err)
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
	if err == nil || !strings.Contains(err.Error(), "pass cannot carry Findings or unfinished blocked responsibilities") {
		t.Fatalf("pass verdict with blocked claims must be rejected, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// site-lost BLOCKER reuse (L3-S7 §9.1 steps 12/14)
// ---------------------------------------------------------------------------

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
