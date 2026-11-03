package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointKeepsHistoricalReceiptsWithoutDigestProof(t *testing.T) {
	state := map[string]any{
		"runtime_id": "loop-checkpoint",
		"journal":    map[string]any{"last_sequence": 0, "last_event_id": nil},
		"workspace": map[string]any{"executions": map[string]any{
			"original": map[string]any{"tool_check_receipts": []any{map[string]any{"path": "missing-old-file", "sha256": "historical-only"}}},
		}},
	}
	if err := ValidateCheckpointIndices(t.TempDir(), state, nil); err != nil {
		t.Fatalf("historical receipt must not require source or digest proof: %v", err)
	}
	state["journal"].(map[string]any)["last_sequence"] = 1
	if err := ValidateCheckpointIndices(t.TempDir(), state, nil); err == nil {
		t.Fatal("state and journal pair mismatch was not reported")
	}
}

func TestCheckpointIndexValidationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateCheckpointIndicesContext(ctx, t.TempDir(), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation = %v", err)
	}
}

// checkpointCommittedEvent builds one schema-valid committed journal event so
// that a checkpoint and an active Runtime can be compared by their committed
// event coordinate instead of by a revision counter.
func checkpointCommittedEvent(eventID string, sequence int) map[string]any {
	return map[string]any{
		"schema_version":          "1.0.0",
		"runtime_id":              "loop-1",
		"event_id":                eventID,
		"idempotency_key":         "checkpoint-checks:" + eventID,
		"sequence":                sequence,
		"event":                   "transition_committed",
		"outcome":                 "committed",
		"actor":                   map[string]any{"type": "user", "id": "user"},
		"request_id":              "req-1",
		"baseline_generation":     1,
		"before_revision":         0,
		"after_revision":          sequence,
		"transition_id":           "TR-" + eventID,
		"gate_id":                 "MANUAL",
		"producer_responsibility": "user",
		"from":                    map[string]any{"state": "inactive", "phase": nil},
		"to":                      map[string]any{"state": "planning", "phase": "design"},
		"guard_results":           []any{},
		"action_results":          []any{},
		"evidence_ids":            []any{},
		"message":                 "committed event " + eventID,
		"occurred_at":             "2026-06-18T00:00:00Z",
	}
}

func encodeCheckpointJournal(t *testing.T, events ...map[string]any) []byte {
	t.Helper()
	var payload []byte
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, encoded...)
		payload = append(payload, '\n')
	}
	return payload
}

func TestCheckpointRecoveryUsesOriginalIdentityRatherThanWholeStateEquality(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	source := map[string]any{"runtime_id": "loop-1", "revision": 1, "bound_req": map[string]any{"id": "REQ-1", "path": "req.md", "version": "v1"}, "baseline": map[string]any{"generation": 1}, "journal": map[string]any{"last_sequence": 0, "last_event_id": nil}, "business": map[string]any{"notes": "original"}}
	current := map[string]any{"runtime_id": "loop-1", "revision": 1, "bound_req": map[string]any{"id": "REQ-1", "path": "req.md", "version": "v1", "unrelated": "extra"}, "baseline": map[string]any{"generation": 1}, "journal": map[string]any{"last_sequence": 0, "last_event_id": nil}, "business": map[string]any{"notes": "changed"}}
	data, _ := json.Marshal(current)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	pair := CheckpointPair{State: source, Manifest: RuntimeCheckpoint{RuntimeID: "loop-1", Generation: 1, Revision: 1}}
	if err := ValidateCheckpointRestore(root, pair); err != nil {
		t.Fatalf("unrelated professional content became a qualification: %v", err)
	}

	// The checkpoint was taken after the Runtime committed its first event, and
	// the active Runtime has committed another event since then. Restoring the
	// checkpoint would roll back committed progress, so the committed event
	// coordinate must refuse it. A revision counter is only an audit
	// coordinate and never decides this.
	committed := checkpointCommittedEvent("evt-000001", 1)
	later := checkpointCommittedEvent("evt-000002", 2)
	pair.JournalBytes = encodeCheckpointJournal(t, committed)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), encodeCheckpointJournal(t, committed, later), 0600); err != nil {
		t.Fatal(err)
	}
	current["journal"] = map[string]any{"last_sequence": 2, "last_event_id": "evt-000002"}
	data, _ = json.Marshal(current)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	err := ValidateCheckpointRestore(root, pair)
	if err == nil {
		t.Fatal("later committed Runtime progress was not protected")
	}
	if !strings.Contains(err.Error(), "committed Runtime progress") {
		t.Fatalf("restore refused for another reason than a committed progress rollback: %v", err)
	}

	// A journal that cannot be parsed leaves the committed event coordinate
	// unknown. Unknown is not a reason to refuse the restore.
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), []byte("{not a journal event\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCheckpointRestore(root, pair); err != nil {
		t.Fatalf("an unreadable live journal refused the restore: %v", err)
	}
}
