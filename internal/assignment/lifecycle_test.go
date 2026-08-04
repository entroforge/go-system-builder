package assignment_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestAdvanceAgentRequiresReadbackApprovalBeforeActivation(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "delivery", 7)
	entities := state["entities"].(map[string]any)
	entities["agents"] = []any{map[string]any{
		"id": "agent-ver-1", "role": "delivery-verifier", "state": "reading",
		"task_ids": []any{"TASK-012"}, "team_id": "workgroup-delivery-round-1",
		"definition_ref": ".claude/agents/delivery-verifier.md",
		"prompt_ref":     "manifest#assignment-ver-1", "readback_ref": nil,
		"activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-06-22T00:00:00Z",
	}}
	writeJSON(t, statePath, state)
	readbackPath := writeAgentExample(t, root, dir, "readback_response", "agent-ver-1", "TASK-012", 7)

	submitted, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-ver-1", Event: "readback_submitted",
		MessagePath: readbackPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: submitted.Revision, AgentID: "agent-ver-1", Event: "understanding_approved",
		MessagePath: readbackPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	activationPath := writeAgentExample(t, root, dir, "activation", "agent-ver-1", "TASK-012", approved.Revision)
	activated, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: approved.Revision, AgentID: "agent-ver-1", Event: "activation_sent",
		MessagePath: activationPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(activated.State)
	if err := semantic.ValidateRuntimeBytes(root, data); err != nil {
		t.Fatalf("Agent lifecycle produced invalid runtime: %v", err)
	}
}

func TestAdvanceAgentRejectsActivationBeforeApproval(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "delivery", 7)
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "agent-ver-1", "role": "delivery-verifier", "state": "reading",
		"task_ids": []any{"TASK-012"}, "team_id": "workgroup-delivery-round-1",
		"definition_ref": ".claude/agents/delivery-verifier.md",
		"prompt_ref":     "manifest#assignment-ver-1", "readback_ref": nil,
		"activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-06-22T00:00:00Z",
	}}
	writeJSON(t, statePath, state)
	activationPath := writeAgentExample(t, root, dir, "activation", "agent-ver-1", "TASK-012", 7)
	if _, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-ver-1", Event: "activation_sent",
		MessagePath: activationPath,
	}); err == nil {
		t.Fatal("expected activation-before-approval rejection")
	}
}

func writeAgentExample(t *testing.T, root, dir, messageType, agentID, taskID string, revision int) string {
	t.Helper()
	data, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message["message_type"] != messageType {
			continue
		}
		message["agent_id"] = agentID
		message["task_id"] = taskID
		message["runtime_id"] = "loop-REQ-002"
		message["expected_runtime_revision"] = float64(revision)
		path := filepath.Join(dir, messageType+".json")
		writeJSON(t, path, message)
		return path
	}
	t.Fatalf("missing example %s", messageType)
	return ""
}
