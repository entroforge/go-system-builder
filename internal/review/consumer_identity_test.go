package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func identityConsumerFixture(t *testing.T, withoutFrozen ...bool) (string, string, string, loopruntime.Snapshot, *Plan) {
	t.Helper()
	root := t.TempDir()
	state := packetModeVerificationState()
	rev := state["review"].(map[string]any)
	delete(rev, "round")
	planID := reviewmeta.NewPlanID(stringField(state["runtime_id"]), "actual-entry-event")
	rev["round_entry"] = map[string]any{"plan_id": planID, "baseline_generation": baselineGeneration(state), "transition_id": "TR-006", "change_impact_ref": nil}
	statePath, journalPath := writeState(t, root, state)
	planPath := writePlanFile(t, root)
	var plan Plan
	if err := json.Unmarshal(mustReadTestFile(t, planPath), &plan); err != nil {
		t.Fatal(err)
	}
	plan.SchemaVersion, plan.ReviewPlanID = reviewmeta.IdentitySchemaVersion, planID
	if len(withoutFrozen) > 0 && withoutFrozen[0] {
		plan.FrozenSubjects = []FrozenSubject{}
		plan.CoverageInventory = []CoverageItem{{ID: "changed-app", Kind: "post_repair_change", SourceRef: "src/app.go", Target: "src/app.go", Lens: "qa"}}
		for i := range plan.Claims {
			if plan.Claims[i].Lens == "qa" {
				plan.Claims[i].SourceRefs = append(plan.Claims[i].SourceRefs, "src/app.go")
			}
		}
	}
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(planPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	snapshot, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: planPath})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = markDispatched(t, root, statePath, journalPath, snapshot, "assignment-dv-1", "agent-dv-1")
	snapshot = markDispatched(t, root, statePath, journalPath, snapshot, "assignment-qa-1", "agent-qa-1")
	seedResultMethodSources(t, root, snapshot.State, &plan)
	writeMethodState(t, statePath, snapshot.State)
	reviewGitBaseline(t, root)
	return root, statePath, journalPath, snapshot, &plan
}

func TestIdentityS7AcceptsOriginalResultWithoutFrozenSubjects(t *testing.T) {
	root, statePath, journalPath, _, _ := identityConsumerFixture(t, true)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original packet: count=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	var subjects []FrozenSubject
	if err := json.Unmarshal(packet.Inputs.FrozenSubjects, &subjects); err != nil {
		t.Fatal(err)
	}
	if len(subjects) != 0 {
		t.Fatalf("invented frozen proof: %+v", packet.Binding)
	}
	packet.Draft.Answer = json.RawMessage(`{"claim_results":[],"blocked_claims":[],"checks":[],"findings":[],"deviations":["environment unavailable"],"site_lost":[],"verification_artifact_digest":null,"verdict":"blocked"}`)
	data, _ := json.Marshal(packet.Draft)
	if err := os.WriteFile(filepath.Join(root, packet.AnswerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := SubmitIssuedResult(root, statePath, journalPath, IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID})
	if err != nil {
		t.Fatal(err)
	}
	row := resultPacketAssignmentRow(result.State, packet.Binding.AssignmentID)
	if _, exists := row["result_receipt"].(map[string]any)["subject_digest"]; exists {
		t.Fatal("accepted result invented subject digest")
	}
}

func TestIdentityS7OriginalPacketReceptionAndReplay(t *testing.T) {
	root, statePath, journalPath, _, plan := identityConsumerFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: packets=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	if packet.Binding.ReviewPlanID != plan.ReviewPlanID {
		t.Fatal("packet lost original Plan identity")
	}
	if filepath.Base(filepath.Dir(filepath.Dir(packet.AnswerPath))) == "r0" {
		t.Fatal("new answer uses manufactured round")
	}
	packet.Draft.Answer = json.RawMessage(`{"claim_results":[],"blocked_claims":[],"checks":[],"findings":[],"deviations":["environment unavailable"],"site_lost":[],"verification_artifact_digest":null,"verdict":"blocked"}`)
	data, _ := json.Marshal(packet.Draft)
	if err := os.WriteFile(filepath.Join(root, packet.AnswerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	req := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, req)
	if err != nil {
		t.Fatal(err)
	}
	row := resultPacketAssignmentRow(accepted.State, packet.Binding.AssignmentID)
	receipt := row["result_receipt"].(map[string]any)
	if _, exists := receipt["subject_digest"]; exists {
		t.Fatal("accepted new responsibility generated a subject proof")
	}
	if _, exists := receipt["review_round"]; exists {
		t.Fatal("new receipt writes a round")
	}
	if RequiredResponsibilitiesComplete(accepted.State) {
		t.Fatal("blocked responsibility claims completion")
	}
	replay, err := SubmitIssuedResult(root, statePath, journalPath, req)
	if err != nil || replay.Revision != accepted.Revision {
		t.Fatalf("replay: %v", err)
	}
}

func TestIdentityS7RegistrationRejectsDifferentOriginalPlan(t *testing.T) {
	root, statePath, journalPath, _, plan := identityConsumerFixture(t)
	plan.ReviewPlanID = reviewmeta.NewPlanID("other-runtime", "other-entry")
	data, _ := json.Marshal(plan)
	_, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: "unused", AnswerBytes: data})
	if err == nil {
		t.Fatal("different entry Plan accepted")
	}
}

