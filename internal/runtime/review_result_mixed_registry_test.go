package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestReviewResultReadersSelectOnlyS7InMixedPacketRegistry(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, p, nil)
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	s7, err := packetbody.Publish(context.Background(), root, rows[0].(map[string]any))
	if err != nil {
		t.Fatal(err)
	}
	s9 := newRepairResultRuntimeFixture(t)
	toRow := func(value any) map[string]any {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	var otherIDs []string
	mixed := []any{s7}
	for _, packet := range []any{s9.plan.packet, s9.packet} {
		row := toRow(packet)
		index, err := packetbody.Publish(context.Background(), root, row)
		if err != nil {
			t.Fatal(err)
		}
		ref, _, err := packetbody.ReferenceFromIndex(index)
		if err != nil {
			t.Fatal(err)
		}
		// A consumer of the S7 registry must not touch these unavailable S9
		// bodies just because their lightweight indexes share the namespace.
		if err := os.Remove(filepath.Join(root, ref.Path)); err != nil {
			t.Fatal(err)
		}
		mixed = append(mixed, index)
		otherIDs = append(otherIDs, stringValue(index["packet_id"]))
	}
	s5, err := workpacket.BuildS5(workpacket.Binding{
		RuntimeID: p.Binding.RuntimeID, Generation: 1, StageEntry: 7, Owner: "reviewer-spec", AssignmentID: "assignment-spec", AssignmentSHA: workpacket.Digest([]byte("manifest")), Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1,
	}, []workpacket.Reference{{ID: "REQ-039", Path: "req.md", Version: "v1.0.0", SHA256: workpacket.Digest([]byte("requirement"))}})
	if err != nil {
		t.Fatal(err)
	}
	mixed = append(mixed, toRow(s5))
	otherIDs = append(otherIDs, s5.ID)
	entities["work_packets"] = mixed
	ctx := WithReviewResultReadCache(WithPacketBodyRoot(context.Background(), root))
	for _, id := range otherIDs {
		if _, found, err := FindReviewResultPacketContext(ctx, state, id); err != nil || found {
			t.Fatalf("S7 lookup consumed another subtype %s: %t %v", id, found, err)
		}
	}
	got, err := CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], p) {
		t.Fatalf("mixed current S7 packets: %v %v", got, err)
	}
	all, err := ReviewResultPacketsContext(ctx, state)
	if err != nil || len(all) != 1 || !reflect.DeepEqual(all[0], p) {
		t.Fatalf("mixed historical S7 packets: %v %v", all, err)
	}
	// Global identity remains shared even when the duplicate belongs to an
	// unrelated subtype and no caller asks for that packet's body.
	s5Row := toRow(s5)
	s5Row["packet_id"] = p.ID
	entities["work_packets"] = append(mixed, s5Row)
	if _, _, err := FindReviewResultPacketContext(ctx, state, p.ID); err == nil {
		t.Fatal("cross-subtype duplicate bypassed S7 lookup")
	}
	if _, err := CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID); err == nil {
		t.Fatal("cross-subtype duplicate bypassed current selection")
	}
	if _, err := ReviewResultPacketsContext(ctx, state); err == nil {
		t.Fatal("cross-subtype duplicate bypassed historical list")
	}
}
