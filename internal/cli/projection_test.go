package cli

import "testing"

func TestStatusProjectionAtHumanReleaseUsesEmptyOpenItemsArray(t *testing.T) {
	projection := buildStatusProjection(map[string]any{
		"runtime_id": "loop-REQ-001",
		"revision":   52,
	}, "S11", "awaiting_human_release", nil, ".")
	if projection.OpenItems == nil {
		t.Fatal("S11 status projection must encode open_items as an empty array, not null")
	}
	if len(projection.OpenItems) != 0 {
		t.Fatalf("expected no open items at human release gateway, got %v", projection.OpenItems)
	}
}
