package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

// helper: set up the common test scenario where worktree <wt> is on
// branch `feature`, the integration target is `develop`, and a single
// commit `feature-commit` exists with parent `base-commit`. The
// integration branch lives on the repo root.
func setupCleanTree(t *testing.T, fr *fakeRunner) (root, wt string) {
	t.Helper()
	root = "/tmp/repo"
	wt = "/tmp/wt-feature"

	// Three commits: base-commit (shared), feature-commit (only feature).
	fr.addCommit("base-commit")
	fr.addCommit("feature-commit", "base-commit")
	fr.setBranch("feature", "feature-commit")
	fr.setBranch("develop", "base-commit")
	fr.checkout(wt, "feature")
	fr.checkout(root, "develop")
	fr.addWorktree(root, wt)
	return root, wt
}

// hookctx.AssignmentContext is the read-only view the Integrator
// consumes. The InspectRequest path field passes the WorktreePath through
// unchanged.
func assignmentContext(worktree, branch, target string) hookctx.AssignmentContext {
	return hookctx.AssignmentContext{
		AssignmentID: "assignment-test",
		TaskID:       "TASK-TEST",
		WorktreePath: worktree,
		Branch:       branch,
		TargetBranch: target,
	}
}

func TestInspectCleanTreeReportsReady(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()

	root, wt := setupCleanTree(t, fr)
	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}

	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !insp.Ready {
		t.Fatalf("expected ready, blockers=%v", insp.Blockers)
	}
	if insp.SourceHead != "feature-commit" {
		t.Fatalf("source head: got %q want feature-commit", insp.SourceHead)
	}
	if insp.TargetHead != "base-commit" {
		t.Fatalf("target head: got %q want base-commit", insp.TargetHead)
	}
	if insp.MergeBase != "base-commit" {
		t.Fatalf("merge base: got %q want base-commit", insp.MergeBase)
	}
	if !insp.NonSquashMode {
		t.Fatal("NonSquashMode must always be true")
	}
	if len(insp.RequiredChecks) != 0 {
		t.Fatalf("expected no checks, got %d", len(insp.RequiredChecks))
	}
}

func TestInspectDirtyTreeReportsBlocker(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)
	fr.markDirty(wt)

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("expected not ready (dirty), got ready; blockers=%v", insp.Blockers)
	}
	if !containsBlocker(insp.Blockers, ErrDirtyWorktree.Error()) {
		t.Fatalf("expected dirty-worktree blocker, got %v", insp.Blockers)
	}
}

func TestInspectMissingCommitsReportsBlocker(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)
	// Force feature == base by removing the feature-commit; both
	// branches point at base-commit so the count drops to zero.
	fr.setBranch("feature", "base-commit")

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("expected not ready (no commits), got ready")
	}
	if !containsBlocker(insp.Blockers, ErrMissingCommits.Error()) {
		t.Fatalf("expected missing-commits blocker, got %v", insp.Blockers)
	}
}

func TestInspectConflictReportsBlocker(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)
	// merge-tree produces conflict markers — set them in the runner so
	// the next merge-tree call returns them.
	fr.mergeTreeOutput = "changed in both\n<<<<<<< source\n"

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("expected not ready (conflict), got ready")
	}
	if !containsBlocker(insp.Blockers, ErrMergeConflict.Error()) {
		t.Fatalf("expected merge-conflict blocker, got %v", insp.Blockers)
	}
	if len(insp.Conflicts) == 0 {
		t.Fatal("expected conflicts to be populated")
	}
}

func TestInspectLockedArtifactDiffReportsBlocker(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)
	// The fake diff driver emits files that exist on head but not on
	// base. We register the changed file under head only; the base
	// entry intentionally does NOT include it so the diff is non-empty.
	fr.fileContents["feature-commit:internal/hook/adapter.go"] = "tampered"

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
		RequiredChecks:      []string{"locked:internal/hook/adapter.go"},
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("expected not ready (locked diff), got ready; blockers=%v", insp.Blockers)
	}
	if !containsBlocker(insp.Blockers, ErrLockedArtifact.Error()) {
		t.Fatalf("expected locked-artifact blocker, got %v", insp.Blockers)
	}
}

