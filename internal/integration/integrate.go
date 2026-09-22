package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

// CheckpointStoreFactory returns the CheckpointStore Integrate should use.
// The default (nil) means use the package-level default. Tests override it
// to inject a deterministic clock.
type CheckpointStoreFactory func() *CheckpointStore

// IntegrateConfig tunes Integrate's behaviour.
type IntegrateConfig struct {
	// CheckpointStore selects the persistence backend. nil → default.
	CheckpointStore *CheckpointStore
	// CheckpointDir, when set, overrides the canonical evidence
	// directory layout. Tests use it to point at a temp dir.
	CheckpointDir string
	// Root, when set, is the repository root used to compute the
	// canonical checkpoint path. Required.
	Root string
	// RuntimeID, when set, is the runtime identifier used to compute
	// the canonical checkpoint path. Defaults to "loop-REQ-039".
	RuntimeID string
	// CheckRunner runs integration checks; nil with required checks fails closed.
	CheckRunner RequiredCheckRunner
	// RequiredChecks lists commands to run after merge for the
	// verified transition.
	RequiredChecks []string
	// GitRoot is the repository root used for git operations. Defaults
	// to cfg.Root when unset.
	GitRoot string
}

// Integrate drives the checkpoint state machine for one assignment:
//
//	pending → ready → merged → verified → acknowledged → cleanup_pending → complete
//	             \_________________________________________________→ blocked
//	             \_________________________________________________→ preserved
//
// The function is idempotent at the (assignment_id + source_head +
// target_branch + baseline_generation) key. Re-calling Integrate with the
// same idempotent key after the merge succeeded but before
// acknowledgement (CT-039-17) resumes the state machine from `merged` and
// does NOT re-merge.
//
// The caller controls the merge step by setting IntegrateRequest.Cleanup.
// Acknowledge is automatic once verified; a separate flag is provided so
// BUG-06 can call Integrate twice — once for the merge/verify phase and
// again after writing the completion_ack.
func Integrate(ctx context.Context, req IntegrateRequest, cfg IntegrateConfig) (result Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	start := time.Now()
	defer func() {
		if cfg.Root == "" {
			return
		}
		_ = metrics.ObserveIntegrationDuration(cfg.Root, integrationDurationStatus(result, err), time.Since(start).Milliseconds())
	}()
	if cfg.Root == "" {
		return Result{}, errors.New("config.Root is required")
	}
	gitRoot := cfg.GitRoot
	if gitRoot == "" {
		gitRoot = cfg.Root
	}
	// Checkpoints may be stored separately, but serialization belongs to
	// the checkout whose Git state this operation actually mutates.
	ctx, release, err := LockWorkspace(ctx, gitRoot)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if req.Inspection.WorktreePath == "" {
		return Result{}, errors.New("inspection.worktree_path is required")
	}
	if req.Inspection.SourceBranch == "" {
		return Result{}, errors.New("inspection.source_branch is required")
	}
	if req.Inspection.TargetBranch == "" {
		return Result{}, errors.New("inspection.target_branch is required")
	}
	if !req.Inspection.NonSquashMode {
		return Result{}, ErrSquashForbidden
	}

	store := cfg.CheckpointStore
	if store == nil {
		store = DefaultCheckpointStore()
	}
	runtimeID := cfg.RuntimeID
	if runtimeID == "" {
		runtimeID = "loop-REQ-039"
	}
	checkpointPath := cfg.CheckpointDir
	if checkpointPath == "" {
		checkpointPath = store.Path(cfg.Root, runtimeID, req.Inspection.BaselineGeneration, idempAssignment(req))
	}

	current, found, err := store.Load(checkpointPath)
	if err != nil {
		return Result{}, fmt.Errorf("load checkpoint: %w", err)
	}
	completionBinding, reportChanged, bindingErr := resolveCompletionReportBinding(cfg.Root, req.Inspection, current, found)
	if bindingErr != nil {
		return Result{Checkpoint: current}, bindingErr
	}
	// A verified/acknowledged/complete checkpoint is reusable only for the
	// exact Result bytes it verified. A changed report re-enters the check
	// stage while retaining MergeCommit, so recovery never merges twice.
	reverifyResult := found && reportChanged && checkpointHasVerifiedStage(current)
	if found && checkpointHasVerifiedStage(current) {
		if current.MergeCommit == "" {
			return preserveInvalidMergeReceipt(store, checkpointPath, current,
				fmt.Errorf("verified checkpoint has no merge receipt; re-inspect and reconcile"))
		}
		if current.TargetBranch == "" || current.TargetBranch != req.Inspection.TargetBranch {
			return preserveInvalidMergeReceipt(store, checkpointPath, current,
				fmt.Errorf("target branch changed from %s to %s after recorded merge", current.TargetBranch, req.Inspection.TargetBranch))
		}
		reachable, reachErr := mergeCommitReachable(ctx, gitRoot, current.MergeCommit, req.Inspection.TargetBranch)
		if reachErr != nil || !reachable {
			return preserveInvalidMergeReceipt(store, checkpointPath, current,
				fmt.Errorf("recorded merge commit %s is not reachable from target %s; re-inspect and reconcile", current.MergeCommit, req.Inspection.TargetBranch))
		}
	}

	// Before the first acknowledgement, checks must cover the current target.
	// Completed historical deliveries and cleanup-only retries retain their
	// original receipt; later assignments must not cause endless revalidation.
	if found && current.State == StateVerified && current.TestedHead != "" {
		target, resolveErr := revParse(ctx, gitRoot, current.TargetBranch)
		if resolveErr != nil {
			return Result{Checkpoint: current}, resolveErr
		}
		if target != current.TestedHead {
			reverifyResult = true
		}
	}

	if !found || current.State != StateComplete || reverifyResult {
		if err := checkoutBranch(ctx, gitRoot, req.Inspection.TargetBranch); err != nil {
			if found {
				return preserveAfterCAS(ctx, store, checkpointPath, current, current, err, ErrCheckFailed)
			}
			return Result{}, err
		}
	}

	if !req.Inspection.Ready {
		// Post-merge ack/cleanup resume may arrive with Inspect Ready=false
		// (ErrMissingCommits after merge). Do not overwrite a verified+
		// durable checkpoint with preserved (BUG-039-38).
		resumeState := checkpointResumeState(current)
		preservedAfterMerge := found && current.State == StatePreserved &&
			current.MergeCommit != "" && !lessThan(resumeState, StateMerged)
		if found && (req.Acknowledge || req.Cleanup) &&
			(current.State == StateVerified ||
				current.State == StateAcknowledged ||
				current.State == StateCleanupPending ||
				current.State == StateComplete ||
				preservedAfterMerge) {
			// Fall through and resume from the durable record.
		} else {
			return preserveFromInspection(cfg, req, "inspect rejected integration")
		}
	}

	idem := IdempotencyKey(
		idempAssignment(req),
		req.Inspection.SourceHead,
		req.Inspection.TargetBranch,
		req.Inspection.BaselineGeneration,
	)

	// CT-039-17: a previous call may have committed the merge but been
	// interrupted before ack. We detect that by looking at the durable
	// state and skip the merge when it has already happened.
	if found && current.MergeCommit != "" && current.SourceHead != req.Inspection.SourceHead && req.Inspection.SourceHead != "" {
		return Result{}, fmt.Errorf("source changed after recorded merge; preserve worktree and reconcile new commits")
	}
	if found {
		if current.AssignmentID != idempAssignment(req) || current.TargetBranch != req.Inspection.TargetBranch || current.SourceBranch != req.Inspection.SourceBranch || current.WorktreePath != req.Inspection.WorktreePath || current.BaselineGeneration != req.Inspection.BaselineGeneration {
			return Result{}, fmt.Errorf("integration checkpoint identity mismatch")
		}
		switch current.State {
		case StateComplete:
			if reverifyResult {
				break
			}
			return Result{Checkpoint: current, Reused: true}, nil
		case StateBlocked, StatePreserved:
			if checkpointResumeState(current) == StateCleanupPending || checkpointResumeState(current) == StateAcknowledged || checkpointResumeState(current) == StateVerified {
				// A fresh successful inspection is the recovery signal. Resume from
				// the last successful stage recorded before preservation. In
				// particular, cleanup failures resume at cleanup_pending and do not
				// re-run verification checks.
				resumed := current
				resumed.State = checkpointResumeState(current)
				resumed.ResumeState = ""
				resumed.FailureReason = ""
				resumed.LastErrorCode = ""
				written, writeErr := store.CompareAndSwap(checkpointPath, current, resumed)
				if writeErr != nil {
					return Result{}, writeErr
				}
				current = written
			} else {
				if req.RetryPreserved && req.Inspection.Ready {
					break
				}
				// A previous failure stops the chain. Re-surfacing the
				// blocker is the right behaviour — we don't try to
				// resurrect a failed integration without an explicit
				// human/builder repair.
				return Result{Checkpoint: current, Reused: true}, nil
			}
		}
	}

	// Ack/cleanup must retain the original inspected coordinates. A post-merge
	// inspection has a different merge base (often the source head itself).
	if found && (current.State == StateVerified || current.State == StateAcknowledged || current.State == StateCleanupPending) {
		if current.AssignmentID != idempAssignment(req) || current.SourceBranch != req.Inspection.SourceBranch || current.TargetBranch != req.Inspection.TargetBranch || current.WorktreePath != req.Inspection.WorktreePath || current.BaselineGeneration != req.Inspection.BaselineGeneration {
			return Result{}, fmt.Errorf("cleanup checkpoint identity mismatch")
		}
		source, err := revParse(ctx, gitRoot, current.SourceBranch)
		if err != nil || source != current.SourceHead {
			return deferCleanup(store, checkpointPath, current, current, fmt.Errorf("cleanup source changed since verification"))
		}
		base, err := mergeBase(ctx, gitRoot, current.MergeCommit, current.TargetBranch)
		if err != nil || base != current.MergeCommit {
			return Result{}, fmt.Errorf("cleanup target no longer contains verified merge")
		}
		req.Inspection.SourceHead = current.SourceHead
		req.Inspection.TargetHead = current.TargetHead
		req.Inspection.MergeBase = current.MergeBase
		idem = current.IdempotencyKey
	}

	if found && current.State == StateReady {
		recovered, err := recoverUnrecordedMerge(ctx, gitRoot, current)
		if err != nil {
			return Result{Checkpoint: current}, err
		}
		if recovered != "" {
			current.MergeCommit = recovered
			prior := current
			prior.MergeCommit = ""
			current.State = StateMerged
			written, err := store.CompareAndSwap(checkpointPath, prior, current)
			if err != nil {
				return Result{}, err
			}
			current = written
			req.Inspection = InspectionFromCheckpoint(current)
		}
	}

	// Build the next-state checkpoint from the inspection + existing
	// durable record. CAS gates the transition.
	next := current
	if found && req.RetryPreserved && (current.State == StateBlocked || current.State == StatePreserved) {
		if current.AssignmentID != idempAssignment(req) || current.SourceBranch != req.Inspection.SourceBranch || current.TargetBranch != req.Inspection.TargetBranch || current.BaselineGeneration != req.Inspection.BaselineGeneration || current.WorktreePath != req.Inspection.WorktreePath {
			return Result{}, fmt.Errorf("retry checkpoint identity mismatch")
		}
		// Start a new checked attempt using CAS against the preserved record.
		// Prior failure remains in the runtime journal; never grant verified.
		next.State = StatePending
		next.FailureReason = ""
		next.LastErrorCode = ""
		next.MergeCommit = ""
		if current.MergeCommit != "" && current.SourceHead == req.Inspection.SourceHead {
			if _, err := RecoveryBaseForCheckpoint(ctx, gitRoot, current); err != nil {
				return Result{}, err
			}
			next.State = StateMerged
			next.MergeCommit = current.MergeCommit
		}
		next.VerifiedAt = ""
		next.TestedHead = ""
	} else if reverifyResult {
		next.State = StateMerged
		next.VerifiedAt = ""
	}
	if next.State == "" {
		next.State = StatePending
	}
	if next.AssignmentID == "" {
		next.AssignmentID = idempAssignment(req)
	}
	if next.TaskID == "" {
		next.TaskID = req.Inspection.TaskID
	}
	if next.WorktreePath == "" {
		next.WorktreePath = req.Inspection.WorktreePath
	}
	next.SourceBranch = req.Inspection.SourceBranch
	next.SourceHead = req.Inspection.SourceHead
	next.TargetBranch = req.Inspection.TargetBranch
	next.TargetHead = req.Inspection.TargetHead
	next.MergeBase = req.Inspection.MergeBase
	next.BaselineGeneration = req.Inspection.BaselineGeneration
	next.IdempotencyKey = idem

	// Step 1: persist ready.
	if lessThan(next.State, StateReady) {
		next.State = StateReady
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist ready: %w", err)
		}
		current = written
	}

	// Step 2: perform the merge (unless durable record shows it already
	// happened). We always use a plain `--no-ff` merge; never squash.
	// REQ-039 §13.6 forbids automatic merge when the integration tree
	// has uncommitted changes — we check the integration tree here too
	// so a dirty target branch is preserved instead of merged into.
	if lessThan(next.State, StateMerged) {
		clean, err := worktreeClean(ctx, gitRoot)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrDirtyWorktree)
		}
		if !clean {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, ErrDirtyWorktree, ErrDirtyWorktree)
		}
		branch, err := defaultRunner.Run(ctx, gitRoot, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil || strings.TrimSpace(branch) != strings.TrimPrefix(req.Inspection.TargetBranch, "refs/heads/") {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("main workspace must remain on target branch %s; found %s", req.Inspection.TargetBranch, strings.TrimSpace(branch)), ErrMergeConflict)
		}
		target, err := revParse(ctx, gitRoot, "HEAD")
		if err != nil || target != req.Inspection.TargetHead {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("target changed after inspection; inspect again"), ErrMergeConflict)
		}
		source, err := revParse(ctx, gitRoot, req.Inspection.SourceBranch)
		if err != nil || source != req.Inspection.SourceHead {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("source changed after inspection"), ErrMergeConflict)
		}
		if err := ctx.Err(); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrMergeConflict)
		}

		sourceHead, sourceErr := revParse(ctx, gitRoot, req.Inspection.SourceBranch)
		if sourceErr != nil || sourceHead != req.Inspection.SourceHead {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("source changed since inspection; re-inspect"), ErrMergeConflict)
		}
		// Recheck the frozen paths under the integration lock immediately before
		// merging. Rename detection is disabled so removals cannot hide a lock.
		if len(req.Inspection.LockedPaths) > 0 {
			base, err := mergeBase(ctx, gitRoot, req.Inspection.TargetBranch, sourceHead)
			if err != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrLockedArtifact)
			}
			changed, err := listChangedFiles(ctx, gitRoot, base, sourceHead)
			if err != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrLockedArtifact)
			}
			if hits := intersectLocked(changed, req.Inspection.LockedPaths); len(hits) > 0 {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next,
					fmt.Errorf("%w: %s", ErrLockedArtifact, strings.Join(hits, ", ")), ErrLockedArtifact)
			}
		}
		mergeCommit, err := performMerge(ctx, gitRoot, req.Inspection.SourceHead)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrMergeConflict)
		}
		next.MergeCommit = mergeCommit
		next.State = StateMerged
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist merged: %w", err)
		}
		current = written
	}

	// Step 3: integration checks → verified.
	if lessThan(next.State, StateVerified) {
		// The report may be edited while the merge is in progress. Re-read it
		// immediately before the verification checks so those checks are tied to
		// the same bytes that Inspect observed.
		if completionBinding.Bound {
			latest, readErr := readCompletionReportBinding(cfg.Root, completionBinding.Path)
			if readErr != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, readErr, ErrMissingCompletion)
			}
			if !reportBindingMatches(completionBinding, latest) {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next,
					fmt.Errorf("%w: before verification", ErrCompletionReportChanged), ErrCompletionReportChanged)
			}
		}
		// Refuse to advance to verified if the target tree is now
		// dirty (e.g. an external process wrote into the integration
		// branch). The worktree-preserved recovery applies.
		clean, err := worktreeClean(ctx, gitRoot)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrDirtyWorktree)
		}
		if !clean {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, ErrDirtyWorktree, ErrDirtyWorktree)
		}
		checkedHead, err := revParse(ctx, gitRoot, "HEAD")
		if err != nil {
			return Result{}, err
		}
		if err := verifyCheckTarget(ctx, gitRoot, next, checkedHead); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
		}
		// Run checks. Default behaviour: if no checks are configured we
		// still advance (per BUG §4.1 "default to none if not
		// specified"). Any failing check stops the chain.
		commands, _ := splitRequiredChecks(cfg.RequiredChecks)
		if len(commands) > 0 {
			for _, command := range commands {
				if cfg.CheckRunner == nil {
					return preserveAfterCAS(ctx, store, checkpointPath, current, next, ErrCheckRunnerMissing, ErrCheckFailed)
				}
				checkCtx := context.WithValue(ctx, checkEvidenceKey{}, checkEvidence{Directory: filepath.Join(filepath.Dir(checkpointPath), "checks"), Receipts: &next.CheckReceipts})
				if err := cfg.CheckRunner(checkCtx, gitRoot, command); err != nil {
					next.FailureReason = fmt.Sprintf("integration check %s failed: %v", command, err)
					next.LastErrorCode = "LOOP_INTEGRATION_CHECK_FAILED"
					return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
				}
			}
		}
		finalHead, headErr := revParse(ctx, gitRoot, "HEAD")
		cleanAfter, cleanErr := worktreeClean(ctx, gitRoot)
		if headErr != nil || cleanErr != nil || finalHead != checkedHead || !cleanAfter {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, fmt.Errorf("integration target changed during checks"), ErrCheckFailed)
		}
		if err := verifyCheckTarget(ctx, gitRoot, next, finalHead); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrCheckFailed)
		}
		next.TestedHead = finalHead
		// A check command can itself rewrite the completion report. Re-read
		// after checks and refuse to certify a different Result.
		if completionBinding.Bound {
			latest, readErr := readCompletionReportBinding(cfg.Root, completionBinding.Path)
			if readErr != nil {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next, readErr, ErrMissingCompletion)
			}
			if !reportBindingMatches(completionBinding, latest) {
				return preserveAfterCAS(ctx, store, checkpointPath, current, next,
					fmt.Errorf("%w: after verification checks", ErrCompletionReportChanged), ErrCompletionReportChanged)
			}
			completionBinding = latest
		}
		clean, err = worktreeClean(ctx, gitRoot)
		if err != nil || !clean {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, ErrDirtyWorktree, ErrDirtyWorktree)
		}
		next.State = StateVerified
		if completionBinding.Bound {
			next.CompletionReportPath = completionBinding.Path
			next.CompletionReportSHA256 = completionBinding.SHA256
		}
		verifiedAt := time.Now().UTC()
		if store.Clock != nil {
			verifiedAt = store.Clock()
		}
		next.VerifiedAt = verifiedAt.UTC().Format(time.RFC3339Nano)
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist verified: %w", err)
		}
		current = written
	}

	// Step 4: acknowledgement. The caller flips Acknowledge after writing
	// the completion_ack (BUG-06). When the caller has not yet
	// acknowledged we stop here and return the verified record — the
	// next Integrate call will resume from this point.
	if lessThan(next.State, StateAcknowledged) {
		if !req.Acknowledge {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		next.State = StateAcknowledged
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist acknowledged: %w", err)
		}
		current = written
	}

	// Step 5: cleanup. Cleanup_pending marks the durable record that
	// the acknowledgement has been written and we are about to remove
	// the worktree; the actual removal (and the transition to complete)
	// only happens when the caller has set req.Cleanup. Cleanup runs
	// only when the tree is clean (BE-039 §8 / REQ-039 §13.6).
	if lessThan(next.State, StateCleanupPending) {
		if !req.Cleanup {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		next.State = StateCleanupPending
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{}, fmt.Errorf("persist cleanup_pending: %w", err)
		}
		current = written
	}

	if lessThan(next.State, StateComplete) {
		if !req.Cleanup {
			return Result{Checkpoint: current, Reused: found}, nil
		}
		if _, statErr := os.Stat(req.Inspection.WorktreePath); !os.IsNotExist(statErr) {
			clean, err := worktreeClean(ctx, req.Inspection.WorktreePath)
			if err != nil {
				return deferCleanup(store, checkpointPath, current, next, err)
			}
			if !clean {
				return deferCleanup(store, checkpointPath, current, next, ErrDirtyWorktree)
			}
			head, err := revParse(ctx, req.Inspection.WorktreePath, "HEAD")
			if err != nil || head != next.SourceHead {
				return deferCleanup(store, checkpointPath, current, next, fmt.Errorf("worktree HEAD changed since reception; preserve it"))
			}
		}

		// Best-effort removal. If git worktree remove fails because
		// the worktree is already gone (e.g. an earlier cleanup
		// succeeded but the ack step was lost), we treat that as
		// success — BE-039 §8 cleanup_response_loss_recovery says
		// reconcile only from durable checkpoints.
		if err := cleanupWorktree(ctx, gitRoot, req.Inspection.WorktreePath); err != nil {
			return deferCleanup(store, checkpointPath, current, next, err)
		}
		next.State = StateComplete
		next.FailureReason = ""
		next.LastErrorCode = ""
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			return Result{Checkpoint: current}, fmt.Errorf("persist complete (cleanup can safely resume): %w", err)
		}
		current = written
	}

	return Result{Checkpoint: current, Reused: found}, nil
}

