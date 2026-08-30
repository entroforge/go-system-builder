package policy_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestRepairPlanningAndReproductionFreezeProductWrites(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	for _, phase := range []string{"planning", "reproducing"} {
		decision, err := engine.Evaluate(policy.Input{
			Event: "PreToolUse", ToolName: "Edit",
			ToolInput: map[string]any{"file_path": "internal/service.go"},
			Runtime:   policy.RuntimeContext{CurrentState: "bug_resolution", CurrentPhase: phase},
		})
		if err != nil {
			t.Fatalf("Evaluate(%s): %v", phase, err)
		}
		if decision.Decision != "deny" || decision.RuleID != policy.RuleRepairWriteBeforeExecution {
			t.Fatalf("S9 %s product write must deny, got %q (%s)", phase, decision.Decision, decision.RuleID)
		}
		if !strings.Contains(strings.Join(decision.Recovery, "\n"), "BeginRepairExecution") {
			t.Fatalf("S9 %s recovery must name the execution checkpoint: %v", phase, decision.Recovery)
		}
	}

	for _, path := range []string{".claude/review/repair/plan-reports/report.json", ".claude/evidence/plan.json", "docs/reports/repair.md"} {
		decision, err := engine.Evaluate(policy.Input{
			Event: "PreToolUse", ToolName: "Write",
			ToolInput: map[string]any{"file_path": path},
			Runtime:   policy.RuntimeContext{CurrentState: "bug_resolution", CurrentPhase: "planning"},
		})
		if err != nil {
			t.Fatalf("Evaluate(control %s): %v", path, err)
		}
		if decision.Decision == "block" {
			t.Fatalf("control artifact %s should remain writable, got %s", path, decision.RuleID)
		}
	}
}

func TestRepairExecutingWorkerIsBoundToAssignmentScope(t *testing.T) {
	input := policy.Input{
		Event: "PreToolUse", ToolName: "Edit",
		ToolInput: map[string]any{"file_path": "internal/service.go"},
		Runtime: policy.RuntimeContext{
			CurrentState: "bug_resolution", CurrentPhase: "fixing",
			Agent: &policy.AgentContext{
				ID: "builder-1", RepairAssignmentID: "repair-assignment-unit-1",
				RepairAllowedWritePaths: []string{"internal/"},
			},
		},
	}
	decision, blocked := policy.EvaluateAgentScoped(input)
	if blocked || decision.Decision != "" {
		t.Fatalf("in-scope repair write should pass the repair scope rule: blocked=%v decision=%#v", blocked, decision)
	}

	input.ToolInput["file_path"] = "web/app.tsx"
	decision, blocked = policy.EvaluateAgentScoped(input)
	if !blocked || decision.Decision != "deny" || decision.RuleID != policy.RuleRepairAssignmentScope {
		t.Fatalf("out-of-scope repair write must deny, blocked=%v decision=%#v", blocked, decision)
	}
	if !strings.Contains(strings.Join(decision.Recovery, "\n"), "scope deviation") {
		t.Fatalf("scope recovery must explain the correction path: %v", decision.Recovery)
	}

	input.ToolName = "Bash"
	input.ToolInput = map[string]any{"command": "python3 -c 'open(\"internal/service.go\", \"w\").write(\"x\")'"}
	decision, blocked = policy.EvaluateAgentScoped(input)
	if blocked || decision.Decision != "" {
		t.Fatalf("in-scope Bash repair write should pass: blocked=%v decision=%#v", blocked, decision)
	}

	input.ToolInput = map[string]any{"command": "python3 -c 'open(\"web/app.tsx\", \"w\").write(\"x\")'"}
	decision, blocked = policy.EvaluateAgentScoped(input)
	if !blocked || decision.Decision != "deny" || decision.RuleID != policy.RuleRepairAssignmentScope {
		t.Fatalf("out-of-scope Bash repair write must deny: blocked=%v decision=%#v", blocked, decision)
	}

}
