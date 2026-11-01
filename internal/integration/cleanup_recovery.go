package integration

import (
	"context"
	"errors"
	"fmt"
)

var ErrCleanupPending = errors.New("integration verified; worktree cleanup pending")

func InspectionFromCheckpoint(cp Checkpoint) Inspection {
	out := Inspection{
		CompletionReportPath: cp.CompletionReportPath,
		ChangedPaths:         append([]string{}, cp.ChangedPaths...),
		Ready:                true,
		AssignmentID:         cp.AssignmentID,
		TaskID:               cp.TaskID,
		WorktreePath:         cp.WorktreePath,
		SourceBranch:         cp.SourceBranch,
		TargetBranch:         cp.TargetBranch,
		BaselineGeneration:   cp.BaselineGeneration,
		NonSquashMode:        true,
	}
	if cp.CanonicalMainFacts != nil {
		binding := cp.CanonicalMainFacts.Binding
		out.CanonicalResult = &binding
		out.ChangedPaths = append([]string{}, cp.CanonicalMainFacts.ChangedPaths...)
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
	state := cp.State
	if state == StatePreserved || state == StateBlocked {
		state = cp.ResumeState
	}
	switch state {
	case StateMerged, StateVerified, StateAcknowledged, StateCleanupPending, StateComplete:
		// The saved completed step is usable without reconstructing a Git OID.
		return "", nil
	default:
		return "", fmt.Errorf("the original checkpoint does not record a completed merge")
	}
}

const readyMergeHistoryLimit = 256

// RecoverUnrecordedMerge observes the original ready intent in bounded
// first-parent history. It writes nothing and grants no check/ACK authority.
func RecoverUnrecordedMerge(ctx context.Context, root string, cp Checkpoint) (string, error) {
	if cp.State != StateReady {
		return "", fmt.Errorf("checkpoint is not in the merge-intent window")
	}
	return "", fmt.Errorf("merge outcome is unknown after interruption; preserve the checkpoint and worktree for Main")
}
