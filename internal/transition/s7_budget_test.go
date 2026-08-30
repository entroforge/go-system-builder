package transition_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestStartReviewRoundRejectsExhaustedS7Budget(t *testing.T) {
	action, ok := transition.LookupAction("start_review_round")
	if !ok {
		t.Fatal("start_review_round action is not registered")
	}
	state := map[string]any{
		"review": map[string]any{"round": float64(5), "clean_round": nil},
		"configuration": map[string]any{
			"repair": map[string]any{"max_full_review_rounds": float64(5)},
		},
	}

	result, err := action(state, &transition.ActionContext{Spec: transition.TransitionSpec{ID: "TR-012"}})
	if err == nil {
		t.Fatal("expected an exhausted S7 budget to reject a new review round")
	}
	if result.Status != "failed" {
		t.Fatalf("action status = %q, want failed", result.Status)
	}
	if state["review"].(map[string]any)["round"] != float64(5) {
		t.Fatal("rejected review round must not mutate review.round")
	}
}

func TestStartReviewRoundRecordsTR012RepairBaselineReference(t *testing.T) {
	action, ok := transition.LookupAction("start_review_round")
	if !ok {
		t.Fatal("start_review_round action is not registered")
	}
	state := map[string]any{
		"review":   map[string]any{"round": float64(1), "clean_round": nil},
		"baseline": map[string]any{"generation": float64(3)},
		"configuration": map[string]any{
			"repair": map[string]any{"max_full_review_rounds": float64(5)},
		},
	}

	result, err := action(state, &transition.ActionContext{
		Spec:     transition.TransitionSpec{ID: "TR-012"},
		Evidence: map[string]string{"change_impact_record": "ev-impact-1"},
	})
	if err != nil {
		t.Fatalf("start_review_round(TR-012): %v", err)
	}
	if result.Status != "committed" {
		t.Fatalf("action status = %q, want committed", result.Status)
	}
	entry := state["review"].(map[string]any)["round_entry"].(map[string]any)
	if entry["transition_id"] != "TR-012" || entry["change_impact_ref"] != "ev-impact-1" {
		t.Fatalf("round_entry = %#v, want TR-012 and ev-impact-1", entry)
	}
	if entry["round"] != 2 || entry["baseline_generation"] != 3 {
		t.Fatalf("round_entry coordinates = %#v, want round 2 generation 3", entry)
	}
}
