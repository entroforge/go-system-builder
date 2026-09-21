package cli

import (
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// Refresh only at lifecycle checkpoints, not on each tool invocation. Errors
// are observations, never new Stop/Idle or ordinary-tool gates.
func applyDispatchGuidance(root string, state map[string]any, event string, input policy.Input, guidance *policy.Guidance) {
	if !dispatchGuidanceCheckpoint(event, input) {
		return
	}
	board, err := dispatch.Load(root, state, 0)
	if err != nil {
		guidance.Automation = append(guidance.Automation, "S6 dispatch projection unavailable; inspect `loop-harness s6 status` before choosing another TASK")
		return
	}
	guidance.Automation = append(guidance.Automation, dispatch.Advisory(board)...)
}

func dispatchGuidanceCheckpoint(event string, input policy.Input) bool {
	if input.EffectiveAgentID() != "" {
		return false
	}
	switch event {
	case "SessionStart":
	case "PostToolUse":
		switch input.ToolName {
		case "Agent", "Task":
			if input.ToolResponse["status"] == "async_launched" {
				return false
			}
		case "Bash":
			command, _ := input.ToolInput["command"].(string)
			if !strings.Contains(command, "loop-harness") || !(strings.Contains(command, "task-integrate") || strings.Contains(command, "task-complete") || strings.Contains(command, "register-workgroup")) {
				return false
			}
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// PostToolUse is an observer, so project its advisory without invoking the
// Controller cycle or persisting another gate/transition. Read after the tool
// has completed, using the same lifecycle restriction as recovery guidance.
func postToolDispatchContext(root string, input policy.Input) string {
	if !dispatchGuidanceCheckpoint("PostToolUse", input) {
		return ""
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return ""
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "building" || !qualitygate.HasDispatchPlan(snapshot.State) {
		return ""
	}
	guidance := policy.Guidance{}
	applyDispatchGuidance(root, snapshot.State, "PostToolUse", input, &guidance)
	return strings.Join(guidance.Automation, "\n")
}
