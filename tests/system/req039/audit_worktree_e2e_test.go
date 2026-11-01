package req039_test

// These tests exercise the worktree lifecycle against real git repositories.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/integration"
)

type auditGitRepo struct {
	root       string
	worktree   string
	branch     string
	target     string
	baseReadme []byte
}

func newAuditGitRepo(t *testing.T, name string) *auditGitRepo {
	t.Helper()
	root := t.TempDir()
	runAuditGit(t, root, "init", "--initial-branch=dev")
	runAuditGit(t, root, "config", "user.email", "audit@example.test")
	runAuditGit(t, root, "config", "user.name", "L4 audit")
	runAuditGit(t, root, "config", "commit.gpgsign", "false")
	baseReadme := []byte("authority baseline\n")
	if err := os.WriteFile(filepath.Join(root, "README.md"), baseReadme, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "delete-me.txt"), []byte("remove from feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Runtime/checkpoint state is intentionally outside the Git delivery
	// surface in this fixture, just as it is in the system fixtures.
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte(".claude/\n.worktrees/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, root, "add", "README.md", "delete-me.txt")
	runAuditGit(t, root, "commit", "-m", "authority baseline")

	branch := "wt/" + name
	worktree := filepath.Join(root, ".worktrees", name)
	runAuditGit(t, root, "worktree", "add", "-b", branch, worktree, "dev")
	return &auditGitRepo{root: root, worktree: worktree, branch: branch, target: "dev", baseReadme: baseReadme}
}

func (r *auditGitRepo) assignment(name string) hookctx.AssignmentContext {
	return hookctx.AssignmentContext{
		AssignmentID: name,
		TaskID:       "TASK-" + name,
		WorktreePath: r.worktree,
		Branch:       r.branch,
		TargetBranch: r.target,
		WritePaths:   []string{"**"},
	}
}

func (r *auditGitRepo) commitFeature(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(r.worktree, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.worktree, "internal", "feature.go"), []byte("package internal\n\nconst Feature = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.worktree, "payload.bin"), []byte{0x00, 0x11, 0x80, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.worktree, "delete-me.txt")); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, r.worktree, "add", "-A")
	runAuditGit(t, r.worktree, "commit", "-m", "worker delivery")
}

func (r *auditGitRepo) inspect(t *testing.T, assignment hookctx.AssignmentContext) integration.Inspection {
	t.Helper()
	insp, err := integration.Inspect(context.Background(), integration.InspectRequest{
		Root:               r.root,
		Assignment:         assignment,
		TargetBranch:       r.target,
		BaselineGeneration: 1,
		RuntimeID:          "loop-l4-audit",
	}, integration.InspectConfig{SkipCompletionCheck: true})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	return insp
}

func (r *auditGitRepo) cfg() integration.IntegrateConfig {
	return integration.IntegrateConfig{
		Root:      r.root,
		GitRoot:   r.root,
		RuntimeID: "loop-l4-audit",
	}
}

func runAuditGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func auditIntegrateComplete(t *testing.T, r *auditGitRepo, insp integration.Inspection, cfg integration.IntegrateConfig) integration.Checkpoint {
	t.Helper()
	prior, found, loadErr := integration.DefaultCheckpointStore().Load(integration.CheckpointPath(cfg.Root, cfg.RuntimeID, insp.BaselineGeneration, insp.AssignmentID))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	retry := found && (prior.State == integration.StatePreserved || prior.State == integration.StateBlocked)
	first, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, RetryPreserved: retry}, cfg)
	if err != nil || first.Checkpoint.State != integration.StateVerified {
		t.Fatalf("first integration: state=%s err=%v checkpoint=%+v", first.Checkpoint.State, err, first.Checkpoint)
	}
	second, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true}, cfg)
	if err != nil || second.Checkpoint.State != integration.StateAcknowledged {
		t.Fatalf("ack integration: state=%s err=%v checkpoint=%+v", second.Checkpoint.State, err, second.Checkpoint)
	}
	third, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, cfg)
	if err != nil || third.Checkpoint.State != integration.StateComplete {
		t.Fatalf("cleanup integration: state=%s err=%v checkpoint=%+v", third.Checkpoint.State, err, third.Checkpoint)
	}
	if _, err := os.Stat(r.worktree); !os.IsNotExist(err) {
		t.Fatalf("complete integration must remove worktree, stat err=%v", err)
	}
	return third.Checkpoint
}

