package policy_test

import (
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// The paths are issued facts. Their spelling, and especially any former
// review-round directory, is not another source of ownership.
func TestS8S9IssuedAnswerPathDoesNotDependOnRoundOrTemplate(t *testing.T) {
	for _, tc := range []struct {
		name, phase string
		prepare     func(*policy.RuntimeContext, string)
	}{
		{"S8", "investigation", func(c *policy.RuntimeContext, p string) {
			c.Agent.HypothesisResultAssignment = true
			c.HypothesisResultAnswerPaths = []string{p}
		}},
		{"S9 plan", "planning", func(c *policy.RuntimeContext, p string) { c.RepairPlanReportAnswerPaths = []string{p} }},
		{"S9 result", "fixing", func(c *policy.RuntimeContext, p string) {
			c.Agent.RepairResultAssignment = true
			c.RepairResultAnswerPaths = []string{p}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := ".claude/submissions/original/issued-questionnaire.json"
			c := policy.RuntimeContext{PacketMode: true, ProjectRoot: root, CurrentState: "bug_resolution", CurrentPhase: tc.phase, Agent: &policy.AgentContext{ID: "actual-owner", AssignmentID: "assignment", RepairAssignmentID: "repair"}}
			tc.prepare(&c, path)
			input := policy.Input{Event: "PreToolUse", AgentID: "actual-owner", ToolName: "Write", ToolInput: map[string]any{"file_path": path}, Runtime: c}
			if d, blocked := policy.EvaluateAgentScoped(input); blocked {
				t.Fatalf("issued path was blocked: %+v", d)
			}
			input.AgentID = "other-owner"
			if d, blocked := policy.EvaluateAgentScoped(input); !blocked || d.Decision != "deny" {
				t.Fatalf("known different owner inherited issued path: %+v", d)
			}
		})
	}
}

func TestS9PendingContinuationKeepsOriginalAssignmentWriteScope(t *testing.T) {
	root := t.TempDir()
	in := policy.Input{Event: "PreToolUse", AgentID: "builder", ToolName: "Write", ToolInput: map[string]any{"file_path": "internal/api/one.go"}, Runtime: policy.RuntimeContext{ProjectRoot: root, CurrentState: "bug_resolution", CurrentPhase: "fixing", Agent: &policy.AgentContext{ID: "builder", RepairAssignmentID: "repair-assignment-1", RepairAllowedWritePaths: []string{"internal/api"}, RepairResultResumePending: true}}}
	if d, blocked := policy.EvaluateAgentScoped(in); blocked {
		t.Fatalf("original Assignment write blocked while continuation pending: %+v", d)
	}
	in.ToolInput["file_path"] = "internal/other/other.go"
	if d, blocked := policy.EvaluateAgentScoped(in); !blocked || d.Decision != "deny" {
		t.Fatalf("unrelated product path escaped original Assignment boundary: %+v", d)
	}
}
