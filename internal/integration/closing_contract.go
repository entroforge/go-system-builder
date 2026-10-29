package integration

import (
	"errors"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
)

// ErrClosingContractPending blocks Main integration and acknowledgement when
// the accepted owner Result does not close the frozen S6 responsibility.
var ErrClosingContractPending = errors.New("S6 closing contract is pending; Main integration and acknowledgement are forbidden")

func canonicalClosingBlockers(canonical *canonicalResultFacts) []string {
	if canonical == nil || canonical.Record.ClosingContract == nil {
		return nil
	}
	var blockers []string
	assessment := canonical.Accepted.Answer.ClosingAssessment
	if assessment == nil {
		blockers = append(blockers, "S6 closing pending: owner Result has no closing_assessment")
	} else if assessment.Disposition != "complete" {
		blockers = append(blockers, fmt.Sprintf("S6 closing pending: owner closing_assessment is %q", assessment.Disposition))
	} else if !assignmentresult.ClosingAssessmentComplete(canonical.Accepted.Answer) {
		blockers = append(blockers, "S6 closing pending: owner closing_assessment does not satisfy every frozen DoneWhen")
	}
	if len(canonical.Accepted.Answer.RisksBlockers) > 0 {
		blockers = append(blockers, "S6 closing pending: owner Result declares risks or blockers")
	}
	if len(canonical.Accepted.Answer.ScopeDeviations) > 0 {
		blockers = append(blockers, "S6 closing pending: owner Result declares scope deviations")
	}
	if len(canonical.Record.ClosingContract.WorkerChecks) > 0 && !assignmentresult.WorkerClosureComplete(canonical.Record, canonical.Accepted) {
		blockers = append(blockers, "S6 closing pending: owned_check_evidence_unavailable")
	}
	return blockers
}

func preserveCanonicalClosingPending(store *CheckpointStore, path string, current Checkpoint, found bool, inspection Inspection, record assignmentrecord.Record, blockers []string) (Result, error) {
	reason := strings.Join(blockers, "; ")
	if reason == "" {
		return Result{Checkpoint: current}, fmt.Errorf("%w: missing closing blocker detail", ErrClosingContractPending)
	}
	resume := StatePending
	if found && current.MergeCommit != "" {
		// Preserve the exact old merge and Main facts as history, but future
		// current-Result checks must start from the merged boundary.
		resume = StateMerged
	}
	if found {
		if current.AssignmentID != record.AssignmentID || current.TaskID != "" && current.TaskID != record.Task.ID {
			return Result{Checkpoint: current}, fmt.Errorf("canonical closing blocker checkpoint identity mismatch")
		}
		if current.BaselineGeneration != 0 && current.BaselineGeneration != record.BaselineGeneration ||
			current.SourceBranch != "" && current.SourceBranch != inspection.SourceBranch ||
			current.TargetBranch != "" && current.TargetBranch != inspection.TargetBranch {
			return Result{Checkpoint: current}, fmt.Errorf("canonical closing blocker checkpoint coordinates mismatch")
		}
		if current.State == StatePreserved && current.ResumeState == resume && current.FailureReason == reason && hasAllStrings(current.Blockers, blockers) {
			return Result{Checkpoint: current, Reused: true}, fmt.Errorf("%w: %s", ErrClosingContractPending, reason)
		}
		next := current
		next.State = StatePreserved
		next.ResumeState = resume
		for _, blocker := range blockers {
			next.Blockers = appendUniqueString(next.Blockers, blocker)
		}
		next.FailureReason = reason
		next.LastErrorCode = "LOOP_S6_CLOSING_PENDING"
		written, err := store.CompareAndSwap(path, current, next)
		if err != nil {
			return Result{Checkpoint: current}, fmt.Errorf("preserve historical Main receipt while closing is pending: %w", err)
		}
		return Result{Checkpoint: written}, fmt.Errorf("%w: %s", ErrClosingContractPending, reason)
	}
	checkpoint := Checkpoint{
		AssignmentID:       record.AssignmentID,
		TaskID:             record.Task.ID,
		WorktreePath:       inspection.WorktreePath,
		SourceBranch:       inspection.SourceBranch,
		SourceHead:         inspection.SourceHead,
		TargetBranch:       inspection.TargetBranch,
		TargetHead:         inspection.TargetHead,
		MergeBase:          inspection.MergeBase,
		BaselineGeneration: record.BaselineGeneration,
		State:              StatePreserved,
		ResumeState:        resume,
		IdempotencyKey:     IdempotencyKey(record.AssignmentID, inspection.SourceHead, inspection.TargetBranch, record.BaselineGeneration),
		Blockers:           append([]string{}, blockers...),
		FailureReason:      reason,
		LastErrorCode:      "LOOP_S6_CLOSING_PENDING",
	}
	ref := record.Reference()
	sha, err := record.Digest()
	if err != nil {
		return Result{}, err
	}
	checkpoint.AssignmentRecord = &ref
	checkpoint.AssignmentRecordSHA256 = sha
	written, err := store.CompareAndSwap(path, current, checkpoint)
	if err != nil {
		return Result{Checkpoint: checkpoint}, fmt.Errorf("persist canonical closing blocker: %w", err)
	}
	return Result{Checkpoint: written}, fmt.Errorf("%w: %s", ErrClosingContractPending, reason)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func hasAllStrings(values, required []string) bool {
	for _, item := range required {
		if !containsString(values, item) {
			return false
		}
	}
	return len(required) > 0
}
