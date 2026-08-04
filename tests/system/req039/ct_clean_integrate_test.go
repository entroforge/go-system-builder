// ct_clean_integrate_test.go — CT-039-09 / AC-006 clean SubagentStop integrate
// path via system Hook CLI. Full merge → verified → ack → cleanup → complete
// (BUG-039-38 Closing Contract).

package req039_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03909_CleanWorktreeStopViaSubagentStop covers SYNC-039 §12 CT-039-09:
// first SubagentStop merges to verified; second reaches complete, removes the
// worktree, and leaves develop HEAD unchanged (no re-merge).
func TestCT03909_CleanWorktreeStopViaSubagentStop(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	repo := setupGitWorktreeFixture(t, root)

	state := systemPlanningState(t, root, "tasks", 11)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-ct09", "role": "builder", "state": "reported",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-ct09",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-039-01", "state": "review",
			"owner_agent_ids": []any{"builder-ct09"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)

	writeWorkgroupWithWorktree(t, root, "TASK-039-01", "assignment-ct09", "builder-ct09", repo.wtPath, repo.branch)
	writeCompletionReport(t, root, "loop-system-test", "assignment-ct09")

	developBefore := repo.developHEAD()
	body := req039fixtures.SubagentStopBody("session-ct-039-09", "builder-ct09", "assignment-ct09")
	code, stdout, stderr := runHookWithRunner(t, runner, root, "SubagentStop", body)
	if code != 0 {
		t.Fatalf("SubagentStop failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-09 must not use manual transition CLI")
	}

	out := stdout + stderr
	developAfterFirst := repo.developHEAD()
	if developAfterFirst == developBefore {
		t.Fatalf("CT-039-09 clean stop must non-squash merge into develop; out=%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "state=verified") &&
		!strings.Contains(strings.ToLower(out), "verified") {
		t.Fatalf("CT-039-09 must surface verified progress, got %s", out)
	}
	cpPath, cpState := readIntegrationCheckpoint(t, root)
	if cpState != "verified" && cpState != "merged" {
		t.Fatalf("CT-039-09 durable checkpoint want verified/merged, got %q path=%s", cpState, cpPath)
	}
	if !strings.Contains(cpPath, filepath.Join("worktree", "assignment-ct09")) {
		t.Fatalf("CT-039-09 checkpoint must be keyed by assignment_id, got path=%s", cpPath)
	}

	// Second SubagentStop: ack + cleanup → complete (BUG-039-38).
	code2, stdout2, stderr2 := runHookWithRunner(t, runner, root, "SubagentStop", body)
	if code2 != 0 {
		t.Fatalf("second SubagentStop failed: code=%d stderr=%s stdout=%s", code2, stderr2, stdout2)
	}
	out2 := stdout2 + stderr2
	developAfterSecond := repo.developHEAD()
	if developAfterSecond != developAfterFirst {
		t.Fatalf("CT-039-09 second stop must not re-merge: before=%s after=%s out=%s",
			developAfterFirst, developAfterSecond, out2)
	}
	if !strings.Contains(strings.ToLower(out2), "state=complete") &&
		!strings.Contains(strings.ToLower(out2), "complete") {
		t.Fatalf("CT-039-09 second stop must surface complete, got %s", out2)
	}
	_, cpState2 := readIntegrationCheckpoint(t, root)
	if cpState2 != "complete" {
		t.Fatalf("CT-039-09 second stop durable checkpoint want complete, got %q (forbidden: verified-only)", cpState2)
	}
	if _, err := os.Stat(repo.wtPath); !os.IsNotExist(err) {
		t.Fatalf("CT-039-09 complete must remove worktree at %s (stat err=%v)", repo.wtPath, err)
	}
}

// TestAC006_SubagentStopCleanIntegrateViaHook is the AC-006 twin of CT-039-09.
func TestAC006_SubagentStopCleanIntegrateViaHook(t *testing.T) {
	TestCT03909_CleanWorktreeStopViaSubagentStop(t)
}

func writeWorkgroupWithWorktree(t *testing.T, root, taskID, assignmentID, agentID, wtPath, branch string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "workgroups", "REQ-039", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"schema_version":"1.0.0",
		"manifest_id":"team-manifest-` + assignmentID + `",
		"version":"v1.0.0",
		"runtime_id":"loop-system-test",
		"req_id":"REQ-039",
		"baseline_generation":1,
		"status":"active",
		"workgroup_id":"workgroup-` + assignmentID + `",
		"workgroup_kind":"builder",
		"assignments":[{
			"assignment_id":"` + assignmentID + `",
			"responsibility_id":"BUILD-WORK-PACKAGE",
			"role_family":"backend-builder",
			"agent_id":"` + agentID + `",
			"write_paths":["internal/"],
			"status":"complete",
			"worktree_path":"` + wtPath + `",
			"branch":"` + branch + `",
			"target_branch":"develop"
		}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCompletionReport(t *testing.T, root, runtimeID, assignmentID string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "evidence", runtimeID, "g1", "assignments", assignmentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := `{"schema_version":"1.0.0","message_type":"completion_report","assignment_id":"` + assignmentID + `"}`
	if err := os.WriteFile(filepath.Join(dir, "completion.json"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readIntegrationCheckpointState(t *testing.T, root string) string {
	t.Helper()
	_, state := readIntegrationCheckpoint(t, root)
	return state
}

func readIntegrationCheckpoint(t *testing.T, root string) (path, state string) {
	t.Helper()
	_ = filepath.Walk(filepath.Join(root, ".claude", "evidence"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, "checkpoint.json") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var cp map[string]any
		if json.Unmarshal(raw, &cp) != nil {
			return nil
		}
		if s, _ := cp["state"].(string); s != "" {
			path = p
			state = s
		}
		return nil
	})
	return path, state
}
