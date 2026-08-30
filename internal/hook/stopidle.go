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
// assignment) — it reuses the same fact sources as the Controller's
// TeammateIdle/SubagentStop handlers (hookctx.LoadFull) and never invents a
// second state machine. Fail-open contract: when the payload identifies no
// known agent, or the runtime cannot be read, the event is allowed — never
// fabricate an agent binding.
package hook

import (
	"fmt"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
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
//   - one_shot dispatch                       → allow (the final message IS
//     the result; nothing is registered in the runtime)
//   - plan_approval_required dispatch         → allow (L4 §3.3: this mode
//     routes through `understanding_approved`, not through the
//     PLAN_REPORT checkpoint — the stop/idle gate is not the authority
//     that decides whether the plan has been approved)
//   - state reported/done/completed/closed    → allow (Result registered)
//   - valid blocker (state blocked or blocker report on the assignment)
//     → allow (Main consumes it)
//   - completion report on the assignment     → allow (awaiting consumer;
//     no automatic next-task claim here)
//   - no PLAN_REPORT checkpoint               → block: send the plan first
//   - plan but no Result                      → block: the plan is not the
//     deliverable; continue the current assignment
func StopIdleDecision(root string, input policy.Input) (policy.Decision, bool) {
	if !IsStopIdleEvent(input.Event) {
		return policy.Decision{}, false
	}
	if input.StopHookActive {
		return policy.Decision{}, false
	}
	agentID := input.EffectiveAgentID()
	if agentID == "" {
		return policy.Decision{}, false
	}
	loaded, err := hookctx.LoadFull(root, agentID)
	if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil {
		return policy.Decision{}, false
	}
	agent := loaded.PolicyContext.Agent
	if agent.State == "queued" {
		// A resource-lock-queued Agent has not been dispatched. It must not
		// be forced through PLAN_REPORT/Result gates before the queue consumer
		// wakes it into reading in the same CAS that releases the lock.
		return policy.Decision{}, false
	}
	switch agent.State {
	case "reported", "done", "completed", "closed":
		return policy.Decision{}, false
	}
	switch agent.DispatchMode {
	case "one_shot", "plan_approval_required":
		// one_shot: the final message IS the result; nothing is
		// registered in the runtime.
		// plan_approval_required: L4 §3.3 routes this mode through
		// `understanding_approved`, not through the PLAN_REPORT
		// checkpoint. The stop/idle gate has no authority to demand a
		// PLAN_REPORT it is not the authoritative checkpoint for.
		return policy.Decision{}, false
	}
	assignment := assignmentForAgent(loaded.Assignments, agentID)
	if agent.State == "blocked" || hasBlockerReport(assignment) {
		return policy.Decision{}, false
	}
	if agent.CompletionReportedRef != "" || hasCompletionReport(assignment) {
		return policy.Decision{}, false
	}
	return stopIdleBlock(input.Event, agent), true
}

// RenderStopBlockFeedback builds the stderr text fed back to the agent when
// the transport exits 2. The Guidance action/instruction (when the
// Controller attached one) is appended so the feedback names the next step.
func RenderStopBlockFeedback(decision policy.Decision) string {
	body := message(decision)
	if decision.Guidance != nil && decision.Guidance.Action != "" {
		body += " Next: " + decision.Guidance.Action + "."
	}
	return body
}

func stopIdleBlock(event string, agent *policy.AgentContext) policy.Decision {
	planRecorded := agent.PlanReportedRef != ""
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
		reason = fmt.Sprintf("teammate %s went idle after the plan but before the assignment Result; the plan is not the deliverable (L4 §7.4)", agent.ID)
		if agent.AssignmentID != "" {
			// S7-10 (RC-12): an S7 ReviewPlan Assignment's deliverable is a
			// Canonical ReviewResult, not the S6 `task-complete` verb — name
			// the actual submit path when the plan checkpoint was recorded.
			recovery = []string{
				"continue the current assignment",
				"register the Result via `runtime review-result submit --assignment-id " + agent.AssignmentID + " --result <result.json>` (or a completion_report/blocker_report via SendMessage) before going idle",
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
		reason = fmt.Sprintf("subagent %s is treating the plan as its final response; a PLAN_REPORT is not the Result (L4 §16.1)", agent.ID)
		if agent.AssignmentID != "" {
			// S7-10 (RC-12): see the TeammateIdle branch — S7 Assignments
			// deliver via review-result submit, not `runtime task-complete`.
			recovery = []string{
				"continue executing the planned steps in this turn",
				"register the Result via `runtime review-result submit --assignment-id " + agent.AssignmentID + " --result <result.json>` before stopping; if the session already ended, Main must SendMessage the same agent id — never spawn a replacement",
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

func assignmentForAgent(assignments []hookctx.AssignmentContext, agentID string) *hookctx.AssignmentContext {
	for i := range assignments {
		if assignments[i].OwnerAgentID == agentID {
			return &assignments[i]
		}
	}
	return nil
}

// AssignmentForAgent is the exported counterpart of assignmentForAgent
// so the Controller's buildGuidance can resolve the same assignment the
// stop/idle verdict reads (L4 §7.4 / §16.1 — the Hook transport and the
// Controller must share facts).
func AssignmentForAgent(assignments []hookctx.AssignmentContext, agentID string) *hookctx.AssignmentContext {
	return assignmentForAgent(assignments, agentID)
}

// HasCompletionReport mirrors the Controller's completion test
// (internal/cli/controller.go hasCompletionReport) so the stop/idle verdict
// and the buildGuidance projection read the same facts (L4 §7.4 / §16.1).
func HasCompletionReport(assignment *hookctx.AssignmentContext) bool {
	if assignment == nil {
		return false
	}
	if assignment.CompletionRef != "" {
		return true
	}
	switch assignment.ReportStatus {
	case "completion_report", "complete", "completed":
		return true
	}
	return false
}

// HasBlockerReport mirrors the Controller's blocker test
// (internal/cli/controller.go hasBlockerReport) so the stop/idle verdict
// and the buildGuidance projection read the same facts (L4 §7.4 / §16.1).
func HasBlockerReport(assignment *hookctx.AssignmentContext) bool {
	if assignment == nil {
		return false
	}
	return assignment.ReportStatus == "blocked" || assignment.ReportStatus == "blocker_report"
}

func hasCompletionReport(assignment *hookctx.AssignmentContext) bool {
	return HasCompletionReport(assignment)
}

func hasBlockerReport(assignment *hookctx.AssignmentContext) bool {
	return HasBlockerReport(assignment)
}
