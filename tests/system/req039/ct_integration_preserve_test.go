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
// CT-039-10: merge conflict / not-ready inspection preserves worktree + blocker.
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
			"id": "builder-ct10", "role": "builder", "state": "reported",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-ct10",
		}},
		"tasks": []any{map[string]any{"id": "TASK-039-01", "state": "review"}},
		"bugs":  []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)

	assignmentManifest := map[string]any{
		"assignment_id": "assignment-ct10",
		"agent_id":      "builder-ct10",
		"worktree_path": repo.wtPath,
		"branch":        repo.branch,
		"target_branch": "develop",
	}
	manifestDir := filepath.Join(root, ".claude", "assignments")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestRaw := `{"schema_version":"1.0.0","assignment_id":"assignment-ct10","agent_id":"builder-ct10","worktree_path":"` + repo.wtPath + `","branch":"` + repo.branch + `","target_branch":"develop"}`
	_ = assignmentManifest
	if err := os.WriteFile(filepath.Join(manifestDir, "assignment-ct10.json"), []byte(manifestRaw), 0o644); err != nil {
		t.Fatal(err)
	}

	body := req039fixtures.SubagentStopBody("session-ct-039-10", "builder-ct10", "assignment-ct10")
	code, stdout, stderr := runHookWithRunner(t, runner, root, "SubagentStop", body)
	if code != 0 {
		t.Fatalf("SubagentStop failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	out := stdout + stderr
	for _, needle := range []string{"preserv", "block", "conflict", "not ready", "not_ready"} {
		if strings.Contains(strings.ToLower(out), needle) {
			goto preservedOK
		}
	}
	t.Fatalf("CT-039-10 must surface preserve/blocker guidance on conflict, got: %s", out)
preservedOK:
	if _, err := os.Stat(repo.wtPath); err != nil {
		t.Fatalf("CT-039-10 worktree must be preserved on conflict: %v", err)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-10 must not use manual transition CLI")
	}
}
