package hook_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// IsStopIdleEvent scopes the exit-2 platform control to exactly the two
// lifecycle events that support it.
func TestIsStopIdleEvent(t *testing.T) {
	for _, event := range []string{"TeammateIdle", "SubagentStop"} {
		if !hook.IsStopIdleEvent(event) {
			t.Fatalf("%s must be a stop/idle event", event)
		}
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "SessionStart", "SubagentStart", "PreCompact"} {
		if hook.IsStopIdleEvent(event) {
			t.Fatalf("%s must not be a stop/idle event", event)
		}
	}
}

// The official stop_hook_active loop guard: a SubagentStop fired while the
// agent is already continuing due to a previous stop hook is never blocked
// again — independent of runtime state.
func TestStopIdleDecisionHonorsStopHookActive(t *testing.T) {
	input := policy.Input{
		Event:          "SubagentStop",
		AgentID:        "builder-1",
		StopHookActive: true,
	}
	if decision, controlled := hook.StopIdleDecision(t.TempDir(), input); controlled {
		t.Fatalf("stop_hook_active must allow, got %#v", decision)
	}
}

// Fail-open: no agent identity in the payload, or an unreadable runtime,
// never fabricates a block.
func TestStopIdleDecisionFailsOpen(t *testing.T) {
	if _, controlled := hook.StopIdleDecision(t.TempDir(), policy.Input{Event: "TeammateIdle"}); controlled {
		t.Fatalf("payload without agent identity must fail open")
	}
	if _, controlled := hook.StopIdleDecision(t.TempDir(), policy.Input{Event: "TeammateIdle", TeammateName: "ghost"}); controlled {
		t.Fatalf("unreadable runtime must fail open")
	}
	if _, controlled := hook.StopIdleDecision(t.TempDir(), policy.Input{Event: "SessionStart", AgentID: "x"}); controlled {
		t.Fatalf("non-stop/idle events are out of scope")
	}
}

// The stderr feedback for a blocked stop/idle names the rule, the recovery
// and — when the Controller attached one — the guidance action.
func TestRenderStopBlockFeedback(t *testing.T) {
	decision := policy.Decision{
		Decision: "block",
		RuleID:   hook.RuleTeammateIdleResumeAssignment,
		Reason:   "teammate builder-1 went idle before its PLAN_REPORT checkpoint",
		Recovery: []string{"send the PLAN_REPORT via SendMessage", "continue the assignment"},
		Guidance: &policy.Guidance{Action: "resume the current assignment"},
	}
	body := hook.RenderStopBlockFeedback(decision)
	for _, needle := range []string{
		hook.RuleTeammateIdleResumeAssignment,
		"PLAN_REPORT",
		"Recovery: send the PLAN_REPORT via SendMessage",
		"Next: resume the current assignment.",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("feedback must contain %q, got %q", needle, body)
		}
	}
}

// The PostToolUse(SendMessage) observer must resolve the official top-level
// teammate_name when the payload carries no agent_id.
func TestIdentifySenderUsesOfficialTeammateName(t *testing.T) {
	input := policy.Input{
		Event:        "PostToolUse",
		ToolName:     "SendMessage",
		TeammateName: "builder-7",
		ToolInput:    map[string]any{"message_type": "plan_report", "plan_ref": ".claude/plan-report.json"},
	}
	obs := hook.HandlePostToolUse(input, []hook.AgentRow{{ID: "builder-7", State: "working"}})
	if !obs.Recorded || obs.AgentID != "builder-7" {
		t.Fatalf("top-level teammate_name must identify the sender, got %#v", obs)
	}
}
