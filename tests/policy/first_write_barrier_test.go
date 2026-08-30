package policy_test

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// The L4 first-write barrier (assignment_write_before_plan): a dispatched
// Worker in a pre-plan state may not mutate the product surface before its
// PLAN_REPORT is recorded. The main session (no Agent context) is exempt.
func TestFirstWriteBarrier(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	write := map[string]any{"file_path": "internal/foo/bar.go"}
	cases := []struct {
		name      string
		agent     *policy.AgentContext
		tool      string
		wantBlock bool
	}{
		{"reading without plan blocks", &policy.AgentContext{ID: "a1", State: "reading", DispatchMode: "plan_checkpoint"}, "Write", true},
		{"spawned without plan blocks", &policy.AgentContext{ID: "a1", State: "spawned", DispatchMode: "plan_checkpoint"}, "Edit", true},
		{"plan recorded allows", &policy.AgentContext{ID: "a1", State: "reading", DispatchMode: "plan_checkpoint", PlanReportedRef: "plan_report:msg-1"}, "Write", false},
		{"working agent exempt", &policy.AgentContext{ID: "a1", State: "working", DispatchMode: "plan_checkpoint"}, "Write", false},
		{"one_shot exempt", &policy.AgentContext{ID: "a1", State: "reading", DispatchMode: "one_shot"}, "Write", false},
		{"main session (no agent) exempt", nil, "Write", false},
		{"Bash not covered", &policy.AgentContext{ID: "a1", State: "reading", DispatchMode: "plan_checkpoint"}, "Bash", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := policy.Input{
				Event:     "PreToolUse",
				ToolName:  tc.tool,
				ToolInput: write,
				Runtime:   policy.RuntimeContext{CurrentState: "building", ProjectRoot: "/repo", Agent: tc.agent},
			}
			decision, err := engine.Evaluate(input)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			gotDenied := (decision.Decision == "deny" || decision.Decision == "block") && decision.RuleID == policy.RuleAssignmentWriteBeforePlan
			if gotDenied != tc.wantBlock {
				t.Fatalf("wantDenied=%v got decision=%q rule=%q", tc.wantBlock, decision.Decision, decision.RuleID)
			}
		})
	}
}
