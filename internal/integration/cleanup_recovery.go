package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var ErrCleanupPending = errors.New("integration verified; worktree cleanup pending")

// A missing directory is not a failed cleanliness check. Only remove its exact
// Git registration; never prune other worktrees or force-delete remaining files.
// The checkpoint is the existing authorization: this helper is called only
// after verified Main checks, ACK, and cleanup_pending have been persisted.
func cleanupWorktree(ctx context.Context, root, path string, checkpoint Checkpoint, areaSpec WorkerPrivateAreaSpec, logEvidence ...map[string]string) error {
	var logs map[string]string
	var private, ignored map[string]string
	if len(logEvidence) > 0 {
		logs = logEvidence[0]
	}
	if len(logEvidence) > 1 {
		private = logEvidence[1]
	}
	if len(logEvidence) > 2 {
		ignored = logEvidence[2]
	}
	if checkpoint.State != StateCleanupPending || checkpoint.VerifiedAt == "" || checkpoint.MergeCommit == "" || checkpoint.SourceHead == "" || checkpoint.TargetBranch == "" || filepath.Clean(checkpoint.WorktreePath) != filepath.Clean(path) {
		return fmt.Errorf("Worker cleanup requires its verified cleanup_pending checkpoint")
	}
	if _, err := RecoveryBaseForCheckpoint(ctx, root, checkpoint); err != nil {
		return fmt.Errorf("verified cleanup checkpoint no longer matches the target merge: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Worker cleanup source is not its original regular directory; preserve it")
		}
		head, err := revParse(ctx, path, "HEAD")
		if err != nil || head != checkpoint.SourceHead {
			return fmt.Errorf("Worker HEAD differs from verified checkpoint source; preserve it")
		}
		privateArea, err := ObserveWorkerPrivateArea(ctx, path, areaSpec)
		if err != nil {
			return fmt.Errorf("observe original Worker private area before cleanup: %w", err)
		}
		projection, err := workerBootstrapProjectionAt(ctx, path, true)
		if err != nil {
			return fmt.Errorf("revalidate Main-pinned Worker bootstrap before cleanup: %w", err)
		}
		native, err := nativeDeliveryProjection(root, path, true)
		if err != nil {
			return err
		}
		if projection == nil {
			projection = map[string]string{}
		}
		for rel, digest := range native {
			projection[rel] = digest
		}
		for rel, digest := range private {
			projection[rel] = digest
		}
		// Existing exact receipt/accepted projections also prove ignored system
		// assets. This is a file map, never an installation-directory exemption.
		knownIgnored := map[string]string{}
		for _, evidence := range []map[string]string{ignored, projection, logs} {
			for rel, digest := range evidence {
				if prior := knownIgnored[rel]; prior != "" && prior != digest {
					return fmt.Errorf("conflicting cleanup byte identities for %s", rel)
				}
				knownIgnored[rel] = digest
			}
		}
		// Git's ordinary cleanliness check omits ignored files, but ordinary
		// worktree removal deletes them. Inventory before removing any original
		// professional file, captured log or installation projection.
		if err := guardIgnoredCleanupWithPrivateArea(ctx, path, knownIgnored, privateArea); err != nil {
			return err
		}
		// A previous attempt may have restored all installation files before
		// ordinary Git removal failed. The verified source coordinate above
		// and a clean checkout are enough to retry that removal.
		clean, err := worktreeCleanWithProjection(ctx, path, nil)
		if err != nil {
			return err
		}
		if clean && privateArea == nil {
			return removeGuardedWorktree(ctx, root, path, knownIgnored)
		}

		clean, err = worktreeCleanWithPrivateArea(ctx, path, projection, privateArea, logs)
		if err != nil {
			return err
		}
		if !clean {
			return fmt.Errorf("Builder checkout %q is dirty; inspect with git -C %q status --short: %w", path, path, ErrDirtyWorktree)
		}
		if len(projection) > 0 || len(logs) > 0 || privateArea != nil {
			// Recheck the source coordinate immediately before restoring only the
			// receipt-listed installation overlay.
			head, err = revParse(ctx, path, "HEAD")
			if err != nil || head != checkpoint.SourceHead {
				return fmt.Errorf("Worker HEAD changed before bootstrap cleanup; preserve it")
			}
			if err := removeCapturedLogOutputs(ctx, path, logs); err != nil {
				return err
			}
			if err := restorePinnedBootstrap(ctx, path, projection); err != nil {
				return err
			}
			if err := removeWorkerPrivateAreaScratch(ctx, path, privateArea, knownIgnored); err != nil {
				return err
			}
			clean, err = worktreeCleanWithProjection(ctx, path, nil)
			if err != nil {
				return err
			}
			if !clean {
				return fmt.Errorf("Builder checkout %q is still dirty after bootstrap restore; inspect with git -C %q status --short: %w", path, path, ErrDirtyWorktree)
			}
		}
		return removeGuardedWorktree(ctx, root, path, knownIgnored)
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

// Called only after the original verified cleanup_pending checkpoint and a
// complete clean-tree check. Never remove tracked, changed or neighboring files.
func removeCapturedLogOutputs(ctx context.Context, root string, logs map[string]string) error {
	for rel, digest := range logs {
		if err := ctx.Err(); err != nil {
			return err
		}
		status, err := defaultRunner.Run(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", rel)
		if err != nil {
			return err
		}
		if status == "" {
			continue
		}
		if status != "?? "+rel+"\x00" || !fileMatchesDigest(root, rel, digest) {
			return fmt.Errorf("preserve captured log changed before cleanup: %s", rel)
		}
		parent, name, err := workpacket.OpenParent(root, rel, false)
		if err != nil {
			return err
		}
		err = parent.Remove(name)
		_ = parent.Close()
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// restorePinnedBootstrap removes only unchanged receipt-backed untracked
// assets and restores only unchanged receipt-backed tracked overlays. The Git
// index is never changed and cleanup remains non-forced.
func restorePinnedBootstrap(ctx context.Context, root string, projection map[string]string) error {
	paths := make([]string, 0, len(projection))
	for path := range projection {
		if path != ".claude/loop-bootstrap.json" && path != ".claude/loop-workspace.json" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	// Keep the original pinned receipt and pointer until all assets are
	// restored, so interruption can resume without another persistent object.
	paths = append(paths, ".claude/loop-bootstrap.json", ".claude/loop-workspace.json")
	for _, rel := range paths {
		expected := projection[rel]
		if expected == "" {
			continue
		}
		status, err := defaultRunner.Run(ctx, root, "status", "--porcelain", "--untracked-files=all", "--", rel)
		if err != nil {
			return err
		}
		if status == "" {
			continue
		}
		if !fileMatchesDigest(root, rel, expected) {
			return fmt.Errorf("preserve Worker bootstrap asset changed after receipt validation: %s", rel)
		}
		tracked, err := defaultRunner.Run(ctx, root, "ls-files", "-z", "--", rel)
		if err != nil {
			return fmt.Errorf("inspect receipt-backed Worker path %s: %w", rel, err)
		}
		if tracked != "" {
			for _, recorded := range strings.Split(tracked, "\x00") {
				if recorded != "" && filepath.ToSlash(recorded) != rel {
					return fmt.Errorf("Git returned an unexpected tracked Worker path for %s", rel)
				}
			}
			if _, err := defaultRunner.Run(ctx, root, "restore", "--worktree", "--", rel); err != nil {
				return fmt.Errorf("restore receipt-backed tracked Worker asset %s: %w", rel, err)
			}
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("preserve non-regular receipt-backed Worker asset %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expected {
			return fmt.Errorf("preserve changed receipt-backed Worker asset %s", rel)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func InspectionFromCheckpoint(cp Checkpoint) Inspection {
	out := Inspection{
		CompletionReportPath:   cp.CompletionReportPath,
		CompletionReportSHA256: cp.CompletionReportSHA256,
		ChangedPaths:           append([]string{}, cp.ChangedPaths...),
		ChangedPathsSHA256:     cp.ChangedPathsSHA256,
		Ready:                  true,
		AssignmentID:           cp.AssignmentID,
		TaskID:                 cp.TaskID,
		WorktreePath:           cp.WorktreePath,
		SourceBranch:           cp.SourceBranch,
		SourceHead:             cp.SourceHead,
		TargetBranch:           cp.TargetBranch,
		TargetHead:             cp.TargetHead,
		MergeBase:              cp.MergeBase,
		BaselineGeneration:     cp.BaselineGeneration,
		NonSquashMode:          true,
	}
	if cp.CanonicalMainFacts != nil {
		binding := cp.CanonicalMainFacts.Binding
		out.CanonicalResult = &binding
		out.ChangedPaths = append([]string{}, cp.CanonicalMainFacts.ChangedPaths...)
		out.ChangedPathsSHA256 = cp.CanonicalMainFacts.ChangedPathsSHA256
	}
	return out
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

// Recover scope from the recorded target/source facts. Changed deliveries
// retain their two-parent merge; no-change deliveries retain the existing Main
// HEAD. This does not restore PASS: callers rerun the checks.
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
	// A no-change professional delivery records the existing target HEAD,
	// not a two-parent merge. Its original source/base range must be empty;
	// canonical revision recovery continues through its existing merge path.
	if cp.CanonicalMainFacts == nil && cp.SourceHead == cp.MergeBase && cp.MergeCommit == cp.TargetHead && cp.ChangedPathsSHA256 != "" && len(cp.ChangedPaths) == 0 {
		paths, sha, err := canonicalGitChangedPaths(ctx, root, cp.MergeBase, cp.SourceHead)
		if err != nil {
			return "", err
		}
		if len(paths) == 0 && sha == cp.ChangedPathsSHA256 {
			return cp.MergeBase, nil
		}
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

const readyMergeHistoryLimit = 256

// RecoverUnrecordedMerge observes the original ready intent in bounded
// first-parent history. It writes nothing and grants no check/ACK authority.
func RecoverUnrecordedMerge(ctx context.Context, root string, cp Checkpoint) (string, error) {
	if cp.State != StateReady || cp.MergeCommit != "" || cp.TargetHead == "" || cp.SourceHead == "" || cp.SourceBranch == "" || cp.TargetBranch == "" {
		return "", fmt.Errorf("merge response recovery requires the original ready checkpoint and exact source/target coordinates")
	}
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
	source, err := revParse(ctx, root, cp.SourceBranch)
	if err != nil || source != cp.SourceHead {
		return "", fmt.Errorf("Main must reconcile source %s with ready source %s before merge recovery; preserve the Worker", cp.SourceBranch, cp.SourceHead)
	}
	parents, err := defaultRunner.Run(ctx, root, "rev-list", "--first-parent", "--parents", "--max-count="+strconv.Itoa(readyMergeHistoryLimit+1), head, "^"+cp.TargetHead)
	if err != nil {
		return "", err
	}
	rows := strings.Split(strings.TrimSpace(parents), "\n")
	if len(rows) > readyMergeHistoryLimit {
		return "", fmt.Errorf("merge recovery exceeds %d first-parent commits; Main must inspect ready target %s/source %s and target history, preserve the checkpoint and Worker, and avoid another merge", readyMergeHistoryLimit, cp.TargetHead, cp.SourceHead)
	}
	merge := ""
	for _, row := range rows {
		parts := strings.Fields(row)
		if len(parts) != 3 || parts[1] != cp.TargetHead || parts[2] != cp.SourceHead {
			continue
		}
		if merge != "" {
			return "", fmt.Errorf("multiple exact ready-parent merges observed; Main must preserve and inspect the original checkpoint")
		}
		merge = parts[0]
	}
	if merge == "" {
		return "", fmt.Errorf("no exact two-parent merge for ready target %s/source %s in %d first-parent commits; Main must inspect original target history and preserve the Worker without merging again", cp.TargetHead, cp.SourceHead, readyMergeHistoryLimit)
	}
	return merge, nil
}
