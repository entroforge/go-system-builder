package policy_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// The official Claude Code 2.1.218 TeammateIdle payload (teammate_name /
// team_name / transcript_path, no agent_id) must parse losslessly into
// policy.Input so controller/policy rules identify the exact teammate
// instead of guessing (L4 §15.2 P0-1, §16.1 "无 agent_id").
func TestOfficialTeammateIdlePayloadParses(t *testing.T) {
	var input policy.Input
	if err := json.Unmarshal([]byte(`{
		"session_id": "sess-1",
		"hook_event_name": "TeammateIdle",
		"teammate_name": "builder-7",
		"team_name": "team-alpha",
		"transcript_path": "/tmp/claude/transcripts/builder-7.jsonl"
	}`), &input); err != nil {
		t.Fatalf("decode official TeammateIdle payload: %v", err)
	}
	if input.AgentID != "" {
		t.Fatalf("official TeammateIdle payload carries no agent_id, got %q", input.AgentID)
	}
	if input.TeammateName != "builder-7" || input.TeamName != "team-alpha" {
		t.Fatalf("teammate identity lost: %#v", input)
	}
	if input.TranscriptPath != "/tmp/claude/transcripts/builder-7.jsonl" {
		t.Fatalf("transcript_path lost: %q", input.TranscriptPath)
	}
	if got := input.EffectiveAgentID(); got != "builder-7" {
		t.Fatalf("EffectiveAgentID must fall back to teammate_name, got %q", got)
	}
}

// The official SubagentStop payload carries agent_id plus
// agent_transcript_path / last_assistant_message / stop_hook_active.
func TestOfficialSubagentStopPayloadParses(t *testing.T) {
	var input policy.Input
	if err := json.Unmarshal([]byte(`{
		"session_id": "sess-2",
		"hook_event_name": "SubagentStop",
		"agent_id": "builder-9",
		"transcript_path": "/tmp/claude/transcripts/main.jsonl",
		"agent_transcript_path": "/tmp/claude/transcripts/builder-9.jsonl",
		"last_assistant_message": "PLAN_REPORT: ...",
		"stop_hook_active": true
	}`), &input); err != nil {
		t.Fatalf("decode official SubagentStop payload: %v", err)
	}
	if input.AgentID != "builder-9" {
		t.Fatalf("agent_id lost: %q", input.AgentID)
	}
	if input.AgentTranscriptPath != "/tmp/claude/transcripts/builder-9.jsonl" {
		t.Fatalf("agent_transcript_path lost: %q", input.AgentTranscriptPath)
	}
	if input.LastAssistantMessage != "PLAN_REPORT: ..." {
		t.Fatalf("last_assistant_message lost: %q", input.LastAssistantMessage)
	}
	if !input.StopHookActive {
		t.Fatalf("stop_hook_active lost")
	}
	if got := input.EffectiveAgentID(); got != "builder-9" {
		t.Fatalf("EffectiveAgentID must prefer agent_id, got %q", got)
	}
}

// PreToolUse(TaskUpdate) self-claim guard (L4 §15.2 P0-5, §16.1): a
// teammate may not claim an undispatched Team task; owned-task updates pass.
func TestTaskUpdateSelfClaim(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	agent := &policy.AgentContext{ID: "builder-1", State: "working", TaskIDs: []string{"TASK-1"}}
	cases := []struct {
		name      string
		agent     *policy.AgentContext
		toolInput map[string]any
		wantBlock bool
	}{
		{"in_progress on undispatched task blocks", agent, map[string]any{"taskId": "TASK-2", "status": "in_progress"}, true},
		{"owner self-assignment blocks", agent, map[string]any{"taskId": "TASK-2", "owner": "builder-1"}, true},
		{"in_progress on own task allowed", agent, map[string]any{"taskId": "TASK-1", "status": "in_progress"}, false},
		{"completing own task allowed", agent, map[string]any{"taskId": "TASK-1", "status": "completed"}, false},
		{"completing unowned task blocks", agent, map[string]any{"taskId": "TASK-2", "status": "completed"}, true},
		{"main session (no agent) exempt", nil, map[string]any{"taskId": "TASK-2", "status": "in_progress"}, false},
		{"missing taskId blocks", agent, map[string]any{"status": "in_progress"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := engine.Evaluate(policy.Input{
				Event:     "PreToolUse",
				ToolName:  "TaskUpdate",
				ToolInput: tc.toolInput,
				Runtime:   policy.RuntimeContext{Agent: tc.agent},
			})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			gotDenied := (decision.Decision == "deny" || decision.Decision == "block") && decision.RuleID == policy.RuleUnauthorizedTaskSelfClaim
			if gotDenied != tc.wantBlock {
				t.Fatalf("wantBlock=%v got decision=%q rule=%q", tc.wantBlock, decision.Decision, decision.RuleID)
			}
		})
	}
}

// The first-write barrier recovery must route the agent through the
// plan_checkpoint path (SendMessage plan_report captured by the
// PostToolUse(SendMessage) observer), not the retired two-phase
// readback_submitted command (L4 §15.2 P0 / P1-3).
func TestFirstWriteBarrierGuidesPlanCheckpoint(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Write",
		ToolInput: map[string]any{"file_path": "internal/foo/bar.go"},
		Runtime: policy.RuntimeContext{
			Agent: &policy.AgentContext{ID: "a1", State: "reading", DispatchMode: "plan_checkpoint"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.Decision != "deny" {
		t.Fatalf("pre-plan write must deny, got %q", decision.Decision)
	}
	joined := ""
	for _, step := range decision.Recovery {
		joined += step + "\n"
	}
	if !strings.Contains(joined, "SendMessage") || !strings.Contains(joined, "plan_report") {
		t.Fatalf("recovery must guide the SendMessage plan_report checkpoint path, got %v", decision.Recovery)
	}
	if strings.Contains(joined, "readback_submitted") {
		t.Fatalf("recovery must not route through the retired readback_submitted command, got %v", decision.Recovery)
	}
}
