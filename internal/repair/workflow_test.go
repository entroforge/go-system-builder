package repair_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestS9EvidenceChainRequiresImpactAndIndependentReverification(t *testing.T) {
	root := t.TempDir()
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{
		ImpactID: "impact-1", RuntimeID: "loop-REQ-001", ReqID: "REQ-001", BaselineGeneration: 1,
		SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api.go", SHA256: strings.Repeat("a", 64)}},
		Decisions:        []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", ResponsibilityID: nil, Scope: []string{"internal/api.go"}, Rationale: "boundary changed", RecoveryEvidence: []string{"repair-result-1"}}},
		EscalationLevel:  "assignment", AnalyzedBy: "qa",
	})
	if err != nil || impact.RecordType != "change_impact" || impactRef.SHA256 == "" {
		t.Fatalf("CreateChangeImpact() = %#v, %v", impact, err)
	}
	if _, _, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{
		ReverificationID: "reverify-1", RuntimeID: "loop-REQ-001", BugID: "BUG-001", BaselineGeneration: 1,
		OriginalAssignmentID: "assignment-builder", PerformingAssignmentID: "assignment-builder", ImpactID: impact.ImpactID,
		AssertionResults: []repair.AssertionResult{{AssertionID: "assert-1", Result: "pass", EvidenceRefs: []string{"test://reverify-log"}}}, ScopeCompliance: "pass", Result: "pass",
	}); err == nil || !strings.Contains(err.Error(), "independent") {
		t.Fatalf("same assignment should be rejected: %v", err)
	}
	reverify, reverifyRef, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{
		ReverificationID: "reverify-1", RuntimeID: "loop-REQ-001", BugID: "BUG-001", BaselineGeneration: 1,
		OriginalAssignmentID: "assignment-builder", PerformingAssignmentID: "assignment-qa", ContinuityReason: "test://verifier-log: independent verification", ImpactID: impact.ImpactID,
		AssertionResults: []repair.AssertionResult{{AssertionID: "assert-1", Result: "pass", EvidenceRefs: []string{"test://reverify-log"}}}, ScopeCompliance: "pass", Result: "pass",
	})
	if err != nil || reverify.Result != "pass" || reverifyRef.SHA256 == "" {
		t.Fatalf("CreateTargetedReverification() = %#v, %v", reverify, err)
	}
	if _, err := repair.ValidateTargetedReverification(root, reverifyRef); err != nil {
		t.Fatalf("ValidateTargetedReverification() error = %v", err)
	}
}

func TestS9EvidenceChainAcceptsCanonicalCaseIdentityWithoutBugID(t *testing.T) {
	root := t.TempDir()
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{
		ImpactID: "impact-case-only", RuntimeID: "loop-REQ-001", ReqID: "REQ-001", BaselineGeneration: 1,
		SourceCaseIDs: []string{"investigation-case-001"}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts: []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api.go", SHA256: strings.Repeat("a", 64)}},
		Decisions:        []repair.ImpactDecision{{SourceID: "investigation-case-001", TargetID: "root-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", Scope: []string{"internal/api.go"}, Rationale: "case contract changed the boundary", RecoveryEvidence: []string{"repair-result-case-only"}}},
		AnalyzedBy:       "investigator",
	})
	if err != nil || impact.ImpactID == "" || impactRef.SHA256 == "" || len(impact.SourceCaseIDs) != 1 {
		t.Fatalf("case-only ChangeImpact = %#v, %v", impact, err)
	}
	reverification, ref, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{
		ReverificationID: "reverify-case-only", RuntimeID: impact.RuntimeID, CaseID: "investigation-case-001", BaselineGeneration: 1,
		OriginalAssignmentID: "assignment-builder", PerformingAssignmentID: "assignment-qa", ContinuityReason: "test://verifier-log: independent verification", ImpactID: impact.ImpactID,
		AssertionResults: []repair.AssertionResult{{AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://root"}}}, ScopeCompliance: "pass", Result: "pass",
	})
	if err != nil || reverification.CaseID == "" || ref.SHA256 == "" {
		t.Fatalf("case-only targeted reverification = %#v, %v", reverification, err)
	}
	if _, err := repair.ValidateTargetedReverification(root, ref); err != nil {
		t.Fatalf("ValidateTargetedReverification(case-only) error = %v", err)
	}
}

