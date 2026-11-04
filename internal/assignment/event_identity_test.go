package assignment_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// G18 §1/§1.3: an event id and an idempotency key are built from the
// responsibility and the intent, never from the Runtime write counter. These
// tests pin the new shapes and the two reverse assertions the blueprint asks
// for: the same intent keeps the same identity, and two responsibilities
// never share one.

func readJournalEvents(t *testing.T, journalPath string) []map[string]any {
	t.Helper()
	file, err := os.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	events := []map[string]any{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func writeAgentMessage(t *testing.T, dir, name, messageType, agentID, taskID, messageID string, revision int) string {
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
		message["message_id"] = messageID
		path := filepath.Join(dir, name)
		writeJSON(t, path, message)
		return path
	}
	t.Fatalf("missing example %s", messageType)
	return ""
}

// TestAgentLifecycleEventIdentityIsSemantic proves the committed Agent
// lifecycle identity names the owner, the operation and the authored message
// and carries no Runtime write counter (G18 §1).
func TestAgentLifecycleEventIdentityIsSemantic(t *testing.T) {
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

	next, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-ver-1", Event: "readback_submitted", MessagePath: messagePath,
	})
	if err != nil {
		t.Fatalf("original Agent lifecycle Writer: %v", err)
	}
	events := readJournalEvents(t, journalPath)
	if len(events) != 1 {
		t.Fatalf("journal events=%d, want 1", len(events))
	}
	event := events[0]
	if event["event_id"] != "evt-agent-agent-ver-1-readback_submitted-msg-shared-intent" {
		t.Fatalf("event_id=%v, want the semantic owner/operation/intent shape", event["event_id"])
	}
	if event["idempotency_key"] != "runtime:agent:agent-ver-1:readback_submitted:msg-shared-intent" {
		t.Fatalf("idempotency_key=%v, want the semantic owner/operation/intent shape", event["idempotency_key"])
	}
	// The Runtime write counter stays a recorded audit coordinate only.
	if strings.Contains(event["event_id"].(string), "-r8") || strings.Contains(event["idempotency_key"].(string), ":8") {
		t.Fatalf("identity leaked the Runtime write counter: event=%v", event)
	}

	// G18 §1.3: retrying the same intent yields the same identity, so the
	// journal refuses the second commit instead of recording a second event.
	_, err = assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: next.Revision, AgentID: "agent-ver-1", Event: "readback_submitted", MessagePath: messagePath,
	})
	if err == nil {
		t.Fatal("retrying the same authored message must not commit a second event")
	}
	if !strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("retry error=%v, want the stable-identity journal refusal", err)
	}
}

// TestAgentLifecycleEventIdentitySeparatesResponsibilities proves two
// responsibilities never share one identity even when the authored intent is
// the same message (G18 §1.3).
func TestAgentLifecycleEventIdentitySeparatesResponsibilities(t *testing.T) {
	root := assignmentTestRoot(t)
	dir := t.TempDir()
	statePath, journalPath := filepath.Join(dir, "loop-state.json"), filepath.Join(dir, "loop-events.jsonl")
	state := activeState(t, root, "verification", "running", 7)
	state["entities"].(map[string]any)["agents"] = []any{
		map[string]any{
			"id": "agent-ver-1", "role": "delivery-verifier", "state": "reading",
			"task_ids": []any{"TASK-012"}, "team_id": "workgroup-delivery-round-1",
			"definition_ref": ".claude/agents/delivery-verifier.md", "prompt_ref": "manifest#assignment-ver-1",
			"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-06-22T00:00:00Z",
		},
		map[string]any{
			"id": "agent-ver-2", "role": "delivery-verifier", "state": "reading",
			"task_ids": []any{"TASK-013"}, "team_id": "workgroup-delivery-round-2",
			"definition_ref": ".claude/agents/delivery-verifier.md", "prompt_ref": "manifest#assignment-ver-2",
			"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-06-22T00:00:00Z",
		},
	}
	writeJSON(t, statePath, state)
	first := writeAgentMessage(t, dir, "first.json", "readback_response", "agent-ver-1", "TASK-012", "msg-shared-intent", 7)
	second := writeAgentMessage(t, dir, "second.json", "readback_response", "agent-ver-2", "TASK-013", "msg-shared-intent", 7)

	if _, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 7, AgentID: "agent-ver-1", Event: "readback_submitted", MessagePath: first,
	}); err != nil {
		t.Fatalf("first responsibility: %v", err)
	}
	if _, err := assignment.AdvanceAgent(root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: 8, AgentID: "agent-ver-2", Event: "readback_submitted", MessagePath: second,
	}); err != nil {
		t.Fatalf("second responsibility: %v", err)
	}
	events := readJournalEvents(t, journalPath)
	if len(events) != 2 {
		t.Fatalf("journal events=%d, want 2", len(events))
	}
	firstID, secondID := events[0]["event_id"].(string), events[1]["event_id"].(string)
	if firstID == secondID {
		t.Fatalf("two responsibilities share one identity: %s", firstID)
	}
	if events[0]["idempotency_key"] == events[1]["idempotency_key"] {
		t.Fatal("two responsibilities share one idempotency key")
	}
}

