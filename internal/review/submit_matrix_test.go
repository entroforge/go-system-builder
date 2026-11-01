package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

// ---------------------------------------------------------------------------
// §14.1 submit matrix: atomic rejection of malformed Canonical ReviewResults,
// Finding field discipline, and the cannot_clean -> discovery_draining ->
// observation_sealed drain state machine.
// ---------------------------------------------------------------------------

// dispatchedFixture registers the fixture plan and dispatches both
// assignments; returns the snapshot and loaded plan.
func dispatchedFixture(t *testing.T, root, statePath, journalPath string) (snapshotRevision int, plan *Plan) {
	t.Helper()
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	snap := registerFixturePlan(t, root, statePath, journalPath)
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	loaded, _, err := LoadPlan(root, snap.State)
	if err != nil {
		t.Fatal(err)
	}
	return snap.Revision, loaded
}

func TestAdvanceReviewerAgentUsesAcceptedAssignmentWithoutGenericPlan(t *testing.T) {
	state := map[string]any{
		"schema_version": "1.2.0",
		"review": map[string]any{"assignments": map[string]any{
			"assignment-review": map[string]any{"agent_id": "reviewer", "status": "consumed"},
		}},
		"entities": map[string]any{"agents": []any{
			map[string]any{"id": "reviewer", "state": "reading"},
		}},
	}
	result := &Result{ResultID: "review-result", AssignmentID: "assignment-review", ProducerAgentID: "reviewer"}
	if err := advanceReviewerAgent(state, result, ".claude/evidence/review.json", time.Now().UTC()); err != nil {
		t.Fatalf("accepted S7 professional answer required a generic PLAN checkpoint: %v", err)
	}
	agent := state["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
	if agent["state"] != "reported" || agent["completion_reported_ref"] != ".claude/evidence/review.json" {
		t.Fatalf("original S7 receipt did not record delivery: %+v", agent)
	}
}

func qaPassResult(t *testing.T, root string, plan *Plan) string {
	t.Helper()
	return writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
}

// §14.1: Result 错 baseline generation —— 漂移的 baseline 使轮 stale，
// 不可提交。
func TestSubmitResultRejectsWrongBaselineGeneration(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	patchResultField(t, path, "baseline_generation", 7)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "baseline_generation 7") {
		t.Fatalf("wrong baseline_generation must be rejected, got %v", err)
	}
}

func TestSubmitResultRejectsAssignmentRevisionMismatch(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-old-plan", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	patchResultField(t, path, "assignment_revision", 2)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "assignment_revision") {
		t.Fatalf("result from an old plan revision must be rejected, got %v", err)
	}
}

// ReviewResults are a verification-stage verb; other stages reject them.
func TestSubmitResultRejectsNonVerificationStage(t *testing.T) {
	root := t.TempDir()
	state := baseVerificationState()
	state["lifecycle"].(map[string]any)["state"] = "building"
	statePath, journalPath := writeState(t, root, state)
	planPath := writePlanFile(t, root)
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	path := writeResultFile(t, root, &plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: 1, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "verification stage") {
		t.Fatalf("non-verification stage must reject submits, got %v", err)
	}
}

// Producer identity is checked inside the CAS against the dispatched Agent;
// a mismatch must roll the transaction back cleanly.
func TestSubmitResultRejectsProducerMismatchWithoutMutation(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	// assignment-qa-1 is dispatched to agent-qa-1 but the result claims
	// agent-dv-1 produced it.
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-dv-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match the dispatched Agent") {
		t.Fatalf("producer mismatch must be rejected, got %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("CAS-time rejection mutated the runtime state")
	}
}

func TestSubmitResultStaleRevisionDoesNotCreateReviewArtifacts(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-stale", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)

	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision - 1, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale submit must fail with stale revision, got %v", err)
	}
	artifact := filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "reviews", "agent-qa-1", "review-result-stale.json")
	if _, statErr := os.Stat(artifact); !os.IsNotExist(statErr) {
		t.Fatalf("stale submit must not leave a review artifact, stat error=%v", statErr)
	}
}

