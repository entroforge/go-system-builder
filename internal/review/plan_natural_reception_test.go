package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestNaturalPlanAmendmentPreservesUnchangedIssuedResult(t *testing.T) {
	root, statePath, journalPath, before, packets, requests := issuedConcurrentResultsFixture(t, true)
	plan, oldPtr, err := LoadPlan(root, before.State)
	if err != nil {
		t.Fatal(err)
	}
	answer := filepath.Join(root, "natural-plan.json")
	write := func() {
		t.Helper()
		data, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		var original map[string]any
		if err := json.Unmarshal(data, &original); err != nil {
			t.Fatal(err)
		}
		for _, raw := range original["claims"].([]any) {
			claim := raw.(map[string]any)
			if claim["lens"] == "qa" && claim["source_refs"] == nil {
				delete(claim, "source_refs")
			}
		}
		data, _ = json.Marshal(original)
		if err := os.WriteFile(answer, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	same, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: answer})
	if err != nil || outcome != PlanReceptionSame || same.Revision != before.Revision {
		t.Fatalf("same answer: outcome=%s revision=%d err=%v", outcome, same.Revision, err)
	}
	for i := range plan.Claims {
		if plan.Claims[i].ClaimID == "claim-dv-1" {
			plan.Claims[i].Oracle = "first updated delivery judgment"
		}
	}
	write()
	first, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: answer})
	if err != nil || outcome != PlanReceptionRevised {
		t.Fatalf("first amendment: outcome=%s err=%v", outcome, err)
	}
	for i := range plan.Claims {
		if plan.Claims[i].ClaimID == "claim-dv-1" {
			plan.Claims[i].Oracle = "second updated delivery judgment"
		}
	}
	write()
	second, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: answer})
	if err != nil || outcome != PlanReceptionRevised {
		t.Fatalf("second amendment: outcome=%s err=%v", outcome, err)
	}
	if PlanPointerFromState(first.State).Revision != oldPtr.Revision+1 || PlanPointerFromState(second.State).Revision != oldPtr.Revision+2 {
		t.Fatal("amendments did not preserve the original plan identity")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(oldPtr.Path))); err != nil {
		t.Fatalf("original plan was not preserved: %v", err)
	}
	reissued, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(reissued) != 1 || reissued[0].ID != packets[0].ID {
		t.Fatalf("unchanged issued responsibility was replaced: %#v, %v", reissued, err)
	}
	// QA was not changed; its previously issued answer must count for the
	// current Assignment. The changed delivery answer remains original-only.
	for i, p := range packets {
		requests[i].ExpectedRevision = -1
		result, err := SubmitIssuedResult(root, statePath, journalPath, requests[i])
		if err != nil {
			t.Fatalf("receive %s original answer: %v", p.Binding.AssignmentID, err)
		}
		row := resultPacketAssignmentRow(result.State, p.Binding.AssignmentID)
		if p.Binding.AssignmentID == "assignment-qa-1" {
			if row["result_ref"] == nil || Dispositions(result.State)["claim-qa-1"].Disposition != "pass" {
				t.Fatal("unchanged issued answer did not complete its current responsibility")
			}
		} else if row["result_ref"] != nil || Dispositions(result.State)["claim-dv-1"].Disposition == "pass" {
			t.Fatal("changed original answer was applied to a new responsibility")
		}
	}
	latest, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if RequiredResponsibilitiesComplete(latest.State) {
		t.Fatal("old answer incorrectly completed the changed responsibility")
	}
}

