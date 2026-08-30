package transition_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestS7BudgetGovernanceReturnIsDeclaredHumanGateway(t *testing.T) {
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}
	var found bool
	for _, global := range catalog.GlobalTransitions {
		if global.ID != "GTR-006" {
			continue
		}
		found = true
		if global.Event != "human_s7_governance_return" || global.To != "planning" {
			t.Fatalf("GTR-006 route = event %q to %q, want human_s7_governance_return -> planning", global.Event, global.To)
		}
		if global.Automation == nil || global.Automation.Eligible || !global.Automation.HumanBoundary {
			t.Fatalf("GTR-006 must be a human-only gateway, got %#v", global.Automation)
		}
		if global.HumanDecisionScope != "runtime_governance" {
			t.Fatalf("GTR-006 human decision scope = %q, want runtime_governance", global.HumanDecisionScope)
		}
		if len(global.RequiredEvidence) != 1 || global.RequiredEvidence[0] != "human_decision_record" {
			t.Fatalf("GTR-006 required evidence = %v, want human_decision_record", global.RequiredEvidence)
		}
	}
	if !found {
		t.Fatal("GTR-006 human S7 governance return is not declared")
	}
}

func TestS7GovernanceReturnResetsReviewProjectionAndPreservesDecision(t *testing.T) {
	action, ok := transition.LookupAction("reset_s7_review_after_governance")
	if !ok {
		t.Fatal("reset_s7_review_after_governance action is not registered")
	}
	state := map[string]any{
		"review": map[string]any{
			"round": 5, "clean_round": 5,
			"plan":              map[string]any{"plan_id": "plan-r5"},
			"claims":            map[string]any{"claim-1": map[string]any{}},
			"assignments":       map[string]any{"assignment-1": map[string]any{}},
			"observation_batch": map[string]any{"batch_id": "batch-r5"},
		},
		"evidence": []any{
			map[string]any{"id": "decision-1", "kind": "human_decision", "status": "valid"},
			map[string]any{"id": "review-1", "kind": "review_result", "status": "valid"},
		},
	}
	result, err := action(state, &transition.ActionContext{
		Spec:     transition.TransitionSpec{ID: "GTR-006"},
		Evidence: map[string]string{"human_decision_record": "decision-1"},
	})
	if err != nil {
		t.Fatalf("governance reset failed: %v", err)
	}
	if result.Status != "committed" {
		t.Fatalf("action status = %q, want committed", result.Status)
	}
	review := state["review"].(map[string]any)
	if review["round"] != 0 || review["clean_round"] != nil || review["plan"] != nil || review["observation_batch"] != nil {
		t.Fatalf("review projection was not reset: %#v", review)
	}
	if len(review["claims"].(map[string]any)) != 0 || len(review["assignments"].(map[string]any)) != 0 {
		t.Fatalf("review work rows were not cleared: %#v", review)
	}
	if state["evidence"].([]any)[0].(map[string]any)["status"] != "valid" || state["evidence"].([]any)[1].(map[string]any)["status"] != "invalid" {
		t.Fatalf("decision/review evidence statuses = %#v", state["evidence"])
	}
}
