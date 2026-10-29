package transition_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/transition"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestLegacyTR006UsesCurrentBuilderBatchGateAtomically(t *testing.T) {
	tests := []struct {
		name        string
		planned     bool
		omitSecond  bool
		wantSuccess bool
	}{
		{name: "complete legacy batch advances", wantSuccess: true},
		{name: "unplanned incomplete batch is rejected", omitSecond: true},
		{name: "dispatch-plan incomplete batch is rejected", planned: true, omitSecond: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := req039fixtures.FreshRoot(t)
			state := req039fixtures.BaseState(t, root, "building", "", 0)
			req039fixtures.SeedBuilderBatchReady(t, root, state)
			if test.planned {
				documents := state["documents"].([]any)
				state["documents"] = append(documents, map[string]any{
					"id": "dispatch-plan-current", "kind": "dispatch_plan", "generation": 1,
				})
			}
			if test.omitSecond {
				evidence := state["evidence"].([]any)
				filtered := make([]any, 0, len(evidence))
				for _, raw := range evidence {
					row := raw.(map[string]any)
					if row["id"] != "ev-completion-2" {
						filtered = append(filtered, row)
					}
				}
				state["evidence"] = filtered
			}
			req039fixtures.WriteState(t, root, state)
			statePath := filepath.Join(root, ".claude", "loop-state.json")
			journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := transition.Apply(root, statePath, journalPath, transition.Request{
				Files:             fileview.Disk{Root: root},
				TransitionID:      "TR-006",
				ExpectedRevision:  0,
				ExpectedRuntimeID: req039fixtures.RuntimeIDFromState(state),
				Actor:             "orchestrator",
				Evidence:          map[string]string{"builder_report_record": "ev-team"},
			})
			if test.wantSuccess {
				if err != nil {
					t.Fatalf("complete legacy TR-006 batch rejected: %v", err)
				}
				lifecycle := snapshot.State["lifecycle"].(map[string]any)
				if lifecycle["state"] != "verification" {
					t.Fatalf("TR-006 lifecycle state = %v, want verification", lifecycle["state"])
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "TASK-039-02") {
				t.Fatalf("incomplete legacy batch must reject with the missing current TASK, got %v", err)
			}
			afterState, readErr := os.ReadFile(statePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			afterJournal, readErr := os.ReadFile(journalPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(beforeState) != string(afterState) || string(beforeJournal) != string(afterJournal) {
				t.Fatal("rejected legacy TR-006 batch changed Runtime or journal")
			}
		})
	}
}
