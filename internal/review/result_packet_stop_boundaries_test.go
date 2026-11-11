package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workstate"
)

func TestIssuedResultPacketPreservesP0CaptureGapRejectionAndImmediateSeal(t *testing.T) {
	root, statePath, journalPath, _, plan := packetModeFixture(t)
	var state map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, statePath), &state); err != nil {
		t.Fatal(err)
	}
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			agent["state"] = "reading"
			agent["dispatch_mode"] = "plan_checkpoint"
			agent["plan_reported_ref"] = nil
			agent["activation_ref"] = nil
			agent["readback_ref"] = nil
		}
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBytes, 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue typed QA packet: count=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	answerPath := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	request := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID}
	finding := codeInspectionFinding("finding-typed-p0", "claim-qa-1")
	for i, ref := range finding.EvidenceRefs {
		finding.EvidenceRefs[i] = strings.SplitN(ref, "#sha256=", 2)[0]
	}
	finding.Severity = "P0"
	finding.Encounter.LastGoodCheckpoint = ""
	finding.Encounter.CaptureGaps = nil
	resultPath := writeResultFile(t, root, plan, packet.Binding.AssignmentID, packet.ResultID, packet.Binding.OwnerAgentID, "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{finding})
	var original map[string]json.RawMessage
	if err := json.Unmarshal(mustReadTestFile(t, resultPath), &original); err != nil {
		t.Fatal(err)
	}
	answer := s7ProfessionalTestAnswer(t, original)
	omitS7FindingMachineFields(t, answer)
	packet.Draft.Answer, _ = json.Marshal(answer)
	draft, _ := json.Marshal(packet.Draft)
	if err := os.WriteFile(answerPath, draft, 0600); err != nil {
		t.Fatal(err)
	}
	// A Finding can describe another lens without changing its author's responsibility.
	var authored map[string]json.RawMessage
	if err := json.Unmarshal(draft, &authored); err != nil {
		t.Fatal(err)
	}
	var professional map[string]json.RawMessage
	if err := json.Unmarshal(authored["answer"], &professional); err != nil {
		t.Fatal(err)
	}
	var findings []map[string]json.RawMessage
	if err := json.Unmarshal(professional["findings"], &findings); err != nil {
		t.Fatal(err)
	}
	findings[0]["lens"] = json.RawMessage(`"delivery"`)
	professional["findings"], _ = json.Marshal(findings)
	authored["answer"], _ = json.Marshal(professional)
	draft, _ = json.Marshal(authored)
	if err := os.WriteFile(answerPath, draft, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("typed P0 cross-lens observation rejected: %v", err)
	}
	ptr := PlanPointerFromState(accepted.State)
	if ptr == nil || ptr.Status != "observation_sealed" {
		t.Fatalf("P0 did not seal immediately: %+v", ptr)
	}
	reviewState := accepted.State["review"].(map[string]any)
	if len(RoundFindings(accepted.State)) != 1 {
		t.Fatalf("P0 original Finding was not preserved: %v", RoundFindings(accepted.State))
	}
	dv := reviewState["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
	if dv["result_ref"] != nil || dv["result_receipt"] != nil {
		t.Fatalf("P0 manufactured the unfinished DV result: %v", dv)
	}
	receipt, found, err := IssuedResultReceipt(root, accepted.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !found || receipt.ResultRef == "" {
		t.Fatalf("P0 did not retain original accepted receipt: %+v found=%t err=%v", receipt, found, err)
	}
	row := RoundFindings(accepted.State)[0]
	var persisted Finding
	if err := json.Unmarshal(mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(stringField(row["path"])))), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Lens != "delivery" || persisted.FindingID == "" {
		t.Fatalf("authored cross-lens Finding lost: %+v", persisted)
	}
	stateBeforeReplay, journalBeforeReplay := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("same authored Finding became a changed answer after machine adaptation: %v", err)
	}
	if !reflect.DeepEqual(stateBeforeReplay, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(journalBeforeReplay, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(draft, mustReadTestFile(t, answerPath)) {
		t.Fatal("same-answer Finding replay rewrote authority or author bytes")
	}
}

func omitS7FindingMachineFields(t *testing.T, answer map[string]json.RawMessage) {
	t.Helper()
	var findings []map[string]json.RawMessage
	if err := json.Unmarshal(answer["findings"], &findings); err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		for _, key := range []string{"schema_version", "finding_id", "lens"} {
			delete(finding, key)
		}
	}
	answer["findings"], _ = json.Marshal(findings)
}

func TestIssuedResultPacketReceivesBlockedConclusionWithoutCompleting(t *testing.T) {
	root, statePath, journalPath, _, plan := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original QA packet: count=%d err=%v", len(packets), err)
	}
	packet := packets[0]
	resultPath := writeResultFile(t, root, plan, packet.Binding.AssignmentID, packet.ResultID, packet.Binding.OwnerAgentID, "blocked", map[string]string{}, nil)
	var original map[string]json.RawMessage
	if err := json.Unmarshal(mustReadTestFile(t, resultPath), &original); err != nil {
		t.Fatal(err)
	}
	packet.Draft.Answer, _ = json.Marshal(s7ProfessionalTestAnswer(t, original))
	draft, _ := json.Marshal(packet.Draft)
	answerPath := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.WriteFile(answerPath, draft, 0600); err != nil {
		t.Fatal(err)
	}
	received, err := SubmitIssuedResult(root, statePath, journalPath, IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID})
	if err != nil {
		t.Fatalf("original blocked answer was not received: %v", err)
	}
	reviewState := received.State["review"].(map[string]any)
	row := reviewState["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)
	if workstate.ReviewAssignmentActivity(row) != "blocked" || row["result_ref"] == nil {
		t.Fatalf("blocked answer lost original responsibility: %v", row)
	}
	if ptr := PlanPointerFromState(received.State); ptr == nil || ptr.Status == "clean" {
		t.Fatalf("blocked answer completed the plan: %+v", ptr)
	}
	if !reflect.DeepEqual(draft, mustReadTestFile(t, answerPath)) {
		t.Fatal("reception changed the original answer")
	}
}
