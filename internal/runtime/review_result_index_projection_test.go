package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
)

func TestReviewResultIndexProjectionPreservesKnownFactsWithoutBodyAuthority(t *testing.T) {
	root := t.TempDir()
	packet := checkpointTestReviewPacket(t, root)
	state, _ := checkpointTestReviewState(t, packet, nil)
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	index, err := packetbody.Publish(context.Background(), root, rows[0].(map[string]any))
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := packetbody.ReferenceFromIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ref.Path)); err != nil {
		t.Fatal(err)
	}
	entities["work_packets"] = []any{index}
	projected, err := ProjectCurrentReviewResultPacketIndex(state, packet.Binding.OwnerAgentID)
	if err != nil || len(projected) != 1 || projected[0].ID != packet.ID || projected[0].ViewPath != packet.ViewPath || projected[0].AnswerPath != packet.AnswerPath || projected[0].Binding.OwnerAgentID != packet.Binding.OwnerAgentID {
		t.Fatalf("known responsibility lost: %v %v", projected, err)
	}
	projected[0].Binding.ClaimIDs[0] = "caller mutation"
	again, err := ProjectCurrentReviewResultPacketIndex(state, packet.Binding.OwnerAgentID)
	if err != nil || again[0].Binding.ClaimIDs[0] != packet.Binding.ClaimIDs[0] {
		t.Fatalf("projection mutated Runtime binding: %v", err)
	}
	canceled, cancel := context.WithCancel(WithPacketBodyRoot(context.Background(), root))
	cancel()
	if _, err := CurrentReviewResultPacketsContext(canceled, state, packet.Binding.OwnerAgentID); !errors.Is(err, context.Canceled) {
		t.Fatalf("body authority reader ignored cancellation: %v", err)
	}
	if _, err := CurrentReviewResultPacketsContext(WithPacketBodyRoot(context.Background(), root), state, packet.Binding.OwnerAgentID); err == nil {
		t.Fatal("display projection substituted for required missing body")
	}
	entities["work_packets"] = []any{index, map[string]any{"packet_id": packet.ID, "template_id": "s5-document-review-v1"}}
	if _, err := ProjectCurrentReviewResultPacketIndex(state, packet.Binding.OwnerAgentID); err == nil {
		t.Fatal("display projection hid cross-subtype duplicate identity")
	}
	entities["work_packets"] = []any{index}
	index["storage_format"] = "future-unknown-format"
	if _, err := ProjectCurrentReviewResultPacketIndex(state, packet.Binding.OwnerAgentID); err == nil {
		t.Fatal("display projection accepted unknown index format")
	}
}
