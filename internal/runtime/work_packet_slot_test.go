package runtime

import (
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"testing"
)

func TestDocumentReviewSlotCoexistsWithOtherTypedDomains(t *testing.T) {
	packet := workpacket.Packet{Binding: workpacket.Binding{RuntimeID: "loop-test", Generation: 2, StageEntry: 3, Owner: "reviewer-1", Responsibility: "DV-MAIN"}}
	rows := []any{}
	for _, subtype := range []string{"s2-design-v1", "s3-contract-v1", "s4-task-planning-v1", "s7-review-result-v1", "s8-hypothesis-result-v1", "s9-plan-report-v1", "s9-repair-result-v1"} {
		rows = append(rows, map[string]any{"template_id": subtype, "binding": map[string]any{"owner_agent_id": "reviewer-1", "runtime_id": "loop-test", "baseline_generation": 2}})
	}
	if err := validateDocumentReviewPacketSlot(rows, packet); err != nil {
		t.Fatalf("unrelated historical subtype blocked original S5 issuance slot: %v", err)
	}
	own := map[string]any{"template_id": "s5-document-review-v1", "binding": map[string]any{"runtime_id": "loop-test", "generation": 2, "stage_entry": 3, "owner": "reviewer-1", "responsibility": "DV-MAIN"}}
	rows = append(rows, own)
	if err := validateDocumentReviewPacketSlot(rows, packet); err == nil {
		t.Fatal("exact original S5 responsibility allowed second issuance")
	}
	own["binding"].(map[string]any)["stage_entry"] = 4
	if err := validateDocumentReviewPacketSlot(rows, packet); err != nil {
		t.Fatalf("different original S5 stage entry blocked: %v", err)
	}
	delete(own["binding"].(map[string]any), "stage_entry")
	if err := validateDocumentReviewPacketSlot(rows, packet); err == nil {
		t.Fatal("malformed original S5 binding silently skipped")
	}
}