// A stable HEAD alone does not identify the tree we intended to test. Recovery
// can run after another process switched branches, including to a branch with
// the same commit. Do not checkout here: preserve the user's current location.
func verifyCheckTarget(ctx context.Context, root string, cp Checkpoint, head string) error {
	branch, err := defaultRunner.Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(branch) != strings.TrimPrefix(cp.TargetBranch, "refs/heads/") {
		return fmt.Errorf("integration check requires target branch %s; current branch is %s (error: %v)", cp.TargetBranch, strings.TrimSpace(branch), err)
	}
	target, err := revParse(ctx, root, cp.TargetBranch)
	if err != nil || target != head {
		return fmt.Errorf("integration check HEAD does not match target branch %s", cp.TargetBranch)
	}
	base, err := mergeBase(ctx, root, cp.MergeCommit, head)
	if cp.MergeCommit == "" || err != nil || base != cp.MergeCommit {
		return fmt.Errorf("integration check target no longer contains recorded merge %s", cp.MergeCommit)
	}
	return nil
}

// preserveAfterCAS writes a `preserved` checkpoint describing the failure
// mode and returns a Result with the failure reason populated. The
// worktree is intentionally retained; this satisfies BE-039 §8's
// "failure_before_cleanup_preserves_worktree_and_branch" rule.
func preserveAfterCAS(ctx context.Context, store *CheckpointStore, path string, current Checkpoint, next Checkpoint, cause error, sentinel error) (Result, error) {
	preserved := next
	preserved.ResumeState = successfulResumeState(next.State)
	preserved.State = StatePreserved
	if cause != nil {
		if preserved.FailureReason == "" {
			preserved.FailureReason = cause.Error()
		}
		if preserved.LastErrorCode == "" {
			preserved.LastErrorCode = stableErrorCode(sentinel)
		}
	}
	written, err := store.CompareAndSwap(path, current, preserved)
	if err != nil {
		return Result{Checkpoint: preserved}, fmt.Errorf("persist preserved: %w", err)
	}
	return Result{Checkpoint: written}, sentinel
}

