package runtime

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
)

func TestOriginalReviewPacketBodyIgnoresHistoricalMachineMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  bool
		old  any
	}{
		{"absent", false, nil}, {"null", true, nil}, {"nonstandard", true, "legacy"},
		{"number", true, 7}, {"object", true, map[string]any{"old": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			packet := checkpointTestReviewPacket(t, root)
			state, _ := checkpointTestReviewState(t, packet, nil)
			entities := state["entities"].(map[string]any)
			rows := entities["work_packets"].([]any)
			body := rows[0].(map[string]any)
			index, err := packetbody.Publish(context.Background(), root, body)
			if err != nil {
				t.Fatal(err)
			}
			set := func(object map[string]any, key string) {
				if tc.set {
					object[key] = tc.old
				} else {
					delete(object, key)
				}
			}
			set(index, "template_sha256")
			set(index["binding"].(map[string]any), "review_plan_sha256")
			set(index["body_ref"].(map[string]any), "sha256")
			rows[0] = index
			before, _ := json.Marshal(state)
			got, found, err := FindReviewResultPacketContext(WithPacketBodyRoot(context.Background(), root), state, packet.ID)
			if err != nil || !found || got.Binding.OwnerAgentID != packet.Binding.OwnerAgentID || got.Binding.AssignmentID != packet.Binding.AssignmentID || got.AnswerPath != packet.AnswerPath {
				t.Fatalf("original review packet: found=%v binding=%+v err=%v", found, got.Binding, err)
			}
			after, _ := json.Marshal(state)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("read rewrote original Runtime")
			}
			index["binding"].(map[string]any)["owner_agent_id"] = "other"
			if _, _, err := FindReviewResultPacketContext(WithPacketBodyRoot(context.Background(), root), state, packet.ID); err == nil {
				t.Fatal("different original owner must not be silently selected")
			}
		})
	}
}
