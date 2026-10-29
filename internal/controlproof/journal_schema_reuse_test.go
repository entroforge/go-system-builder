package controlproof

import (
	"encoding/json"
	"testing"
)

func TestInspectJournalLocalSchemaReuseStillValidatesLaterEvents(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing_outcome", func(e map[string]any) { delete(e, "outcome") }},
		{"unknown_property", func(e map[string]any) { e["forged_grant"] = true }},
		{"duplicate_id", func(e map[string]any) { e["event_id"] = "evt-one" }},
		{"wrong_runtime", func(e map[string]any) { e["runtime_id"] = "loop-foreign" }},
		{"sequence_gap", func(e map[string]any) { e["sequence"] = 3 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := controlProofEvent("loop-proof", "evt-one", 1, "TEST-ONE", 0, 1)
			second := controlProofEvent("loop-proof", "evt-two", 2, "TEST-TWO", 1, 2)
			test.change(second)
			// Build raw bytes directly: the normal fixture helper itself
			// validates events and would reject these before the reader runs.
			firstBytes, _ := json.Marshal(first)
			secondBytes, _ := json.Marshal(second)
			data := append(append(append(firstBytes, '\n'), secondBytes...), '\n')
			if _, err := InspectJournalData(data); err == nil {
				t.Fatal("later invalid event passed reused schema/cursor validation")
			}
		})
	}
}
