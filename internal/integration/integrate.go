package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// The function is idempotent at the (assignment_id + target_branch +
// baseline_generation) key. Re-calling Integrate with the
// same idempotent key after the merge succeeded but before
// acknowledgement (CT-039-17) resumes the state machine from `merged` and
// does NOT re-merge. Opted-in S6 Records additionally select this checkpoint
// by their already-resolved immutable revision; a later revision cannot read
// the predecessor slot's verification or acknowledgement.
//
// The caller controls the merge step by setting IntegrateRequest.Cleanup.
// Acknowledge is automatic once verified; a separate flag is provided so
// BUG-06 can call Integrate twice — once for the merge/verify phase and
// again after writing the completion_ack.
func Integrate(ctx context.Context, req IntegrateRequest, cfg IntegrateConfig) (Result, error) {
	return integrateByResponsibility(ctx, req, cfg)
}

func mainWorktreeDirtyFailure(root string) error {
	return fmt.Errorf("Main integration checkout %q is dirty; inspect with git -C %q status --short: %w", root, root, ErrDirtyWorktree)
}

func mainWorktreeCheckFailure(root string, cause error) error {
	return fmt.Errorf("Main integration checkout %q clean check failed; inspect with git -C %q status --short: %w", root, root, cause)
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
	if cause != nil {
		return Result{Checkpoint: written}, fmt.Errorf("%v; Builder checkout %q preserved: %w", cause, written.WorktreePath, sentinel)
	}
	return Result{Checkpoint: written}, sentinel
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

// checkpointResumeState returns only an actually recorded boundary. An old
// Git object ID cannot prove whether an interrupted merge completed.
func checkpointResumeState(cp Checkpoint) string {
	if state := successfulResumeState(cp.ResumeState); state != "" {
		return state
	}
	return ""
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
