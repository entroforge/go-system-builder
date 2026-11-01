package repair_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
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

	snapshot, session, sessionRef, err := repair.OpenRepairSession(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "main"}, SessionID: "repair-session-1", CreatedBy: "main"})
	if err != nil {
		t.Fatalf("OpenRepairSession() error = %v", err)
	}
	if snapshot.Revision != 1 || session.Status != "planned" {
		t.Fatalf("session snapshot=%d status=%s", snapshot.Revision, session.Status)
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude", "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journal), "r0") || strings.Contains(string(journal), ":-1") {
		t.Fatalf("omitted Runtime revision must not leak into internal S9 event identity: %s", journal)
	}
	snapshot, plan, planRef, err := repair.CompileRepairPlan(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 1, Actor: "main"}, PlanID: "repair-plan-1", CreatedBy: "main"})
	if err != nil {
		t.Fatalf("CompileRepairPlan() error = %v", err)
	}
	if snapshot.Revision != 2 || len(plan.Units) != 1 || planRef.Path == "" || sessionRef.Path == "" {
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
	return repair.ContractRef{Path: rel}, fileHash(data)
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
	return repair.ContractRef{Path: rel}
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
func TestS9TransitionIDWhitelist(t *testing.T) {
	accepted := []string{
		// Loop Definition repair transitions.
		"PTR-BUG-05", "PTR-BUG-06", "PTR-BUG-09", "PTR-BUG-10", "PTR-BUG-11", "TR-012",
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
	// Handoff fixture begins after the current Plan was recorded; native
	// Plan reception and replay are covered through the issued answer tests.
	writer := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	if _, err := writer.Update(2, runtime.Mutation{EventID: "fixture-plan", TransitionID: "PTR-BUG-10", Event: "repair_plan_reported", Actor: "builder-1", RuntimeID: "loop-req039-ct", BaselineGeneration: 1, IdempotencyKey: "fixture-plan", RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		pointer := state["review"].(map[string]any)["repair"].(map[string]any)
		pointer["plan_report_ref"] = planReportRef.Path
		pointer["plan_report_refs"] = []any{map[string]any{"path": planReportRef.Path}}
		pointer["assignment_owners"] = map[string]any{"repair-assignment-unit-1": "builder-1"}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	started, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 3, Actor: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "internal/api/payload.go", "package api\n")
	_, resultRef, err := repair.SubmitRepairResult(root, repair.RepairResultRequest{Contract: contractRef, Session: sessionRef, Plan: planRef, PlanReport: planReportRef, BeforeFixChecks: []repair.RepairCheck{{Name: "original failure", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ResultID: "repair-result-budget", AssignmentID: "repair-assignment-unit-1", ProducerAgentID: "builder-1", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit-1"}}}, ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/payload.go", Status: "added"}}, Checks: []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	// This fixture starts the handoff test from an already recorded Result;
	// current native reception has separate original-entry coverage.
	rawState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var currentState map[string]any
	if err := json.Unmarshal(rawState, &currentState); err != nil {
		t.Fatal(err)
	}
	pointer := currentState["review"].(map[string]any)["repair"].(map[string]any)
	pointer["result_ref"] = resultRef.Path
	pointer["result_refs"] = []any{map[string]any{"path": resultRef.Path}}
	pointer["status"] = "impact_reconciliation"
	encodedState, err := json.MarshalIndent(currentState, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, encodedState, 0644); err != nil {
		t.Fatal(err)
	}
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{ImpactID: "impact-budget", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go"}}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api/payload.go"}, Rationale: "repair changed the boundary", RecoveryEvidence: []string{resultRef.Path}}}, EscalationLevel: "assignment", AnalyzedBy: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: started.Revision, Actor: "main"}, Impact: impactRef}); err != nil {
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
	if _, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: started.Revision + 1, Actor: "qa"}, Reverification: reverifyRef}); err != nil {
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