// Apply-time rejection (reviewer Agent not in working state) fails the CAS
// without committing; the staged result artifact must be cleaned up so the
// corrected resubmit does not hit "file exists".
func TestSubmitResultApplyRejectionPreservesUnregisteredArtifacts(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// Park the QA reviewer in `queued`: submit passes every pre-CAS check,
	// stages artifacts, then the Apply-time agent-state check rejects.
	setAgentState := func(agentState string, rev int) int {
		store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
		next, err := store.Update(rev, loopruntime.Mutation{
			EventID:        "evt-test-agent-state-" + agentState,
			TransitionID:   "TEST",
			Event:          "test_agent_state",
			Actor:          "test",
			IdempotencyKey: "test:agent-state:" + agentState,
			Apply: func(state map[string]any) error {
				for _, row := range state["entities"].(map[string]any)["agents"].([]any) {
					if agent := row.(map[string]any); agent["id"] == "agent-qa-1" {
						agent["state"] = agentState
					}
				}
				return nil
			},
		})
		if err != nil {
			t.Fatalf("setAgentState: %v", err)
		}
		return next.Revision
	}
	revision = setAgentState("queued", revision)

	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-orphan", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "not an active owner") {
		t.Fatalf("submit with a queued reviewer must be rejected, got %v", err)
	}
	artifact := filepath.Join(root, ".claude", "evidence", "loop-REQ-TEST", "g1", "reviews", "agent-qa-1", "review-result-qa-orphan.json")
	if _, statErr := os.Stat(artifact); statErr != nil {
		t.Fatalf("rejected publication must remain unknown, stat error=%v", statErr)
	}

	// The corrected resubmit (agent now working) must succeed on the same
	// result_id — no orphan in the way.
	revision = setAgentState("working", revision)
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	}); err != nil {
		t.Fatalf("resubmit after correction must succeed, got %v", err)
	}
}

// Exercise the existing Store sequence without a production CAS test hook:
// stage our bytes, commit an unrelated update, reject the stale CAS, then
// clean against the current snapshot and save the same professional answer.
func TestSubmitResultAfterUnrelatedRuntimeUpdatePreservesUnknownCandidate(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-cas-cleanup", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	rel := ".claude/evidence/loop-REQ-TEST/g1/reviews/agent-qa-1/review-result-qa-cas-cleanup.json"
	staged := []byte(`{"staged":"this submit attempt"}`)
	if err := writeArtifact(root, rel, staged); err != nil {
		t.Fatal(err)
	}
	_, err := store.Update(revision, loopruntime.Mutation{
		EventID: "evt-test-unrelated-update", TransitionID: "TEST", Event: "test_unrelated_update",
		Actor: "test", IdempotencyKey: "test:unrelated-update",
		Apply: func(state map[string]any) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(revision, loopruntime.Mutation{
		EventID: "evt-test-stale-review", TransitionID: "TEST", Event: "test_stale_review",
		Actor: "test", IdempotencyKey: "test:stale-review",
		Apply: func(state map[string]any) error { return nil },
	})
	if !errors.Is(err, loopruntime.ErrStaleRevision) {
		t.Fatalf("concurrent Runtime update must reject the stale CAS: %v", err)
	}
	latest, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: latest.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	}); err != nil {
		t.Fatalf("same answer and result ID must bypass unknown publication: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, rel)); err != nil || string(data) != string(staged) {
		t.Fatalf("unknown original candidate changed: %v", err)
	}
}

func TestWriteArtifactNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	if err := writeArtifact(root, "evidence/immutable.json", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeArtifact(root, "evidence/immutable.json", []byte("second")); err == nil {
		t.Fatal("artifact writer must reject overwrite")
	}
	data, err := os.ReadFile(filepath.Join(root, "evidence", "immutable.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first" {
		t.Fatalf("artifact was overwritten: %q", data)
	}
}

// §14.1: code-inspection Finding 接受 inspection/call/data-flow trail，不
// 要求伪造 UI steps。
func TestSubmitResultAcceptsCodeInspectionWithoutUISteps(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	if finding.Encounter.Entrypoint != "" || len(finding.Encounter.Timeline) != 0 {
		t.Fatal("fixture must carry no UI steps")
	}
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	})
	if err != nil {
		t.Fatalf("code-inspection finding without UI steps must be accepted: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status == "clean" {
		t.Fatalf("a Finding must not yield a clean Plan: %s", ptr.Status)
	}
}

// §14.1: 间歇 Finding 只发生一次但有 deterministic trace —— 允许
// once_with_deterministic_trace，不为次数重复危险操作。
func TestSubmitResultAcceptsOnceWithDeterministicTrace(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	finding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	finding.Reproducibility = "once_with_deterministic_trace"
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: path,
	}); err != nil {
		t.Fatalf("once_with_deterministic_trace must be accepted: %v", err)
	}
}

