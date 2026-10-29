package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// continueS6Integration composes the original batch Gate and TR-006 Writer at
// Main's integration exit. It never manufactures a batch, completion or check.
// A replay outside building cannot advance a later review round.
func continueS6Integration(root, statePath, journalPath, assignment string, snapshot runtime.Snapshot) (runtime.Snapshot, string) {
	if lifecycleState(snapshot.State) != "building" {
		return snapshot, ""
	}
	retry := fmt.Sprintf("loop-harness runtime task-integrate --root %s --assignment-id %s", s7CaptureShellQuote(root), s7CaptureShellQuote(assignment))
	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{Root: root, StatePath: statePath, JournalPath: journalPath, CWD: root, Event: "IntegrationComplete", AgentID: "Main", TargetID: assignment})
	if result.Snapshot.State != nil {
		snapshot = result.Snapshot
	}
	if err != nil || result.Error != "" {
		detail := result.Error
		if err != nil {
			detail = err.Error()
		}
		return snapshot, "Main integration is recorded; S6→S7 continuation failed: " + detail + ". Preserve the accepted Builder Result and checkpoint. After resolving this cause, re-enter the original command to resume unfinished steps from that checkpoint, without another merge or Result reception: " + retry
	}
	if !result.QualityGate.TransitionCommitted {
		missing := strings.Join(result.QualityGate.Missing, "; ")
		if len(result.Warnings) > 0 {
			missing += "; original diagnostics: " + strings.Join(result.Warnings, "; ")
		}
		if len(result.QualityGate.Conflicts) > 0 {
			missing += "; conflicts: " + strings.Join(result.QualityGate.Conflicts, "; ")
		}
		if result.QualityGate.ErrorCode != "" {
			missing += "; error_code=" + result.QualityGate.ErrorCode
		}
		if result.QualityGate.Status == controller.StatusUnknown || result.QualityGate.Status == controller.StatusBlocked {
			missing = "original batch Gate status=" + string(result.QualityGate.Status) + ": " + missing
		}
		if missing == "" {
			missing = "inspect the original batch Gate and remaining current TASK responsibilities"
		}
		return snapshot, "Main integration is recorded; S6 batch remains open: " + missing + ". Continue the remaining responsibility; do not reopen this completed Builder. If this recorded integration's continuation needs recovery after resolving the named cause, re-enter its original command; the checkpoint preserves its existing merge and reception: " + retry
	}
	message := "Main integration and original " + result.QualityGate.CandidateTransition + " are recorded. "
	request := policy.Input{Event: "IntegrationComplete", AgentID: "Main", CWD: root}
	if result.QualityGate.CandidateTransition == "TR-006" && lifecycleState(snapshot.State) == "verification" {
		message += stageReviewPlanQuestionnaireContext(root, request, snapshot)
	} else {
		guidance := BuildGuidanceForState(root, snapshot.State, "SessionStart", request)
		message += guidance.Action
	}

	return snapshot, message
}
