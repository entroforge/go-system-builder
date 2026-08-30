package assignment

import (
	"fmt"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/review"
)

// bindReviewPlanAssignments binds a reviewer workgroup's manifest
// assignments to the registered ReviewPlan (L3-S7 §3.4/§8 Assignment
// generator): every manifest assignment must name a planned plan assignment
// of the same lens, carry its exact Claim set, and respect the static ->
// behavior wave gate. On success the runtime projection records the
// dispatched Agent and flips the covered Claims to running.
//
// When a manifest assignment shares a resource_lock with an already-running
// assignment (status=dispatched, not result_submitted / consumed), the
// second arrival is NOT rejected and the round is NOT trimmed —
// dispatch_capacity_policy=coverage_complete forbids either response. The
// conflict is recorded as queue_reason="resource_lock:<keys>" and the
// assignment stays planned; once the holder releases the lock (Result
// consumed), a subsequent register-workgroup call re-evaluates and
// dispatches it (L3-S7 §4.5 + L4 §6.2).
//
// Runs inside the register-workgroup CAS so a plan revision and a dispatch
// never interleave.
func bindReviewPlanAssignments(root string, state map[string]any, value manifest) error {
	kind := value.WorkgroupKind
	if kind != "delivery_verifier" && kind != "qa" && kind != "e2e_browser" {
		return nil
	}
	reviewMap, _ := state["review"].(map[string]any)
	if reviewMap == nil {
		return fmt.Errorf("runtime review section missing; register the ReviewPlan via `runtime review-plan` first")
	}
	plan, ptr, err := review.LoadPlan(root, state)
	if err != nil {
		return err
	}
	if ptr.Status != "running" && ptr.Status != "cannot_clean" && ptr.Status != "discovery_draining" {
		return fmt.Errorf("ReviewPlan %s is %s; reviewer dispatch requires a running or draining round", ptr.PlanID, ptr.Status)
	}
	assignmentsProjection, _ := reviewMap["assignments"].(map[string]any)
	claimsProjection, _ := reviewMap["claims"].(map[string]any)
	if assignmentsProjection == nil || claimsProjection == nil {
		return fmt.Errorf("runtime review projection missing; re-register the ReviewPlan")
	}

	staticSettled := review.StaticClaimsSettled(state, plan)
	for _, item := range value.Assignments {
		row, _ := assignmentsProjection[item.AssignmentID].(map[string]any)
		if row == nil {
			return fmt.Errorf("assignment %s is not part of ReviewPlan %s (known: %s)", item.AssignmentID, ptr.PlanID, strings.Join(knownAssignmentIDs(assignmentsProjection), ", "))
		}
		lens, _ := row["lens"].(string)
		if review.LensToWorkgroupKind(lens) != kind {
			return fmt.Errorf("assignment %s belongs to lens %s but the workgroup kind is %s; a workgroup never mixes lenses (L3-S7 §3.4)", item.AssignmentID, lens, kind)
		}
		status, _ := row["status"].(string)
		if status != "planned" && status != "blocked" {
			return fmt.Errorf("assignment %s is already %s; one plan Assignment dispatches once", item.AssignmentID, status)
		}
		if status == "blocked" && !agentHasBlockerResolution(state, mapString(row["agent_id"])) {
			return fmt.Errorf("assignment %s is blocked and cannot be dispatched until its Agent records the canonical blocker_resolved event", item.AssignmentID)
		}
		if len(item.ClaimIDs) == 0 {
			return fmt.Errorf("assignment %s carries no claim_ids; the manifest must bind the exact Claim set from the ReviewPlan", item.AssignmentID)
		}
		if err := exactClaimSet(row["claim_ids"], item.ClaimIDs); err != nil {
			return fmt.Errorf("assignment %s claim set mismatch: %w", item.AssignmentID, err)
		}
		planAssignment := findPlanAssignmentByID(plan, item.AssignmentID)
		if planAssignment != nil && planAssignment.ExecutionWave == "behavior" && !staticSettled {
			return fmt.Errorf("assignment %s is a behavior-wave (E2E/specialty) Assignment but required static Claims are not all dispositioned yet; behavior E2E unlocks only after the static set settles (L3-S7 §5.2-5.3)", item.AssignmentID)
		}
		if planAssignment != nil {
			if err := review.DependenciesSettled(state, plan, planAssignment); err != nil {
				return err
			}
		}
		// Resource-lock conflict: another running assignment already
		// holds the keys this assignment declares. Per L3-S7 §4.5 the
		// round may queue but never trim coverage, so we record the
		// conflict and stay planned. queue_reason names the held keys
		// so the next register-workgroup attempt sees exactly why.
		if conflict := findResourceLockConflict(item.AssignmentID, row, assignmentsProjection); conflict != "" {
			row["status"] = "planned"
			row["queued_agent_id"] = item.AgentID
			row["queue_reason"] = "resource_lock:" + conflict
			// Claims stay planned; the assignment is on the queue, not
			// running, so the round consumer does not expect a Result.
			continue
		}
		row["status"] = "dispatched"
		row["queue_reason"] = nil
		row["agent_id"] = item.AgentID
		row["queued_agent_id"] = nil
		row["blocker_ref"] = nil
		row["blocked_at"] = nil
		for _, claimID := range item.ClaimIDs {
			if claimRow, _ := claimsProjection[claimID].(map[string]any); claimRow != nil {
				claimRow["disposition"] = "running"
			}
		}
	}
	return nil
}