// TestBugLifecycleEventIdentityUsesDomainAttempt proves the BUG lifecycle
// identity names the BUG, the operation and the BUG's own investigation
// attempt instead of the Runtime write counter (G18 §1.2).
func TestBugLifecycleEventIdentityUsesDomainAttempt(t *testing.T) {
	for _, attempt := range []int{0, 1} {
		t.Run("attempt", func(t *testing.T) {
			root, statePath, journalPath := writeBUG103Runtime(t, map[string]any{
				"bugs": []any{map[string]any{
					"id":                          "BUG-103",
					"state":                       "draft",
					"path":                        "docs/reports/bugs/BUG-103.md",
					"severity":                    "P1",
					"attempt_count":               attempt,
					"same_contract_failure_count": 0,
					"original_finder_agent_ids":   []any{"agent-finder"},
				}},
			})
			if _, err := assignment.AdvanceBug(root, statePath, journalPath, assignment.BugEventRequest{
				ExpectedRevision: 1, BugID: "BUG-103", Event: "investigation_started",
			}); err != nil {
				t.Fatalf("BUG event should commit: %v", err)
			}
			events := readJournalEvents(t, journalPath)
			if len(events) != 1 {
				t.Fatalf("journal events=%d, want 1", len(events))
			}
			event := events[0]
			want := "evt-bug-BUG-103-investigation_started-" + strconv.Itoa(attempt)
			if event["event_id"] != want {
				t.Fatalf("event_id=%v, want %s", event["event_id"], want)
			}
			if event["idempotency_key"] != "runtime:bug:BUG-103:investigation_started:"+strconv.Itoa(attempt) {
				t.Fatalf("idempotency_key=%v, want the semantic BUG/operation/attempt shape", event["idempotency_key"])
			}
			if strings.Contains(event["event_id"].(string), "-r2") {
				t.Fatalf("BUG identity leaked the Runtime write counter: event=%v", event)
			}
		})
	}
}

// TestTaskLifecycleEventIdentityUsesDomainIntent proves the TASK lifecycle
// identity names the TASK, the operation and the domain artifact the
// operation answers instead of the Runtime write counter (G18 §1.2).
func TestTaskLifecycleEventIdentityUsesDomainIntent(t *testing.T) {
	root, statePath, journalPath := writeBUG103Runtime(t, map[string]any{
		"tasks": []any{map[string]any{
			"id":              "TASK-103",
			"state":           "in_progress",
			"path":            "docs/dev/tasks/TASK-103.md",
			"sha256":          "0000000000000000000000000000000000000000000000000000000000000000",
			"owner_agent_ids": []any{"agent-builder"},
		}},
	})
	if _, err := assignment.AdvanceTask(root, statePath, journalPath, assignment.TaskEventRequest{
		ExpectedRevision: 1, TaskID: "TASK-103", Event: "builder_reported",
		Params: map[string]any{"completion_report_ref": "ev-completion-TASK-103-g1"},
	}); err != nil {
		t.Fatalf("TASK event should commit: %v", err)
	}
	events := readJournalEvents(t, journalPath)
	if len(events) != 1 {
		t.Fatalf("journal events=%d, want 1", len(events))
	}
	event := events[0]
	if event["event_id"] != "evt-task-TASK-103-builder_reported-ev-completion-TASK-103-g1" {
		t.Fatalf("event_id=%v, want the semantic TASK/operation/intent shape", event["event_id"])
	}
	if event["idempotency_key"] != "runtime:task:TASK-103:builder_reported:ev-completion-TASK-103-g1" {
		t.Fatalf("idempotency_key=%v, want the semantic TASK/operation/intent shape", event["idempotency_key"])
	}
	if strings.Contains(event["event_id"].(string), "-r2") {
		t.Fatalf("TASK identity leaked the Runtime write counter: event=%v", event)
	}
}
