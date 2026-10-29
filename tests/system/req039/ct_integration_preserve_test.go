// ct_integration_preserve_test.go — L4 CT-039-10: conflict/dirty preserve via SubagentStop Hook.

package req039_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03910_ConflictPreservesWorktreeViaSubagentStop covers SYNC-039 §12
// CT-039-10: an unreported conflict keeps the worktree intact and Stop reminds
// the worker to submit its original PLAN_REPORT/Result.
func TestCT03910_ConflictPreservesWorktreeViaSubagentStop(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	repo := setupGitWorktreeFixture(t, root)

	// Create a merge conflict: feature branch touches README, develop diverges.
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("develop line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", "README.md")
	runGitIn(t, root, "commit", "-m", "develop change")
	if err := os.WriteFile(filepath.Join(repo.wtPath, "README.md"), []byte("feature line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, repo.wtPath, "add", "README.md")
	runGitIn(t, repo.wtPath, "commit", "-m", "feature change")

	state := systemPlanningState(t, root, "tasks", 12)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-ct10", "role": "builder", "state": "working",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-ct10",
		}},
		"tasks": []any{map[string]any{"id": "TASK-039-01", "state": "review"}},
		"bugs":  []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)

	manifestDir := filepath.Join(root, ".claude", "assignments")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestRaw := `{"schema_version":"1.0.0","assignment_id":"assignment-ct10","agent_id":"builder-ct10","worktree_path":"` + repo.wtPath + `","branch":"` + repo.branch + `","target_branch":"develop"}`
	if err := os.WriteFile(filepath.Join(manifestDir, "assignment-ct10.json"), []byte(manifestRaw), 0o644); err != nil {
		t.Fatal(err)
	}

	before := req039fixtures.ReadState(t, root)
	body := req039fixtures.SubagentStopBody("session-ct-039-10", "builder-ct10", "assignment-ct10")
	code, stdout, stderr := runHookWithRunner(t, runner, root, "SubagentStop", body)
	if code != 2 || !strings.Contains(stderr, "subagent_stop_missing_result") || !strings.Contains(stderr, "PLAN_REPORT") {
		t.Fatalf("SubagentStop must remind the worker to submit its actual delivery: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	assertRuntimeCursorUnchanged(t, "SubagentStop missing-delivery reminder", before, req039fixtures.ReadState(t, root))
	if _, err := os.Stat(repo.wtPath); err != nil {
		t.Fatalf("CT-039-10 worktree must be preserved on conflict: %v", err)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-10 must not use manual transition CLI")
	}
}