// TestL4AuditRealGitClosedLoop covers binding's target branch in the
// assignment, a real linked checkout, child commit, non-squash root merge,
// verification, acknowledgement, and cleanup. It also checks that the merge
// retains additions, deletion, and binary bytes at the authority root.
func TestL4AuditRealGitClosedLoop(t *testing.T) {
	r := newAuditGitRepo(t, "closed-loop")
	r.commitFeature(t)
	assignment := r.assignment("audit-closed-loop")
	insp := r.inspect(t, assignment)
	if !insp.Ready {
		t.Fatalf("clean committed worker must be ready: %#v", insp.Blockers)
	}
	mergeBefore := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	cp := auditIntegrateComplete(t, r, insp, r.cfg())
	mergeAfter := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	if cp.State != integration.StateComplete || mergeBefore == mergeAfter {
		t.Fatalf("authority branch did not land actual merge: before=%s after=%s state=%s", mergeBefore, mergeAfter, cp.State)
	}
	parents := strings.Fields(runAuditGit(t, r.root, "show", "-s", "--format=%P", mergeAfter))
	if len(parents) != 2 {
		t.Fatalf("integration must retain a two-parent merge commit, parents=%q", parents)
	}
	if got, err := os.ReadFile(filepath.Join(r.root, "internal", "feature.go")); err != nil || !bytes.Contains(got, []byte("Feature = true")) {
		t.Fatalf("merged source file missing: err=%v bytes=%q", err, got)
	}
	if got, err := os.ReadFile(filepath.Join(r.root, "payload.bin")); err != nil || !bytes.Equal(got, []byte{0x00, 0x11, 0x80, 0xff}) {
		t.Fatalf("merged binary changed: err=%v bytes=%v", err, got)
	}
	if _, err := os.Stat(filepath.Join(r.root, "delete-me.txt")); !os.IsNotExist(err) {
		t.Fatalf("committed deletion was not integrated, stat err=%v", err)
	}
}

// TestL4AuditUntrackedResultIsPreservedAndRetryable proves an untracked child
// result is not silently discarded or force-removed. After the worker commits
// that result, the same assignment can be integrated to completion.
func TestL4AuditUntrackedResultIsPreservedAndRetryable(t *testing.T) {
	r := newAuditGitRepo(t, "untracked-retry")
	r.commitFeature(t)
	assignment := r.assignment("audit-untracked")
	privatePath := filepath.Join(r.worktree, "still-untracked.txt")
	if err := os.WriteFile(privatePath, []byte("late worker result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	insp := r.inspect(t, assignment)
	if !insp.Ready {
		t.Fatalf("untracked private file must not block receiving/integration: %v", insp.Blockers)
	}
	res, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, r.cfg())
	if !errors.Is(err, integration.ErrCleanupPending) || res.Checkpoint.State != integration.StateCleanupPending {
		t.Fatalf("integrated work should retain private content and defer cleanup: state=%s err=%v", res.Checkpoint.State, err)
	}
	if got, err := os.ReadFile(privatePath); err != nil || string(got) != "late worker result\n" {
		t.Fatalf("private file changed: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "internal", "feature.go")); err != nil {
		t.Fatalf("committed delivery was not merged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "still-untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked content was silently promoted into the product: %v", err)
	}
	if err := os.Remove(privatePath); err != nil {
		t.Fatal(err)
	}
	completed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, r.cfg())
	if err != nil || completed.Checkpoint.State != integration.StateComplete {
		t.Fatalf("cleanup retry failed: state=%s err=%v", completed.Checkpoint.State, err)
	}
}

func TestL4AuditDirtyAuthorityRetry(t *testing.T) {
	r := newAuditGitRepo(t, "dirty-root-retry")
	r.commitFeature(t)
	assignment := r.assignment("audit-dirty-root")
	insp := r.inspect(t, assignment)
	if !insp.Ready {
		t.Fatalf("inspection unexpectedly blocked: %#v", insp.Blockers)
	}
	if err := os.WriteFile(filepath.Join(r.root, "README.md"), []byte("unrelated local draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	_, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, r.cfg())
	if !errors.Is(err, integration.ErrDirtyWorktree) {
		t.Fatalf("dirty authority must block actual merge: %v", err)
	}
	if got := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); got != before {
		t.Fatalf("dirty authority advanced target unexpectedly: got=%s want=%s", got, before)
	}
	if err := os.WriteFile(filepath.Join(r.root, "README.md"), r.baseReadme, 0o644); err != nil {
		t.Fatal(err)
	}
	cp := auditIntegrateComplete(t, r, insp, r.cfg())
	if cp.State != integration.StateComplete || strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)) == before {
		t.Fatal("retry did not perform actual merge")
	}
}

// TestL4AuditCheckFailureRetry exercises a failure after the real merge. The
// retry must continue from the durable merge receipt and retain one merge
// commit rather than applying the branch a second time.
func TestL4AuditCheckFailureRetry(t *testing.T) {
	r := newAuditGitRepo(t, "check-retry")
	r.commitFeature(t)
	assignment := r.assignment("audit-check")
	insp := r.inspect(t, assignment)
	var checks int
	cfg := r.cfg()
	cfg.RequiredChecks = []string{"audit-check"}
	cfg.CheckRunner = func(context.Context, string, string) error {
		checks++
		if checks == 1 {
			return errors.New("intentional audit check failure")
		}
		return nil
	}
	first, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, cfg)
	if err == nil || first.Checkpoint.State != integration.StatePreserved {
		t.Fatalf("post-merge check failure must preserve merge boundary: state=%s err=%v", first.Checkpoint.State, err)
	}
	if first.Checkpoint.ResumeState != integration.StateMerged {
		t.Fatalf("post-merge check failure must retain merged resume stage, got %q", first.Checkpoint.ResumeState)
	}
	rootHead := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	second, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true, RetryPreserved: true}, cfg)
	if err != nil || second.Checkpoint.State != integration.StateComplete {
		t.Fatalf("check retry did not complete: state=%s err=%v", second.Checkpoint.State, err)
	}
	if strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)) != rootHead {
		t.Fatal("check retry applied a second root merge")
	}
}