// preserveInvalidMergeReceipt handles a verified-or-later checkpoint whose
// recorded merge is no longer reachable from the currently bound target. The
// old receipt remains in the durable record for audit, but ResumeState is
// explicitly reset to pending so a later reconciliation may create a new
// merge. Keeping this as preserved prevents a stale receipt from certifying or
// deleting a worker, while descendants of the recorded merge remain valid
// because mergeCommitReachable uses ancestry rather than HEAD equality.
func preserveInvalidMergeReceipt(store *CheckpointStore, path string, current Checkpoint, cause error) (Result, error) {
	preserved := current
	preserved.State = StatePreserved
	preserved.ResumeState = StatePending
	preserved.FailureReason = cause.Error()
	preserved.LastErrorCode = stableErrorCode(ErrMergeConflict)
	written, err := store.ForceWrite(path, preserved)
	if err != nil {
		return Result{Checkpoint: preserved}, fmt.Errorf("persist preserved: %w", err)
	}
	return Result{Checkpoint: written}, fmt.Errorf("%w: %v", ErrMergeConflict, cause)
}

// successfulResumeState returns the last durable stage boundary represented by
// next before it is changed to StatePreserved. The state machine only calls
// preserveAfterCAS after the stage in next has either been persisted or is the
// stage currently being attempted, so this is the exact retry boundary.
func successfulResumeState(state string) string {
	switch state {
	case StatePending, StateReady, StateMerged, StateVerified, StateAcknowledged, StateCleanupPending, StateComplete:
		return state
	default:
		return ""
	}
}

