package cli

import (
	"github.com/entroforge/go-system-builder/internal/policy"
	"strings"
	"testing"
)

func TestDispatchGuidanceFailureRemainsAdvisory(t *testing.T) {
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "documents": []any{map[string]any{"kind": "dispatch_plan", "generation": 1}}}
	for _, event := range []string{"SessionStart", "PostToolUse"} {
		g := policy.Guidance{}
		applyDispatchGuidance(t.TempDir(), state, event, policy.Input{ToolName: "Agent"}, &g)
		if g.Blocked || g.HumanRequired || len(g.Automation) != 1 || !strings.Contains(g.Automation[0], "projection unavailable") {
			t.Fatalf("%s: %+v", event, g)
		}
	}
	g := policy.Guidance{}
	applyDispatchGuidance(t.TempDir(), state, "PreToolUse", policy.Input{}, &g)
	if len(g.Automation) > 0 {
		t.Fatal("ordinary tools must not repeatedly scan the plan")
	}
}

func TestDispatchGuidanceUsesParentContextCheckpoints(t *testing.T) {
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "documents": []any{map[string]any{"kind": "dispatch_plan", "generation": 1}}}
	for _, tc := range []struct {
		event string
		input policy.Input
	}{
		{"SubagentStop", policy.Input{}},
		{"TeammateIdle", policy.Input{}},
		{"SessionStart", policy.Input{AgentID: "worker"}},
		{"PostToolUse", policy.Input{ToolName: "Agent", ToolResponse: map[string]any{"status": "async_launched"}}},
		{"PostToolUse", policy.Input{ToolName: "Bash", ToolInput: map[string]any{"command": "go test ./..."}}},
	} {
		g := policy.Guidance{}
		applyDispatchGuidance(t.TempDir(), state, tc.event, tc.input, &g)
		if len(g.Automation) != 0 {
			t.Fatalf("unexpected delivery to %s %+v", tc.event, tc.input)
		}
	}
	g := policy.Guidance{}
	applyDispatchGuidance(t.TempDir(), state, "PostToolUse", policy.Input{ToolName: "Bash", ToolInput: map[string]any{"command": "loop-harness runtime task-integrate --task-id TASK-042-01"}}, &g)
	if len(g.Automation) != 1 {
		t.Fatal("missing post-integration checkpoint")
	}
}