func agentHasBlockerResolution(state map[string]any, agentID string) bool {
	if agentID == "" {
		return false
	}
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if mapString(agent["id"]) == agentID && mapString(agent["blocker_resolved_ref"]) != "" {
			return true
		}
	}
	return false
}

func mapString(value any) string {
	valueString, _ := value.(string)
	return valueString
}

// findResourceLockConflict returns the sorted, comma-joined set of locks
// the candidate assignment shares with any currently-running sibling
// assignment. Returns "" when no conflict exists. "Running" means the
// sibling has been dispatched but not yet consumed — exactly the window
// during which the shared resource is actually held.
func findResourceLockConflict(candidateID string, candidateRow map[string]any, projection map[string]any) string {
	want := readLockSet(candidateRow["resource_locks"])
	if len(want) == 0 {
		return ""
	}
	conflicts := map[string]bool{}
	for id, raw := range projection {
		if id == candidateID {
			continue
		}
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if !isLockHeldBy(row) {
			continue
		}
		have := readLockSet(row["resource_locks"])
		for lock := range have {
			if want[lock] {
				conflicts[lock] = true
			}
		}
	}
	if len(conflicts) == 0 {
		return ""
	}
	out := make([]string, 0, len(conflicts))
	for lock := range conflicts {
		out = append(out, lock)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// isLockHeldBy reports whether the projection row is currently holding its
// declared locks — the assignment was dispatched but has not yet had its
// Result consumed. consumed / result_submitted rows release the locks.
func isLockHeldBy(row map[string]any) bool {
	status, _ := row["status"].(string)
	switch status {
	case "dispatched":
		// dispatched but no Result yet = actively holding the lock.
		return row["result_ref"] == nil
	case "result_submitted":
		// submitted but not consumed — still holding until the consumer
		// flips status to consumed; freeing earlier would race the
		// coverage diff.
		return true
	default:
		return false
	}
}

// readLockSet converts the projection's resource_locks ([]any of string) to
// a set. Tolerant of missing or malformed fields.
func readLockSet(raw any) map[string]bool {
	out := map[string]bool{}
	items, _ := raw.([]any)
	for _, item := range items {
		if value, ok := item.(string); ok {
			value = strings.TrimSpace(value)
			if value != "" {
				out[value] = true
			}
		}
	}
	return out
}

func knownAssignmentIDs(projection map[string]any) []string {
	ids := make([]string, 0, len(projection))
	for id := range projection {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func exactClaimSet(planValue any, manifestIDs []string) error {
	planIDs := map[string]bool{}
	if raw, ok := planValue.([]any); ok {
		for _, value := range raw {
			if id, _ := value.(string); id != "" {
				planIDs[id] = true
			}
		}
	}
	manifestSet := map[string]bool{}
	for _, id := range manifestIDs {
		manifestSet[id] = true
	}
	for id := range manifestSet {
		if !planIDs[id] {
			return fmt.Errorf("claim %s is not part of the plan assignment", id)
		}
	}
	for id := range planIDs {
		if !manifestSet[id] {
			return fmt.Errorf("plan claim %s is missing from the manifest assignment", id)
		}
	}
	return nil
}

func findPlanAssignmentByID(plan *review.Plan, assignmentID string) *review.PlanAssignment {
	for i := range plan.Assignments {
		if plan.Assignments[i].AssignmentID == assignmentID {
			return &plan.Assignments[i]
		}
	}
	return nil
}
