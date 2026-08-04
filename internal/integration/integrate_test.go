package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

// integrationFixture wires the fake runner + a real temp dir as the
// canonical evidence root. Returns paths the test can inspect.
type integrationFixture struct {
	fr            *fakeRunner
	root          string
	wt            string
	restoreRunner func()
	restoreStore  func()
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	fr, restoreRunner := runFakeRunner(t)
	root := t.TempDir()
	wt := filepath.Join(root, "wt-feature")

	// Wire fake git state for a clean, mergeable worktree.
	fr.addCommit("base-commit")
	fr.addCommit("feature-commit", "base-commit")
	fr.setBranch("feature", "feature-commit")
	fr.setBranch("develop", "base-commit")
	fr.checkout(wt, "feature")
	fr.checkout(root, "develop")
	fr.addWorktree(root, wt)

	// Stub a deterministic clock so checkpoint UpdatedAt is comparable.
	fixed := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	store := &CheckpointStore{
		Clock:     func() time.Time { return fixed },
		NowString: func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) },
	}
	restoreStore := withStore(store)

	return &integrationFixture{
		fr:            fr,
		root:          root,
		wt:            wt,
		restoreRunner: restoreRunner,
		restoreStore:  restoreStore,
	}
}

func (f *integrationFixture) cleanup() {
	f.restoreRunner()
	f.restoreStore()
}

func (f *integrationFixture) readyInspection() Inspection {
	return Inspection{
		Ready:              true,
		AssignmentID:       "assignment-test",
		WorktreePath:       f.wt,
		SourceBranch:       "feature",
		TargetBranch:       "develop",
		SourceHead:         "feature-commit",
		TargetHead:         "base-commit",
		MergeBase:          "base-commit",
		NonSquashMode:      true,
		BaselineGeneration: 1,
	}
}

func (f *integrationFixture) checkpointPath(assignmentID string) string {
	return DefaultCheckpointStore().Path(f.root, "loop-REQ-039", 1, assignmentID)
}

