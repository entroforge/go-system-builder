// stopidle.go — real platform control for TeammateIdle / SubagentStop
// (L4 §7.4, §15.2 P0-2/P0-3, §16.1).
//
// Before this file the adapter answered these lifecycle events with a
// systemMessage + exit 0 no matter what the scheduling decision was, so a
// teammate that went idle before its Result — or a Sub-agent that stopped
// with the plan as its final response — was never actually continued by the
// platform. Claude Code 2.1.218 semantics: exit code 2 blocks the idle/stop
// and stderr is fed back to that same agent.
//
// The verdict is a minimal determination over facts the runtime already
// owns (agent state, plan checkpoint, completion/blocker report on the
// assignment, and an S7 ReviewResult receipt). Hook context and receipt
// validation share one original Runtime snapshot. Fail-open contract: when
// the payload identifies no known agent, or the runtime cannot be read, the
// event is allowed — never fabricate an agent binding.
package hook

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// Rule IDs for the stop/idle control path. They live beside the policy
// rules so the audit outbox attributes the block to a stable identifier.
const (
	RuleTeammateIdleResumeAssignment = "teammate_idle_resume_assignment"
	RuleSubagentStopMissingResult    = "subagent_stop_missing_result"
)

// IsStopIdleEvent reports whether the event supports the exit-2 platform
// control implemented here.
func IsStopIdleEvent(event string) bool {
	return event == "TeammateIdle" || event == "SubagentStop"
}

