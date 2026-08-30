package controller_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestRecoveryReplayUsesStagingPairAndAdvancesThroughPlanning(t *testing.T) {
	root, state := replayFixture(t)
	activeStatePath := filepath.Join(root, ".claude", "loop-state.json")
	activeJournalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	stagingDir := filepath.Join(root, ".claude", "recovery-staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagingStatePath := filepath.Join(stagingDir, "loop-state.json")
	stagingJournalPath := filepath.Join(stagingDir, "loop-events.jsonl")
	writeReplayPair(t, stagingStatePath, stagingJournalPath, state)
	activeStateBefore := mustRead(t, activeStatePath)
	activeJournalBefore := mustRead(t, activeJournalPath)

	result, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   stagingStatePath,
		JournalPath: stagingJournalPath,
		MaxSteps:    2,
	})
	if err != nil {
		t.Fatalf("RecoveryReplay: %v", err)
	}
	if got := result.FinalCursor; got != "planning.tasks" {
		t.Fatalf("final cursor = %q, want planning.tasks; stop=%q trace=%#v", got, result.StopReason, result.Trace)
	}
	if len(result.Trace) != 2 {
		t.Fatalf("trace length = %d, want 2", len(result.Trace))
	}
	for _, trace := range result.Trace {
		if trace.Status != controller.StatusAdvanced {
			t.Fatalf("trace step %d status = %q, want advanced", trace.Step, trace.Status)
		}
		if !trace.TransitionCommitted {
			t.Fatalf("trace step %d did not commit", trace.Step)
		}
	}
	if got := mustRead(t, activeStatePath); string(got) != string(activeStateBefore) {
		t.Fatal("replay changed active state")
	}
	if got := mustRead(t, activeJournalPath); string(got) != string(activeJournalBefore) {
		t.Fatal("replay changed active journal")
	}
	staging, err := runtime.NewStore(stagingStatePath, stagingJournalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := lifecycleCursor(staging.State); got != result.FinalCursor {
		t.Fatalf("staging cursor = %q, result cursor = %q", got, result.FinalCursor)
	}
}

func TestRecoveryReplayStopsAtFirstNotReady(t *testing.T) {
	root, state := replayFixture(t)
	// The planning gates fall back to disk-declared facts, so
	// an empty documents[] no longer produces not_ready — remove the disk
	// architecture document as well to keep this test's intent (a genuine
	// first-step gap).
	if err := os.Remove(filepath.Join(root, "docs", "design", "architecture", "ARCHITECTURE-039-loop-control-plane.md")); err != nil {
		t.Fatal(err)
	}
	state["documents"] = []any{}
	stagingStatePath, stagingJournalPath := stagingPair(t, root, state)

	result, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   stagingStatePath,
		JournalPath: stagingJournalPath,
		MaxSteps:    4,
	})
	if err != nil {
		t.Fatalf("RecoveryReplay: %v", err)
	}
	if result.StopReason != controller.ReplayStopNotReady {
		t.Fatalf("stop reason = %q, want %q", result.StopReason, controller.ReplayStopNotReady)
	}
	if len(result.Trace) != 1 || result.Trace[0].Status != controller.StatusNotReady {
		t.Fatalf("trace = %#v, want one not_ready trace", result.Trace)
	}
}

func TestRecoveryReplayStopsOnUnknownConflictAndKeepsTrace(t *testing.T) {
	root, state := replayFixture(t)
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": 0}
	stagingStatePath, stagingJournalPath := stagingPair(t, root, state)

	result, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:          root,
		StatePath:     stagingStatePath,
		JournalPath:   stagingJournalPath,
		MaxSteps:      4,
		GateEvaluator: replayEvaluator{status: qualitygate.StatusUnknown, code: qualitygate.ErrorGateUnknown},
	})
	if err != nil {
		t.Fatalf("RecoveryReplay: %v", err)
	}
	if result.StopReason != controller.ReplayStopUnknown {
		t.Fatalf("stop reason = %q, want %q", result.StopReason, controller.ReplayStopUnknown)
	}
	if len(result.Trace) != 1 || result.Trace[0].ErrorCode != controller.CodeGateUnknown {
		t.Fatalf("trace = %#v, want retained unknown trace", result.Trace)
	}
}

