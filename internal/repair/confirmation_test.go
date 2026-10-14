package repair_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestConfirmationFullChainPreservesTruthAndRequiresCurrentVerification(t *testing.T) {
	root, statePath, journalPath, priorRef := overBudgetHandoffFixture(t)
	prior, err := repair.ValidateRepairHandoff(root, priorRef)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repair.CommitRepairHandoff(root, statePath, journalPath, repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 7, Actor: "main"}, Handoff: priorRef})
	if err != nil {
		t.Fatal(err)
	}
	// Fixture re-entry models a new approved S8 case, retaining the prior
	// committed handoff evidence. Production obtains this state through S8.
	state := first.State
	review := state["review"].(map[string]any)
	review["repair"] = nil
	review["plan"] = nil
	review["claims"] = map[string]any{}
	review["assignments"] = map[string]any{}
	review["round_entry"] = nil
	state["lifecycle"].(map[string]any)["state"] = "bug_resolution"
	state["lifecycle"].(map[string]any)["phase"] = "repair_readback"
	contract := repair.ContractRef{Path: prior.ContractRef.Path, SHA256: prior.ContractRef.SHA256}
	review["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": strings.Repeat("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contract.Path, "repair_contract_sha256": contract.SHA256}
	payload, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	unchanged := func(call func() error, want string) {
		t.Helper()
		before, _ := os.ReadFile(statePath)
		journal, _ := os.ReadFile(journalPath)
		if err := call(); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want rejection %q got %v", want, err)
		}
		after, _ := os.ReadFile(statePath)
		later, _ := os.ReadFile(journalPath)
		if !bytes.Equal(before, after) || !bytes.Equal(journal, later) {
			t.Fatal("rejection mutated Runtime")
		}
	}
	request := repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 8, Actor: "main", OperationID: "confirm-open"}, SessionID: "repair-session-confirm", CreatedBy: "main", Intent: "confirm", ConfirmationSources: []repair.ArtifactRef{priorRef}}
	missing := request
	missing.ConfirmationSources = nil
	unchanged(func() error { _, _, _, e := repair.OpenRepairSession(root, statePath, journalPath, missing); return e }, "confirmation_sources")
	uncommitted := request
	uncommitted.ConfirmationSources = []repair.ArtifactRef{{Path: priorRef.Path, SHA256: strings.Repeat("f", 64)}}
	unchanged(func() error {
		_, _, _, e := repair.OpenRepairSession(root, statePath, journalPath, uncommitted)
		return e
	}, "committed")
	sourcesPath := filepath.Join(root, ".claude/evidence/confirmation-sources.json")
	os.MkdirAll(filepath.Dir(sourcesPath), 0700)
	sourceBytes, _ := json.Marshal(request.ConfirmationSources)
	os.WriteFile(sourcesPath, sourceBytes, 0600)
	var openOut, openErr bytes.Buffer
	code := cli.Run([]string{"runtime", "repair", "session", "open", "--root", root, "--session-id", request.SessionID, "--created-by", "main", "--actor", "main", "--intent", "confirm", "--confirmation-sources", sourcesPath, "--operation-id", "confirm-open", "--expected-revision", "8"}, bytes.NewReader(nil), &openOut, &openErr)
	if code != 0 {
		t.Fatalf("public confirmation CLI: %s", openErr.String())
	}
	var opened struct {
		Session repair.RepairSession `json:"session"`
		Ref     repair.ArtifactRef   `json:"artifact_ref"`
	}
	if err := json.Unmarshal(openOut.Bytes(), &opened); err != nil {
		t.Fatal(err)
	}
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	session, sessionRef := opened.Session, opened.Ref
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 9 || session.Intent != "confirm" || len(session.VerifiedSubjects) != 1 {
		t.Fatalf("invalid confirmation Session %#v", session)
	}
	if _, _, same, err := repair.OpenRepairSession(root, statePath, journalPath, request); err != nil || same != sessionRef {
		t.Fatalf("open retry %v", err)
	}
	_, plan, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 9, Actor: "main"}, PlanID: "repair-plan-confirm", CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := repair.PlanReportRequest{Session: sessionRef, Plan: planRef, AssignmentID: plan.Assignments[0].AssignmentID, AgentID: "confirm-builder", ReportID: "repair-plan-report-confirm", PlanText: "confirm the prior repair without modifying code", RedChecks: []repair.RepairCheck{{Name: "current check", Command: "inspect current payload", Result: "pass", EvidenceRefs: []string{"test://current-check"}}}}
	report, reportRef, err := repair.CreatePlanReport(root, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.RedChecks) != 1 {
		t.Fatal("missing current checks")
	}
	if _, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 10, Actor: "confirm-builder"}, Report: reportRef}); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 11, Actor: "main"}}); err != nil {
		t.Fatal(err)
	}
	resultRequest := repair.SubmitResultRuntimeRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 12, Actor: "confirm-builder", OperationID: "confirm-result"}, Result: repair.RepairResultRequest{ResultID: "repair-result-confirm", ProducerAgentID: "confirm-builder", UnitResults: []repair.RepairUnitResult{{UnitID: "unit-1", Status: "pass", EvidenceRefs: []string{"test://current-unit"}}}, VerifiedSubjects: session.VerifiedSubjects, Checks: []repair.RepairCheck{{Name: "fresh verification", Command: "inspect payload", Result: "pass", EvidenceRefs: []string{"test://fresh"}}}, Result: "pass"}}
	badResult := resultRequest
	badResult.Result.VerifiedSubjects = nil
	unchanged(func() error {
		_, _, _, e := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, badResult)
		return e
	}, "exactly")
	badResult = resultRequest
	badResult.Result.Checks = nil
	unchanged(func() error {
		_, _, _, e := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, badResult)
		return e
	}, "non-empty checks")
	badResult = resultRequest
	badResult.Result.ChangedArtifacts = []repair.ChangedArtifact{{Path: session.VerifiedSubjects[0].Path, SHA256: session.VerifiedSubjects[0].SHA256, Status: "added"}}
	unchanged(func() error {
		_, _, _, e := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, badResult)
		return e
	}, "predecessor changes")
	product := filepath.Join(root, session.VerifiedSubjects[0].Path)
	original, _ := os.ReadFile(product)
	if err := os.WriteFile(product, []byte("changed after confirmation"), 0600); err != nil {
		t.Fatal(err)
	}
	unchanged(func() error {
		_, _, _, e := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, resultRequest)
		return e
	}, "bytes changed")
	if err := os.WriteFile(product, original, 0600); err != nil {
		t.Fatal(err)
	}
	_, result, resultRef, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, resultRequest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != "confirm" || len(result.ChangedArtifacts) != 0 || len(result.VerifiedSubjects) != 1 {
		t.Fatal("confirmation result misrepresented changes")
	}
	if _, _, same, err := repair.SubmitRepairResultToRuntime(root, statePath, journalPath, resultRequest); err != nil || same != resultRef {
		t.Fatalf("result retry %v", err)
	}
	changeset, err := repair.ComputeSessionChangesetRecord(root, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(changeset.Artifacts) != 0 || len(changeset.VerifiedSubjects) != 1 {
		t.Fatal("changeset fabricated a change")
	}
	changesetRef, err := repair.PersistChangeset(root, changeset)
	if err != nil {
		t.Fatal(err)
	}
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{Session: &sessionRef, VerifiedSubjects: session.VerifiedSubjects, ImpactID: "impact-confirm", RuntimeID: session.RuntimeID, ReqID: session.ReqID, BaselineGeneration: session.BaselineGeneration, SourceBugIDs: []string{"BUG-001"}, ChangeTypes: []string{"implementation"}, Decisions: []repair.ImpactDecision{{SourceID: "BUG-001", TargetID: "confirm-claim", Relation: "requires verification", RuleID: "IM-API", Decision: "reverify", Scope: []string{session.VerifiedSubjects[0].Path}, Rationale: "confirm inherited repair against current Contract", RecoveryEvidence: []string{resultRef.Path}}}, AnalyzedBy: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.ChangedArtifacts) != 0 {
		t.Fatal("impact fabricated predecessor changes")
	}
	if _, err := repair.CommitChangeImpact(root, statePath, journalPath, repair.CommitImpactRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 13, Actor: "main"}, Impact: impactRef}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(statePath)
	json.Unmarshal(raw, &state)
	pointer := state["review"].(map[string]any)["repair"].(map[string]any)
	owners := pointer["assignment_owners"].(map[string]any)
	owners["assignment-s9-confirm-verifier"] = "qa"
	payload, _ = json.Marshal(state)
	os.WriteFile(statePath, payload, 0600)
	_, targetRef, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{StopConditionAssessments: stopAssessments(t, root, contract), ReverificationID: "reverify-confirm", RuntimeID: session.RuntimeID, BugID: "BUG-001", BaselineGeneration: 1, OriginalAssignmentID: "assignment-s9-unit-1", PerformingAssignmentID: "assignment-s9-confirm-verifier", ContinuityReason: "test://confirmation-independent", ImpactID: impact.ImpactID, AssertionResults: []repair.AssertionResult{{AssertionID: "symptom-1", Result: "pass", EvidenceRefs: []string{"test://fresh-symptom"}}, {AssertionID: "root-1", Result: "pass", EvidenceRefs: []string{"test://fresh-root"}}, {AssertionID: "gap-1", Result: "pass", EvidenceRefs: []string{"test://fresh-gap"}}}, ScopeCompliance: "pass", Result: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitTargetedReverification(root, statePath, journalPath, repair.CommitTargetedRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 14, Actor: "qa"}, Reverification: targetRef}); err != nil {
		t.Fatal(err)
	}
	_, handoffRef, err := repair.CreateRepairHandoff(root, repair.HandoffRequest{HandoffID: "repair-handoff-confirm", Session: sessionRef, Plan: planRef, Contract: contract, Result: resultRef, Changeset: changesetRef, ChangeImpact: impactRef, TargetedReverifications: []repair.ArtifactRef{targetRef}, HandedOffBy: "main", NextAction: "fresh full S7"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := repair.CommitRepairHandoff(root, statePath, journalPath, repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 15, Actor: "main", OperationID: "confirm-handoff"}, Handoff: handoffRef})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Revision != 16 || completed.State["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatal("confirmation bypassed fresh S7")
	}
	pointer = completed.State["review"].(map[string]any)["repair"].(map[string]any)
	seedBytes, err := os.ReadFile(filepath.Join(root, pointer["review_plan_seed_ref"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(seedBytes, []byte(session.VerifiedSubjects[0].Path)) {
		t.Fatal("confirmation surface disappeared from S7 seed")
	}
	// Exercise concrete CLI help and the installed-format artifact reader.
	var out, errout bytes.Buffer
	if cli.Run([]string{"runtime", "repair", "session", "open", "--help"}, bytes.NewReader(nil), &out, &errout) != 0 || !strings.Contains(out.String()+errout.String(), "confirmation-sources") {
		t.Fatal("missing CLI contract")
	}
	for _, ref := range []repair.ArtifactRef{sessionRef, resultRef, changesetRef, impactRef} {
		data, _ := os.ReadFile(filepath.Join(root, ref.Path))
		if err := schema.NewEmbeddedValidator().ValidateBytes(map[string]string{sessionRef.Path: "repair-session.schema.json", resultRef.Path: "repair-result.schema.json", changesetRef.Path: "repair-changeset.schema.json", impactRef.Path: "review-evidence.schema.json"}[ref.Path], data); err != nil {
			t.Fatal(err)
		}
	}
	final, _ := runtime.NewStore(statePath, journalPath).Snapshot()
	if final.Revision != 16 {
		t.Fatal("read changed Runtime")
	}
}
