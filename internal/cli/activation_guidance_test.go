package cli

import (
	"strings"
	"testing"
)

func activationGuidanceState(role string, packet bool) map[string]any {
	return map[string]any{
		"schema_version": "1.3.0",
		"configuration": map[string]any{
			"optimization": map[string]any{"packet": packet},
		},
		"entities": map[string]any{
			"agents": []any{map[string]any{"id": "agent-1", "role": role}},
		},
	}
}

func TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		role string
		want string
	}{
		{name: "reviewer", role: "qa", want: "exact ReviewResult questionnaire path supplied by the listener"},
		{name: "builder", role: "backend-builder", want: "exact Builder Result questionnaire path supplied by the listener"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := agentActivationNextMessage(activationGuidanceState(tc.role, true), "agent-1")
			if !strings.Contains(message, tc.want) || !strings.Contains(message, "fill and save") || !strings.Contains(message, "original consumer") {
				t.Fatalf("packet activation guidance lacks its supplied questionnaire flow: %s", message)
			}
			for _, stale := range []string{"Canonical ReviewResult", "review-result submit", "worktree-create", "worktree_path/branch/target_branch", "task-complete"} {
				if strings.Contains(message, stale) {
					t.Fatalf("packet activation guidance still requires duplicate command or metadata %q: %s", stale, message)
				}
			}
		})
	}
}

func TestActivationSentGuidanceKeepsExplicitLegacyInstructions(t *testing.T) {
	reviewer := agentActivationNextMessage(activationGuidanceState("qa", false), "agent-1")
	if !strings.Contains(reviewer, "runtime review-result submit") {
		t.Fatalf("legacy reviewer command path was not preserved: %s", reviewer)
	}
	builder := agentActivationNextMessage(activationGuidanceState("backend-builder", false), "agent-1")
	if !strings.Contains(builder, "runtime task-complete") {
		t.Fatalf("legacy Builder command path was not preserved: %s", builder)
	}
}
