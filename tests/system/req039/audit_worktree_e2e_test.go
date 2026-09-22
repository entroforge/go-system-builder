package req039_test

// These tests exercise the worktree lifecycle against real git repositories.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	if cp.MergeCommit == "" {
		t.Fatal("complete checkpoint must retain merge_commit")
	}
	mergeAfter := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	if mergeBefore == mergeAfter || mergeAfter != cp.MergeCommit {
		t.Fatalf("authority branch did not land recorded merge: before=%s after=%s cp=%s", mergeBefore, mergeAfter, cp.MergeCommit)
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
	if err := os.WriteFile(filepath.Join(r.worktree, "still-untracked.txt"), []byte("late worker result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	insp := r.inspect(t, assignment)
	if insp.Ready || !strings.Contains(strings.Join(insp.Blockers, " "), integration.ErrDirtyWorktree.Error()) {
		t.Fatalf("untracked worker result must block inspection: ready=%v blockers=%v", insp.Ready, insp.Blockers)
	}
	res, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
	if err != nil || res.Checkpoint.State != integration.StatePreserved {
		t.Fatalf("not-ready integration must preserve child: state=%s err=%v", res.Checkpoint.State, err)
	}
	if _, err := os.Stat(filepath.Join(r.worktree, "still-untracked.txt")); err != nil {
		t.Fatalf("untracked result was lost while preserving worktree: %v", err)
	}
	runAuditGit(t, r.worktree, "add", "still-untracked.txt")
	runAuditGit(t, r.worktree, "commit", "-m", "commit previously untracked result")
	insp = r.inspect(t, assignment)
	if !insp.Ready {
		t.Fatalf("committed retry must become ready: %#v", insp.Blockers)
	}
	cp := auditIntegrateComplete(t, r, insp, r.cfg())
	if cp.State != integration.StateComplete {
		t.Fatalf("retry did not complete: %+v", cp)
	}
	if got, err := os.ReadFile(filepath.Join(r.root, "still-untracked.txt")); err != nil || string(got) != "late worker result\n" {
		t.Fatalf("committed late result missing at authority root: err=%v bytes=%q", err, got)
	}
}

// TestL4AuditDirtyAuthorityRetry exercises the preserved-before-merge path:
// root changes remain untouched, then a retry merges exactly once once the
// authority tree is restored.
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
	first, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, r.cfg())
	if !errors.Is(err, integration.ErrDirtyWorktree) || first.Checkpoint.State != integration.StatePreserved {
		t.Fatalf("dirty authority must preserve before merge: state=%s err=%v", first.Checkpoint.State, err)
	}
	if got := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)); got != insp.TargetHead {
		t.Fatalf("dirty authority advanced target unexpectedly: got=%s want=%s", got, insp.TargetHead)
	}
	if err := os.WriteFile(filepath.Join(r.root, "README.md"), r.baseReadme, 0o644); err != nil {
		t.Fatal(err)
	}
	cp := auditIntegrateComplete(t, r, insp, r.cfg())
	if cp.MergeCommit == "" {
		t.Fatal("retry must record merge commit")
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
	if err == nil || first.Checkpoint.State != integration.StatePreserved || first.Checkpoint.MergeCommit == "" {
		t.Fatalf("post-merge check failure must preserve receipt: state=%s merge=%s err=%v", first.Checkpoint.State, first.Checkpoint.MergeCommit, err)
	}
	if first.Checkpoint.ResumeState != integration.StateMerged {
		t.Fatalf("post-merge check failure must retain merged resume stage, got %q", first.Checkpoint.ResumeState)
	}
	mergeCommit := first.Checkpoint.MergeCommit
	rootHead := strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target))
	second, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true, RetryPreserved: true}, cfg)
	if err != nil || second.Checkpoint.State != integration.StateComplete {
		t.Fatalf("check retry did not complete: state=%s err=%v", second.Checkpoint.State, err)
	}
	if second.Checkpoint.MergeCommit != mergeCommit || strings.TrimSpace(runAuditGit(t, r.root, "rev-parse", r.target)) != rootHead {
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
	mergeCommit := verified.Checkpoint.MergeCommit
	if mergeCommit == "" {
		t.Fatal("initial verify must retain the root merge receipt")
	}
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
	if failed.Checkpoint.MergeCommit != mergeCommit {
		t.Fatalf("cleanup failure lost merge receipt: got %q want %q", failed.Checkpoint.MergeCommit, mergeCommit)
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
	if retryInspection.Ready {
		t.Fatalf("post-merge reinspection should report no new source commits: %#v", retryInspection.Blockers)
	}
	complete, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: retryInspection, Acknowledge: true, Cleanup: true}, cfg)
	if err != nil || complete.Checkpoint.State != integration.StateComplete {
		t.Fatalf("cleanup retry: state=%s checks=%d err=%v", complete.Checkpoint.State, checks, err)
	}
	if complete.Checkpoint.MergeCommit != mergeCommit {
		t.Fatalf("cleanup retry changed merge receipt: got %q want %q", complete.Checkpoint.MergeCommit, mergeCommit)
	}
	if checks != 1 {
		t.Fatalf("cleanup-only retry re-ran required checks: checks=%d", checks)
	}
}

