package cli_test

import (
	"bytes"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredPressureOriginalReportWriterRegistrationAndActualMainIntegration(t *testing.T) {
	f := coreBuilderFixture(t, func(root string, state map[string]any) {
		state["configuration"].(map[string]any)["optimization"].(map[string]any)["dispatch_backpressure"] = map[string]any{"max_unreceived_results": 1, "resume_unreceived_results": 0}
	})
	runCoreBuilderIntegration(t, f, func(e workspace.Execution) {
		snap := stageSnapshot(t, f.root)
		pressure, err := dispatch.ProjectPressure(f.root, snap.State)
		if err != nil || pressure == nil || !pressure.Paused || pressure.Unreceived != 1 {
			t.Fatalf("original report did not trigger pressure: %+v %v", pressure, err)
		}
		cached, _ := snap.State["dispatch_pressure"].(map[string]any)
		if cached["paused"] != true {
			t.Fatal("original completion transaction did not persist hysteresis")
		}
		manifestPath := ".claude/workgroups/REQ-039/core-builder/manifest.json"
		beforeState, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		beforeJournal, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
		var out, stderr bytes.Buffer
		if code := cli.Run([]string{"runtime", "register-workgroup", "--root", f.root, "--manifest", manifestPath, "--task-id", f.taskID, "--task", f.taskPath}, nil, &out, &stderr); code == 0 || !strings.Contains(stderr.String(), "queued by explicit dispatch backpressure") {
			t.Fatalf("original registration bypassed configured gate: %d %s", code, stderr.String())
		}
		afterState, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
		afterJournal, _ := os.ReadFile(filepath.Join(f.root, ".claude/loop-events.jsonl"))
		if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
			t.Fatal("queued registration mutated original authority")
		}
		output := stageCLI(t, "s6", "status", "--root", f.root, "--advice", "--json")
		var view struct {
			Pressure *dispatch.Pressure `json:"configured_backpressure"`
			Advice   struct {
				Budget *int `json:"dispatch_budget"`
			} `json:"reception_advice"`
		}
		if err := json.Unmarshal([]byte(output), &view); err != nil || view.Pressure == nil || !view.Pressure.Paused || view.Advice.Budget != nil {
			t.Fatal("configured pause invented capacity or disappeared", output, err)
		}
	})
	snap := stageSnapshot(t, f.root)
	pressure, err := dispatch.ProjectPressure(f.root, snap.State)
	if err != nil || pressure == nil || pressure.Paused || pressure.Unreceived != 0 {
		t.Fatalf("actual Main reception did not resume: %+v %v", pressure, err)
	}
	cached, _ := snap.State["dispatch_pressure"].(map[string]any)
	if cached["paused"] != false {
		t.Fatal("original ACK transaction did not release hysteresis")
	}
	// Preserve the actual accepted report/ACK, then vary only the checkpoint
	// fixture to cover cleanup and corruption. This is not a live cleanup fault.
	path := integration.CheckpointPath(f.root, snap.State["runtime_id"].(string), 1, "assignment-core-builder")
	store := integration.DefaultCheckpointStore()
	cp, found, err := store.Load(path)
	if err != nil || !found {
		t.Fatal(err)
	}
	original := cp
	cp.State = integration.StateCleanupPending
	if _, err := store.ForceWrite(path, cp); err != nil {
		t.Fatal(err)
	}
	pressure, err = dispatch.ProjectPressure(f.root, snap.State)
	if err != nil || pressure.Paused || pressure.Unreceived != 0 {
		t.Fatal("actual report ACK was held by cleanup fixture", pressure, err)
	}
	cp.CompletionReportSHA256 = strings.Repeat("0", 64)
	if _, err := store.ForceWrite(path, cp); err != nil {
		t.Fatal(err)
	}
	pressure, err = dispatch.ProjectPressure(f.root, snap.State)
	if err != nil || !pressure.Paused || pressure.Unreceived != 1 {
		t.Fatal("stale checkpoint hash released original pending result", pressure, err)
	}
	if _, err := store.ForceWrite(path, original); err != nil {
		t.Fatal(err)
	}
}
