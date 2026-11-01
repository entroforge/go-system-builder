package investigation_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Receipt identity comes from the original Case Writer, including after a
// sibling receives its answer and the original route is consumed.
func TestS8OriginalReceiptSurvivesSiblingAndRouteContinuation(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, true)
	for _, owner := range owners[:2] {
		runS8AgentBegin(t, fixture, owner)
	}
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
	if err != nil {
		t.Fatal(err)
	}
	byOwner := make(map[string]int)
	for i := range packets {
		byOwner[packets[i].Binding.OwnerAgentID] = i
	}
	for _, owner := range owners[:2] {
		index, found := byOwner[owner.ownerID]
		if !found {
			t.Fatalf("original owner has no issued packet: %s", owner.ownerID)
		}
		packet := packets[index]
		answerPath := writeS8ProfessionalAnswer(t, fixture.root, packet, "The recorded boundary observation supports the original question.")
		if _, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.IssuedHypothesisResultRequest{
			ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: answerPath, OwnerAgentID: owner.ownerID, SourceRoot: fixture.root,
		}); err != nil {
			t.Fatalf("original Case Writer for %s: %v", owner.ownerID, err)
		}
	}
	first := packets[byOwner[owners[0].ownerID]]
	current := currentS8Snapshot(t, fixture)
	if receipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, current.State, first.ID, owners[0].ownerID); err != nil || !found || receipt.PacketID != first.ID {
		t.Fatalf("original receipt after sibling: %+v %t %v", receipt, found, err)
	}
	selected, err := runtime.CurrentHypothesisResultPackets(fixture.root, current.State, owners[0].ownerID)
	if err != nil || len(selected) != 1 || selected[0].ID != first.ID {
		t.Fatalf("original responsibility after sibling: %+v %v", selected, err)
	}
	pointer := investigationPointerFromState(t, current.State)
	routed, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision: current.Revision, ExpectedCaseRevision: s8TestInt(pointer["revision"]), CaseID: pointer["case_id"].(string), Route: "investigate_more", RouteReason: "Continue the remaining original questions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := investigation.ConsumeCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.ConsumeRouteRequest{
		ExpectedRevision: routed.Revision, CaseID: pointer["case_id"].(string), Actor: "orchestrator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, consumed.State, first.ID, owners[0].ownerID); err != nil || !found || receipt.PacketID != first.ID {
		t.Fatalf("original receipt after route: %+v %t %v", receipt, found, err)
	}
}

func TestS8InconclusiveOriginalReceiptSurvivesPrivateAnswerRemoval(t *testing.T) {
	fixture, owners := prepareS8GenerationTwoFixture(t, false)
	owner := owners[0]
	runS8AgentBegin(t, fixture, owner)
	packets, err := investigation.IssueHypothesisResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, owner.ownerID)
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue original question: %+v %v", packets, err)
	}
	packet := packets[0]
	answerPath := writeS8ProfessionalAnswer(t, fixture.root, packet, "The available evidence does not distinguish the possibilities.")
	absolute := filepath.Join(fixture.root, filepath.FromSlash(answerPath))
	data, err := os.ReadFile(absolute)
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	draft["answer"].(map[string]any)["result"] = "inconclusive"
	data, err = json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, data, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := investigation.SubmitIssuedHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.IssuedHypothesisResultRequest{
		ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: answerPath, OwnerAgentID: owner.ownerID, SourceRoot: fixture.root,
	})
	if err != nil {
		t.Fatalf("original inconclusive Case Writer: %v", err)
	}
	if err := os.Remove(absolute); err != nil {
		t.Fatal(err)
	}
	receipt, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, accepted.State, packet.ID, owner.ownerID)
	if err != nil || !found || receipt.Result != "inconclusive" {
		t.Fatalf("original receipt after private removal: %+v %t %v", receipt, found, err)
	}
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "SubagentStop", "agent_id": owner.ownerID, "session_id": "s8-real-receipt", "cwd": fixture.root})
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", fixture.root, "--event", "SubagentStop"}, bytes.NewReader(payload), &out, &diagnostic); code != 0 {
		t.Fatalf("accepted inconclusive delivery blocked on private answer: %d %s %s", code, out.String(), diagnostic.String())
	}
	after := currentS8Snapshot(t, fixture)
	if _, found, err := runtime.VerifiedHypothesisResultReceipt(fixture.root, after.State, packet.ID, owner.ownerID); err != nil || !found {
		t.Fatalf("Hook lost accepted original receipt: %t %v", found, err)
	}
}
