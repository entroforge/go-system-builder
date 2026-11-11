package runtime

import (
	"reflect"
	"testing"
)

func TestRelocationRetainsHistoricalMachineFieldsAndChecksRealResponsibility(t *testing.T) {
	old := map[string]any{
		"main_root": "/old", "common_dir": "/old/.git", "bound_head": map[string]any{"old": true},
		"executions": map[string]any{"a": map[string]any{"agent_id": "owner", "worktree_path": "/old/w", "base_commit": []any{1.0}, "bootstrap_sha256": 3.0, "inputs": map[string]any{"doc": map[string]any{"old": true}}}},
	}
	old["unrecognized_history"] = map[string]any{"sha256": []any{nil, 7.0}}
	old["executions"].(map[string]any)["a"].(map[string]any)["unknown_detail"] = []any{map[string]any{"old": true}}
	proposed := map[string]any{
		"main_root": "/new", "common_dir": "/new/.git",
		"executions": map[string]any{"a": map[string]any{"agent_id": "owner", "worktree_path": "/new/w", "inputs": map[string]any{"doc": "doc"}}},
	}
	retained, err := relocationRegistryWithCoordinates(old, proposed)
	if err != nil {
		t.Fatal(err)
	}
	row := retained["executions"].(map[string]any)["a"].(map[string]any)
	if row["worktree_path"] != "/new/w" || !reflect.DeepEqual(row["base_commit"], []any{1.0}) || row["bootstrap_sha256"] != 3.0 || !reflect.DeepEqual(retained["bound_head"], old["bound_head"]) || !reflect.DeepEqual(row["inputs"], old["executions"].(map[string]any)["a"].(map[string]any)["inputs"]) {
		t.Fatalf("relocation rewrote historical Runtime data: %#v", retained)
	}
	if !reflect.DeepEqual(retained["unrecognized_history"], old["unrecognized_history"]) || !reflect.DeepEqual(row["unknown_detail"], old["executions"].(map[string]any)["a"].(map[string]any)["unknown_detail"]) {
		t.Fatal("relocation rewrote unknown historical data")
	}
	row["worktree_path"] = "/old/w"
	retained["main_root"], retained["common_dir"] = "/old", "/old/.git"
	a, _ := relocationComparableRegistry(retained)
	b, _ := relocationComparableRegistry(old)
	projected, _ := relocationComparableRegistry(proposed)
	projected["main_root"], projected["common_dir"] = "/old", "/old/.git"
	projected["executions"].(map[string]any)["a"].(map[string]any)["worktree_path"] = "/old/w"
	if !reflect.DeepEqual(projected, b) {
		t.Fatal("unknown history became a relocation requirement")
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("historical machine data affected original responsibility comparison")
	}
	row["agent_id"] = "other"
	a, _ = relocationComparableRegistry(retained)
	if reflect.DeepEqual(a, b) {
		t.Fatal("different owner was ignored")
	}
}

func TestRelocationComparesCurrentNestedFactsWithoutHistoricalMetadata(t *testing.T) {
	old := map[string]any{"executions": map[string]any{"a": map[string]any{
		"assignment_record":   map[string]any{"assignment_id": "a", "unknown": map[string]any{"old": true}},
		"tool_check_receipts": []any{map[string]any{"path": "/old/check", "command": "true", "binding": map[string]any{"owner": "author", "unknown": 7}, "unknown": false}},
		"check_profiles":      []any{map[string]any{"command": "true", "dependencies": []any{map[string]any{"path": "source", "unknown": 7}}, "unknown": []any{1}}},
	}}}
	proposed := map[string]any{"executions": map[string]any{"a": map[string]any{
		"assignment_record":   map[string]any{"assignment_id": "a"},
		"tool_check_receipts": []any{map[string]any{"path": "/old/check", "command": "true", "binding": map[string]any{"owner": "author"}}},
		"check_profiles":      []any{map[string]any{"command": "true", "dependencies": []any{map[string]any{"path": "source"}}}},
	}}}
	a, _ := relocationComparableRegistry(old)
	b, _ := relocationComparableRegistry(proposed)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("nested historical metadata became a responsibility requirement")
	}
	row := proposed["executions"].(map[string]any)["a"].(map[string]any)
	row["tool_check_receipts"].([]any)[0].(map[string]any)["binding"].(map[string]any)["owner"] = "other"
	b, _ = relocationComparableRegistry(proposed)
	if reflect.DeepEqual(a, b) {
		t.Fatal("actual check owner change was ignored")
	}
}
