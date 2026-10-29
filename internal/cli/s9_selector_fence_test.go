package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9TypedSelectorCannotInferLegacyFromMalformedIndex(t *testing.T) {
	for _, index := range []any{nil, "not an array", map[string]any{}, []any{"invalid row"}, []any{map[string]any{}}, []any{map[string]any{"packet_id": "p", "template_id": "s5-document-review-v1"}, map[string]any{"packet_id": "p", "template_id": "s5-document-review-v1"}}} {
		state := map[string]any{"entities": map[string]any{"work_packets": index}}
		if _, _, err := typedRepairResultPacketForAssignmentContext(t.Context(), t.TempDir(), state, "assignment", "owner"); err == nil {
			t.Fatalf("malformed index became absence of typed responsibility: %#v", index)
		}
	}
}

func TestS9TypedSelectorRetainsHistoricalAssignmentFence(t *testing.T) {
	binding := workpacket.RepairResultBinding{Sources: workpacket.RepairPlanReportBinding{RuntimeID: "runtime", BaselineGeneration: 1, ReviewRound: 1, ManifestAssignmentID: "assignment", RepairAssignmentID: "repair", OwnerAgentID: "owner", PlanRef: "old-plan"}}
	data, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 2}, "review": map[string]any{"round": 2, "repair": map[string]any{"plan_ref": "new-plan"}}, "entities": map[string]any{"work_packets": []any{map[string]any{"packet_id": "original", "template_id": "s9-repair-result-v1", "binding": object}}}}
	_, found, err := typedRepairResultPacketForAssignmentContext(t.Context(), t.TempDir(), state, "assignment", "owner")
	if !found || err == nil || !strings.Contains(err.Error(), "no legacy fallback") {
		t.Fatalf("historical typed assignment lost its fence: found=%v error=%v", found, err)
	}
	if _, found, err := typedRepairResultPacketForAssignmentContext(t.Context(), t.TempDir(), state, "unrelated", "owner"); found || err != nil {
		t.Fatalf("same owner borrowed another assignment: found=%v error=%v", found, err)
	}
}
