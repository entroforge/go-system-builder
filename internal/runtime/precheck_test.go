package runtime_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

// errPrecheckSentinel is the domain outcome a Precheck returns. The store must
// hand it back unchanged, so the caller keeps the domain's own answer instead
// of the journal's global event-id collision message.
var errPrecheckSentinel = errors.New("task TASK-001 already registered")

// precheckEventID names one responsibility and one intent, so a repeated
// intent produces the same id and collides in the journal.
const precheckEventID = "evt-task-register-TASK-001"

// A Precheck that rejects a repeated intent owns the outcome: the domain error
// comes back unchanged and the journal's key collision never answers.
func TestStorePrecheckKeepsDomainErrorOverEventIDCollision(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := testWriter(statePath, journalPath)
	if _, err := store.Update(1, runtime.Mutation{
		EventID:        precheckEventID,
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-06-20T00:00:01Z"
			return nil
		},
	}); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}

	applyRan := false
	_, err := store.Update(2, runtime.Mutation{
		EventID:        precheckEventID,
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:2",
		Precheck: func(map[string]any) error {
			return errPrecheckSentinel
		},
		Apply: func(map[string]any) error {
			applyRan = true
			return nil
		},
	})
	if !errors.Is(err, errPrecheckSentinel) {
		t.Fatalf("error = %v, want the domain precheck error", err)
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("error = %v, want no journal event-id collision message", err)
	}
	if applyRan {
		t.Fatal("Apply must not run after a failing Precheck")
	}
}

// A rejected intent writes nothing: no journal event and no revision move.
func TestStorePrecheckFailureWritesNoJournalEventAndKeepsRevision(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := testWriter(statePath, journalPath)
	if _, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-task-register-TASK-002",
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-06-20T00:00:01Z"
			return nil
		},
	}); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Update(2, runtime.Mutation{
		EventID:        "evt-task-register-TASK-003",
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:2",
		Precheck: func(map[string]any) error {
			return errPrecheckSentinel
		},
	})
	if !errors.Is(err, errPrecheckSentinel) {
		t.Fatalf("error = %v, want the domain precheck error", err)
	}
	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatal("precheck failure changed state bytes")
	}
	if string(journalAfter) != string(journalBefore) {
		t.Fatal("precheck failure changed journal bytes")
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 2 {
		t.Fatalf("precheck failure moved the revision: got %d, want 2", snapshot.Revision)
	}
}

// The revision check outranks the domain judgment: a stale expected revision
// answers with the store's stale-revision error and the Precheck never runs.
func TestStorePrecheckRunsAfterRevisionCAS(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 2)

	store := testWriter(statePath, journalPath)
	precheckRan := false
	_, err := store.Update(1, runtime.Mutation{
		EventID:        precheckEventID,
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Precheck: func(map[string]any) error {
			precheckRan = true
			return errPrecheckSentinel
		},
	})
	if !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("error = %v, want ErrStaleRevision", err)
	}
	if precheckRan {
		t.Fatal("Precheck must not run before the revision check")
	}
}

// The Precheck sees the locked current state: the entities that exist before
// the mutation, in the same state map Apply later receives.
func TestStorePrecheckObservesLockedCurrentState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)
	writeStateTaskEntity(t, statePath, "TASK-001")

	store := testWriter(statePath, journalPath)
	var observed map[string]any
	var applyState map[string]any
	var observedRevision float64
	var observedTaskCount int
	var observedTaskID string
	var observedUpdatedAt string
	precheckRan := false
	next, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-precheck-observes-state",
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Precheck: func(state map[string]any) error {
			precheckRan = true
			observed = state
			observedRevision, _ = state["revision"].(float64)
			observedUpdatedAt, _ = state["updated_at"].(string)
			entities, _ := state["entities"].(map[string]any)
			tasks, _ := entities["tasks"].([]any)
			observedTaskCount = len(tasks)
			if len(tasks) > 0 {
				task, _ := tasks[0].(map[string]any)
				observedTaskID, _ = task["id"].(string)
			}
			return nil
		},
		Apply: func(state map[string]any) error {
			applyState = state
			state["updated_at"] = "2026-06-20T00:00:02Z"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if next.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", next.Revision)
	}
	if !precheckRan {
		t.Fatal("Precheck must survive normalization and run")
	}
	if observed == nil {
		t.Fatal("Precheck received no state")
	}
	if observedRevision != 1 {
		t.Fatalf("Precheck saw revision %v, want the locked revision 1", observedRevision)
	}
	if observedTaskCount != 1 || observedTaskID != "TASK-001" {
		t.Fatalf("Precheck saw %d tasks (last id %q), want the committed TASK-001", observedTaskCount, observedTaskID)
	}
	if observedUpdatedAt == "2026-06-20T00:00:02Z" {
		t.Fatal("Precheck must observe the state before Apply runs")
	}
	if reflect.ValueOf(observed).Pointer() != reflect.ValueOf(applyState).Pointer() {
		t.Fatal("Precheck must observe the same locked state map that Apply receives")
	}
}

