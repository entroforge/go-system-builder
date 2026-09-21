package req039_test

// These probes cover delivery mutations that occur after a successful
// inspection/verification. They are intentionally opt-in while the audit is
// documenting the current behavior; each assertion states the fail-closed
// contract expected from a production fix.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
)

func TestFinalAuditVerifiedTargetRewritePreservesWorker(t *testing.T) {
	r := newAuditGitRepo(t, "final-target-rewrite")
	targetBefore := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	r.commitFeature(t)
	insp := finalAuditInspection(t, r, "final-target-rewrite")
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: state=%s err=%v checkpoint=%+v", verified.Checkpoint.State, err, verified.Checkpoint)
	}

	// The target ref is rewritten after verification, before acknowledgement /
	// cleanup. No new worker commit or Result is involved.
	runAuditGit(t, r.root, "reset", "--hard", targetBefore)
	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
		Cleanup:     true,
	}, r.cfg())
	if err == nil && resumed.Checkpoint.State == integration.StateComplete {
		t.Logf("target rewrite evidence: authority=%s worker=%s checkpoint=%+v", strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)), r.worktree, resumed.Checkpoint)
		t.Fatalf("cleanup must reject a verified checkpoint whose recorded merge is no longer reachable from target")
	}
	if _, statErr := os.Stat(r.worktree); os.IsNotExist(statErr) {
		t.Fatalf("target rewrite must preserve the worker for explicit reconciliation: err=%v", statErr)
	}
}

func TestFinalAuditVerifiedTargetRewriteViaTaskIntegratePreservesWorker(t *testing.T) {
	root := freshRoot(t)
	workerPath := seedIntegrableAssignment(t, root)
	targetBefore := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil || len(loaded.Assignments) != 1 {
		t.Fatalf("load registered assignment: count=%d err=%v", len(loaded.Assignments), err)
	}
	assignment := loaded.Assignments[0]
	insp, err := integration.Inspect(context.Background(), integration.InspectRequest{
		Root:               root,
		Assignment:         assignment,
		TargetBranch:       "develop",
		BaselineGeneration: 1,
		RuntimeID:          "loop-system-test",
	}, integration.InspectConfig{})
	if err != nil || !insp.Ready {
		t.Fatalf("CLI fixture inspection: ready=%v blockers=%v err=%v", insp.Ready, insp.Blockers, err)
	}
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, integration.IntegrateConfig{
		Root: root, GitRoot: root, RuntimeID: "loop-system-test",
	})
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: state=%s err=%v checkpoint=%+v", verified.Checkpoint.State, err, verified.Checkpoint)
	}

	// Route recovery through the shipped task-integrate CLI after the target
	// ref was rewritten. The CLI must preserve the worker and report a blocked
	// reconciliation instead of accepting the stale verified receipt.
	runGitIn(t, root, "reset", "--hard", targetBefore)
	code, stdout, stderr := runTaskIntegrate(t, root, "assignment-ti")
	cpPath := integration.CheckpointPath(root, "loop-system-test", 1, "assignment-ti")
	cp, found, loadErr := integration.DefaultCheckpointStore().Load(cpPath)
	if loadErr != nil || !found {
		t.Fatalf("load CLI recovery checkpoint: found=%v err=%v stdout=%s stderr=%s", found, loadErr, stdout, stderr)
	}
	if code == 0 && cp.State == integration.StateComplete {
		t.Logf("CLI target rewrite evidence: code=%d authority=%s worker=%s checkpoint=%+v stdout=%s stderr=%s", code, strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop")), workerPath, cp, stdout, stderr)
		t.Fatalf("runtime task-integrate must reject a verified checkpoint whose recorded merge is no longer reachable from target")
	}
	if _, statErr := os.Stat(workerPath); os.IsNotExist(statErr) {
		t.Fatalf("CLI target rewrite must preserve worker for explicit reconciliation: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestFinalAuditVerifiedMissingMergeReceiptPreservesWorker(t *testing.T) {
	r := newAuditGitRepo(t, "final-missing-merge-receipt")
	r.commitFeature(t)
	insp := finalAuditInspection(t, r, "final-missing-merge-receipt")
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: state=%s err=%v checkpoint=%+v", verified.Checkpoint.State, err, verified.Checkpoint)
	}
	checkpointPath := integration.CheckpointPath(r.root, "loop-l4-audit", 1, "final-missing-merge-receipt")
	raw, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint map[string]any
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	delete(checkpoint, "merge_commit")
	raw, err = json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpointPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
		Cleanup:     true,
	}, r.cfg())
	if err == nil || resumed.Checkpoint.State != integration.StatePreserved {
		t.Fatalf("verified checkpoint without merge receipt was reused: state=%s err=%v checkpoint=%+v", resumed.Checkpoint.State, err, resumed.Checkpoint)
	}
	if _, statErr := os.Stat(r.worktree); statErr != nil {
		t.Fatalf("missing merge receipt must preserve worker: %v", statErr)
	}
}

