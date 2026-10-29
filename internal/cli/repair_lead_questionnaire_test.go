package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestS9ImpactPrepareRejectsWrongResponsibilityWithoutMutation(t *testing.T) {
	root := repairLeadFreshRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &out, &diagnostic); code != 1 || !strings.Contains(diagnostic.String(), "requires the current S9 impact_reconciliation") {
		t.Fatalf("wrong-stage preparation was not refused: code=%d out=%s diagnostic=%s", code, &out, &diagnostic)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("wrong-stage preparation mutated Runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "submissions", "s9")); !os.IsNotExist(err) {
		t.Fatalf("wrong-stage preparation created S9 draft tree: %v", err)
	}
}

func TestS9RepairLeadStopAndIdleDoNotGuessPathWithoutAuthority(t *testing.T) {
	root := repairLeadFreshRoot(t)
	state := repairLeadBaseState(t, root, "bug_resolution", "fixing", 0)
	mapFieldCLI(state, "review")["repair"] = map[string]any{"status": "impact_reconciliation"}
	snapshot := runtime.Snapshot{State: state}
	for _, event := range []string{"Stop", "Idle"} {
		context := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: event, AgentID: "Main"}, snapshot)
		if !strings.Contains(context, "current approved Case, RepairSession, and RepairPlan source are required") {
			t.Fatalf("%s guessed a ChangeImpact path without current source authority: %s", event, context)
		}
		if paths := repairLeadQuestionnairePaths(root, state); len(paths) != 0 {
			t.Fatalf("%s path selector invented a questionnaire path without current authority: %#v", event, paths)
		}
	}

	// S9 Handoff has no Main-authored professional fields. A ready Stop/Idle
	// reports its read-only behavior without creating a handoff or new form.
	ready := repairLeadBaseState(t, root, "bug_resolution", "ready_for_full_review", 0)
	mapFieldCLI(ready, "review")["repair"] = map[string]any{"status": "ready_for_full_review", "required_reverification_ids": []any{}}
	repairLeadWriteState(t, root, ready)
	beforeReady, _ := json.Marshal(ready)
	beforeRuntime := repairDispatchRuntimeBytes(t, root)
	for _, event := range []string{"Stop", "Idle"} {
		context := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: event, AgentID: "Main"}, runtime.Snapshot{State: ready})
		if !strings.Contains(context, repairExitRecovery(root)) || !strings.Contains(context, "Preserve accepted Targeted observations") || !strings.Contains(context, "only completes the original Handoff and S7 preparation") {
			t.Fatalf("%s did not report read-only automatic handoff: %s", event, context)
		}
	}
	afterReady, _ := json.Marshal(ready)
	if !bytes.Equal(beforeReady, afterReady) {
		t.Fatal("ready reminder changed its input facts")
	}
	repairDispatchAssertUnchanged(t, root, beforeRuntime)
	if got := repairLeadQuestionnairePaths(root, ready); len(got) != 0 {
		t.Fatalf("ready handoff has no professional answer path, got %#v", got)
	}
}

func TestS9RepairLeadPathSelectorUsesCurrentPhaseWithoutPreparing(t *testing.T) {
	root := repairLeadFreshRoot(t)
	state := repairLeadBaseState(t, root, "bug_resolution", "fixing", 0)
	mapFieldCLI(state, "review")["repair"] = map[string]any{"status": "impact_reconciliation"}
	if paths := repairLeadQuestionnairePaths(root, state); len(paths) != 0 {
		t.Fatalf("path selector guessed without current Case/Session/Plan source: %#v", paths)
	}
	ready := repairLeadBaseState(t, root, "bug_resolution", "ready_for_full_review", 0)
	mapFieldCLI(ready, "review")["repair"] = map[string]any{"status": "ready_for_full_review", "required_reverification_ids": []any{}}
	if got := repairLeadQuestionnairePaths(root, ready); len(got) != 0 {
		t.Fatalf("ready handoff should not manufacture an answer path: %#v", got)
	}

	state = repairLeadBaseState(t, root, "bug_resolution", "targeted_reverification", 0)
	if got := repairLeadQuestionnairePaths(root, state); len(got) != 0 {
		t.Fatalf("targeted path selector guessed without a current plan/impact/dispatch: %#v", got)
	}
}