// checkpointResumeState supports checkpoints written before ResumeState was
// introduced. A recorded merge is the safest legacy boundary; otherwise a
// retry starts from pending and rebuilds the ready/merge stages.
func checkpointResumeState(cp Checkpoint) string {
	if state := successfulResumeState(cp.ResumeState); state != "" {
		return state
	}
	if cp.MergeCommit != "" {
		return StateMerged
	}
	return StatePending
}

// preserveFromInspection persists a preserved checkpoint when the
// caller hands Integrate a non-ready Inspection. Existing durable progress
// is retained; a new record is created through CAS.
func preserveFromInspection(cfg IntegrateConfig, req IntegrateRequest, reason string) (Result, error) {
	store := cfg.CheckpointStore
	if store == nil {
		store = DefaultCheckpointStore()
	}
	runtimeID := cfg.RuntimeID
	if runtimeID == "" {
		runtimeID = "loop-REQ-039"
	}
	checkpointPath := cfg.CheckpointDir
	if checkpointPath == "" {
		checkpointPath = store.Path(cfg.Root, runtimeID, req.Inspection.BaselineGeneration, idempAssignment(req))
	}

	idem := IdempotencyKey(
		idempAssignment(req),
		req.Inspection.SourceHead,
		req.Inspection.TargetBranch,
		req.Inspection.BaselineGeneration,
	)

	cp := Checkpoint{
		AssignmentID:       idempAssignment(req),
		TaskID:             req.Inspection.TaskID,
		WorktreePath:       req.Inspection.WorktreePath,
		SourceBranch:       req.Inspection.SourceBranch,
		SourceHead:         req.Inspection.SourceHead,
		TargetBranch:       req.Inspection.TargetBranch,
		TargetHead:         req.Inspection.TargetHead,
		MergeBase:          req.Inspection.MergeBase,
		BaselineGeneration: req.Inspection.BaselineGeneration,
		State:              StatePreserved,
		IdempotencyKey:     idem,
		Blockers:           append([]string(nil), req.Inspection.Blockers...),
		FailureReason:      reason,
		LastErrorCode:      "LOOP_INTEGRATION_CONFLICT",
		LockedDiff:         append([]string(nil), req.Inspection.LockedDiff...),
	}
	current, found, err := store.Load(checkpointPath)
	if err != nil {
		return Result{}, err
	}
	if found {
		return Result{Checkpoint: current, Reused: true}, nil
	}
	written, err := store.CompareAndSwap(checkpointPath, current, cp)
	if err != nil {
		return Result{Checkpoint: cp}, fmt.Errorf("persist preserved: %w", err)
	}
	return Result{Checkpoint: written}, nil
}

