package integration

import (
	"context"
	"errors"
	"fmt"
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
	// CheckRunner runs the integration checks. nil = skip.
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
	start := time.Now()
	defer func() {
		if cfg.Root == "" {
			return
		}
		_ = metrics.RecordIntegrationDuration(cfg.Root, integrationDurationStatus(result, err), time.Since(start).Milliseconds())
	}()
	if cfg.Root == "" {
		return Result{}, errors.New("config.Root is required")
	}
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
	gitRoot := cfg.GitRoot
	if gitRoot == "" {
		gitRoot = cfg.Root
	}

	checkpointPath := cfg.CheckpointDir
	if checkpointPath == "" {
		checkpointPath = store.Path(cfg.Root, runtimeID, req.Inspection.BaselineGeneration, idempAssignment(req))
	}

	current, found, err := store.Load(checkpointPath)
	if err != nil {
		return Result{}, fmt.Errorf("load checkpoint: %w", err)
	}

	if !req.Inspection.Ready {
		// Post-merge ack/cleanup resume may arrive with Inspect Ready=false
		// (ErrMissingCommits after merge). Do not overwrite a verified+
		// durable checkpoint with preserved (BUG-039-38).
		if found && (req.Acknowledge || req.Cleanup) &&
			(current.State == StateVerified ||
				current.State == StateAcknowledged ||
				current.State == StateCleanupPending ||
				current.State == StateComplete) {
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
	if found {
		switch current.State {
		case StateComplete:
			return Result{Checkpoint: current, Reused: true}, nil
		case StateBlocked, StatePreserved:
			// A previous failure stops the chain. Re-surfacing the
			// blocker is the right behaviour — we don't try to
			// resurrect a failed integration without an explicit
			// human/builder repair.
			return Result{Checkpoint: current, Reused: true}, nil
		}
	}

	// Build the next-state checkpoint from the inspection + existing
	// durable record. CAS gates the transition.
	next := current
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
		if err := checkoutBranch(ctx, gitRoot, req.Inspection.TargetBranch); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrMergeConflict)
		}
		mergeCommit, err := performMerge(ctx, gitRoot, req.Inspection.SourceBranch)
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
		// Run checks. Default behaviour: if no checks are configured we
		// still advance (per BUG §4.1 "default to none if not
		// specified"). Any failing check stops the chain.
		if len(cfg.RequiredChecks) > 0 {
			for _, command := range cfg.RequiredChecks {
				if cfg.CheckRunner == nil {
					continue
				}
				if err := cfg.CheckRunner(ctx, gitRoot, command); err != nil {
					next.FailureReason = fmt.Sprintf("integration check %s failed: %v", command, err)
					next.LastErrorCode = "LOOP_INTEGRATION_CONFLICT"
					return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrMergeConflict)
				}
			}
		}
		next.State = StateVerified
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
		// Belt-and-braces: refuse to delete a dirty worktree.
		clean, err := worktreeClean(ctx, req.Inspection.WorktreePath)
		if err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrDirtyWorktree)
		}
		if !clean {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, ErrDirtyWorktree, ErrDirtyWorktree)
		}
		// Best-effort removal. If git worktree remove fails because
		// the worktree is already gone (e.g. an earlier cleanup
		// succeeded but the ack step was lost), we treat that as
		// success — BE-039 §8 cleanup_response_loss_recovery says
		// reconcile only from durable checkpoints.
		if err := removeWorktree(ctx, gitRoot, req.Inspection.WorktreePath); err != nil {
			return preserveAfterCAS(ctx, store, checkpointPath, current, next, err, ErrDirtyWorktree)
		}
		next.State = StateComplete
		written, err := store.CompareAndSwap(checkpointPath, current, next)
		if err != nil {
			// If we successfully removed the worktree but the
			// durable write failed we still want to return a useful
			// signal — fall back to ForceWrite so the durable
			// record says complete (the next reconcile pass will
			// see the absent worktree anyway).
			written, ferr := store.ForceWrite(checkpointPath, next)
			if ferr != nil {
				return Result{Checkpoint: written}, fmt.Errorf("persist complete: %w", err)
			}
			current = written
		} else {
			current = written
		}
	}

	return Result{Checkpoint: current, Reused: found}, nil
}

// preserveAfterCAS writes a `preserved` checkpoint describing the failure
// mode and returns a Result with the failure reason populated. The
// worktree is intentionally retained; this satisfies BE-039 §8's
// "failure_before_cleanup_preserves_worktree_and_branch" rule.
func preserveAfterCAS(ctx context.Context, store *CheckpointStore, path string, current Checkpoint, next Checkpoint, cause error, sentinel error) (Result, error) {
	preserved := next
	preserved.State = StatePreserved
	if cause != nil {
		if preserved.FailureReason == "" {
			preserved.FailureReason = cause.Error()
		}
		if preserved.LastErrorCode == "" {
			preserved.LastErrorCode = stableErrorCode(sentinel)
		}
	}
	written, err := store.ForceWrite(path, preserved)
	if err != nil {
		return Result{Checkpoint: preserved}, fmt.Errorf("persist preserved: %w", err)
	}
	return Result{Checkpoint: written}, sentinel
}

// preserveFromInspection persists a preserved checkpoint when the
// caller hands Integrate a non-ready Inspection. We use ForceWrite
// because no prior CAS owner exists in that scenario.
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
	written, err := store.ForceWrite(checkpointPath, cp)
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
