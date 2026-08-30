package hook_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestRenderWithAdditionalContextUsesNativeLifecycleField(t *testing.T) {
	output, code, err := hook.RenderWithAdditionalContext("", "SessionStart", policy.Decision{
		Decision: "info",
		Reason:   "resume the current stage",
	}, policy.RuntimeContext{}, "stage=S7 @ rev=4; next=inspect the review status")
	if err != nil || code != 0 {
		t.Fatalf("render lifecycle context: code=%d err=%v", code, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatal(err)
	}
	if got, _ := payload["additionalContext"].(string); got != "stage=S7 @ rev=4; next=inspect the review status" {
		t.Fatalf("additionalContext = %q", got)
	}
	if _, ok := payload["hookSpecificOutput"]; ok {
		t.Fatal("SessionStart must keep the native lifecycle envelope")
	}
}

func TestBuildLifecycleAdditionalContextInjectsOnlyUniqueAssignment(t *testing.T) {
	decision := policy.Decision{Guidance: &policy.Guidance{
		Stage: "S7", Revision: 9, Action: "execute the assignment", ProtocolRef: "docs/agent-protocol.md#s7",
	}}
	assignment := hookctx.AssignmentContext{
		AssignmentID: "assignment-qa-1", TaskID: "task-qa-1", OwnerAgentID: "agent-qa-1",
		RoleFamily: "qa", AgentDefinitionRef: ".claude/agents/qa.md",
		WritePaths: []string{"internal/api", "docs/reports"}, RequiredChecks: []string{"go test ./..."},
		DoneWhen: []string{"register every QA Result with exact Claim evidence", "leave no unexplained affected surface"},
	}
	context := hook.BuildLifecycleAdditionalContext("SubagentStart", policy.Input{AgentType: "qa"}, decision, []hookctx.AssignmentContext{assignment})
	for _, expected := range []string{"assignment_id=assignment-qa-1", "scope=internal/api,docs/reports", "done_when=register every QA Result with exact Claim evidence; leave no unexplained affected surface", "required_checks=go test ./..."} {
		if !strings.Contains(context, expected) {
			t.Fatalf("context %q does not contain %q", context, expected)
		}
	}

	ambiguous := hook.BuildLifecycleAdditionalContext("SubagentStart", policy.Input{AgentType: "qa"}, decision, []hookctx.AssignmentContext{
		assignment,
		{AssignmentID: "assignment-qa-2", TaskID: "task-qa-2", RoleFamily: "qa", AgentDefinitionRef: ".claude/agents/qa.md"},
	})
	if ambiguous != "" {
		t.Fatalf("ambiguous assignment must not be guessed: %q", ambiguous)
	}
}

func TestBuildLifecycleAdditionalContextKeepsSessionStartCompact(t *testing.T) {
	decision := policy.Decision{Guidance: &policy.Guidance{
		Stage: "S2", Revision: 18, Action: "complete architecture", ProtocolRef: "docs/agent-protocol.md#s2",
	}}
	context := hook.BuildLifecycleAdditionalContext("SessionStart", policy.Input{Source: "resume"}, decision, nil)
	for _, expected := range []string{"source=resume", "stage=S2 @ rev=18", "next=complete architecture", "read=docs/agent-protocol.md#s2"} {
		if !strings.Contains(context, expected) {
			t.Fatalf("context %q does not contain %q", context, expected)
		}
	}
	if len(context) > 800 {
		t.Fatalf("SessionStart context must remain bounded, got %d bytes", len(context))
	}
}
