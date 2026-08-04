package cli

import "testing"

func TestStatusProjectionUsesChangeRecordOpenWorkAndChecks(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-REQ-002",
		"revision":   3,
		"change": map[string]any{
			"id": "CHG-001", "summary": "fix timeout", "class": "bugfix", "risk": "medium",
			"work_items": []any{
				map[string]any{"id": "W-1", "text": "fix timeout", "status": "open", "owner": "main", "depends_on": []any{}, "write_paths": []any{"internal/client/**"}},
			},
			"required_checks": []any{
				map[string]any{"id": "CK-1", "kind": "regression_test", "status": "open"},
			},
			"findings": []any{},
		},
	}
	projection := buildStatusProjection(state, "S6", "building", nil, ".")
	if projection.Change == nil || projection.Change.ID != "CHG-001" {
		t.Fatalf("missing change projection: %#v", projection.Change)
	}
	if len(projection.OpenItems) != 2 {
		t.Fatalf("open items = %#v", projection.OpenItems)
	}
	if projection.OpenItems[0] != "W-1: fix timeout" || projection.OpenItems[1] != "CK-1: regression_test" {
		t.Fatalf("unexpected open items = %#v", projection.OpenItems)
	}
}
func TestNextProjectionReturnsChangeWorkInsteadOfTransitionInstruction(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-REQ-002",
		"revision":   3,
		"change": map[string]any{
			"id": "CHG-001", "summary": "fix timeout", "class": "bugfix", "risk": "medium",
			"work_items": []any{
				map[string]any{"id": "W-1", "text": "fix timeout", "status": "open", "owner": "main", "depends_on": []any{}, "write_paths": []any{"internal/client/**"}},
			},
			"required_checks": []any{
				map[string]any{"id": "CK-1", "kind": "regression_test", "status": "open"},
			},
			"findings": []any{},
		},
	}
	projection := buildNextProjection(state, "S6", "loop-orchestration", "legacy transition instruction", ".")
	if projection.ChangeID != "CHG-001" || projection.WorkItemID != "W-1" || projection.CheckIDs[0] != "CK-1" {
		t.Fatalf("change next projection = %#v", projection)
	}
	if projection.Action != "implement W-1 and run CK-1" {
		t.Fatalf("next action = %q", projection.Action)
	}
}