// idempAssignment extracts the assignment id used as the durable
// checkpoint identity. Canonical order (BUG-039-38):
//
//  1. Inspection.AssignmentID (populated by Inspect from the assignment)
//  2. "assignment:<id>" blocker sentinel (legacy / test harness)
//  3. WorktreePath fallback (last resort; must not be the production key)
func idempAssignment(req IntegrateRequest) string {
	if id := strings.TrimSpace(req.Inspection.AssignmentID); id != "" {
		return id
	}
	if id := deriveAssignmentFromBlockers(req.Inspection.Blockers); id != "" {
		return id
	}
	return req.Inspection.WorktreePath
}

func deriveAssignmentFromBlockers(blockers []string) string {
	for _, b := range blockers {
		if strings.HasPrefix(b, "assignment:") {
			return strings.TrimPrefix(b, "assignment:")
		}
	}
	return ""
}

// lessThan is the canonical state ordering for the integration state
// machine. We use it instead of switch+rank to keep the precedence in one
// place and to make adding intermediate states cheap.
func lessThan(a, b string) bool {
	return stateRank(a) < stateRank(b)
}

func stateRank(s string) int {
	switch s {
	case StatePending:
		return 0
	case StateReady:
		return 1
	case StateMerged:
		return 2
	case StateVerified:
		return 3
	case StateAcknowledged:
		return 4
	case StateCleanupPending:
		return 5
	case StateComplete:
		return 6
	}
	return -1
}