// TestIntegrateCleanPathReachesComplete covers CT-039-09:
//
//	ready → merged → verified → acknowledged → cleanup_pending → complete
func TestIntegrateCleanPathReachesComplete(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	// No Acknowledge, no Cleanup yet: first call should land at verified
	// and return early so the controller can write the ack.
	res, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if res.Checkpoint.State != StateVerified {
		t.Fatalf("expected verified, got %s", res.Checkpoint.State)
	}
	if res.Checkpoint.MergeCommit == "" {
		t.Fatalf("expected merge_commit to be recorded, got empty")
	}

	// Second call: Acknowledge=true should advance to acknowledged.
	res, err = Integrate(context.Background(), IntegrateRequest{
		Inspection:  f.readyInspection(),
		Acknowledge: true,
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatalf("integrate ack: %v", err)
	}
	if res.Checkpoint.State != StateAcknowledged {
		t.Fatalf("expected acknowledged, got %s", res.Checkpoint.State)
	}

	// Third call: Cleanup=true should complete the chain.
	res, err = Integrate(context.Background(), IntegrateRequest{
		Inspection:  f.readyInspection(),
		Acknowledge: true,
		Cleanup:     true,
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatalf("integrate cleanup: %v", err)
	}
	if res.Checkpoint.State != StateComplete {
		t.Fatalf("expected complete, got %s", res.Checkpoint.State)
	}
	if res.Checkpoint.MergeCommit == "" {
		t.Fatalf("expected merge_commit preserved at complete, got empty")
	}
}

// TestIntegrateConflictPreservesWorktree covers CT-039-10:
//
//	merge conflict → checkpoint=preserved, worktree retained, blocker surfaced
func TestIntegrateConflictPreservesWorktree(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	// Drive merge to fail: simulate a conflict by making the fake
	// runner's merge command exit non-zero. Easiest: remove the source
	// branch so merge --no-ff fails.
	f.fr.setBranch("missing-source", "")

	insp := f.readyInspection()
	insp.SourceBranch = "missing-source"

	res, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: insp,
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Fatalf("expected merge-related error, got %v", err)
	}
	if res.Checkpoint.State != StatePreserved {
		t.Fatalf("expected preserved, got %s", res.Checkpoint.State)
	}
	if res.Checkpoint.FailureReason == "" {
		t.Fatalf("expected failure_reason to be recorded")
	}
	// Worktree entry must still be present (we did NOT remove it).
	if !f.fr.worktrees[f.root+":"+f.wt] {
		t.Fatal("worktree should still be registered (preserved)")
	}
}

// TestIntegrateDirtyTreeDuringVerifyPreserves covers CT-039-10 for the
// dirty-tree branch: a merge that succeeds but the integration tree is
// dirty before checks run → preserved.
func TestIntegrateDirtyTreeDuringVerifyPreserves(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	// Mark the integration root (git root) as dirty so the verified
	// transition's `worktreeClean` check fails.
	f.fr.markDirty(f.root)

	res, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if res.Checkpoint.State != StatePreserved {
		t.Fatalf("expected preserved, got %s", res.Checkpoint.State)
	}
	if res.Checkpoint.LastErrorCode != "LOOP_INTEGRATION_PARTIAL" {
		t.Fatalf("expected LOOP_INTEGRATION_PARTIAL, got %s", res.Checkpoint.LastErrorCode)
	}
}

// TestIntegrateFailingCheckPreserves covers CT-039-10 for the check-fail
// branch.
func TestIntegrateFailingCheckPreserves(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	res, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root:           f.root,
		GitRoot:        f.root,
		CheckpointDir:  f.checkpointPath("assignment-test"),
		RuntimeID:      "loop-REQ-039",
		RequiredChecks: []string{"go test ./..."},
		CheckRunner: func(_ context.Context, _ string, _ string) error {
			return errBoom
		},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if res.Checkpoint.State != StatePreserved {
		t.Fatalf("expected preserved, got %s", res.Checkpoint.State)
	}
	if res.Checkpoint.LastErrorCode != "LOOP_INTEGRATION_CONFLICT" {
		t.Fatalf("expected LOOP_INTEGRATION_CONFLICT, got %s", res.Checkpoint.LastErrorCode)
	}
}

// TestIntegrateNotReadyInspectionPreserves ensures that handing Integrate
// a non-ready Inspection (i.e. Inspect said "no") persists a preserved
// checkpoint without running any git operation.
func TestIntegrateNotReadyInspectionPreserves(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	insp := f.readyInspection()
	insp.Ready = false
	insp.Blockers = []string{"inspect rejected integration"}

	res, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: insp,
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if res.Checkpoint.State != StatePreserved {
		t.Fatalf("expected preserved, got %s", res.Checkpoint.State)
	}
}

// TestIntegrateIdempotentResumeAfterMerge covers CT-039-17: merge
// succeeded but ack interrupted → resume does NOT re-merge.
func TestIntegrateIdempotentResumeAfterMerge(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	// First call: merge + verify (no ack yet).
	first, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatalf("first integrate: %v", err)
	}
	if first.Checkpoint.State != StateVerified {
		t.Fatalf("expected verified after first call, got %s", first.Checkpoint.State)
	}
	originalMerge := first.Checkpoint.MergeCommit
	if originalMerge == "" {
		t.Fatal("expected merge_commit to be recorded")
	}

	// Snapshot the fakeRunner's branch state so we can confirm no
	// second merge happens.
	developBefore := f.fr.branchHeads["develop"]

	// Second call: resume with Acknowledge=true. Should NOT re-merge.
	second, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  f.readyInspection(),
		Acknowledge: true,
	}, IntegrateConfig{
		Root:          f.root,
		GitRoot:       f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatalf("second integrate: %v", err)
	}
	if second.Checkpoint.State != StateAcknowledged {
		t.Fatalf("expected acknowledged, got %s", second.Checkpoint.State)
	}
	if second.Checkpoint.MergeCommit != originalMerge {
		t.Fatalf("merge_commit must be preserved: was %q now %q", originalMerge, second.Checkpoint.MergeCommit)
	}
	if f.fr.branchHeads["develop"] != developBefore {
		t.Fatalf("develop branch HEAD changed unexpectedly: was %q now %q",
			developBefore, f.fr.branchHeads["develop"])
	}
	if !second.Reused {
		t.Fatal("Reused must be true on idempotent resume")
	}
}

// TestIntegrateIdempotentReachingComplete verifies that calling Integrate
// repeatedly with the same idempotency key never re-merges and reaches
// the complete state in finite steps.
func TestIntegrateIdempotentReachingComplete(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	mergeHEAD := func() string { return f.fr.branchHeads["develop"] }

	// Step 1: verify
	if _, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	}); err != nil {
		t.Fatal(err)
	}
	mergeAfterFirst := mergeHEAD()

	// Step 2: idempotent re-call without ack — state machine should
	// resume from `verified` and stop there.
	r2, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Checkpoint.State != StateVerified {
		t.Fatalf("expected verified (no ack), got %s", r2.Checkpoint.State)
	}
	if mergeHEAD() != mergeAfterFirst {
		t.Fatal("re-call must not re-merge")
	}

	// Step 3: ack
	r3, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  f.readyInspection(),
		Acknowledge: true,
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r3.Checkpoint.State != StateAcknowledged {
		t.Fatalf("expected acknowledged, got %s", r3.Checkpoint.State)
	}

	// Step 4: cleanup completes the chain.
	r4, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  f.readyInspection(),
		Acknowledge: true,
		Cleanup:     true,
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r4.Checkpoint.State != StateComplete {
		t.Fatalf("expected complete, got %s", r4.Checkpoint.State)
	}
	if _, stillThere := f.fr.worktrees[f.root+":"+f.wt]; stillThere {
		t.Fatal("worktree should be removed after complete")
	}
}

