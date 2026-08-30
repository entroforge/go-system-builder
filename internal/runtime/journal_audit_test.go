package runtime_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestJournalEventContainsSYNC039AuditFields(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeState(t, statePath, 1)

	store := testWriter(statePath, journalPath)
	_, err := store.Update(1, runtime.Mutation{
		EventID:                "evt-audit-2",
		TransitionID:           "PTR-PLAN-01",
		Event:                  "planning_design_complete",
		Actor:                  "hook_controller",
		IdempotencyKey:         "runtime:PTR-PLAN-01:1",
		RequestID:              "session-audit-1",
		BaselineGeneration:     2,
		GateID:                 "GATE-PLANNING-DESIGN-COMPLETE",
		GateFingerprint:        "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		ProducerResponsibility: "Architect",
		From:                   map[string]any{"state": "planning", "phase": "design"},
		To:                     map[string]any{"state": "planning", "phase": "contracts"},
		GuardResults:           []map[string]any{{"id": "planning_complete", "result": "pass", "detail": "ok"}},
		ActionResults:          []map[string]any{{"id": "set_planning_phase_contracts", "result": "committed", "detail": "ok"}},
		Message:                "Planning design complete.",
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	event, err := readLastJournalEvent(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"event_id", "request_id", "runtime_id", "baseline_generation", "event",
		"transition_id", "actor", "producer_responsibility", "gate_id", "gate_fingerprint",
		"before_revision", "after_revision", "from", "to", "evidence_ids", "occurred_at",
	} {
		if _, ok := event[key]; !ok {
			t.Fatalf("journal event missing required field %q: %#v", key, event)
		}
	}
	if event["event"] != "transition_committed" {
		t.Fatalf("event = %v, want transition_committed", event["event"])
	}
	if event["before_revision"] != float64(1) || event["after_revision"] != float64(2) {
		t.Fatalf("revision fields = %v/%v, want 1/2", event["before_revision"], event["after_revision"])
	}

	validator := schema.NewEmbeddedValidator()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateBytes("loop-event.schema.json", raw); err != nil {
		t.Fatalf("journal event failed schema validation: %v", err)
	}
}

func readLastJournalEvent(path string) (map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var last map[string]any
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event map[string]any
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, err
			}
			last = event
		}
		if err != nil {
			break
		}
	}
	if last == nil {
		return nil, os.ErrNotExist
	}
	return last, nil
}
