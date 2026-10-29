package controlproof

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/productbaseline"
)

func TestInspectJournalUsesEmbeddedSchemaAndOriginalCursorRules(t *testing.T) {
	first := controlProofEvent("loop-proof", "evt-one", 1, "TEST-ONE", 0, 1)
	second := controlProofEvent("loop-proof", "evt-two", 2, "TEST-TWO", 1, 2)
	data := controlProofJournal(t, first, second)
	inspection, err := InspectJournalData(data)
	if err != nil {
		t.Fatalf("InspectJournalData: %v", err)
	}
	if inspection.RuntimeID != "loop-proof" || inspection.TailSequence != 2 || inspection.EventIndex["evt-two"] != 1 {
		t.Fatalf("inspection = %#v", inspection)
	}
	state := map[string]any{
		"runtime_id": "loop-proof",
		"journal":    map[string]any{"last_sequence": 2, "last_event_id": "evt-two"},
	}
	if err := ValidateStateJournalPair(state, inspection); err != nil {
		t.Fatalf("matching state/journal pair rejected: %v", err)
	}
	state["journal"].(map[string]any)["last_event_id"] = "forged"
	if err := ValidateStateJournalPair(state, inspection); err == nil {
		t.Fatal("state cursor with a forged tail event was accepted")
	}

	malformed := map[string]any{}
	if err := ValidateJournalEvent(malformed); err == nil {
		t.Fatal("event missing required original schema fields was accepted")
	}
	segment, err := InspectJournalSegmentDataFrom(controlProofJournal(t, controlProofEvent("loop-proof", "evt-later", 41, "TEST-LATER", 40, 41)), 41)
	if err != nil || segment.TailSequence != 41 {
		t.Fatalf("rotated segment inspection = %#v, err=%v", segment, err)
	}
}

