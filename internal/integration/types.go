// Package integration implements the durable worktree integration service
// described by REQ-039 §13.6, SYNC-039 §8, BE-039 §8 and ARCHITECTURE-039 §12.
//
// The package exposes a single Integrator surface that owns the inspect →
// merge → verify → acknowledge → cleanup state machine. The state machine is
// persisted to a sidecar checkpoint JSON via atomic writes with an embedded
// CAS key so that Inspect/Integrate are idempotent across retries (CT-039-17)
// and the merge response can be reconciled without re-running the merge when
// a downstream step was interrupted.
//
// The Integrator is called by the Controller's SubagentStop wiring (owned by
// BUG-06) once that controller patch is in place. This package does NOT
// modify internal/cli/controller.go (BUG-06 territory).
//
// Forbidden behaviours (BUG-039-05 §4.2):
//
//   - No automatic squash merge. The merge is always `git merge --no-ff`.
//   - No automatic conflict resolution.
//   - No force-delete of a worktree with uncommitted changes.
//   - No overwrite of locked artifacts (locked-diff check is part of Inspect).
//   - No `--no-verify` / `--no-gpg-sign` / `--amend` / force-push.
package integration

import (
	"errors"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

// Checkpoint states (BE-039 §8 / SYNC-039 §8 / ARCHITECTURE-039 §12).
//
//	pending → ready → merged → verified → acknowledged → cleanup_pending → complete
//	             \___________________________________________________→ blocked
//	             \___________________________________________________→ preserved
//
// `pending` is the initial value the agent's checkpoint arrives in (before
// Inspect declares Ready). `preserved` is the failure recovery value used
// when a precondition fails or the merge/check/ack chain breaks before
// cleanup_pending — in that case the worktree is intentionally retained.
const (
	StatePending        = "pending"
	StateReady          = "ready"
	StateMerged         = "merged"
	StateVerified       = "verified"
	StateAcknowledged   = "acknowledged"
	StateCleanupPending = "cleanup_pending"
	StateComplete       = "complete"
	StateBlocked        = "blocked"
	StatePreserved      = "preserved"
)

// InspectRequest is the input to Inspect. It carries the assignment context
// (sourced from hookctx.LoadedContext.Assignments) plus the target branch
// and baseline generation observed by the caller.
type InspectRequest struct {
	// Root is the repository root that contains the worktree under
	// inspection. All git operations are executed relative to this root.
	Root string

	// Assignment is the active assignment whose worktree is being integrated.
	// Fields used by Inspect: AssignmentID, TaskID, WorktreePath, Branch,
	// TargetBranch, CompletionRef, WritePaths.
	Assignment hookctx.AssignmentContext

	// TargetBranch is the integration branch the caller expects the merge to
	// land on. It must equal the assignment's recorded TargetBranch.
	TargetBranch string

	// BaselineGeneration is the generation observed at inspect time. It is
	// used to build the idempotent checkpoint key.
	BaselineGeneration int

	// RuntimeID selects the evidence tree under `.claude/evidence/<id>/`
	// when locating the completion report. Empty falls back to
	// "loop-REQ-039" and a broader evidence scan.
	RuntimeID string
}

// CheckResult mirrors the SYNC-039 §8 {"command","status"} record shape.
// Status is normalised to "pass", "fail" or "skip" by Inspect.
type CheckResult struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Output  string `json:"output,omitempty"`
}

// Inspection is the structured outcome of Inspect. The Integrator passes
// this struct to Integrate so the decision is reproducible from a recorded
// snapshot (idempotency).
type Inspection struct {
	Ready          bool          `json:"ready"`
	Blockers       []string      `json:"blockers,omitempty"`
	AssignmentID   string        `json:"assignment_id,omitempty"`
	WorktreePath   string        `json:"worktree_path,omitempty"`
	SourceBranch   string        `json:"source_branch,omitempty"`
	TargetBranch   string        `json:"target_branch,omitempty"`
	SourceHead     string        `json:"source_head,omitempty"`
	TargetHead     string        `json:"target_head,omitempty"`
	MergeBase      string        `json:"merge_base,omitempty"`
	RequiredChecks []CheckResult `json:"required_checks,omitempty"`
	LockedDiff     []string      `json:"locked_diff,omitempty"`
	Conflicts      []string      `json:"conflicts,omitempty"`
	NonSquashMode  bool          `json:"non_squash_mode"`

	// BaselineGeneration is echoed from InspectRequest so callers can build
	// the idempotent checkpoint key without re-supplying it.
	BaselineGeneration int `json:"baseline_generation"`
}

