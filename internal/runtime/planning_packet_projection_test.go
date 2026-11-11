package runtime

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Exercise the same registry reads used by each planning stage with historical
// machine values, preserving the owner and original Runtime document.
func TestPlanningPacketRegistryHistoricalMachineValues(t *testing.T) {
	for _, stage := range []struct{ name, template, inputs string }{
		{"S2", "s2-design-v1", ""},
		{"S3", "s3-contract-v1", "designs"},
		{"S4", "s4-task-planning-v1", "contracts"},
	} {
		for _, old := range []struct {
			name  string
			set   bool
			value any
		}{
			{"absent", false, nil}, {"null", true, nil}, {"text", true, "historical"}, {"number", true, 7}, {"object", true, map[string]any{"old": true}},
		} {
			t.Run(stage.name+"/"+old.name, func(t *testing.T) {
				req := map[string]any{"id": "REQ-1", "path": "docs/requirements/REQ-1.md", "version": "v1"}
				row := map[string]any{"packet_id": "packet", "template_id": stage.template, "binding": map[string]any{"runtime_id": "loop", "owner": "Main", "stage": stage.name, "requirement": req}}
				var ref map[string]any
				if stage.inputs != "" {
					ref = map[string]any{"id": "doc", "path": "docs/doc.md", "version": "v1"}
					row["inputs"] = map[string]any{stage.inputs: []any{ref}}
				}
				if old.set {
					row["template_sha256"] = old.value
					req["sha256"] = old.value
					if ref != nil {
						ref["sha256"] = old.value
					}
				}
				state := map[string]any{"entities": map[string]any{"work_packets": []any{row}}}
				before, _ := json.Marshal(state)
				var id, owner string
				var found bool
				var err error
				switch stage.name {
				case "S2":
					p, ok, e := FindDesignPacket(state, "packet")
					id, owner, found, err = p.ID, p.Binding.Owner, ok, e
				case "S3":
					p, ok, e := FindContractPacket(state, "packet")
					id, owner, found, err = p.ID, p.Binding.Owner, ok, e
				case "S4":
					p, ok, e := FindTaskPlanningPacket(state, "packet")
					id, owner, found, err = p.ID, p.Binding.Owner, ok, e
				}
				if err != nil || !found || id != "packet" || owner != "Main" {
					t.Fatalf("read original %s packet: id=%s owner=%s found=%v err=%v", stage.name, id, owner, found, err)
				}
				after, _ := json.Marshal(state)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("original Runtime changed during read")
				}
				row["binding"].(map[string]any)["owner"] = "other"
				switch stage.name {
				case "S2":
					p, _, e := FindDesignPacket(state, "packet")
					owner, err = p.Binding.Owner, e
				case "S3":
					p, _, e := FindContractPacket(state, "packet")
					owner, err = p.Binding.Owner, e
				case "S4":
					p, _, e := FindTaskPlanningPacket(state, "packet")
					owner, err = p.Binding.Owner, e
				}
				if err != nil || owner != "other" {
					t.Fatalf("projection changed actual owner: %q %v", owner, err)
				}
			})
		}
	}
}
