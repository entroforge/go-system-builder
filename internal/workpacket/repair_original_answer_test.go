package workpacket

import (
	"encoding/json"
	"testing"
)

func TestS9OriginalFlatAnswersUseOriginalConsumers(t *testing.T) {
	binding, inputs := repairPlanReportTestFixture(t)
	plan, err := BuildRepairPlanReport(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"plan":"Apply the approved correction and check the original behavior","red_checks":[{"name":"original behavior","command":"go test ./api","result":"blocked","evidence_refs":["logs/blocker.txt"]}],"proposed_paths":[]}`)
	if _, fields := ValidateRepairPlanReportAnswer(plan, raw); len(fields) != 0 {
		t.Fatalf("original Plan rejected: %+v", fields)
	}
	rb, ri := repairResultTestFixture(t)
	result, err := BuildRepairResult(rb, ri)
	if err != nil {
		t.Fatal(err)
	}
	professional := repairResultCompleteAnswer()
	raw, err = json.Marshal(professional)
	if err != nil {
		t.Fatal(err)
	}
	if _, fields := ValidateRepairResultAnswer(result, raw); len(fields) != 0 {
		t.Fatalf("original Result rejected: %+v", fields)
	}
}
