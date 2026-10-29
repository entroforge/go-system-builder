package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// workerPlanDraftPostToolUse recognizes a successful save to the exact plan
// draft frozen for the current registered Worker. It remains an observer: the
// existing workspace begin command is the consumer that validates the draft
// and commits the original activation/work-start lifecycle.
func workerPlanDraftPostToolUse(root string, request policy.Input, snapshot runtime.Snapshot) (hook.PostToolUseObservation, bool) {
	if request.Event != "PostToolUse" || request.AgentID == "" || request.CWD == "" {
		return hook.PostToolUseObservation{}, false
	}
	switch request.ToolName {
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
	default:
		return hook.PostToolUseObservation{}, false
	}
	path := workerPlanToolPath(request.ToolInput)
	if path == "" {
		return hook.PostToolUseObservation{}, false
	}

	binding, err := workspace.Decode(snapshot.State)
	if err != nil || binding == nil {
		return hook.PostToolUseObservation{}, false
	}
	if canonicalRoot, err := workspace.Canonical(root); err != nil || canonicalRoot != binding.MainRoot {
		return hook.PostToolUseObservation{}, false
	}
	agentRow := currentAgentRow(snapshot.State, request.AgentID)
	if agentRow == nil || agentRow["dispatch_mode"] != "plan_checkpoint" || stringValue(agentRow["plan_reported_ref"]) != "" {
		return hook.PostToolUseObservation{}, false
	}
	switch workstate.AgentActivity(agentRow) {
	case "spawned", "queued", "reading":
	default:
		// A five-state active label alone is not enough to claim that the
		// original plan is waiting to be consumed.
		return hook.PostToolUseObservation{}, false
	}

	runtimeID, generation := workspace.RuntimeID(snapshot.State), workspace.Generation(snapshot.State)
	requestedCWD, err := workspace.Canonical(request.CWD)
	if err != nil {
		return hook.PostToolUseObservation{}, false
	}
	var execution workspace.Execution
	count := 0
	for _, candidate := range binding.Executions {
		if candidate.RuntimeID != runtimeID || candidate.BaselineGeneration != generation ||
			candidate.AgentID != request.AgentID || candidate.Status != "ready" || candidate.Path != requestedCWD || candidate.BootstrapSHA256 == "" {
			continue
		}
		execution = candidate
		count++
	}
	if count != 1 {
		return hook.PostToolUseObservation{}, false
	}
	if execution.Purpose == workspace.DocumentReviewPurpose {
		packet, err := workspace.DocumentReviewPacket(snapshot.State, execution)
		if err != nil {
			return hook.PostToolUseObservation{}, false
		}
		lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
		if lifecycle["state"] != "document_verification" || stageAnswerEntry(lifecycle["phase_revision"]) != packet.Binding.StageEntry {
			return hook.PostToolUseObservation{}, false
		}
		if _, received, err := runtime.FindSubmission(snapshot.State, packet.ID); err != nil || received {
			return hook.PostToolUseObservation{}, false
		}
	}
	paths, err := binding.WorkerSubmissionPaths(execution)
	if err != nil || paths.Plan == "" {
		return hook.PostToolUseObservation{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err = binding.ValidateExecution(ctx, execution, false)
	cancel()
	if err != nil {
		return hook.PostToolUseObservation{}, false
	}

	expected := filepath.Join(execution.Path, filepath.FromSlash(paths.Plan))
	actual := path
	if !filepath.IsAbs(actual) {
		actual = filepath.Join(requestedCWD, actual)
	}
	expected, err = workspace.CanonicalProspective(expected)
	if err != nil {
		return hook.PostToolUseObservation{}, false
	}
	actual, err = workspace.CanonicalProspective(actual)
	if err != nil || actual != expected {
		return hook.PostToolUseObservation{}, false
	}
	info, err := os.Lstat(expected)
	if err != nil || !info.Mode().IsRegular() {
		return hook.PostToolUseObservation{}, false
	}

	beginCommand := workspace.WorkerInvocation(execution, "begin")
	message := fmt.Sprintf("The registered PLAN draft was saved at %s. Continue through its existing consumer by running this exact workspace begin command: %s. Begin validates the frozen draft and applies the original activation rules.", expected, beginCommand)
	if execution.Purpose == workspace.DocumentReviewPurpose {
		message += " Save the issued answer only after begin succeeds."
	}
	return hook.PostToolUseObservation{
		Recorded:  true,
		AgentID:   request.AgentID,
		Message:   "plan_report",
		SystemMsg: message,
	}, true
}

func workerPlanToolPath(input map[string]any) string {
	for _, key := range []string{"file_path", "path", "notebook_path"} {
		if path, ok := input[key].(string); ok && strings.TrimSpace(path) != "" {
			return strings.TrimSpace(path)
		}
	}
	return ""
}

func currentAgentRow(state map[string]any, agentID string) map[string]any {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		row, _ := raw.(map[string]any)
		if row != nil && row["id"] == agentID {
			return row
		}
	}
	return nil
}