func TestS9RequestJSONUsesStableSnakeCaseIdentityFields(t *testing.T) {
	var request repair.RepairResultRequest
	if err := json.Unmarshal([]byte(`{"result_id":"repair-result-json","producer_agent_id":"builder","assignment_id":"repair-assignment-unit-1","result":"blocked"}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.ResultID != "repair-result-json" || request.ProducerAgentID != "builder" || request.AssignmentID != "repair-assignment-unit-1" {
		t.Fatalf("snake_case request fields were not decoded: %#v", request)
	}
}

func TestS9HandoffRejectsMissingChainAndPersistsCompleteChain(t *testing.T) {
	root := t.TempDir()
	missing, err := repair.CheckRepairHandoff(root, repair.RepairHandoff{HandoffID: "repair-handoff-1"})
	if err != nil || missing.Complete || len(missing.Missing) == 0 {
		t.Fatalf("missing handoff check = %#v, %v", missing, err)
	}
	// The handoff writer itself must reject an absent targeted re-verification.
	_, _, err = repair.CreateRepairHandoff(root, repair.HandoffRequest{HandoffID: "repair-handoff-1", HandedOffBy: "main", NextAction: "S7 full review"})
	if err == nil || !strings.Contains(err.Error(), "targeted") {
		t.Fatalf("incomplete handoff error = %v", err)
	}
}

func TestS9HandoffCompletenessChecksReferencedArtifactIntegrity(t *testing.T) {
	root := t.TempDir()
	check, err := repair.CheckRepairHandoff(root, repair.RepairHandoff{
		HandoffID:                  "repair-handoff-1",
		HandedOffBy:                "main",
		NextAction:                 "S7 review",
		SessionRef:                 repair.ArtifactRef{Path: "session.json", SHA256: strings.Repeat("a", 64)},
		PlanRef:                    repair.ArtifactRef{Path: "plan.json", SHA256: strings.Repeat("a", 64)},
		ContractRef:                repair.ArtifactRef{Path: "contract.json", SHA256: strings.Repeat("a", 64)},
		ResultRef:                  repair.ArtifactRef{Path: "result.json", SHA256: strings.Repeat("a", 64)},
		ChangesetRef:               repair.ArtifactRef{Path: "changeset.json", SHA256: strings.Repeat("a", 64)},
		ChangeImpactRef:            repair.ArtifactRef{Path: "impact.json", SHA256: strings.Repeat("a", 64)},
		TargetedReverificationRefs: []repair.ArtifactRef{{Path: "reverify.json", SHA256: strings.Repeat("a", 64)}},
	})
	if err != nil {
		t.Fatalf("CheckRepairHandoff() error = %v", err)
	}
	if check.Complete || len(check.Invalid) == 0 {
		t.Fatalf("integrity-broken handoff check = %#v", check)
	}
}

func TestS9ChangesetExplicitPathHasStableReferenceIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "api.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changeset, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: "repair-session-1", ExplicitPaths: []string{"internal/api.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changeset.Artifacts) != 1 || changeset.Artifacts[0].ID == "" || changeset.Digest == "" {
		t.Fatalf("changeset = %#v", changeset)
	}

	// Ensure the persisted representation remains schema-compatible.
	data, err := json.Marshal(changeset)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "repair_changeset") {
		t.Fatalf("unexpected changeset JSON: %s", data)
	}
}

func TestS9ChangesetGitDiffFingerprintsCommittedRange(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "commit.gpgsign", "false")
	path := writeFile(t, root, "internal/api.go", "package api\nconst V = 1\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(path, []byte("package api\nconst V = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "repair")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

	changeset, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: "repair-session-1", BaseRef: base, HeadRef: head})
	if err != nil {
		t.Fatalf("git changeset error = %v", err)
	}
	if changeset.Source != "git_diff" || len(changeset.Artifacts) != 1 || changeset.Artifacts[0].Path != "internal/api.go" {
		t.Fatalf("git changeset = %#v", changeset)
	}
}

func TestS9ChangesetIncludesDeletedArtifactsFromGitRange(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "commit.gpgsign", "false")
	path := writeFile(t, root, "internal/removed.go", "package removed\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "delete")
	head := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	changeset, err := repair.ComputeChangeset(root, repair.ChangesetRequest{SessionID: "repair-session-1", BaseRef: base, HeadRef: head})
	if err != nil {
		t.Fatal(err)
	}
	if len(changeset.Artifacts) != 1 || changeset.Artifacts[0].Status != "deleted" || changeset.Artifacts[0].Path != "internal/removed.go" {
		t.Fatalf("deleted changeset = %#v", changeset)
	}
}

func TestRepairResultRejectsUnreportedSessionChanges(t *testing.T) {
	root := t.TempDir()
	contractRef, _ := writeRuntimeContract(t, root)
	session, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-diff", RuntimeID: "loop-REQ-039",
		ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{
		Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-diff", CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	first := writeFile(t, root, "internal/api/first.go", "package api\n")
	second := writeFile(t, root, "internal/api/second.go", "package api\n")
	firstData, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = repair.SubmitRepairResult(root, repair.RepairResultRequest{
		Contract: contractRef, Session: sessionRef, Plan: planRef, ResultID: "repair-result-diff",
		ProducerAgentID: "builder", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://unit"}}},
		ChangedArtifacts: []repair.ChangedArtifact{{Path: "internal/api/first.go", SHA256: fileHash(firstData), Status: "added"}}, Result: "pass",
		BeforeFixChecks: []repair.RepairCheck{{Name: "pre-fix", Command: "go test ./...", Result: "fail", EvidenceRefs: []string{"test://red"}}},
		Checks:          []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "actual Session diff") {
		t.Fatalf("unreported session change should be rejected: %v", err)
	}
	_ = session
	_ = second
}

func TestRepairResultAllowsBlockedOutcomeWithoutRepositoryDiff(t *testing.T) {
	root := t.TempDir()
	contractRef, _ := writeRuntimeContract(t, root)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-no-diff", RuntimeID: "loop-REQ-039",
		ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{
		Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-no-diff", CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ref, err := repair.SubmitRepairResult(root, repair.RepairResultRequest{
		Contract: contractRef, Session: sessionRef, Plan: planRef, ResultID: "repair-result-no-diff",
		ProducerAgentID: "builder", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "blocked", EvidenceRefs: []string{"runtime://blocker"}}},
		Result:          "blocked",
		BeforeFixChecks: []repair.RepairCheck{{Name: "pre-fix", Command: "go test ./...", Result: "blocked", EvidenceRefs: []string{"runtime://blocker"}}},
		ResidualRisks:   []string{"the approved Contract cannot be executed in the current environment"},
	})
	if err != nil {
		t.Fatalf("blocked no-diff result should be persisted: %v", err)
	}
	if result.Result != "blocked" || len(result.ChangedArtifacts) != 0 || ref.SHA256 == "" {
		t.Fatalf("blocked no-diff result = %#v ref=%#v", result, ref)
	}
}

func TestRepairPlanCreatesExplicitAssignmentAndPlanReportRequiresRedCheck(t *testing.T) {
	root := t.TempDir()
	contractRef, _ := writeRuntimeContract(t, root)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{Contract: contractRef, SessionID: "repair-session-plan-report", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-plan-report", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Assignments) != len(plan.Units) || plan.Assignments[0].UnitIDs[0] != plan.Units[0].ID {
		t.Fatalf("plan assignments do not partition units: %#v", plan.Assignments)
	}
	_, _, err = repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: plan.Assignments[0].AssignmentID, AgentID: "builder", ReportID: "repair-plan-report-green-only", PlanText: "write the fix", RedChecks: []repair.RepairCheck{{Name: "pre-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{"test://green"}}}, ProposedPaths: []string{"internal/api/payload.go"}})
	if err == nil || !strings.Contains(err.Error(), "fail or blocked") {
		t.Fatalf("green-only plan report should be rejected: %v", err)
	}
	report, ref, err := repair.CreatePlanReport(root, repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: plan.Assignments[0].AssignmentID, AgentID: "builder", ReportID: "repair-plan-report-red", PlanText: "write the fix", RedChecks: []repair.RepairCheck{{Name: "pre-fix", Command: "go test ./...", Result: "fail", EvidenceRefs: []string{"test://red"}}}, ProposedPaths: []string{"internal/api/payload.go"}})
	if err != nil || report.Status != "reported" || ref.SHA256 == "" {
		t.Fatalf("red plan report = %#v %v", report, err)
	}
}

func TestRepairPlanPreservesPerUnitScopeAndAssertionBoundaries(t *testing.T) {
	root := t.TempDir()
	contractRef := writeScopedRuntimeContract(t, root)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-scoped", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-scoped", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Assignments) != 2 {
		t.Fatalf("scoped plan assignments = %#v", plan.Assignments)
	}
	if got := plan.Assignments[0]; strings.Join(got.Scope, ",") != "internal/api" || strings.Join(got.AssertionIDs, ",") != "symptom-1" || strings.Join(got.ResourceLocks, ",") != "repo:api" {
		t.Fatalf("api assignment lost its boundary: %#v", got)
	}
	if got := plan.Assignments[1]; strings.Join(got.Scope, ",") != "internal/store" || strings.Join(got.AssertionIDs, ",") != "root-1" || strings.Join(got.ResourceLocks, ",") != "db:test" {
		t.Fatalf("store assignment lost its boundary: %#v", got)
	}
}

func TestRepairPlanCarriesDependenciesAndRejectsCycles(t *testing.T) {
	root := t.TempDir()
	contractRef := writeDependencyContract(t, root, false)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-dependency", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-dependency", CreatedBy: "main"})
	if err != nil {
		t.Fatalf("CreateRepairPlan() error = %v", err)
	}
	if len(plan.Assignments) != 2 || len(plan.Assignments[1].DependsOn) != 1 || plan.Assignments[1].DependsOn[0] != "unit-a" {
		t.Fatalf("assignment dependencies = %#v, want unit-b depends on unit-a", plan.Assignments)
	}
	if _, err := repair.ValidateRepairPlan(root, planRef); err != nil {
		t.Fatalf("ValidateRepairPlan() error = %v", err)
	}

	cycleContract := writeDependencyContract(t, root, true)
	_, cycleSessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: cycleContract, SessionID: "repair-session-cycle", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: cycleContract, Session: cycleSessionRef, PlanID: "repair-plan-cycle", CreatedBy: "main"}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cyclic RepairPlan should be rejected with cycle guidance: %v", err)
	}
}

func TestRepairPlanRejectsAssignmentResourceLockDrift(t *testing.T) {
	root := t.TempDir()
	contractRef := writeDependencyContract(t, root, false)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-lock-drift", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-lock-drift", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	plan.Assignments[0].ResourceLocks = []string{"repo:unrelated"}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, ".claude/review/repair/plans/repair-plan-lock-drift-copy.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = repair.ValidateRepairPlan(root, repair.ArtifactRef{Path: ".claude/review/repair/plans/repair-plan-lock-drift-copy.json", SHA256: fileHash(data)})
	if err == nil || !strings.Contains(err.Error(), "resource-lock coverage") {
		t.Fatalf("expected assignment resource lock drift to be rejected, got %v", err)
	}
}

func TestRepairPlanRejectsAssignmentDependencyWhenUnitHasNone(t *testing.T) {
	root := t.TempDir()
	contractRef := writeDependencyContract(t, root, false)
	contract, err := repair.ValidateApprovedContractRef(root, contractRef)
	if err != nil {
		t.Fatal(err)
	}
	// The immutable plan validator must not allow an Assignment to invent a
	// dependency that its Contract unit never declared. This is the inverse of
	// the normal copy-down check and protects runtime scheduling from a phantom
	// edge.
	plan := map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_plan", "plan_id": "repair-plan-extra-dependency",
		"session_id": "repair-session-extra-dependency", "contract_id": contract.ContractID,
		"contract_ref": contract.Ref.Path, "contract_sha256": contract.Ref.SHA256,
		"units": []any{
			map[string]any{"id": "unit-1", "description": "one unit"},
		},
		"assignments": []any{
			map[string]any{"assignment_id": "repair-assignment-unit-1", "unit_ids": []any{"unit-1"}, "depends_on": []any{"unit-1"}, "status": "queued", "scope": []any{"internal/api"}, "contract_ref": contract.Ref.Path},
		},
		"execution_policy": "coverage_complete", "prospective_scope": []any{"internal/api"}, "forbidden_scope": []any{"docs/requirements"},
		"created_by": "test", "created_at": "2026-08-25T00:00:00Z",
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, ".claude/review/repair/plans/repair-plan-extra-dependency.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = repair.ValidateRepairPlan(root, repair.ArtifactRef{Path: ".claude/review/repair/plans/repair-plan-extra-dependency.json", SHA256: fileHash(data)})
	if err == nil || !strings.Contains(err.Error(), "dependency coverage") {
		t.Fatalf("expected invented assignment dependency to be rejected, got %v", err)
	}
}

func writeDependencyContract(t *testing.T, root string, cycle bool) repair.ContractRef {
	t.Helper()
	name := "dependency"
	if cycle {
		name = "cycle"
	}
	units := []map[string]any{
		{"id": "unit-a", "description": "repair boundary a", "scope": []string{"internal/a"}, "assertion_ids": []string{"symptom-1"}, "resource_locks": []string{"repo:a"}},
		{"id": "unit-b", "description": "repair boundary b", "scope": []string{"internal/b"}, "assertion_ids": []string{"root-1"}, "resource_locks": []string{"repo:b"}},
	}
	if cycle {
		units[0]["depends_on"] = []string{"unit-b"}
		units[1]["depends_on"] = []string{"unit-a"}
	} else {
		units[1]["depends_on"] = []string{"unit-a"}
	}
	value := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-" + name, "case_id": "investigation-case-" + name, "revision": 2, "status": "approved", "source_finding_ids": []string{"finding-" + name},
		"root_cause_statement": "two bounded authorities drift", "violated_invariant": "each repair unit owns one boundary", "causal_model_ref": "case://" + name + "/model", "architecture_intent": "keep unit ownership explicit",
		"repair_units": units, "prospective_scope": []string{"internal/a", "internal/b"}, "forbidden_scope": []string{"docs/requirements"}, "symptom_assertions": []string{"api symptom"}, "root_invariant_assertions": []string{"store invariant"}, "detection_gap_assertions": []string{"regression catches both"}, "stop_escalation_conditions": []string{"scope expands"},
		"approved_by": "human", "approved_at": "2026-08-25T00:00:00Z", "approval_hash": repeatHex("a", 64),
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	rel := ".claude/review/investigation/contracts/repair-contract-" + name + "-r2.json"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return repair.ContractRef{Path: rel, SHA256: fileHash(data)}
}

func TestTargetedFailurePersistsTypedRecoveryRoute(t *testing.T) {
	root := t.TempDir()
	impact, _, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{
		ImpactID: "impact-failure-route", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1,
		SourceBugIDs: []string{"BUG-039"}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts: []repair.ArtifactRef{{Path: "internal/api.go", SHA256: strings.Repeat("a", 64)}},
		Decisions:        []repair.ImpactDecision{{SourceID: "BUG-039", TargetID: "root-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", Scope: []string{"internal/api.go"}, Rationale: "boundary changed", RecoveryEvidence: []string{"repair-result-1"}}},
		AnalyzedBy:       "qa",
	})
	if err != nil {
		t.Fatal(err)
	}
	value, ref, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{
		ReverificationID: "reverify-failure-route", RuntimeID: impact.RuntimeID, BugID: "BUG-039", BaselineGeneration: 1,
		OriginalAssignmentID: "assignment-builder", PerformingAssignmentID: "assignment-qa", ContinuityReason: "test://new-symptom: independent verifier found a new causal symptom", ImpactID: impact.ImpactID,
		AssertionResults: []repair.AssertionResult{{AssertionID: "root-1", Result: "fail", EvidenceRefs: []string{"test://root-fail"}}}, ScopeCompliance: "pass", Result: "fail", FailureClass: "fail_new_cause",
	})
	if err != nil || value.FailureClass != "fail_new_cause" || ref.SHA256 == "" {
		t.Fatalf("typed targeted failure = %#v %v", value, err)
	}
	validated, err := repair.ValidateTargetedReverification(root, ref)
	if err != nil || validated.FailureClass != "fail_new_cause" {
		t.Fatalf("typed targeted failure validation = %#v %v", validated, err)
	}
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, root, relative, contents string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRepairPlanRejectsUnitWithoutExplicitAssertionDeclaration is the RC-09
// (S9-12) negative case: the planner used to silently copy the FULL contract
// assertion surface onto any unit that declared no assertion_ids of its own,
// inflating the reverification surface. Coverage must now be declared — a
// per-unit list, or the explicit ["all"] full-surface form.
func TestRepairPlanRejectsUnitWithoutExplicitAssertionDeclaration(t *testing.T) {
	root := t.TempDir()
	value := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-implicit", "case_id": "investigation-case-implicit", "revision": 2, "status": "approved", "source_finding_ids": []string{"finding-implicit"},
		"root_cause_statement": "implicit assertion copy", "violated_invariant": "one authority", "causal_model_ref": "case://implicit/model", "architecture_intent": "explicit coverage",
		"repair_units":      []map[string]any{{"id": "unit-1", "description": "no assertion declaration"}},
		"prospective_scope": []string{"internal/api"}, "forbidden_scope": []string{"docs/requirements"},
		"symptom_assertions": []string{"value persists"}, "root_invariant_assertions": []string{"one authority"}, "detection_gap_assertions": []string{"contract catches drift"}, "stop_escalation_conditions": []string{"scope expands"},
		"approved_by": "human", "approved_at": "2026-08-25T00:00:00Z", "approval_hash": repeatHex("a", 64),
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	rel := ".claude/review/investigation/contracts/repair-contract-implicit-r2.json"
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	contractRef := repair.ContractRef{Path: rel, SHA256: fileHash(data)}
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-implicit", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-implicit", CreatedBy: "main"}); err == nil || !strings.Contains(err.Error(), "declares no assertion_ids") {
		t.Fatalf("plan compile must reject a unit without an explicit assertion declaration, got %v", err)
	}

	// The explicit ["all"] declaration still reaches the full surface.
	value["repair_units"] = []map[string]any{{"id": "unit-1", "description": "explicit full surface", "assertion_ids": []string{"all"}}}
	data, err = json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	contractRef.SHA256 = fileHash(data)
	_, sessionRef, err = repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-implicit-all", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-implicit-all", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Assignments[0]; strings.Join(got.AssertionIDs, ",") != "symptom-1,root-1,gap-1" {
		t.Fatalf("explicit all expansion = %v, want symptom-1,root-1,gap-1", got.AssertionIDs)
	}
}

// TestTargetedReverificationRejectsUnanchoredContinuityReason is the RC-09
// (S9-11) negative case: continuity_reason used to be silently defaulted to
// "independent verification after repair", letting the original-finder
// continuity be satisfied by auto-generated prose. A reason with no evidence
// anchor is now rejected at the artifact boundary.
func TestTargetedReverificationRejectsUnanchoredContinuityReason(t *testing.T) {
	root := t.TempDir()
	contractRef, _ := writeRuntimeContract(t, root)
	_, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-continuity", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-continuity", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	base := repair.TargetedReverificationRequest{
		ReverificationID: "reverify-continuity", RuntimeID: "loop-REQ-039", BugID: "BUG-100", BaselineGeneration: 1,
		OriginalAssignmentID: "assignment-builder", PerformingAssignmentID: "assignment-qa",
		ImpactID: "impact-continuity", AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://symptom"}}},
		ScopeCompliance: "pass", Result: "pass",
	}
	for _, reason := range []string{"", "independent verification after repair", "a thorough manual recheck with no anchor"} {
		request := base
		request.ContinuityReason = reason
		if _, _, err := repair.CreateTargetedReverification(root, request); err == nil || !strings.Contains(err.Error(), "continuity_reason must cite at least one evidence reference") {
			t.Fatalf("continuity_reason %q must be rejected for lacking an evidence anchor, got %v", reason, err)
		}
	}
	// Anchored reasons are accepted.
	request := base
	request.ContinuityReason = "test://verifier-run: independent verification after repair"
	request.ReverificationID = "reverify-continuity-anchored"
	if _, _, err := repair.CreateTargetedReverification(root, request); err != nil {
		t.Fatalf("anchored continuity_reason must be accepted: %v", err)
	}
	request.ReverificationID = "reverify-continuity-evidence-path"
	request.ContinuityReason = "evidence/reverify-red.json shows the original failure"
	if _, _, err := repair.CreateTargetedReverification(root, request); err != nil {
		t.Fatalf("evidence-path continuity_reason must be accepted: %v", err)
	}
}