// TestIntegrateRefusesSquash covers the BUG-039-05 §4.2 "no auto squash"
// rule at the Integrate entry-point. Inspect always sets
// NonSquashMode=true so we exercise the API guard that refuses a
// caller-supplied squash request.
func TestIntegrateRefusesSquash(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	insp := f.readyInspection()
	insp.NonSquashMode = false

	_, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: insp,
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err == nil {
		t.Fatal("expected ErrSquashForbidden, got nil")
	}
	if !strings.Contains(err.Error(), "squash") {
		t.Fatalf("expected squash-related error, got %v", err)
	}
}

// TestIntegrateRefusesForceDeleteOfDirtyWorktree covers the BUG-039-05
// §4.2 "no force-delete of dirty worktree" rule. The worktree becomes
// dirty between verified and cleanup_pending → preserved.
func TestIntegrateRefusesForceDeleteOfDirtyWorktree(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	// Walk to acknowledged by running the same idempotent chain.
	// First call: merge + verify.
	if _, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	}); err != nil {
		t.Fatal(err)
	}
	// Second call: ack.
	if _, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  f.readyInspection(),
		Acknowledge: true,
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	}); err != nil {
		t.Fatal(err)
	}

	// Now mark the worktree dirty. Cleanup=true should refuse to remove.
	f.fr.markDirty(f.wt)

	res, err := Integrate(context.Background(), IntegrateRequest{
		Inspection:  f.readyInspection(),
		Acknowledge: true,
		Cleanup:     true,
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-test"),
		RuntimeID:     "loop-REQ-039",
	})
	if err == nil {
		t.Fatal("expected error when worktree is dirty at cleanup time, got nil")
	}
	if res.Checkpoint.State != StatePreserved {
		t.Fatalf("expected preserved, got %s", res.Checkpoint.State)
	}
	if !f.fr.worktrees[f.root+":"+f.wt] {
		t.Fatal("worktree must NOT be deleted when dirty")
	}
}

// TestIntegrateCheckpointPathAndContent verifies that the durable
// checkpoint is written to the canonical evidence path and matches the
// in-memory return value.
func TestIntegrateCheckpointPathAndContent(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	cpPath := f.checkpointPath("assignment-test")
	res, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: cpPath,
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if !strings.Contains(string(data), `"state": "verified"`) {
		t.Fatalf("expected state verified in checkpoint, got %s", data)
	}
	if !strings.Contains(string(data), res.Checkpoint.IdempotencyKey) {
		t.Fatalf("expected idempotency key %q in checkpoint, got %s", res.Checkpoint.IdempotencyKey, data)
	}
}

// TestIdempotencyKeyIsStable verifies the canonical key formula.
func TestIdempotencyKeyIsStable(t *testing.T) {
	k1 := IdempotencyKey("assignment-1", "feat-sha", "develop", 1)
	k2 := IdempotencyKey("assignment-1", "feat-sha", "develop", 1)
	if k1 != k2 {
		t.Fatalf("idempotency key must be deterministic; got %q vs %q", k1, k2)
	}
	k3 := IdempotencyKey("assignment-1", "feat-sha", "develop", 2)
	if k1 == k3 {
		t.Fatalf("different baseline generation must produce different key")
	}
	k4 := IdempotencyKey("assignment-2", "feat-sha", "develop", 1)
	if k1 == k4 {
		t.Fatalf("different assignment id must produce different key")
	}
}

func TestIntegrateRecordsDurationMetric(t *testing.T) {
	f := newIntegrationFixture(t)
	defer f.cleanup()

	_, err := Integrate(context.Background(), IntegrateRequest{
		Inspection: f.readyInspection(),
	}, IntegrateConfig{
		Root: f.root, GitRoot: f.root,
		CheckpointDir: f.checkpointPath("assignment-metrics"),
		RuntimeID:     "loop-REQ-039",
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := metrics.NewStore(f.root).Read()
	if err != nil {
		t.Fatal(err)
	}
	success := snap.IntegrationDuration["success"]
	if success.Count != 1 {
		t.Fatalf("success count=%d want 1", success.Count)
	}
	if success.SumMS < 0 {
		t.Fatalf("sum_ms must be non-negative, got %d", success.SumMS)
	}
}
