package assignmentrecord

import (
	"encoding/json"
	"testing"
)

func TestAssignmentStateLookupPreservesOriginalOwner(t *testing.T) {
	taskBytes := []byte("locked TASK source")
	manifest, state, sources := recordTestSources(t, taskBytes)
	record, err := BuildRecord(manifest, "docs/workgroups/s6/manifest.json", state, taskBytes, "TASK-042-01", "assignment-s6-record", []string{"internal"}, sources)
	if err != nil {
		t.Fatal(err)
	}
	entities := state["entities"].(map[string]any)
	data, _ := json.Marshal(record)
	var row map[string]any
	_ = json.Unmarshal(data, &row)
	entities["assignments"] = []any{row}
	got, found, err := ForOwner(state, record.OwnerAgentID)
	if err != nil || !found || got.AssignmentID != record.AssignmentID {
		t.Fatalf("ForOwner = %v, %v, %v", got.AssignmentID, found, err)
	}
	if !AllowsPath(got, "internal/pkg/handler.go") || AllowsPath(got, "internal/other/handler.go") || AllowsPath(got, "../outside") {
		t.Fatal("canonical scope was not preserved")
	}
	entities["agents"].([]any)[0].(map[string]any)["team_id"] = "workgroup-other"
	if _, _, err := ForOwner(state, record.OwnerAgentID); err == nil {
		t.Fatal("owner projection drift must reject")
	}
}

func TestAssignmentStateMalformedRegistryCannotFallBack(t *testing.T) {
	for _, raw := range []any{nil, "legacy", []any{map[string]any{"assignment_id": "assignment-broken"}}} {
		state := map[string]any{"entities": map[string]any{"assignments": raw}}
		if _, _, err := ForOwner(state, "agent-owner"); err == nil {
			t.Fatalf("malformed registry accepted: %v", raw)
		}
	}
}
