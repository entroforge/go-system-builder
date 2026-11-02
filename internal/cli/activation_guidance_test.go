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

// TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode pins that the
// packet-mode activation guidance points at the exact questionnaire the
// listener supplies and at the original consumer that records the answer.
// L3-S7-verification-round.md:223 — "PreToolUse、成功 PostToolUse、SubagentStop
// 使用同一接收路径观察自然保存的原答卷；不要求制造第二次保存、返回引用或无关
// 工具事件"; L4-workflow-convergence.md:312 removes the retired CLI and its
// duplicate-command wiring.
func TestActivationSentGuidanceUsesExactQuestionnaireInPacketMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		role string
		want string
	}{
		{name: "reviewer", role: "qa", want: "exact ReviewResult questionnaire supplied by the listener"},
		{name: "builder", role: "backend-builder", want: "exact Builder Result questionnaire supplied by the listener"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := agentActivationNextMessage(activationGuidanceState(tc.role, true), "agent-1")
			if !strings.Contains(message, tc.want) {
				t.Fatalf("packet activation guidance lacks its supplied questionnaire flow: %s", message)
			}
			if tc.role == "qa" && !strings.Contains(message, "the original consumer records your answer") {
				t.Fatalf("packet activation guidance lost the original consumer: %s", message)
			}
			if tc.role == "backend-builder" && !strings.Contains(message, "Main handles integration and required checks") {
				t.Fatalf("packet activation guidance lost the Main successor: %s", message)
			}
			for _, stale := range []string{"Canonical ReviewResult", "review-result submit", "worktree-create", "worktree_path/branch/target_branch", "task-complete"} {
				if strings.Contains(message, stale) {
					t.Fatalf("packet activation guidance still requires duplicate command or metadata %q: %s", stale, message)
				}
			}
		})
	}
}

// TestActivationSentGuidanceDropsRetiredCommandPaths pins that the retired
// reviewer and Builder command paths stay out of the activation guidance in
// every mode. The old commands were removed with the legacy wiring —
// L4-workflow-convergence.md:312 — "旧入口、旧 schema、旧 Hook／CLI、旧消费者及
// 兼容适配连同仅为它们存在的测试一并删除"; L4-workflow-convergence.md:71 —
// "删除已被现行责任链取代的旧入口、适配、消费者、格式和测试".
func TestActivationSentGuidanceDropsRetiredCommandPaths(t *testing.T) {
	reviewer := agentActivationNextMessage(activationGuidanceState("qa", false), "agent-1")
	if strings.Contains(reviewer, "runtime review-result submit") {
		t.Fatalf("retired reviewer command path reappeared: %s", reviewer)
	}
	if !strings.Contains(reviewer, "exact ReviewResult questionnaire supplied by the listener") || !strings.Contains(reviewer, "the original consumer records your answer") {
		t.Fatalf("reviewer guidance lost the listener-supplied questionnaire flow: %s", reviewer)
	}
	builder := agentActivationNextMessage(activationGuidanceState("backend-builder", false), "agent-1")
	if strings.Contains(builder, "runtime task-complete") {
		t.Fatalf("retired Builder command path reappeared: %s", builder)
	}
	if !strings.Contains(builder, "exact Builder Result questionnaire supplied by the listener") || !strings.Contains(builder, "Main handles integration and required checks") {
		t.Fatalf("Builder guidance lost the listener-supplied questionnaire flow: %s", builder)
	}
}