// IntegrateRequest feeds Integrate. Inspection is the recorded inspect
// snapshot and ExpectedRevision is the CAS key used to gate the checkpoint
// persistence (see CheckpointStore.CompareAndSwap).
type IntegrateRequest struct {
	Inspection       Inspection
	ExpectedRevision int64
	// Acknowledge, when true, advances the state machine from verified to
	// acknowledged and then to cleanup_pending. BUG-06 calls Integrate
	// again with Acknowledge=true after SubagentStop writes the
	// completion_ack so the merge response can be reconciled (CT-039-17).
	Acknowledge bool
	// Cleanup, when true, performs the worktree removal step and
	// transitions to complete. Cleanup runs only when the tree is clean,
	// the merge is recorded, the integration checks have been recorded as
	// verified, and acknowledgement is recorded.
	Cleanup bool
}

// Result is what Integrate returns. MilestoneUpdate is an optional map the
// caller (BUG-06 / Controller) can apply to its Milestone projection; this
// package does NOT write the runtime Milestone directly because that path
// is owned by BUG-06.
type Result struct {
	Checkpoint      Checkpoint     `json:"checkpoint"`
	MilestoneUpdate map[string]any `json:"milestone_update,omitempty"`
	Reused          bool           `json:"reused"`
}

// Checkpoint is the durable integration record. The IdempotencyKey is the
// merge-attempt identity (assignment_id + source_head + target_branch +
// baseline_generation); CAS uses Revision as the optimistic lock.
type Checkpoint struct {
	AssignmentID       string   `json:"assignment_id"`
	TaskID             string   `json:"task_id,omitempty"`
	SourceBranch       string   `json:"source_branch,omitempty"`
	SourceHead         string   `json:"source_head,omitempty"`
	TargetBranch       string   `json:"target_branch,omitempty"`
	TargetHead         string   `json:"target_head,omitempty"`
	MergeBase          string   `json:"merge_base,omitempty"`
	MergeCommit        string   `json:"merge_commit,omitempty"`
	BaselineGeneration int      `json:"baseline_generation"`
	State              string   `json:"state"`
	Revision           int64    `json:"revision"`
	IdempotencyKey     string   `json:"idempotency_key,omitempty"`
	Blockers           []string `json:"blockers,omitempty"`
	FailureReason      string   `json:"failure_reason,omitempty"`
	LastErrorCode      string   `json:"last_error_code,omitempty"`
	LockedDiff         []string `json:"locked_diff,omitempty"`
	UpdatedAt          string   `json:"updated_at"`
}

// CheckpointPath returns the canonical on-disk path for a checkpoint. The
// layout mirrors the completion-evidence directory used elsewhere in REQ-039
// so an operator can find the durable record with the same tool:
//
//	<root>/.claude/evidence/<runtime_id>/g<gen>/worktree/<assignment_id>/checkpoint.json
func CheckpointPath(root, runtimeID string, baselineGeneration int, assignmentID string) string {
	return DefaultCheckpointStore().Path(root, runtimeID, baselineGeneration, assignmentID)
}

// ErrLockedArtifact is returned when the inspect-time diff touches a locked
// artifact. Callers map this to LOOP_LOCKED_ARTIFACT.
var ErrLockedArtifact = errors.New("locked artifact touched by worktree diff")

// ErrSquashForbidden is returned when the caller asks for a non-plain merge
// (currently this can only happen via misuse of the API; Inspect's
// NonSquashMode check is what catches this).
var ErrSquashForbidden = errors.New("squash merge is forbidden; integration requires --no-ff")

// ErrMergeConflict is returned when merge-tree reports a conflict. The
// worktree is preserved (state preserved).
var ErrMergeConflict = errors.New("merge conflict detected; worktree preserved")

// ErrDirtyWorktree is returned when the worktree has uncommitted changes.
// The worktree is preserved (state preserved).
var ErrDirtyWorktree = errors.New("worktree is dirty; worktree preserved")

// ErrMissingCompletion is returned when the completion report is missing
// or schema-invalid for the assignment.
var ErrMissingCompletion = errors.New("completion report missing or invalid")

// ErrMissingCommits is returned when the source branch has no commits
// beyond the merge base.
var ErrMissingCommits = errors.New("source branch has no commits beyond merge base")

// ErrMissingTarget is returned when the target branch does not exist on
// disk / git refs.
var ErrMissingTarget = errors.New("target branch does not exist")

// ErrCASStale is returned when the checkpoint file's Revision does not
// match ExpectedRevision. Callers should re-read the checkpoint and decide
// whether to retry or surface a stale-merge error.
var ErrCASStale = errors.New("checkpoint revision is stale")
