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
	SHA256      string `json:"sha256"`
}

// RepairResultCandidateProvenance is embedded in the original immutable S9
// WorkspaceCandidate. It proves which answer produced the proposal, not that
// the candidate was received. DeliveryRef/SHA and the original journal prove
// reception; no candidate file contains its own hash or predicted sequence.
type RepairResultPrivateSubmission struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type RepairResultCandidateProvenance struct {
	PrivateSubmissions      []RepairResultPrivateSubmission `json:"private_submissions,omitempty"`
	LocalEvidence           []RepairResultLocalEvidence     `json:"local_evidence,omitempty"`
	Contract                string                          `json:"contract"`
	PacketID                string                          `json:"packet_id"`
	PacketSHA256            string                          `json:"packet_sha256"`
	IssuanceEventID         string                          `json:"issuance_event_id"`
	AnswerSHA256            string                          `json:"answer_sha256"`
	SubmittedAnswer         json.RawMessage                 `json:"submitted_answer"`
	OwnerAgentID            string                          `json:"owner_agent_id"`
	RuntimeID               string                          `json:"runtime_id"`
	BaselineGeneration      int                             `json:"baseline_generation"`
	ReviewRound             int                             `json:"review_round"`
	ExecutionAssignmentID   string                          `json:"execution_assignment_id"`
	ExecutionGeneration     int                             `json:"execution_generation"`
	ExecutionSHA256         string                          `json:"execution_sha256"`
	ExecutionRoot           string                          `json:"execution_root"`
	SourceHead              string                          `json:"source_head"`
	BaseCommit              string                          `json:"base_commit"`
	PlanReportRef           string                          `json:"plan_report_ref"`
	PlanReportSHA256        string                          `json:"plan_report_sha256"`
	CompletionReportRef     string                          `json:"completion_report_ref,omitempty"`
	CompletionReportSHA256  string                          `json:"completion_report_sha256,omitempty"`
	CompletionReportEventID string                          `json:"completion_report_event_id,omitempty"`
	ReworkRef               string                          `json:"rework_ref"`
	ReworkSHA256            string                          `json:"rework_sha256"`
	Resume                  *workpacket.RepairResultResume  `json:"resume,omitempty"`
	ResumeSHA256            string                          `json:"resume_sha256,omitempty"`
	WorkerCheckReceipts     []checkexec.ToolReceipt         `json:"worker_check_receipts"`
	OwnedCheckReceipts      []RepairResultOwnedCheckReceipt `json:"owned_check_receipts"`
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
	Provenance                   RepairResultCandidateProvenance `json:"provenance"`
	CandidateRef                 string                          `json:"candidate_ref"`
	CandidateSHA256              string                          `json:"candidate_sha256"`
	AcceptedEventID              string                          `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string                          `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int                             `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int                             `json:"accepted_sequence"`
}

// MainProof records the verified projection, not the mutable checkpoint's
// entire bytes. CheckpointSHA256 is the acceptance-time audit identity only;
// ordinary ACK and cleanup must retain the verified facts.
type RepairResultMainProof struct {
	CheckpointRef       string          `json:"checkpoint_ref"`
	CheckpointSHA256    string          `json:"checkpoint_sha256"`
	VerifiedFacts       json.RawMessage `json:"verified_facts"`
	VerifiedFactsSHA256 string          `json:"verified_facts_sha256"`
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
// The containing artifact's hash comes from the original Runtime result refs.
type RepairResultReceipt struct {
	Contract                     string                          `json:"contract"`
	Provenance                   RepairResultCandidateProvenance `json:"provenance"`
	CandidateRef                 string                          `json:"candidate_ref"`
	CandidateSHA256              string                          `json:"candidate_sha256"`
	ResultID                     string                          `json:"result_id"`
	ResultRef                    string                          `json:"result_ref"`
	MainProof                    RepairResultMainProof           `json:"main_proof"`
	WorkComplete                 bool                            `json:"work_complete"`
	AcceptedEventID              string                          `json:"accepted_event_id"`
	AcceptedIdempotencyKey       string                          `json:"accepted_idempotency_key"`
	AcceptedAfterRuntimeRevision int                             `json:"accepted_after_runtime_revision"`
	AcceptedSequence             int                             `json:"accepted_sequence"`
}

// ProjectRepairResultMainFacts is a pure projection shared by the original
// integration consumer and receipt reader. It is not an executor or verifier.
// Mutable lifecycle and diagnostic fields are deliberately excluded.
func ProjectRepairResultMainFacts(checkpointJSON []byte) (json.RawMessage, error) {
	if err := workpacket.ValidateUniqueJSON(checkpointJSON); err != nil {
		return nil, err
	}
	var cp map[string]json.RawMessage
	if err := json.Unmarshal(checkpointJSON, &cp); err != nil || cp == nil {
		return nil, fmt.Errorf("S9 Main checkpoint must be an object")
	}
	for _, key := range []string{"assignment_id", "baseline_generation", "source_head", "merge_commit", "tested_head", "verified_at"} {
		if len(cp[key]) == 0 || string(cp[key]) == "null" || string(cp[key]) == `""` {
			return nil, fmt.Errorf("S9 verified Main checkpoint lacks %s", key)
		}
	}
	projection := map[string]json.RawMessage{}
	for _, key := range []string{
		"assignment_id", "task_id", "baseline_generation", "assignment_record", "assignment_record_sha256",
		"source_branch", "source_head", "target_branch", "target_head", "merge_base", "merge_commit", "tested_head",
		"worktree_path", "changed_paths", "changed_paths_sha256", "canonical_main_facts",
		"completion_report_path", "completion_report_sha256", "verified_at", "idempotency_key",
		"check_receipts", "tool_check_receipts",
	} {
		v := cp[key]
		if len(v) == 0 {
			if key == "check_receipts" || key == "tool_check_receipts" {
				v = json.RawMessage("[]")
			} else {
				v = json.RawMessage("null")
			}
		}
		projection[key] = v
	}
	data, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	return canonicalRuntimeJSON(data)
}
