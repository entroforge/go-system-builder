package review

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
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

// The registration identity names the Plan responsibility and carries no
// Runtime write count (G18 §1.1, §1.3): the same registration retried keeps
// one identity, and two Plan responsibilities never share one.
func TestReviewPlanRegistrationIdentityIsSemantic(t *testing.T) {
	planID := reviewmeta.NewPlanID("runtime", "evt-tr-012-1:2:12")
	eventID := reviewPlanEventID(planID)
	key := reviewPlanIdempotencyKey(planID)
	if eventID != reviewPlanEventID(planID) || key != reviewPlanIdempotencyKey(planID) {
		t.Fatal("the same registration must keep one identity")
	}
	if !strings.HasPrefix(eventID, "evt-review-plan-") || !strings.HasPrefix(key, "runtime:review-plan:") {
		t.Fatalf("unexpected registration identity: %q / %q", eventID, key)
	}
	other := reviewmeta.NewPlanID("runtime", "evt-tr-012-1:2:21")
	if reviewPlanEventID(other) == eventID || reviewPlanIdempotencyKey(other) == key {
		t.Fatal("two Plan responsibilities share one registration identity")
	}
	// The historical shape stays readable on its original path (G18 §1.4).
	if !legacyReviewPlanEventID("evt-review-plan-"+planID+"-r12", planID) {
		t.Fatal("historical registration event id not recognized")
	}
	if legacyReviewPlanEventID(eventID, planID) {
		t.Fatal("the current registration event id must not pass as the historical shape")
	}
	if legacyReviewPlanEventID("evt-review-plan-"+other+"-r12", planID) {
		t.Fatal("another Plan's historical event id must not be recognized")
	}
}

// The revision identity names the Plan responsibility and the Plan revision it
// produces. The Plan revision is the domain's own number, not a Runtime write
// count (G18 §1.2).
func TestReviewPlanRevisionIdentityNamesThePlanRevision(t *testing.T) {
	planID := reviewmeta.NewPlanID("runtime", "evt-tr-012-1:2:12")
	eventID := reviewReviseEventID(planID, 2)
	key := reviewReviseIdempotencyKey(planID, 2)
	if eventID != reviewReviseEventID(planID, 2) || key != reviewReviseIdempotencyKey(planID, 2) {
		t.Fatal("the same revision must keep one identity")
	}
	if reviewReviseEventID(planID, 3) == eventID || reviewReviseIdempotencyKey(planID, 3) == key {
		t.Fatal("two Plan revisions share one identity")
	}
	if reviewReviseEventID("", 2) != "" || reviewReviseIdempotencyKey(planID, 0) != "" {
		t.Fatal("an unidentified revision must not name an identity")
	}
}

// The supplement identity names the observation it extends, never the Runtime
// write count (G18 §1.1, §1.2).
func TestFindingSupplementIdentityNamesTheObservation(t *testing.T) {
	state := map[string]any{"baseline": map[string]any{"generation": 2}}
	observation := map[string]any{"author": "agent-demo", "assertion": "the build stays reproducible"}
	first := generatedSupplementID(state, "finding-1", observation)
	if first != generatedSupplementID(state, "finding-1", observation) {
		t.Fatal("the same observation must keep one identity")
	}
	if !strings.HasPrefix(first, "supplement-g2-") {
		t.Fatalf("the supplement identity must name its generation: %q", first)
	}
	if strings.Contains(first, "-r") {
		t.Fatalf("the supplement identity still carries a Runtime write count: %q", first)
	}
	if generatedSupplementID(state, "finding-2", observation) == first {
		t.Fatal("two Findings share one supplement identity")
	}
	other := map[string]any{"author": "agent-demo", "assertion": "a different observation"}
	if generatedSupplementID(state, "finding-1", other) == first {
		t.Fatal("two observations share one supplement identity")
	}
	if findingSupplementEventID(first) != "evt-finding-supplement-"+first ||
		findingSupplementIdempotencyKey(first) != "runtime:finding-supplement:"+first {
		t.Fatal("the supplement identity is not the append identity")
	}
}
