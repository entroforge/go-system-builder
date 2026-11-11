package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// RepairResultLocalEvidence is system-owned source capture, not an authored
// report. SourcePath is relative to provenance.ExecutionRoot; Path is in Main.
type RepairResultLocalEvidence struct {
	AuthoredRef string `json:"authored_ref"`
	SourcePath  string `json:"source_path"`
	Path        string `json:"path"`
}

// RepairResultCandidateProvenance is embedded in the original immutable S9
// WorkspaceCandidate. It proves which answer produced the proposal, not that
// the candidate was received. DeliveryRef and the original journal establish
// reception; no candidate file contains its own hash or predicted sequence.
type RepairResultPrivateSubmission struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type RepairResultCandidateProvenance struct {
	PrivateSubmissions []RepairResultPrivateSubmission `json:"private_submissions,omitempty"`
	LocalEvidence      []RepairResultLocalEvidence     `json:"local_evidence,omitempty"`
	Contract           string                          `json:"contract"`
	PacketID           string                          `json:"packet_id"`

	IssuanceEventID string `json:"issuance_event_id"`

	SubmittedAnswer    json.RawMessage `json:"submitted_answer"`
	OwnerAgentID       string          `json:"owner_agent_id"`
	RuntimeID          string          `json:"runtime_id"`
	BaselineGeneration int             `json:"baseline_generation"`

	ExecutionAssignmentID string `json:"execution_assignment_id"`
	ExecutionGeneration   int    `json:"execution_generation"`

	ExecutionRoot string `json:"execution_root"`

	PlanReportRef string `json:"plan_report_ref"`

	ReworkRef string `json:"rework_ref"`

	Resume *workpacket.RepairResultResume `json:"resume,omitempty"`

	WorkerCheckReceipts []checkexec.ToolReceipt         `json:"worker_check_receipts"`
	OwnedCheckReceipts  []RepairResultOwnedCheckReceipt `json:"owned_check_receipts"`
}

type RepairResultOwnedCheckReceipt struct {
	WorkerCheckIndex int                   `json:"worker_check_index"`
	Receipt          checkexec.ToolReceipt `json:"receipt"`
}

func (p *RepairResultCandidateProvenance) UnmarshalJSON(data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairResultCandidateProvenance
	var v wire
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	answer, err := canonicalRuntimeJSON(v.SubmittedAnswer)
	if err != nil {
		return fmt.Errorf("S9 candidate submitted answer: %w", err)
	}
	v.SubmittedAnswer = answer
	*p = RepairResultCandidateProvenance(v)
	return nil
}

// RepairResultCandidateReceipt is a read-only projection of original delivery
// facts. Current source/check authority must be evaluated by the S9 consumer
// before projecting a fresh permission or the owner's present work_complete.
type RepairResultCandidateReceipt struct {
	Provenance   RepairResultCandidateProvenance `json:"provenance"`
	CandidateRef string                          `json:"candidate_ref"`

	AcceptedEventID              string `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int    `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int    `json:"accepted_sequence"`
}

func decodeOriginalRepairCandidateReceipt(data []byte, receipt *RepairResultCandidateReceipt) error {
	return json.Unmarshal(data, receipt)
}

// MainProof records the verified projection, not the mutable checkpoint's
// entire bytes;
// ordinary ACK and cleanup must retain the verified facts.
type RepairResultMainProof struct {
	CheckpointRef string `json:"checkpoint_ref"`

	VerifiedFacts json.RawMessage `json:"verified_facts"`
}

func (p *RepairResultMainProof) UnmarshalJSON(data []byte) error {
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return err
	}
	type wire RepairResultMainProof
	var v wire
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	facts, err := canonicalRuntimeJSON(v.VerifiedFacts)
	if err != nil {
		return fmt.Errorf("S9 verified Main facts: %w", err)
	}
	v.VerifiedFacts = facts
	*p = RepairResultMainProof(v)
	return nil
}

// RepairResultReceipt is stored only in the original immutable RepairResult.
// The original Runtime result refs locate the containing artifact.
type RepairResultReceipt struct {
	Contract     string                          `json:"contract"`
	Provenance   RepairResultCandidateProvenance `json:"provenance"`
	CandidateRef string                          `json:"candidate_ref"`

	ResultID                     string                `json:"result_id"`
	ResultRef                    string                `json:"result_ref"`
	MainProof                    RepairResultMainProof `json:"main_proof"`
	WorkComplete                 bool                  `json:"work_complete"`
	AcceptedEventID              string                `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string                `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int                   `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int                   `json:"accepted_sequence"`
}

func decodeOriginalRepairResultReceipt(data []byte, receipt *RepairResultReceipt) error {
	return json.Unmarshal(data, receipt)
}

// ProjectRepairResultMainFacts is a pure projection shared by the original
// integration consumer and receipt reader. It is not an executor or verifier.
// Mutable lifecycle and diagnostic fields are deliberately excluded.
func ProjectRepairResultMainFacts(checkpointJSON []byte) (json.RawMessage, error) {
	if err := workpacket.ValidateUniqueJSON(checkpointJSON); err != nil {
		return nil, err
	}
	var cp map[string]any
	if err := json.Unmarshal(checkpointJSON, &cp); err != nil || cp == nil {
		return nil, fmt.Errorf("S9 Main checkpoint must be an object")
	}
	if stringValue(cp["assignment_id"]) == "" {
		return nil, fmt.Errorf("S9 Main checkpoint lacks original responsibility")
	}
	projection := map[string]any{}
	for _, key := range []string{"assignment_id", "task_id", "baseline_generation", "assignment_record", "source_branch", "target_branch", "worktree_path", "changed_paths", "canonical_main_facts", "completion_report_path", "verified_at", "idempotency_key", "check_receipts", "tool_check_receipts"} {
		if value, ok := cp[key]; ok && value != nil {
			projection[key] = value
		}
	}
	data, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	return canonicalRuntimeJSON(data)
}
