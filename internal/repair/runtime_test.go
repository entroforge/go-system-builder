package repair_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestRuntimeRepairSessionAndPlanAdvanceByCAS(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRel := ".claude/review/investigation/contracts/repair-contract-1-r2.json"
	contract := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-1", "case_id": "investigation-case-1", "revision": 2, "status": "approved", "source_finding_ids": []string{"finding-1"},
		"root_cause_statement": "two payload authorities drift", "violated_invariant": "one payload authority", "causal_model_ref": "case://investigation-case-1/causal-model", "architecture_intent": "centralize the contract",
		"repair_units": []map[string]any{{"id": "unit-1", "description": "restore the payload authority", "assertion_ids": []string{"all"}}}, "prospective_scope": []string{"internal/api"}, "forbidden_scope": []string{"docs/requirements"},
		"symptom_assertions": []string{"value persists"}, "root_invariant_assertions": []string{"one authority"}, "detection_gap_assertions": []string{"contract catches drift"}, "stop_escalation_conditions": []string{"scope expands"},
		"approved_by": "human", "approved_at": "2026-08-25T00:00:00Z", "approval_hash": repeatHex("a", 64),
	}
	contractBytes, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contractBytes = append(contractBytes, '\n')
	contractPath := filepath.Join(root, filepath.FromSlash(contractRel))
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, contractBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRel, "repair_contract_sha256": fileHash(contractBytes)}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, session, sessionRef, err := repair.OpenRepairSession(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-1", CreatedBy: "main"})
	if err != nil {
		t.Fatalf("OpenRepairSession() error = %v", err)
	}
	if snapshot.Revision != 1 || session.Status != "planned" {
		t.Fatalf("session snapshot=%d status=%s", snapshot.Revision, session.Status)
	}
	snapshot, plan, planRef, err := repair.CompileRepairPlan(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-1", CreatedBy: "main"})
	if err != nil {
		t.Fatalf("CompileRepairPlan() error = %v", err)
	}
	if snapshot.Revision != 2 || len(plan.Units) != 1 || planRef.SHA256 == "" || sessionRef.SHA256 == "" {
		t.Fatalf("plan snapshot=%d plan=%#v ref=%#v", snapshot.Revision, plan, planRef)
	}
	current, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if current.State["lifecycle"].(map[string]any)["phase"] != "planning" {
		t.Fatalf("lifecycle did not enter fixing: %#v", current.State["lifecycle"])
	}
}

func TestResumeTargetedReverificationAfterBlockedResult(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "investigation", 0)
	state["review"].(map[string]any)["repair"] = map[string]any{
		"session_id": "repair-session-blocked", "case_id": "investigation-case-1", "contract_id": "repair-contract-1",
		"contract_ref": ".claude/review/investigation/contracts/repair-contract-1.json", "contract_sha256": strings.Repeat("a", 64),
		"path": ".claude/review/repair/sessions/repair-session-blocked.json", "sha256": strings.Repeat("b", 64), "revision": 1,
		"status": "blocked", "targeted_reverification_refs": []string{".claude/review/repair/reverification/blocked.json"},
		"targeted_reverification_artifacts": []any{}, "failure_route": "blocked", "updated_at": "2026-08-26T00:00:00Z",
		"next_action": "resolve the targeted verification blocker, then submit a new independent reverification",
	}
	req039fixtures.WriteState(t, root, state)
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")

	snapshot, err := repair.ResumeTargetedReverification(root, statePath, journalPath, repair.ResumeTargetedRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "qa", OccurredAt: time.Date(2026, 8, 26, 0, 1, 0, 0, time.UTC)},
		Reason:         "the browser session was restored and the verifier can run again",
	})
	if err != nil {
		t.Fatalf("ResumeTargetedReverification() error = %v", err)
	}
	if snapshot.Revision != 1 || snapshot.State["lifecycle"].(map[string]any)["phase"] != "targeted_reverification" {
		t.Fatalf("resume cursor = %#v revision=%d, want targeted_reverification/1", snapshot.State["lifecycle"], snapshot.Revision)
	}
	repairPointer := snapshot.State["review"].(map[string]any)["repair"].(map[string]any)
	if repairPointer["status"] != "targeted_reverification" || !strings.Contains(repairPointer["next_action"].(string), "independent targeted reverification") {
		t.Fatalf("resume pointer = %#v", repairPointer)
	}
	if repairPointer["blocker_resolved_by"] != "qa" || repairPointer["blocker_resolution"] == nil {
		t.Fatalf("resume must retain blocker resolution audit fields: %#v", repairPointer)
	}
	if data, readErr := os.ReadFile(journalPath); readErr != nil {
		t.Fatal(readErr)
	} else if !strings.Contains(string(data), "PTR-BUG-12") {
		t.Fatalf("resume must journal the recovery event: %s", data)
	}
	var persisted map[string]any
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted["lifecycle"].(map[string]any)["phase"] != "targeted_reverification" {
		t.Fatalf("persisted lifecycle = %#v", persisted["lifecycle"])
	}
}

