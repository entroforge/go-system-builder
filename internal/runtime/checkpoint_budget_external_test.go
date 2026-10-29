package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// This is a low-level budget fixture; the packet row is structural input,
// not a claimed professional issuance or domain acceptance.
func TestStoreCheckpointBudgetRejectsBeforeStateJournalAndPending(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := filepath.Join(root, "loop-state.json"), filepath.Join(root, "loop-events.jsonl")
	writeState(t, statePath, 1)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	req := state["bound_req"].(map[string]any)
	p, err := workpacket.BuildDesign(workpacket.PlanningBinding{RuntimeID: "loop-test", Generation: 1, StageEntry: 1, Stage: "S2", Owner: "Main", Requirement: workpacket.Reference{ID: req["id"].(string), Path: req["path"].(string), Version: req["version"].(string), SHA256: req["sha256"].(string)}})
	if err != nil {
		t.Fatal(err)
	}
	packetJSON, _ := json.Marshal(p)
	var row map[string]any
	if err := json.Unmarshal(packetJSON, &row); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["work_packets"] = []any{row}
	data, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	store := testWriter(statePath, journalPath)
	_, err = store.Update(1, runtime.Mutation{EventID: "evt-budget-refuse", TransitionID: "TR-TEST", Event: "test_transition", Actor: "orchestrator", IdempotencyKey: "runtime:budget:1", Apply: func(candidate map[string]any) error {
		candidate["milestone"].(map[string]any)["instruction"] = strings.Repeat("x", 17<<20)
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "checkpoint state exceeds 16 MiB") {
		t.Fatalf("did not reach prospective size refusal: %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("budget refusal replaced state")
	}
	if journal, err := os.ReadFile(journalPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if len(journal) != 0 {
		t.Fatal("budget refusal appended journal")
	}
	for _, pattern := range []string{"*pending*", "*commit*"} {
		paths, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) > 0 {
			t.Fatalf("budget refusal created pending artifact: %v", paths)
		}
	}
}
