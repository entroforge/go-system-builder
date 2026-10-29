package review

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workstate"
)

// An original TR-012 seed is machine input, not the Planner's first accepted
// professional plan. Only that exact untouched seed can enter RegisterPlan
// once. Discovery revisions keep their existing workspace/budget constraints.
func canRegisterS9Seed(root string, state map[string]any, next *Plan) (bool, error) {
	ptr := PlanPointerFromState(state)
	review, _ := state["review"].(map[string]any)
	entry, _ := review["round_entry"].(map[string]any)
	if ptr == nil || entry["transition_id"] != "TR-012" || ptr.Revision != 1 || ptr.Status != "running" || entry["review_plan_seed_ref"] != ptr.Path || entry["review_plan_seed_sha256"] != ptr.SHA256 {
		return false, nil
	}
	current, _, err := LoadPlan(root, state)
	if err != nil {
		return false, fmt.Errorf("read original S9 seed: %w", err)
	}
	if current.CreatedBy != "s9-handoff" || intField(entry["round"]) != current.ReviewRound || intField(entry["baseline_generation"]) != current.BaselineGeneration {
		return false, nil
	}
	unresolved := false
	for _, claim := range current.Claims {
		unresolved = unresolved || strings.Contains(claim.Assertion, "PLANNER-REFINE") || strings.Contains(claim.Oracle, "PLANNER-REFINE") || strings.Contains(claim.Method, "PLANNER-REFINE")
	}
	if !unresolved {
		return false, nil
	}
	if next.ReviewPlanID != current.ReviewPlanID || next.ReviewRound != current.ReviewRound || next.BaselineGeneration != current.BaselineGeneration || !reflect.DeepEqual(next.ProductBaseline, current.ProductBaseline) {
		return false, fmt.Errorf("first S9 seed registration keeps the original plan id, round, generation and pinned product baseline")
	}
	assignments, _ := review["assignments"].(map[string]any)
	if len(assignments) != len(current.Assignments) {
		return false, fmt.Errorf("original S9 seed Assignment projection is incomplete")
	}
	for _, assignment := range current.Assignments {
		id := assignment.AssignmentID
		raw, found := assignments[id]
		if !found {
			return false, fmt.Errorf("original S9 seed Assignment %s is missing", id)
		}
		row, _ := raw.(map[string]any)
		if workstate.ReviewAssignmentActivity(row) != "planned" {
			return false, fmt.Errorf("S9 seed Assignment %s has already started; preserve its original responsibility", id)
		}
		for _, key := range []string{"agent_id", "queued_agent_id", "result_ref", "result_receipt", "blocker_ref"} {
			if value := row[key]; value != nil && value != "" {
				return false, fmt.Errorf("S9 seed Assignment %s already has %s; preserve its original responsibility", id, key)
			}
		}
	}
	return true, nil
}