func TestRuntimeRepairRequiresCompleteAssignmentResultBatch(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRel := ".claude/review/investigation/contracts/repair-contract-batch-r2.json"
	contract := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-batch", "case_id": "investigation-case-batch", "revision": 2, "status": "approved", "source_finding_ids": []string{"finding-batch"},
		"root_cause_statement": "two bounded repair units", "violated_invariant": "both boundaries agree", "causal_model_ref": "case://batch/model", "architecture_intent": "split work by unit",
		"repair_units": []map[string]any{{"id": "unit-1", "description": "repair api", "assertion_ids": []string{"all"}}, {"id": "unit-2", "description": "repair persistence", "assertion_ids": []string{"all"}, "depends_on": []string{"unit-1"}}}, "prospective_scope": []string{"internal/api"}, "forbidden_scope": []string{"docs/requirements"},
		"symptom_assertions": []string{"api symptom"}, "root_invariant_assertions": []string{"shared invariant"}, "detection_gap_assertions": []string{"regression catches both"}, "stop_escalation_conditions": []string{"scope expands"},
		"approved_by": "human", "approved_at": "2026-08-25T00:00:00Z", "approval_hash": repeatHex("a", 64),
	}
	data, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, filepath.FromSlash(contractRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-batch", "path": ".claude/review/investigation/cases/investigation-case-batch-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-batch"}, "observation_batch_id": "observation-batch-batch", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRel, "repair_contract_sha256": fileHash(data)}
	req039fixtures.WriteState(t, root, state)
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-batch", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, plan, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-batch", CreatedBy: "main"})
	if err != nil || len(plan.Assignments) != 2 {
		t.Fatalf("compile batch plan = %#v, %v", plan, err)
	}
	for i, agent := range []string{"builder-1", "builder-2"} {
		report, reportRef, reportErr := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: plan.Assignments[i].AssignmentID, AgentID: agent, ReportID: "repair-plan-report-batch-" + string(rune('1'+i)), PlanText: "repair the assigned unit", RedChecks: []repair.RepairCheck{{Name: "pre-fix", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api"}})
		if reportErr != nil {
			t.Fatal(reportErr)
		}
		if _, _, reportErr = repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 2 + i, Actor: agent}, Report: reportRef}); reportErr != nil || report.ReportID == "" {
			t.Fatalf("submit PlanReport[%d] = %v", i, reportErr)
		}
	}
	started, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 4, Actor: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	startedRepair := started.State["review"].(map[string]any)["repair"].(map[string]any)
	if nextAction := startedRepair["next_action"].(string); !strings.Contains(nextAction, "continue the already-dispatched Builder") || strings.Contains(nextAction, "dispatch the bound repair assignment") {
		t.Fatalf("execution begin must point the already-dispatched Builder to its result, got %q", nextAction)
	}
	firstPath := writeFile(t, root, "internal/api/first.go", "package api\n")
	secondPath := writeFile(t, root, "internal/api/second.go", "package api\n")
	firstData, _ := os.ReadFile(firstPath)
	secondData, _ := os.ReadFile(secondPath)
	_, _, _, err = repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "builder-2"}, Result: repair.RepairResultRequest{ResultID: "repair-result-batch-2-early", AssignmentID: "repair-assignment-unit-2", ProducerAgentID: "builder-2", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-2", Status: "pass", EvidenceRefs: []string{"test://unit-2"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/second.go", SHA256: fileHash(secondData), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}})
	if err == nil || !strings.Contains(err.Error(), "queued behind dependency") {
		t.Fatalf("unit-2 must wait for unit-1, got %v", err)
	}
	_, _, _, err = repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "builder-1"}, Result: repair.RepairResultRequest{ResultID: "repair-result-batch-1", AssignmentID: "repair-assignment-unit-1", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit-1"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/first.go", SHA256: fileHash(firstData), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}})
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || intermediate.State["review"].(map[string]any)["repair"].(map[string]any)["status"] != "repairing" {
		t.Fatalf("first Assignment must not close batch: revision=%d err=%v state=%#v", intermediate.Revision, err, intermediate.State["review"])
	}
	final, _, _, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 6, Actor: "builder-2"}, Result: repair.RepairResultRequest{ResultID: "repair-result-batch-2", AssignmentID: "repair-assignment-unit-2", ProducerAgentID: "builder-2", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-2", Status: "pass", EvidenceRefs: []string{"test://unit-2"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/second.go", SHA256: fileHash(secondData), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}})
	if err != nil {
		t.Fatal(err)
	}
	if final.State["review"].(map[string]any)["repair"].(map[string]any)["status"] != "impact_reconciliation" {
		t.Fatalf("complete Assignment batch should release impact reconciliation: %#v", final.State["review"])
	}
}

