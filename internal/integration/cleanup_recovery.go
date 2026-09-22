package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrCleanupPending = errors.New("integration verified; worktree cleanup pending")

// A missing directory is not a failed cleanliness check. Only remove its exact
// Git registration; never prune other worktrees or force-delete remaining files.
func cleanupWorktree(ctx context.Context, root, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return removeWorktree(ctx, root, path)
	} else if !os.IsNotExist(err) {
		return err
	}
	out, err := defaultRunner.Run(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	for _, field := range strings.Split(strings.ReplaceAll(out, "\x00", "\n"), "\n") {
		if !strings.HasPrefix(field, "worktree ") {
			continue
		}
		registered := strings.TrimPrefix(field, "worktree ")
		if strings.HasPrefix(registered, "\"") {
			decoded, err := strconv.Unquote(registered)
			if err != nil {
				return fmt.Errorf("decode registered worktree: %w", err)
			}
			registered = decoded
		}
		if filepath.Clean(registered) == filepath.Clean(path) {
			_, err := defaultRunner.Run(ctx, root, "worktree", "remove", path)
			return err
		}
	}
	return nil
}

func InspectionFromCheckpoint(cp Checkpoint) Inspection {
	return Inspection{CompletionReportPath: cp.CompletionReportPath, CompletionReportSHA256: cp.CompletionReportSHA256, Ready: true, AssignmentID: cp.AssignmentID, TaskID: cp.TaskID, WorktreePath: cp.WorktreePath, SourceBranch: cp.SourceBranch, SourceHead: cp.SourceHead, TargetBranch: cp.TargetBranch, TargetHead: cp.TargetHead, MergeBase: cp.MergeBase, BaselineGeneration: cp.BaselineGeneration, NonSquashMode: true}
}

func deferCleanup(store *CheckpointStore, path string, current, next Checkpoint, cause error) (Result, error) {
	next.State = StateCleanupPending
	next.LastErrorCode = "LOOP_INTEGRATION_CLEANUP_PENDING"
	next.FailureReason = cause.Error()
	written, err := store.CompareAndSwap(path, current, next)
	if err != nil {
		return Result{}, fmt.Errorf("persist cleanup pending: %w", err)
	}
	return Result{Checkpoint: written}, fmt.Errorf("%w: %v", ErrCleanupPending, cause)
}

// Recover scope only from a recorded, two-parent non-squash merge still in
// the target history. This does not restore PASS: callers rerun the checks.
func RecoveryBaseForCheckpoint(ctx context.Context, root string, cp Checkpoint) (string, error) {
	if cp.MergeCommit == "" || cp.SourceHead == "" || cp.TargetBranch == "" {
		return "", fmt.Errorf("merged recovery requires recorded merge/source/target")
	}
	target, err := revParse(ctx, root, cp.TargetBranch)
	if err != nil {
		return "", err
	}
	if _, err = defaultRunner.Run(ctx, root, "merge-base", "--is-ancestor", cp.MergeCommit, target); err != nil {
		return "", fmt.Errorf("recorded merge is not in target history: %w", err)
	}
	out, err := defaultRunner.Run(ctx, root, "rev-list", "--parents", "-n", "1", cp.MergeCommit)
	if err != nil {
		return "", err
	}
	parts := strings.Fields(out)
	if len(parts) != 3 || parts[0] != cp.MergeCommit || parts[2] != cp.SourceHead {
		return "", fmt.Errorf("recorded merge parents do not match checkpoint source")
	}
	base, err := mergeBase(ctx, root, parts[1], cp.SourceHead)
	if err != nil {
		return "", err
	}
	if base == cp.SourceHead {
		return "", fmt.Errorf("recorded merge has no original source delta")
	}
	if cp.MergeBase != "" && cp.MergeBase != cp.SourceHead && cp.MergeBase != base {
		return "", fmt.Errorf("recorded scope disagrees with merge ancestry")
	}
	return base, nil
}

// Only the exact two-parent merge at current HEAD proves response loss.
// Anything ambiguous is preserved for inspection; no reflog guessing.
func recoverUnrecordedMerge(ctx context.Context, root string, cp Checkpoint) (string, error) {
	head, err := revParse(ctx, root, "HEAD")
	if err != nil {
		return "", err
	}
	if head == cp.TargetHead {
		return "", nil
	}
	branch, err := defaultRunner.Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(branch) != cp.TargetBranch {
		return "", fmt.Errorf("ready checkpoint recovery requires original target branch")
	}
	parents, err := defaultRunner.Run(ctx, root, "rev-list", "--parents", "-n", "1", head)
	if err != nil {
		return "", err
	}
	parts := strings.Fields(parents)
	if len(parts) != 3 || parts[1] != cp.TargetHead || parts[2] != cp.SourceHead {
		return "", fmt.Errorf("target moved after ready checkpoint; no exact recorded-parent merge to recover")
	}
	return head, nil
}