func TestNaturalPlanAcceptsUnresolvedAndCompletesAfterAmendment(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	path := writePlanFile(t, root)
	var plan Plan
	if err := json.Unmarshal(mustReadTestFile(t, path), &plan); err != nil {
		t.Fatal(err)
	}
	fullAssignments := append([]PlanAssignment(nil), plan.Assignments...)
	fullClaims := append([]Claim(nil), plan.Claims...)
	plan.Assignments = []PlanAssignment{fullAssignments[0]}
	plan.E2ECoverageState = ""
	for i := range plan.Claims {
		if plan.Claims[i].ClaimID == "claim-qa-1" {
			plan.Claims[i].Oracle = "TODO(planner)"
			plan.Claims[i].Applicability = ""
		}
	}
	write := func() {
		t.Helper()
		data, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	snap, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionAccepted {
		t.Fatalf("partial plan reception: %s %v", outcome, err)
	}
	if RequiredResponsibilitiesComplete(snap.State) {
		t.Fatal("unassigned Claims and unresolved E2E were marked complete")
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-dv-1", "agent-dv-1")
	result := writeResultFile(t, root, &plan, "assignment-dv-1", "review-result-dv-partial", "agent-dv-1", "pass", map[string]string{"claim-dv-1": "pass"}, nil)
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{ExpectedRevision: -1, AssignmentID: "assignment-dv-1", ResultPath: result})
	if err != nil {
		t.Fatal(err)
	}
	if RequiredResponsibilitiesComplete(snap.State) || PlanPointerFromState(snap.State).Status == "clean" {
		t.Fatal("partial professional plan was declared complete")
	}
	plan.Assignments = fullAssignments
	plan.Claims = fullClaims
	plan.E2ECoverageState = "not_applicable"
	write()
	snap, outcome, err = ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionRevised {
		t.Fatalf("completed plan reception: %s %v", outcome, err)
	}
	if Dispositions(snap.State)["claim-dv-1"].Disposition != "pass" {
		t.Fatal("existing accepted responsibility was lost")
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, "assignment-qa-1", "agent-qa-1")
	result = writeResultFile(t, root, &plan, "assignment-qa-1", "review-result-qa-complete", "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	var answer map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, result), &answer); err != nil {
		t.Fatal(err)
	}
	answer["assignment_revision"] = PlanPointerFromState(snap.State).Revision
	data, _ := json.Marshal(answer)
	if err := os.WriteFile(result, data, 0600); err != nil {
		t.Fatal(err)
	}
	snap, err = SubmitResult(root, statePath, journalPath, SubmitRequest{ExpectedRevision: -1, AssignmentID: "assignment-qa-1", ResultPath: result})
	if err != nil {
		t.Fatal(err)
	}
	if !RequiredResponsibilitiesComplete(snap.State) || PlanPointerFromState(snap.State).Status != "clean" {
		t.Fatal("completed responsibilities did not reach the clean exit")
	}
}

func TestNaturalPlanResolvedAfterResultsReachesClean(t *testing.T) {
	root, statePath, journalPath, before, _, requests := issuedConcurrentResultsFixture(t)
	plan, _, err := LoadPlan(root, before.State)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "natural-plan.json")
	write := func() {
		t.Helper()
		data, _ := json.Marshal(plan)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	plan.E2ECoverageState = ""
	write()
	snap, _, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		request.ExpectedRevision = -1
		snap, err = SubmitIssuedResult(root, statePath, journalPath, request)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !RequiredResponsibilitiesComplete(snap.State) || PlanPointerFromState(snap.State).Status != "clean" {
		t.Fatal("unresolved E2E metadata blocked completed actual responsibilities")
	}
	plan.E2ECoverageState = "not_applicable"
	write()
	snap, _, err = ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if !RequiredResponsibilitiesComplete(snap.State) || PlanPointerFromState(snap.State).Status != "clean" {
		t.Fatal("resolving the original plan did not continue already accepted results")
	}
}

func TestUnclassifiedClaimDoesNotBecomeRequired(t *testing.T) {
	state := map[string]any{"review": map[string]any{
		"claims": map[string]any{
			"claim-done":    map[string]any{"applicability": "required", "assignment_id": "assignment-done", "disposition": "pass"},
			"claim-unknown": map[string]any{"applicability": "", "assignment_id": "", "disposition": "planned"},
		},
		"assignments": map[string]any{
			"assignment-done": map[string]any{"claim_ids": []any{"claim-done"}, "status": "consumed", "result_receipt": map[string]any{"packet_id": "packet-done"}},
		},
	}}
	if !RequiredResponsibilitiesComplete(state) || len(UndispositionedRequired(state)) != 0 {
		t.Fatal("unclassified planning was silently turned into a required responsibility")
	}
	reviewState := state["review"].(map[string]any)
	unknown := reviewState["claims"].(map[string]any)["claim-unknown"].(map[string]any)
	unknown["assignment_id"] = "assignment-issued"
	rows := reviewState["assignments"].(map[string]any)
	rows["assignment-issued"] = map[string]any{"claim_ids": []any{"claim-unknown"}, "status": "dispatched", "agent_id": "agent-issued", "result_receipt": nil}
	if RequiredResponsibilitiesComplete(state) {
		t.Fatal("an actually issued responsibility was silently skipped")
	}
	issued := rows["assignment-issued"].(map[string]any)
	issued["status"] = "consumed"
	issued["result_receipt"] = map[string]any{"packet_id": "packet-issued"}
	if !RequiredResponsibilitiesComplete(state) {
		t.Fatal("the issued original result was not respected")
	}
}

func TestNaturalPlanAcceptsOmittedUnresolvedFields(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	path := writePlanFile(t, root)
	var answer map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, path), &answer); err != nil {
		t.Fatal(err)
	}
	delete(answer, "e2e_coverage_state")
	delete(answer, "dispatch_capacity_policy")
	delete(answer, "frozen_subjects")
	claims := answer["claims"].([]any)
	claims[0].(map[string]any)["assertion"] = "SHA values in this product are ordinary professional content"
	answer["e2e_assets"] = []any{map[string]any{"case_ref": "CASE-legacy", "path": "test.spec", "sha256": "old unverified metadata"}}
	partial := claims[1].(map[string]any)
	for _, field := range []string{"applicability", "target", "oracle", "method", "source_refs"} {
		delete(partial, field)
	}
	partial["assertion"] = "TODO(planner)"
	answer["assignments"] = []any{}
	data, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	snap, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionAccepted {
		t.Fatalf("partially expressed original plan: %s %v", outcome, err)
	}
	if RequiredResponsibilitiesComplete(snap.State) {
		t.Fatal("unassigned actual required claims were marked complete")
	}
	accepted, _, err := LoadPlan(root, snap.State)
	if err != nil || accepted.Claims[0].Assertion != "SHA values in this product are ordinary professional content" {
		t.Fatalf("professional text or ignored legacy metadata was mishandled: %v", err)
	}
	before := snap.Revision
	snap, outcome, err = ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionSame || snap.Revision != before {
		t.Fatalf("partial replay: %s %v", outcome, err)
	}
}