func TestRuntimeRepairEvidenceChainHandsOffToFreshS7Cursor(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	_, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-1", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-1", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, planReportRef, err := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-1", PlanText: "restore the payload authority", RedChecks: []repair.RepairCheck{{Name: "original failure", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 2, Actor: "builder-1"}, Report: planReportRef}); err != nil {
		t.Fatal(err)
	}
	started, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 3, Actor: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	startedRepair := started.State["review"].(map[string]any)["repair"].(map[string]any)
	if nextAction := startedRepair["next_action"].(string); !strings.Contains(nextAction, "continue the already-dispatched Builder") || strings.Contains(nextAction, "dispatch the bound repair assignment") {
		t.Fatalf("execution begin must point the already-dispatched Builder to its result, got %q", nextAction)
	}
	changedPath := writeFile(t, root, "internal/api/payload.go", "package api\n")
	changedData, _ := os.ReadFile(changedPath)
	_, _, resultRef, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 4, Actor: "builder"}, Result: repair.RepairResultRequest{ResultID: "repair-result-1", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit-1"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/payload.go", SHA256: fileHash(changedData), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}})
	if err != nil {
		t.Fatal(err)
	}
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{ImpactID: "impact-1", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go", SHA256: fileHash(changedData)}}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api/payload.go"}, Rationale: "repair changed the boundary", RecoveryEvidence: []string{resultRef.Path}}}, EscalationLevel: "assignment", AnalyzedBy: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	otherImpact, otherImpactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{ImpactID: "impact-unrelated", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-other", Path: "internal/api/other.go", SHA256: repeatHex("c", 64)}}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api/other.go"}, Rationale: "unrelated artifact", RecoveryEvidence: []string{resultRef.Path}}}, EscalationLevel: "assignment", AnalyzedBy: "qa"})
	if err != nil || otherImpact.ImpactID == "" {
		t.Fatalf("unrelated impact = %#v, %v", otherImpact, err)
	}
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "main"}, Impact: otherImpactRef}); err == nil {
		t.Fatal("CommitChangeImpact must reject an artifact not bound to the current RepairResult")
	}
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "main"}, Impact: impactRef}); err != nil {
		t.Fatal(err)
	}
	// RC-01 fixture strengthening: the performing verifier must be a
	// dispatched identity. Bind a second owner into the runtime
	// assignment_owners map so assignment-s9-qa-verifier resolves to an
	// independent agent (qa) distinct from the repair owner builder-1.
	// This is the minimal state-level dispatch without inventing a second
	// repair unit — the verifier pool is not a repair assignment.
	{
		raw, readErr := os.ReadFile(statePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var cur map[string]any
		if err := json.Unmarshal(raw, &cur); err != nil {
			t.Fatal(err)
		}
		repairMap, _ := cur["review"].(map[string]any)["repair"].(map[string]any)
		if repairMap == nil {
			t.Fatalf("repair pointer missing: %#v", cur["review"])
		}
		owners, _ := repairMap["assignment_owners"].(map[string]any)
		if owners == nil {
			owners = map[string]any{}
			repairMap["assignment_owners"] = owners
		}
		owners["assignment-s9-qa-verifier"] = "qa"
		next, err := json.MarshalIndent(cur, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, next, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// RC-01: the reverification identities must be the dispatched repair
	// chain — the original is the actual repair assignment (its manifest
	// alias assignment-s9-unit-1), the performing verifier is a dispatched
	// identity that is not owned by the repair owner builder-1.
	_, reverifyRef, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{ReverificationID: "reverify-1", RuntimeID: "loop-req039-ct", BugID: "BUG-001", BaselineGeneration: 1, OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-qa-verifier", ContinuityReason: "test://verifier-log: independent verifier", ImpactID: impact.ImpactID, AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://symptom"}}, {AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root"}}, {AssertionID: "gap-1", Result: "pass", EvidenceRefs: []string{"test://gap"}}}, ScopeCompliance: "pass", Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	_, unrelatedReverifyRef, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{ReverificationID: "reverify-unrelated", RuntimeID: "loop-req039-ct", BugID: "BUG-001", BaselineGeneration: 1, OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-independent", ContinuityReason: "test://unrelated: unrelated candidate", ImpactID: "impact-unrelated", AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://symptom"}}, {AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root"}}, {AssertionID: "gap-1", Result: "pass", EvidenceRefs: []string{"test://gap"}}}, ScopeCompliance: "pass", Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 6, Actor: "qa"}, Reverification: unrelatedReverifyRef}); err == nil {
		t.Fatal("CommitTargetedReverification must reject a reverification for a non-current ChangeImpact")
	}
	// RC-01 negative: the original implementer (builder-1, owner of
	// repair-assignment-unit-1) cannot self-verify by filling a fabricated
	// independent verifier ID — the performing identity must resolve to a
	// dispatched assignment that is not owned by the repair owner.
	_, fakeVerifierReverifyRef, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{ReverificationID: "reverify-fake-verifier", RuntimeID: "loop-req039-ct", BugID: "BUG-001", BaselineGeneration: 1, OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-fake-verifier", ContinuityReason: "test://fabricated: fabricated independent verifier", ImpactID: impact.ImpactID, AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://symptom"}}, {AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root"}}, {AssertionID: "gap-1", Result: "pass", EvidenceRefs: []string{"test://gap"}}}, ScopeCompliance: "pass", Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 6, Actor: "qa"}, Reverification: fakeVerifierReverifyRef}); err == nil || !strings.Contains(err.Error(), "not a dispatched verifier identity") {
		t.Fatalf("CommitTargetedReverification must reject a fabricated performing verifier ID, got %v", err)
	}
	// RC-01 negative: the repair owner's own manifest alias is not an
	// independent verifier either — hand-editing the persisted artifact to
	// present the same assignment under two spellings must be rejected at
	// validation time (the artifact pattern forbids the repair-assignment-
	// spelling, so the alias collision is the realistic owner-disguise form).
	ownerDisguise, _, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{ReverificationID: "reverify-owner-disguise", RuntimeID: "loop-req039-ct", BugID: "BUG-001", BaselineGeneration: 1, OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-unit-1x", ContinuityReason: "test://alias: owner alias self-verification", ImpactID: impact.ImpactID, AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://symptom"}}, {AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root"}}, {AssertionID: "gap-1", Result: "pass", EvidenceRefs: []string{"test://gap"}}}, ScopeCompliance: "pass", Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	disguiseData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ownerDisguiseRefPath(ownerDisguise))))
	if err != nil {
		t.Fatal(err)
	}
	disguised := strings.Replace(string(disguiseData), `"performing_assignment_id": "assignment-s9-unit-1x"`, `"performing_assignment_id": "assignment-s9-unit-1"`, 1)
	if disguised == string(disguiseData) {
		t.Fatalf("owner-disguise fixture did not apply: %s", disguiseData)
	}
	disguisePath := filepath.Join(root, filepath.FromSlash(ownerDisguiseRefPath(ownerDisguise)))
	if err := os.WriteFile(disguisePath, []byte(disguised), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.ValidateTargetedReverification(root, repair.ArtifactRef{Path: ownerDisguiseRefPath(ownerDisguise), SHA256: fileHash([]byte(disguised))}); err == nil || !strings.Contains(err.Error(), "not independent") {
		t.Fatalf("ValidateTargetedReverification must reject the owner alias as performing verifier, got %v", err)
	}
	// RC-01 negative: an omitted actor must be a hard rejection — the silent
	// qa default would let an implicit machine identity endorse the gate.
	if _, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 6, Actor: ""}, Reverification: fakeVerifierReverifyRef}); err == nil || !strings.Contains(err.Error(), "requires an explicit actor") {
		t.Fatalf("CommitTargetedReverification must reject an omitted actor, got %v", err)
	}
	if _, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 6, Actor: "qa"}, Reverification: reverifyRef}); err != nil {
		t.Fatal(err)
	}
	var persistedSession repair.RepairSession
	if data, readErr := os.ReadFile(filepath.Join(root, sessionRef.Path)); readErr != nil {
		t.Fatal(readErr)
	} else if unmarshalErr := json.Unmarshal(data, &persistedSession); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	changeset, err := repair.ComputeSessionChangesetRecord(root, persistedSession)
	if err != nil {
		t.Fatal(err)
	}
	changesetRef, err := repair.PersistChangeset(root, changeset)
	if err != nil {
		t.Fatal(err)
	}
	handoff, handoffRef, err := repair.CreateRepairHandoff(root, repair.HandoffRequest{HandoffID: "repair-handoff-1", Session: sessionRef, Plan: planRef, Contract: contractRef, Result: resultRef, Changeset: changesetRef, ChangeImpact: impactRef, TargetedReverifications: []repair.ArtifactRef{reverifyRef}, HandedOffBy: "main", NextAction: "S7 full review", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.HandoffID == "" {
		t.Fatal("handoff was not created")
	}
	_, alternateTargetRef, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{ReverificationID: "reverify-alternate", RuntimeID: "loop-req039-ct", BugID: "BUG-001", BaselineGeneration: 1, OriginalAssignmentID: "assignment-builder", PerformingAssignmentID: "assignment-another-qa", ContinuityReason: "test://alternate: alternate candidate", ImpactID: impact.ImpactID, AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://symptom"}}, {AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root"}}, {AssertionID: "gap-1", Result: "pass", EvidenceRefs: []string{"test://gap"}}}, ScopeCompliance: "pass", Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	alternateHandoff, alternateHandoffRef, err := repair.CreateRepairHandoff(root, repair.HandoffRequest{HandoffID: "repair-handoff-alternate", Session: sessionRef, Plan: planRef, Contract: contractRef, Result: resultRef, Changeset: changesetRef, ChangeImpact: impactRef, TargetedReverifications: []repair.ArtifactRef{alternateTargetRef}, HandedOffBy: "main", NextAction: "S7 full review", OccurredAt: time.Now().UTC()})
	if err != nil || alternateHandoff.HandoffID == "" {
		t.Fatalf("alternate handoff = %#v, %v", alternateHandoff, err)
	}
	if _, err := repair.CommitRepairHandoff(root, statePath, journalPath, repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "main"}, Handoff: alternateHandoffRef}); err == nil {
		t.Fatal("CommitRepairHandoff must reject targeted evidence not recorded by the current Runtime chain")
	}
	snapshot, err := repair.CommitRepairHandoff(root, statePath, journalPath, repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 7, Actor: "main"}, Handoff: handoffRef})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 8 || snapshot.State["lifecycle"].(map[string]any)["state"] != "verification" || snapshot.State["lifecycle"].(map[string]any)["phase"] != "running" {
		t.Fatalf("handoff cursor = %#v revision=%d", snapshot.State["lifecycle"], snapshot.Revision)
	}
	if investigation := snapshot.State["review"].(map[string]any)["investigation"]; investigation != nil {
		t.Fatalf("old S8 investigation pointer must be cleared on TR-012 handoff, got %#v", investigation)
	}
	review := snapshot.State["review"].(map[string]any)
	registeredPlan, ok := review["plan"].(map[string]any)
	if !ok || registeredPlan["status"] != "running" || registeredPlan["plan_id"] != "review-plan-s9-round-2" {
		t.Fatalf("TR-012 handoff must register the generated S7 seed: %#v", review["plan"])
	}
	if assignments, ok := review["assignments"].(map[string]any); !ok || len(assignments) != 2 {
		t.Fatalf("registered S7 seed must expose dispatchable assignments: %#v", review["assignments"])
	}
	roundEntry, ok := review["round_entry"].(map[string]any)
	if !ok || roundEntry["repair_handoff_ref"] != handoffRef.Path || roundEntry["repair_handoff_sha256"] != handoffRef.SHA256 {
		t.Fatalf("TR-012 round entry must retain the repair handoff reference: %#v", review["round_entry"])
	}
	wantEvidence := map[string]string{
		"repair_handoff":          handoffRef.Path,
		"change_impact":           impactRef.Path,
		"targeted_reverification": reverifyRef.Path,
	}
	seenEvidence := map[string]bool{}
	for _, raw := range snapshot.State["evidence"].([]any) {
		entry, _ := raw.(map[string]any)
		kind, _ := entry["kind"].(string)
		if expectedPath, ok := wantEvidence[kind]; ok && entry["path"] == expectedPath {
			seenEvidence[kind] = true
		}
	}
	for kind, path := range wantEvidence {
		if !seenEvidence[kind] {
			t.Fatalf("TR-012 must index %s evidence at %s: %#v", kind, path, snapshot.State["evidence"])
		}
	}
	repairPointer := snapshot.State["review"].(map[string]any)["repair"].(map[string]any)
	nextAction, _ := repairPointer["next_action"].(string)
	if !strings.Contains(nextAction, "runtime review-plan revise") {
		t.Fatalf("TR-012 next_action must explain how to refine the staged seed before dispatch, got %q", nextAction)
	}
}