func TestRecoveryReplayStopsOnSelectorConflict(t *testing.T) {
	root, state := replayFixture(t)
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": 0}
	stagingStatePath, stagingJournalPath := stagingPair(t, root, state)

	result, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   stagingStatePath,
		JournalPath: stagingJournalPath,
		MaxSteps:    4,
		GateEvaluator: replayEvaluator{
			status: qualitygate.StatusUnknown,
			code:   qualitygate.ErrorTriggerConflict,
		},
	})
	if err != nil {
		t.Fatalf("RecoveryReplay: %v", err)
	}
	if result.StopReason != controller.ReplayStopConflict {
		t.Fatalf("stop reason = %q, want %q", result.StopReason, controller.ReplayStopConflict)
	}
	if len(result.Trace) != 1 || result.Trace[0].ErrorCode != controller.CodeTriggerConfl {
		t.Fatalf("trace = %#v, want retained conflict trace", result.Trace)
	}
}

func TestRunControlCycleDefaultRuntimePairRemainsRootClaude(t *testing.T) {
	root, state := replayFixture(t)
	activeStatePath := filepath.Join(root, ".claude", "loop-state.json")
	stagingStatePath, stagingJournalPath := stagingPair(t, root, state)
	activeBefore := mustRead(t, activeStatePath)

	result, err := controller.RunControlCycle(context.Background(), controller.ControlRequest{
		Root:     root,
		Event:    "PreToolUse",
		ToolName: "Edit",
		ToolInput: map[string]any{
			"file_path": "internal/controller/replay.go",
		},
	})
	if err != nil {
		t.Fatalf("RunControlCycle: %v", err)
	}
	if result.Snapshot.Revision <= 0 {
		t.Fatalf("default cycle did not read root Runtime revision: %d", result.Snapshot.Revision)
	}
	if string(mustRead(t, activeStatePath)) == string(activeBefore) {
		t.Fatalf("default cycle did not use root Runtime pair: result=%#v", result)
	}
	staging, err := runtime.NewStore(stagingStatePath, stagingJournalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := lifecycleCursor(staging.State); got != "planning.design" {
		t.Fatalf("staging cursor = %q, want planning.design", got)
	}
}

func TestRecoveryReplayHonorsMaxSteps(t *testing.T) {
	root, state := replayFixture(t)
	stagingStatePath, stagingJournalPath := stagingPair(t, root, state)

	result, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   stagingStatePath,
		JournalPath: stagingJournalPath,
		MaxSteps:    1,
	})
	if err != nil {
		t.Fatalf("RecoveryReplay: %v", err)
	}
	if result.StopReason != controller.ReplayStopMaxSteps {
		t.Fatalf("stop reason = %q, want %q; trace=%#v", result.StopReason, controller.ReplayStopMaxSteps, result.Trace)
	}
	if len(result.Trace) != 1 {
		t.Fatalf("trace length = %d, want 1", len(result.Trace))
	}
}

func TestRecoveryReplayHonorsContextCancellation(t *testing.T) {
	root, state := replayFixture(t)
	stagingStatePath, stagingJournalPath := stagingPair(t, root, state)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := controller.RecoveryReplay(ctx, controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   stagingStatePath,
		JournalPath: stagingJournalPath,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestRecoveryReplayRejectsStagingPairOutsideRootBeforeReading(t *testing.T) {
	root, state := replayFixture(t)
	outside := t.TempDir()
	statePath := filepath.Join(outside, "loop-state.json")
	journalPath := filepath.Join(outside, "loop-events.jsonl")
	writeReplayPair(t, statePath, journalPath, state)

	_, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   statePath,
		JournalPath: journalPath,
	})
	if err == nil || !strings.Contains(err.Error(), "outside recovery root") {
		t.Fatalf("RecoveryReplay error = %v, want an outside recovery root error", err)
	}
}

