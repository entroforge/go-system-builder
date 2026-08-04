// ct_integration_resume_test.go — system-level CT-039-17 via SubagentStop Hook.
// After merge→verified, second SubagentStop resumes ack/cleanup → complete
// without re-merging develop (BUG-039-38).

package req039_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCT03917_SubagentStopIdempotentResumeAfterMerge covers CT-039-17:
// after merge→verified, a second SubagentStop must not re-merge develop and
// must advance the durable checkpoint to complete (ack+cleanup resume).
func TestCT03917_SubagentStopIdempotentResumeAfterMerge(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	repo := setupGitWorktreeFixture(t, root)

	state := systemPlanningState(t, root, "tasks", 10)
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	state["entities"] = map[string]any{
		"agents": []any{map[string]any{
			"id": "builder-ct17", "role": "builder", "state": "reported",
			"task_ids": []any{"TASK-039-01"}, "team_id": "team-ct17",
		}},
		"tasks": []any{map[string]any{
			"id": "TASK-039-01", "state": "review",
			"owner_agent_ids": []any{"builder-ct17"},
		}},
		"bugs": []any{}, "teams": []any{},
	}
	writeSystemState(t, root, state)

	writeWorkgroupWithWorktree(t, root, "TASK-039-01", "assignment-ct17", "builder-ct17", repo.wtPath, repo.branch)
	writeCompletionReport(t, root, "loop-system-test", "assignment-ct17")

	body := req039fixtures.SubagentStopBody("session-ct-039-17", "builder-ct17", "assignment-ct17")
	developBefore := repo.developHEAD()

	code, stdout, stderr := runHookWithRunner(t, runner, root, "SubagentStop", body)
	if code != 0 {
		t.Fatalf("first SubagentStop failed: code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("CT-039-17 must not use manual transition CLI")
	}

	developAfterFirst := repo.developHEAD()
	if developAfterFirst == developBefore {
		t.Fatalf("CT-039-17 first SubagentStop must merge into develop; out=%s", stdout+stderr)
	}
	cpState := readIntegrationCheckpointState(t, root)
	if cpState != "verified" && cpState != "merged" {
		t.Fatalf("CT-039-17 first stop checkpoint want verified/merged, got %q", cpState)
	}

	// Second SubagentStop: must not re-merge; must resume to complete.
	code2, stdout2, stderr2 := runHookWithRunner(t, runner, root, "SubagentStop", body)
	if code2 != 0 {
		t.Fatalf("resume SubagentStop failed: code=%d stderr=%s stdout=%s", code2, stderr2, stdout2)
	}
	out2 := stdout2 + stderr2
	developAfterSecond := repo.developHEAD()
	if developAfterSecond != developAfterFirst {
		t.Fatalf("CT-039-17 idempotent resume must not re-merge: before=%s after=%s out=%s",
			developAfterFirst, developAfterSecond, out2)
	}
	cpState2 := readIntegrationCheckpointState(t, root)
	if cpState2 != "complete" {
		t.Fatalf("CT-039-17 resume durable checkpoint want complete, got %q out=%s", cpState2, out2)
	}
	if _, err := os.Stat(repo.wtPath); !os.IsNotExist(err) {
		t.Fatalf("CT-039-17 complete must remove worktree at %s (stat err=%v)", repo.wtPath, err)
	}
}

type gitWorktreeFixture struct {
	wtPath      string
	branch      string
	developHEAD func() string
}

func setupGitWorktreeFixture(t *testing.T, root string) gitWorktreeFixture {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--initial-branch=develop"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"checkout", "-b", "develop"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		runGitIn(t, root, args...)
	}
	wtPath := filepath.Join(root, "wt-feature")
	runGitIn(t, root, "worktree", "add", "-b", "codex/feature", wtPath, "develop")
	runGitIn(t, wtPath, "commit", "--allow-empty", "-m", "feature")
	head := func() string {
		out := runGitIn(t, root, "rev-parse", "develop")
		return strings.TrimSpace(out)
	}
	return gitWorktreeFixture{wtPath: wtPath, branch: "codex/feature", developHEAD: head}
}

func runGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}