func ownerDisguiseRefPath(value repair.TargetedReverification) string {
	return ".claude/review/repair/reverification/" + value.ReverificationID + ".json"
}

func writeRuntimeContract(t *testing.T, root string) (repair.ContractRef, string) {
	t.Helper()
	rel := ".claude/review/investigation/contracts/repair-contract-1-r2.json"
	value := map[string]any{"schema_version": "1.0.0", "repair_contract_id": "repair-contract-1", "case_id": "investigation-case-1", "revision": 2, "status": "approved", "source_finding_ids": []string{"finding-1"}, "root_cause_statement": "two payload authorities drift", "violated_invariant": "one payload authority", "causal_model_ref": "case://investigation-case-1/causal-model", "architecture_intent": "centralize the contract", "repair_units": []map[string]any{{"id": "unit-1", "description": "restore authority", "assertion_ids": []string{"all"}}}, "prospective_scope": []string{"internal/api"}, "forbidden_scope": []string{"docs/requirements"}, "symptom_assertions": []string{"value persists"}, "root_invariant_assertions": []string{"one authority"}, "detection_gap_assertions": []string{"contract catches drift"}, "stop_escalation_conditions": []string{"scope expands"}, "approved_by": "human", "approved_at": "2026-08-25T00:00:00Z", "approval_hash": repeatHex("a", 64)}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return repair.ContractRef{Path: rel, SHA256: fileHash(data)}, fileHash(data)
}

