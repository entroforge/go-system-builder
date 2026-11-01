package runtime

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestHypothesisPacketRegistryReadsHistoricalMachineMetadataWithoutDigest(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  bool
		old  any
	}{
		{"absent", false, nil},
		{"null", true, nil},
		{"nonstandard", true, "old-value"},
		{"number", true, float64(7)},
		{"object", true, map[string]any{"legacy": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newHypothesisPacketRuntimeFixture(t)
			rows := f.state["entities"].(map[string]any)["work_packets"].([]any)
			row := rows[0].(map[string]any)
			binding := row["binding"].(map[string]any)
			inputs := row["inputs"].(map[string]any)
			finding := inputs["findings"].([]any)[0].(map[string]any)
			evidence := inputs["evidence_refs"].([]any)[0].(map[string]any)
			set := func(object map[string]any, keys ...string) {
				for _, key := range keys {
					if tc.set {
						object[key] = tc.old
					} else {
						delete(object, key)
					}
				}
			}
			set(row, "template_sha256")
			set(binding, "case_sha256", "case_route_sha256", "observation_batch_sha256", "baseline_digest", "hypothesis_question_sha256", "assignment_sha256", "task_sha256", "manifest_sha256")
			set(finding, "sha256")
			set(evidence, "sha256")
			before, _ := json.Marshal(f.state)
			got, found, err := FindHypothesisResultPacket(f.state, f.packet.ID)
			if err != nil || !found {
				t.Fatalf("find original packet with historical metadata: found=%v err=%v", found, err)
			}
			if got.ID != f.packet.ID || got.Binding.OwnerAgentID != f.packet.Binding.OwnerAgentID || got.Binding.AssignmentID != f.packet.Binding.AssignmentID || got.AnswerPath != f.packet.AnswerPath {
				t.Fatalf("original packet responsibility changed: %+v", got.Binding)
			}
			after, _ := json.Marshal(f.state)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("reading packet changed the stored Runtime")
			}
			binding["owner_agent_id"] = ""
			if _, _, err := FindHypothesisResultPacket(f.state, f.packet.ID); err == nil {
				t.Fatal("a packet without its real owner must not be selected")
			}
		})
	}
}
