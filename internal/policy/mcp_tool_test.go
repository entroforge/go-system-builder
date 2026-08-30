package policy_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestUnknownMCPToolDoesNotSilentlyPassControlledHooks(t *testing.T) {
	engine := loadRepositoryPolicy(t)

	cases := []struct {
		name         string
		runtime      policy.RuntimeContext
		toolInput    map[string]any
		wantDecision string
		wantRule     string
		wantRecovery string
	}{
		{
			name:         "verification pathless MCP",
			runtime:      policy.RuntimeContext{CurrentState: "verification"},
			toolInput:    map[string]any{"operation": "write"},
			wantDecision: "deny",
			wantRule:     policy.RuleUnknownMCPTool,
			wantRecovery: "classify",
		},
		{
			name:         "worker pathless MCP",
			runtime:      policy.RuntimeContext{CurrentState: "building", Agent: &policy.AgentContext{ID: "builder-1", State: "working"}},
			toolInput:    map[string]any{"operation": "write"},
			wantDecision: "deny",
			wantRule:     policy.RuleUnknownMCPTool,
			wantRecovery: "allowed tool",
		},
		{
			name:         "main pathless MCP is visible",
			runtime:      policy.RuntimeContext{CurrentState: "building"},
			toolInput:    map[string]any{"operation": "write"},
			wantDecision: "warn",
			wantRule:     policy.RuleUnknownMCPTool,
			wantRecovery: "classify",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := engine.Evaluate(policy.Input{
				Event:     "PreToolUse",
				ToolName:  "mcp__example__mutate",
				ToolInput: tc.toolInput,
				Runtime:   tc.runtime,
			})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if decision.Decision != tc.wantDecision || decision.RuleID != tc.wantRule {
				t.Fatalf("decision = %q (%q), want %q (%q)", decision.Decision, decision.RuleID, tc.wantDecision, tc.wantRule)
			}
			if !strings.Contains(strings.ToLower(strings.Join(decision.Recovery, "\n")), tc.wantRecovery) {
				t.Fatalf("recovery = %v, want guidance containing %q", decision.Recovery, tc.wantRecovery)
			}
		})
	}
}

func TestMCPPathMutationReusesExistingVerificationScopeGate(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "mcp__example__write_file",
		ToolInput: map[string]any{"path": "internal/service.go"},
		Runtime: policy.RuntimeContext{
			CurrentState:          "verification",
			VerificationWorkspace: "e2e-workspace/review-1",
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Decision != "deny" || decision.RuleID != policy.RuleReviewerProductWrite {
		t.Fatalf("path-bearing MCP mutation must use reviewer scope gate, got %q (%s)", decision.Decision, decision.RuleID)
	}
}

func TestMCPReadOnlyAllowlistRemainsNonMutating(t *testing.T) {
	engine := loadRepositoryPolicy(t)
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "mcp__filesystem__read_file",
		ToolInput: map[string]any{"path": "internal/service.go"},
		Runtime:   policy.RuntimeContext{CurrentState: "verification"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Decision == "block" {
		t.Fatalf("known read-only MCP tool must not block, got %q (%s)", decision.Decision, decision.RuleID)
	}
}

func TestMCPToolNamesAreRecognizedByPolicyWithoutAPlatformRoundTrip(t *testing.T) {
	if !policy.IsMCPTool("mcp__filesystem__read_file") {
		t.Fatal("mcp tool name must be recognized")
	}
	if policy.IsMCPTool("Bash") {
		t.Fatal("built-in tool must not be classified as MCP")
	}
}