func writeScopedRuntimeContract(t *testing.T, root string) repair.ContractRef {
	t.Helper()
	rel := ".claude/review/investigation/contracts/repair-contract-scoped-r2.json"
	value := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-scoped", "case_id": "investigation-case-scoped", "revision": 2, "status": "approved", "source_finding_ids": []string{"finding-scoped"},
		"root_cause_statement": "two bounded authorities drift", "violated_invariant": "each repair unit owns one boundary", "causal_model_ref": "case://scoped/model", "architecture_intent": "keep unit ownership explicit",
		"repair_units": []map[string]any{
			{"id": "unit-api", "description": "repair api", "scope": []string{"internal/api"}, "assertion_ids": []string{"symptom-1"}, "resource_locks": []string{"repo:api"}},
			{"id": "unit-store", "description": "repair store", "scope": []string{"internal/store"}, "assertion_ids": []string{"root-1"}, "resource_locks": []string{"db:test"}},
		},
		"prospective_scope": []string{"internal/api", "internal/store"}, "forbidden_scope": []string{"docs/requirements"}, "symptom_assertions": []string{"api symptom"}, "root_invariant_assertions": []string{"store invariant"}, "detection_gap_assertions": []string{"regression catches both"}, "stop_escalation_conditions": []string{"scope expands"},
		"approved_by": "human", "approved_at": "2026-08-25T00:00:00Z", "approval_hash": repeatHex("a", 64),
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return repair.ContractRef{Path: rel, SHA256: fileHash(data)}
}

