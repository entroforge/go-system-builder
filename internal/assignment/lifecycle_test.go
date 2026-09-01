package assignment_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	state := activeState(t, root, "verification", "running", 7)
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

func TestAdvanceAgentAcceptsMessageWithoutRuntimeRevision(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 7)
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "agent-ver-1", "role": "delivery-verifier", "state": "reading",
		"task_ids": []any{"TASK-012"}, "team_id": "workgroup-delivery-round-1",
		"definition_ref": "agents/delivery-verifier.md", "prompt_ref": "manifest#assignment-ver-1",
		"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-06-22T00:00:00Z",
	}}
	writeJSON(t, statePath, state)
	messagePath := writeAgentExample(t, root, dir, "readback_response", "agent-ver-1", "TASK-012", 7)
	messageData, err := os.ReadFile(messagePath)
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := json.Unmarshal(messageData, &message); err != nil {
		t.Fatal(err)
	}
	delete(message, "expected_runtime_revision")
	messageData, err = json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messagePath, messageData, 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: -1, AgentID: "agent-ver-1", Event: "readback_submitted", MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("readback without Runtime revision failed: %v", err)
	}
	if snapshot.Revision != 8 {
		t.Fatalf("snapshot revision = %d, want 8", snapshot.Revision)
	}
}

func TestAdvanceAgentRejectsAuthoringPlaceholderAgentID(t *testing.T) {
	_, err := assignment.AdvanceAgent(t.TempDir(), "loop-state.json", "loop-events.jsonl", assignment.AgentEventRequest{
		AgentID: "TODO(planner):agent-id-for-qa",
		Event:   "work_started",
	})
	if err == nil || !strings.Contains(err.Error(), "authoring placeholder") {
		t.Fatalf("expected authoring-placeholder rejection before message lookup, got %v", err)
	}
}

func TestAdvanceAgentRejectsActivationBeforeApproval(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 7)
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

func TestBlockerResolvedReturnsReviewerToWorking(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 7)
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "agent-ver-1", "role": "qa", "state": "blocked",
		"task_ids": []any{"TASK-012"}, "team_id": "workgroup-review-1",
		"definition_ref": "agents/qa.md", "prompt_ref": "manifest#assignment-qa-1",
		"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-08-24T00:00:00Z",
	}}
	writeJSON(t, statePath, state)
	messagePath := filepath.Join(dir, "blocker-resolution.json")
	writeJSON(t, messagePath, map[string]any{
		"schema_version": "1.0.0", "message_type": "blocker_resolution",
		"message_id": "msg-blocker-resolution-1", "correlation_id": "corr-blocker-1",
		"runtime_id": "loop-REQ-002", "expected_runtime_revision": 7,
		"agent_id": "agent-ver-1", "agent_definition_ref": "agents/qa.md",
		"task_id": "TASK-012", "bug_id": nil, "team_id": "workgroup-review-1",
		"occurred_at": "2026-08-24T00:00:00Z", "body": "capture conditions restored",
	})
	snapshot, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-ver-1", Event: "blocker_resolved", MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("blocker_resolved: %v", err)
	}
	agent := snapshot.State["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
	if agent["state"] != "working" {
		t.Fatalf("blocker_resolved must resume work directly for review recovery, got %v", agent["state"])
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
		// The runtime verifies the activation chain against the readback
		// file bytes (L3-S6 complexity pass): patch the hash to the file
		// this helper actually wrote.
		if messageType == "activation" {
			readbackData, err := os.ReadFile(filepath.Join(dir, "readback_response.json"))
			if err == nil {
				sum := sha256.Sum256(readbackData)
				message["approved_readback_sha256"] = hex.EncodeToString(sum[:])
			}
		}
		path := filepath.Join(dir, messageType+".json")
		writeJSON(t, path, message)
		return path
	}
	t.Fatalf("missing example %s", messageType)
	return ""
}