func TestIdentityS7OriginalResultsCompleteWithoutMachineProof(t *testing.T) {
	root, statePath, journalPath, _, plan := identityConsumerFixture(t)
	var accepted loopruntime.Snapshot
	for _, item := range []struct {
		assignment, owner, id string
		claims                map[string]string
	}{
		{"assignment-dv-1", "agent-dv-1", "review-result-dv-identity", map[string]string{"claim-dv-1": "pass"}},
		{"assignment-qa-1", "agent-qa-1", "review-result-qa-identity", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}},
	} {
		path := writeResultFile(t, root, plan, item.assignment, item.id, item.owner, "pass", item.claims, nil)
		var answer map[string]any
		if err := json.Unmarshal(mustReadTestFile(t, path), &answer); err != nil {
			t.Fatal(err)
		}
		answer["schema_version"] = reviewmeta.IdentitySchemaVersion
		delete(answer, "review_round")
		data, _ := json.Marshal(answer)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		var err error
		accepted, err = SubmitResult(root, statePath, journalPath, SubmitRequest{ExpectedRevision: -1, AssignmentID: item.assignment, ResultPath: path})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !RequiredResponsibilitiesComplete(accepted.State) || PlanPointerFromState(accepted.State).Status != "clean" {
		t.Fatal("accepted responsibilities did not complete")
	}
	rev := accepted.State["review"].(map[string]any)
	if _, exists := rev["clean_round"]; exists {
		t.Fatal("new completion writes numbered statistic")
	}
	for _, raw := range evidenceEntries(accepted.State) {
		row := raw.(map[string]any)
		if row["kind"] == "clean_round" || row["kind"] == "observation_batch" {
			t.Fatal("completion created retired machine proof")
		}
		if row["kind"] == "review_result" {
			if row["review_plan_id"] != plan.ReviewPlanID {
				t.Fatal("accepted Result lost original Plan relation")
			}
			if _, exists := row["review_round"]; exists {
				t.Fatal("new accepted Result writes a number")
			}
		}
	}
}

func TestIdentityFindingsKeepOriginalRelationshipAndUnknown(t *testing.T) {
	state := map[string]any{"review": map[string]any{"round_entry": map[string]any{"plan_id": "review-plan-current"}}, "entities": map[string]any{"findings": []any{
		map[string]any{"finding_id": "finding-current", "review_plan_id": "review-plan-current"},
		map[string]any{"finding_id": "finding-old", "review_plan_id": "review-plan-old"},
		map[string]any{"finding_id": "finding-unknown"},
	}}}
	rows := RoundFindings(state)
	if len(rows) != 1 || rows[0]["finding_id"] != "finding-current" {
		t.Fatalf("mixed original identities: %v", rows)
	}
	if !UnresolvedFindingIdentity(state) {
		t.Fatal("missing original identity was silently treated as historical")
	}

}

func TestIdentityS7FindingStaysWithOriginalPlanWithoutBatchProof(t *testing.T) {
	root, statePath, journalPath, _, plan := identityConsumerFixture(t)
	path := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-finding-identity", "agent-qa-1", "finding", map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{codeInspectionFinding("finding-identity", "claim-qa-1")})
	var answer map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, path), &answer); err != nil {
		t.Fatal(err)
	}
	answer["schema_version"] = reviewmeta.IdentitySchemaVersion
	delete(answer, "review_round")
	data, _ := json.Marshal(answer)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{ExpectedRevision: -1, AssignmentID: "assignment-qa-1", ResultPath: path})
	if err != nil {
		t.Fatal(err)
	}
	rows := RoundFindings(accepted.State)
	if len(rows) != 1 || rows[0]["review_plan_id"] != plan.ReviewPlanID {
		t.Fatalf("Finding lost original Plan: %v", rows)
	}
	if _, exists := rows[0]["review_round"]; exists {
		t.Fatal("new Finding writes numbered identity")
	}
	rev := accepted.State["review"].(map[string]any)
	if rev["observation_batch"] != nil {
		t.Fatal("Finding reception creates retired batch proof")
	}
	if PlanPointerFromState(accepted.State).Status != "observation_sealed" {
		t.Fatal("Finding did not enter original handoff")
	}
}

func TestIdentityUnknownHistoricalFindingDoesNotGateCurrentResponsibility(t *testing.T) {
	state := map[string]any{"review": map[string]any{
		"round_entry": map[string]any{"plan_id": "review-plan-current"},
		"assignments": map[string]any{"assignment-current": map[string]any{"status": "consumed", "claim_ids": []any{"claim-current"}, "result_verdict": "pass"}},
		"claims":      map[string]any{"claim-current": map[string]any{"applicability": "required", "disposition": "pass"}},
	}, "entities": map[string]any{"findings": []any{map[string]any{"finding_id": "finding-unknown"}}}}
	if !UnresolvedFindingIdentity(state) {
		t.Fatal("actual unknown disappeared")
	}
	if len(RoundFindings(state)) != 0 || !RequiredResponsibilitiesComplete(state) {
		t.Fatal("unknown history changed current professional conclusion")
	}
}