// StopIdleDecision decides whether a TeammateIdle/SubagentStop may proceed.
// A block Decision means the transport must exit 2 with the feedback on
// stderr so the platform continues the same agent.
//
// Matrix (L4 §7.4 / §16.1):
//
//   - stop_hook_active                       → allow (official loop guard:
//     the agent is already continuing because of a previous stop hook)
//   - agent not identifiable / not in runtime → allow (fail-open)
//   - one_shot non-S7 dispatch                → allow (the final message IS
//     the result; nothing is registered in the runtime); an S7 Reviewer must
//     satisfy the current Result or real blocker checks
//   - plan_approval_required non-S7 dispatch → allow (L4 §3.3: this mode
//     routes through `understanding_approved`, not through the
//     PLAN_REPORT checkpoint — the stop/idle gate is not the authority
//     that decides whether the plan has been approved); S7 Reviewers still
//     need their Canonical ReviewResult
//   - terminal Agent state without original evidence → block until a
//     current consumer or blocker proof allows the original stop
//   - valid non-S7 blocker                    → allow (Main consumes it)
//   - S7 Reviewer blocker                     → allow only for the original
//     work_blocked event or current site_lost transaction
//   - completion report on a non-S7 assignment → allow (awaiting consumer;
//     no automatic next-task claim here)
//   - generic S7 Reviewer completion report    → block until the exact
//     current Canonical ReviewResult is consumed by the round consumer
//   - no PLAN_REPORT checkpoint on generic work → block: send the plan first
//   - plan but no Result                      → block: the plan is not the
//     deliverable; continue the current assignment
func StopIdleDecision(root string, input policy.Input) (policy.Decision, bool) {
	if !IsStopIdleEvent(input.Event) {
		return policy.Decision{}, false
	}
	if input.Event == "SubagentStop" && input.StopHookActive {
		return policy.Decision{}, false
	}
	agentID := input.EffectiveAgentID()
	if agentID == "" {
		return policy.Decision{}, false
	}
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	snapshot, err := loopruntime.NewStore(
		filepath.Join(root, ".claude", "loop-state.json"),
		filepath.Join(root, ".claude", "loop-events.jsonl"),
	).Snapshot()
	if err != nil {
		return policy.Decision{}, false
	}
	assignmentID, reviewBound := ReviewAssignmentBinding(snapshot.State, agentID)
	hypothesisAssignmentID, hypothesisBound := HypothesisResultAssignmentBinding(root, snapshot.State, agentID)
	repairAssignmentID, repairBound := RepairResultAssignmentBinding(root, snapshot.State, agentID)
	loaded, err := hookctx.FromSnapshot(root, agentID, snapshot)
	if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil {
		if repairBound && !reviewBound && !hypothesisBound {
			if repairAssignmentID == "" {
				// The S9 Assignment↔Builder binding itself cannot be determined.
				return repairResultAssignmentBindingNotice(input.Event, agentID, repairAssignmentID), true
			}
			// The binding is known; only the retired frozen execution / source-pin
			// proof is missing. That retired gate must not close the return, but
			// the honest fact is still projected and nothing is written
			// (L4-workflow-convergence.md:342, :300; L4-hook-platform-wiring.md:83).
			return repairResultAssignmentUnfinishedNotice(input.Event, agentID, repairAssignmentID), true
		}
		if reviewBound || hypothesisBound {
			if hypothesisBound && !reviewBound {
				return hypothesisResultAssignmentBindingNotice(input.Event, agentID, hypothesisAssignmentID), true
			}
			return reviewAssignmentBindingNotice(input.Event, agentID, assignmentID), true
		}
		return policy.Decision{}, false
	}
	agent := loaded.PolicyContext.Agent
	if reviewBound && (assignmentID == "" || !agent.ReviewAssignment || agent.AssignmentID != assignmentID) {
		return reviewAssignmentBindingNotice(input.Event, agentID, assignmentID), true
	}
	if agent.ReviewAssignment && !reviewBound {
		return reviewAssignmentBindingNotice(input.Event, agentID, agent.AssignmentID), true
	}
	if hypothesisBound && (hypothesisAssignmentID == "" || !agent.HypothesisResultAssignment || agent.AssignmentID != hypothesisAssignmentID) {
		return hypothesisResultAssignmentBindingNotice(input.Event, agentID, hypothesisAssignmentID), true
	}
	if agent.HypothesisResultAssignment && !hypothesisBound {
		return hypothesisResultAssignmentBindingNotice(input.Event, agentID, agent.AssignmentID), true
	}
	// The S9 binding is only "unknown" when the Assignment↔Builder binding
	// itself cannot be determined. A bound Assignment whose old execution /
	// source-pin proof is missing is a known, unfinished responsibility and must
	// not be refused here (L4-workflow-convergence.md:342 drops the old source
	// pin proof and its recovery wiring).
	if repairBound && (repairAssignmentID == "" || agent.RepairAssignmentID != repairAssignmentID) {
		return repairResultAssignmentBindingNotice(input.Event, agentID, repairAssignmentID), true
	}
	if agent.RepairResultAssignment && !repairBound {
		return repairResultAssignmentBindingNotice(input.Event, agentID, agent.RepairAssignmentID), true
	}
	assignment := assignmentForAgent(loaded.Assignments, agentID)
	if agent.ReviewAssignment {
		assignment = assignmentForReviewOwner(loaded.Assignments, agentID, agent.AssignmentID)
		if assignment == nil || assignment.OwnerAgentID != agentID || assignment.AssignmentID != assignmentID {
			return reviewAssignmentBindingNotice(input.Event, agentID, assignmentID), true
		}
	}
	packetReceived := loaded.PolicyContext.PacketMode && submission.CompletedOwner(root, agentID)
	if CurrentResponsibility(root, snapshot, agent, assignment, packetReceived).ExecutionMayEnd {
		return policy.Decision{}, false
	}
	if agent.ReviewAssignment || agent.HypothesisResultAssignment || agent.RepairResultAssignment {
		stage := "S7"
		if agent.HypothesisResultAssignment {
			stage = "S8"
		}
		if agent.RepairResultAssignment {
			stage = "S9"
		}
		return policy.Decision{
			Decision: "deny", RuleID: stopIdleRuleID(input.Event),
			Reason: fmt.Sprintf("%s original responsibility for %s is not complete. Main/system must inspect the original answer and actual pending work; this stop does not establish receipt or completion.", stage, agentID),
			Recovery: []string{
				"continue this original responsibility using its current exact PLAN and Result questionnaire paths",
				"save the professional answer on the issued original questionnaire path; the original consumer records the receipt",
				"if the exact questionnaire context is unavailable, Main must restore this Assignment context; do not invent a path or send duplicate messages",
			},
			Retry: policy.RetryAfterRecoveryValidation,
		}, true
	}
	if agent.RepairAssignmentID != "" {
		// The actor still holds an original native Assignment record (the
		// issued RepairPlanReport binding), so the legacy PLAN_REPORT closing
		// matrix must not close it: the native branch judges the original
		// responsibility, and TeammateIdle is not the native flow's closing
		// wiring (L4-hook-platform-wiring.md:162, L4-hook-anchor-catalog.md:54).
		return policy.Decision{}, false
	}
	decision := stopIdleBlock(input.Event, agent)
	if requiresBuilderReceipt(assignment) {
		decision.Recovery = []string{
			"continue this Builder responsibility using its current exact PLAN and Result questionnaire paths",
			"save the professional PLAN before implementation; after actual work, owned checks and commit, save the Result for the original consumer",
			"if the exact questionnaire context is unavailable, Main must restore this Assignment context; do not invent a path or send duplicate messages",
		}
	}
	return decision, true
}

// ReviewAssignmentBinding selects an S7 ReviewAssignment directly from the
// original Runtime projection. A bound owner with ambiguous rows is still
// known (bound=true, assignmentID="") and must never fall through to a
// legacy completion path.
func ReviewAssignmentBinding(state map[string]any, agentID string) (assignmentID string, bound bool) {
	if state == nil || agentID == "" {
		return "", false
	}
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	for id, raw := range assignments {
		row, _ := raw.(map[string]any)
		if row == nil || reviewProofText(row["agent_id"]) != agentID {
			continue
		}
		if bound {
			return "", true
		}
		bound = true
		assignmentID = id
	}
	return assignmentID, bound
}

