package cli

import (
	"context"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/policy"
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
	review := mapFieldCLI(state, "review")
	plan := mapFieldCLI(review, "plan")
	if packet.Binding.RuntimeID != state["runtime_id"] || packet.Binding.BaselineGeneration != tolerantInt(mapFieldCLI(state, "baseline")["generation"]) ||
		packet.Binding.ReviewRound != tolerantInt(review["round"]) || packet.Binding.ReviewPlanID != stringValue(plan["plan_id"]) || packet.Binding.ReviewPlanRevision != tolerantInt(plan["revision"]) {
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
			return diagnostic + " The machine CleanRound is recorded; Main follows the original continuation result, preserving accepted Reviewer answers. If the exit is still pending after resolving its cause, resume with `" + s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime review-continue --root " + s7CaptureShellQuote(root) + "`."
		}
		return diagnostic + nativeS7RemainingOutletContext(root, state)
	case "paused", "awaiting_human_release":
		return diagnostic + nativeS7RemainingOutletContext(root, state)
	}
	return ""
}
