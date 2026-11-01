package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// An issued old repair route can coexist with a current S7 responsibility.
// Its mere availability must not consume the Reviewer's original event.
func TestS7OriginalAnswerReachesConsumerWithOldS9Route(t *testing.T) {
	for _, event := range []string{"PreToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, packet := s7ListenerFixture(t)
			current := stageSnapshot(t, root)
			current.State["review"].(map[string]any)["repair"] = map[string]any{
				"session_id": "repair-session-old", "case_id": "investigation-case-old",
				"contract_id": "repair-contract-old", "contract_ref": "old-contract.json",
				"contract_sha256": strings.Repeat("0", 64), "path": "old-session.json",
				"sha256": strings.Repeat("0", 64), "revision": 1,
				"status": "targeted_reverification", "updated_at": "2026-10-30T00:00:00Z",
				"next_action": "Earlier repair responsibilities may finish independently",
			}
			stageJSON(t, root, ".claude/loop-state.json", current.State)
			s7ListenerAnswer(t, root, packet)
			ref := filepath.Join(root, packet.ViewPath)
			payload := s7ListenerPayload(t, packet.Binding.OwnerAgentID, "Review "+ref)
			if event == "SubagentStop" {
				payload = map[string]any{"agent_id": "actual-reviewer", "last_assistant_message": ref}
			}
			_, diagnostic := s7ListenerEvent(t, root, event, payload)
			state := stageSnapshot(t, root).State
			row := state["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)
			if row["result_receipt"] == nil {
				t.Fatalf("original S7 answer was not received: %s", diagnostic)
			}
		})
	}
}

// An asynchronous launch is not a completed return. Once the same actual
// dispatched task returns completed, Main's natural return can deliver its
// original ready answer even if no Worker event has received it yet.
func TestS7MainNaturalReturnReceivesOriginalOnlyAfterCompletion(t *testing.T) {
	root, packet := s7ListenerFixture(t)
	s7ListenerAnswer(t, root, packet)
	prompt := "Review " + filepath.Join(root, packet.ViewPath)
	fields := map[string]any{
		"agent_id": "Main", "tool_name": "Agent",
		"tool_input":    map[string]any{"prompt": prompt},
		"tool_response": map[string]any{"status": "async_launched", "isAsync": true},
	}
	out, _ := s7ListenerEvent(t, root, "PostToolUse", fields)
	if !strings.Contains(out, "launched") {
		t.Fatalf("background launch not reported as such: %s", out)
	}
	state := stageSnapshot(t, root).State
	row := state["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)
	if row["result_receipt"] != nil {
		t.Fatal("background launch consumed the original answer")
	}
	fields["tool_response"] = map[string]any{"status": "completed"}
	out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", fields)
	state = stageSnapshot(t, root).State
	row = state["review"].(map[string]any)["assignments"].(map[string]any)[packet.Binding.AssignmentID].(map[string]any)
	if row["result_receipt"] == nil {
		t.Fatalf("completed return did not receive original answer: %s %s", out, diagnostic)
	}
}