// A mutation without a Precheck behaves exactly as before: it commits.
func TestStoreMutationWithoutPrecheckStillCommits(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := testWriter(statePath, journalPath)
	next, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-plain-commit",
		TransitionID:   "TR-TEST",
		Event:          "test_transition",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-06-20T00:00:03Z"
			return nil
		},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if next.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", next.Revision)
	}
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "evt-plain-commit") {
		t.Fatalf("journal is missing the committed event: %s", string(raw))
	}
}

// Without a Precheck the journal's global event-id uniqueness check still
// rejects a repeated event id.
func TestStoreEventIDCollisionStillRejectedWithoutPrecheck(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := testWriter(statePath, journalPath)
	if _, err := store.Update(1, runtime.Mutation{
		EventID:        precheckEventID,
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-06-20T00:00:01Z"
			return nil
		},
	}); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}

	_, err := store.Update(2, runtime.Mutation{
		EventID:        precheckEventID,
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:2",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-06-20T00:00:02Z"
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("error = %v, want the journal event-id collision", err)
	}
}

// The state/journal pair validation outranks the domain judgment too: a
// broken pair answers with the store's pair error and the Precheck never runs.
func TestStorePrecheckRunsAfterStateJournalPairValidation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := testWriter(statePath, journalPath)
	if _, err := store.Update(1, runtime.Mutation{
		EventID:        "evt-task-register-TASK-004",
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:1",
		Apply: func(state map[string]any) error {
			state["updated_at"] = "2026-06-20T00:00:01Z"
			return nil
		},
	}); err != nil {
		t.Fatalf("first commit failed: %v", err)
	}
	writeStateJournalCursor(t, statePath, 7)

	precheckRan := false
	_, err := store.Update(2, runtime.Mutation{
		EventID:        "evt-task-register-TASK-005",
		TransitionID:   "TR-TEST",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: "runtime:TR-TEST:2",
		Precheck: func(map[string]any) error {
			precheckRan = true
			return errPrecheckSentinel
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match state last_sequence") {
		t.Fatalf("error = %v, want the state/journal pair error", err)
	}
	if precheckRan {
		t.Fatal("Precheck must not run before the state/journal pair validation")
	}
}

// writeStateTaskEntity adds one TASK entity to an already written runtime
// state file so a Precheck can observe an entity that exists before the
// mutation.
func writeStateTaskEntity(t *testing.T, path, taskID string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	entities, _ := state["entities"].(map[string]any)
	if entities == nil {
		t.Fatal("state entities are required")
	}
	tasks, _ := entities["tasks"].([]any)
	entities["tasks"] = append(tasks, map[string]any{
		"id":              taskID,
		"state":           "candidate",
		"path":            "tasks/" + taskID + ".md",
		"owner_agent_ids": []any{},
	})
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeStateJournalCursor moves the state's journal cursor away from the
// journal tail so the state/journal pair validation rejects the pair.
func writeStateJournalCursor(t *testing.T, path string, lastSequence int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	journal, _ := state["journal"].(map[string]any)
	if journal == nil {
		t.Fatal("state journal is required")
	}
	journal["last_sequence"] = lastSequence
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
