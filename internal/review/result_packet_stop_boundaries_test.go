package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/workpacket"
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
	beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	var wrongDraft map[string]json.RawMessage
	if err := json.Unmarshal(draft, &wrongDraft); err != nil {
		t.Fatal(err)
	}
	var wrongAnswer map[string]json.RawMessage
	_ = json.Unmarshal(wrongDraft["answer"], &wrongAnswer)
	var wrongFindings []map[string]json.RawMessage
	_ = json.Unmarshal(wrongAnswer["findings"], &wrongFindings)
	wrongFindings[0]["lens"] = json.RawMessage(`"delivery"`)
	wrongAnswer["findings"], _ = json.Marshal(wrongFindings)
	wrongDraft["answer"], _ = json.Marshal(wrongAnswer)
	wrongBytes, _ := json.Marshal(wrongDraft)
	if err := os.WriteFile(answerPath, wrongBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err == nil || !strings.Contains(err.Error(), "contradicts") {
		t.Fatalf("automatic metadata silently corrected an explicit conflicting lens: %v", err)
	}
	if err := os.WriteFile(answerPath, draft, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err == nil || !strings.Contains(err.Error(), "capture_gaps") {
		t.Fatalf("typed P0 without capture gaps must reach original rejection: %v", err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) {
		t.Fatal("rejected typed P0 changed Runtime or journal")
	}
	finding.Encounter.CaptureGaps = []string{"stopped before touching the destructive path"}
	answer["findings"], _ = json.Marshal([]Finding{finding})
	omitS7FindingMachineFields(t, answer)
	packet.Draft.Answer, _ = json.Marshal(answer)
	draft, _ = json.Marshal(packet.Draft)
	if err := os.WriteFile(answerPath, draft, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("typed P0 with capture gaps: %v", err)
	}
	ptr := PlanPointerFromState(accepted.State)
	if ptr == nil || ptr.Status != "observation_sealed" {
		t.Fatalf("P0 did not seal immediately: %+v", ptr)
	}
	reviewState := accepted.State["review"].(map[string]any)
	batch, _ := reviewState["observation_batch"].(map[string]any)
	if batch["drain_policy"] != "immediate_stop" {
		t.Fatalf("P0 lost immediate stop policy: %v", batch)
	}
	dv := reviewState["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
	if dv["result_ref"] != nil || dv["result_receipt"] != nil {
		t.Fatalf("P0 manufactured the unfinished DV result: %v", dv)
	}
	receipt, found, err := IssuedResultReceipt(root, accepted.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !found || receipt.ResultRef == "" {
		t.Fatalf("P0 did not retain original accepted receipt: %+v found=%t err=%v", receipt, found, err)
	}
	generatedID := "finding-" + workpacket.Digest([]byte(packet.ID + "\n" + finding.ClaimID))[:32]
	row := findFindingRow(accepted.State, generatedID)
	var persisted Finding
	if err := json.Unmarshal(mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(stringField(row["path"])))), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.SchemaVersion != "1.0.0" || persisted.Lens != "qa" || persisted.FindingID != generatedID {
		t.Fatalf("normal Finding omitted machine fields but original consumer did not bind them: %+v", persisted)
	}
	for _, ref := range persisted.EvidenceRefs {
		if !strings.Contains(ref, "#sha256=") {
			t.Fatalf("S7 handed S8 a bare path requiring author hash repair: %q", ref)
		}
		if err := evidence.ValidateFindingReference(root, ref); err != nil {
			t.Fatalf("original accepted Finding source is not consumable by S8: %v", err)
		}
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

func TestIssuedResultPacketPreservesSiteLostAssignmentBlocker(t *testing.T) {
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
	finding := codeInspectionFinding("finding-typed-site-lost", "claim-qa-1")
	finding.Encounter.LastGoodCheckpoint = ""
	resultPath := writeResultFile(t, root, plan, packet.Binding.AssignmentID, packet.ResultID, packet.Binding.OwnerAgentID, "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"}, []Finding{finding})
	var original map[string]json.RawMessage
	if err := json.Unmarshal(mustReadTestFile(t, resultPath), &original); err != nil {
		t.Fatal(err)
	}
	answer := s7ProfessionalTestAnswer(t, original)
	answer["site_lost"], _ = json.Marshal([]SiteLostDeclaration{{FindingID: finding.FindingID, Reason: "container destroyed and pre-failure logs rotated; scene cannot be reconstructed"}})
	packet.Draft.Answer, _ = json.Marshal(answer)
	draft, _ := json.Marshal(packet.Draft)
	if err := os.WriteFile(answerPath, draft, 0600); err != nil {
		t.Fatal(err)
	}
	blocked, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("typed site-lost must report original blocker: %v", err)
	}
	reviewState := blocked.State["review"].(map[string]any)
	row := reviewState["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)
	if workstate.ReviewAssignmentActivity(row) != "blocked" || row["result_ref"] != nil || row["result_receipt"] != nil {
		t.Fatalf("site-lost consumed the responsibility instead of blocking: %v", row)
	}
	if ptr := PlanPointerFromState(blocked.State); ptr == nil || ptr.Status != "running" {
		t.Fatalf("site-lost changed the round disposition: %+v", ptr)
	}
	if reviewState["observation_batch"] != nil {
		t.Fatal("site-lost sealed the round")
	}
	if findings, _ := blocked.State["entities"].(map[string]any)["findings"].([]any); len(findings) != 0 {
		t.Fatalf("site-lost registered a Finding: %v", findings)
	}
	foundAgent := false
	for _, raw := range blocked.State["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] != packet.Binding.OwnerAgentID {
			continue
		}
		foundAgent = true
		ref, _ := agent["work_blocked_ref"].(string)
		if workstate.AgentActivity(agent) != "blocked" || ref == "" {
			t.Fatalf("site-lost lost original agent blocker: %v", agent)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
			t.Fatalf("original blocker artifact missing: %v", err)
		}
	}
	if !foundAgent {
		t.Fatal("original Reviewer agent missing")
	}
	if !reflect.DeepEqual(draft, mustReadTestFile(t, answerPath)) {
		t.Fatal("site-lost rewrote author answer bytes")
	}
}
