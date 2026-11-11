package cli

import (
	"context"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Main's Agent return observes the original reception/continuation facts. It
// cannot run the Controller, prepare material, or receive a private answer.
func nativeS7AcceptedOutletContext(root string, snapshot runtime.Snapshot, packet workpacket.ReviewResultPacket) string {
	return nativeS7AcceptedOutletForRequest(context.Background(), root, snapshot, packet)
}

func nativeS7AcceptedOutletForRequest(ctx context.Context, root string, snapshot runtime.Snapshot, packet workpacket.ReviewResultPacket) string {
	state := snapshot.State
	reviewState := mapFieldCLI(state, "review")
	plan := mapFieldCLI(reviewState, "plan")
	applies, err := review.IssuedResultAppliesToCurrent(root, state, packet, ctx)
	if err != nil {
		return " This original S7 Result is accepted; its relationship to the current responsibility is unknown: " + err.Error() + ". Main restores the original plan materials and continues the original outlet; the Reviewer need not resubmit."
	}
	if !applies {
		return " This accepted S7 Result belongs to an earlier responsibility; preserve its history and follow the current round/stage."
	}
	diagnostic := submission.PlanningDiagnosticContext(ctx, root, state, "S7 acceptance continuation")
	switch lifecycleState(state) {
	case "acceptance":
		return diagnostic + " Original TR-009 has entered S10 acceptance. " + stageAuditQuestionnaireContext(root, policy.Input{Context: ctx, Event: "PostToolUse", AgentID: "Main", CWD: root}, snapshot)
	case "verification":
		if state["pause"] != nil {
			return diagnostic + nativeS7RemainingOutletContext(root, state)
		}
		if plan["status"] == "clean" {
			return diagnostic + " The original required S7 responsibilities report completion; Main follows the original continuation result, preserving accepted Reviewer answers. If the exit is still pending after resolving its cause, resume with `" + s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime review-continue --root " + s7CaptureShellQuote(root) + "`."
		}
		return diagnostic + nativeS7RemainingOutletContext(root, state)

	case "bug_resolution":
		rows := mapFieldCLI(reviewState, "assignments")
		row, _ := rows[packet.Binding.AssignmentID].(map[string]any)
		for _, finding := range review.RoundFindings(state) {
			if finding["assignment_id"] == packet.Binding.AssignmentID {
				return " This original S7 answer is accepted. Main receives the new Finding facts from " + stringValue(row["result_ref"]) + " and decides their effect on the current investigation or follow-up; existing approvals remain unchanged."
			}
		}
		return " This late original S7 answer is accepted; preserve its recorded conclusion and current S8 work."
	case "paused", "awaiting_human_release":
		return diagnostic + nativeS7RemainingOutletContext(root, state)
	}
	return ""
}