// currentCompletionAcknowledgementAccepted validates the current exact
// completion report digest that the original completion_acknowledged Writer
// stored. Builder responsibilities use CurrentBuilderReceipt instead.
func currentCompletionAcknowledgementAccepted(root string, agent *policy.AgentContext, assignment *hookctx.AssignmentContext) bool {
	if agent == nil || assignment == nil || agent.ID == "" || assignment.OwnerAgentID != agent.ID ||
		agent.State != string(workstate.Done) || agent.LifecycleActivity() != "done" ||
		assignment.CompletionAckRef == "" || assignment.CompletionRef == "" || requiresBuilderReceipt(assignment) {
		return false
	}
	_, accepted := assignment.AcknowledgedReports[assignment.CompletionRef]
	return accepted
}

// ReviewResultConsumed proves that a current S7 ReviewAssignment's exact
// Canonical ReviewResult was committed and consumed by the original round
// consumer. The caller supplies the same original Runtime snapshot used to
// build agent; generic completion_report refs and terminal Agent states are
// intentionally not evidence here.
func ReviewResultConsumed(root string, state map[string]any, agent *policy.AgentContext) bool {
	if agent == nil || !agent.ReviewAssignment || agent.ID == "" || agent.AssignmentID == "" || state == nil {
		return false
	}
	boundID, bound := ReviewAssignmentBinding(state, agent.ID)
	if !bound || boundID != agent.AssignmentID {
		return false
	}
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	row, _ := assignments[agent.AssignmentID].(map[string]any)
	return row != nil && reviewProofText(row["agent_id"]) == agent.ID &&
		workstate.ReviewAssignmentActivity(row, reviewProofText(state["schema_version"])) == "consumed" &&
		reviewProofText(row["result_ref"]) != ""
}

// ReviewAssignmentBlocked accepts only a current S7 Reviewer blocker whose
// source is either the original work_blocked Agent event or S7's site-lost
// blocker transaction. Site-lost remains valid while the plan is running or
// draining (`cannot_clean` / `discovery_draining`); manifest status strings
// never authorize blocking.
func ReviewAssignmentBlocked(root string, state map[string]any, agent *policy.AgentContext, assignment *hookctx.AssignmentContext) bool {
	if !reviewAssignmentContextMatches(state, agent, assignment) || !reviewProofDispatchedIdentity(state, agent) {
		return false
	}
	if reviewSiteLostBlocker(root, state, agent) {
		return true
	}
	return reviewWorkBlockedMessage(root, state, agent, assignment)
}