func TestS9RepairLeadTargetedPathUsesRegisteredIndependentAssignment(t *testing.T) {
	root := repairLeadFreshRoot(t)
	contractRef := writeRepairLeadTestContract(t, root, "investigation-case-lead-path")
	session, sessionRef, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-lead-path", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, planRef, err := repair.CreateRepairPlan(root, repair.PlanRequest{Contract: contractRef, Session: sessionRef, PlanID: "repair-plan-lead-path", CreatedBy: "Main"})
	if err != nil {
		t.Fatal(err)
	}
	changedHash := strings.Repeat("a", 64)
	impact, impactRef, err := repair.CreateChangeImpact(root, repair.ChangeImpactRequest{
		ImpactID: "impact-lead-path", RuntimeID: "loop-req039-ct", ReqID: "REQ-039", BaselineGeneration: 1,
		SourceCaseIDs: []string{"investigation-case-lead-path"}, ChangeTypes: []string{"implementation"},
		ChangedArtifacts:          []repair.ArtifactRef{{ID: "changed-api", Path: "internal/api/payload.go", SHA256: changedHash}},
		Decisions:                 []repair.ImpactDecision{{SourceID: "finding-1", TargetID: "claim-1", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify", Scope: []string{"internal/api/payload.go"}, Rationale: "the changed authority must be checked", RecoveryEvidence: []string{"test://lead-path"}}},
		RequiredReverificationIDs: []string{"reverify-lead-path"}, AnalyzedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if impact.ImpactID == "" {
		t.Fatal("CreateChangeImpact returned no current Impact")
	}
	assignmentID := plan.Assignments[0].AssignmentID
	verifierID := repair.VerificationAssignmentID(assignmentID)
	state := repairLeadBaseState(t, root, "bug_resolution", "targeted_reverification", 0)
	state["runtime_id"] = "loop-req039-ct"
	review := mapFieldCLI(state, "review")
	review["repair"] = map[string]any{
		"status":  "targeted_reverification",
		"case_id": "investigation-case-lead-path", "contract_id": "repair-contract-investigation-case-lead-path",
		"contract_ref": contractRef.Path, "contract_sha256": contractRef.SHA256, "session_id": session.SessionID,
		"path": sessionRef.Path, "sha256": sessionRef.SHA256,
		"plan_ref": planRef.Path, "plan_sha256": planRef.SHA256,
		"impact_ref": impactRef.Path, "impact_sha256": impactRef.SHA256,
		"required_reverification_ids": []any{"reverify-lead-path"},
		"assignment_owners":           map[string]any{assignmentID: "builder-lead-path", verifierID: "verifier-lead-path"},
	}
	want, err := repairTargetedQuestionnairePath(root, state, verifierID)
	if err != nil {
		t.Fatal(err)
	}
	got := repairLeadQuestionnairePaths(root, state)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("targeted paths = %#v, want registered verifier path %s", got, want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("read-only path selector created verifier questionnaire: %v", err)
	}
	selections, err := repairTargetedSelections(root, state)
	if err != nil || len(selections) != 1 {
		t.Fatalf("legacy single obligation source did not resolve one selection: selections=%#v err=%v", selections, err)
	}
	_, legacyDraft, err := prepareRepairTargetedQuestionnaire(root, state, selections[0])
	if err != nil || legacyDraft.ReverificationID == "" || legacyDraft.OriginalAssignmentID != assignmentID || legacyDraft.PerformingAssignmentID != verifierID || legacyDraft.ImpactID != impact.ImpactID {
		t.Fatalf("original Targeted DTO lost its current source responsibility: %+v err=%v", legacyDraft, err)
	}
	if legacyDraft.Result != "" {
		t.Fatal("independent verdict was prefilled")
	}
	start := policy.Input{Event: "SubagentStart", AgentID: "opaque-verifier", LastAssistantMessage: want}
	context := nativeS9TargetedStartContext(root, state, start)
	if !strings.Contains(context, "the approved scope expands") || !strings.Contains(context, "scope_changed") {
		t.Fatalf("native Targeted start lost original stop conditions: %s", context)
	}
	start.LastAssistantMessage = "unrelated native work"
	if got := nativeS9TargetedStartContext(root, state, start); got != "" {
		t.Fatalf("unrelated actor acquired guessed responsibility: %s", got)
	}
	repeatedPath, repeated, err := prepareRepairTargetedQuestionnaire(root, state, selections[0])
	if err != nil || repeatedPath != want || repeated.ReverificationID != legacyDraft.ReverificationID {
		t.Fatalf("same actual Targeted responsibility changed identity on retry: %+v %v", repeated, err)
	}
}

func TestS9RepairLeadImpactTargetedAndHandoffUseOriginalConsumers(t *testing.T) {
	s9RepairLeadImpactTargetedAndHandoff(t, false)
}
func TestNativeS9OriginalTargetedConsumePreservesPartialHandoff(t *testing.T) {
	s9RepairLeadImpactTargetedAndHandoff(t, true)
}
func s9RepairLeadImpactTargetedAndHandoff(t *testing.T, mainRecovery bool) {
	root := ""
	if mainRecovery {
		root = repairExitInstalledRoot(t)
	} else {
		root = repairLeadFreshRoot(t)
	}
	// Install dispatch inputs before the Session captures their authority.
	writeRepairLeadFile(t, root, "docs/control/agent-protocol.md", "# Agent protocol\n")
	writeRepairLeadFile(t, root, "agents/test-builder.md", "# Test Builder\n")
	caseID := "investigation-case-lead-chain"
	repairLeadApprovedCase(t, root, caseID, []map[string]any{
		{"id": "unit-lead-questionnaire-one", "description": "restore the first source authority", "scope": []string{"internal/api/payload.go"}, "assertion_ids": []string{"all"}},
		{"id": "unit-lead-questionnaire-two", "description": "restore the second source authority", "scope": []string{"internal/api/other.go"}, "assertion_ids": []string{"all"}},
	})
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	current, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	opened, _, sessionRef, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: current.Revision, Actor: "Main"},
		SessionID:      "repair-session-lead-chain", CreatedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	planSnapshot, plan, planRef, err := repair.CompileRepairPlan(root, statePath, journalPath, repair.CompilePlanRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: opened.Revision, Actor: "Main"},
		PlanID:         "repair-plan-lead-chain", CreatedBy: "Main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Assignments) != 2 {
		t.Fatalf("multi-assignment fixture compiled %d assignments, want 2", len(plan.Assignments))
	}
	assignments := plan.Assignments
	changedPaths := []string{"internal/api/payload.go", "internal/api/other.go"}
	builders := []string{"builder-lead-chain-1", "builder-lead-chain-2"}
	expectedRevision := planSnapshot.Revision
	for index, assignment := range assignments {
		_, reportRef, err := repair.CreatePlanReport(root, repair.PlanReportRequest{
			Session: sessionRef, Plan: planRef, AssignmentID: assignment.AssignmentID,
			AgentID: builders[index], ReportID: fmt.Sprintf("repair-plan-report-lead-chain-%d", index+1),
			PlanText:      "restore the source authority",
			RedChecks:     []repair.RepairCheck{{Name: "reported failure", Command: "go test ./internal/api", Result: "fail", EvidenceRefs: []string{fmt.Sprintf("test://red-%d", index+1)}}},
			ProposedPaths: []string{changedPaths[index]},
		})
		if err != nil {
			t.Fatal(err)
		}
		if saved, _, err := repair.SubmitRepairPlanReportToRuntime(root, statePath, journalPath, repair.SubmitPlanReportRequest{
			RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expectedRevision, Actor: builders[index]}, Report: reportRef,
		}); err != nil {
			t.Fatal(err)
		} else {
			expectedRevision = saved.Revision
		}
	}
	if started, err := repair.BeginRepairExecution(root, statePath, journalPath, repair.BeginRepairExecutionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expectedRevision, Actor: "Main"}}); err != nil {
		t.Fatal(err)
	} else {
		expectedRevision = started.Revision
	}
	changedBytes := [][]byte{
		[]byte("package api\n\nfunc FixedPayload() {}\n"),
		[]byte("package api\n\nfunc FixedOther() {}\n"),
	}
	for index, path := range changedPaths {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, changedBytes[index], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var resultSnapshot runtime.Snapshot
	var resultRef repair.ArtifactRef
	for index, assignment := range assignments {
		resultSnapshot, _, resultRef, err = repair.SubmitRepairResultToRuntime(root, statePath, journalPath, repair.SubmitResultRuntimeRequest{
			RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: expectedRevision, Actor: builders[index]},
			Result: repair.RepairResultRequest{
				ResultID: fmt.Sprintf("repair-result-lead-chain-%d", index+1), AssignmentID: assignment.AssignmentID, ProducerAgentID: builders[index],
				UnitResults:      []repair.RepairUnitResult{{UnitID: assignment.UnitIDs[0], Status: "pass", EvidenceRefs: []string{fmt.Sprintf("test://repair-unit-%d", index+1)}}},
				ChangedArtifacts: []repair.ChangedArtifact{{Path: changedPaths[index], SHA256: repairLeadDigest(changedBytes[index]), Status: "added"}},
				Checks:           []repair.RepairCheck{{Name: "post-fix", Command: "go test ./...", Result: "pass", EvidenceRefs: []string{fmt.Sprintf("test://green-%d", index+1)}}}, Result: "pass",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		expectedRevision++
	}
	repairPointer := repairPointerCLI(resultSnapshot.State)
	if got := stringValue(repairPointer["status"]); got != "impact_reconciliation" {
		t.Fatalf("RepairResult pointer status = %q, want impact_reconciliation", got)
	}
	if got := stringValue(mapFieldCLI(resultSnapshot.State, "lifecycle")["phase"]); got != "fixing" {
		t.Fatalf("RepairResult changed lifecycle phase to %q; routing must use the repair pointer status", got)
	}

	// A transient Python cache is not part of either accepted synthetic Final.
	// Keep it present through Impact/Handoff to exercise the recorded source.
	cachePath := filepath.Join(root, "src", "__pycache__", "transient.cpython-312.pyc")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("synthetic test transient Python cache\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "src/__pycache__/transient.cpython-312.pyc").Run(); err == nil {
		t.Fatal("test transient cache must not be tracked")
	}
	finalArtifacts := map[string]string{}
	for _, ref := range repairArtifactRefs(repairPointer, "result_refs", "result_ref", "result_sha256") {
		result, err := repair.ValidateRepairResult(root, ref)
		if err != nil {
			t.Fatal(err)
		}
		for _, artifact := range result.ChangedArtifacts {
			finalArtifacts[artifact.Path] = artifact.SHA256
		}
	}
	if len(finalArtifacts) != len(changedPaths) {
		t.Fatalf("accepted original Final batch changed artifacts: %+v", finalArtifacts)
	}
	impactPath, err := repairLeadQuestionnairePath(root, resultSnapshot.State, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}
	// Final is already committed, but questionnaire preparation has not run.
	// Passive listeners report the original recovery without manufacturing a draft.
	for _, event := range []string{"Stop", "Idle", "PreToolUse"} {
		context := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: event, AgentID: "Main"}, resultSnapshot)
		if !strings.Contains(context, impactPath) || !strings.Contains(context, "runtime repair impact prepare") {
			t.Fatalf("%s omitted exact missing questionnaire/recovery: %s", event, context)
		}
		if _, err := os.Stat(impactPath); !os.IsNotExist(err) {
			t.Fatalf("%s created questionnaire: %v", event, err)
		}
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	// A filesystem failure after accepted Final is recoverable independently.
	if err := os.MkdirAll(impactPath, 0o755); err != nil {
		t.Fatal(err)
	}
	var prepareOut, prepareErr bytes.Buffer
	if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &prepareOut, &prepareErr); code != 1 || !strings.Contains(prepareErr.String(), "not a regular file") {
		t.Fatalf("expected explicit preparation failure: code=%d out=%s err=%s", code, &prepareOut, &prepareErr)
	}
	if err := os.Remove(impactPath); err != nil {
		t.Fatal(err)
	}
	prepareOut.Reset()
	prepareErr.Reset()
	if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &prepareOut, &prepareErr); code != 0 {
		t.Fatalf("original preparation recovery failed: code=%d out=%s err=%s", code, &prepareOut, &prepareErr)
	}
	var preparedImpact map[string]any
	if err := json.Unmarshal(prepareOut.Bytes(), &preparedImpact); err != nil || preparedImpact["questionnaire_path"] != impactPath {
		t.Fatalf("preparation returned wrong literal questionnaire path: %v %s", err, &prepareOut)
	}
	// Re-entry must preserve ongoing professional work exactly.
	originalDraft, err := os.ReadFile(impactPath)
	if err != nil {
		t.Fatal(err)
	}
	var authoredImpact repair.ChangeImpactRequest
	if err := json.Unmarshal(originalDraft, &authoredImpact); err != nil {
		t.Fatal(err)
	}
	authoredImpact.ChangeTypes = []string{"implementation"}
	professionalDraft, err := json.MarshalIndent(authoredImpact, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	professionalDraft = append(professionalDraft, '\n')
	if err := os.WriteFile(impactPath, professionalDraft, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runRuntimeRepair([]string{"impact", "prepare", "--root", root}, &prepareOut, &prepareErr); code != 0 {
		t.Fatalf("repeated preparation failed: %s", &prepareErr)
	}
	draftAfter, err := os.ReadFile(impactPath)
	if err != nil || !bytes.Equal(professionalDraft, draftAfter) {
		t.Fatalf("recovery rewrote professional draft: %v", err)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(journalBefore, journalAfter) {
		t.Fatalf("preparation replayed accepted Final or mutated Runtime: %v", err)
	}
	var impactAnswer repair.ChangeImpactRequest
	readRepairLeadJSON(t, impactPath, &impactAnswer)
	if len(impactAnswer.ChangedArtifacts) != len(finalArtifacts) {
		t.Fatalf("Impact must use only accepted Final artifacts, not transient cache: %+v", impactAnswer.ChangedArtifacts)
	}
	for _, artifact := range impactAnswer.ChangedArtifacts {
		if expected, ok := finalArtifacts[artifact.Path]; !ok || artifact.SHA256 != expected {
			t.Fatalf("Impact artifact differs from accepted Final batch: %+v", artifact)
		}
	}
	wantReverificationIDs, err := repairImpactReverificationIDs(resultSnapshot.State)
	if err != nil || len(impactAnswer.RequiredReverificationIDs) != 1 || impactAnswer.RequiredReverificationIDs[0] != wantReverificationIDs[0] {
		t.Fatalf("Impact draft required IDs = %#v, want one current Contract obligation %#v (err=%v)", impactAnswer.RequiredReverificationIDs, wantReverificationIDs, err)
	}
	var feedback bytes.Buffer
	impactSave := repairLeadSaveInput(root, impactPath, "")
	if handled, _ := runSavedRepairLeadQuestionnaire(root, impactSave, &feedback, &bytes.Buffer{}); !handled || !strings.Contains(feedback.String(), "incomplete") {
		t.Fatalf("incomplete professional Impact answer was not rejected: handled=%v feedback=%s", handled, feedback.String())
	}
	unchanged, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || unchanged.Revision != resultSnapshot.Revision {
		t.Fatalf("incomplete Impact answer wrote Runtime: revision=%d want=%d err=%v", unchanged.Revision, resultSnapshot.Revision, err)
	}
	impactAnswer.ChangeTypes = []string{"implementation"}
	impactAnswer.Decisions = []repair.ImpactDecision{{
		SourceID: caseID, TargetID: "claim-lead-chain", Relation: "invalidates", RuleID: "IM-API", Decision: "reverify",
		Scope:            append([]string(nil), changedPaths...),
		Rationale:        "the changed API surfaces require independent assertion runs",
		RecoveryEvidence: []string{resultRef.Path},
	}}
	// Machine-only edits cannot redefine the required Contract obligation or
	// turn an author's chosen timestamp into an execution fact.
	impactAnswer.RequiredReverificationIDs = []string{"reverify-author-invented"}
	impactAnswer.AnalyzedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRepairLeadJSON(t, impactPath, impactAnswer)
	feedback.Reset()
	if handled, _ := runSavedRepairLeadQuestionnaire(root, impactSave, &feedback, &bytes.Buffer{}); !handled || !strings.Contains(feedback.String(), "committed through the original Runtime Writer") {
		t.Fatalf("Impact did not reach its original Runtime consumer: handled=%v feedback=%s", handled, feedback.String())
	}
	impactSnapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || stringValue(repairPointerCLI(impactSnapshot.State)["status"]) != "targeted_reverification" {
		t.Fatalf("original Impact consumer did not register Targeted obligation: status=%q err=%v", stringValue(repairPointerCLI(impactSnapshot.State)["status"]), err)
	}
	registeredRequired := stringSliceAny(repairPointerCLI(impactSnapshot.State)["required_reverification_ids"])
	if len(registeredRequired) != 1 || registeredRequired[0] != wantReverificationIDs[0] {
		t.Fatalf("Runtime registered required IDs = %#v, want one Contract obligation %#v", registeredRequired, wantReverificationIDs)
	}
	readRepairLeadJSON(t, impactPath, &impactAnswer)
	if impactAnswer.AnalyzedAt.Before(time.Now().Add(-time.Hour)) {
		t.Fatalf("Impact retained author-supplied machine time: %s", impactAnswer.AnalyzedAt)
	}
	impactRevision := impactSnapshot.Revision
	feedback.Reset()
	if handled, _ := runSavedRepairLeadQuestionnaire(root, impactSave, &feedback, &bytes.Buffer{}); !handled || !strings.Contains(feedback.String(), "Replay did not write Runtime") {
		t.Fatalf("Impact replay was not recognized: handled=%v feedback=%s", handled, feedback.String())
	}
	impactReplay, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || impactReplay.Revision != impactRevision {
		t.Fatalf("Impact replay changed Runtime revision: %d -> %d err=%v", impactRevision, impactReplay.Revision, err)
	}
	t.Run("ImpactReplayComparesOnlyProfessionalContent", func(t *testing.T) {
		assertRepairLeadAnswerReplay(t, root, impactPath, impactSave, stringValue(repairPointerCLI(impactReplay.State)["impact_ref"]), func(answer map[string]any) {
			answer["decisions"].([]any)[0].(map[string]any)["rationale"] = "different professional impact judgment"
		})
	})

	verifierOwners := []string{"verifier-lead-chain-1"}
	divisionPath, err := repairLeadQuestionnairePath(root, impactSnapshot.State, "independent-verifier-dispatch.json")
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, divisionPath, []repairVerifierDispatchChoice{{AssignmentID: assignments[0].AssignmentID, IndependentVerification: true, AgentID: verifierOwners[0]}})
	feedback.Reset()
	if handled, _ := runSavedRepairVerifierDispatch(root, repairLeadSaveInput(root, divisionPath, ""), &feedback, &bytes.Buffer{}); !handled || !strings.Contains(feedback.String(), "Independent verifier answer:") {
		t.Fatalf("actual verifier division save did not prepare Targeted: %s", feedback.String())
	}
	targetPaths := make([]string, len(verifierOwners))
	targetSaves := make([]policy.Input, len(verifierOwners))
	dispatchedSnapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for index, assignment := range assignments[:1] {
		path, err := repairTargetedQuestionnairePath(root, dispatchedSnapshot.State, repair.VerificationAssignmentID(assignment.AssignmentID))
		if err != nil {
			t.Fatal(err)
		}
		targetPaths[index] = path
	}
	t.Run("registered verifier save restores original missing target", func(t *testing.T) {
		assertS9RegisteredVerifierSaveRestoresTarget(t, root, divisionPath, targetPaths[0], dispatchedSnapshot)
	})
	if paths := repairLeadQuestionnairePaths(root, dispatchedSnapshot.State); len(paths) != 1 {
		t.Fatalf("fresh Contract-level path selector returned %#v, want one path for the actual verifier", paths)
	} else {
		for index, path := range paths {
			if path != targetPaths[index] {
				t.Fatalf("multi-verifier path[%d] = %s, want assignment path %s", index, path, targetPaths[index])
			}
		}
	}
	selections, err := repairTargetedSelections(root, dispatchedSnapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if mappingErr := repairTargetedRequestMappingError(selections, []string{"reverify-legacy-one", "reverify-legacy-two"}); mappingErr != nil {
		t.Fatalf("stored obligation IDs must not become report qualifications: %v", mappingErr)
	}
	var handoffContext string
	for index := range assignments[:1] {
		targetSnapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		targetContext := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: verifierOwners[index]}, targetSnapshot)
		targetPath := targetPaths[index]
		if !strings.Contains(targetContext, targetPath) {
			t.Fatalf("dispatched verifier %s context omitted its TargetedReverification path %s: %s", verifierOwners[index], targetPath, targetContext)
		}
		for otherIndex, otherPath := range targetPaths {
			if otherIndex != index && otherPath != "" && strings.Contains(targetContext, otherPath) {
				t.Fatalf("verifier %s received another assignment's TargetedReverification path %s: %s", verifierOwners[index], otherPath, targetContext)
			}
		}
		var targetAnswer repair.TargetedReverificationRequest
		readRepairLeadJSON(t, targetPath, &targetAnswer)
		var draftFields map[string]json.RawMessage
		readRepairLeadJSON(t, targetPath, &draftFields)
		if draftFields["continuity_reason"] != nil || draftFields["scope_compliance"] != nil {
			t.Fatal("new Targeted questionnaire still asks for duplicate legacy report fields")
		}
		if targetAnswer.ReverificationID == "" {
			t.Fatal("Targeted responsibility must supply its machine record identity")
		}
		if len(targetAnswer.AssertionResults) != 3 {
			t.Fatalf("Targeted DTO has %d assertions, want all 3 current Contract assertion slots", len(targetAnswer.AssertionResults))
		}
		targetSaves[index] = repairLeadSaveInput(root, targetPath, verifierOwners[index])
		if index == 0 {
			feedback.Reset()
			if handled, _ := runSavedRepairLeadQuestionnaire(root, targetSaves[index], &feedback, &bytes.Buffer{}); !handled || !strings.Contains(feedback.String(), "must record") {
				t.Fatalf("incomplete independent result was not rejected: handled=%v feedback=%s", handled, feedback.String())
			}
			unchanged, err = runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil || unchanged.Revision != targetSnapshot.Revision {
				t.Fatalf("incomplete Targeted answer wrote Runtime: revision=%d want=%d err=%v", unchanged.Revision, targetSnapshot.Revision, err)
			}
		}
		targetAnswer.Result = "blocked"
		targetAnswer.FailureClass = "blocked"
		targetAnswer.PerformedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := range targetAnswer.AssertionResults {
			targetAnswer.AssertionResults[i].Result = "blocked"
			targetAnswer.AssertionResults[i].EvidenceRefs = []string{"test://blocked-assertion"}
		}
		writeRepairLeadJSON(t, targetPath, targetAnswer)
		feedback.Reset()
		nativeReturn := policy.Input{Event: "SubagentStop", AgentID: "opaque-targeted-native-id", SessionID: "native-parent", CWD: root, LastAssistantMessage: targetPath}
		if handled, code := nativeS9Hook(root, nativeReturn, &bytes.Buffer{}, &feedback); !handled || code != 0 || !strings.Contains(feedback.String(), "committed through the original Runtime Writer") {
			t.Fatalf("blocked Targeted answer for %s did not reach original Runtime consumer: handled=%v feedback=%s", verifierOwners[index], handled, feedback.String())
		}
		blockedSnapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
		blockedPointer := repairPointerCLI(blockedSnapshot.State)
		if err != nil || stringValue(blockedPointer["status"]) != "blocked" || stringValue(blockedPointer["failure_route"]) != "blocked" {
			t.Fatalf("blocked Targeted consumer status=%q route=%q err=%v", stringValue(blockedPointer["status"]), stringValue(blockedPointer["failure_route"]), err)
		}
		if paths := repairLeadQuestionnairePaths(root, blockedSnapshot.State); len(paths) != 1 || paths[0] != targetPath {
			t.Fatalf("blocked Contract responsibility did not retain its original answer path %s: %#v", targetPath, paths)
		}
		blockedContext := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: verifierOwners[index]}, blockedSnapshot)
		if !strings.Contains(blockedContext, targetPath) || !strings.Contains(blockedContext, "environmental blocker") || !strings.Contains(blockedContext, "actual assertion observations") {
			t.Fatalf("blocked verifier context omitted the same questionnaire or resume instruction: %s", blockedContext)
		}
		var persistedBlocked repair.TargetedReverificationRequest
		readRepairLeadJSON(t, targetPath, &persistedBlocked)
		blockedPerformedAt := persistedBlocked.PerformedAt
		if blockedPerformedAt.Before(time.Now().Add(-time.Hour)) {
			t.Fatalf("blocked Targeted retained author-supplied machine time: %s", blockedPerformedAt)
		}
		blockedRefs := repairArtifactRefs(blockedPointer, "targeted_reverification_artifacts", "", "")
		if len(blockedRefs) != 1 {
			t.Fatalf("blocked Runtime references = %#v, want the original failed attempt", blockedRefs)
		}
		blockedMain, err := nativeReturnSummary(root, blockedSnapshot, nativeReturnSubject{kind: "Targeted", id: selections[0].performingAssignmentID, ref: targetPath, targeted: &selections[0]})
		if err != nil || strings.Contains(blockedMain, "runtime repair handoff complete") {
			t.Fatalf("blocked Targeted incorrectly offered Handoff recovery: %v %s", err, blockedMain)
		}
		blockedArtifactPath := filepath.Join(root, filepath.FromSlash(blockedRefs[0].Path))
		blockedBytes, err := os.ReadFile(blockedArtifactPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range []string{"Stop", "Idle"} {
			readOnly := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: event, AgentID: verifierOwners[index]}, blockedSnapshot)
			if !strings.Contains(readOnly, targetPath) {
				t.Fatalf("%s omitted the existing blocked questionnaire or read-only note: %s", event, readOnly)
			}
		}
		readOnlyAfter, err := runtime.NewStore(statePath, journalPath).Snapshot()
		if err != nil || readOnlyAfter.Revision != blockedSnapshot.Revision {
			t.Fatalf("blocked Stop/Idle context changed Runtime revision: %d -> %d err=%v", blockedSnapshot.Revision, readOnlyAfter.Revision, err)
		}
		feedback.Reset()
		if handled, _ := runSavedRepairLeadQuestionnaire(root, targetSaves[index], &feedback, &bytes.Buffer{}); !handled || !strings.Contains(feedback.String(), "already recorded") {
			t.Fatalf("replayed blocked questionnaire was not reported without resubmission: handled=%v feedback=%s", handled, feedback.String())
		}
		replayedBlocked, err := runtime.NewStore(statePath, journalPath).Snapshot()
		if err != nil || replayedBlocked.Revision != blockedSnapshot.Revision {
			t.Fatalf("blocked questionnaire replay changed Runtime revision: %d -> %d err=%v", blockedSnapshot.Revision, replayedBlocked.Revision, err)
		}
		if after, readErr := os.ReadFile(blockedArtifactPath); readErr != nil || !bytes.Equal(after, blockedBytes) {
			t.Fatalf("blocked replay changed the old failed artifact: %v", readErr)
		}

		if persistedBlocked.ContinuityReason != "" || persistedBlocked.ScopeCompliance != "" {
			t.Fatal("recorded environmental blocker fabricated optional legacy judgments")
		}
		persistedBlocked.Result = "pass"
		persistedBlocked.FailureClass = ""
		persistedBlocked.PerformedAt = time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := range persistedBlocked.AssertionResults {
			persistedBlocked.AssertionResults[i].Result = "pass"
			persistedBlocked.AssertionResults[i].EvidenceRefs = []string{"test://assertion-replay-pass"}
		}
		writeRepairLeadJSON(t, targetPath, persistedBlocked)
		feedback.Reset()
		// Force a partial success: the independent PASS is accepted while the
		// automatic Handoff cannot create its original artifact directory.
		handoffDir := filepath.Join(root, ".claude/review/repair/handoffs")
		if err := os.WriteFile(handoffDir, []byte("temporary preparation blocker"), 0600); err != nil {
			t.Fatal(err)
		}
		nativeReturn.AgentID = "opaque-targeted-id-after-native-resume"
		if mainRecovery {
			var result, diagnostic bytes.Buffer
			code := Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", targetPath}, nil, &result, &diagnostic)
			if code != 1 || !strings.Contains(result.String(), "committed through the original Runtime Writer") || !strings.Contains(diagnostic.String(), "runtime repair handoff complete") {
				t.Fatalf("original Main consume lost accepted Targeted/failed successor: code=%d result=%s error=%s", code, &result, &diagnostic)
			}
			feedback.Write(result.Bytes())
			feedback.Write(diagnostic.Bytes())
		} else if handled, code := nativeS9Hook(root, nativeReturn, &bytes.Buffer{}, &feedback); !handled || code != 0 || !strings.Contains(feedback.String(), "committed through the original Runtime Writer") {
			t.Fatalf("resolved blocker did not resume/create/commit the pass result: handled=%v feedback=%s", handled, feedback.String())
		}
		partial, err := runtime.NewStore(statePath, journalPath).Snapshot()
		if err != nil || stringValue(repairPointerCLI(partial.State)["status"]) != "ready_for_full_review" || !strings.Contains(feedback.String(), "runtime repair handoff complete") {
			t.Fatalf("partial reception lost recovery: %v %s", err, &feedback)
		}
		if mainRecovery {
			original, readErr := os.ReadFile(targetPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			before := repairDispatchRuntimeBytes(t, root)
			if err := os.Remove(targetPath); err != nil {
				t.Fatal(err)
			}
			var result, diagnostic bytes.Buffer
			if code := Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", targetPath}, nil, &result, &diagnostic); code != 0 || !strings.Contains(result.String(), "Targeted is already received") || !strings.Contains(result.String(), "runtime repair handoff complete") {
				t.Fatalf("accepted Targeted Main replay requires author input: %d %s %s", code, &result, &diagnostic)
			}
			repairDispatchAssertUnchanged(t, root, before)
			if err := os.WriteFile(targetPath, original, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if mainRecovery {
			assertRepairExitReadyPublicRecovery(t, root)
			assertRepairExitAcceptedProjectionCases(t, root, targetPath, partial, []string{"partial"})
		}
		refsBefore := repairArtifactRefs(repairPointerCLI(partial.State), "targeted_reverification_artifacts", "", "")
		mainPartial, _ := nativeReturnSummary(root, partial, nativeReturnSubject{kind: "Targeted", id: selections[0].performingAssignmentID, ref: targetPath, targeted: &selections[0]})
		if !strings.Contains(mainPartial, "runtime repair handoff complete") || !strings.Contains(mainPartial, handoffDir) {
			t.Fatalf("Main return omitted partial completion recovery: %s", mainPartial)
		}
		stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, partial)
		unchanged, _ := runtime.NewStore(statePath, journalPath).Snapshot()
		if unchanged.Revision != partial.Revision {
			t.Fatal("ordinary projection retried Handoff")
		}
		// Exhaust the actual budget at the same completion boundary. Preparing
		// its application must not choose an increase or open another round.
		configuration := mapFieldCLI(partial.State, "configuration")
		if configuration == nil {
			configuration = map[string]any{}
			partial.State["configuration"] = configuration
		}
		repairBudget := mapFieldCLI(configuration, "repair")
		if repairBudget == nil {
			t.Fatal("fixture is missing the original repair budget configuration")
		}
		repairBudget["max_full_review_rounds"] = integerValue(nestedStateValue(partial.State, "review", "round"))
		writeRepairLeadJSON(t, statePath, partial.State)
		_, exitErr := completeRepairExit(root)
		if exitErr == nil {
			t.Fatal("blocked completion unexpectedly succeeded")
		}
		applicationPath, app, appErr := s7BudgetDecisionApplicationForState(root, partial.State)
		if appErr != nil {
			t.Fatal(appErr)
		}
		var prepared s7BudgetDecisionApplication
		readRepairLeadJSON(t, applicationPath, &prepared)
		if prepared.Decision != "" || prepared.NewMaxFullReviewRounds != nil {
			t.Fatal("completion invented budget approval")
		}
		if err := os.Remove(handoffDir); err != nil {
			t.Fatal(err)
		}
		// The unchanged real budget still blocks Handoff even after the source
		// preparation problem is repaired.
		if _, err := completeRepairExit(root); err == nil {
			t.Fatal("Handoff bypassed actual budget")
		}
		newMax := app.PreviousMaxFullReviewRounds + 1
		prepared.Decision, prepared.Reason, prepared.NewMaxFullReviewRounds = runtime.S7BudgetDecisionIncrease, "Human explicitly approved one additional round", &newMax
		writeRepairLeadJSON(t, applicationPath, prepared)
		feedback.Reset()
		if handled, code := runSavedS7BudgetDecisionQuestionnaire(root, repairLeadSaveInput(root, applicationPath, ""), &feedback, &bytes.Buffer{}); !handled || code != 0 {
			t.Fatalf("budget decision did not resume original completion: %s", &feedback)
		}
		budgetFeedback := feedback.String()
		feedback.Reset()

		var completionDiagnostic bytes.Buffer
		if code := runRuntimeRepairHandoffComplete([]string{"--root", root}, &feedback, &completionDiagnostic); code != 0 {
			t.Fatalf("explicit completion failed: %s %s; budget reception: %s", &feedback, &completionDiagnostic, budgetFeedback)
		}
		recovered, _ := runtime.NewStore(statePath, journalPath).Snapshot()
		if integerValue(nestedStateValue(recovered.State, "review", "round")) != integerValue(nestedStateValue(partial.State, "review", "round"))+1 || len(repairArtifactRefs(repairPointerCLI(recovered.State), "targeted_reverification_artifacts", "", "")) != len(refsBefore) {
			t.Fatal("recovery repeated Targeted or review rounds")
		}
		var completed map[string]any
		if err := json.Unmarshal(feedback.Bytes(), &completed); err != nil {
			t.Fatalf("completion JSON: %v %s", err, &feedback)
		}
		handoffContext = stringValue(completed["next_action"])
		var persistedPass repair.TargetedReverificationRequest
		readRepairLeadJSON(t, targetPath, &persistedPass)
		if persistedPass.PerformedAt.Before(blockedPerformedAt) || persistedPass.PerformedAt.Equal(blockedPerformedAt) || persistedPass.PerformedAt.After(time.Now().Add(time.Hour)) {
			t.Fatalf("resumed Targeted save did not persist a fresh PerformedAt: blocked=%s resumed=%s", blockedPerformedAt, persistedPass.PerformedAt)
		}
		afterTarget, err := runtime.NewStore(statePath, journalPath).Snapshot()
		if err != nil || stringValue(repairPointerCLI(afterTarget.State)["status"]) != "closed" {
			t.Fatalf("resumed Targeted consumer did not reach handoff status: status=%q err=%v", stringValue(repairPointerCLI(afterTarget.State)["status"]), err)
		}
		var mainOut bytes.Buffer
		mainInput := policy.Input{Event: "PostToolUse", ToolName: "Agent", CWD: root, ToolInput: map[string]any{"prompt": targetPath}}
		if handled, code := nativeMainReturnHook(root, mainInput, &mainOut); !handled || code != 0 || !strings.Contains(mainOut.String(), "Original Targeted records") || !strings.Contains(mainOut.String(), "result=pass") {
			t.Fatalf("Main missed closed Targeted reception: %t %d %s", handled, code, &mainOut)
		}
		resumedRefs := repairArtifactRefs(repairPointerCLI(afterTarget.State), "targeted_reverification_artifacts", "", "")
		if len(resumedRefs) != 2 {
			t.Fatalf("resume replaced or omitted the original blocked attempt: refs=%#v", resumedRefs)
		}
		foundResume := false
		for _, resumedRef := range resumedRefs {
			value, validateErr := repair.ValidateTargetedReverification(root, resumedRef)
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			if resumedRef.Path == blockedRefs[0].Path {
				if value.Result != "blocked" || value.FailureClass != "blocked" {
					t.Fatalf("original failed artifact was rewritten: %#v", value)
				}
				continue
			}
			if value.Result == "pass" && value.ReverificationID == persistedPass.ReverificationID && strings.Contains(resumedRef.Path, "-resume-") {
				foundResume = true
			}
		}
		if !foundResume {
			t.Fatalf("Runtime did not retain a new same-ID resumed PASS artifact: %#v", resumedRefs)
		}
		if after, readErr := os.ReadFile(blockedArtifactPath); readErr != nil || !bytes.Equal(after, blockedBytes) {
			t.Fatalf("resumed attempt changed the old failed artifact: %v", readErr)
		}
		remaining := repairLeadQuestionnairePaths(root, afterTarget.State)
		if len(remaining) != 0 {
			t.Fatalf("completed Contract responsibility remained outstanding: paths=%#v", remaining)
		}
		completedContext := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: verifierOwners[0]}, afterTarget)
		if strings.Contains(completedContext, targetPaths[0]) {
			t.Fatalf("completed verifier still received its answer path: %s", completedContext)
		}
		feedback.Reset()
		if handled, _ := runSavedRepairLeadQuestionnaire(root, targetSaves[index], &feedback, &bytes.Buffer{}); !handled || !strings.Contains(feedback.String(), "Replay did not write Runtime") {
			t.Fatalf("Targeted replay for %s was not recognized: handled=%v feedback=%s", verifierOwners[index], handled, feedback.String())
		}
		replayedTarget, err := runtime.NewStore(statePath, journalPath).Snapshot()
		if err != nil || replayedTarget.Revision != afterTarget.Revision {
			t.Fatalf("Targeted replay for %s changed Runtime revision: %d -> %d err=%v", verifierOwners[index], afterTarget.Revision, replayedTarget.Revision, err)
		}
		t.Run("TargetedReplayComparesOnlyProfessionalContent", func(t *testing.T) {
			ref, ok := registeredTargetedForSelection(root, repairPointerCLI(replayedTarget.State), selections[0])
			if !ok {
				t.Fatal("original accepted PASS not found")
			}
			assertRepairLeadAnswerReplay(t, root, targetPath, targetSaves[index], ref.Path, func(answer map[string]any) {
				results := answer["assertion_results"].([]any)
				results[0].(map[string]any)["evidence_refs"] = []any{"logs/different-actual-observation.log"}
			})
		})
	}
	readySnapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || stringValue(repairPointerCLI(readySnapshot.State)["status"]) != "closed" {
		t.Fatalf("all independent verifiers did not reach handoff status: status=%q err=%v", stringValue(repairPointerCLI(readySnapshot.State)["status"]), err)
	}
	for _, event := range []string{"Stop", "Idle", "PreToolUse"} {
		stageRepairLeadQuestionnaireContext(root, policy.Input{Event: event, AgentID: "Main"}, readySnapshot)
	}
	handoffSnapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	handoffPointer := repairPointerCLI(handoffSnapshot.State)
	handoffRef, err := repairPointerRef(handoffPointer, "handoff_ref", "handoff_sha256", "committed RepairHandoff")
	if err != nil {
		t.Fatalf("actual Targeted save did not automatically commit Handoff: %v; context=%s", err, handoffContext)
	}
	handoffPath, err := absoluteQuestionnairePath(root, handoffRef.Path)
	if err != nil || !strings.Contains(handoffContext, handoffPath) {
		t.Fatalf("handoff context omitted actual artifact path %s: %s err=%v", handoffPath, handoffContext, err)
	}
	handoff, err := repair.ValidateRepairHandoff(root, handoffRef)
	if err != nil {
		t.Fatal(err)
	}
	changeset, err := repair.ValidateChangeset(root, handoff.ChangesetRef)
	if err != nil || len(changeset.Artifacts) != len(finalArtifacts) {
		t.Fatalf("Handoff Changeset must retain the accepted Final artifact set: %+v %v", changeset.Artifacts, err)
	}
	for _, artifact := range changeset.Artifacts {
		if expected, ok := finalArtifacts[artifact.Path]; !ok || artifact.SHA256 != expected {
			t.Fatalf("Handoff Changeset differs from accepted Final batch: %+v", artifact)
		}
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("normal Impact/Handoff must not require deleting transient cache: %v", err)
	}
	seedPath, err := absoluteQuestionnairePath(root, stringValue(handoffPointer["review_plan_seed_ref"]))
	if err != nil || !strings.Contains(handoffContext, seedPath) {
		t.Fatalf("handoff context omitted actual S7 seed path %s: %s err=%v", seedPath, handoffContext, err)
	}
	if got := stringValue(mapFieldCLI(handoffSnapshot.State, "lifecycle")["state"]); got != "verification" {
		t.Fatalf("original Handoff consumer did not start S7, lifecycle state=%q", got)
	}
	if _, err := os.Stat(seedPath); err != nil {
		t.Fatalf("registered S7 seed is missing at its reported path: %v", err)
	}
	for _, path := range []string{reviewPlanQuestionnairePath(root, handoffSnapshot.State), reviewPlanGuidePath(reviewPlanQuestionnairePath(root, handoffSnapshot.State))} {
		if _, err := os.Stat(path); err != nil || !strings.Contains(handoffContext, path) {
			t.Fatalf("completion did not deliver S7 preparation %s: %v %s", path, err, handoffContext)
		}
	}
	if mainRecovery {
		assertRepairExitClosedPublicRecovery(t, root, handoffSnapshot)
		assertRepairExitAcceptedProjectionCases(t, root, targetPaths[0], handoffSnapshot, []string{"registered", "departed"})
	}
	t.Run("seed projections read only and explicit completion restores preparation", func(t *testing.T) {
		assertS9SeedProjectionReadOnlyAndCompletionRepair(t, root, handoffSnapshot)
	})
	var mainReturn bytes.Buffer
	mainEvent := policy.Input{Event: "PostToolUse", ToolName: "Agent", CWD: root, ToolInput: map[string]any{"prompt": "Return the independent Targeted answer " + targetPaths[0]}}
	if handled, code := nativeMainReturnHook(root, mainEvent, &mainReturn); !handled || code != 0 {
		t.Fatalf("Main Agent return did not deliver the completed S7 entry: %t %d %s", handled, code, &mainReturn)
	}
	var returned map[string]any
	if err := json.Unmarshal(mainReturn.Bytes(), &returned); err != nil {
		t.Fatal(err)
	}
	mainContext := stringValue(mapFieldCLI(returned, "hookSpecificOutput")["additionalContext"])
	if !strings.Contains(mainContext, seedPath) || !strings.Contains(mainContext, reviewPlanQuestionnairePath(root, handoffSnapshot.State)) {
		t.Fatalf("Main Agent return lost literal S7 paths: %s", mainContext)
	}
	planAnswer := reviewPlanQuestionnairePath(root, handoffSnapshot.State)
	var seedSave bytes.Buffer
	if handled, _ := runSavedReviewPlanQuestionnaire(root, repairLeadSaveInput(root, planAnswer, "Main"), &seedSave, &bytes.Buffer{}); !handled || strings.Contains(seedSave.String(), "already registered") || !strings.Contains(seedSave.String(), "runtime review-plan") {
		t.Fatalf("untouched machine seed bypassed first professional registration: %s", &seedSave)
	}
	if after := repairDispatchSnapshot(t, root); after.Revision != handoffSnapshot.Revision {
		t.Fatal("untouched machine seed was accepted as a professional S7 plan")
	}
	committedAgain, replayed, err := completeRepairHandoff(root)
	if err != nil || committedAgain.Path != handoffRef.Path || replayed.Revision != handoffSnapshot.Revision {
		t.Fatalf("repeated automatic Handoff changed the result: ref=%#v revision=%d want=%d err=%v", committedAgain, replayed.Revision, handoffSnapshot.Revision, err)
	}
	// A later no-change S8 return can preserve the historical repair pointer.
	// Its own S7 entry must not be described using this old repair's seed.
	entry := mapFieldCLI(mapFieldCLI(handoffSnapshot.State, "review"), "round_entry")
	entry["transition_id"] = "S8-ROUTE-CONSUME"
	if guidance := repairHandoffProjectionContext(root, handoffSnapshot.State); strings.Contains(guidance, "Fresh S7 seed") || strings.Contains(guidance, "ReviewPlan questionnaire") {
		t.Fatalf("historical repair offered its seed for a later S7 entry: %s", guidance)
	}
}