func repeatHex(value string, count int) string {
	result := ""
	for len(result) < count {
		result += value
	}
	return result[:count]
}
func fileHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// TestRuntimeRepairBlocksOnAuthorityDrift is the RC-09 (S9-4) negative case:
// after the RepairSession opens and a legitimate RepairResult is committed,
// the repository baseline drifts — an upstream commit or out-of-band edit to
// a baseline file the repair never claimed. The authority fingerprint pinned
// at session open must make the next S9 checkpoint (impact commit) fail
// stale instead of pinning a drifted surface into the next review round.
func TestRuntimeRepairBlocksOnAuthorityDrift(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if _, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-drift", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-drift", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	// Re-read the runtime pointer so the chain artifacts stay transitively bound.
	raw, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var cur map[string]any
	if err := json.Unmarshal(raw, &cur); err != nil {
		t.Fatal(err)
	}
	pointer := cur["review"].(map[string]any)["repair"].(map[string]any)
	sessionRef := repair.ArtifactRef{Path: pointer["path"].(string), SHA256: pointer["sha256"].(string)}
	planRef := repair.ArtifactRef{Path: pointer["plan_ref"].(string), SHA256: pointer["plan_sha256"].(string)}
	if _, planReportRef, reportErr := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-drift", PlanText: "restore the payload authority", RedChecks: []repair.RepairCheck{{Name: "original failure", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}}); reportErr != nil {
		t.Fatal(reportErr)
	} else if _, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 2, Actor: "builder-1"}, Report: planReportRef}); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 3, Actor: "main"}}); err != nil {
		t.Fatal(err)
	}
	// The repair claims exactly its own file and commits cleanly.
	changedPath := writeFile(t, root, "internal/api/payload.go", "package api\n\nfunc Fixed() {}\n")
	changedData, readErr := os.ReadFile(changedPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if _, _, _, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 4, Actor: "builder-1"}, Result: repair.RepairResultRequest{ResultID: "repair-result-drift", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/payload.go", SHA256: fileHash(changedData), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}}); err != nil {
		t.Fatalf("clean result submit before drift must succeed: %v", err)
	}
	// RC-09 (S9-4): AFTER the result, an out-of-band edit mutates a baseline
	// file the repair never claimed — upstream drift. docs/loop-definition.json
	// exists in the fixture root before the session opened, so it is a real
	// baseline path being silently mutated. The next S9 checkpoint must fail
	// stale on the authority fingerprint.
	defPath := filepath.Join(root, "docs", "loop-definition.json")
	defData, readErr := os.ReadFile(defPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	driftedDef := append(defData, '\n')
	if err := os.WriteFile(defPath, driftedDef, 0o644); err != nil {
		t.Fatal(err)
	}
	impactRequest := repair.ChangeImpactRequest{ImpactID: "impact-drift", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go", SHA256: fileHash(changedData)}}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api/payload.go"}, Rationale: "repair changed the boundary", RecoveryEvidence: []string{"test://unit"}}}, EscalationLevel: "assignment", AnalyzedBy: "qa"}
	impact, impactRef, err := repair.CreateChangeImpact(root, impactRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = impact
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "main"}, Impact: impactRef}); err == nil {
		t.Fatal("impact commit must be blocked after out-of-band baseline drift (S9-4 stale authority fingerprint)")
	} else if !strings.Contains(err.Error(), "authority fingerprint is stale") {
		t.Fatalf("expected stale authority fingerprint error, got %v", err)
	}
}

// TestS9TransitionIDWhitelist is the RC-09 (S9-8) negative case: synthetic
// checkpoint TransitionIDs used to be free-form strings written straight
// into the journal. Every emission-site ID must now be whitelisted against
// the Loop Definition repair transitions plus the three pinned S9 runtime
// checkpoints, and an undeclared ID must fail closed.
func TestS9TransitionIDWhitelist(t *testing.T) {
	accepted := []string{
		// Loop Definition repair transitions.
		"PTR-BUG-05", "PTR-BUG-06", "PTR-BUG-09", "PTR-BUG-10", "PTR-BUG-11", "PTR-BUG-12", "TR-012",
		// Pinned S9 runtime checkpoint IDs.
		"S9-SESSION-OPEN", "S9-RESULT-SUBMIT", "S9-TARGETED-FAILURE",
	}
	for _, id := range accepted {
		if err := repair.ValidateS9TransitionID(id); err != nil {
			t.Errorf("declared transition %s must be accepted, got %v", id, err)
		}
	}
	rejected := []string{
		"S9-MADE-UP-CHECKPOINT",
		"PTR-BUG-99",
		"tr-012",
		"",
		"S9-SESSION-OPEN ",
		"TR-008", // declared elsewhere in the catalog but not a repair emission
	}
	for _, id := range rejected {
		if err := repair.ValidateS9TransitionID(id); err == nil {
			t.Errorf("undeclared transition id %q must be rejected", id)
		}
	}
}

// TestChangeImpactRequiredReverificationIDsAreRegistered is the RC-09 (S9-6)
// ordering case: required_reverification_ids is registered as a durable
// obligation when ChangeImpact commits. The downstream TargetedReverification
// artifact is created and committed afterward; RepairHandoff remains the
// consumer that blocks any missing required ID.
func TestChangeImpactRequiredReverificationIDsAreRegistered(t *testing.T) {
	// Drive the standard Impact commit → Targeted create/commit ordering through
	// a minimal runtime chain.
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if _, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-reqids", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-reqids", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	raw, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var cur map[string]any
	if err := json.Unmarshal(raw, &cur); err != nil {
		t.Fatal(err)
	}
	pointer := cur["review"].(map[string]any)["repair"].(map[string]any)
	sessionRef := repair.ArtifactRef{Path: pointer["path"].(string), SHA256: pointer["sha256"].(string)}
	planRef := repair.ArtifactRef{Path: pointer["plan_ref"].(string), SHA256: pointer["plan_sha256"].(string)}
	if _, planReportRef, reportErr := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-reqids", PlanText: "restore the payload authority", RedChecks: []repair.RepairCheck{{Name: "original failure", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}}); reportErr != nil {
		t.Fatal(reportErr)
	} else if _, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 2, Actor: "builder-1"}, Report: planReportRef}); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 3, Actor: "main"}}); err != nil {
		t.Fatal(err)
	}
	changedPath := writeFile(t, root, "internal/api/payload.go", "package api\n\nfunc Fixed() {}\n")
	changedData, readErr := os.ReadFile(changedPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if _, _, _, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 4, Actor: "builder-1"}, Result: repair.RepairResultRequest{ResultID: "repair-result-reqids", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/payload.go", SHA256: fileHash(changedData), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}}); err != nil {
		t.Fatal(err)
	}
	// The impact declares a required reverification ID before its downstream
	// TargetedReverification exists. Impact commit must register the obligation
	// and move the Runtime to targeted_reverification.
	impactRequest := repair.ChangeImpactRequest{ImpactID: "impact-reqids", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go", SHA256: fileHash(changedData)}}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api/payload.go"}, Rationale: "repair changed the boundary", RecoveryEvidence: []string{"test://unit"}}}, EscalationLevel: "assignment", RequiredReverificationIDs: []string{"reverify-reqids"}, AnalyzedBy: "qa"}
	_, impactRef, err := repair.CreateChangeImpact(root, impactRequest)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "main"}, Impact: impactRef})
	if err != nil {
		t.Fatalf("impact commit must register downstream reverification obligation before artifact creation: %v", err)
	}
	if snapshot.State["lifecycle"].(map[string]any)["phase"] != "targeted_reverification" {
		t.Fatalf("impact commit phase = %#v, want targeted_reverification", snapshot.State["lifecycle"])
	}
	repairPointer := snapshot.State["review"].(map[string]any)["repair"].(map[string]any)
	ids, ok := repairPointer["required_reverification_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "reverify-reqids" {
		t.Fatalf("required reverification obligation = %#v, want [reverify-reqids]", repairPointer["required_reverification_ids"])
	}
}