// reviewSiteLostBlocker proves S7's site-lost blocker from its original
// source: the original Assignment row, the original Agent row, the original
// blocker evidence and artifact, and the original Runtime journal event.
// Derived manifest status strings are not the source, and historical digest,
// revision and round fields are read at their original path only — they never
// gate this fact.
func reviewSiteLostBlocker(root string, state map[string]any, agent *policy.AgentContext) bool {
	runtimeID := reviewProofText(state["runtime_id"])
	baseline, _ := state["baseline"].(map[string]any)
	generation := reviewProofInt(baseline["generation"])
	if runtimeID == "" || filepath.Base(runtimeID) != runtimeID || generation < 1 {
		return false
	}
	plan, pointer, err := review.LoadPlan(root, state)
	if err != nil || plan == nil || pointer == nil ||
		!reviewSiteLostRoundAcceptsResult(pointer.Status) ||
		plan.ReviewPlanID == "" || plan.ReviewPlanID != pointer.PlanID {
		return false
	}
	var planAssignment *review.PlanAssignment
	for i := range plan.Assignments {
		if plan.Assignments[i].AssignmentID != agent.AssignmentID {
			continue
		}
		if planAssignment != nil {
			return false
		}
		planAssignment = &plan.Assignments[i]
	}
	if planAssignment == nil || len(planAssignment.ClaimIDs) == 0 {
		return false
	}
	reviewState, _ := state["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	row, _ := assignments[agent.AssignmentID].(map[string]any)
	if row == nil || workstate.ReviewAssignmentActivity(row, reviewProofText(state["schema_version"])) != "blocked" ||
		reviewProofText(row["agent_id"]) != agent.ID || reviewProofText(row["lens"]) != planAssignment.Lens {
		return false
	}
	if reviewProofText(row["result_ref"]) != "" {
		return false
	}
	claimIDs, ok := reviewProofStrings(row["claim_ids"])
	if !ok || !sameReviewProofSet(claimIDs, planAssignment.ClaimIDs) {
		return false
	}
	claims, _ := reviewState["claims"].(map[string]any)
	for _, claimID := range planAssignment.ClaimIDs {
		claim, _ := claims[claimID].(map[string]any)
		if claim == nil || reviewProofText(claim["assignment_id"]) != agent.AssignmentID {
			return false
		}
		switch reviewProofText(claim["disposition"]) {
		case "planned", "running":
		default:
			return false
		}
		if reviewProofText(claim["result_id"]) != "" {
			return false
		}
	}
	blockerRef := reviewProofText(row["blocker_ref"])
	queueReason := reviewProofText(row["queue_reason"])
	blockedAt := reviewProofText(row["blocked_at"])
	if blockerRef == "" || !strings.HasPrefix(queueReason, "site_lost:") || blockedAt == "" {
		return false
	}
	owner, ok := reviewProofAgentRow(state, agent.ID)
	if !ok || reviewProofText(owner["state"]) != "blocked" || reviewProofText(owner["work_blocked_ref"]) != blockerRef ||
		reviewProofText(owner["blocker_resolved_ref"]) != "" || reviewProofText(owner["updated_at"]) != blockedAt {
		return false
	}
	evidence, evidenceCount := reviewProofEvidenceAtPath(state, blockerRef, "review_blocker")
	if evidenceCount != 1 || evidence == nil ||
		!reviewBlockerEvidenceMatchesOriginalSource(evidence, blockerRef, agent.ID, agent.AssignmentID, planAssignment.Lens) {
		return false
	}
	evidenceID := reviewProofText(evidence["id"])
	resultID := strings.TrimPrefix(evidenceID, "review-blocker-")
	if resultID == evidenceID || resultID == "" || filepath.Base(resultID) != resultID {
		return false
	}
	if reviewProofEvidenceIDCount(state, evidenceID) != 1 {
		return false
	}
	if !strings.HasSuffix(filepath.ToSlash(blockerRef), "/review-blockers/"+resultID+"-site-lost.json") {
		return false
	}
	artifactBytes, err := workpacket.ReadFile(root, blockerRef, 1<<20)
	if err != nil {
		return false
	}
	var envelope map[string]any
	if json.Unmarshal(artifactBytes, &envelope) != nil ||
		reviewProofText(envelope["schema_version"]) != "1.0.0" ||
		reviewProofText(envelope["evidence_id"]) != evidenceID ||
		reviewProofText(envelope["kind"]) != "review_blocker" ||
		reviewProofText(envelope["runtime_id"]) != runtimeID ||
		reviewProofText(envelope["assignment_id"]) != agent.AssignmentID ||
		reviewProofText(envelope["producer_agent_id"]) != agent.ID ||
		reviewProofText(envelope["finding_id"]) == "" ||
		reviewProofText(envelope["reason"]) == "" {
		return false
	}
	findingID, reason := reviewProofText(envelope["finding_id"]), reviewProofText(envelope["reason"])
	if queueReason != "site_lost:"+findingID {
		return false
	}
	return reviewProofJournalHasSiteLost(root, state, generation, resultID, evidenceID, agent.AssignmentID, blockedAt, findingID, reason)
}

// reviewSiteLostRoundAcceptsResult reports whether the registered plan still
// accepts a result, so a site-lost blocker stays valid while the plan is
// running or draining.
func reviewSiteLostRoundAcceptsResult(status string) bool {
	switch status {
	case "running", "cannot_clean", "discovery_draining":
		return true
	default:
		return false
	}
}

// reviewBlockerEvidenceMatchesOriginalSource checks the original blocker
// evidence row. Historical digest, revision and round fields are read at their
// original path only and never decide this fact.
func reviewBlockerEvidenceMatchesOriginalSource(evidence map[string]any, ref, agentID, assignmentID, lens string) bool {
	if reviewProofText(evidence["status"]) != "valid" ||
		reviewProofText(evidence["path"]) != ref ||
		reviewProofText(evidence["responsibility_id"]) != review.LensToResponsibility(lens) {
		return false
	}
	invalidatedBy, hasInvalidatedBy := evidence["invalidated_by"]
	invalidationRule, hasInvalidationRule := evidence["invalidation_rule"]
	invalidationReason, hasInvalidationReason := evidence["invalidation_reason"]
	if !hasInvalidatedBy || invalidatedBy != nil || !hasInvalidationRule || invalidationRule != nil || !hasInvalidationReason || invalidationReason != nil {
		return false
	}
	producers, ok := reviewProofStrings(evidence["produced_by"])
	if !ok || len(producers) != 1 || producers[0] != agentID {
		return false
	}
	scopeRefs, ok := reviewProofStrings(evidence["scope_refs"])
	if !ok || len(scopeRefs) != 1 || scopeRefs[0] != assignmentID {
		return false
	}
	return true
}

func reviewAssignmentContextMatches(state map[string]any, agent *policy.AgentContext, assignment *hookctx.AssignmentContext) bool {
	if agent == nil || assignment == nil || !agent.ReviewAssignment || agent.ID == "" || agent.AssignmentID == "" ||
		assignment.OwnerAgentID != agent.ID || assignment.AssignmentID != agent.AssignmentID || assignment.TaskID == "" {
		return false
	}
	boundAssignment, bound := ReviewAssignmentBinding(state, agent.ID)
	if !bound || boundAssignment != agent.AssignmentID || !reviewProofHasString(agent.TaskIDs, assignment.TaskID) {
		return false
	}
	return reviewProofTaskOwnedByAgent(state, assignment.TaskID, agent.ID)
}

func reviewProofEvidenceAtPath(state map[string]any, ref, kind string) (map[string]any, int) {
	var found map[string]any
	count := 0
	evidenceRows, _ := state["evidence"].([]any)
	for _, raw := range evidenceRows {
		row, _ := raw.(map[string]any)
		if row == nil || reviewProofText(row["path"]) != ref {
			continue
		}
		count++
		if reviewProofText(row["kind"]) == kind {
			found = row
		}
	}
	return found, count
}

func reviewProofEvidenceIDCount(state map[string]any, evidenceID string) int {
	count := 0
	evidenceRows, _ := state["evidence"].([]any)
	for _, raw := range evidenceRows {
		row, _ := raw.(map[string]any)
		if row != nil && reviewProofText(row["id"]) == evidenceID {
			count++
		}
	}
	return count
}

func reviewProofJournalHasSiteLost(root string, state map[string]any, generation int, resultID, evidenceID, assignmentID, blockedAt, findingID, reason string) bool {
	journalBytes, err := loopruntime.ReadJournalForState(root, state)
	if err != nil {
		return false
	}
	inspection, err := controlproof.InspectJournalData(journalBytes)
	if err != nil || controlproof.ValidateStateJournalPair(state, inspection) != nil {
		return false
	}
	prefix := "evt-review-site-lost-" + resultID + "-r"
	for _, event := range inspection.Events {
		if !strings.HasPrefix(reviewProofText(event["event_id"]), prefix) ||
			event["event"] != "transition_committed" || event["transition_id"] != "REVIEW-RESULT" || event["outcome"] != "committed" ||
			reviewProofInt(event["baseline_generation"]) != generation {
			continue
		}
		actor, _ := event["actor"].(map[string]any)
		if reviewProofText(actor["type"]) != "orchestrator" || reviewProofText(actor["id"]) != "orchestrator" {
			continue
		}
		evidenceIDs, ok := reviewProofStrings(event["evidence_ids"])
		wantMessage := fmt.Sprintf("Assignment %s blocked in S7: finding %s scene unrecoverable (%s); the result is not consumed and the round is not sealed", assignmentID, findingID, reason)
		if ok && len(evidenceIDs) == 1 && evidenceIDs[0] == evidenceID &&
			reviewProofText(event["occurred_at"]) == blockedAt && reviewProofText(event["message"]) == wantMessage {
			return true
		}
	}
	return false
}

func reviewWorkBlockedMessage(root string, state map[string]any, agent *policy.AgentContext, assignment *hookctx.AssignmentContext) bool {
	owner, ok := reviewProofAgentRow(state, agent.ID)
	if !ok || reviewProofText(owner["state"]) != "blocked" || reviewProofText(owner["blocker_resolved_ref"]) != "" {
		return false
	}
	blockerRef := reviewProofText(owner["work_blocked_ref"])
	if blockerRef == "" {
		return false
	}
	data, err := workpacket.ReadFile(root, blockerRef, 1<<20)
	if err != nil || schema.NewValidator(root).ValidateBytes("agent-message.schema.json", data) != nil {
		return false
	}
	var message map[string]any
	if json.Unmarshal(data, &message) != nil {
		return false
	}
	if reviewProofText(message["message_type"]) != "blocker_report" ||
		reviewProofText(message["runtime_id"]) != reviewProofText(state["runtime_id"]) ||
		reviewProofText(message["agent_id"]) != agent.ID || reviewProofText(message["task_id"]) != assignment.TaskID ||
		reviewProofText(message["agent_definition_ref"]) != reviewProofText(owner["definition_ref"]) {
		return false
	}
	if teamID, exists := owner["team_id"]; !exists || message["team_id"] != teamID {
		return false
	}
	if !reviewProofJournalBindsWorkBlock(root, state, agent.ID, blockerRef) {
		return false
	}
	return true
}

func reviewProofJournalBindsWorkBlock(root string, state map[string]any, agentID, ref string) bool {
	journalBytes, err := loopruntime.ReadJournalForState(root, state)
	if err != nil {
		return false
	}
	inspection, err := controlproof.InspectJournalData(journalBytes)
	if err != nil || controlproof.ValidateStateJournalPair(state, inspection) != nil {
		return false
	}
	prefix := "evt-agent-" + agentID + "-"
	runtimeID := reviewProofText(state["runtime_id"])
	latestSequence := -1
	latestKind, latestRef := "", ""
	for _, event := range inspection.Events {
		eventID := reviewProofText(event["event_id"])
		if !strings.HasPrefix(eventID, prefix) || event["event"] != "transition_committed" || event["transition_id"] != "AGENT-LIFECYCLE" || event["outcome"] != "committed" ||
			reviewProofText(event["runtime_id"]) != runtimeID {
			continue
		}
		actor, _ := event["actor"].(map[string]any)
		if reviewProofText(actor["type"]) != "orchestrator" || reviewProofText(actor["id"]) != "orchestrator" {
			continue
		}
		kind := ""
		switch reviewProofText(event["message"]) {
		case "Recorded a schema-valid Agent lifecycle event (work_blocked)":
			kind = "work_blocked"
		case "Recorded a schema-valid Agent lifecycle event (blocker_resolved)":
			kind = "blocker_resolved"
		default:
			continue
		}
		// Both the historical "-r<revision>" shape and the semantic
		// "-<intent key>" shape of the Agent lifecycle event id are accepted
		// so an already written journal stays readable (G18 §1.4).
		if !strings.HasPrefix(eventID, prefix+kind+"-") {
			continue
		}
		sequence := reviewProofInt(event["sequence"])
		if sequence <= latestSequence {
			continue
		}
		latestSequence = sequence
		latestKind, latestRef = kind, ""
		if kind == "work_blocked" {
			ids, ok := reviewProofStrings(event["evidence_ids"])
			if ok && len(ids) == 1 {
				latestRef = ids[0]
			}
		}
	}
	return latestKind == "work_blocked" && latestRef == ref
}

func reviewProofAgentRow(state map[string]any, agentID string) (map[string]any, bool) {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	var matched map[string]any
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row == nil || reviewProofText(row["id"]) != agentID {
			continue
		}
		if matched != nil {
			return nil, false
		}
		matched = row
	}
	return matched, matched != nil
}

