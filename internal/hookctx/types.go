package hookctx

import "github.com/entroforge/go-system-builder/internal/policy"

// AssignmentContext is the read-only view of one active agent assignment as
// projected into the Hook Context. It is sourced from runtime.entities.tasks[]
// (for status/task_id) and from .claude/workgroups/REQ-039/<TASK>/manifest.json
// (for assignment_id, agent_id, write_paths and report status). The fields
// here intentionally do NOT include any path the loader could mutate — the
// Hook is observation-only on the runtime, per SYNC-039 §3 / §8.
type AssignmentContext struct {
	AssignmentID      string   `json:"assignment_id"`
	TaskID            string   `json:"task_id"`
	OwnerAgentID      string   `json:"owner_agent_id"`
	WorktreePath      string   `json:"worktree_path,omitempty"`
	Branch            string   `json:"branch,omitempty"`
	TargetBranch      string   `json:"target_branch,omitempty"`
	State             string   `json:"state"`
	ManifestRef       string   `json:"manifest_ref,omitempty"`
	ReportStatus      string   `json:"report_status,omitempty"`
	CompletionRef     string   `json:"completion_ref,omitempty"`
	CompletionAckRef  string   `json:"completion_ack_ref,omitempty"`
	WritePaths        []string `json:"write_paths,omitempty"`
	ResponsibilityIDs []string `json:"responsibility_ids,omitempty"`
}

// IntegrationCheckpoint mirrors the schema from SYNC-039 §8 for an active
// worktree integration record. The fields are populated from the runtime's
// milestone.integration block (or entities.worktree_integration once that is
// canonical — only the former is observable today).
//
// Checks carries the SYNC-039 §8 record shape:
// {"command": "go test ./...", "status": "pass"} — the loader preserves
// the raw shape (command+status) so the Integrator can inspect both
// fields. Earlier variants stored just `[]string`, but that drops the
// status field and is incompatible with the contract.
type IntegrationCheckpoint struct {
	AssignmentID       string             `json:"assignment_id"`
	AgentID            string             `json:"agent_id"`
	WorktreePath       string             `json:"worktree_path"`
	Branch             string             `json:"branch"`
	TargetBranch       string             `json:"target_branch"`
	ReportRef          string             `json:"report_ref,omitempty"`
	MergeMode          string             `json:"merge_mode,omitempty"`
	Status             string             `json:"status"`
	Checks             []IntegrationCheck `json:"checks,omitempty"`
	LockedPaths        []string           `json:"locked_paths_touched,omitempty"`
	SourceHead         string             `json:"source_head,omitempty"`
	MergeCommit        string             `json:"merge_commit,omitempty"`
	LastErrorCode      string             `json:"last_error_code,omitempty"`
	IdempotencyKey     string             `json:"idempotency_key,omitempty"`
	BaselineGeneration int                `json:"baseline_generation,omitempty"`
}

// IntegrationCheck is one record inside IntegrationCheckpoint.Checks,
// matching SYNC-039 §8: {"command": "go test ./...", "status": "pass"}.
type IntegrationCheck struct {
	Command string `json:"command"`
	Status  string `json:"status"`
}

// LoadedContext is the wrapper returned by Load / LoadFull. It pairs the
// engine-side policy.RuntimeContext (which the Safety Policy consumes
// directly) with assignment + worktree checkpoint surfaces that the
// Controller and Worktree Integrator consumers need but which are NOT
// internal to the policy engine today. Putting them on a separate wrapper
// keeps policy/engine.go untouched (BUG-01 territory) while still letting
// Hook observe locked artifacts and active worktrees.
//
// The Controller (BUG-02) and Integrator (BUG-05) consume *LoadedContext;
// the Safety Policy continues to consume LoadedContext.PolicyContext
// directly.
type LoadedContext struct {
	PolicyContext         policy.RuntimeContext  `json:"policy_context"`
	Assignments           []AssignmentContext    `json:"assignments,omitempty"`
	IntegrationCheckpoint *IntegrationCheckpoint `json:"integration_checkpoint,omitempty"`
	// BaselineGeneration is copied from the snapshot for downstream
	// generation-aware consumers (Controller / Integrator) without forcing
	// them to re-parse the policy context.
	BaselineGeneration int `json:"baseline_generation,omitempty"`
}
