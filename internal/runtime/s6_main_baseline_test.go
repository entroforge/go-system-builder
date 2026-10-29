package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/productbaseline"
)

func s6StartFixture(t *testing.T) (map[string]any, productbaseline.S6MainStart, []byte) {
	t.Helper()
	start, err := productbaseline.Seal(productbaseline.S6MainStart{
		RuntimeID:               "loop-REQ-001",
		BaselineGeneration:      1,
		ProjectRoot:             t.TempDir(),
		DevelopmentBranch:       "main",
		StartCommit:             strings.Repeat("a", 40),
		TransitionEventID:       "evt-tr-003-r2",
		TransitionAfterRevision: 2,
		TransitionSequence:      1,
		TaskSet: []productbaseline.Task{{
			TaskID: "TASK-001", Path: "docs/dev/tasks/TASK-001.md", Version: "v1.0.0", SHA256: strings.Repeat("b", 64),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal([]productbaseline.S6MainStart{start})
	if err != nil {
		t.Fatal(err)
	}
	var starts []any
	if err := json.Unmarshal(encoded, &starts); err != nil {
		t.Fatal(err)
	}
	detail := fmt.Sprintf("registered 1 complete task(s); s6_main_start_sha256=%s", start.ReceiptSHA256)
	event := map[string]any{
		"schema_version": "1.0.0", "runtime_id": start.RuntimeID,
		"event_id": start.TransitionEventID, "idempotency_key": "runtime:TR-003:1",
		"sequence": 1, "event": "transition_committed", "outcome": "committed",
		"actor": map[string]any{"type": "orchestrator", "id": "orchestrator"}, "request_id": "test",
		"baseline_generation": 1, "before_revision": 1, "after_revision": 2,
		"transition_id": "TR-003", "gate_id": "GATE-DOCUMENT-PASS", "gate_fingerprint": "sha256:test",
		"producer_responsibility": "orchestrator",
		"from":                    map[string]any{"state": "document_verification", "phase": nil},
		"to":                      map[string]any{"state": "building", "phase": nil},
		"evidence_ids":            []any{"REV-001"}, "message": "TR-003 committed",
		"occurred_at":    "2026-10-14T00:00:00Z",
		"guard_results":  []any{},
		"action_results": []any{map[string]any{"id": "register_execution_batch", "result": "committed", "detail": detail}},
	}
	line, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	line = append(line, '\n')
	state := map[string]any{
		"schema_version": "1.2.0", "runtime_id": start.RuntimeID, "revision": 2,
		"baseline": map[string]any{"generation": 1, "s6_main_starts": starts},
		"journal":  map[string]any{"last_sequence": 1, "last_event_id": start.TransitionEventID},
	}
	return state, start, line
}

func TestS6MainStartReceiptRequiresExactOriginalTR003Journal(t *testing.T) {
	state, want, journal := s6StartFixture(t)
	if err := ValidateS6MainStartReceipts(state, journal); err != nil {
		t.Fatalf("valid original receipt rejected: %v", err)
	}
	if got, err := RequireCurrentS6MainStart(state, journal); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("current start=%+v err=%v", got, err)
	}
	if found, err := HasCommittedTransitionForGeneration(state, journal, "TR-003", 1); err != nil || !found {
		t.Fatalf("TR-003 history found=%t err=%v", found, err)
	}

	var event map[string]any
	if err := json.Unmarshal(bytesTrimLine(journal), &event); err != nil {
		t.Fatal(err)
	}
	actions := event["action_results"].([]any)
	actions[0].(map[string]any)["detail"] = "registered 1 complete task(s); s6_main_start_sha256=" + strings.Repeat("0", 64)
	wrong, _ := json.Marshal(event)
	wrong = append(wrong, '\n')
	if err := ValidateS6MainStartReceipts(state, wrong); err == nil || !strings.Contains(err.Error(), "action receipt") {
		t.Fatalf("mismatched journal receipt accepted: %v", err)
	}
	if err := ValidateS6MainStartReceipts(state, nil); err == nil {
		t.Fatal("missing original journal accepted")
	}
}

func TestS6MainStartReceiptDoesNotBackfillMissingCurrentGeneration(t *testing.T) {
	state, _, journal := s6StartFixture(t)
	state["baseline"].(map[string]any)["s6_main_starts"] = []any{}
	if _, err := RequireCurrentS6MainStart(state, journal); err == nil || !strings.Contains(err.Error(), "no original TR-003") {
		t.Fatalf("missing current start was not fail closed: %v", err)
	}
	found, err := HasCommittedTransitionForGeneration(state, journal, "TR-003", 1)
	if err != nil || !found {
		t.Fatalf("old TR-003 detection found=%t err=%v", found, err)
	}
}

func TestS6MainStartReceiptPreservesOriginalTR003Actors(t *testing.T) {
	for _, actor := range []struct{ kind, id string }{
		{"orchestrator", "orchestrator"}, {"agent", "document_verifier"}, {"agent", "hook_controller"},
	} {
		t.Run(actor.id, func(t *testing.T) {
			state, _, journal := s6StartFixture(t)
			var event map[string]any
			if err := json.Unmarshal(journal, &event); err != nil {
				t.Fatal(err)
			}
			event["actor"] = map[string]any{"type": actor.kind, "id": actor.id}
			data, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateS6MainStartReceipts(state, data); err != nil {
				t.Fatalf("original TR-003 actor refused: %v", err)
			}
			event["actor"] = map[string]any{"type": "agent", "id": "frontend_builder"}
			data, _ = json.Marshal(event)
			if err := ValidateS6MainStartReceipts(state, data); err == nil {
				t.Fatal("unauthorized original TR-003 actor accepted")
			}
		})
	}
}

func TestReadJournalForStateIncludesRotationArchives(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	state, _, first := s6StartFixture(t)
	var event map[string]any
	if err := json.Unmarshal(bytesTrimLine(first), &event); err != nil {
		t.Fatal(err)
	}
	second := map[string]any{}
	for key, value := range event {
		second[key] = value
	}
	second["event_id"] = "evt-later"
	second["sequence"] = 2
	second["before_revision"] = 2
	second["after_revision"] = 3
	second["transition_id"] = "TR-LATER"
	second["from"] = map[string]any{"state": "building", "phase": nil}
	second["to"] = map[string]any{"state": "building", "phase": nil}
	second["action_results"] = []any{}
	secondBytes, _ := json.Marshal(second)
	secondBytes = append(secondBytes, '\n')
	if err := os.WriteFile(filepath.Join(claude, "loop-events.jsonl.archive.1.jsonl"), first, 0o644); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(claude, "loop-events.jsonl")
	if err := os.WriteFile(active, secondBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["revision"] = 3
	state["journal"].(map[string]any)["last_sequence"] = 2
	state["journal"].(map[string]any)["last_event_id"] = "evt-later"
	combined, err := ReadJournalForState(root, state)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := inspectJournalData(combined)
	if err != nil || len(inspection.Events) != 2 || inspection.Events[0]["event_id"] != "evt-tr-003-r2" {
		t.Fatalf("combined journal events=%d err=%v", len(inspection.Events), err)
	}
}

func bytesTrimLine(data []byte) []byte {
	return []byte(strings.TrimSuffix(string(data), "\n"))
}
