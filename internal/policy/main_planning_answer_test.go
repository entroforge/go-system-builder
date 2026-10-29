package policy_test

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func evaluateMainPlanningAnswerWrite(t *testing.T, input policy.Input) policy.Decision {
	t.Helper()
	decision, blocked := policy.EvaluateAgentScoped(input)
	if blocked {
		return decision
	}
	return policy.Decision{Decision: "allow"}
}

func TestMainPlanningAnswerWriteRequiresExactCurrentMainGrant(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ stage, phase string }{{"S2", "design"}, {"S3", "contracts"}, {"S4", "tasks"}} {
		t.Run(tc.stage, func(t *testing.T) {
			answer := ".claude/submissions/loop-test/g1/" + tc.stage + "/e4/packet-current/Main/answer.json"
			main := policy.Input{
				Event: "PreToolUse", ToolName: "Write", CWD: root,
				ToolInput: map[string]any{"file_path": answer},
				Runtime: policy.RuntimeContext{
					PacketMode: true, MainPlanningAnswerPaths: []string{answer},
					RuntimeID: "loop-test", CurrentBaselineGeneration: 1,
					CurrentStage: tc.stage, CurrentState: "planning", CurrentPhase: tc.phase, ProjectRoot: root,
				},
			}
			for _, tool := range []string{"Write", "Edit"} {
				main.ToolName = tool
				if decision := evaluateMainPlanningAnswerWrite(t, main); decision.Decision != "allow" {
					t.Fatalf("exact current Main %s answer path should be allowed through %s, got %+v", tc.stage, tool, decision)
				}
			}
		})
	}

	answer := ".claude/submissions/loop-test/g1/S2/e4/packet-current/Main/answer.json"
	main := policy.Input{
		Event: "PreToolUse", ToolName: "Write", CWD: root,
		ToolInput: map[string]any{"file_path": answer},
		Runtime: policy.RuntimeContext{
			PacketMode: true, MainPlanningAnswerPaths: []string{answer},
			RuntimeID: "loop-test", CurrentBaselineGeneration: 1,
			CurrentStage: "S2", CurrentState: "planning", CurrentPhase: "design", ProjectRoot: root,
		},
	}

	for _, tc := range []struct {
		name   string
		mutate func(*policy.Input)
	}{
		{name: "other path", mutate: func(in *policy.Input) {
			in.ToolInput["file_path"] = filepath.Join(root, ".claude/submissions/other/answer.json")
		}},
		{name: "wrong lifecycle phase", mutate: func(in *policy.Input) { in.Runtime.CurrentPhase = "contracts" }},
		{name: "wrong projected stage", mutate: func(in *policy.Input) { in.Runtime.CurrentStage = "S3" }},
		{name: "worker agent id", mutate: func(in *policy.Input) { in.AgentID = "worker-1" }},
		{name: "worker teammate name", mutate: func(in *policy.Input) { in.TeammateName = "worker-1" }},
		{name: "worker runtime context", mutate: func(in *policy.Input) { in.Runtime.Agent = &policy.AgentContext{ID: "worker-1"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := main
			input.ToolInput = map[string]any{"file_path": answer}
			tc.mutate(&input)
			if decision := evaluateMainPlanningAnswerWrite(t, input); decision.Decision != "deny" {
				t.Fatalf("non-current or non-Main write must deny, got %+v", decision)
			}
		})
	}
	t.Run("packet mode off preserves legacy decision", func(t *testing.T) {
		input := main
		input.Runtime.PacketMode = false
		withGrant := evaluateMainPlanningAnswerWrite(t, input)
		input.Runtime.MainPlanningAnswerPaths = nil
		withoutGrant := evaluateMainPlanningAnswerWrite(t, input)
		if withGrant.Decision != withoutGrant.Decision || withGrant.RuleID != withoutGrant.RuleID {
			t.Fatalf("disabled packet grant changed legacy policy: with=%+v without=%+v", withGrant, withoutGrant)
		}
	})
}
