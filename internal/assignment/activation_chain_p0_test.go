// Activation follows the original readback message and responsibility.
package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// activationChainFixture drives an agent from reading to
// understanding_approved and returns the ready-to-send activation message
// path plus hooks to tamper with it.
type activationChainFixture struct {
	root       string
	statePath  string
	journal    string
	revision   int
	agentID    string
	activation string
}

func newActivationChainFixture(t *testing.T) activationChainFixture {
	t.Helper()
	root := assignmentTestRoot(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "building", nil, 7)
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "builder-chain-1", "role": "backend-builder", "state": "reading",
		"task_ids": []any{"TASK-001"}, "team_id": "workgroup-build-1",
		"definition_ref": ".claude/agents/backend-builder.md",
		"prompt_ref":     "manifest#assignment-builder-chain-1",
		"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-08-20T00:00:00Z",
	}}
	writeJSON(t, statePath, state)

	rev := 7
	readbackPath := writeAgentExample(t, root, dir, "readback_response", "builder-chain-1", "TASK-001", rev)
	submitted, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: rev, AgentID: "builder-chain-1", Event: "readback_submitted",
		MessagePath: readbackPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: submitted.Revision, AgentID: "builder-chain-1", Event: "understanding_approved",
		MessagePath: readbackPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	activationPath := writeAgentExample(t, root, dir, "activation", "builder-chain-1", "TASK-001", approved.Revision)
	return activationChainFixture{
		root: root, statePath: statePath, journal: journalPath,
		revision: approved.Revision, agentID: "builder-chain-1", activation: activationPath,
	}
}

func (f activationChainFixture) patchActivation(t *testing.T, mutate func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(f.activation)
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	mutate(message)
	writeJSON(t, f.activation, message)
}

func (f activationChainFixture) send(t *testing.T) error {
	t.Helper()
	_, err := assignment.AdvanceAgent(f.root, f.statePath, f.journal, assignment.AgentEventRequest{
		ExpectedRevision: f.revision, AgentID: f.agentID, Event: "activation_sent",
		MessagePath: f.activation,
	})
	return err
}

func TestActivationChainAcceptsOriginalReadback(t *testing.T) {
	f := newActivationChainFixture(t)
	if err := f.send(t); err != nil {
		t.Fatalf("consistent activation must pass: %v", err)
	}
}

func TestActivationChainRejectsWrongMessageID(t *testing.T) {
	f := newActivationChainFixture(t)
	f.patchActivation(t, func(message map[string]any) {
		message["approved_readback_message_id"] = "msg-not-the-readback"
	})
	err := f.send(t)
	if err == nil {
		t.Fatal("mismatched approved_readback_message_id must reject the activation")
	}
	if want := "approved_readback_message_id"; !contains(err.Error(), want) {
		t.Fatalf("error must name the field, got: %v", err)
	}
}

func TestExampleActivationReferencesOriginalReadback(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "schema", "assets", "agent-message.examples.json"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	var readback, activation map[string]any
	for _, message := range messages {
		switch message["message_type"] {
		case "readback_response":
			readback = message
		case "activation":
			activation = message
		}
	}
	if readback == nil || activation == nil || activation["approved_readback_message_id"] != readback["message_id"] {
		t.Fatalf("activation must reference the original readback message: %#v %#v", activation, readback)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