func TestNaturalPlanAllNotApplicableNeedsNoEmptyReviewer(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	path := writePlanFile(t, root)
	var plan Plan
	if err := json.Unmarshal(mustReadTestFile(t, path), &plan); err != nil {
		t.Fatal(err)
	}
	for i := range plan.Claims {
		plan.Claims[i].Applicability = "not_applicable"
	}
	plan.Assignments = []PlanAssignment{}
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	snap, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionAccepted {
		t.Fatalf("accept explicitly N/A plan: %s %v", outcome, err)
	}
	if !RequiredResponsibilitiesComplete(snap.State) || PlanPointerFromState(snap.State).Status != "clean" {
		t.Fatal("explicitly N/A plan required an invented Reviewer")
	}
}

func TestNaturalPlanAcceptsProfessionalTextWithoutClassification(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	path := writePlanFile(t, root)
	var answer map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, path), &answer); err != nil {
		t.Fatal(err)
	}
	claims := answer["claims"].([]any)
	claim := claims[1].(map[string]any)
	for _, key := range []string{"lens", "applicability", "assertion", "oracle", "source_refs"} {
		delete(claim, key)
	}
	claim["target"] = "internal/product"
	claim["method"] = "review the TODO(planner) text as product input"
	answer["claims"] = []any{claim}
	assignments := answer["assignments"].([]any)
	qa := assignments[1].(map[string]any)
	qa["claim_ids"] = []any{claim["claim_id"]}
	answer["assignments"] = []any{qa}
	write := func() {
		t.Helper()
		data, _ := json.Marshal(answer)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	snap, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionAccepted {
		t.Fatalf("unclassified target and method rejected: %s %v", outcome, err)
	}
	if RequiredResponsibilitiesComplete(snap.State) || PlanPointerFromState(snap.State).Status == "clean" {
		t.Fatal("undispatched Assignment was claimed complete")
	}
	claim["target"] = "internal/product/module"
	write()
	snap, outcome, err = ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionRevised {
		t.Fatalf("natural modification rejected: %s %v", outcome, err)
	}
	if RequiredResponsibilitiesComplete(snap.State) || PlanPointerFromState(snap.State).Status == "clean" {
		t.Fatal("rewriting the Plan falsely completed an undispatched Assignment")
	}
}

func TestNaturalPartialPlanIssuesAndReceivesOriginalReviewer(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, packetModeVerificationState())
	path := writePlanFile(t, root)
	var plan Plan
	if err := json.Unmarshal(mustReadTestFile(t, path), &plan); err != nil {
		t.Fatal(err)
	}
	claim := plan.Claims[1]
	claim.Lens, claim.Applicability, claim.Method = "", "", ""
	claim.SourceRefs = nil
	claim.Assertion = "the intended operation remains observable"
	assignment := plan.Assignments[1]
	assignment.ClaimIDs = []string{claim.ClaimID}
	plan.Claims = []Claim{claim}
	plan.Assignments = []PlanAssignment{assignment}
	plan.FrozenSubjects = nil
	plan.E2ECoverageState = ""
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	snap, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: path})
	if err != nil || outcome != PlanReceptionAccepted {
		t.Fatalf("partial plan reception: %s %v", outcome, err)
	}
	snap = markDispatched(t, root, statePath, journalPath, snap, assignment.AssignmentID, "agent-qa-1")
	seedResultMethodSources(t, root, snap.State, &plan)
	for _, raw := range snap.State["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			agent["state"], agent["dispatch_mode"] = "reading", "plan_checkpoint"
		}
	}
	writeMethodState(t, statePath, snap.State)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original Reviewer: %d %v", len(packets), err)
	}
	packet := packets[0]
	packet.Draft.Answer = json.RawMessage(`{"verdict":"pass"}`)
	data, _ = json.Marshal(packet.Draft)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(packet.AnswerPath)), data, 0600); err != nil {
		t.Fatal(err)
	}
	snap, err = SubmitIssuedResult(root, statePath, journalPath, IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID})
	if err != nil {
		t.Fatalf("receive original Reviewer: %v", err)
	}
	if !RequiredResponsibilitiesComplete(snap.State) || resultPacketAssignmentRow(snap.State, assignment.AssignmentID)["result_receipt"] == nil {
		t.Fatal("the actual issued Reviewer result was not accepted")
	}
}