func reviewProofTaskOwnedByAgent(state map[string]any, taskID, agentID string) bool {
	entities, _ := state["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	var matched map[string]any
	for _, raw := range tasks {
		row, _ := raw.(map[string]any)
		if row == nil || reviewProofText(row["id"]) != taskID {
			continue
		}
		if matched != nil {
			return false
		}
		matched = row
	}
	if matched == nil {
		return false
	}
	owners, ok := reviewProofStrings(matched["owner_agent_ids"])
	return ok && reviewProofHasString(owners, agentID)
}

func reviewProofHasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func reviewProofDispatchedIdentity(state map[string]any, agent *policy.AgentContext) bool {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	var matched map[string]any
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row == nil || reviewProofText(row["id"]) != agent.ID {
			continue
		}
		if matched != nil {
			return false
		}
		matched = row
	}
	if matched == nil || !reviewProofAgentStateMatches(state, matched, agent) {
		return false
	}
	if mode := reviewProofText(matched["dispatch_mode"]); mode != "" && mode != agent.DispatchMode {
		return false
	}
	taskIDs, ok := reviewProofStrings(matched["task_ids"])
	if !ok || len(taskIDs) == 0 || !sameReviewProofSet(taskIDs, agent.TaskIDs) {
		return false
	}
	return true
}

