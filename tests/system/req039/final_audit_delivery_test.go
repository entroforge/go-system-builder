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
		t.Fatalf("initial verification: %+v %v", verified.Checkpoint, err)
	}
	// This is a real later change. The old result is retained as a historical
	// fact; the framework does not certify the new target or re-merge by OID.
	runAuditGit(t, r.root, "reset", "--hard", targetBefore)
	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, r.cfg())
	if err != nil || resumed.Checkpoint.State != integration.StateComplete || resumed.Checkpoint.VerifiedAt != verified.Checkpoint.VerifiedAt {
		t.Fatalf("existing integration fact not retained: %+v %v", resumed.Checkpoint, err)
	}
	if got := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); got != targetBefore {
		t.Fatalf("resumption changed the modified target: %s != %s", got, targetBefore)
	}
	if _, err := os.Stat(filepath.Join(r.root, "internal", "feature.go")); !os.IsNotExist(err) {
		t.Fatalf("old result was misrepresented as a new target merge: %v", err)
	}
}

func TestFinalAuditVerifiedTargetRewriteViaTaskIntegratePreservesWorker(t *testing.T) {
	root, builder := seedCanonicalIntegrableAssignment(t)
	target := canonicalTargetBranch(t, root)
	targetBefore := strings.TrimSpace(runGitIn(t, root, "rev-parse", target))
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil || len(loaded.Assignments) != 1 {
		t.Fatalf("load registered assignment: %v", err)
	}
	assignment := loaded.Assignments[0]
	insp, err := integration.Inspect(context.Background(), integration.InspectRequest{Root: root, Assignment: assignment, TargetBranch: target, BaselineGeneration: 1, RuntimeID: "loop-system-test"}, integration.InspectConfig{})
	if err != nil || !insp.Ready {
		t.Fatalf("inspection: %+v %v", insp, err)
	}
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, integration.IntegrateConfig{Root: root, GitRoot: root, RuntimeID: "loop-system-test"})
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: %+v %v", verified.Checkpoint, err)
	}
	runGitIn(t, root, "reset", "--hard", targetBefore)
	code, stdout, stderr := runTaskIntegrate(t, root, builder.record.AssignmentID)
	cpPath := integration.CheckpointPathForRecord(root, builder.record.Reference())
	cp, found, loadErr := integration.DefaultCheckpointStore().Load(cpPath)
	if code != 0 || loadErr != nil || !found || cp.State != integration.StateComplete || cp.VerifiedAt != verified.Checkpoint.VerifiedAt {
		t.Fatalf("existing facts lost: code=%d checkpoint=%+v load=%v stdout=%s stderr=%s", code, cp, loadErr, stdout, stderr)
	}
	if got := strings.TrimSpace(runGitIn(t, root, "rev-parse", target)); got != targetBefore {
		t.Fatalf("task-integrate unexpectedly changed target: %s", got)
	}
}

func TestFinalAuditVerifiedMissingMergeReceiptPreservesWorker(t *testing.T) {
	r := newAuditGitRepo(t, "final-missing-merge-receipt")
	r.commitFeature(t)
	insp := finalAuditInspection(t, r, "final-missing-merge-receipt")
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: %+v %v", verified.Checkpoint, err)
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
	if _, found := checkpoint["merge_commit"]; found {
		t.Fatal("checkpoint registered a Git object ID")
	}
	before := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, r.cfg())
	if err != nil || resumed.Checkpoint.State != integration.StateComplete {
		t.Fatalf("no-OID result failed to continue: %+v %v", resumed.Checkpoint, err)
	}
	if after := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); after != before {
		t.Fatal("continuation repeated merge")
	}
}

func TestFinalAuditVerifiedSourceRefMovePreservesWorker(t *testing.T) {
	r := newAuditGitRepo(t, "final-source-ref")
	r.commitFeature(t)
	insp := finalAuditInspection(t, r, "final-source-ref")
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err != nil || verified.Checkpoint.State != integration.StateVerified {
		t.Fatalf("initial verification: %+v %v", verified.Checkpoint, err)
	}
	movedWorktree := filepath.Join(t.TempDir(), "moved-source")
	runAuditGit(t, r.root, "worktree", "add", "-b", "final-source-moved", movedWorktree, r.branch)
	if err := os.WriteFile(filepath.Join(movedWorktree, "source-ref-moved.txt"), []byte("source ref moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, movedWorktree, "add", "source-ref-moved.txt")
	runAuditGit(t, movedWorktree, "commit", "-m", "advance source ref")
	movedHead := strings.TrimSpace(runAuditGit(t, movedWorktree, "rev-parse", "HEAD"))
	runAuditGit(t, r.root, "worktree", "remove", movedWorktree)
	runAuditGit(t, r.root, "update-ref", "refs/heads/"+r.branch, movedHead)
	// An independently created private file keeps cleanup pending; changing a
	// ref does not silently amend or invalidate the previously accepted work.
	privatePath := filepath.Join(r.worktree, "private-after-result.txt")
	if err := os.WriteFile(privatePath, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resumed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, r.cfg())
	if !errors.Is(err, integration.ErrCleanupPending) || resumed.Checkpoint.State != integration.StateCleanupPending || resumed.Checkpoint.VerifiedAt != verified.Checkpoint.VerifiedAt {
		t.Fatalf("private work or accepted fact lost: %+v %v", resumed.Checkpoint, err)
	}
	if got, err := os.ReadFile(privatePath); err != nil || string(got) != "keep me\n" {
		t.Fatalf("private file changed: %q %v", got, err)
	}
	if got := runAuditGit(t, r.root, "show", r.branch+":source-ref-moved.txt"); got != "source ref moved\n" {
		t.Fatalf("new source work lost: %q", got)
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