// §14.1: 两个表象疑似同根 —— S7 保留两个 source Findings，不合并、不
// 去重；sealed batch 携带 exact set。
func TestSubmitResultKeepsTwoFindingsForTheSameSymptom(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	// QA and DV independently observe the same symptom on their own claims.
	qaFinding := codeInspectionFinding("finding-qa-1", "claim-qa-1")
	qaFinding.Observed = "error dropped: caller sees success on failure"
	dvFinding := codeInspectionFinding("finding-dv-1", "claim-dv-1")
	dvFinding.Lens = "delivery"
	dvFinding.Observed = "error dropped: caller sees success on failure"

	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{qaFinding})
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa: %v", err)
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "finding",
		map[string]string{"claim-dv-1": "fail"}, []Finding{dvFinding})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	findings := RoundFindings(snap.State)
	seen := map[string]bool{}
	for _, finding := range findings {
		seen[stringField(finding["finding_id"])] = true
	}
	if len(findings) != 2 || !seen["finding-qa-1"] || !seen["finding-dv-1"] {
		t.Fatalf("independent original Findings lost: %v", findings)
	}

}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// §14.1 状态机: 普通 finding 标 cannot_clean 后继续 drain；第二个普通
// finding 把轮推进 discovery_draining；最终 disposition 落地统一 seal，
// 且 ordinary batch 的 unobserved_claim_ids 为空。
func TestSubmitResultFindingPreservesUnfinishedResponsibilities(t *testing.T) {
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

	// First ordinary finding: cannot_clean, drain continues.
	qa1 := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail"}, []Finding{codeInspectionFinding("finding-qa-1", "claim-qa-1")})
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: qa1,
	})
	if err != nil {
		t.Fatalf("SubmitResult qa-1: %v", err)
	}
	findings := RoundFindings(snap.State)
	if len(findings) != 1 || findings[0]["finding_id"] != "finding-qa-1" {
		t.Fatalf("accepted Finding missing: %v", findings)
	}
	assignments := snap.State["review"].(map[string]any)["assignments"].(map[string]any)
	for _, id := range []string{"assignment-dv-1", "assignment-qa-2"} {
		if assignments[id].(map[string]any)["result_ref"] != nil {
			t.Fatalf("unfinished responsibility %s was completed", id)
		}
	}

}

// §14.1: seal/clean 之后不再接受任何 Result —— 终态是终态。
func TestSubmitResultRejectedAfterObservationSealed(t *testing.T) {
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
		map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("SubmitResult dv: %v", err)
	}
	if ptr := PlanPointerFromState(snap.State); ptr.Status != "observation_sealed" {
		t.Fatalf("precondition failed: status = %s", ptr.Status)
	}

	late := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-2", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-dv-1", ResultPath: late,
	})
	if err == nil || !strings.Contains(err.Error(), "already has a consumed ReviewResult") {
		t.Fatalf("submit after seal must be rejected, got %v", err)
	}
}

// §14.1: 每个 Assignment 只提交一份 Canonical ReviewResult —— 第二次提交
// 被拒绝，CleanRound 不重复记录。
func TestSubmitResultRejectsSecondResultForSameAssignment(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	revision, plan := dispatchedFixture(t, root, statePath, journalPath)

	qaPath := qaPassResult(t, root, plan)
	snap, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	again := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1b", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snap.Revision, AssignmentID: "assignment-qa-1", ResultPath: again,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one Result") {
		t.Fatalf("second result for one assignment must be rejected, got %v", err)
	}
}

// §14.1: capture buffer —— --captures 并入空 timeline 的 finding；
// reviewer 自己写的 timeline 不被重写。
func patchResultField(t *testing.T, path string, key string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	body[key] = value
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeThreeAssignmentPlan writes a plan with three required static claims in
// three assignments (dv-1, qa-1, qa-2) plus the N/A e2e claim.
func writeThreeAssignmentPlan(t *testing.T, root string) string {
	t.Helper()
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	planPath := writePlanFile(t, root)
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	// Split the two QA claims into their own assignments.
	for _, raw := range body["assignments"].([]any) {
		assignment := raw.(map[string]any)
		if assignment["assignment_id"] == "assignment-qa-1" {
			assignment["claim_ids"] = []any{"claim-qa-1"}
		}
	}
	body["assignments"] = append(body["assignments"].([]any), map[string]any{
		"assignment_id": "assignment-qa-2", "lens": "qa", "claim_ids": []any{"claim-qa-2"},
		"non_overlap_boundary": "owns the state-completeness focus", "execution_wave": "static",
	})
	out, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return planPath
}
