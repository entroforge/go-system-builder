package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/change"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestCreateChangeStoresRecordWithCASAndJournal(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatal(err)
	}

	record, err := change.BuildRecord(change.Input{
		ID: "CHG-001", REQRef: "REQ-002", REQSHA: "1111111111111111111111111111111111111111111111111111111111111111",
		Summary: "fix timeout mapping", Class: "bugfix", Risk: "medium",
		WorkItems: []change.WorkItem{{ID: "W-1", Text: "fix timeout", Owner: "main", WritePaths: []string{"internal/client/**"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := runtime.CreateChange(dir, statePath, journalPath, runtime.ChangeRequest{ExpectedRevision: 1, Record: record})
	if err != nil {
		t.Fatalf("CreateChange() error = %v", err)
	}
	if next.Revision != 2 {
		t.Fatalf("revision = %d, want 2", next.Revision)
	}
	if got, ok := next.State["change"].(map[string]any); !ok || got["id"] != "CHG-001" {
		t.Fatalf("stored change = %#v", next.State["change"])
	}
	var event map[string]any
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(journal, &event); err != nil {
		t.Fatal(err)
	}
	if event["transition_id"] != "CHANGE-RECORD-CREATE" || event["event"] != "transition_committed" {
		t.Fatalf("journal event = %#v", event)
	}
}

func TestCreateChangeRejectsSecondRecordWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{}
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["change"] = map[string]any{"id": "CHG-EXISTING"}
	stateData, _ = json.Marshal(state)
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatal(err)
	}
	record := change.Record{ID: "CHG-002", REQRef: "REQ-002", REQSHA: "1111111111111111111111111111111111111111111111111111111111111111", Summary: "second", Class: "docs-only", Risk: "low", WorkItems: []change.WorkItem{{ID: "W-1", Text: "work", Status: "open"}}, RequiredChecks: []change.Check{{ID: "CK-1", Kind: "link_check", Status: "open"}}}
	_, err = runtime.CreateChange(dir, statePath, journalPath, runtime.ChangeRequest{ExpectedRevision: 1, Record: record})
	if err == nil {
		t.Fatal("expected second Change Record to be rejected")
	}
	if _, statErr := os.Stat(journalPath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected create must not append journal, stat error = %v", statErr)
	}
}

func TestCreateChangeRejectsRecordWithMismatchedREQRef(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatal(err)
	}
	record, err := change.BuildRecord(change.Input{
		ID: "CHG-001", REQRef: "REQ-999", REQSHA: "1111111111111111111111111111111111111111111111111111111111111111",
		Summary: "mismatched req ref", Class: "docs-only", Risk: "low",
		WorkItems: []change.WorkItem{{ID: "W-1", Text: "work", Owner: "main"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.CreateChange(dir, statePath, journalPath, runtime.ChangeRequest{ExpectedRevision: 1, Record: record})
	if err == nil {
		t.Fatal("expected mismatched req_ref to be rejected")
	}
	if _, statErr := os.Stat(journalPath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected create must not append journal, stat error = %v", statErr)
	}
}

func TestCreateChangeRejectsRecordWithMismatchedREQSHA(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatal(err)
	}
	record, err := change.BuildRecord(change.Input{
		ID: "CHG-001", REQRef: "REQ-002", REQSHA: "2222222222222222222222222222222222222222222222222222222222222222",
		Summary: "mismatched req sha", Class: "docs-only", Risk: "low",
		WorkItems: []change.WorkItem{{ID: "W-1", Text: "work", Owner: "main"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.CreateChange(dir, statePath, journalPath, runtime.ChangeRequest{ExpectedRevision: 1, Record: record})
	if err == nil {
		t.Fatal("expected mismatched req_sha256 to be rejected")
	}
}

func TestCreateChangeRejectsRecordWithReducedRequiredChecks(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatal(err)
	}
	record, err := change.BuildRecord(change.Input{
		ID: "CHG-001", REQRef: "REQ-002", REQSHA: "1111111111111111111111111111111111111111111111111111111111111111",
		Summary: "reduced checks", Class: "bugfix", Risk: "medium",
		WorkItems: []change.WorkItem{{ID: "W-1", Text: "work", Owner: "main"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(record.RequiredChecks) < 2 {
		t.Fatalf("test fixture must produce at least 2 checks, got %d", len(record.RequiredChecks))
	}
	record.RequiredChecks = record.RequiredChecks[:len(record.RequiredChecks)-1]
	_, err = runtime.CreateChange(dir, statePath, journalPath, runtime.ChangeRequest{ExpectedRevision: 1, Record: record})
	if err == nil {
		t.Fatal("expected reduced required_checks to be rejected")
	}
	if _, statErr := os.Stat(journalPath); !os.IsNotExist(statErr) {
		t.Fatalf("rejected create must not append journal, stat error = %v", statErr)
	}
}