func repairLeadSaveInput(root, path, agentID string) policy.Input {
	return policy.Input{Event: "PostToolUse", AgentID: agentID, CWD: root, ToolName: "Write", ToolInput: map[string]any{"file_path": path}}
}

func readRepairLeadJSON(t *testing.T, path string, destination any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		t.Fatal(err)
	}
}

func writeRepairLeadJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRepairLeadFile(t *testing.T, root, relative, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repairLeadDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func repairLeadFreshRoot(t *testing.T) string {
	t.Helper()
	root := initFormatRoot(t)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("initialize local S9 fixture: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	return root
}

func repairLeadBaseState(t *testing.T, root, lifecycleState, phase string, revision int) map[string]any {
	t.Helper()
	reqPath := "docs/requirements/REQ-039-loop-control-plane.md"
	reqBytes := []byte("# REQ-039\n> Status: locked\n> Version: v2.0.0\n")
	writeRepairLeadFile(t, root, reqPath, string(reqBytes))
	state, err := inactiveRuntimeState(root, time.Now().UTC())
	if err != nil {
		t.Fatalf("build local S9 Runtime from current initialization facts: %v", err)
	}
	state["runtime_id"] = "loop-req039-ct"
	state["revision"] = float64(revision)
	state["lifecycle"] = map[string]any{
		"state": lifecycleState, "phase": repairLeadPhaseValue(phase), "phase_revision": 0,
	}
	milestone := mapFieldCLI(state, "milestone")
	milestone["stage"] = "S8"
	milestone["lifecycle_state"] = lifecycleState
	milestone["lifecycle_phase"] = repairLeadPhaseValue(phase)
	milestone["source_revision"] = float64(revision)
	state["authorization"] = map[string]any{
		"mode": "binding", "command": "loop-harness req bind", "actor": "user",
		"occurred_at": "2026-10-01T00:00:00Z",
	}
	state["bound_req"] = map[string]any{
		"id": "REQ-039", "path": reqPath, "version": "v2.0.0", "sha256": repairLeadDigest(reqBytes),
		"status": "locked", "approved_by": "user", "approved_at": "2026-10-01T00:00:00Z",
		"metadata": map[string]any{"ui_impact": "none"},
	}
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-10-01T00:00:00Z"}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	return state
}

func repairLeadPhaseValue(phase string) any {
	if phase == "" {
		return nil
	}
	return phase
}

func repairLeadWriteState(t *testing.T, root string, state map[string]any) {
	t.Helper()
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil {
		t.Fatal("local S9 fixture is missing its locked REQ binding")
	}
	head := repairLeadCommitFixture(t, root)
	bound["workspace"] = map[string]any{
		"project_root": root, "dev_branch": "test-development",
		"release_upstream": "origin/release", "bound_commit": head,
	}
	putInitMap(t, filepath.Join(root, ".claude", "loop-state.json"), state)
}

func repairLeadCommitFixture(t *testing.T, root string) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		repairLeadGit(t, root, "init", "-q", "-b", "test-development")
	} else if err != nil {
		t.Fatalf("inspect local S9 fixture Git repository: %v", err)
	}
	repairLeadGit(t, root, "config", "--local", "user.name", "S9 Fixture")
	repairLeadGit(t, root, "config", "--local", "user.email", "s9-fixture@example.invalid")
	repairLeadGit(t, root, "config", "--local", "commit.gpgsign", "false")
	info := filepath.Join(root, ".git", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(info, "exclude"), []byte(".claude/\n.worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repairLeadGit(t, root, "add", "-A")
	repairLeadGit(t, root, "commit", "--allow-empty", "-qm", "S9 test fixture")
	repairLeadGit(t, root, "branch", "-f", "origin/release", "HEAD")
	return repairLeadGit(t, root, "rev-parse", "--verify", "refs/heads/test-development^{commit}")
}

func repairLeadGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestS9RepairReadbackUsesOriginalSessionAndPlanWriters(t *testing.T) {
	root := repairLeadFreshRoot(t)
	state := repairLeadApprovedCase(t, root, "investigation-case-lead-readback", []map[string]any{{
		"id": "unit-lead-questionnaire", "description": "restore the source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})

	before, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	reminder := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
	after, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil || before.Revision != after.Revision || !strings.Contains(reminder, "Main/system must restore") || strings.Contains(reminder, "runtime repair session open") {
		t.Fatalf("ordinary reminder changed Runtime or omitted startup: %s (%v)", reminder, err)
	}
	runtimeID, generation, contract, err := repairReadbackContractSource(root, state)
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := repairReadbackIDSuffix(runtimeID, generation, contract.CaseID, contract.ContractID, contract.Ref)
	if err != nil {
		t.Fatal(err)
	}
	var out, commandErr bytes.Buffer
	if code := runRuntimeRepairSessionOpen([]string{"--root", root, "--session-id", "repair-session-" + suffix, "--created-by", "Main", "--actor", "Main"}, &out, &commandErr); code != 0 {
		t.Fatalf("normal session startup: %s", commandErr.String())
	}
	if code := runRuntimeRepairPlanCompile([]string{"--root", root, "--plan-id", "repair-plan-" + suffix, "--created-by", "Main", "--actor", "Main"}, &out, &commandErr); code != 0 {
		t.Fatalf("normal plan compile: %s", commandErr.String())
	}
	compiled, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	state = compiled.State
	for _, name := range []string{"builder-dispatch.json", "independent-verifier-dispatch.json"} {
		path, err := repairLeadQuestionnairePath(root, state, name)
		if err != nil || questionnaireMissing(path) {
			t.Fatalf("normal startup omitted division %s: %v", path, err)
		}
	}
	context := stageRepairDispatchContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
	prepared, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	pointer := repairPointerCLI(prepared.State)
	for _, key := range []string{"contract_ref", "plan_ref"} {
		refPath := stringValue(pointer[key])
		if refPath == "" {
			t.Fatalf("original writer did not register %s: %#v", key, pointer)
		}
		absPath, err := absoluteQuestionnairePath(root, refPath)
		if err != nil || !strings.Contains(context, absPath) {
			t.Fatalf("readback context omitted exact %s path %s: %s (err=%v)", key, absPath, context, err)
		}
	}
	if stringValue(pointer["status"]) != "planning" || !strings.Contains(context, "original RepairPlan") {
		t.Fatalf("readback did not use the original Session/Plan APIs: pointer=%#v context=%s", pointer, context)
	}
	_, err = prepareRepairReadback(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), prepared.State)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Revision != prepared.Revision {
		t.Fatalf("repeated readback preparation wrote Runtime again: revision %d -> %d", prepared.Revision, replayed.Revision)
	}
}

func TestS9RepairReadbackIDsIncludeCurrentCaseAndContract(t *testing.T) {
	root := repairLeadFreshRoot(t)
	caseA, caseB := "investigation-case-lead-readback-a", "investigation-case-lead-readback-b"
	stateA := repairLeadApprovedCase(t, root, caseA, []map[string]any{{
		"id": "unit-lead-questionnaire-a", "description": "restore Case A source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})
	first, err := prepareRepairReadback(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), stateA)
	if err != nil {
		t.Fatal(err)
	}
	firstPointer := repairPointerCLI(first)
	firstPlan, err := repair.ValidateRepairPlan(root, repair.ArtifactRef{Path: stringValue(firstPointer["plan_ref"]), SHA256: stringValue(firstPointer["plan_sha256"])})
	if err != nil {
		t.Fatal(err)
	}
	firstQuestionnairePath, err := repairLeadQuestionnairePath(root, first, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}

	// Seed the same Runtime and generation from a different approved Case while
	// leaving Case A's immutable Session/Plan artifacts in the shared root.
	stateB := repairLeadApprovedCase(t, root, caseB, []map[string]any{{
		"id": "unit-lead-questionnaire-b", "description": "restore Case B source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})
	second, err := prepareRepairReadback(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), stateB)
	if err != nil {
		t.Fatal(err)
	}
	secondPointer := repairPointerCLI(second)
	secondPlan, err := repair.ValidateRepairPlan(root, repair.ArtifactRef{Path: stringValue(secondPointer["plan_ref"]), SHA256: stringValue(secondPointer["plan_sha256"])})
	if err != nil {
		t.Fatal(err)
	}
	secondQuestionnairePath, err := repairLeadQuestionnairePath(root, second, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}
	if firstPointer["session_id"] == secondPointer["session_id"] || firstPlan.PlanID == secondPlan.PlanID {
		t.Fatalf("different Cases reused immutable S9 artifacts at identical Runtime/generation: session %q/%q plan %q/%q", firstPointer["session_id"], secondPointer["session_id"], firstPlan.PlanID, secondPlan.PlanID)
	}
	if firstPointer["contract_ref"] == secondPointer["contract_ref"] {
		t.Fatalf("different Cases reused the same approved Contract: %q", firstPointer["contract_ref"])
	}
	if firstQuestionnairePath == secondQuestionnairePath {
		t.Fatalf("different Case/Plan sources reused the same same-round questionnaire path %s", firstQuestionnairePath)
	}
	if firstPointer["case_id"] != caseA || secondPointer["case_id"] != caseB {
		t.Fatalf("readback did not follow current approved Case: first=%q second=%q", firstPointer["case_id"], secondPointer["case_id"])
	}
}

// repairLeadApprovedCase creates the S9 source through the same S7->S8 and
// Case/Contract writers used by the product. The ObservationBatch and Finding
// are minimal schema-valid fixture facts; the InvestigationCase is never
// hand-pinned into Runtime.
func repairLeadApprovedCase(t *testing.T, root, caseID string, units []map[string]any, assertionCopies ...int) map[string]any {
	t.Helper()
	state := repairLeadBaseState(t, root, "bug_resolution", "investigation", 0)
	token := dispatchSlug(caseID)
	findingID := "finding-" + token
	batchID := "observation-batch-" + token
	occurredAt := "2026-10-01T00:00:00Z"
	finding := map[string]any{
		"schema_version": "1.0.0", "finding_id": findingID, "claim_id": "claim-" + token,
		"lens": "qa", "severity": "P1", "expected": "the expected behavior holds",
		"authority_refs": []string{"REQ-039"}, "observed": "the observed behavior deviates",
		"observation_mode": "code_inspection",
		"encounter": map[string]any{
			"journey_summary":  "inspect the bound implementation and observe the deviation",
			"inspection_entry": "internal/api", "symbol_trail": "entry -> boundary",
			"wall_action": "inspect the boundary", "first_bad_checkpoint": "the invariant is false",
			"terminal_state": "review stopped",
		},
		"reproducibility": "always", "evidence_refs": []string{"test://repair-lead-finding"},
	}
	findingBytes, err := json.MarshalIndent(finding, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	findingBytes = append(findingBytes, '\n')
	findingRel := filepath.ToSlash(filepath.Join(".claude", "evidence", "findings", findingID+".json"))
	writeRepairLeadFile(t, root, findingRel, string(findingBytes))
	mapFieldCLI(state, "entities")["findings"] = []any{map[string]any{
		"finding_id": findingID, "path": findingRel, "sha256": repairLeadDigest(findingBytes),
		"claim_id": finding["claim_id"], "assignment_id": "assignment-s8-fixture", "lens": "qa",
		"severity": "P1", "observation_mode": "code_inspection", "original_finder": "reviewer-fixture",
		"review_round": 1, "created_at": occurredAt,
	}}
	batch := map[string]any{
		"schema_version": "1.0.0", "observation_batch_id": batchID, "conclusion": "sealed",
		"evidence_id": batchID, "kind": "observation_batch", "runtime_id": "loop-req039-ct",
		"producer_agent_id": "round-consumer", "producer_responsibility": "Orchestrator",
		"review_plan_id": "review-plan-repair-lead-fixture", "review_round": 1,
		"baseline_generation": 1, "subject_digest": strings.Repeat("a", 64),
		"finding_ids": []string{findingID}, "drain_policy": "complete_required_claims",
		"claim_coverage_summary": map[string]any{
			"total_required": 1, "pass": 0, "finding": 1, "not_applicable": 0,
			"blocked": 0, "blocked_claims": []any{}, "plan_revision": 1,
		},
		"unobserved_claim_ids": []any{}, "original_finder_routes": []any{},
		"investigation_readiness": []any{map[string]any{"finding_id": findingID, "status": "ready"}},
		"sealed_at":               occurredAt, "sealed_by": "round-consumer", "revision": 1,
	}
	batchBytes, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	batchBytes = append(batchBytes, '\n')
	batchRel := filepath.ToSlash(filepath.Join(".claude", "evidence", batchID+".json"))
	writeRepairLeadFile(t, root, batchRel, string(batchBytes))
	mapFieldCLI(state, "review")["observation_batch"] = map[string]any{
		"batch_id": batchID, "path": batchRel, "sha256": repairLeadDigest(batchBytes),
		"finding_ids": []string{findingID}, "drain_policy": "complete_required_claims", "sealed_at": occurredAt,
	}
	repairLeadWriteState(t, root, state)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ingested, err := investigation.Ingest(root, statePath, journalPath, investigation.IngestRequest{
		ExpectedRevision: 0, CaseID: caseID, GroupingRationale: "the sealed fixture Finding is a bounded repair Case",
		OccurredAt: time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original InvestigationCase Ingest writer: %v", err)
	}
	caseRevision := func(state map[string]any) int {
		pointer := mapFieldCLI(mapFieldCLI(state, "review"), "investigation")
		switch value := pointer["revision"].(type) {
		case float64:
			return int(value)
		case int:
			return value
		default:
			t.Fatalf("active InvestigationCase has no numeric revision: %#v", pointer["revision"])
			return 0
		}
	}
	pointer := mapFieldCLI(mapFieldCLI(ingested.State, "review"), "investigation")
	assignmentID := "assignment-hypothesis-" + token
	hypothesisID := "hypothesis-" + token
	registered, err := investigation.RegisterHypothesis(root, statePath, journalPath, investigation.HypothesisRequest{
		ExpectedRevision: ingested.Revision, ExpectedCaseRevision: caseRevision(ingested.State), ExpectedCaseSHA256: stringValue(pointer["sha256"]),
		CaseID: caseID, HypothesisID: hypothesisID, AssignmentID: assignmentID,
		Statement:        "the source authority is duplicated at the reported boundary",
		Invariant:        "the boundary has one authoritative source",
		Discriminator:    "trace the observed value across the source boundary",
		ExpectedOutcomes: map[string]any{"support": "the source differs at the boundary", "refute": "the source is consistent at the boundary"},
		SourceFindingIDs: []string{findingID}, EvidenceRefs: []string{"test://repair-lead-finding"},
		OccurredAt: time.Date(2026, 10, 1, 0, 0, 2, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original hypothesis registration writer: %v", err)
	}
	pointer = mapFieldCLI(mapFieldCLI(registered.State, "review"), "investigation")
	result, err := investigation.SubmitHypothesisResult(root, statePath, journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision: registered.Revision, ExpectedCaseRevision: caseRevision(registered.State), ExpectedCaseSHA256: stringValue(pointer["sha256"]),
		CaseID: caseID, HypothesisID: hypothesisID, AssignmentID: assignmentID,
		Method: "read-only inspection of the cited source boundary", EvidenceRefs: []string{"test://repair-lead-finding"},
		SourceBoundaryRefs: []string{"finding-source-boundary"},
		Observed:           "the observed value crosses two conflicting source definitions",
		Counterfactual:     "one authoritative source would preserve the same value across the boundary",
		Result:             "supported",
		ExplainsFindingIDs: []string{findingID},
		DoesNotExplain:     []string{},
		OccurredAt:         time.Date(2026, 10, 1, 0, 0, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original HypothesisResult writer: %v", err)
	}
	pointer = mapFieldCLI(mapFieldCLI(result.State, "review"), "investigation")
	routed, err := investigation.UpdateCaseRoute(root, statePath, journalPath, investigation.RouteRequest{
		ExpectedRevision: result.Revision, ExpectedCaseRevision: caseRevision(result.State), ExpectedCaseSHA256: stringValue(pointer["sha256"]),
		CaseID: caseID, Route: "s9_repair", RouteReason: "the fixture requires a bounded implementation repair",
		PrimaryRootCause: "the source authority is duplicated",
		CausalModel: map[string]any{
			"trigger": "payload crosses the boundary", "violated_invariant": "one owner",
			"faulty_mechanism": "duplicate authority", "propagation": "the boundary diverges",
			"symptoms": []string{"the Finding is reproduced"},
		},
		NoCompetingHypothesis: "the sealed observation supports no credible competing mechanism",
		BlastRadius:           map[string]any{"paths": []string{"internal/api"}},
		DetectionGap:          map[string]any{"gap_type": "contract", "evidence_refs": []string{"test://repair-lead-detection-gap"}},
		OccurredAt:            time.Date(2026, 10, 1, 0, 0, 4, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original InvestigationCase route writer: %v", err)
	}
	approvedCaseRevision := caseRevision(routed.State)
	contractID := "repair-contract-" + token
	draftRel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "contracts", "drafts", contractID+".json"))
	draft := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": contractID, "case_id": caseID,
		"revision": approvedCaseRevision, "status": "draft", "source_finding_ids": []string{findingID},
		"root_cause_statement": "the source authority is duplicated", "violated_invariant": "one owner",
		"causal_model_ref": "case://" + caseID + "/causal-model", "architecture_intent": "restore one source authority",
		"repair_units": units, "prospective_scope": []string{"internal/api"},
		"forbidden_scope":            []string{"docs/requirements"},
		"symptom_assertions":         []string{"the observed behavior is corrected"},
		"root_invariant_assertions":  []string{"one authority remains"},
		"detection_gap_assertions":   []string{"the boundary test detects drift"},
		"stop_escalation_conditions": []string{"the approved scope expands"},
	}
	if len(assertionCopies) > 0 && assertionCopies[0] > 1 {
		for _, field := range []string{"symptom_assertions", "root_invariant_assertions", "detection_gap_assertions"} {
			values := draft[field].([]string)
			for copy := 1; copy < assertionCopies[0]; copy++ {
				values = append(values, fmt.Sprintf("%s variation %d", values[0], copy))
			}
			draft[field] = values
		}
	}
	draftBytes, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	draftBytes = append(draftBytes, '\n')
	writeRepairLeadFile(t, root, draftRel, string(draftBytes))
	approvalHash := repairLeadDigest(draftBytes)
	decisionID := "ev-contract-approval-" + token
	decisionRel := filepath.ToSlash(filepath.Join(".claude", "decisions", decisionID+".json"))
	decisionBytes, err := json.MarshalIndent(map[string]any{
		"decision": "approve_contract", "decision_id": decisionID, "runtime_id": "loop-req039-ct",
		"case_id": caseID, "contract_id": contractID, "approved_by": "main-session", "approval_hash": approvalHash,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	decisionBytes = append(decisionBytes, '\n')
	writeRepairLeadFile(t, root, decisionRel, string(decisionBytes))
	evidenceSnapshot, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{
		ExpectedRevision: routed.Revision, ID: decisionID, Kind: "human_decision", Path: decisionRel,
		ProducedBy: []string{"main-session"}, ScopeRefs: []string{"s8_contract_approval:loop-req039-ct"},
		Validator: semantic.RuntimeCandidateValidator{},
	})
	if err != nil {
		t.Fatalf("original human approval evidence writer: %v", err)
	}
	approved, err := investigation.ApproveContract(root, statePath, journalPath, investigation.ContractRequest{
		ExpectedRevision: evidenceSnapshot.Revision, CaseID: caseID, ContractPath: draftRel,
		ApprovedBy: "main-session", ApprovalHash: approvalHash, ApprovalEvidenceID: decisionID,
		OccurredAt: time.Date(2026, 10, 1, 0, 0, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("original RepairContract approval writer: %v", err)
	}
	return approved.State
}

func writeRepairLeadTestContract(t *testing.T, root, caseID string) repair.ContractRef {
	return writeRepairLeadContractWithUnits(t, root, caseID, []map[string]any{{
		"id": "unit-lead-questionnaire", "description": "restore the source authority",
		"scope": []string{"internal/api"}, "assertion_ids": []string{"all"},
	}})
}

func writeRepairLeadContractWithUnits(t *testing.T, root, caseID string, units []map[string]any) repair.ContractRef {
	t.Helper()
	rel := filepath.ToSlash(filepath.Join(".claude", "review", "investigation", "contracts", "repair-contract-"+dispatchSlug(caseID)+".json"))
	value := map[string]any{
		"schema_version": "1.0.0", "repair_contract_id": "repair-contract-" + dispatchSlug(caseID),
		"case_id": caseID, "revision": 1, "status": "approved",
		"source_finding_ids": []string{"finding-lead-readback"}, "root_cause_statement": "two authorities drift",
		"violated_invariant": "one authority", "causal_model_ref": "case://lead-readback/model",
		"architecture_intent": "restore one authority",
		"repair_units":        units,
		"prospective_scope":   []string{"internal/api"}, "forbidden_scope": []string{"docs/requirements"},
		"symptom_assertions":         []string{"the reported behavior is corrected"},
		"root_invariant_assertions":  []string{"one authority remains"},
		"detection_gap_assertions":   []string{"the existing test catches drift"},
		"stop_escalation_conditions": []string{"the approved scope expands"},
		"approved_by":                "human", "approved_at": "2026-10-01T00:00:00Z", "approval_hash": strings.Repeat("a", 64),
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
	digest := sha256.Sum256(data)
	return repair.ContractRef{Path: rel, SHA256: hex.EncodeToString(digest[:])}
}
