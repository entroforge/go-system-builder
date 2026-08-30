package hook

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
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
	return mainStopDecisionForState(snapshot.State, input)
}

func mainStopDecisionForState(state map[string]any, input policy.Input) (policy.Decision, bool) {
	if input.Event != "Stop" || input.StopHookActive || state == nil {
		return policy.Decision{}, false
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
		if pendingDispatch(id, row) {
			return mainStopPendingDispatch(id), true
		}
		if unconsumedResult(id, row) {
			return mainStopUnconsumedResult(id), true
		}
	}
	return policy.Decision{}, false
}

func pendingDispatch(id string, row map[string]any) bool {
	status := stringValue(row["status"])
	if status != "planned" && status != "queued" && status != "ready" {
		return false
	}
	return strings.TrimSpace(stringValue(row["agent_id"])) == "" &&
		strings.TrimSpace(stringValue(row["result_ref"])) == ""
}

func unconsumedResult(id string, row map[string]any) bool {
	if stringValue(row["status"]) == "consumed" {
		return false
	}
	if strings.TrimSpace(stringValue(row["result_ref"])) != "" {
		return true
	}
	switch stringValue(row["status"]) {
	case "result_submitted", "submitted", "awaiting_consumption":
		return true
	default:
		return false
	}
}

func mainStopPendingDispatch(id string) policy.Decision {
	return policy.Decision{
		Decision:       "deny",
		RuleID:         RuleMainStopPendingDispatch,
		Reason:         fmt.Sprintf("Main cannot finish while ReviewPlan assignment %s is ready but has not been dispatched", id),
		Recovery:       []string{"continue DRIVE and dispatch the highest-priority queued assignment", "after dispatch, let the next Stop check re-evaluate the remaining ReviewPlan work"},
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
