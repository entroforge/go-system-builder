package policy_test

import (
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestLegacyPermissionCorpusAllows(t *testing.T) {
	engine := loadPolicyEngine(t)
	cases := []struct {
		name  string
		input policy.Input
	}{
		{
			name: "activation",
			input: policy.Input{
				Event:     "PreToolUse",
				AgentID:   "agent-reading",
				ToolName:  "Write",
				ToolInput: map[string]any{"file_path": "internal/example.go"},
			},
		},
		{
			name: "allowed tools and scope",
			input: policy.Input{
				Event:     "PreToolUse",
				AgentID:   "agent-working",
				ToolName:  "Edit",
				ToolInput: map[string]any{"file_path": "outside/old-scope.go"},
			},
		},
		{
			name: "command class",
			input: policy.Input{
				Event:     "PreToolUse",
				AgentID:   "agent-working",
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": "custom-build-tool --run"},
			},
		},
		{
			name: "team required for Task",
			input: policy.Input{
				Event:     "PreToolUse",
				ToolName:  "Task",
				ToolInput: map[string]any{"subagent_type": "backend-builder"},
			},
		},
		{
			name: "team required for Agent",
			input: policy.Input{
				Event:     "PreToolUse",
				ToolName:  "Agent",
				ToolInput: map[string]any{"subagent_type": "backend-builder"},
			},
		},
		{
			name: "runtime integrity",
			input: policy.Input{
				Event:    "PreToolUse",
				ToolName: "Write",
			},
		},
		{
			name: "UI prototype",
			input: policy.Input{
				Event:    "PreToolUse",
				ToolName: "Write",
			},
		},
		{
			name: "clean round",
			input: policy.Input{
				Event:     "PreToolUse",
				ToolName:  "Write",
				ToolInput: map[string]any{"file_path": "docs/reports/acceptance/ACC-039.md"},
			},
		},
		{
			name: "policy tamper",
			input: policy.Input{
				Event:     "PreToolUse",
				ToolName:  "Edit",
				ToolInput: map[string]any{"file_path": "docs/hook-policy.json"},
			},
		},
		{
			name: "subagent report",
			input: policy.Input{
				Event: "SubagentStop",
			},
		},
		{
			name: "assignment report",
			input: policy.Input{
				Event: "TeammateIdle",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := engine.Evaluate(tc.input)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if decision.Decision != "allow" {
				t.Fatalf("retired permission predicate must not enforce: %#v", decision)
			}
		})
	}
}

func TestOrdinaryCommandCorpusAllows(t *testing.T) {
	engine := loadPolicyEngine(t)
	commands := map[string]string{
		"git merge":           "git merge feature/req-039",
		"git push":            "git push origin main",
		"git tag":             "git tag v2.0.0",
		"GitHub merge":        "gh pr merge 39 --merge",
		"publish":             "npm publish",
		"deploy":              "kubectl apply -f deploy.yaml",
		"dependency mutation": "npm install example",
		"unknown":             "custom-release-tool --execute",
	}

	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			decision, err := engine.Evaluate(policy.Input{
				Event:     "PreToolUse",
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": command},
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if decision.Decision != "allow" {
				t.Fatalf("ordinary command must remain policy-allowed: %#v", decision)
			}
		})
	}
}

func TestHookPolicyLoadsInEnforceMode(t *testing.T) {
	if mode := loadPolicyEngine(t).Mode(); mode != "enforce" {
		t.Fatalf("policy mode = %q, want enforce", mode)
	}
}

func loadPolicyEngine(t *testing.T) *policy.Engine {
	t.Helper()
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	return engine
}
