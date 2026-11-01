package transition_test

import (
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestStartReviewEntryUsesOriginalEventWithoutCountingOrBudget(t *testing.T) {
	for _, id := range []string{"TR-006", "TR-012", "TR-016", "TR-022", "TR-031"} {
		t.Run(id, func(t *testing.T) {
			action, _ := transition.LookupAction("start_review_round")
			state := map[string]any{"runtime_id": "loop-test", "baseline": map[string]any{"generation": 3}, "review": map[string]any{"round": 5, "clean_round": "old"}, "configuration": map[string]any{"repair": map[string]any{"max_full_review_rounds": 1}}}
			ctx := &transition.ActionContext{Spec: transition.TransitionSpec{ID: id}, EventID: "evt-original", Evidence: map[string]string{"change_impact_record": "impact"}}
			result, err := action(state, ctx)
			if err != nil || result.Status != "committed" {
				t.Fatalf("entry: %#v %v", result, err)
			}
			rev := state["review"].(map[string]any)
			entry := rev["round_entry"].(map[string]any)
			if entry["plan_id"] != reviewmeta.NewPlanID("loop-test", "evt-original") || entry["baseline_generation"] != 3 {
				t.Fatalf("entry=%#v", entry)
			}
			if _, ok := entry["round"]; ok {
				t.Fatal("new entry generated a round")
			}
			if rev["round"] != 5 || rev["clean_round"] != nil {
				t.Fatalf("review=%#v", rev)
			}
			rev["clean_round"] = "later-result"
			before := map[string]any{}
			for k, v := range entry {
				before[k] = v
			}
			result, err = action(state, ctx)
			if err != nil || result.Status != "skipped" || !reflect.DeepEqual(entry, before) || rev["clean_round"] != "later-result" {
				t.Fatalf("replay changed committed work: %#v %v", result, err)
			}
			ctx.EventID = "evt-another-entry"
			if _, err := action(state, ctx); err != nil {
				t.Fatal(err)
			}
			if rev["round_entry"].(map[string]any)["plan_id"] == entry["plan_id"] {
				t.Fatal("new entry reused old identity")
			}
		})
	}
}

func TestStartReviewEntryNeedsOriginalEventBeforeMutation(t *testing.T) {
	action, _ := transition.LookupAction("start_review_round")
	rev := map[string]any{"round": 2, "clean_round": "keep"}
	_, err := action(map[string]any{"runtime_id": "runtime", "review": rev}, &transition.ActionContext{})
	if err == nil || rev["clean_round"] != "keep" || rev["round"] != 2 {
		t.Fatalf("missing original event: %v %#v", err, rev)
	}
}
