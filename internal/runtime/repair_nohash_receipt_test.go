package runtime

import (
	"encoding/json"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9OriginalReceiptsUseActualResponsibilityAndContent(t *testing.T) {
	binding := workpacket.RepairPlanReportBinding{RuntimeID: "loop-test", SessionID: "repair-session-a", PlanID: "repair-plan-a", RepairAssignmentID: "repair-assignment-a", OwnerAgentID: "builder-a"}
	plan := workpacket.RepairPlanReportPacket{ID: "packet-plan", Binding: binding}
	answer := json.RawMessage(`{"plan":"do the repair","red_checks":[],"proposed_paths":[]}`)
	doc := map[string]any{"plan": "do the repair", "red_checks": []any{}, "proposed_paths": []any{}}
	receipt := RepairPlanReportReceipt{PacketID: plan.ID, OwnerAgentID: binding.OwnerAgentID, RuntimeID: binding.RuntimeID, RepairAssignmentID: binding.RepairAssignmentID, ReportRef: "report.json", SubmittedAnswer: answer}
	if err := verifyRepairPlanReportReceipt("", nil, plan, controlproof.JournalInspection{}, doc, "report.json", receipt); err != nil {
		t.Fatal(err)
	}
	doc["plan"] = "different work"
	if err := verifyRepairPlanReportReceipt("", nil, plan, controlproof.JournalInspection{}, doc, "report.json", receipt); err == nil {
		t.Fatal("different professional content was accepted as same")
	}
	result := workpacket.RepairResultPacket{ID: "packet-result", Binding: workpacket.RepairResultBinding{Sources: binding}}
	resultReceipt := RepairResultReceipt{ResultRef: "result.json", Provenance: RepairResultCandidateProvenance{PacketID: result.ID, OwnerAgentID: binding.OwnerAgentID, SubmittedAnswer: json.RawMessage(`{"verdict":"completed"}`)}}
	if err := verifyRepairResultReceipt("", nil, result, controlproof.JournalInspection{}, map[string]any{"verdict": "completed"}, "result.json", resultReceipt); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepairResultReceipt("", nil, result, controlproof.JournalInspection{}, map[string]any{"verdict": "blocked"}, "result.json", resultReceipt); err == nil {
		t.Fatal("different verdict was accepted as same")
	}
}