func TestFinalAuditVerifiedSourceRefMovePreservesWorker(t *testing.T) {
	r := newAuditGitRepo(t, "final-source-ref")
	r.commitFeature(t)
	insp := finalAuditInspection(t, r, "final-source-ref")
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: state=%s err=%v checkpoint=%+v", verified.Checkpoint.State, err, verified.Checkpoint)
	}

	// Move the source ref to a new commit while the linked worker checkout is
	// still at the source head that was actually verified. This models a force
	// update or an external producer advancing the source ref after verification.
	movedWorktree := filepath.Join(t.TempDir(), "moved-source")
	runAuditGit(t, r.root, "worktree", "add", "-b", "final-source-moved", movedWorktree, verified.Checkpoint.SourceHead)
	if err := os.WriteFile(filepath.Join(movedWorktree, "source-ref-moved.txt"), []byte("source ref moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, movedWorktree, "add", "source-ref-moved.txt")
	runAuditGit(t, movedWorktree, "commit", "-m", "advance source ref after verification")
	movedHead := strings.TrimSpace(runAuditGit(t, movedWorktree, "rev-parse", "HEAD"))
	runAuditGit(t, r.root, "worktree", "remove", movedWorktree)
	runAuditGit(t, r.root, "update-ref", "refs/heads/"+r.branch, movedHead)

	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
		Cleanup:     true,
	}, r.cfg())
	if err == nil && resumed.Checkpoint.State == integration.StateComplete {
		t.Logf("source ref move evidence: source=%s recorded=%s checkpoint=%+v", movedHead, verified.Checkpoint.SourceHead, resumed.Checkpoint)
		t.Fatalf("cleanup must reject a verified checkpoint after the source ref moves independently of the verified worker checkout")
	}
	t.Logf("source ref move preserved delivery: state=%s err=%v", resumed.Checkpoint.State, err)
	if _, statErr := os.Stat(r.worktree); os.IsNotExist(statErr) {
		t.Fatalf("source ref move must preserve the worker for explicit reconciliation: err=%v", statErr)
	}
}

func TestFinalAuditCorruptCheckpointFailsClosed(t *testing.T) {
	r := newAuditGitRepo(t, "final-corrupt-checkpoint")
	r.commitFeature(t)
	insp := finalAuditInspection(t, r, "final-corrupt-checkpoint")
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: state=%s err=%v checkpoint=%+v", verified.Checkpoint.State, err, verified.Checkpoint)
	}
	checkpointPath := integration.CheckpointPath(r.root, "loop-l4-audit", 1, "final-corrupt-checkpoint")
	if err := os.WriteFile(checkpointPath, []byte("{corrupt checkpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{
		Inspection:  insp,
		Acknowledge: true,
		Cleanup:     true,
	}, r.cfg())
	if err == nil {
		t.Fatalf("corrupt checkpoint must fail closed: checkpoint=%+v", resumed.Checkpoint)
	}
	if got := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); got != before {
		t.Fatalf("corrupt checkpoint changed authority target: before=%s after=%s", before, got)
	}
	if _, statErr := os.Stat(r.worktree); statErr != nil {
		t.Fatalf("corrupt checkpoint must preserve worker: %v", statErr)
	}
}

func TestFinalAuditConcurrentIntegrateSerializesAndReuses(t *testing.T) {
	r := newAuditGitRepo(t, "final-concurrent-integrate")
	r.commitFeature(t)
	insp := finalAuditInspection(t, r, "final-concurrent-integrate")
	cfg := r.cfg()
	type outcome struct {
		result integration.Result
		err    error
	}
	outcomes := make([]outcome, 2)
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i].result, outcomes[i].err = integration.Integrate(context.Background(), integration.IntegrateRequest{
				Inspection:  insp,
				Acknowledge: true,
				Cleanup:     true,
			}, cfg)
		}(i)
	}
	wg.Wait()
	for i, outcome := range outcomes {
		if errors.Is(outcome.err, integration.ErrIntegrationBusy) {
			outcome.result, outcome.err = integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, cfg)
		}
		if outcome.err != nil || outcome.result.Checkpoint.State != integration.StateComplete {
			t.Fatalf("concurrent integration %d did not serialize to complete: state=%s err=%v", i, outcome.result.Checkpoint.State, outcome.err)
		}
	}
	if _, statErr := os.Stat(r.worktree); !os.IsNotExist(statErr) {
		t.Fatalf("serialized completion must clean worker exactly once: %v", statErr)
	}
}

func finalAuditInspection(t *testing.T, r *auditGitRepo, assignmentID string) integration.Inspection {
	t.Helper()
	writeCompletionReport(t, r.root, "loop-l4-audit", assignmentID)
	insp, err := integration.Inspect(context.Background(), integration.InspectRequest{
		Root:               r.root,
		Assignment:         r.assignment(assignmentID),
		TargetBranch:       r.target,
		BaselineGeneration: 1,
		RuntimeID:          "loop-l4-audit",
	}, integration.InspectConfig{})
	if err != nil || !insp.Ready {
		t.Fatalf("bound inspection %s: ready=%v blockers=%v err=%v", assignmentID, insp.Ready, insp.Blockers, err)
	}
	return insp
}