// TestCommitRepairHandoffBudgetGateRejectsOverBudgetRound covers RC-15
// (S9-M1/T1): the handoff is the only site that opens a new full review
// round, so when review.round has already reached max_full_review_rounds the
// commit raises the typed *transition.RepairLimitError (which the CLI bridge
// dispatches through GTR-004) instead of silently opening round N+1.
func TestCommitRepairHandoffBudgetGateRejectsOverBudgetRound(t *testing.T) {
	root, statePath, journalPath, handoffRef := overBudgetHandoffFixture(t)

	// Round already equals the configured budget (1): the handoff must be
	// rejected with the typed limit error before review.round is incremented.
	state := req039fixtures.ReadState(t, root)
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_full_review_rounds"] = 1
	state["review"].(map[string]any)["round"] = 1
	req039fixtures.WriteState(t, root, state)
	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	_, commitErr := repair.CommitRepairHandoff(root, statePath, journalPath, repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: current.Revision, Actor: "main"}, Handoff: handoffRef})
	var limitErr *transition.RepairLimitError
	if commitErr == nil || !errors.As(commitErr, &limitErr) {
		t.Fatalf("CommitRepairHandoff() error = %v, want *transition.RepairLimitError", commitErr)
	}
	if limitErr.Max != 1 {
		t.Fatalf("RepairLimitError.Max = %d, want 1", limitErr.Max)
	}
	after, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.State["review"].(map[string]any)["round"] != float64(1) {
		t.Fatalf("review.round must not advance on a rejected handoff: %v", after.State["review"].(map[string]any)["round"])
	}
}

