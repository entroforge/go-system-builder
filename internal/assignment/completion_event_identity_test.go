// completion_event_identity_test.go pins the identity of a legacy Builder
// Result delivery (G18 §1/§1.3) and the domain outcome of a repeated
// delivery.
//
// The identity of a Builder Result names the responsibility and the intent:
// the reporting Agent and the delivery it registers. It never carries the
// Runtime write counter, which stays an audit coordinate of the Writer.
//
// The delivery identity is derived from the accepted delivery history, so a
// re-delivery of the same completion names a new identity and stays a
// legitimate replay. Two deliveries of the same completion that overlap in
// the field name the same identity, and the Runtime's own fact - that the
// identity is already registered - is the domain's outcome for the second
// one. The journal's global event-id uniqueness check must not answer first:
// the caller asked for the domain's decision, not for a storage mechanism.
package assignment_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// completionBuildingState is the smallest Runtime that accepts one Builder
// Result: a Builder Agent that works on one TASK, and the TASK in progress.
func completionBuildingState(t *testing.T, root string, revision int) map[string]any {
	t.Helper()
	state := activeState(t, root, "building", nil, revision)
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-08-20T00:00:00Z"}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-1", "role": "backend-builder", "state": "working",
			"task_ids": []any{"TASK-001"}, "team_id": "workgroup-build-1",
			"definition_ref": ".claude/agents/backend-builder.md",
			"prompt_ref":     "manifest#assignment-builder-1",
			"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-20T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-001", "state": "in_progress",
			"path": "docs/dev/tasks/TASK-001.md", "sha256": semanticSha(t, []byte("# TASK-001\n")),
			"owner_agent_ids": []any{"builder-1"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	return state
}

// completionDeliveryMessage writes one completion_report delivery. The
// summary names the delivery, so a test can tell the delivered envelopes
// apart on disk.
func completionDeliveryMessage(t *testing.T, dir, name, agentID, taskID, summary string) string {
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
		if message["message_type"] != "completion_report" {
			continue
		}
		message["agent_id"] = agentID
		message["task_id"] = taskID
		message["runtime_id"] = "loop-REQ-002"
		message["status"] = "completed"
		message["summary"] = summary
		message["checks"] = []any{
			map[string]any{"name": "unit", "command": "go test ./...", "result": "pass", "evidence_ref": nil},
		}
		message["scope_deviations"] = []any{}
		path := filepath.Join(dir, name)
		writeJSON(t, path, message)
		return path
	}
	t.Fatal("missing completion_report example")
	return ""
}

// completionJournalEvents reads the Runtime journal as the recorded events.
func completionJournalEvents(t *testing.T, journalPath string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	events := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

// TestCompleteTaskEventIdentityNamesTheDeliveryWithoutAWriteCounter proves
// the committed Builder Result identity names the responsibility and the
// intent, and carries no Runtime write counter (G18 §1).
func TestCompleteTaskEventIdentityNamesTheDeliveryWithoutAWriteCounter(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeJSON(t, statePath, completionBuildingState(t, root, 7))
	messagePath := completionDeliveryMessage(t, root, "completion.json", "builder-1", "TASK-001", "the builder result")

	if _, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1,
		AgentID:          "builder-1",
		MessagePath:      messagePath,
	}); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	events := completionJournalEvents(t, journalPath)
	if len(events) != 1 {
		t.Fatalf("journal events=%d, want 1", len(events))
	}
	event := events[0]
	wantEventID := "evt-complete-builder-1-ev-completion-TASK-001-g1"
	if event["event_id"] != wantEventID {
		t.Fatalf("event_id=%v, want %s", event["event_id"], wantEventID)
	}
	wantKey := "runtime:complete:builder-1:ev-completion-TASK-001-g1"
	if event["idempotency_key"] != wantKey {
		t.Fatalf("idempotency_key=%v, want %s", event["idempotency_key"], wantKey)
	}
	// The semantic coordinates are present: the responsibility (the reporting
	// Agent) and the intent (the delivery, which names the TASK and the
	// baseline generation it belongs to).
	for _, coordinate := range []string{"builder-1", "TASK-001", "g1"} {
		if !strings.Contains(wantEventID, coordinate) {
			t.Fatalf("event id %q does not name the coordinate %q", wantEventID, coordinate)
		}
	}
	// The Runtime write counter stays an audit coordinate of the Writer. The
	// commit moved the revision to 8, and no identity carries it.
	if strings.Contains(wantEventID, "-r8") || strings.Contains(wantKey, ":8") {
		t.Fatalf("completion identity leaked the Runtime write counter: event=%v", event)
	}
}

// TestCompleteTaskReDeliveryStaysAReplay proves a re-delivery of the same
// completion keeps its own outcome: the delivery identity escalates, the
// Runtime registers the new Result and the caller sees no error. The
// Precheck must not turn that replay into a refusal (G18 §1.3).
func TestCompleteTaskReDeliveryStaysAReplay(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeJSON(t, statePath, completionBuildingState(t, root, 7))
	messagePath := completionDeliveryMessage(t, root, "completion.json", "builder-1", "TASK-001", "the builder result")

	first, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1,
		AgentID:          "builder-1",
		MessagePath:      messagePath,
	})
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	second, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
		ExpectedRevision: -1,
		AgentID:          "builder-1",
		MessagePath:      messagePath,
	})
	if err != nil {
		t.Fatalf("a re-delivery of the same completion must stay a replay: %v", err)
	}
	items := second.State["evidence"].([]any)
	if len(items) != 2 {
		t.Fatalf("evidence entries=%d, want the first delivery and its escalated replay", len(items))
	}
	events := completionJournalEvents(t, journalPath)
	if len(events) != 2 {
		t.Fatalf("journal events=%d, want 2", len(events))
	}
	if events[0]["event_id"] == events[1]["event_id"] {
		t.Fatalf("the replay reused the delivery identity: %v", events[0]["event_id"])
	}
	if first.Revision+1 != second.Revision {
		t.Fatalf("revisions = %d then %d, want one bump per delivery", first.Revision, second.Revision)
	}
}

