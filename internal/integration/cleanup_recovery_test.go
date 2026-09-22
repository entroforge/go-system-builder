package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupKeepsOriginalCoordinatesAndCanResume(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()
	cfg := IntegrateConfig{Root: f.root, GitRoot: f.root, RuntimeID: "loop-REQ-039", CheckpointDir: f.checkpointPath("assignment-test")}
	initial, err := Integrate(context.Background(), IntegrateRequest{Inspection: f.readyInspection()}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	late := f.readyInspection()
	late.MergeBase = late.SourceHead
	late.TargetHead = initial.Checkpoint.MergeCommit
	f.fr.markDirty(f.wt)
	got, err := Integrate(context.Background(), IntegrateRequest{Inspection: late, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) {
		t.Fatalf("want cleanup error: %v", err)
	}
	if got.Checkpoint.State != StateCleanupPending || got.Checkpoint.MergeBase != "base-commit" || got.Checkpoint.TargetHead != "base-commit" {
		t.Fatalf("coordinates regressed: %+v", got.Checkpoint)
	}
}

func TestMergedRecoveryUsesRealParentsAndRerunsChecks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	wt := filepath.Join(t.TempDir(), "work")
	old := defaultRunner
	defaultRunner = execRunner{}
	defer func() { defaultRunner = old }()
	git := func(args ...string) string {
		t.Helper()
		out, err := defaultRunner.Run(ctx, root, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	git("init", "-b", "develop")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("base"), 0644)
	git("add", "a.txt")
	git("commit", "-m", "base")
	base := git("rev-parse", "HEAD")
	git("worktree", "add", "-b", "feature", wt)
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("feature"), 0644)
	if _, err := defaultRunner.Run(ctx, wt, "add", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultRunner.Run(ctx, wt, "commit", "-m", "feature"); err != nil {
		t.Fatal(err)
	}
	source := git("rev-parse", "feature")
	git("merge", "--no-ff", "feature", "-m", "merge")
	merge := git("rev-parse", "HEAD")
	cp := Checkpoint{AssignmentID: "a", WorktreePath: wt, SourceBranch: "feature", SourceHead: source, TargetBranch: "develop", TargetHead: merge, MergeBase: source, MergeCommit: merge, BaselineGeneration: 1, State: StatePreserved}
	restored, err := RecoveryBaseForCheckpoint(ctx, root, cp)
	if err != nil || restored != base {
		t.Fatalf("base=%s err=%v", restored, err)
	}
	bad := cp
	bad.SourceHead = base
	if _, err := RecoveryBaseForCheckpoint(ctx, root, bad); err == nil {
		t.Fatal("accepted mismatched source")
	}
	bad = cp
	bad.TargetBranch = "feature"
	if _, err := RecoveryBaseForCheckpoint(ctx, root, bad); err == nil {
		t.Fatal("accepted target missing merge")
	}
	store := DefaultCheckpointStore()
	cpPath := filepath.Join(t.TempDir(), "checkpoint.json")
	if _, err := store.ForceWrite(cpPath, cp); err != nil {
		t.Fatal(err)
	}
	inspection := Inspection{Ready: true, AssignmentID: "a", WorktreePath: wt, SourceBranch: "feature", SourceHead: source, TargetBranch: "develop", TargetHead: merge, MergeBase: base, BaselineGeneration: 1, NonSquashMode: true}
	checks := 0
	cfg := IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "test", CheckpointDir: cpPath, RequiredChecks: []string{"check"}, CheckRunner: func(context.Context, string, string) error { checks++; return errors.New("real failure") }}
	got, err := Integrate(ctx, IntegrateRequest{Inspection: inspection, RetryPreserved: true}, cfg)
	if !errors.Is(err, ErrCheckFailed) || got.Checkpoint.State != StatePreserved || checks != 1 {
		t.Fatalf("failed check bypassed: %+v %v %d", got, err, checks)
	}
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
	got, err = Integrate(ctx, IntegrateRequest{Inspection: inspection, RetryPreserved: true}, cfg)
	if err != nil || got.Checkpoint.State != StateVerified || checks != 2 {
		t.Fatalf("retry failed: %+v %v %d", got, err, checks)
	}
	if git("rev-parse", "HEAD") != merge {
		t.Fatal("recovery created another merge")
	}
	// Untracked test artifacts make git refuse removal. Verification must survive.
	os.WriteFile(filepath.Join(wt, "test-output.log"), []byte("retain me"), 0644)
	got, err = Integrate(ctx, IntegrateRequest{Inspection: inspection, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, ErrCleanupPending) || got.Checkpoint.State != StateCleanupPending {
		t.Fatalf("cleanup lost verification: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "test-output.log")); err != nil {
		t.Fatal("untracked output removed")
	}
	os.Remove(filepath.Join(wt, "test-output.log"))
	got, err = Integrate(ctx, IntegrateRequest{Inspection: inspection, Acknowledge: true, Cleanup: true}, cfg)
	if err != nil || got.Checkpoint.State != StateComplete || checks != 2 {
		t.Fatalf("cleanup retry: %+v %v checks=%d", got, err, checks)
	}
}

func TestResponseLossRecoversExactMergeAndAbsentWorktree(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	wt := filepath.Join(t.TempDir(), "worker")
	old := defaultRunner
	defaultRunner = execRunner{}
	defer func() { defaultRunner = old }()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := defaultRunner.Run(ctx, dir, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	git(root, "init", "-b", "test2")
	git(root, "config", "user.name", "Test")
	git(root, "config", "user.email", "test@example.invalid")
	git(root, "commit", "--allow-empty", "-m", "base")
	base := git(root, "rev-parse", "HEAD")
	git(root, "worktree", "add", "-b", "codex/worker", wt, base)
	git(wt, "commit", "--allow-empty", "-m", "worker")
	source := git(wt, "rev-parse", "HEAD")
	store := DefaultCheckpointStore()
	cpPath := filepath.Join(t.TempDir(), "checkpoint.json")
	cp := Checkpoint{AssignmentID: "a", WorktreePath: wt, SourceBranch: "codex/worker", SourceHead: source, TargetBranch: "test2", TargetHead: base, MergeBase: base, BaselineGeneration: 1, State: StateReady}
	if _, err := store.ForceWrite(cpPath, cp); err != nil {
		t.Fatal(err)
	}
	git(root, "merge", "--no-ff", source, "-m", "merge")
	merged := git(root, "rev-parse", "HEAD")
	cfg := IntegrateConfig{Root: root, RuntimeID: "loop-test", CheckpointDir: cpPath, RequiredChecks: []string{"true"}, CheckRunner: CommandCheckRunner}
	got, err := Integrate(ctx, IntegrateRequest{Inspection: InspectionFromCheckpoint(cp)}, cfg)
	if err != nil || got.Checkpoint.State != StateVerified || got.Checkpoint.MergeCommit != merged {
		t.Fatalf("response loss: %+v %v", got, err)
	}
	if got.Checkpoint.TestedHead != merged || len(got.Checkpoint.CheckReceipts) != 1 {
		t.Fatalf("missing checked-tree receipt: %+v", got.Checkpoint)
	}
	if _, err := os.Stat(got.Checkpoint.CheckReceipts[0]); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after removal, before completion CAS.
	cp = got.Checkpoint
	cp.State = StateCleanupPending
	if _, err = store.ForceWrite(cpPath, cp); err != nil {
		t.Fatal(err)
	}
	git(root, "worktree", "remove", wt)
	got, err = Integrate(ctx, IntegrateRequest{Inspection: InspectionFromCheckpoint(cp), Acknowledge: true, Cleanup: true}, cfg)
	if err != nil || got.Checkpoint.State != StateComplete {
		t.Fatalf("absent cleanup: %+v %v", got, err)
	}
	if git(root, "rev-parse", "HEAD") != merged {
		t.Fatal("recovery remerged")
	}
	if git(root, "branch", "--show-current") != "test2" {
		t.Fatal("main moved")
	}
}
