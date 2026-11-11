package workpacket

import (
	"encoding/json"
	"testing"
)

func TestRepairResultWithoutPlanPreservesOriginalBinding(t *testing.T) {
	binding, inputs := repairResultTestFixture(t)
	binding.PlanReportRef = ""
	inputs.PlanReport = json.RawMessage("null")
	packet, err := BuildRepairResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var replay RepairResultPacket
	if err := json.Unmarshal(encoded, &replay); err != nil {
		t.Fatal(err)
	}
	if _, errors := ValidateRepairResultAnswer(replay, []byte(`{"verdict":"blocked"}`)); len(errors) != 0 {
		t.Fatalf("valid blocker: %+v", errors)
	}
}
