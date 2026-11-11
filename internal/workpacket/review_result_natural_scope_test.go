package workpacket

import (
	"encoding/json"
	"testing"
)

func TestReviewPacketWithNoFrozenSubjectsUsesOriginalPlan(t *testing.T) {
	binding := reviewResultTestBinding()
	inputs := reviewResultTestInputs()
	inputs.FrozenSubjects = json.RawMessage(`[]`)
	packet, err := BuildReviewResult(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, read := range packet.Draft.Read {
		found = found || read.Path == binding.ReviewPlanRef
	}
	if !found {
		t.Fatal("Reviewer cannot find assigned Claims and their real sources")
	}
}
