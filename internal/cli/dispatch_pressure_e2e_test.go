package cli_test

import (
	"bytes"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/hookctx"
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
	if err != nil || pressure != nil || snap.State["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatalf("terminal S7 retained S6 dispatch: %+v %v", pressure, err)
	}
	cached, _ := snap.State["dispatch_pressure"].(map[string]any)
	if cached["paused"] != false {
		t.Fatal("original ACK transaction did not release hysteresis")
	}
	loaded, err := hookctx.FromSnapshot(f.root, "", snap)
	if err != nil {
		t.Fatal(err)
	}
	var a hookctx.AssignmentContext
	for _, item := range loaded.Assignments {
		if item.AssignmentID == "assignment-core-builder" {
			a = item
		}
	}
	if a.AssignmentID == "" || a.OwnerAgentID != "core-builder" || a.TaskID != f.taskID {
		t.Fatalf("original Builder assignment missing: %+v", a)
	}
	path := integration.CheckpointPath(f.root, loaded.PolicyContext.RuntimeID, loaded.BaselineGeneration, a.AssignmentID)
	store := integration.DefaultCheckpointStore()
	cp, found, err := store.Load(path)
	if err != nil || !found {
		t.Fatalf("original integration checkpoint missing: found=%t %v", found, err)
	}
	if cp.State != integration.StateCleanupPending || cp.VerifiedAt == "" || a.CompletionAckRef == "" || cp.CompletionReportPath == "" || cp.CompletionReportPath != a.CompletionRef {
		t.Fatalf("original ACK/check/report binding missing: cp=%+v assignment=%+v", cp, a)
	}
	assertPending := func(wantCurrent, wantAck bool) {
		t.Helper()
		rows, err := integration.Pending(f.root, loaded)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].AssignmentID != a.AssignmentID || rows[0].AgentID != a.OwnerAgentID || rows[0].State != integration.StateCleanupPending || rows[0].CurrentReport != wantCurrent || rows[0].Acknowledged != wantAck {
			t.Fatalf("current original Pending binding: rows=%+v", rows)
		}
	}
	assertPending(true, true)
	original := cp
	// This is the real retained evidence, not an injected cleanup failure.
	evidence := filepath.Join(cp.WorktreePath, ".claude/evidence/owned-core-check.txt")
	before, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	head, err := workspace.Git(t.Context(), f.root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	replay := stageCLI(t, "runtime", "task-integrate", "--root", f.root, "--assignment-id", a.AssignmentID, "--agent-id", a.OwnerAgentID)
	var result struct {
		State       string   `json:"state"`
		Blocked     bool     `json:"blocked"`
		Integration []string `json:"integration"`
	}
	if err := json.Unmarshal([]byte(replay), &result); err != nil || result.State != "integrated" || result.Blocked || !strings.Contains(strings.Join(result.Integration, "\n"), ".claude/evidence/owned-core-check.txt") {
		t.Fatalf("pending replay lost original integration or retained path: %s; %v", replay, err)
	}
	after, err := os.ReadFile(evidence)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("replay removed or changed original evidence", err)
	}
	nextHead, err := workspace.Git(t.Context(), f.root, "rev-parse", "HEAD")
	if err != nil || nextHead != head || stageSnapshot(t, f.root).Revision != snap.Revision {
		t.Fatal("pending replay changed completed integration", err)
	}
	replayed, found, err := store.Load(path)
	if err != nil || !found || replayed.State != integration.StateCleanupPending || replayed.VerifiedAt != cp.VerifiedAt || replayed.CompletionReportPath != cp.CompletionReportPath {
		t.Fatalf("pending replay changed original verified facts: %+v; %v", replayed, err)
	}
	// A checkpoint for a different original Result is not this report's ACK.
	cp.CompletionReportPath = ".claude/evidence/a-different-result.json"
	if _, err := store.ForceWrite(path, cp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := store.ForceWrite(path, original); err != nil {
			t.Error(err)
		}
	})
	assertPending(false, false)
	if _, err := store.ForceWrite(path, original); err != nil {
		t.Fatal(err)
	}
	assertPending(true, true)
}