// overBudgetHandoffFixture drives the full S9 chain to a ready_for_full_review
// pointer with a staged handoff, reusing the same sequence as
// TestRuntimeRepairEvidenceChainHandsOffToFreshS7Cursor.
func overBudgetHandoffFixture(t *testing.T) (string, string, string, repair.ArtifactRef) {
	t.Helper()
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	_, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-budget", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-budget", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	_, planReportRef, err := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: "repair-assignment-unit-1", AgentID: "builder-1", ReportID: "repair-plan-report-budget", PlanText: "restore the payload authority", RedChecks: []repair.RepairCheck{{Name: "original failure", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 2, Actor: "builder-1"}, Report: planReportRef}); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 3, Actor: "main"}}); err != nil {
		t.Fatal(err)
	}
	changedPath := writeFile(t, root, "internal/api/payload.go", "package api\n")
	changedData, _ := os.ReadFile(changedPath)
	_, _, resultRef, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 4, Actor: "builder-1"}, Result: repair.RepairResultRequest{ResultID: "repair-result-budget", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit-1"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/payload.go", SHA256: fileHash(changedData), Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"}})
	if err != nil {
		t.Fatal(err)
	}
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{ImpactID: "impact-budget", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go", SHA256: fileHash(changedData)}}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api/payload.go"}, Rationale: "repair changed the boundary", RecoveryEvidence: []string{resultRef.Path}}}, EscalationLevel: "assignment", AnalyzedBy: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 5, Actor: "main"}, Impact: impactRef}); err != nil {
		t.Fatal(err)
	}
	// Bind an independent verifier owner so the targeted reverification passes
	// the RC-15 identity gate.
	{
		raw, readErr := os.ReadFile(statePath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var cur map[string]any
		if err := json.Unmarshal(raw, &cur); err != nil {
			t.Fatal(err)
		}
		repairMap, _ := cur["review"].(map[string]any)["repair"].(map[string]any)
		owners, _ := repairMap["assignment_owners"].(map[string]any)
		if owners == nil {
			owners = map[string]any{}
			repairMap["assignment_owners"] = owners
		}
		owners["assignment-s9-qa-verifier"] = "qa"
		next, err := json.MarshalIndent(cur, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, next, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, reverifyRef, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{ReverificationID: "reverify-budget", RuntimeID: "loop-req039-ct", BugID: "BUG-001", BaselineGeneration: 1, OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-qa-verifier", ContinuityReason: "test://verifier-log: independent verifier", ImpactID: impact.ImpactID, AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://symptom"}}, {AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root"}}, {AssertionID: "gap-1", Result: "pass", EvidenceRefs: []string{"test://gap"}}}, ScopeCompliance: "pass", Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 6, Actor: "qa"}, Reverification: reverifyRef}); err != nil {
		t.Fatal(err)
	}
	var persistedSession repair.RepairSession
	sessionData, err := os.ReadFile(filepath.Join(root, sessionRef.Path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sessionData, &persistedSession); err != nil {
		t.Fatal(err)
	}
	changeset, err := repair.ComputeSessionChangesetRecord(root, persistedSession)
	if err != nil {
		t.Fatal(err)
	}
	changesetRef, err := repair.PersistChangeset(root, changeset)
	if err != nil {
		t.Fatal(err)
	}
	_, handoffRef, err := repair.CreateRepairHandoff(root, repair.HandoffRequest{HandoffID: "repair-handoff-budget", Session: sessionRef, Plan: planRef, Contract: contractRef, Result: resultRef, Changeset: changesetRef, ChangeImpact: impactRef, TargetedReverifications: []repair.ArtifactRef{reverifyRef}, HandedOffBy: "main", NextAction: "S7 full review", OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	// The budget gate runs before the CAS commit, so the handoff can be
	// replayed against the current revision snapshot taken by the test.
	return root, statePath, journalPath, handoffRef
}

// TestCheckS9AuthorityFreshnessEmptyFingerprintFailsClosed covers RC-15
// (S9-T2/L1): an S9 session pointer without an authority fingerprint no
// longer passes silently; the gate fails closed unless the explicit
// migration escape LOOP_ALLOW_EMPTY_FINGERPRINT=1 is set.
func TestCheckS9AuthorityFreshnessEmptyFingerprintFailsClosed(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	sessionRef, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-legacy", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a legacy session pointer: no authority fingerprint channel.
	raw, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var cur map[string]any
	if err := json.Unmarshal(raw, &cur); err != nil {
		t.Fatal(err)
	}
	pointer := cur["review"].(map[string]any)["repair"].(map[string]any)
	pointer["authority_fingerprint"] = map[string]any{}
	_ = sessionRef
	// Exported validation wrapper: fail closed by default…
	err = repair.ValidateAuthorityFreshness(root, pointer, nil)
	if err == nil || !strings.Contains(err.Error(), "no authority_fingerprint") {
		t.Fatalf("empty fingerprint error = %v, want fail-closed guidance", err)
	}
	// …and releasable only through the explicit migration escape hatch.
	t.Setenv("LOOP_ALLOW_EMPTY_FINGERPRINT", "1")
	if err := repair.ValidateAuthorityFreshness(root, pointer, nil); err != nil {
		t.Fatalf("migration escape must release a legacy session, got %v", err)
	}
}

// TestBindTargetedReverificationRejectsUnownedIdentities covers RC-15
// (S9-H8): the original repair assignment must carry a non-empty recorded
// owner, the performing identity must resolve to a different agent, and an
// unclaimed performing spelling is a fabricated verifier even when it passes
// the plan-membership check.
func TestBindTargetedReverificationRejectsUnownedIdentities(t *testing.T) {
	plan := repair.RepairPlan{PlanID: "repair-plan-owner", Assignments: []repair.RepairAssignment{{AssignmentID: "repair-assignment-unit-1", UnitIDs: []string{"unit-1"}}}}

	// Case 1: original assignment present in the plan but never claimed by a
	// PlanReport (no assignment_owners entry) — the owner is a ghost.
	unownedPointer := map[string]any{"assignment_owners": map[string]any{"assignment-s9-qa-verifier": "qa"}}
	err := repair.BindTargetedReverificationIdentitiesForTest(unownedPointer, plan, repair.TargetedReverification{OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-qa-verifier"})
	if err == nil || !strings.Contains(err.Error(), "has no recorded owner") {
		t.Fatalf("unowned original error = %v, want no-recorded-owner rejection", err)
	}

	// Case 2: performing identity resolves to the same agent as the owner.
	ownedPointer := map[string]any{"assignment_owners": map[string]any{"repair-assignment-unit-1": "builder-1", "assignment-s9-qa-verifier": "builder-1"}}
	err = repair.BindTargetedReverificationIdentitiesForTest(ownedPointer, plan, repair.TargetedReverification{OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-qa-verifier"})
	if err == nil || !strings.Contains(err.Error(), "not independent") {
		t.Fatalf("same-agent verifier error = %v, want independence rejection", err)
	}

	// Case 3: performing assignment dispatched in the plan but claimed by
	// nobody — an unclaimed verifier identity cannot be bound.
	unclaimedPerforming := map[string]any{"assignment_owners": map[string]any{"repair-assignment-unit-1": "builder-1"}}
	err = repair.BindTargetedReverificationIdentitiesForTest(unclaimedPerforming, plan, repair.TargetedReverification{OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-unit-1-unclaimed"})
	if err == nil || !strings.Contains(err.Error(), "not a dispatched verifier identity") {
		t.Fatalf("unclaimed performing error = %v, want dispatched-identity rejection", err)
	}

	// Case 4 (positive): distinct claimed owners in both spellings bind.
	validPointer := map[string]any{"assignment_owners": map[string]any{"repair-assignment-unit-1": "builder-1", "assignment-s9-qa-verifier": "qa"}}
	if err := repair.BindTargetedReverificationIdentitiesForTest(validPointer, plan, repair.TargetedReverification{OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-qa-verifier"}); err != nil {
		t.Fatalf("valid independent binding error = %v", err)
	}
}
