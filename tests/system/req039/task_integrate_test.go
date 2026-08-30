// task_integrate_test.go — L3-S6 complexity pass N1: the explicit
// `runtime task-integrate` verb drives the identical Inspect → non-squash
// merge → verified checkpoint chain as the SubagentStop hook, without
// depending on the platform payload carrying the assignment identity.
package req039_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// seedIntegrableAssignment prepares a building runtime whose assignment
// has a real git worktree, a registered workgroup manifest, and a
// completion report — everything task-integrate needs.
func seedIntegrableAssignment(t *testing.T, root string) string {
	t.Helper()
	repo := setupGitWorktreeFixture(t, root)

	state := systemPlanningState(t, root, "tasks", 12)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-ti", "role": "builder", "state": "reported",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-ti",
			"definition_ref": ".claude/agents/backend-builder.md",
			"prompt_ref":     "manifest#assignment-ti",
			"readback_ref":   nil, "activation_ref": nil, "activation_revision": nil,
			"updated_at": "2026-08-20T00:00:00Z",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-039-01", "state": "review",
			"path":            "docs/tasks/TASK-039-01.md",
			"sha256":          "0000000000000000000000000000000000000000000000000000000000000001",
			"owner_agent_ids": []any{"builder-ti"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)

	writeWorkgroupWithWorktree(t, root, "TASK-039-01", "assignment-ti", "builder-ti", repo.wtPath, repo.branch)
	writeCompletionReport(t, root, "loop-system-test", "assignment-ti")
	return repo.wtPath
}

func runTaskIntegrate(t *testing.T, root, assignmentID string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "task-integrate", "--root", root, "--assignment-id", assignmentID,
	}, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTaskIntegrateMergesWorktreeToVerified(t *testing.T) {
	root := freshRoot(t)
	wtPath := seedIntegrableAssignment(t, root)
	developBefore := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))

	code, stdout, stderr := runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("task-integrate failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stderr, "integrated") {
		t.Fatalf("task-integrate must report integration, stderr=%s", stderr)
	}
	developAfter := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	if developAfter == developBefore {
		t.Fatalf("task-integrate must advance develop (merge missing): %s", stderr)
	}
	// The merge is non-squash: develop's new head has two parents.
	parents := runGitIn(t, root, "show", "-s", "--format=%P", developAfter)
	if len(strings.Fields(strings.TrimSpace(parents))) != 2 {
		t.Fatalf("task-integrate must produce a merge commit, parents=%q", parents)
	}
	// Durable checkpoint reached verified with the task bound.
	checkpointPath := filepath.Join(root, ".claude", "evidence", "loop-system-test", "g1", "worktree", "assignment-ti", "checkpoint.json")
	data, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatalf("durable checkpoint missing: %v", err)
	}
	var checkpoint struct {
		State  string `json:"state"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.State != "verified" {
		t.Fatalf("checkpoint state = %q, want verified (checkpoint=%s)", checkpoint.State, data)
	}
	if checkpoint.TaskID != "TASK-039-01" {
		t.Fatalf("checkpoint task_id = %q, want TASK-039-01 (gate binding)", checkpoint.TaskID)
	}
	_ = wtPath
}

func TestTaskIntegrateUnknownAssignmentListsKnown(t *testing.T) {
	root := freshRoot(t)
	seedIntegrableAssignment(t, root)

	code, _, stderr := runTaskIntegrate(t, root, "assignment-nope")
	if code == 0 {
		t.Fatal("unknown assignment must fail")
	}
	if !strings.Contains(stderr, "assignment-ti") {
		t.Fatalf("error must list the known assignment id for discoverability, stderr=%s", stderr)
	}
}

func TestTaskIntegrateConflictPreservesWorktree(t *testing.T) {
	root := freshRoot(t)
	wtPath := seedIntegrableAssignment(t, root)

	// Diverge develop and the feature branch on the same in-scope file.
	internalFile := filepath.Join(root, "internal", "shared.go")
	if err := os.MkdirAll(filepath.Dir(internalFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(internalFile, []byte("// develop line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", "internal/shared.go")
	runGitIn(t, root, "commit", "-m", "develop change")
	if err := os.WriteFile(filepath.Join(wtPath, "internal", "shared.go"), []byte("// feature line\n"), 0o644); err != nil {
		if err := os.MkdirAll(filepath.Join(wtPath, "internal"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wtPath, "internal", "shared.go"), []byte("// feature line\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitIn(t, wtPath, "add", "internal/shared.go")
	runGitIn(t, wtPath, "commit", "-m", "feature change")

	code, stdout, stderr := runTaskIntegrate(t, root, "assignment-ti")
	out := stdout + stderr
	if !strings.Contains(strings.ToLower(out), "preserv") && !strings.Contains(strings.ToLower(out), "block") {
		t.Fatalf("conflict must surface preserve/blocker guidance, out=%s", out)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("conflicted worktree must be preserved: %v", err)
	}
	// Inspect-stage conflicts persist the milestone record (the durable
	// checkpoint file is only written once Integrate owns the attempt).
	after := req039fixtures.ReadState(t, root)
	milestone, _ := after["milestone"].(map[string]any)
	entries, _ := milestone["integration"].([]any)
	joined := ""
	for _, raw := range entries {
		if s, ok := raw.(string); ok {
			joined += s + ";"
		}
	}
	if !strings.Contains(joined, "status=preserved") {
		t.Fatalf("milestone must record status=preserved, got %v", entries)
	}
	_ = code
}
