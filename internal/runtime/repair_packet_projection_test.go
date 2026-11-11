package runtime

import (
	"encoding/json"
	"testing"
)

func TestRepairPacketRegistryRetainsOriginalResponsibilityWithAdditionalFields(t *testing.T) {
	f := newRepairResultRuntimeFixture(t)
	rows := f.plan.state["entities"].(map[string]any)["work_packets"].([]any)
	var row map[string]any
	for _, item := range rows {
		candidate, _ := item.(map[string]any)
		if candidate["packet_id"] == f.packet.ID {
			row = candidate
			break
		}
	}
	if row == nil {
		t.Fatal("fixture missing original packet")
	}
	row["additional_context"] = "non-authoritative annotation"
	row["binding"].(map[string]any)["additional_context"] = "another annotation"
	before, err := json.Marshal(f.plan.state)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := FindRepairResultPacket(f.plan.state, f.packet.ID)
	if err != nil || !found || got.ID != f.packet.ID || got.Binding.Sources.OwnerAgentID != f.packet.Binding.Sources.OwnerAgentID || got.AnswerPath != f.packet.AnswerPath {
		t.Fatalf("original packet not read: found=%t id=%s owner=%s err=%v", found, got.ID, got.Binding.Sources.OwnerAgentID, err)
	}
	after, err := json.Marshal(f.plan.state)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("packet observation changed Runtime")
	}
}