// TestL4AuditCleanupRetryRejectsWorkerHeadChange keeps the cleanup-only
// retry guard honest: a clean but newly committed worker HEAD is still a new
// delivery and must remain preserved for explicit reconciliation.
func TestL4AuditCleanupRetryRejectsWorkerHeadChange(t *testing.T) {
	r := newAuditGitRepo(t, "cleanup-head-change")
	r.commitFeature(t)
	assignment := r.assignment("audit-cleanup-head")
	insp := r.inspect(t, assignment)
	var checks int
	cfg := r.cfg()
	cfg.RequiredChecks = []string{"audit-check"}
	cfg.CheckRunner = func(context.Context, string, string) error { checks++; return nil }
	verified, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, cfg)
	if err != nil || verified.Checkpoint.State != integration.StateVerified || checks != 1 {
		t.Fatalf("initial verify: state=%s checks=%d err=%v", verified.Checkpoint.State, checks, err)
	}
	if err := os.WriteFile(filepath.Join(r.worktree, "new-delivery.txt"), []byte("changed after verify\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAuditGit(t, r.worktree, "add", "new-delivery.txt")
	runAuditGit(t, r.worktree, "commit", "-m", "new delivery after verify")
	failed, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp, Acknowledge: true, Cleanup: true}, cfg)
	if !errors.Is(err, integration.ErrCleanupPending) || failed.Checkpoint.State != integration.StateCleanupPending {
		t.Fatalf("new worker HEAD must preserve cleanup: state=%s checks=%d err=%v", failed.Checkpoint.State, checks, err)
	}
	if failed.Checkpoint.State != integration.StateCleanupPending {
		t.Fatalf("new worker HEAD must retain cleanup_pending resume stage, got %q", failed.Checkpoint.ResumeState)
	}
	if checks != 1 {
		t.Fatalf("new worker HEAD guard unexpectedly reran checks: checks=%d", checks)
	}
	if _, err := os.Stat(r.worktree); err != nil {
		t.Fatalf("new worker HEAD must preserve worktree: %v", err)
	}
}

// TestL4AuditAuthorityLockBlocksMutation ensures an existing integration lock
// fails closed before the root branch or checkpoint is changed.
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
	res, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, r.cfg())
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
	root := freshRoot(t)
	seedIntegrableAssignment(t, root)
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var assignment hookctx.AssignmentContext
	for _, candidate := range loaded.Assignments {
		if candidate.AssignmentID == "assignment-ti" {
			assignment = candidate
			break
		}
	}
	if assignment.AssignmentID == "" {
		t.Fatal("assignment fixture did not load")
	}
	insp, err := integration.Inspect(context.Background(), integration.InspectRequest{
		Root:               root,
		Assignment:         assignment,
		TargetBranch:       "develop",
		BaselineGeneration: loaded.BaselineGeneration,
		RuntimeID:          "loop-system-test",
	}, integration.InspectConfig{})
	if err != nil || !insp.Ready {
		t.Fatalf("pre-crash inspection: ready=%v blockers=%v err=%v", insp.Ready, insp.Blockers, err)
	}
	// Drive only through merge+verify so the linked checkout remains present,
	// then model a lost response before acknowledgement/cleanup is recorded.
	merged, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: insp}, integration.IntegrateConfig{
		Root: root, GitRoot: root, RuntimeID: "loop-system-test",
	})
	if err != nil || merged.Checkpoint.State != integration.StateVerified {
		t.Fatalf("pre-crash merge+verify: state=%s err=%v", merged.Checkpoint.State, err)
	}
	checkpointPath := filepath.Join(root, ".claude", "evidence", "loop-system-test", "g1", "worktree", "assignment-ti", "checkpoint.json")
	raw, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	var cp map[string]any
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	mergeCommit, _ := cp["merge_commit"].(string)
	if mergeCommit == "" {
		t.Fatal("fixture did not record a merge commit")
	}
	// Advance the bound target one ordinary commit, then simulate loss of the
	// merge receipt response while the original worker checkout still exists.
	if err := os.WriteFile(filepath.Join(root, "post-merge-followup.txt"), []byte("target advanced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", "post-merge-followup.txt")
	runGitIn(t, root, "commit", "-m", "target advanced after merge")
	cp["state"] = "ready"
	delete(cp, "merge_commit")
	updated, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpointPath, updated, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errout := runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("receipt recovery command errored: code=%d out=%s err=%s", code, out, errout)
	}
	recoveredRaw, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	var recovered map[string]any
	if err := json.Unmarshal(recoveredRaw, &recovered); err != nil {
		t.Fatal(err)
	}
	if state, _ := recovered["state"].(string); state != integration.StateComplete {
		t.Fatalf("lost merge receipt after target advance was not recovered: state=%v merge=%v out=%s err=%s", recovered["state"], recovered["merge_commit"], out, errout)
	}
	if got, _ := recovered["merge_commit"].(string); got != mergeCommit {
		t.Fatalf("recovered wrong merge commit: got=%q want=%q", got, mergeCommit)
	}
}

