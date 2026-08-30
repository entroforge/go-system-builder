package hook_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// The observer never blocks and never errors; identification gaps produce a
// silent observation (L3-S7 §8 / L4 §7.4 platform-reality ladder).
func TestPostToolUseObservationLadder(t *testing.T) {
	agents := []hook.AgentRow{
		{ID: "agent-build-1", State: "working"},
		{ID: "agent-qa-1", State: "reading"},
	}

	cases := []struct {
		name       string
		input      policy.Input
		wantRecord bool
		wantAgent  string
	}{
		{
			name: "payload agent_id wins",
			input: policy.Input{
				ToolName: "SendMessage", AgentID: "agent-build-1",
				ToolInput: map[string]any{"message_type": "plan_report", "plan_ref": ".claude/plan-report.json"},
			},
			wantRecord: true, wantAgent: "agent-build-1",
		},
		{
			name: "teammate_name match",
			input: policy.Input{
				ToolName:  "SendMessage",
				ToolInput: map[string]any{"message_type": "plan_report", "plan_ref": ".claude/plan-report.json", "teammate_name": "agent-qa-1"},
			},
			wantRecord: true, wantAgent: "agent-qa-1",
		},
		{
			name: "unidentifiable sender stays silent with actionable reason",
			input: policy.Input{
				ToolName:  "SendMessage",
				ToolInput: map[string]any{"message_type": "blocker_report"},
			},
			wantRecord: false, // S7-12: no lifecycle-state guessing
		},
		{
			name: "missing agent_id reason is actionable",
			input: policy.Input{
				ToolName:  "SendMessage",
				ToolInput: map[string]any{"message_type": "plan_report", "plan_ref": ".claude/plan-report.json"},
			},
			wantRecord: false, // S7-12: no sole-reading fallback
		},
		{
			name: "unrelated tool passes silently",
			input: policy.Input{
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": "go test ./..."},
			},
			wantRecord: false,
		},
		{
			name: "unrelated message type passes silently",
			input: policy.Input{
				ToolName: "SendMessage", AgentID: "agent-build-1",
				ToolInput: map[string]any{"message_type": "chitchat"},
			},
			wantRecord: false,
		},
		{
			name: "plan report without authoritative file ref stays silent",
			input: policy.Input{
				ToolName: "SendMessage", AgentID: "agent-build-1",
				ToolInput: map[string]any{"message_type": "plan_report"},
			},
			wantRecord: false,
		},
		{
			name: "unidentifiable sender passes silently",
			input: policy.Input{
				ToolName:  "SendMessage",
				ToolInput: map[string]any{"message_type": "blocker_report"},
			},
			wantRecord: false, // S7-12: no sole waiting-agent fallback
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := hook.HandlePostToolUse(tc.input, agents)
			if obs.Recorded != tc.wantRecord {
				t.Fatalf("Recorded = %v, want %v (reason=%q)", obs.Recorded, tc.wantRecord, obs.Reason)
			}
			if tc.wantRecord && obs.AgentID != tc.wantAgent {
				t.Fatalf("AgentID = %q, want %q", obs.AgentID, tc.wantAgent)
			}
		})
	}
}

// Ambiguity must fail silent, never guess — even when exactly one agent is
// waiting on its plan checkpoint (S7-12: parallel dispatch makes the old
// "sole reading agent" heuristic unsafe).
func TestPostToolUseAmbiguousFallbackSilent(t *testing.T) {
	agents := []hook.AgentRow{
		{ID: "agent-a", State: "reading"},
		{ID: "agent-b", State: "understanding_submitted"},
	}
	obs := hook.HandlePostToolUse(policy.Input{
		ToolName:  "SendMessage",
		ToolInput: map[string]any{"message_type": "plan_report", "plan_ref": ".claude/plan-report.json"},
	}, agents)
	if obs.Recorded {
		t.Fatalf("ambiguous sender must not record, got %q", obs.AgentID)
	}
}

// S7-12: even a single waiting agent must not be guessed from lifecycle
// state; the reason must name the actionable fix instead.
func TestPostToolUseSoleWaitingAgentNotGuessed(t *testing.T) {
	obs := hook.HandlePostToolUse(policy.Input{
		ToolName:  "SendMessage",
		ToolInput: map[string]any{"message_type": "plan_report", "plan_ref": ".claude/plan-report.json"},
	}, []hook.AgentRow{{ID: "agent-solo", State: "reading"}})
	if obs.Recorded || obs.AgentID != "" {
		t.Fatalf("sole-reading fallback must not attribute a sender: %#v", obs)
	}
	if !strings.Contains(obs.Reason, "S7-12: missing agent_id") {
		t.Fatalf("reason must name S7-12 with the actionable fix, got %q", obs.Reason)
	}
}

func TestPostToolUseIgnoresAuthoringPlaceholderAgentID(t *testing.T) {
	obs := hook.HandlePostToolUse(policy.Input{
		ToolName: "SendMessage", AgentID: "TODO(planner):agent-id-for-qa",
		ToolInput: map[string]any{
			"message_type": "plan_report",
			"plan_ref":     ".claude/plan-report.json",
		},
	}, []hook.AgentRow{{ID: "TODO(planner):agent-id-for-qa", State: "reading"}})
	if obs.Recorded {
		t.Fatalf("placeholder identity must not be observed or auto-chained: %#v", obs)
	}
}

func TestPostToolUseFallsThroughMalformedAgentIDToVerifiedTeammate(t *testing.T) {
	obs := hook.HandlePostToolUse(policy.Input{
		ToolName:     "SendMessage",
		AgentID:      "TODO(planner):agent-id-for-qa",
		TeammateName: "agent-qa-real",
		ToolInput: map[string]any{
			"message_type": "plan_report",
			"plan_ref":     ".claude/evidence/plan.json",
		},
	}, []hook.AgentRow{{ID: "agent-qa-real", State: "reading"}})
	if !obs.Recorded || obs.AgentID != "agent-qa-real" {
		t.Fatalf("verified teammate identity should survive a malformed optional agent_id: %#v", obs)
	}
}