func reviewProofAgentStateMatches(state, row map[string]any, agent *policy.AgentContext) bool {
	if agent == nil {
		return false
	}
	version, _ := state["schema_version"].(string)
	if version == "" {
		version = "1.2.0"
	}
	projection, err := workstate.AgentProjection(row, version)
	return err == nil && string(projection.Status) == agent.State && projection.Activity == agent.LifecycleActivity()
}

func reviewEnvelopeClaimSetMatches(envelope map[string]any, expected []string) bool {
	actual := []string{}
	for _, field := range []string{"claim_results", "blocked_claims"} {
		value, present := envelope[field]
		if !present {
			return false
		}
		if value == nil {
			continue
		}
		rows, ok := value.([]any)
		if !ok {
			return false
		}
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row == nil {
				return false
			}
			claimID := reviewProofText(row["claim_id"])
			if claimID == "" {
				return false
			}
			actual = append(actual, claimID)
		}
	}
	return sameReviewProofSet(actual, expected)
}

func sameReviewProofSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, value := range left {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	rightSeen := make(map[string]bool, len(right))
	for _, value := range right {
		if value == "" || rightSeen[value] || !seen[value] {
			return false
		}
		rightSeen[value] = true
	}
	return true
}

func reviewProofStrings(value any) ([]string, bool) {
	switch rows := value.(type) {
	case []string:
		return append([]string(nil), rows...), true
	case []any:
		result := make([]string, 0, len(rows))
		for _, raw := range rows {
			value, ok := raw.(string)
			if !ok {
				return nil, false
			}
			result = append(result, value)
		}
		return result, true
	default:
		return nil, false
	}
}

func reviewProofText(value any) string {
	result, _ := value.(string)
	return result
}

func reviewProofInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	default:
		return 0
	}
}

// RenderStopBlockFeedback builds the stderr text fed back to the agent when
// the transport exits 2. The Guidance action/instruction (when the
// Controller attached one) is appended so the feedback names the next step.
func RenderStopBlockFeedback(decision policy.Decision, questionnaire ...string) string {
	if len(questionnaire) > 0 && strings.TrimSpace(questionnaire[0]) != "" {
		decision.Recovery = nil
		decision.Guidance = nil
		return message(decision) + "\n" + questionnaire[0]
	}
	body := message(decision)
	if decision.Guidance != nil && decision.Guidance.Action != "" {
		body += " Next: " + decision.Guidance.Action + "."
	}
	return body
}

