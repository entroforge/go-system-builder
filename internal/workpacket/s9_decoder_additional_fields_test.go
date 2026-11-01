package workpacket

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestS9PacketAndProfessionalAnswerAllowUnrelatedFields(t *testing.T) {
	t.Run("plan", func(t *testing.T) {
		binding, inputs := repairPlanReportTestFixture(t)
		packet, err := BuildRepairPlanReport(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(packet)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		raw["annotation"] = "outside current responsibility"
		raw["binding"].(map[string]any)["annotation"] = "extra"
		raw["inputs"].(map[string]any)["annotation"] = "extra"
		raw["draft"].(map[string]any)["annotation"] = "extra"
		encoded, _ := json.Marshal(raw)
		var decoded RepairPlanReportPacket
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.ID != packet.ID || decoded.Binding.OwnerAgentID != packet.Binding.OwnerAgentID || decoded.AnswerPath != packet.AnswerPath {
			t.Fatalf("original packet unavailable: %+v %v", decoded.Binding, err)
		}
		answer := []byte(`{"plan":"Inspect the actual boundary and repair the failing behavior.","additional_note":{"sha256":"a business value"}}`)
		actual, problems := ValidateRepairPlanReportAnswer(decoded, answer)
		if len(problems) != 0 || !bytes.Contains(actual, []byte(`"sha256":"a business value"`)) {
			t.Fatalf("original professional content not retained: %s %+v", actual, problems)
		}
		if _, problems := ValidateRepairPlanReportAnswer(decoded, []byte(`{"plan":"one","plan":"two"}`)); len(problems) == 0 {
			t.Fatal("ambiguous professional plan accepted")
		}
	})
	t.Run("result", func(t *testing.T) {
		binding, inputs := repairResultTestFixture(t)
		packet, err := BuildRepairResult(binding, inputs)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(packet)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		raw["annotation"] = "outside current responsibility"
		raw["binding"].(map[string]any)["annotation"] = "extra"
		raw["inputs"].(map[string]any)["annotation"] = "extra"
		raw["draft"].(map[string]any)["annotation"] = "extra"
		encoded, _ := json.Marshal(raw)
		var decoded RepairResultPacket
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.ID != packet.ID || decoded.Binding.Sources.OwnerAgentID != packet.Binding.Sources.OwnerAgentID || decoded.AnswerPath != packet.AnswerPath {
			t.Fatalf("original packet unavailable: %+v %v", decoded.Binding, err)
		}
		answer := repairResultCompleteAnswer()
		answer["additional_note"] = map[string]any{"sha256": "a business value"}
		units := answer["unit_results"].([]any)
		units[0].(map[string]any)["note"] = "additional professional context"
		actual, problems := ValidateRepairResultAnswer(decoded, mustJSON(t, answer))
		if len(problems) != 0 || !bytes.Contains(actual, []byte(`"sha256":"a business value"`)) || !bytes.Contains(actual, []byte(`"note":"additional professional context"`)) {
			t.Fatalf("original professional content not retained: %s %+v", actual, problems)
		}
		if _, problems := ValidateRepairResultAnswer(decoded, []byte(`{"verdict":"completed","verdict":"blocked"}`)); len(problems) == 0 {
			t.Fatal("ambiguous professional verdict accepted")
		}
	})
}
