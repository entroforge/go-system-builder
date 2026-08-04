package hook_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestRenderGuidanceKeepsGateRecoveryActionable(t *testing.T) {
	output, code, err := hook.RenderWithRoot("", "PreToolUse", policy.Decision{
		Decision: "deny",
		RuleID:   "HOOK_AGENT_NOT_ACTIVATED",
		Reason:   "the Agent is not activated",
		Missing:  []string{"activation"},
		Recovery: []string{"approve readback"},
		Retry:    "rerun after recovery validation",
		Guidance: &policy.Guidance{
			Stage:          "S6",
			LifecycleState: "building",
			Revision:       8,
			Action:         "complete Builder assignments",
			ProtocolRef:    "docs/agent-protocol.md#s6",
			ManualRef:      ".claude/bin/loop-harness.md",
			Missing:        []string{"builder_completion_reports"},
			ReadOrder:      []string{"LOOP RECOVERY packet (this message)", "AGENTS.md", "docs/agent-protocol.md#s6"},
			Questions:      []string{"Is a single subagent necessary, or should this responsibility use an Agent Team?"},
			Automation:     []string{"do not call loop-harness for normal continuation"},
			Integration:    []string{"merge the worktree branch back into develop"},
		},
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("deny render should preserve Hook protocol exit 0, got %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatal(err)
	}
	body, _ := payload["systemMessage"].(string)
	for _, fragment := range []string{
		"HOOK_AGENT_NOT_ACTIVATED",
		"LOOP RECOVERY",
		"Next: complete Builder assignments",
		"docs/agent-protocol.md#s6",
		".claude/bin/loop-harness.md",
		"Read in order",
		"Agent Team",
		"do not call loop-harness for normal continuation",
		"merge the worktree branch back into develop",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("guidance output missing %q: %s", fragment, body)
		}
	}
}

func TestRenderGuidanceForAllowedLifecycleEvent(t *testing.T) {
	output, code, err := hook.RenderWithRoot("", "SubagentStop", policy.Decision{
		Decision: "allow",
		Guidance: &policy.Guidance{
			Stage:          "S6",
			LifecycleState: "building",
			Revision:       9,
			Objective:      "implement the locked TASK batch",
			Action:         "integrate the subagent worktree before acknowledging stop",
			ProtocolRef:    "docs/agent-protocol.md#s6",
			ManualRef:      ".claude/bin/loop-harness.md",
			Integration:    []string{"merge the worktree branch back into develop"},
		},
	}, policy.RuntimeContext{})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("allowed lifecycle guidance should render successfully, got %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatal(err)
	}
	body, _ := payload["systemMessage"].(string)
	if !strings.Contains(body, "integrate the subagent worktree") || !strings.Contains(body, "develop") {
		t.Fatalf("allowed lifecycle event must still emit positive guidance: %s", body)
	}
}
