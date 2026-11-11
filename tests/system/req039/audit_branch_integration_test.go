package req039_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readSystemState(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func completionTaskRef(t *testing.T, state map[string]any, taskID string) string {
	t.Helper()
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task["id"] == taskID {
			ref, _ := task["completion_report_ref"].(string)
			return ref
		}
	}
	t.Fatalf("TASK %s missing completion_report_ref", taskID)
	return ""
}