// TestL4AuditCleanupRetryDoesNotReRunChecks verifies that cleanup failure
// preserves the cleanup stage and retries cleanup only.
func TestL4AuditCleanupRetryDoesNotReRunChecks(t *testing.T) {
	r := newAuditGitRepo(t, "cleanup-retry")
	r.commitFeature(t)
	assignment := r.assignment("audit-cleanup")
	insp := r.inspect(t, assignment)
	var checks int
	cfg := r.cfg()
	cfg.RequiredChecks = []string{"audit-check"}
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, cfg)
	if err != nil || verified.Checkpoint.State != integration.StateVerified || checks != 1 {
		t.Fatalf("initial verify: state=%s checks=%d err=%v", verified.Checkpoint.State, checks, err)
	}
	mergeCommit := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	if err := os.WriteFile(filepath.Join(r.worktree, "cleanup-pending.txt"), []byte("must survive failed cleanup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, integration.ErrCleanupPending) || failed.Checkpoint.State != integration.StateCleanupPending {
		t.Fatalf("dirty cleanup must preserve: state=%s checks=%d err=%v", failed.Checkpoint.State, checks, err)
	}
	if failed.Checkpoint.State != integration.StateCleanupPending {
		t.Fatalf("cleanup failure must retain resume stage cleanup_pending, got %q", failed.Checkpoint.ResumeState)
	}
	if got := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); got != mergeCommit {
		t.Fatalf("cleanup failure changed the target: got %q want %q", got, mergeCommit)
	}
	if checks != 1 {
		t.Fatalf("cleanup failure unexpectedly ran checks before retry: checks=%d", checks)
	}
	if err := os.Remove(filepath.Join(r.worktree, "cleanup-pending.txt")); err != nil {
		t.Fatal(err)
	}
	// Re-inspect after the failed cleanup, as the real controller does on a
	// retry. The post-merge source has no new commits, so this deliberately
	// exercises Ready=false recovery from the durable cleanup_pending receipt.
	retryInspection := r.inspect(t, assignment)
	complete, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: retryInspection, Acknowledge: true, Cleanup: true}, cfg)
	if err != nil || complete.Checkpoint.State != integration.StateComplete {
		t.Fatalf("cleanup retry: state=%s checks=%d err=%v", complete.Checkpoint.State, checks, err)
	}
	if got := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); got != mergeCommit {
		t.Fatalf("cleanup retry changed the target: got %q want %q", got, mergeCommit)
	}
	if checks != 1 {
		t.Fatalf("cleanup-only retry re-ran required checks: checks=%d", checks)
	}
}