// stableErrorCode maps internal sentinels to the LOOP_* codes the caller
// surfaces in the controller error envelope. The codes here are the ones
// defined in SYNC-039 §11; the Integrator does not emit caller-safe
// message text, that is the controller's job.
func stableErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrCheckFailed):
		return "LOOP_INTEGRATION_CHECK_FAILED"
	case errors.Is(err, ErrMergeConflict):
		return "LOOP_INTEGRATION_CONFLICT"
	case errors.Is(err, ErrDirtyWorktree):
		return "LOOP_INTEGRATION_PARTIAL"
	case errors.Is(err, ErrLockedArtifact):
		return "LOOP_LOCKED_ARTIFACT"
	case errors.Is(err, ErrScopeViolation):
		return "LOOP_SCOPE_VIOLATION"
	case errors.Is(err, ErrSquashForbidden):
		return "LOOP_SQUASH_MERGE"
	default:
		return "LOOP_INTEGRATION_PARTIAL"
	}
}

// LessThan is exposed as a public helper so external callers (e.g.
// tests in sibling packages) can reason about state precedence without
// duplicating the rank table.
func LessThan(a, b string) bool { return lessThan(a, b) }

func integrationDurationStatus(result Result, err error) string {
	if err != nil {
		switch {
		case errors.Is(err, ErrDirtyWorktree), errors.Is(err, ErrMergeConflict), errors.Is(err, ErrSquashForbidden), errors.Is(err, ErrScopeViolation):
			return "preserved"
		default:
			return "error"
		}
	}
	switch result.Checkpoint.State {
	case StatePreserved:
		return "preserved"
	case StateBlocked:
		return "blocked"
	case StateComplete, StateVerified, StateMerged, StateAcknowledged, StateCleanupPending, StateReady:
		return "success"
	default:
		return "success"
	}
}