func TestInspectMissingTargetBranchReportsBlocker(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)
	delete(fr.branchHeads, "develop")

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("expected not ready (missing target), got ready")
	}
	if !containsBlocker(insp.Blockers, ErrMissingTarget.Error()) {
		t.Fatalf("expected missing-target blocker, got %v", insp.Blockers)
	}
}

func TestInspectRequiredCheckFailureReportsBlocker(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root, wt := setupCleanTree(t, fr)

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{
		SkipCompletionCheck: true,
		RequiredChecks:      []string{"go test ./..."},
		CheckRunner: func(_ context.Context, _ string, _ string) error {
			return errBoom
		},
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("expected not ready (check fail), got ready")
	}
	if len(insp.RequiredChecks) == 0 || insp.RequiredChecks[0].Status != "fail" {
		t.Fatalf("expected failing check, got %+v", insp.RequiredChecks)
	}
	if !containsBlockerPrefix(insp.Blockers, "required check failed") {
		t.Fatalf("expected required-check blocker, got %v", insp.Blockers)
	}
}

func TestInspectCompletionReportMissingReportsBlocker(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	// Use a real temp directory as the root so completionReportPath's
	// os.Stat returns "not exist" without affecting other tests.
	root := t.TempDir()
	wt := root + "/wt-feature"

	// Wire up the fake runner for the temp paths.
	fr.addCommit("base-commit")
	fr.addCommit("feature-commit", "base-commit")
	fr.setBranch("feature", "feature-commit")
	fr.setBranch("develop", "base-commit")
	fr.checkout(wt, "feature")
	fr.checkout(root, "develop")
	fr.addWorktree(root, wt)

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if insp.Ready {
		t.Fatalf("expected not ready (missing report), got ready")
	}
	if !containsBlockerPrefix(insp.Blockers, "completion report missing") {
		t.Fatalf("expected completion-report blocker, got %v", insp.Blockers)
	}
}

func TestInspectCompletionReportPresentAndValid(t *testing.T) {
	fr, restore := runFakeRunner(t)
	defer restore()
	root := t.TempDir()
	wt := root + "/wt-feature"

	fr.addCommit("base-commit")
	fr.addCommit("feature-commit", "base-commit")
	fr.setBranch("feature", "feature-commit")
	fr.setBranch("develop", "base-commit")
	fr.checkout(wt, "feature")
	fr.checkout(root, "develop")
	fr.addWorktree(root, wt)
	writeCompletionReport(t, root, "assignment-test")

	req := InspectRequest{
		Root:               root,
		Assignment:         assignmentContext(wt, "feature", "develop"),
		TargetBranch:       "develop",
		BaselineGeneration: 1,
	}
	insp, err := Inspect(context.Background(), req, InspectConfig{})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !insp.Ready {
		t.Fatalf("expected ready, blockers=%v", insp.Blockers)
	}
}

func writeCompletionReport(t *testing.T, root, assignmentID string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "evidence", "loop-REQ-039", "g1", "assignments", assignmentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte(`{"message_type":"completion_report","status":"completed"}`)
	if err := os.WriteFile(filepath.Join(dir, "completion.json"), body, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
}

// errBoom is a sentinel error used by the failing-check tests.
var errBoom = stringErr("boom")

type stringErr string

func (s stringErr) Error() string { return string(s) }

func containsBlocker(blockers []string, needle string) bool {
	for _, b := range blockers {
		if b == needle || strings.Contains(b, needle) {
			return true
		}
	}
	return false
}

func containsBlockerPrefix(blockers []string, prefix string) bool {
	for _, b := range blockers {
		if strings.HasPrefix(b, prefix) {
			return true
		}
	}
	return false
}