// stopIdleRuleID selects the stable rule id of a stop/idle refusal. The
// TeammateIdle rule id is the one the idle recovery budget counts
// (internal/hook/idle_budget.go), so a refusal must not invent a new id.
func stopIdleRuleID(event string) string {
	if event == "TeammateIdle" {
		return RuleTeammateIdleResumeAssignment
	}
	return RuleSubagentStopMissingResult
}

func stopIdleBlock(event string, agent *policy.AgentContext) policy.Decision {
	questionnaireAssignment := agent.DocumentReviewAssignment || agent.ReviewAssignment || agent.HypothesisResultAssignment || agent.RepairResultAssignment
	planRecorded := agent.PlanReportedRef != "" || questionnaireAssignment
	var ruleID, reason string
	var recovery []string
	switch {
	case event == "TeammateIdle" && !planRecorded:
		ruleID = RuleTeammateIdleResumeAssignment
		reason = fmt.Sprintf("teammate %s went idle before its PLAN_REPORT checkpoint; idle is not allowed before the plan is recorded (L4 §7.4)", agent.ID)
		recovery = []string{
			"send the PLAN_REPORT via SendMessage with message_type=plan_report — the PostToolUse(SendMessage) observer records the checkpoint",
			"then continue executing the current assignment in the same turn",
		}
	case event == "TeammateIdle":
		ruleID = RuleTeammateIdleResumeAssignment
		if questionnaireAssignment {
			reason = fmt.Sprintf("teammate %s went idle before its original professional answer was received; idle is not delivery (L4 §7.4)", agent.ID)
		} else {
			reason = fmt.Sprintf("teammate %s went idle after the plan but before the assignment Result; the plan is not the deliverable (L4 §7.4)", agent.ID)
		}
		if agent.DocumentReviewAssignment {
			recovery = []string{
				"continue the current S5 document review",
				"fill the exact issued professional answer path shown in the questionnaire context and save it; the original S5 consumer records delivery",
			}
		} else if agent.HypothesisResultAssignment && agent.AssignmentID != "" {
			recovery = []string{
				"continue the current Investigator Assignment",
				"fill the exact issued S8 answer path shown in the questionnaire context and save it; the original Case consumer records delivery",
			}
		} else if agent.ReviewAssignment && agent.AssignmentID != "" {
			recovery = []string{
				"continue the current assignment",
				"fill the exact issued S7 ReviewResult answer path shown in the questionnaire context and save it; the original review consumer records delivery",
			}
		} else {
			recovery = []string{
				"continue the current assignment",
				"register the Result (`runtime task-complete`, or a completion_report/blocker_report via SendMessage) before going idle",
			}
		}
	case !planRecorded:
		ruleID = RuleSubagentStopMissingResult
		reason = fmt.Sprintf("subagent %s is stopping before any PLAN_REPORT or Result was recorded; stop is not completion (L4 §16.1)", agent.ID)
		recovery = []string{
			"continue working: send the PLAN_REPORT via SendMessage with message_type=plan_report, then execute it",
			"register the Result before stopping; if the session already ended, Main must SendMessage the same agent id — never spawn a replacement",
		}
	default:
		ruleID = RuleSubagentStopMissingResult
		if questionnaireAssignment {
			reason = fmt.Sprintf("subagent %s is stopping before its original professional answer was received; stop is not delivery (L4 §16.1)", agent.ID)
		} else {
			reason = fmt.Sprintf("subagent %s is treating the plan as its final response; a PLAN_REPORT is not the Result (L4 §16.1)", agent.ID)
		}
		if agent.DocumentReviewAssignment {
			recovery = []string{
				"continue the current S5 document review",
				"fill the exact issued professional answer path shown in the questionnaire context and save it; the original S5 consumer records delivery",
			}
		} else if agent.HypothesisResultAssignment && agent.AssignmentID != "" {
			recovery = []string{
				"continue the current Investigator Assignment",
				"fill the exact issued S8 answer path shown in the questionnaire context and save it; the original Case consumer records delivery",
			}
		} else if agent.ReviewAssignment && agent.AssignmentID != "" {
			recovery = []string{
				"continue the current Reviewer Assignment",
				"fill the exact issued S7 ReviewResult answer path shown in the questionnaire context and save it; the original review consumer records delivery",
			}
		} else {
			recovery = []string{
				"continue executing the planned steps in this turn",
				"register the Result before stopping; if the session already ended, Main must SendMessage the same agent id — never spawn a replacement",
			}
		}
	}
	return policy.Decision{
		Decision: "deny",
		RuleID:   ruleID,
		Reason:   reason,
		Recovery: recovery,
		Retry:    policy.RetryAfterRecoveryValidation,
	}
}