// TestL4AuditLostReceiptDoesNotAcceptUnrelatedMerge verifies the negative
// match: a different second parent on the target's first-parent history must
// not be treated as this assignment's lost merge receipt.
func TestL4AuditLostReceiptDoesNotAcceptUnrelatedMerge(t *testing.T) {
	root := freshRoot(t)
	seedIntegrableAssignment(t, root)
	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	var assignment hookctx.AssignmentContext
	for _, candidate := range loaded.Assignments {
		if candidate.AssignmentID == "assignment-ti" {
			assignment = candidate
			break
		}
	}
	if assignment.AssignmentID == "" {
		t.Fatal("assignment fixture did not load")
	}
	sourceHead := strings.TrimSpace(runGitIn(t, assignment.WorktreePath, "rev-parse", "HEAD"))
	targetHead := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))
	checkpointPath := filepath.Join(root, ".claude", "evidence", "loop-system-test", "g1", "worktree", "assignment-ti", "checkpoint.json")
	if _, err := integration.DefaultCheckpointStore().ForceWrite(checkpointPath, integration.Checkpoint{
		AssignmentID:       assignment.AssignmentID,
		TaskID:             assignment.TaskID,
		WorktreePath:       assignment.WorktreePath,
		SourceBranch:       assignment.Branch,
		SourceHead:         sourceHead,
		TargetBranch:       "develop",
		TargetHead:         targetHead,
		MergeBase:          strings.TrimSpace(runGitIn(t, root, "merge-base", targetHead, sourceHead)),
		BaselineGeneration: 1,
		State:              integration.StateReady,
		IdempotencyKey:     integration.IdempotencyKey(assignment.AssignmentID, sourceHead, "develop", 1),
	}); err != nil {
		t.Fatalf("write synthetic ready checkpoint: %v", err)
	}

	// Add a legitimate but unrelated merge whose second parent is not the
	// worker source head recorded above.
	runGitIn(t, root, "checkout", "-b", "audit-unrelated")
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("other work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", "unrelated.txt")
	runGitIn(t, root, "commit", "-m", "unrelated side branch")
	runGitIn(t, root, "checkout", "develop")
	runGitIn(t, root, "merge", "--no-ff", "-m", "unrelated merge", "audit-unrelated")
	unrelatedMerge := strings.TrimSpace(runGitIn(t, root, "rev-parse", "develop"))

	code, out, errout := runTaskIntegrate(t, root, "assignment-ti")
	if code != 0 {
		t.Fatalf("integration after unrelated merge: code=%d out=%s err=%s", code, out, errout)
	}
	recoveredRaw, err := os.ReadFile(checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	var recovered map[string]any
	if err := json.Unmarshal(recoveredRaw, &recovered); err != nil {
		t.Fatal(err)
	}
	if state, _ := recovered["state"].(string); state != integration.StateComplete {
		t.Fatalf("actual worker delivery did not complete: state=%v out=%s err=%s", state, out, errout)
	}
	mergeCommit, _ := recovered["merge_commit"].(string)
	if mergeCommit == unrelatedMerge {
		t.Fatalf("unrelated merge was mistaken for worker receipt: %s", mergeCommit)
	}
	parents := strings.Fields(runGitIn(t, root, "show", "-s", "--format=%P", mergeCommit))
	if len(parents) != 2 || parents[1] != sourceHead {
		t.Fatalf("worker merge parents=%v, want second parent %s", parents, sourceHead)
	}
}
