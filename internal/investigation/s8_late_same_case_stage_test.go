package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
)

// The owner continues an already issued question after real contract approval.
// Its late answer is retained without modifying the approved Case or stage.
func TestLateS8SameApprovedCaseKeepsOriginalResponsibility(t *testing.T) {
	f, owners := prepareS8GenerationTwoFixture(t, true)
	packets, err := investigation.IssueHypothesisResultPackets(f.root, f.statePath, f.journalPath, -1, "")
	if err != nil || len(packets) != len(owners) {
		t.Fatalf("issue: %d %v", len(packets), err)
	}
	service := &submission.Service{Root: f.root, StatePath: f.statePath, JournalPath: f.journalPath}
	var targetPath, targetOwner, targetID string
	for i, owner := range owners {
		var index int
		for j, p := range packets {
			if p.Binding.OwnerAgentID == owner.ownerID {
				index = j
				break
			}
		}
		packet := packets[index]
		path := writeS8ProfessionalAnswer(t, f.root, packet, "Original professional observation")
		if i == 0 {
			raw, err := os.ReadFile(filepath.Join(f.root, path))
			if err != nil {
				t.Fatal(err)
			}
			var draft map[string]any
			if err := json.Unmarshal(raw, &draft); err != nil {
				t.Fatal(err)
			}
			answer := draft["answer"].(map[string]any)
			answer["result"], answer["explains_finding_ids"] = "inconclusive", []any{}
			updated, _ := json.Marshal(draft)
			if err := os.WriteFile(filepath.Join(f.root, path), updated, 0600); err != nil {
				t.Fatal(err)
			}
			targetPath, targetOwner, targetID = path, owner.ownerID, packet.ID
		}
		receipt, err := service.SaveHypothesisResultFrom(context.Background(), owner.ownerID, filepath.Join(f.root, path), "", nil)
		if err != nil || !receipt.Accepted {
			t.Fatalf("original answer %s: %+v %v", owner.ownerID, receipt, err)
		}
	}
	prepareCaseForContractApproval(t, f)
	contract := writeContractDraft(t, f.root, []string{"finding-1"})
	snapshot := currentS8Snapshot(t, f)
	casePointer := investigationPointerFromState(t, snapshot.State)
	caseID := casePointer["case_id"].(string)
	contractBytes := mustRead(t, contract)
	var draft map[string]any
	if err := json.Unmarshal(contractBytes, &draft); err != nil {
		t.Fatal(err)
	}
	draft["revision"] = casePointer["revision"]
	updatedContract, _ := json.Marshal(draft)
	if err := os.WriteFile(contract, updatedContract, 0600); err != nil {
		t.Fatal(err)
	}
	_, approvalID, _ := registerContractApprovalEvidence(t, f, contract)
	approved, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, investigation.ContractRequest{ExpectedRevision: -1, CaseID: caseID, ContractPath: contract, ApprovedBy: "main-session", ApprovalEvidenceID: approvalID})
	if err != nil {
		t.Fatalf("real approval: %v", err)
	}
	_, _, _, err = repair.OpenRepairSession(f.root, f.statePath, f.journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: -1, Actor: "Main"}, SessionID: "repair-session-s8-late", CreatedBy: "Main"})
	if err != nil {
		t.Fatalf("open real repair session: %v", err)
	}
	before := currentS8Snapshot(t, f)
	pointer := investigationPointerFromState(t, before.State)
	caseBytes := mustRead(t, filepath.Join(f.root, pointer["path"].(string)))
	originalApproval, _ := json.Marshal(investigationPointerFromState(t, approved.State))
	currentApproval, _ := json.Marshal(pointer)
	if !bytes.Equal(originalApproval, currentApproval) {
		t.Fatal("repair opening changed the approved Case")
	}
	var targetPacketID string
	for _, p := range packets {
		if p.ID == targetID {
			writeS8ProfessionalAnswer(t, f.root, p, "Late independent professional observation")
			targetPacketID = p.ID
		}
	}
	receipt, err := service.SaveHypothesisResultFrom(context.Background(), targetOwner, filepath.Join(f.root, targetPath), "", nil)
	if err != nil || !receipt.Accepted {
		t.Fatalf("late original result: %+v %v", receipt, err)
	}
	after := currentS8Snapshot(t, f)
	if !bytes.Equal(caseBytes, mustRead(t, filepath.Join(f.root, pointer["path"].(string)))) {
		t.Fatal("late answer rewrote approved Case")
	}
	afterPointer, _ := json.Marshal(investigationPointerFromState(t, after.State))
	if !bytes.Equal(currentApproval, afterPointer) {
		t.Fatal("late answer changed approval")
	}
	beforeLife, _ := json.Marshal(before.State["lifecycle"])
	afterLife, _ := json.Marshal(after.State["lifecycle"])
	if !bytes.Equal(beforeLife, afterLife) {
		t.Fatal("late answer moved lifecycle")
	}
	packet, found, err := runtime.FindHypothesisResultPacket(after.State, targetPacketID)
	if err != nil || !found || len(packet.LateResults) != 1 || !bytes.Contains(packet.LateResults[0].Answer, []byte("Late independent professional observation")) {
		t.Fatalf("late original packet: %+v %v", packet.LateResults, err)
	}
	replay, err := service.SaveHypothesisResultFrom(context.Background(), targetOwner, filepath.Join(f.root, targetPath), "", nil)
	if err != nil || !replay.Accepted || currentS8Snapshot(t, f).Revision != after.Revision {
		t.Fatalf("late replay: %+v %v", replay, err)
	}
}
