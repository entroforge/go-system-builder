package runtime_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestOrdinaryWriterCannotSwitchSchemaOrOptimization(t *testing.T) {
	for _, kind := range []string{"schema", "optimization", "dispatch"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "loop-state.json")
			journalPath := filepath.Join(root, "loop-events.jsonl")
			writeState(t, statePath, 1)
			state := readJSONMapRuntimeTest(t, statePath)
			if kind != "schema" {
				state["schema_version"] = "1.2.0"
				state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": false}
				writeJSONMapRuntimeTest(t, statePath, state)
			}
			before := mustRead(t, statePath)
			_, err := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{}).UpdateCurrent(runtime.Mutation{EventID: "evt-format-change", TransitionID: "FORMAT-CHANGE", Actor: "orchestrator", IdempotencyKey: "format-change", Apply: func(state map[string]any) error {
				if kind == "schema" {
					state["schema_version"] = "1.2.0"
				} else if kind == "dispatch" {
					state["configuration"].(map[string]any)["optimization"].(map[string]any)["dispatch_backpressure"] = map[string]any{"max_unreceived_results": 3, "resume_unreceived_results": 1}
				} else {
					state["configuration"].(map[string]any)["optimization"].(map[string]any)["packet"] = true
				}
				return nil
			}})
			if err == nil {
				t.Fatal("ordinary mutation silently migrated the format/configuration")
			}
			if after := mustRead(t, statePath); !bytes.Equal(before, after) {
				t.Fatal("refused format change mutated state")
			}
			if data, err := os.ReadFile(journalPath); err == nil && len(data) > 0 {
				t.Fatal("refused format change appended a journal event")
			}
		})
	}
}

func TestPendingCommitKeepsOriginalFormatAndReceptionIndices(t *testing.T) {
	for _, version := range []string{"1.1.0", "1.2.0"} {
		t.Run(version, func(t *testing.T) {
			root, statePath, journalPath := "", "", ""
			if version == "1.2.0" {
				root, statePath, journalPath, _, _ = acceptedDocumentFixture(t)
			} else {
				root = t.TempDir()
				statePath = filepath.Join(root, "loop-state.json")
				journalPath = filepath.Join(root, "loop-events.jsonl")
				writeState(t, statePath, 1)
				if err := os.WriteFile(journalPath, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := readJSONMapRuntimeTest(t, statePath)
			indicesBefore, _ := json.Marshal(before["entities"])
			if err := os.Chmod(journalPath, 0444); err != nil {
				t.Fatal(err)
			}
			writer := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{})
			if _, err := writer.UpdateCurrent(runtime.Mutation{EventID: "evt-format-pending", TransitionID: "FORMAT-CHECKPOINT", Event: "format_checkpoint", Actor: "orchestrator", IdempotencyKey: "format-pending", RetainLastTransition: true}); err == nil {
				t.Fatal("injected journal interruption did not interrupt")
			}
			marker := statePath + ".commit-pending.json"
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("no original pending marker: %v", err)
			}
			if err := os.Chmod(journalPath, 0600); err != nil {
				t.Fatal(err)
			}
			restored, err := writer.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if restored.State["schema_version"] != version || runtime.PacketEnabled(restored.State) != runtime.PacketEnabled(before) {
				t.Fatal("pending recovery changed original mode")
			}
			indicesAfter, _ := json.Marshal(restored.State["entities"])
			if !bytes.Equal(indicesBefore, indicesAfter) {
				t.Fatal("pending recovery lost issued/reception indices")
			}
			revision := restored.Revision
			replay, err := writer.Snapshot()
			if err != nil || replay.Revision != revision {
				t.Fatal("pending recovery replay committed twice")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("completed pending marker was not cleared")
			}
		})
	}
}

func TestPendingResetRefusesFormatDriftWithoutChangingPair(t *testing.T) {
	for _, kind := range []string{"schema", "optimization"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "loop-state.json")
			journalPath := filepath.Join(root, "loop-events.jsonl")
			writeState(t, statePath, 1)
			state := readJSONMapRuntimeTest(t, statePath)
			state["schema_version"] = "1.2.0"
			state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
			writeJSONMapRuntimeTest(t, statePath, state)
			fresh := freshInactiveState(t)
			if kind == "optimization" {
				fresh["schema_version"] = "1.2.0"
				fresh["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": false}
			}
			writePendingRolloverMarker(t, statePath, root, fresh)
			beforeState, beforeJournal, beforeMarker := mustRead(t, statePath), mustRead(t, journalPath), mustRead(t, statePath+".rollover-pending.json")
			if _, err := runtime.NewWriter(statePath, journalPath, root, integrityTestValidator{}).Snapshot(); err == nil {
				t.Fatal("reset pending changed source format")
			}
			if !bytes.Equal(beforeState, mustRead(t, statePath)) || !bytes.Equal(beforeJournal, mustRead(t, journalPath)) || !bytes.Equal(beforeMarker, mustRead(t, statePath+".rollover-pending.json")) {
				t.Fatal("format drift rejection changed state/journal/marker")
			}
		})
	}
}
