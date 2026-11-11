package runtime

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS5PacketRegistryReadsHistoricalMachineMetadataWithoutDigest(t *testing.T) {
	p, err := workpacket.BuildS5(workpacket.Binding{RuntimeID: "loop-REQ-039", Generation: 1, StageEntry: 7, Owner: "reviewer-spec", AssignmentID: "assignment-spec", Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1}, []workpacket.Reference{{ID: "REQ-039", Path: "docs/requirements/REQ-039.md", Version: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err = workpacket.BindTaskAnalysis(p, &workpacket.TaskAnalysisRef{Path: ".claude/work-packets/analysis/current.json", ParserVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		set  bool
		old  any
	}{
		{"absent", false, nil}, {"null", true, nil}, {"nonstandard", true, "old"}, {"number", true, 7}, {"object", true, map[string]any{"legacy": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(p)
			var row map[string]any
			if err := json.Unmarshal(data, &row); err != nil {
				t.Fatal(err)
			}
			binding := row["binding"].(map[string]any)
			ref := row["inputs"].([]any)[0].(map[string]any)
			analysis := row["task_analysis"].(map[string]any)
			set := func(object map[string]any, key string) {
				if tc.set {
					object[key] = tc.old
				} else {
					delete(object, key)
				}
			}
			set(row, "template_sha256")
			set(binding, "assignment_sha256")
			set(ref, "sha256")
			set(analysis, "sha256")
			data, _ = json.Marshal(row)
			if err := schema.NewEmbeddedValidator().ValidateBytes("work-packet.schema.json", data); err != nil {
				t.Fatal(err)
			}
			state := map[string]any{"entities": map[string]any{"work_packets": []any{row}}}
			before, _ := json.Marshal(state)
			got, found, err := FindWorkPacket(state, p.ID)
			if err != nil || !found || got.Binding.Owner != p.Binding.Owner || got.Binding.AssignmentID != p.Binding.AssignmentID || got.AnswerPath != p.AnswerPath || got.TaskAnalysis.Path != p.TaskAnalysis.Path {
				t.Fatalf("original packet responsibility was not retained: found=%v packet=%+v err=%v", found, got.Binding, err)
			}
			after, _ := json.Marshal(state)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("packet read changed the original state")
			}
			binding["owner"] = ""
			data, _ = json.Marshal(row)
			if err := schema.NewEmbeddedValidator().ValidateBytes("work-packet.schema.json", data); err == nil {
				t.Fatal("missing actual owner must remain invalid")
			}
		})
	}
}
