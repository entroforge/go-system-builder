package req039_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		"runtime_id":"` + readSystemState(t, root)["runtime_id"].(string) + `",
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
