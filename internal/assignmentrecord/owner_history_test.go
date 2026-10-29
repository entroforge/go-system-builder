package assignmentrecord

import "testing"

func TestSupersededOwnersPreservesCurrentCharterAndFiltersHistoricalOwners(t *testing.T) {
	first := recordTestRecord()
	second := first
	second.Revision = 2
	second.OwnerAgentID = "agent-revision-successor"
	state := map[string]any{
		"runtime_id": first.RuntimeID, "revision": 20,
		"baseline": map[string]any{"generation": first.BaselineGeneration},
		"entities": map[string]any{"assignments": []Record{first, second}},
	}
	retired, err := SupersededOwners(state)
	if err != nil || !retired[first.OwnerAgentID] || retired[second.OwnerAgentID] {
		t.Fatalf("current owners: %v %v", retired, err)
	}
	// A new baseline has its own r1; older owners are history, not authority.
	third := first
	third.BaselineGeneration++
	third.OwnerAgentID = "agent-new-baseline"
	state["baseline"] = map[string]any{"generation": third.BaselineGeneration}
	state["entities"].(map[string]any)["assignments"] = []Record{first, second, third}
	retired, err = SupersededOwners(state)
	if err != nil || !retired[first.OwnerAgentID] || !retired[second.OwnerAgentID] || retired[third.OwnerAgentID] {
		t.Fatalf("baseline owners: %v %v", retired, err)
	}
	state["entities"].(map[string]any)["assignments"] = []Record{first, third}
	state["baseline"] = map[string]any{"generation": first.BaselineGeneration}
	retired, err = SupersededOwners(state)
	if err != nil || retired[first.OwnerAgentID] {
		t.Fatalf("first revision remains current: %v %v", retired, err)
	}
}
