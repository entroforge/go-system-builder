package runtime

import (
	"encoding/json"
	"testing"
)

func TestTaskPlanningCheckpointProjectsRowsWithoutReplayingHistory(t *testing.T) {
	state := map[string]any{"entities": map[string]any{
		"work_packets": []any{map[string]any{"template_id": "s4-task-planning-v1", "packet_id": "recorded-s4"}, map[string]any{"template_id": "other", "packet_id": "other"}},
		"submissions":  []any{map[string]any{"packet_id": "recorded-s4"}, map[string]any{"packet_id": "other"}},
	}}
	before, _ := json.Marshal(state)
	projected, err := validateTaskPlanningCheckpointIndices(t.TempDir(), state, []byte("history is not a qualification input"))
	if err != nil {
		t.Fatal(err)
	}
	entities := projected["entities"].(map[string]any)
	if len(entities["work_packets"].([]any)) != 1 || len(entities["submissions"].([]any)) != 1 {
		t.Fatalf("S5 temporary projection retained S4 rows: %#v", entities)
	}
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Fatal("temporary projection changed persisted input")
	}
}

func TestTaskPlanningCheckpointReportsUnreadableRow(t *testing.T) {
	state := map[string]any{"entities": map[string]any{"work_packets": []any{map[string]any{"template_id": "s4-task-planning-v1"}}}}
	if _, err := validateTaskPlanningCheckpointIndices("", state, nil); err == nil {
		t.Fatal("missing packet id was not reported")
	}
}
