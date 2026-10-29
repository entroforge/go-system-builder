package hook

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Rule IDs for the Main Stop gate. Main Stop is intentionally separate from
// the Worker stop/idle rules because it checks the whole review queue rather
// than one agent's delivery contract.
const (
	RuleMainStopPendingDispatch  = "main_stop_pending_dispatch"
	RuleMainStopUnconsumedResult = "main_stop_unconsumed_result"
)

// MainStopDecision prevents the orchestrator from ending a turn while the
// review control plane still has a responsibility that can be acted on.
// Runtime read failures fail open: the gate must not turn a missing or
// corrupted runtime into an unexitable user session.
func MainStopDecision(root string, input policy.Input) (policy.Decision, bool) {
	if input.Event != "Stop" || input.StopHookActive {
		return policy.Decision{}, false
	}
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	snapshot, err := runtime.NewStore(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		return policy.Decision{}, false
	}
	var plan *review.Plan
	if assignment.ReviewRoundOpenForDispatch(snapshot.State) {
		plan, _, _ = review.LoadPlan(root, snapshot.State)
	}
	return mainStopDecisionForState(snapshot.State, input, plan)
}

func mainStopDecisionForState(state map[string]any, input policy.Input, plans ...*review.Plan) (policy.Decision, bool) {
	if input.Event != "Stop" || input.StopHookActive || state == nil {
		return policy.Decision{}, false
	}
	if decision, blocked := planningStopDecision(state); blocked {
		return decision, true
	}
	if !assignment.ReviewRoundOpenForDispatch(state) {
		return policy.Decision{}, false
	}
	var plan *review.Plan
	if len(plans) > 0 {
		plan = plans[0]
	}

	review, ok := state["review"].(map[string]any)
	if !ok {
		return policy.Decision{}, false
	}
	assignments, ok := review["assignments"].(map[string]any)
	if !ok || len(assignments) == 0 {
		return policy.Decision{}, false
	}

	ids := make([]string, 0, len(assignments))
	for id := range assignments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		row, ok := assignments[id].(map[string]any)
		if !ok {
			continue
		}
		version := stringValue(state["schema_version"])
		if pendingDispatch(id, row, version) && plan != nil {
			if locks, err := assignment.ReviewDispatchPrerequisite(state, plan, id); err == nil && locks == "" {
				return mainStopPendingDispatch(id), true
			}
		}
		if unconsumedResult(id, row, version) {
			return mainStopUnconsumedResult(id), true
		}
	}
	return policy.Decision{}, false
}

func pendingDispatch(id string, row map[string]any, versions ...string) bool {
	if workstate.ReviewAssignmentActivity(row, versions...) != "planned" {
		return false
	}
	return strings.TrimSpace(stringValue(row["agent_id"])) == "" &&
		strings.TrimSpace(stringValue(row["queued_agent_id"])) == "" &&
		strings.TrimSpace(stringValue(row["result_ref"])) == ""
}

func unconsumedResult(id string, row map[string]any, versions ...string) bool {
	activity := workstate.ReviewAssignmentActivity(row, versions...)
	if activity == "consumed" {
		return false
	}
	if strings.TrimSpace(stringValue(row["result_ref"])) != "" {
		return true
	}
	return activity == "result_submitted"
}

func mainStopPendingDispatch(id string) policy.Decision {
	return policy.Decision{
		Decision:       "deny",
		RuleID:         RuleMainStopPendingDispatch,
		Reason:         fmt.Sprintf("Main cannot finish while ReviewPlan assignment %s is ready but has not been dispatched", id),
		Recovery:       []string{fmt.Sprintf("continue the current S7 preparation and dispatch ready assignment %s with its exact view/answer references; preserve waiting assignments", id), "after dispatch, let the next Stop check re-evaluate the remaining ReviewPlan work"},
		Retry:          policy.RetryAfterRecoveryValidation,
		MatchedRuleIDs: []string{RuleMainStopPendingDispatch},
	}
}

func mainStopUnconsumedResult(id string) policy.Decision {
	return policy.Decision{
		Decision:       "deny",
		RuleID:         RuleMainStopUnconsumedResult,
		Reason:         fmt.Sprintf("Main cannot finish while ReviewPlan assignment %s has a submitted Result that is not consumed", id),
		Recovery:       []string{"consume the submitted Result and reconcile its Claim/Finding projection", "after consumption, let the next Stop check re-evaluate the remaining ReviewPlan work"},
		Retry:          policy.RetryAfterRecoveryValidation,
		MatchedRuleIDs: []string{RuleMainStopUnconsumedResult},
	}
}

func stringValue(value any) string {
	valueString, _ := value.(string)
	return valueString
}

// Only S3/S4 have no normal design sign-off. Other stages retain their
// domain-specific stop rules; absence of a gateway is not proof of readiness.
func planningStopDecision(state map[string]any) (policy.Decision, bool) {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	phase := stringValue(lifecycle["phase"])
	if stringValue(lifecycle["state"]) != "planning" || (phase != "contracts" && phase != "tasks") || state["pause"] != nil {
		return policy.Decision{}, false
	}
	req, _ := state["bound_req"].(map[string]any)
	if stringValue(req["id"]) == "" || stringValue(req["status"]) != "locked" {
		return policy.Decision{}, false
	}
	milestone, _ := state["milestone"].(map[string]any)
	human, known := milestone["human_required"].(bool)
	blocked, knownBlocked := milestone["blocked"].(bool)
	if !known || !knownBlocked || human || blocked || stringValue(milestone["lifecycle_phase"]) != phase {
		return policy.Decision{}, false
	}
	if blockers, ok := state["blockers"].([]any); ok && len(blockers) > 0 {
		return policy.Decision{}, false
	}
	entities, _ := state["entities"].(map[string]any)
	// Do not turn a delegated assignment into main-session self-execution.
	if agents, ok := entities["agents"].([]any); ok && len(agents) > 0 {
		return policy.Decision{}, false
	}
	const rule = "main_stop_planning_continuation"
	return policy.Decision{
		Decision: "deny", RuleID: rule, MatchedRuleIDs: []string{rule},
		Reason:   "Current planning stage still has work. Stage handoff or workload size does not require another user approval.",
		Recovery: []string{"Continue the current S3/S4 responsibility using its exact guidance. If the professional answer is already accepted, follow its named recovery instead of creating another delivery. Use status/next when needed to diagnose a concrete gap; repeated unchanged queries do not advance the stage.", "If the user explicitly requested a stop, or an actual approval/external wait is required, explain that fact and end. This is one bounded reminder: stop_hook_active allows the next Stop, including repeated failure or no progress."},
		Retry:    policy.RetryAfterRecoveryValidation,
	}, true
}