func TestReadJournalForStateMergesOriginalArchiveOrder(t *testing.T) {
	root := t.TempDir()
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	first := controlProofJournal(t, controlProofEvent("loop-proof", "evt-one", 1, "TEST-ONE", 0, 1))
	second := controlProofJournal(t, controlProofEvent("loop-proof", "evt-two", 2, "TEST-TWO", 1, 2))
	if err := os.WriteFile(journalPath+".archive.1.jsonl", first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, second, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath+".archive.not-a-sequence.jsonl", []byte("not an event\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"runtime_id": "loop-proof",
		"journal":    map[string]any{"last_sequence": 2, "last_event_id": "evt-two"},
	}
	combined, err := ReadJournalForState(root, state)
	if err != nil {
		t.Fatalf("ReadJournalForState: %v", err)
	}
	inspection, err := InspectJournalData(combined)
	if err != nil || inspection.TailSequence != 2 || len(inspection.Events) != 2 {
		t.Fatalf("merged journal inspection = %#v, err=%v", inspection, err)
	}
	segments, err := JournalSegmentPaths(journalPath)
	if err != nil || !reflect.DeepEqual(segments, []string{journalPath + ".archive.1.jsonl"}) {
		t.Fatalf("archive paths = %#v, err=%v", segments, err)
	}
}

func TestS6MainStartRequiresOriginalTR003Receipt(t *testing.T) {
	start, err := productbaseline.Seal(productbaseline.S6MainStart{
		RuntimeID:               "loop-proof",
		BaselineGeneration:      1,
		ProjectRoot:             "/repo",
		DevelopmentBranch:       "development",
		StartCommit:             "1111111111111111111111111111111111111111",
		TransitionEventID:       "evt-tr003",
		TransitionAfterRevision: 1,
		TransitionSequence:      1,
		TaskSet: []productbaseline.Task{{
			TaskID: "TASK-ONE", Path: "tasks/one.md", Version: "v1",
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	})
	if err != nil {
		t.Fatalf("Seal S6MainStart: %v", err)
	}
	event := controlProofEvent("loop-proof", "evt-tr003", 1, "TR-003", 0, 1)
	event["from"] = map[string]any{"state": "document_verification", "phase": nil}
	event["to"] = map[string]any{"state": "building", "phase": nil}
	event["action_results"] = []any{map[string]any{
		"id": "register_execution_batch", "result": "committed",
		"detail": "registered 1 complete task(s); s6_main_start_sha256=" + start.ReceiptSHA256,
	}}
	journal := controlProofJournal(t, event)
	state := map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     "loop-proof",
		"baseline": map[string]any{
			"generation": 1, "s6_main_starts": []productbaseline.S6MainStart{start},
		},
		"journal": map[string]any{"last_sequence": 1, "last_event_id": "evt-tr003"},
	}
	got, err := RequireCurrentS6MainStart(state, journal)
	if err != nil || !reflect.DeepEqual(got, start) {
		t.Fatalf("RequireCurrentS6MainStart = %#v, err=%v", got, err)
	}
	committed, err := HasCommittedTransitionForGeneration(state, journal, "TR-003", 1)
	if err != nil || !committed {
		t.Fatalf("HasCommittedTransitionForGeneration = %v, err=%v", committed, err)
	}

	state["journal"].(map[string]any)["last_sequence"] = 2
	if _, err := RequireCurrentS6MainStart(state, journal); err == nil {
		t.Fatal("S6 start receipt was accepted against a different Runtime cursor")
	}
}

func TestAssignmentRevisionReceiptHelpersKeepIntentPartitionAndDTO(t *testing.T) {
	ref := assignmentrecord.RecordReference{
		RuntimeID: "loop-proof", BaselineGeneration: 3, AssignmentID: "assignment-one", Revision: 1,
	}
	const intent = "revise-task-scope"
	eventID := AssignmentRevisionEventID(ref, intent)
	key := AssignmentRevisionIdempotencyKey(ref, intent)
	if eventID == "" || key == "" || eventID == AssignmentRevisionEventID(ref, "different") || key == AssignmentRevisionIdempotencyKey(ref, "different") {
		t.Fatal("revision event/idempotency identity is not partitioned by intent")
	}
	receipt := AssignmentRevisionReceipt{
		IntentKey: intent, RequestDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AssignmentRecord: assignmentrecord.RecordReference{
			RuntimeID: ref.RuntimeID, BaselineGeneration: ref.BaselineGeneration,
			AssignmentID: ref.AssignmentID, Revision: 2,
		},
		PreviousRecord: ref, RuntimeEventID: eventID, RuntimeCommitRevision: 14,
		PreviousSourceSnapshots: []assignmentrecord.SourceSnapshot{{
			SourceRef: "tasks/one.md", SourceKind: "task", SourceSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Path: ".claude/evidence/records/task.json", SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		}},
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip AssignmentRevisionReceipt
	if err := json.Unmarshal(data, &roundTrip); err != nil || !reflect.DeepEqual(roundTrip, receipt) {
		t.Fatalf("revision receipt DTO round trip = %#v, err=%v", roundTrip, err)
	}
	if err := ValidateAssignmentRevisionReceipts(map[string]any{
		"entities": map[string]any{"assignment_revision_receipts": []any{}},
	}, nil); err != nil {
		t.Fatalf("empty legacy revision registry rejected: %v", err)
	}
}

func controlProofEvent(runtimeID, eventID string, sequence int, transition string, before, after int) map[string]any {
	return map[string]any{
		"schema_version": "1.0.0", "runtime_id": runtimeID, "event_id": eventID,
		"sequence": sequence, "event": "transition_committed", "outcome": "committed",
		"actor":      map[string]any{"type": "orchestrator", "id": "orchestrator"},
		"request_id": "request-" + eventID, "idempotency_key": "key-" + eventID,
		"baseline_generation": 1, "before_revision": before, "after_revision": after,
		"transition_id": transition, "gate_id": "CONTROL-PROOF-TEST", "gate_fingerprint": "sha256:test",
		"producer_responsibility": "test", "from": map[string]any{"state": "building", "phase": "build"},
		"to": map[string]any{"state": "building", "phase": "build"}, "guard_results": []any{},
		"action_results": []any{map[string]any{"id": "test_action", "result": "committed", "detail": "test"}},
		"evidence_ids":   []any{}, "message": "controlproof fixture", "occurred_at": "2026-10-14T00:00:00Z",
	}
}

func controlProofJournal(t testing.TB, events ...map[string]any) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, event := range events {
		if err := ValidateJournalEvent(event); err != nil {
			t.Fatalf("fixture event schema: %v", err)
		}
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	return buffer.Bytes()
}
