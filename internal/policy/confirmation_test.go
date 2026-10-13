package policy_test

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"testing"
)

func TestConfirmationSessionCannotUseImplementationWriteScope(t *testing.T) {
	for _, intent := range []string{"confirm", "unknown"} {
		decision, blocked := policy.EvaluateAgentScoped(policy.Input{Event: "PreToolUse", ToolName: "Edit", ToolInput: map[string]any{"file_path": "internal/service.go"}, Runtime: policy.RuntimeContext{CurrentState: "bug_resolution", CurrentPhase: "fixing", RepairIntent: intent, Agent: &policy.AgentContext{ID: "builder", RepairAssignmentID: "repair-assignment-unit-1", RepairAllowedWritePaths: []string{"internal/"}}}})
		if !blocked || decision.Decision != "deny" {
			t.Fatalf("%s allowed implementation write: %#v", intent, decision)
		}
	}
}
