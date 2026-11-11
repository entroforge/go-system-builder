package runtime

import (
	"encoding/json"
	"testing"
)

func TestCurrentReviewPacketReadsOriginalResponsibilityWithUnrelatedFields(t *testing.T) {
	packet := checkpointTestReviewPacket(t, t.TempDir())
	raw, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	row["unrelated_metadata"] = map[string]any{"note": "ordinary source content"}
	row["binding"].(map[string]any)["unrelated_metadata"] = true
	got, err := decodeReviewResultPacket(row)
	if err != nil {
		t.Fatalf("unrelated fields blocked original packet: %v", err)
	}
	if got.ID != packet.ID || got.Binding.ReviewPlanID != packet.Binding.ReviewPlanID || got.Binding.OwnerAgentID != packet.Binding.OwnerAgentID {
		t.Fatalf("original responsibility changed: %+v", got.Binding)
	}
	if row["unrelated_metadata"] == nil {
		t.Fatal("original packet row was rewritten")
	}
}
