package review

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentityTypesUsePlanAndOmitCounts(t *testing.T) {
	for _, version := range []string{"1.0.0", "1.1.0"} {
		raw := `{"schema_version":"` + version + `","review_plan_id":"review-plan-original","review_round":3}`
		var plan Plan
		var result Result
		if err := json.Unmarshal([]byte(raw), &plan); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatal(err)
		}
		if plan.Identity().PlanID != "review-plan-original" || result.Identity().PlanID != "review-plan-original" {
			t.Fatal("lost original plan identity")
		}
	}
	for _, artifact := range []any{Plan{SchemaVersion: "1.1.0", ReviewPlanID: "review-plan-entry"}, Result{SchemaVersion: "1.1.0", ReviewPlanID: "review-plan-entry"}} {
		raw, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"review_round"`) {
			t.Fatalf("artifact generated a count: %s", raw)
		}
	}
}
