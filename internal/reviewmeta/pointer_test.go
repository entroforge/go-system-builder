package reviewmeta

import (
	"encoding/json"
	"testing"
)

func TestFromStatePreservesOriginalPointerProjection(t *testing.T) {
	for _, revision := range []any{3, int64(3), float64(3), json.Number("3")} {
		row := map[string]any{"plan_id": "plan", "path": "plan.json", "revision": revision,
			"review_round": 2, "sha256": "digest", "status": "running",
			"e2e_coverage_state": "cold_start", "submitted_at": "time",
			"verification_artifact_workspace": "e2e-workspace/test", "verification_artifact_digest": "artifact"}
		ptr := FromState(map[string]any{"review": map[string]any{"plan": row}})
		if ptr == nil || ptr.Revision != 3 || ptr.ReviewRound != 2 || ptr.PlanID != "plan" || ptr.Path != "plan.json" || ptr.SHA256 != "digest" || ptr.Status != "running" || ptr.E2ECoverageState != "cold_start" || ptr.SubmittedAt != "time" || ptr.VerificationArtifactWorkspace != "e2e-workspace/test" || ptr.VerificationArtifactDigest != "artifact" {
			t.Fatalf("pointer projection changed: %+v", ptr)
		}
	}
	for _, state := range []map[string]any{nil, {}, {"review": map[string]any{}}, {"review": map[string]any{"plan": map[string]any{"path": "plan.json"}}}, {"review": map[string]any{"plan": map[string]any{"plan_id": "plan"}}}} {
		if got := FromState(state); got != nil {
			t.Fatalf("incomplete projection yielded pointer: %+v", got)
		}
	}
}
