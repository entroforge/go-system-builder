package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// An original dispatched responsibility can report an environment blocker
// without inventing a product Finding or claiming a completed round.
func TestS7OriginalBlockedReceptionAndReceiptWithoutPrivateAnswer(t *testing.T) {
	root, statePath, journalPath, _, _ := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v packets=%d", err, len(packets))
	}
	packet := packets[0]
	packet.Draft.Answer = json.RawMessage(`{"claim_results":[],"blocked_claims":[],"checks":[],"findings":[],"deviations":["test environment unavailable; Main must restore it"],"site_lost":[],"verification_artifact_digest":null,"verdict":"blocked"}`)
	data, err := json.Marshal(packet.Draft)
	if err != nil {
		t.Fatal(err)
	}
	answer := filepath.Join(root, packet.AnswerPath)
	if err = os.WriteFile(answer, data, 0600); err != nil {
		t.Fatal(err)
	}
	request := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	if RequiredResponsibilitiesComplete(accepted.State) {
		t.Fatal("blocked responsibility completed the round")
	}
	if len(RoundFindings(accepted.State)) != 0 {
		t.Fatal("environment blocker manufactured a Finding")
	}
	if err = os.Remove(answer); err != nil {
		t.Fatal(err)
	}
	receipt, ok, err := IssuedResultReceipt(root, accepted.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !ok || receipt.ResultRef == "" {
		t.Fatalf("accepted fact lost with private answer: %t %v", ok, err)
	}
	replay, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil || replay.Revision != accepted.Revision {
		t.Fatalf("replay rereceived or read missing private answer: %v", err)
	}
	if _, err = loopruntime.NewStore(statePath, journalPath).Snapshot(); err != nil {
		t.Fatal(err)
	}
}

func TestS7NecessaryResponsibilityConclusionsDriveCompletion(t *testing.T) {
	state := map[string]any{"review": map[string]any{
		"assignments": map[string]any{"a": map[string]any{"status": "consumed", "claim_ids": []any{"c"}, "result_verdict": "pass"}},
		"claims":      map[string]any{"c": map[string]any{"applicability": "required", "disposition": "finding"}},
	}}
	if !RequiredResponsibilitiesComplete(state) {
		t.Fatal("direct accepted professional conclusion was replaced by claim coverage recomputation")
	}
	state["review"].(map[string]any)["assignments"].(map[string]any)["a"].(map[string]any)["result_verdict"] = "blocked"
	if RequiredResponsibilitiesComplete(state) {
		t.Fatal("blocked conclusion passed")
	}
}

func TestS7BlockedNaturalContinuationPreservesAcceptedHistory(t *testing.T) {
	root, statePath, journalPath, _, _ := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue: %v packets=%d", err, len(packets))
	}
	packet := packets[0]
	path := filepath.Join(root, packet.AnswerPath)
	write := func(answer string) {
		t.Helper()
		packet.Draft.Answer = json.RawMessage(answer)
		data, err := json.Marshal(packet.Draft)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	request := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID}
	write(`{"verdict":"blocked","deviations":["environment unavailable; Main coordinates"]}`)
	blocked, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	first, ok, err := IssuedResultReceipt(root, blocked.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !ok || ResultWorkComplete(blocked.State, packet.Binding.AssignmentID) {
		t.Fatalf("blocked receipt: accepted=%t error=%v", ok, err)
	}
	old, err := os.ReadFile(filepath.Join(root, first.ResultRef))
	if err != nil {
		t.Fatal(err)
	}
	same, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil || same.Revision != blocked.Revision {
		t.Fatalf("same handoff was received twice: %v", err)
	}
	write(`{"verdict":"pass","checks":[{"command":"optional check","result":"fail"}]}`)
	finished, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	last, ok, err := IssuedResultReceipt(root, finished.State, packet.ID, packet.Binding.OwnerAgentID)
	if err != nil || !ok || !ResultWorkComplete(finished.State, packet.Binding.AssignmentID) || last.ResultRef == first.ResultRef {
		t.Fatalf("natural continuation did not receive a new handoff: %t %v", ok, err)
	}
	retained, err := os.ReadFile(filepath.Join(root, first.ResultRef))
	if err != nil || string(retained) != string(old) {
		t.Fatalf("original handoff changed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replay, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil || replay.Revision != finished.Revision {
		t.Fatalf("completed replay inspected private answer: %v", err)
	}
}