// TestL4AuditCleanupRetryRejectsWorkerHeadChange keeps the cleanup-only
// retry guard honest: a clean but newly committed worker HEAD is still a new
// delivery and must remain preserved for explicit reconciliation.
func TestL4AuditCleanupRetainsNewUncommittedWorkerContent(t *testing.T) {
	r := newAuditGitRepo(t, "cleanup-new-content")
	r.commitFeature(t)
	insp := r.inspect(t, r.assignment("audit-cleanup-new"))
	checks := 0
	cfg := r.cfg()
	cfg.RequiredChecks = []string{"audit-check"}
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, cfg)
	if err != nil || verified.Checkpoint.State != integration.StateVerified || checks != 1 {
		t.Fatalf("initial verify: state=%s checks=%d err=%v", verified.Checkpoint.State, checks, err)
	}
	path := filepath.Join(r.worktree, "new-delivery.txt")
	if err := os.WriteFile(path, []byte("new private work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, integration.ErrCleanupPending) || failed.Checkpoint.State != integration.StateCleanupPending || checks != 1 {
		t.Fatalf("new worker content not preserved: state=%s checks=%d err=%v", failed.Checkpoint.State, checks, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "new private work\n" {
		t.Fatalf("new worker content changed: %q %v", got, err)
	}
}

func TestL4AuditAuthorityLockBlocksMutation(t *testing.T) {
	r := newAuditGitRepo(t, "authority-lock")
	r.commitFeature(t)
	assignment := r.assignment("audit-lock")
	insp := r.inspect(t, assignment)
	_, release, lockErr := integration.LockWorkspace(context.Background(), r.root)
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	defer release()
	before := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	lockCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := integration.Integrate(lockCtx, integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err == nil {
		t.Fatal("authority lock must reject concurrent integration")
	}
	if res.Checkpoint.State != "" {
		t.Fatalf("lock rejection must not return a progressed checkpoint: %+v", res.Checkpoint)
	}
	if got := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); got != before {
		t.Fatalf("authority lock rejection advanced target: before=%s after=%s", before, got)
	}
	if _, err := os.Stat(r.worktree); err != nil {
		t.Fatalf("authority lock rejection must preserve worktree: %v", err)
	}
	release()
	auditIntegrateComplete(t, r, insp, r.cfg())
}

// TestL4AuditLostMergeReceiptAfterTargetAdvance verifies recovery by walking
// the target branch's first-parent merge history after the target advances.
func TestL4AuditLostMergeReceiptAfterTargetAdvance(t *testing.T) {
	r := newAuditGitRepo(t, "lost-merge-receipt")
	r.commitFeature(t)
	insp := r.inspect(t, r.assignment("audit-lost-receipt"))
	checkpointPath := integration.CheckpointPath(r.root, "loop-l4-audit", 1, insp.AssignmentID)
	cp := integration.Checkpoint{AssignmentID: insp.AssignmentID, WorktreePath: r.worktree, SourceBranch: r.branch, TargetBranch: r.target, BaselineGeneration: 1, State: integration.StateReady}
	if _, err := integration.DefaultCheckpointStore().ForceWrite(checkpointPath, cp); err != nil {
		t.Fatal(err)
	}
	// The merge occurred but its completion was not persisted before interruption.
	runAuditGit(t, r.root, "merge", "--no-ff", "-m", "worker merge", r.branch)
	if err := os.WriteFile(filepath.Join(r.root, "post-merge-followup.txt"), []byte("target advanced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, r.root, "add", "post-merge-followup.txt")
	runAuditGit(t, r.root, "commit", "-m", "target advanced")
	before := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	got, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, RetryPreserved: true}, r.cfg())
	if err == nil || !strings.Contains(err.Error(), "unknown") || got.Checkpoint.State != integration.StateReady {
		t.Fatalf("unknown merge was guessed: state=%s err=%v", got.Checkpoint.State, err)
	}
	if after := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); after != before {
		t.Fatal("unknown recovery changed target")
	}
	if _, err := os.Stat(r.worktree); err != nil {
		t.Fatalf("unknown recovery removed worker: %v", err)
	}
}

func TestL4AuditLostReceiptDoesNotAcceptUnrelatedMerge(t *testing.T) {
	r := newAuditGitRepo(t, "unrelated-merge")
	r.commitFeature(t)
	insp := r.inspect(t, r.assignment("audit-unrelated"))
	checkpointPath := integration.CheckpointPath(r.root, "loop-l4-audit", 1, insp.AssignmentID)
	cp := integration.Checkpoint{AssignmentID: insp.AssignmentID, WorktreePath: r.worktree, SourceBranch: r.branch, TargetBranch: r.target, BaselineGeneration: 1, State: integration.StateReady}
	if _, err := integration.DefaultCheckpointStore().ForceWrite(checkpointPath, cp); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, r.root, "checkout", "-b", "unrelated")
	if err := os.WriteFile(filepath.Join(r.root, "unrelated.txt"), []byte("other work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, r.root, "add", "unrelated.txt")
	runAuditGit(t, r.root, "commit", "-m", "unrelated side branch")
	runAuditGit(t, r.root, "checkout", r.target)
	runAuditGit(t, r.root, "merge", "--no-ff", "-m", "unrelated merge", "unrelated")
	before := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	got, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err == nil || !strings.Contains(err.Error(), "unknown") || got.Checkpoint.State != integration.StateReady {
		t.Fatalf("unrelated merge was treated as worker completion: %+v %v", got.Checkpoint, err)
	}
	if after := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); after != before {
		t.Fatal("unknown recovery changed target")
	}
	if _, err := os.Stat(r.worktree); err != nil {
		t.Fatalf("unknown recovery removed worker: %v", err)
	}
}