// repairResultAssignmentUnfinishedNotice projects the honest S9 fact without
// blocking the return. The retired frozen execution / source-pin proof must not
// close it, and the projection must not become a second certifying consumer
// (L4-workflow-convergence.md:342; L4-hook-platform-wiring.md:83).
func repairResultAssignmentUnfinishedNotice(event, agentID, assignmentID string) policy.Decision {
	return policy.Decision{
		Decision: "warn", RuleID: stopIdleRuleID(event),
		Reason: fmt.Sprintf("S9 Builder %s, Assignment %s: the original responsibility context is unavailable or inconsistent. Main/system must inspect the actual dispatch, original answer and integration state; the answer has not been established as received and the work is not established as complete.", agentID, assignmentID),
		Recovery: []string{
			"continue this S9 responsibility using its current exact PLAN and Result questionnaire paths",
			"save the issued RepairResult through the platform PostToolUse Hook, then complete its frozen owner-owned checks; Main integration and final Result remain separate",
		},
		Retry: policy.RetryNotApplicable,
	}
}

func reviewAssignmentBindingNotice(event, agentID, assignmentID string) policy.Decision {
	return policy.Decision{
		Decision: "deny", RuleID: stopIdleRuleID(event),
		Reason: fmt.Sprintf("S7 Reviewer %s, Assignment %s: the original responsibility context is unavailable or inconsistent. Main/system must inspect the actual dispatch and original answer; the answer has not been established as received. This stop does not establish receipt or completion.", agentID, assignmentID),
		Recovery: []string{
			"restore or re-register the exact current S7 workgroup manifest and Assignment binding for the same Agent",
			"then submit the issued S7 ReviewResult or record the original blocker_report before idling or stopping",
		},
		Retry: policy.RetryAfterRecoveryValidation,
	}
}

func hypothesisResultAssignmentBindingNotice(event, agentID, assignmentID string) policy.Decision {
	return policy.Decision{
		Decision: "deny", RuleID: stopIdleRuleID(event),
		Reason: fmt.Sprintf("S8 Investigator %s, Assignment %s: the original responsibility context is unavailable or inconsistent. Main/system must inspect the actual Case, dispatch and original answer; the answer has not been established as received. This stop does not establish receipt or completion.", agentID, assignmentID),
		Recovery: []string{
			"restore the exact current Investigator dispatch and its original Case/manifest/TASK sources for the same Agent",
			"then complete the issued professional HypothesisResult and save it through the platform PostToolUse Hook, or follow the original blocker/retirement route",
		},
		Retry: policy.RetryAfterRecoveryValidation,
	}
}

func repairResultAssignmentBindingNotice(event, agentID, assignmentID string) policy.Decision {
	return policy.Decision{
		Decision: "deny", RuleID: stopIdleRuleID(event),
		Reason: fmt.Sprintf("S9 Builder %s, Assignment %s: the original responsibility context is unavailable or inconsistent. Main/system must inspect the actual dispatch, original answer and integration state; the answer has not been established as received and the work is not established as complete. This stop does not establish receipt or completion.", agentID, assignmentID),
		Recovery: []string{
			"restore the exact registered Worker execution and current S9 packet/source tuple for the same Builder",
			"save the issued RepairResult through the platform PostToolUse Hook, then complete its frozen owner-owned checks; Main integration and final Result remain separate",
		},
		Retry: policy.RetryAfterRecoveryValidation,
	}
}

func assignmentForAgent(assignments []hookctx.AssignmentContext, agentID string) *hookctx.AssignmentContext {
	for i := range assignments {
		if assignments[i].OwnerAgentID == agentID {
			return &assignments[i]
		}
	}
	return nil
}

func assignmentForReviewOwner(assignments []hookctx.AssignmentContext, agentID, assignmentID string) *hookctx.AssignmentContext {
	var matched *hookctx.AssignmentContext
	for i := range assignments {
		if assignments[i].OwnerAgentID != agentID || assignments[i].AssignmentID != assignmentID {
			continue
		}
		if matched != nil {
			return nil
		}
		matched = &assignments[i]
	}
	return matched
}

// AssignmentForAgent is the exported counterpart of assignmentForAgent
// so the Controller's buildGuidance can resolve the same assignment the
// stop/idle verdict reads (L4 §7.4 / §16.1 — the Hook transport and the
// Controller must share facts).
func AssignmentForAgent(assignments []hookctx.AssignmentContext, agentID string) *hookctx.AssignmentContext {
	return assignmentForAgent(assignments, agentID)
}

// HasCompletionReport reports a delivery marker for legacy Stop compatibility.
// It is not current consumer acceptance and never projects workstate.Done.
func HasCompletionReport(assignment *hookctx.AssignmentContext) bool {
	if assignment == nil {
		return false
	}
	if assignment.CompletionRef != "" {
		return true
	}
	return assignment.ReportStatus == "completion_report" || workstate.LegacyCompletionReportStatus(assignment.ReportStatus)
}

// HasBlockerReport is retained as a compatibility symbol for callers that
// used the old manifest-status predicate. Manifest status is descriptive;
// current blocker authority comes from the original lifecycle or S7 consumer.
func HasBlockerReport(assignment *hookctx.AssignmentContext) bool {
	return false
}
