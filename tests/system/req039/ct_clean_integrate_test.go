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
// SubagentStop returns a short pending action; explicit integration verifies,
// acknowledges and cleans up. Repeating the command never re-merges.
func TestCT03909_CleanWorktreeStopViaSubagentStop(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	wt := seedIntegrableAssignment(t, root)
	before := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	body := req039fixtures.SubagentStopBody("session-ct09", "builder-ti", "assignment-ti")
	code, stdout, stderr := runHookWithRunner(t, runner, root, "SubagentStop", body)
	if code != 0 {
		t.Fatalf("hook: %d %s %s", code, stdout, stderr)
	}
	if strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")) != before {
		t.Fatal("short Hook performed a merge")
	}
	if !strings.Contains(stdout+stderr, "task-integrate") {
		t.Fatal("Hook lost actionable integration follow-up")
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatal("Hook used a manual lifecycle transition")
	}
	code, stdout, stderr = runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("integrate: %d %s %s", code, stdout, stderr)
	}
	after := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	if before == after {
		t.Fatal("explicit integration did not merge")
	}
	_, state := readIntegrationCheckpoint(t, root)
	if state != "complete" {
		t.Fatalf("checkpoint=%s", state)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree remains: %v", err)
	}
	runtimeState := req039fixtures.ReadState(t, root)
	agent := runtimeState["entities"].(map[string]any)["agents"].([]any)[0].(map[string]any)
	if agent["state"] != "done" || agent["completion_acknowledged_ref"] == nil {
		t.Fatalf("missing durable Agent acknowledgment: %v", agent)
	}
	code, stdout, stderr = runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("repeat: %d %s %s", code, stdout, stderr)
	}
	if strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD")) != after {
		t.Fatal("repeat remerged")
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
