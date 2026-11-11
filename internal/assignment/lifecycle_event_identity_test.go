package assignment_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// G18 §1/§3.2: the Agent lifecycle event id names the owner, the operation
// and the authored message it answers, so the same intent presented twice
// produces the same event id. The mutation's Precheck refuses the repeat
// under the store lock with the domain's own outcome, and the refused
// repeat writes nothing: the Runtime revision stays where the committed
// event left it and the journal gains no event.

// TestAgentLifecycleRepeatRefusalWritesNothing proves the refusal of a
// repeated Agent lifecycle event writes nothing. The Runtime revision does
// not move and the journal gains no event.
func TestAgentLifecycleRepeatRefusalWritesNothing(t *testing.T) {
	root := assignmentTestRoot(t)
	dir := t.TempDir()
	statePath, journalPath := filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 7)
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "agent-ver-1", "role": "delivery-verifier", "state": "reading",
		"task_ids": []any{"TASK-012"}, "team_id": "workgroup-delivery-round-1",
		"definition_ref": ".claude/agents/delivery-verifier.md", "prompt_ref": "manifest#assignment-ver-1",
		"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-06-22T00:00:00Z",
	}}
	writeJSON(t, statePath, state)
	messagePath := writeAgentMessage(t, dir, "readback.json", "readback_response", "agent-ver-1", "TASK-012", "msg-shared-intent", 7)

	committed, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-ver-1", Event: "readback_submitted", MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("original Agent lifecycle Writer: %v", err)
	}
	events := readJournalEvents(t, journalPath)
	if len(events) != 1 {
		t.Fatalf("journal events=%d after the original event, want 1", len(events))
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: committed.Revision, AgentID: "agent-ver-1", Event: "readback_submitted", MessagePath: messagePath,
	})
	if err == nil {
		t.Fatal("the repeated Agent lifecycle event must be refused")
	}
	if !strings.Contains(err.Error(), "Agent event readback_submitted is not legal from state understanding_submitted") {
		t.Fatalf("repeat error=%v, want the domain's own refusal", err)
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("the journal event-id collision shadowed the domain outcome: %v", err)
	}

	// The refused repeat writes nothing: the Runtime revision stays where the
	// committed event left it and the journal gains no event.
	var committedState map[string]any
	if err := json.Unmarshal(stateBefore, &committedState); err != nil {
		t.Fatal(err)
	}
	if revision, _ := committedState["revision"].(float64); int(revision) != committed.Revision {
		t.Fatalf("the committed Runtime revision is %v, want %d", committedState["revision"], committed.Revision)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("the refused repeat changed the Runtime state:\nbefore %s\nafter %s", stateBefore, stateAfter)
	}
	var refusedState map[string]any
	if err := json.Unmarshal(stateAfter, &refusedState); err != nil {
		t.Fatal(err)
	}
	if revision, _ := refusedState["revision"].(float64); int(revision) != committed.Revision {
		t.Fatalf("the refused repeat moved the Runtime revision to %v, want %d", refusedState["revision"], committed.Revision)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(journalAfter) != string(journalBefore) {
		t.Fatalf("the refused repeat added a journal event:\nbefore %s\nafter %s", journalBefore, journalAfter)
	}
	events = readJournalEvents(t, journalPath)
	if len(events) != 1 {
		t.Fatalf("journal events=%d after the refused repeat, want 1", len(events))
	}
}