// TestCompleteTaskOverlappingDeliveryKeepsTheDomainRefusal proves that two
// deliveries of the same completion that overlap in the field keep the
// domain's own outcome.
//
// Both deliveries name the same delivery identity, because the identity is
// derived from the delivery history they both read. The journal's global
// event-id uniqueness check would answer the second one with a storage
// message, so the mutation's Precheck lets the domain's refusal reach the
// caller instead: the delivery identity is already registered.
//
// The refused delivery writes nothing: the Runtime revision moves once and
// the journal gains one event.
func TestCompleteTaskOverlappingDeliveryKeepsTheDomainRefusal(t *testing.T) {
	root := completionRoot(t)
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeJSON(t, statePath, completionBuildingState(t, root, 7))
	firstMessage := completionDeliveryMessage(t, root, "first.json", "builder-1", "TASK-001", "the first delivery")
	secondMessage := completionDeliveryMessage(t, root, "second.json", "builder-1", "TASK-001", "the second delivery")

	// The Writer lock is held while both deliveries read the Runtime, so
	// both name the same delivery identity. Releasing it lets one delivery
	// commit and lets the other one find that identity already registered.
	release, err := filelock.Acquire(context.Background(), statePath+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	var mutex sync.Mutex
	outcomes := map[string]error{}
	var group sync.WaitGroup
	defer func() {
		release()
		group.Wait()
	}()
	deliver := func(name, messagePath string) {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := assignment.CompleteTask(root, statePath, journalPath, assignment.CompletionRequest{
				ExpectedRevision: -1,
				AgentID:          "builder-1",
				MessagePath:      messagePath,
			})
			mutex.Lock()
			defer mutex.Unlock()
			outcomes[name] = err
		}()
	}
	deliver("first", firstMessage)
	waitForDeliveryEnvelope(t, root, "the first delivery", &mutex, outcomes, "first")
	deliver("second", secondMessage)
	waitForDeliveryEnvelope(t, root, "the second delivery", &mutex, outcomes, "second")
	release()
	group.Wait()

	mutex.Lock()
	firstErr, secondErr := outcomes["first"], outcomes["second"]
	mutex.Unlock()
	committed, refused := 0, 0
	for _, err := range []error{firstErr, secondErr} {
		switch {
		case err == nil:
			committed++
		case strings.Contains(err.Error(), "already exists in runtime journal"):
			t.Fatalf("the repeated delivery answered with the journal key collision: %v", err)
		case err.Error() == "evidence ev-completion-TASK-001-g1 is already registered":
			refused++
		default:
			t.Fatalf("unexpected delivery outcome: %v", err)
		}
	}
	if committed != 1 || refused != 1 {
		t.Fatalf("delivery outcomes: %d committed, %d refused, want one of each", committed, refused)
	}

	// The refused delivery wrote nothing: the Runtime revision moved once and
	// the journal gained exactly one event.
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(after, &state); err != nil {
		t.Fatal(err)
	}
	if revision, _ := state["revision"].(float64); int(revision) != 8 {
		t.Fatalf("revision=%v, want the single bump to 8", state["revision"])
	}
	events := completionJournalEvents(t, journalPath)
	if len(events) != 1 {
		t.Fatalf("journal events=%d, want exactly the committed delivery", len(events))
	}
	if events[0]["event_id"] != "evt-complete-builder-1-ev-completion-TASK-001-g1" {
		t.Fatalf("event_id=%v, want the delivery identity", events[0]["event_id"])
	}
	items, _ := state["evidence"].([]any)
	if len(items) != 1 {
		t.Fatalf("evidence entries=%d, want exactly the committed delivery", len(items))
	}
}

// waitForDeliveryEnvelope waits until the delivered envelope of one delivery
// is on disk. The envelope is written before the delivery reaches the Writer
// lock, so its presence proves that the delivery already named its delivery
// identity.
func waitForDeliveryEnvelope(
	t *testing.T,
	root, summary string,
	mutex *sync.Mutex,
	outcomes map[string]error,
	name string,
) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mutex.Lock()
		err, done := outcomes[name]
		mutex.Unlock()
		if done {
			t.Fatalf("the %s delivery ended before it named its delivery identity: %v", name, err)
		}
		if completionEnvelopeCarries(root, summary) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the %s delivery did not write its envelope", name)
}

// completionEnvelopeCarries reports whether any delivered envelope on disk
// carries the given summary.
func completionEnvelopeCarries(root, summary string) bool {
	found := false
	_ = filepath.WalkDir(filepath.Join(root, ".claude", "evidence"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || found || entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), summary) {
			found = true
		}
		return nil
	})
	return found
}