func TestRecoveryReplayRejectsExistingStagingFileSymlinkOutsideRoot(t *testing.T) {
	root, state := replayFixture(t)
	outside := t.TempDir()
	outsideState := filepath.Join(outside, "loop-state.json")
	outsideJournal := filepath.Join(outside, "loop-events.jsonl")
	writeReplayPair(t, outsideState, outsideJournal, state)

	stagingDir := filepath.Join(root, ".claude", "recovery-staging-existing-link")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stagingDir, "loop-state.json")
	journalPath := filepath.Join(stagingDir, "loop-events.jsonl")
	if err := os.Symlink(outsideState, statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   statePath,
		JournalPath: journalPath,
	})
	if err == nil || !strings.Contains(err.Error(), "outside recovery root") {
		t.Fatalf("RecoveryReplay error = %v, want an outside recovery root error", err)
	}
}

func TestRecoveryReplayRejectsStagingParentSymlinkOutsideRoot(t *testing.T) {
	root, state := replayFixture(t)
	outside := t.TempDir()
	outsideState := filepath.Join(outside, "loop-state.json")
	outsideJournal := filepath.Join(outside, "loop-events.jsonl")
	writeReplayPair(t, outsideState, outsideJournal, state)

	linkDir := filepath.Join(root, ".claude", "recovery-staging-parent-link")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(linkDir, "loop-state.json")
	journalPath := filepath.Join(linkDir, "loop-events.jsonl")

	_, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   statePath,
		JournalPath: journalPath,
	})
	if err == nil || !strings.Contains(err.Error(), "outside recovery root") {
		t.Fatalf("RecoveryReplay error = %v, want an outside recovery root error", err)
	}
}

func TestRecoveryReplayRejectsStagingAliasOfActivePairWhenClaudeIsSymlink(t *testing.T) {
	root, _ := replayFixture(t)
	claudeDir := filepath.Join(root, ".claude")
	claudeTarget := filepath.Join(root, "claude-data")
	if err := os.Rename(claudeDir, claudeTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(claudeTarget, claudeDir); err != nil {
		t.Fatal(err)
	}

	stagingDir := filepath.Join(root, "recovery-staging-active-alias")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stagingDir, "loop-state.json")
	journalPath := filepath.Join(stagingDir, "loop-events.jsonl")
	if err := os.Symlink(filepath.Join(claudeDir, "loop-state.json"), statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(claudeDir, "loop-events.jsonl"), journalPath); err != nil {
		t.Fatal(err)
	}

	_, err := controller.RecoveryReplay(context.Background(), controller.RecoveryReplayRequest{
		Root:        root,
		StatePath:   statePath,
		JournalPath: journalPath,
	})
	if err == nil || !strings.Contains(err.Error(), "active Runtime paths") {
		t.Fatalf("RecoveryReplay error = %v, want active Runtime path error", err)
	}
}

type replayEvaluator struct {
	status qualitygate.Status
	code   string
}

func (e replayEvaluator) Evaluate(context.Context, qualitygate.Input) (qualitygate.Evaluation, error) {
	return qualitygate.Evaluation{Status: e.status, ErrorCode: e.code}, nil
}

func replayFixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	root := req039fixtures.FreshRoot(t)
	state := req039fixtures.BaseState(t, root, "planning", "design", 1)
	req039fixtures.SeedPlanningDesignComplete(t, root, state)
	req039fixtures.WritePlanningContractPass(t, root, state)
	req039fixtures.WritePlanningTaskPass(t, root, state)
	reqData, err := os.ReadFile(filepath.Join(root, "docs/requirements/REQ-039-loop-control-plane.md"))
	if err != nil {
		t.Fatal(err)
	}
	state["bound_req"].(map[string]any)["sha256"] = req039fixtures.Sha256Hex(reqData)
	writeReplayPair(t, filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl"), state)
	return root, state
}

func stagingPair(t *testing.T, root string, state map[string]any) (string, string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "recovery-staging")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "loop-state.json")
	journalPath := filepath.Join(dir, "loop-events.jsonl")
	writeReplayPair(t, statePath, journalPath, state)
	return statePath, journalPath
}

func writeReplayPair(t *testing.T, statePath, journalPath string, state map[string]any) {
	t.Helper()
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func lifecycleCursor(state map[string]any) string {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	stateName, _ := lifecycle["state"].(string)
	phase, _ := lifecycle["phase"].(string)
	if phase == "" {
		return stateName
	}
	return stateName + "." + phase
}
