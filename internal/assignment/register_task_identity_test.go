package assignment_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// G18 §1/§3.1: a TASK registration is named by the registered TASK and the
// canonical task document it registers, so its event id carries no Runtime
// write counter. The journal's global event-id uniqueness check therefore runs
// before the Apply closure that carries the duplicate rejection.
//
// A repeated registration of the same TASK must keep its original observable
// outcome: the domain judgment decides (ErrDuplicateTaskID), never the journal
// collision, and the repeat writes no Runtime state and no journal fact.
func TestRegisterTaskDuplicateKeepsDomainOutcomeAndWritesNothing(t *testing.T) {
	root := assignmentTestRoot(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "planning", "tasks", 0)
	state["entities"].(map[string]any)["agents"] = []any{map[string]any{
		"id": "agent-builder-1", "role": "backend-builder", "state": "reading",
		"task_ids": []any{"TASK-100"}, "team_id": "workgroup-build-1",
		"definition_ref": ".claude/agents/backend-builder.md",
		"prompt_ref":     "manifest#assignment-agent-builder-1",
		"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
		"updated_at": "2026-08-20T00:00:00Z",
	}}
	writeJSON(t, statePath, state)

	taskRel := "docs/dev/tasks/TASK-100.md"
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(taskRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskRel), []byte("# TASK-100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := assignment.RegisterTaskRequest{
		ExpectedRevision: 0,
		TaskID:           "TASK-100",
		Path:             taskRel,
		OwnerAgentIDs:    []string{"agent-builder-1"},
		SourceContractID: "BUG-003",
	}
	first, err := assignment.RegisterTask(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("original TASK registration: %v", err)
	}
	if got := len(first.State["entities"].(map[string]any)["tasks"].([]any)); got != 1 {
		t.Fatalf("original registration recorded %d TASKs, want 1", got)
	}

	// The repeat names the Runtime revision the caller now holds. The
	// registration is still refused, and the refusal is the domain one.
	request.ExpectedRevision = 1
	_, err = assignment.RegisterTask(root, statePath, journalPath, request)
	var duplicate *assignment.ErrDuplicateTaskID
	if !errors.As(err, &duplicate) {
		t.Fatalf("repeated TASK registration must keep the domain outcome, got %T: %v", err, err)
	}
	if duplicate.TaskID != "TASK-100" {
		t.Fatalf("duplicate outcome names %q, want TASK-100", duplicate.TaskID)
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("the journal event-id collision shadowed the domain outcome: %v", err)
	}

	// The repeat wrote nothing: one TASK, one revision, one journal event.
	after := readRuntimeState(t, statePath)
	if got := len(after["entities"].(map[string]any)["tasks"].([]any)); got != 1 {
		t.Fatalf("repeated registration recorded %d TASKs, want 1", got)
	}
	if revision, _ := after["revision"].(float64); int(revision) != 1 {
		t.Fatalf("repeated registration moved the Runtime revision to %v, want 1", revision)
	}
	events := readJournalEvents(t, journalPath)
	registrations := 0
	for _, event := range events {
		if event["transition_id"] != "ENTITY-REGISTER" {
			continue
		}
		registrations++
		eventID, _ := event["event_id"].(string)
		if eventID != "evt-register-task-TASK-100" {
			t.Fatalf("TASK registration event id = %q, want evt-register-task-TASK-100", eventID)
		}
	}
	if registrations != 1 {
		t.Fatalf("journal recorded %d TASK registrations, want 1", registrations)
	}
}

// readRuntimeState reads the committed Runtime state without going through
// any write path.
func readRuntimeState(t *testing.T, statePath string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
