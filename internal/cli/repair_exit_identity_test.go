package cli

import "testing"

func TestRepairS7EntryUsesPlanIdentityWithoutHistoricalRound(t *testing.T) {
	state := map[string]any{
		"lifecycle": map[string]any{"state": "verification"},
		"baseline":  map[string]any{"generation": 1},
		"review": map[string]any{
			"round":       7,
			"round_entry": map[string]any{"transition_id": "TR-012", "plan_id": "plan-new", "baseline_generation": 1, "review_plan_seed_ref": "seed.json", "repair_handoff_ref": "handoff.json"},
			"repair":      map[string]any{"review_plan_seed_ref": "seed.json", "handoff_ref": "handoff.json"},
		},
	}
	if !repairCurrentS7Entry(state) {
		t.Fatal("new unnumbered TR-012 entry not recognized")
	}
	if !repairS7PlanIdentityCurrent(state, "plan-new") {
		t.Fatal("same original Plan not recognized")
	}
	if repairS7PlanIdentityCurrent(state, "plan-old") {
		t.Fatal("historical number matched different original Plan")
	}
	review := state["review"].(map[string]any)
	review["round_entry"] = map[string]any{"transition_id": "TR-012", "round": 7, "baseline_generation": 1, "review_plan_seed_ref": "seed.json", "repair_handoff_ref": "handoff.json"}
	if repairCurrentS7Entry(state) {
		t.Fatal("missing identity was treated as round zero")
	}
}
