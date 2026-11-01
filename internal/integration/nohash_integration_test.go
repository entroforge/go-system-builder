package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

// Exercise the real merge and Main check while asserting the framework's
// checkpoint retains responsibility and completed steps without OIDs/digests.
func TestNoHashRealMergeChecksAndReplay(t *testing.T) {
	root := t.TempDir()
	gitRevisionTest(t, root, "init", "-b", "main")
	gitRevisionTest(t, root, "config", "user.email", "test@example.com")
	gitRevisionTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", ".gitignore")
	gitRevisionTest(t, root, "commit", "-m", "initial")
	worker := filepath.Join(t.TempDir(), "worker")
	gitRevisionTest(t, root, "worktree", "add", "-b", "worker", worker, "main")
	if err := os.WriteFile(filepath.Join(worker, "product.txt"), []byte("actual contribution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, worker, "add", "product.txt")
	gitRevisionTest(t, worker, "commit", "-m", "contribution")
	a := hookctx.AssignmentContext{AssignmentID: "assignment-real", TaskID: "TASK-real", WorktreePath: worker, Branch: "worker", TargetBranch: "main"}
	inspection, err := Inspect(context.Background(), InspectRequest{Root: root, Assignment: a, RuntimeID: "loop-real", BaselineGeneration: 1}, InspectConfig{SkipCompletionCheck: true})
	if err != nil || !inspection.Ready {
		t.Fatalf("inspect: ready=%v blockers=%v err=%v", inspection.Ready, inspection.Blockers, err)
	}
	cfg := IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "loop-real", RequiredChecks: []string{"test -f product.txt"}, CheckRunner: CommandCheckRunner}
	first, err := Integrate(context.Background(), IntegrateRequest{Inspection: inspection}, cfg)
	if err != nil || first.Checkpoint.State != StateVerified {
		t.Fatalf("merge/check: state=%s err=%v", first.Checkpoint.State, err)
	}
	if content, err := os.ReadFile(filepath.Join(root, "product.txt")); err != nil || string(content) != "actual contribution\n" {
		t.Fatalf("actual contribution missing: %q, %v", content, err)
	}
	count := gitRevisionTest(t, root, "rev-list", "--count", "main")
	checkpoint := CheckpointPath(root, "loop-real", 1, "assignment-real")
	encoded, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(encoded, &saved); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source_head", "target_head", "merge_base", "merge_commit", "tested_head", "changed_paths_sha256", "completion_report_sha256", "assignment_record_sha256"} {
		if _, exists := saved[key]; exists {
			t.Fatalf("checkpoint retains %s", key)
		}
	}
	resumed, err := Integrate(context.Background(), IntegrateRequest{Inspection: inspection, Acknowledge: true}, cfg)
	if err != nil || resumed.Checkpoint.State != StateAcknowledged {
		t.Fatalf("ack: state=%s err=%v", resumed.Checkpoint.State, err)
	}
	if after := gitRevisionTest(t, root, "rev-list", "--count", "main"); after != count {
		t.Fatalf("replay merged twice: %s -> %s", count, after)
	}
}

func TestNoHashReadyWindowPreservesUnknown(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()
	in := f.readyInspection()
	cp := Checkpoint{AssignmentID: in.AssignmentID, WorktreePath: in.WorktreePath, SourceBranch: in.SourceBranch, TargetBranch: in.TargetBranch, BaselineGeneration: 1, State: StateReady}
	path := f.checkpointPath(in.AssignmentID)
	if _, err := DefaultCheckpointStore().CompareAndSwap(path, Checkpoint{}, cp); err != nil {
		t.Fatal(err)
	}
	_, err := Integrate(context.Background(), IntegrateRequest{Inspection: in}, IntegrateConfig{Root: f.root, CheckpointDir: path})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("interrupted merge intent must stay unknown: %v", err)
	}
}

// A failed real Main check keeps the recorded merge and only the check is
// retried. No source digest or Git object ID is needed to resume.
func TestNoHashFailedMainCheckRetriesWithoutRemerging(t *testing.T) {
	root := t.TempDir()
	gitRevisionTest(t, root, "init", "-b", "main")
	gitRevisionTest(t, root, "config", "user.email", "test@example.com")
	gitRevisionTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, root, "add", ".gitignore")
	gitRevisionTest(t, root, "commit", "-m", "initial")
	worker := filepath.Join(t.TempDir(), "worker")
	gitRevisionTest(t, root, "worktree", "add", "-b", "worker", worker, "main")
	if err := os.WriteFile(filepath.Join(worker, "product.txt"), []byte("actual contribution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRevisionTest(t, worker, "add", "product.txt")
	gitRevisionTest(t, worker, "commit", "-m", "contribution")
	a := hookctx.AssignmentContext{AssignmentID: "assignment-retry", TaskID: "TASK-retry", WorktreePath: worker, Branch: "worker", TargetBranch: "main"}
	inspection, err := Inspect(context.Background(), InspectRequest{Root: root, Assignment: a, RuntimeID: "loop-retry", BaselineGeneration: 1}, InspectConfig{SkipCompletionCheck: true})
	if err != nil || !inspection.Ready {
		t.Fatalf("inspect: ready=%v blockers=%v err=%v", inspection.Ready, inspection.Blockers, err)
	}
	checks := 0
	cfg := IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "loop-retry", RequiredChecks: []string{"actual check"}, CheckRunner: func(context.Context, string, string) error {
		checks++
		return fmt.Errorf("actual check failed")
	}}
	failed, err := Integrate(context.Background(), IntegrateRequest{Inspection: inspection}, cfg)
	if !errors.Is(err, ErrCheckFailed) || failed.Checkpoint.State != StatePreserved || failed.Checkpoint.ResumeState != StateMerged {
		t.Fatalf("actual failure not preserved after merge: %+v, %v", failed.Checkpoint, err)
	}
	count := gitRevisionTest(t, root, "rev-list", "--count", "main")
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
	verified, err := Integrate(context.Background(), IntegrateRequest{Inspection: inspection, RetryPreserved: true}, cfg)
	if err != nil || verified.Checkpoint.State != StateVerified || checks != 2 {
		t.Fatalf("retry did not perform actual check: %+v, checks=%d, %v", verified.Checkpoint, checks, err)
	}
	if after := gitRevisionTest(t, root, "rev-list", "--count", "main"); after != count {
		t.Fatalf("retry merged twice: %s -> %s", count, after)
	}
}
