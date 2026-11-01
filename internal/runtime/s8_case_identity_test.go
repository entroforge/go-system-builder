package runtime

import "testing"

func TestCurrentS8PacketsFollowOriginalCaseAfterNumberlessEntry(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	review := f.state["review"].(map[string]any)
	delete(review, "round")
	review["round_entry"] = map[string]any{"plan_id": "review-plan-next"}
	packets, err := CurrentHypothesisResultPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].ID != f.packet.ID {
		t.Fatalf("original Case issuance lost: %+v", packets)
	}
	pointer := review["investigation"].(map[string]any)
	pointer["case_id"] = "investigation-case-other"
	packets, err = CurrentHypothesisResultPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].ID != f.packet.ID || packets[0].Binding.CaseID != f.packet.Binding.CaseID {
		t.Fatalf("late original Case responsibility was lost or attributed to the new Case: %+v", packets)
	}
}
