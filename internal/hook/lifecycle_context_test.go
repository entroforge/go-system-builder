package hook_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestMainReceptionContextPrioritizesActionOverOptionalCandidateLists(t *testing.T) {
	action := "S6 current Main action: recover_integration assignment-A — recover original failed check"
	argv := "Original action argv: 'loop-harness' 'runtime' 'task-integrate' '--retry-preserved'"
	question := "Describe the required correction or environment recovery before choosing the original retry/rework route."
	context := hook.BuildLifecycleAdditionalContext("SessionStart", policy.Input{}, policy.Decision{Guidance: &policy.Guidance{
		Stage: "S6", Action: "complete Builder assignments", ProtocolRef: "docs/control/agent-protocol.md#s6",
		Automation: []string{"eligible tasks: " + strings.Repeat("非常长的候选清单", 900), action, argv, question},
	}}, nil)
	for _, required := range []string{action, argv, question} {
		if !strings.Contains(context, required) {
			t.Fatalf("optional candidates displaced current reception: %s", context)
		}
	}
	if !utf8.ValidString(context) || utf8.RuneCountInString(context) > 800 || strings.Contains(context, "eligible tasks:") {
		t.Fatalf("partial optional candidate list escaped one budget: %s", context)
	}
	failed := "S6 reception projection unavailable; inspect original workspace pending and checkpoint before choosing another dispatch"
	context = hook.BuildLifecycleAdditionalContext("SessionStart", policy.Input{}, policy.Decision{Guidance: &policy.Guidance{Stage: "S6", Action: "inspect original work", Automation: []string{failed}}}, nil)
	if !strings.Contains(context, failed) {
		t.Fatal("reception failure diagnosis disappeared from native Main context")
	}
}

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
	specific := payload["hookSpecificOutput"].(map[string]any)
	if got, _ := specific["additionalContext"].(string); !strings.Contains(got, "stage=S7 @ rev=4; next=inspect the review status") {
		t.Fatalf("additionalContext = %q", got)
	}
	if _, ok := payload["additionalContext"]; ok {
		t.Fatal("additionalContext must be nested under hookSpecificOutput")
	}
}

func TestBuildLifecycleAdditionalContextInjectsOnlyUniqueAssignment(t *testing.T) {
	decision := policy.Decision{Guidance: &policy.Guidance{
		Stage: "S7", Revision: 9, Action: "execute the assignment", ProtocolRef: "docs/control/agent-protocol.md#s7",
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
		Stage: "S2", Revision: 18, Action: "complete architecture", ProtocolRef: "docs/control/agent-protocol.md#s2",
	}}
	context := hook.BuildLifecycleAdditionalContext("SessionStart", policy.Input{Source: "resume"}, decision, nil)
	for _, expected := range []string{"source=resume", "stage=S2 @ rev=18", "next=complete architecture", "read=docs/control/agent-protocol.md#s2"} {
		if !strings.Contains(context, expected) {
			t.Fatalf("context %q does not contain %q", context, expected)
		}
	}
	if len(context) > 800 {
		t.Fatalf("SessionStart context must remain bounded, got %d bytes", len(context))
	}
}

func TestLifecycleBudgetPreservesOperationsAndUTF8(t *testing.T) {
	assignment := hookctx.AssignmentContext{AssignmentID: "assignment-中文", TaskID: "TASK-中文", OwnerAgentID: "owner", RoleFamily: "qa", ManifestRef: ".claude/workgroups/manifest.json", WritePaths: []string{strings.Repeat("非常长的范围😀", 900)}, RequiredChecks: []string{strings.Repeat("long check ", 200)}, DoneWhen: []string{strings.Repeat("具体完成条件", 600)}}
	decision := policy.Decision{Guidance: &policy.Guidance{Stage: "S7", Action: "read the exact operation", ProtocolRef: "docs/control/agent-protocol.md"}}
	delivery := "read=.claude/inputs/assignment-context.json; next=complete your original plan draft"
	context := hook.BuildLifecycleAdditionalContext("SubagentStart", policy.Input{AgentID: "owner"}, decision, []hookctx.AssignmentContext{assignment}, delivery)
	if !utf8.ValidString(context) || utf8.RuneCountInString(context) > 800 {
		t.Fatalf("bad Unicode budget %d: %q", utf8.RuneCountInString(context), context)
	}
	for _, value := range []string{"assignment_id=assignment-中文", "next=follow the responsibility delivery above", "read=.claude/workgroups/manifest.json", delivery} {
		if !strings.Contains(context, value) {
			t.Fatalf("scope displaced %s: %s", value, context)
		}
	}
	output, _, err := hook.RenderWithAdditionalContext("", "SubagentStart", policy.Decision{Decision: "info", Reason: strings.Repeat("very long guidance 中文", 1000)}, policy.RuntimeContext{}, context)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	json.Unmarshal(output, &envelope)
	actual := envelope["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	if actual != context || utf8.RuneCountInString(actual) > 800 {
		t.Fatal("final wire appended another unbounded budget")
	}
}

func TestLifecycleOversizedActionUsesWholeRuntimeEntrance(t *testing.T) {
	action := "runtime exact-command --root /" + strings.Repeat("非常长的路径", 900) + "/complete.json"
	context := hook.BuildLifecycleAdditionalContext("SessionStart", policy.Input{}, policy.Decision{Guidance: &policy.Guidance{Stage: "S5", Action: action, ProtocolRef: strings.Repeat("路径", 800)}}, nil)
	if !utf8.ValidString(context) || utf8.RuneCountInString(context) > 800 || !strings.Contains(context, "read=.claude/loop-state.json") || strings.Contains(context, "runtime exact-command") {
		t.Fatalf("partially executable action: %s", context)
	}
	invalid := hook.ComposeLifecycleContext("identity="+string([]byte{0xff, 0xfe}), "detail")
	if !utf8.ValidString(invalid) {
		t.Fatal("invalid upstream bytes escaped the wire")
	}
}
