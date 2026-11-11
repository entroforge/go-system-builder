package qualitygate

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"testing"
)

func TestS10SelectsOriginalPlanSourceWithoutNumericOrdering(t *testing.T) {
	state := map[string]any{
		"baseline": map[string]any{"generation": 1},
		"review":   map[string]any{"round": 9, "round_entry": map[string]any{"plan_id": "plan-current"}},
		"evidence": []any{
			map[string]any{"id": "current", "kind": "acceptance", "status": "valid", "baseline_generation": 1, "review_plan_id": "plan-current"},
			map[string]any{"id": "old", "kind": "acceptance", "status": "valid", "baseline_generation": 1, "review_plan_id": "plan-old"},
			map[string]any{"id": "numbered-old", "kind": "acceptance", "status": "valid", "baseline_generation": 1, "review_round": 9},
			map[string]any{"id": "unresolved", "kind": "acceptance", "status": "valid", "baseline_generation": 1},
		},
	}
	input := Input{Snapshot: runtime.Snapshot{State: state}}
	if got := latestS10EvidenceID(input, nil, "acceptance_record"); got != "current" {
		t.Fatalf("old or unresolved material became current: %s", got)
	}
}

func TestS10ManifestBindingUsesActualPlanAndPreservesUnknown(t *testing.T) {
	input := Input{Snapshot: runtime.Snapshot{State: map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 1}, "review": map[string]any{"round_entry": map[string]any{"plan_id": "current"}}}}}
	envelope := evidenceEnvelope{RuntimeID: "runtime", BaselineGeneration: 1, ReviewPlanID: "current"}
	for _, tc := range []struct {
		plan string
		want bool
	}{{"current", true}, {"old", false}, {"", true}} {
		data, _ := json.Marshal(map[string]any{"runtime_id": "runtime", "baseline_generation": 1, "review_plan_id": tc.plan})
		if got := s10ManifestBindingMatches(input, envelope, data); got != tc.want {
			t.Fatalf("source %q: %t, want %t", tc.plan, got, tc.want)
		}
	}
}
