// Package runtime's TASK-039-06 ordered extension. Per docs/tasks/index-039.md
// §7, TASK-039-06 owns internal/integration/ and is allowed to add exactly
// one ordered-extension file under internal/runtime/. That file is this one.
//
// The ordered extension is intentionally minimal — it surfaces the
// integration helpers the runtime store needs to recognise when it
// processes a SubagentStop transition (BUG-06 territory). It MUST NOT
// modify runtime.Store, runtime.Mutation or the journal envelope; doing
// so would create a write-path race with TASK-039-05.
//
// The exported names here are the contract surface BUG-06 will wire
// into internal/cli/controller.go. They are independent of any state
// mutation, so introducing them now does not change runtime behaviour.
package runtime

import (
	"context"
	"fmt"
	"strings"
)

// IntegrationCheckpointEnvelope is the contract surface BUG-06 uses to
// pass a recorded integration checkpoint into the runtime Journal entry
// for a SubagentStop transition. The fields mirror the on-disk
// checkpoint JSON written by internal/integration (BE-039 §8 / SYNC-039
// §8 / ARCHITECTURE-039 §12). The struct lives here, not in
// internal/integration, because the runtime journal is the canonical
// audit trail — placing the contract surface in internal/runtime
// prevents accidental coupling between the controller wiring and the
// integration package's internal types.
type IntegrationCheckpointEnvelope struct {
	AssignmentID       string   `json:"assignment_id"`
	SourceBranch       string   `json:"source_branch,omitempty"`
	SourceHead         string   `json:"source_head,omitempty"`
	TargetBranch       string   `json:"target_branch,omitempty"`
	MergeCommit        string   `json:"merge_commit,omitempty"`
	BaselineGeneration int      `json:"baseline_generation,omitempty"`
	State              string   `json:"state"`
	IdempotencyKey     string   `json:"idempotency_key,omitempty"`
	LastErrorCode      string   `json:"last_error_code,omitempty"`
	FailureReason      string   `json:"failure_reason,omitempty"`
	Blockers           []string `json:"blockers,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
}

// Validate enforces the structural contract BUG-06 must respect when it
// embeds an IntegrationCheckpointEnvelope into a runtime mutation.
// Validation is intentionally conservative: missing required fields
// raise an error, anything else is permitted so future checkpoint
// additions do not break older runtime versions.
func (e IntegrationCheckpointEnvelope) Validate() error {
	if strings.TrimSpace(e.AssignmentID) == "" {
		return fmt.Errorf("integration envelope: assignment_id is required")
	}
	if strings.TrimSpace(e.State) == "" {
		return fmt.Errorf("integration envelope: state is required")
	}
	switch e.State {
	case "pending", "ready", "merged", "verified", "acknowledged",
		"cleanup_pending", "complete", "blocked", "preserved":
	default:
		return fmt.Errorf("integration envelope: unknown state %q", e.State)
	}
	return nil
}

// IntegrationCheckpointContext is the read-side counterpart to
// IntegrationCheckpointEnvelope. It is what runtime exposes (via
// hookctx.LoadedContext.IntegrationCheckpoint) so the controller can
// find the durable checkpoint file before invoking the Integrator.
type IntegrationCheckpointContext struct {
	AssignmentID       string `json:"assignment_id"`
	CheckpointPath     string `json:"checkpoint_path"`
	BaselineGeneration int    `json:"baseline_generation,omitempty"`
}

// ResolveIntegrationCheckpointPath returns the canonical
// `<root>/.claude/evidence/<runtime>/g<gen>/worktree/<assignment>/checkpoint.json`
// path. It is exposed here (rather than inside internal/integration) so
// the runtime layer can hand the controller a path before the
// integration package has been imported.
//
// The current generation defaults to 1 because REQ-039 ships at baseline
// generation 1; tests may pass a different value.
func ResolveIntegrationCheckpointPath(root, runtimeID, assignmentID string, baselineGeneration int) string {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	if strings.TrimSpace(runtimeID) == "" {
		runtimeID = "loop-REQ-039"
	}
	if strings.TrimSpace(assignmentID) == "" {
		assignmentID = "unknown"
	}
	if baselineGeneration <= 0 {
		baselineGeneration = 1
	}
	return fmt.Sprintf("%s/.claude/evidence/%s/g%d/worktree/%s/checkpoint.json",
		root, runtimeID, baselineGeneration, assignmentID)
}

// EnvelopeFromState is a thin helper that lets runtime-level tests
// construct a deterministic envelope from a plain checkpoint summary.
// It is intentionally lossy — callers that need the full checkpoint
// must load it via internal/integration.
func EnvelopeFromState(assignmentID, state, idempotencyKey string, baselineGeneration int) IntegrationCheckpointEnvelope {
	return IntegrationCheckpointEnvelope{
		AssignmentID:       assignmentID,
		BaselineGeneration: baselineGeneration,
		State:              state,
		IdempotencyKey:     idempotencyKey,
	}
}

// Compile-time interface check: the runtime layer does not need to
// depend on internal/integration at compile time, but it does need to
// promise that any future helper here does not block the context.
var _ = context.Background
