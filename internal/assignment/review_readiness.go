package assignment

import (
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/review"
)

// ReviewDispatchPrerequisite is the read-only dependency/resource decision
// shared by actual binding and Main guidance. A resource conflict queues the
// original binding; a dependency failure does not admit it. This does not
// replace binding's owner, lens, exact Claim set or approval checks.
func ReviewDispatchPrerequisite(state map[string]any, plan *review.Plan, assignmentID string) (lockConflict string, err error) {
	if plan == nil {
		return "", fmt.Errorf("restore the current ReviewPlan before selecting review work")
	}
	return reviewDispatchPrerequisite(state, plan, assignmentID, review.StaticClaimsSettled(state, plan))
}

func reviewDispatchPrerequisite(state map[string]any, plan *review.Plan, assignmentID string, staticSettled bool) (lockConflict string, err error) {
	item := findPlanAssignmentByID(plan, assignmentID)
	if item == nil {
		return "", fmt.Errorf("assignment %s is not part of the current ReviewPlan", assignmentID)
	}
	if item.ExecutionWave == "behavior" && !staticSettled {
		return "", fmt.Errorf("assignment %s is a behavior-wave (E2E/specialty) Assignment but required static Claims are not all dispositioned yet; behavior E2E unlocks only after the static set settles (L3-S7 §5.2-5.3)", assignmentID)
	}
	if err := review.DependenciesSettled(state, plan, item); err != nil {
		return "", err
	}
	reviewMap, _ := state["review"].(map[string]any)
	rows, _ := reviewMap["assignments"].(map[string]any)
	row, _ := rows[assignmentID].(map[string]any)
	if row == nil {
		return "", fmt.Errorf("assignment %s is missing its original Runtime projection", assignmentID)
	}
	return findResourceLockConflict(assignmentID, row, rows), nil
}

// ReviewRoundOpenForDispatch checks the original cursor and plan projection.
// Missing metadata is not proof that a stale or inherited queue is ready.
func ReviewRoundOpenForDispatch(state map[string]any) bool {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "verification" || state["pause"] != nil {
		return false
	}
	if phase, _ := lifecycle["phase"].(string); phase != "" && phase != "running" && phase != "cannot_clean" && phase != "discovery_draining" {
		return false
	}
	ptr := review.PlanPointerFromState(state)
	if ptr == nil || (ptr.Status != "running" && ptr.Status != "cannot_clean" && ptr.Status != "discovery_draining") {
		return false
	}
	reviewMap, _ := state["review"].(map[string]any)
	return ptr.ReviewRound > 0 && ptr.ReviewRound == reviewRoundNumber(reviewMap["round"])
}

func reviewRoundNumber(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		value, _ := number.Int64()
		return int(value)
	default:
		return 0
	}
}
