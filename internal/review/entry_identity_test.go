package review

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
)

func TestPlanAuthoringUsesEntryIdentityBeforeDraftAndKeepsLegacyPaths(t *testing.T) {
	root := t.TempDir()
	planID := reviewmeta.NewPlanID("runtime", "original-entry")
	rev := map[string]any{"round": 99, "round_entry": map[string]any{"plan_id": planID}}
	state := map[string]any{"runtime_id": "runtime", "baseline": map[string]any{"generation": 2}, "review": rev, "bound_req": map[string]any{"metadata": map[string]any{"ui_impact": "none"}}}
	want := filepath.Join(root, ".claude/submissions/runtime/g2/S7", planID, "review-plan.json")
	if got := PlanQuestionnairePath(root, state); got != want {
		t.Fatalf("path=%s want=%s", got, want)
	}
	plan, notes := DraftPlan(state)
	if plan == nil {
		t.Fatalf("draft failed: %v", notes)
	}
	if plan.ReviewPlanID != planID || plan.SchemaVersion != reviewmeta.IdentitySchemaVersion {
		t.Fatalf("draft identity=%s schema=%s", plan.ReviewPlanID, plan.SchemaVersion)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["review_round"]; exists {
		t.Fatal("new draft writes a round")
	}
}
